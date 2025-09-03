package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/formatter"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/mikhae1/kubectl-quackops/pkg/mcp"
	"github.com/tmc/langchaingo/llms"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Chat orchestrates a chat completion with the provided llms.Model, handling
// history, streaming, retries, token accounting, and MCP tool calls.
func Chat(cfg *config.Config, client llms.Model, prompt string, stream bool, history bool) (string, error) {
	humanMessage := llms.HumanChatMessage{Content: prompt}
	if history {
		cfg.ChatMessages = append(cfg.ChatMessages, humanMessage)
	}

	var messages []llms.MessageContent
	for _, msg := range cfg.ChatMessages {
		var role llms.ChatMessageType
		var content string

		switch msg.GetType() {
		case llms.ChatMessageTypeHuman:
			role = llms.ChatMessageTypeHuman
			content = msg.GetContent()
		case llms.ChatMessageTypeAI:
			role = llms.ChatMessageTypeAI
			content = msg.GetContent()
		case llms.ChatMessageTypeSystem:
			role = llms.ChatMessageTypeSystem
			content = msg.GetContent()
		default:
			role = llms.ChatMessageTypeGeneric
			content = msg.GetContent()
		}

		messages = append(messages, llms.TextParts(role, content))
	}

	// Include the current prompt only when history is disabled to avoid duplication
	if !history {
		messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, prompt))
	}

	logger.Log("info", "Sending request to %s/%s with %d messages in history", cfg.Provider, cfg.Model, len(messages))

	generateOptions := []llms.CallOption{}

	// Use default temperature values - no configuration needed
	if strings.Contains(cfg.Model, "gpt-5") {
		// gpt-5 models require temperature 1.0
		generateOptions = append(generateOptions, llms.WithTemperature(1.0))
	} else {
		// All other models use temperature 0.0 for deterministic responses
		// generateOptions = append(generateOptions, llms.WithTemperature(0.0))
	}

	var mcpToolReserve int = 0
	if cfg.MCPClientEnabled {
		llmTools := mcp.DiscoverLangchainTools(cfg)
		if len(llmTools) > 0 {
			generateOptions = append(generateOptions, llms.WithTools(llmTools))
			generateOptions = append(generateOptions, llms.WithToolChoice("auto"))
			logger.Log("info", "Exposed %d MCP tools to model: %v", len(llmTools), mcp.ExtractToolNames(llmTools))
			// Reserve additional tokens for MCP tool definitions and results
			// Estimate: ~200 tokens per tool definition + ~1000 tokens per potential tool result
			mcpToolReserve = len(llmTools)*200 + cfg.MCPMaxToolCalls*1000
		}
	}

	if lib.EffectiveMaxTokens(cfg) > 0 {
		effective := lib.EffectiveMaxTokens(cfg)
		limit := effective

		// Reserve tokens for input to avoid exceeding context window
		inputTokenReserve := int(float64(limit) * float64(cfg.InputTokenReservePercent) / 100.0)
		if inputTokenReserve < cfg.MinInputTokenReserve {
			inputTokenReserve = cfg.MinInputTokenReserve
		}

		// Add MCP tool token reserve if tools are enabled
		totalReserve := inputTokenReserve + mcpToolReserve

		// Calculate actual available tokens for output
		availableForOutput := limit - totalReserve
		if availableForOutput < cfg.MinOutputTokens {
			availableForOutput = cfg.MinOutputTokens
		}

		logger.Log("info", "Token allocation: limit=%d, input_reserve=%d, mcp_reserve=%d, available_output=%d",
			limit, inputTokenReserve, mcpToolReserve, availableForOutput)

		generateOptions = append(generateOptions, llms.WithMaxTokens(availableForOutput))
	}

	outgoingTokens := lib.CountTokensWithConfig(cfg, prompt, cfg.ChatMessages)
	cfg.LastOutgoingTokens = outgoingTokens
	cfg.LastIncomingTokens = 0

	tokenMeter := lib.NewTokenMeter(cfg, outgoingTokens)

	spinnerManager := lib.GetSpinnerManager(cfg)
	message := fmt.Sprintf("Waiting for %s/%s... [↑%s tokens] (ESC to cancel)",
		cfg.Provider, cfg.Model, config.Colors.Dim.Sprint(lib.FormatCompactNumber(outgoingTokens)))
	cancelSpinner := spinnerManager.ShowLLM(message)

	var stopOnce sync.Once

	var callbackFn func(ctx context.Context, chunk []byte) error
	var cleanupFn func()
	var contentAlreadyDisplayed bool

	// For MCP-enabled configurations, we need to handle tool calls synchronously
	// So we disable streaming initially and re-enable it for final responses
	useStreaming := stream && !cfg.MCPClientEnabled

	// startEscBreaker starts a raw-input watcher to cancel the context on ESC.
	startEscBreaker := func(ctx context.Context, cancel context.CancelFunc) func() {
		fd := int(os.Stdin.Fd())
		if !term.IsTerminal(fd) {
			return func() {}
		}
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return func() {}
		}
		stopCh := make(chan struct{})
		restored := false
		go func() {
			defer func() {
				if !restored {
					_ = term.Restore(fd, oldState)
				}
			}()
			for {
				select {
				case <-stopCh:
					return
				case <-ctx.Done():
					return
				default:
				}
				var readfds unix.FdSet
				readfds.Set(fd)
				tv := unix.Timeval{Sec: 0, Usec: 200000}
				_, selErr := unix.Select(fd+1, &readfds, nil, nil, &tv)
				if selErr != nil {
					continue
				}
				if readfds.IsSet(fd) {
					var b [1]byte
					n, _ := os.Stdin.Read(b[:])
					if n > 0 {
						switch b[0] {
						case 27: // ESC or start of escape sequence
							// Cancel only on a lone ESC. If bytes follow quickly, it's an escape sequence; swallow it.
							isLoneEsc := true
							for {
								var rfd unix.FdSet
								rfd.Set(fd)
								tv2 := unix.Timeval{Sec: 0, Usec: 50000}
								_, sel2 := unix.Select(fd+1, &rfd, nil, nil, &tv2)
								if sel2 != nil || !rfd.IsSet(fd) {
									break
								}
								var seq [1]byte
								n2, _ := os.Stdin.Read(seq[:])
								if n2 > 0 {
									isLoneEsc = false
									// Keep draining until timeout so the full sequence is consumed (e.g., ESC [ A)
									continue
								}
								break
							}
							if isLoneEsc {
								spinnerManager.Update("Cancelling...")
								cancel()
								return
							}
							// Not a lone ESC: swallow and continue (arrow keys, etc.)
							continue
						case 3: // Ctrl+C -> SIGINT
							_ = term.Restore(fd, oldState)
							restored = true
							lib.CleanupAndExit(cfg, lib.CleanupOptions{ExitCode: -1})
							_ = unix.Kill(os.Getpid(), unix.SIGINT)
							return
						case 26: // Ctrl+Z -> SIGTSTP
							_ = term.Restore(fd, oldState)
							restored = true
							lib.CleanupAndExit(cfg, lib.CleanupOptions{ExitCode: -1})
							_ = unix.Kill(os.Getpid(), unix.SIGTSTP)
							return
						case 28: // Ctrl+\ -> SIGQUIT
							_ = term.Restore(fd, oldState)
							restored = true
							lib.CleanupAndExit(cfg, lib.CleanupOptions{ExitCode: -1})
							_ = unix.Kill(os.Getpid(), unix.SIGQUIT)
							return
						}
					}
				}
			}
		}()
		return func() {
			close(stopCh)
			if !restored {
				_ = term.Restore(fd, oldState)
			}
		}
	}

	if useStreaming {
		onFirstChunk := func() {
			stopOnce.Do(func() {
				spinnerManager.Hide()
				fmt.Fprint(os.Stdout, "\n")
			})
		}

		callbackFn, cleanupFn = createStreamingCallback(cfg, spinnerManager, nil, onFirstChunk)
		defer cleanupFn()

		defer func() {
			stopOnce.Do(func() {
				spinnerManager.Hide()
			})
		}()

		generateOptions = append(generateOptions, llms.WithStreamingFunc(callbackFn))
	} else {
		defer cancelSpinner()
	}

	maxRetries := cfg.Retries
	backoffFactor := 3.0
	initialBackoff := 10.0
	var responseContent string
	var bufferedToolBlocks []string
	var lastError error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			var delay time.Duration
			var messageType string

			// Check if the last error was a 429 rate limit error
			if lib.Is429Error(lastError) {
				retryDelay, parseErr := lib.ParseRetryDelay(lastError)
				if parseErr == nil {
					// Successfully parsed provider delay - use it
					delay = retryDelay
					messageType = "Rate limited"
					logger.Log("info", "Retrying after parsed rate limit delay: %v (attempt %d/%d)", delay, attempt, maxRetries)
				} else {
					// Failed to parse 429 retry delay - fallback to exponential backoff for 429 errors
					delay = lib.CalculateExponentialBackoff(attempt, initialBackoff, backoffFactor)
					messageType = "Rate limited"
					logger.Log("info", "Failed to parse 429 retry delay, using exponential backoff: %v", parseErr)
					logger.Log("info", "Retrying 429 error with backoff in %v (attempt %d/%d)", delay, attempt, maxRetries)
				}
			} else {
				// Use exponential backoff for non-429 errors
				delay = lib.CalculateExponentialBackoff(attempt, initialBackoff, backoffFactor)
				messageType = "Retrying"
				logger.Log("info", "Retrying in %v (attempt %d/%d)", delay, attempt, maxRetries)
			}

			// Apply the retry delay with countdown
			applyRetryDelayWithCountdown(spinnerManager, cfg, delay, attempt, maxRetries, outgoingTokens, messageType)
			// Restore original spinner message after retry delay
			cancelSpinner = spinnerManager.ShowLLM(message)
		}

		// Apply throttling delay before making the request (including retries)
		applyThrottleDelayWithSpinnerManager(cfg, spinnerManager)
		// Ensure LLM spinner is active after throttling delay
		if !spinnerManager.IsActive() {
			cancelSpinner = spinnerManager.ShowLLM(message)
		}

		if len(messages) == 0 {
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, prompt))
		}

		ctx, cancel := context.WithCancel(context.Background())
		stopEsc := startEscBreaker(ctx, cancel)
		resp, err := client.GenerateContent(ctx, messages, generateOptions...)
		stopEsc()
		if err != nil {
			if ctx.Err() == context.Canceled || lib.IsUserCancel(err) {
				// Map to a unified UserCancelError and stop retries
				return "", lib.NewUserCancelError("canceled by user")
			}
			lastError = err
			retriesLeft := maxRetries - attempt

			// Handle 429 errors with special display logic
			if lib.Is429Error(err) {
				if retriesLeft == 0 {
					// No more retries left: show detailed ⚠️ error message
					lib.Display429Error(err, cfg, maxRetries)
					// Return error to exit (will be handled by interactive mode logic)
				} else {
					// Has retries left: only show spinner/waiting message (no ⚠️ message)
					logger.Log("debug", "429 Rate limit error - will retry with delay (%d retries left)", retriesLeft)
					continue
				}
			} else if retriesLeft > 0 {
				// Non-429 errors with retries left: show regular error message
				errorMsg := lib.GetErrorMessage(err)
				if errorMsg != "" {
					fmt.Printf("%s\n", color.RedString(errorMsg))
				} else {
					fmt.Printf("%s\n", color.RedString(err.Error()))
				}
				continue
			}

			if attempt == maxRetries {
				errorMsg := lib.GetErrorMessage(err)
				if errorMsg != "" {
					return "", fmt.Errorf("AI still returning error after %d retries (%s): %w", maxRetries, errorMsg, err)
				}
				return "", fmt.Errorf("AI still returning error after %d retries: %w", maxRetries, err)
			}
			errorMsg := lib.GetErrorMessage(err)
			if errorMsg != "" {
				return "", fmt.Errorf("%s: %w", errorMsg, err)
			}
			return "", err
		}

		// Update response timestamp for throttling calculations
		updateResponseTime()

		if cfg.MCPClientEnabled {
			toolCallCount := 0
			maxToolCalls := cfg.MCPMaxToolCalls
			for {
				if resp == nil || len(resp.Choices) == 0 {
					break
				}
				choice := resp.Choices[0]

				// Display content if available and not already displayed
				if choice.Content != "" && !contentAlreadyDisplayed {
					stopOnce.Do(func() {
						spinnerManager.Hide()
						fmt.Fprint(os.Stdout, "\n")
					})
					if cfg.DisableMarkdownFormat {
						fmt.Fprintln(os.Stdout, choice.Content)
					} else {
						w := formatter.NewStreamingWriter(os.Stdout)
						_, _ = w.Write([]byte(choice.Content))
						_ = w.Flush()
						_ = w.Close()
					}
					contentAlreadyDisplayed = true
				}

				if len(choice.ToolCalls) > 0 {
					if toolCallCount >= maxToolCalls {
						logger.Log("warn", "Maximum MCP tool call limit (%d) reached, stopping tool execution", maxToolCalls)
						break
					}

					logger.Log("info", "Processing MCP tool call: iteration %d of %d...", toolCallCount+1, maxToolCalls)

					// Append the assistant message containing tool_calls so providers can match tool_call_id
					assistantParts := make([]llms.ContentPart, 0, len(choice.ToolCalls))
					for i := range choice.ToolCalls {
						tc := choice.ToolCalls[i]
						if tc.FunctionCall == nil {
							logger.Log("warn", "Tool call %s has no function call data", tc.ID)
							continue
						}
						if tc.ID == "" {
							tc.ID = fmt.Sprintf("tool_%s_%d", tc.FunctionCall.Name, time.Now().UnixNano())
							logger.Log("debug", "Generated missing tool call ID: %s", tc.ID)
						}
						assistantParts = append(assistantParts, tc)
						choice.ToolCalls[i] = tc
					}
					if len(assistantParts) > 0 {
						assistantMsg := llms.MessageContent{Role: llms.ChatMessageTypeAI, Parts: assistantParts}
						messages = append(messages, assistantMsg)
					}

					for _, tc := range choice.ToolCalls {
						logger.Log("debug", "Processing tool call: ID=%q, FunctionCall=%v", tc.ID, tc.FunctionCall != nil)
						if tc.FunctionCall == nil {
							logger.Log("warn", "Tool call %s has no function call data", tc.ID)
							continue
						}

						var args map[string]any
						if strings.TrimSpace(tc.FunctionCall.Arguments) != "" {
							if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
								logger.Log("warn", "Failed to parse tool arguments for %s: %v", tc.FunctionCall.Name, err)
								args = map[string]any{"raw": tc.FunctionCall.Arguments}
							}
						} else {
							args = map[string]any{}
						}

						// Apply throttling delay before each MCP tool execution with iteration info
						customMessage := fmt.Sprintf("Processing MCP tool call: %d of %d...", toolCallCount+1, maxToolCalls)
						applyThrottleDelayWithCustomMessageManager(cfg, spinnerManager, customMessage)

						logger.Log("info", "Executing MCP tool: %s with args: %v", tc.FunctionCall.Name, args)
						toolResult, callErr := mcp.ExecuteTool(cfg, tc.FunctionCall.Name, args)
						if callErr != nil {
							logger.Log("warn", "MCP tool %s failed: %v", tc.FunctionCall.Name, callErr)
							toolResult = fmt.Sprintf("Error executing tool '%s': %v", tc.FunctionCall.Name, callErr)
						} else {
							logger.Log("info", "MCP tool %s executed successfully, result length: %d", tc.FunctionCall.Name, len(toolResult))
						}

						// Update response timestamp for throttling calculations after MCP tool execution
						updateResponseTime()

						var block string
						if cfg.Verbose {
							block = mcp.FormatToolCallVerbose(tc.FunctionCall.Name, args, toolResult)
						} else {
							block = mcp.FormatToolCallBlock(tc.FunctionCall.Name, args, toolResult)
						}

						if useStreaming {
							bufferedToolBlocks = append(bufferedToolBlocks, block)
						} else {
							fmt.Fprint(os.Stdout, block)
						}

						toolMsg := llms.MessageContent{
							Role: llms.ChatMessageTypeTool,
							Parts: []llms.ContentPart{llms.ToolCallResponse{
								ToolCallID: tc.ID,
								Name:       tc.FunctionCall.Name,
								Content:    toolResult,
							}},
						}
						messages = append(messages, toolMsg)
					}

					toolCallCount++

					// Apply throttling delay for MCP tool call follow-up requests
					applyThrottleDelayWithSpinnerManager(cfg, spinnerManager)

					ctxTool, cancelTool := context.WithCancel(context.Background())
					stopEscTool := startEscBreaker(ctxTool, cancelTool)
					resp, err = client.GenerateContent(ctxTool, messages, generateOptions...)
					stopEscTool()
					if err != nil {
						if ctxTool.Err() == context.Canceled || lib.IsUserCancel(err) {
							return "", lib.NewUserCancelError("canceled by user")
						}
						if lib.Is429Error(err) {
							lib.Display429Error(err, cfg, maxRetries)
						}
						if attempt == maxRetries {
							errorMsg := lib.GetErrorMessage(err)
							if errorMsg != "" {
								return "", fmt.Errorf("AI still returning error after %d retries post-tool (%s): %w", maxRetries, errorMsg, err)
							}
							return "", fmt.Errorf("AI still returning error after %d retries post-tool: %w", maxRetries, err)
						}
						errorMsg := lib.GetErrorMessage(err)
						if errorMsg != "" {
							return "", fmt.Errorf("%s: %w", errorMsg, err)
						}
						return "", err
					}

					// Update response timestamp for throttling calculations after MCP tool follow-up
					updateResponseTime()
					continue
				}
				break
			}
		}

		if resp != nil && len(resp.Choices) > 0 {
			responseContent = resp.Choices[0].Content
			cfg.LastIncomingTokens = lib.EstimateTokens(cfg, responseContent)
		}

		// Check if we got empty content and should retry
		if responseContent == "" {
			if attempt < maxRetries {
				logger.Log("warn", "Received empty content from %s/%s", cfg.Provider, cfg.Model)
				lastError = fmt.Errorf("no content generated from %s", cfg.Provider)
				continue // This will trigger the backoff and retry
			}
		}
		break
	}

	if history && responseContent != "" {
		cfg.ChatMessages = append(cfg.ChatMessages, llms.AIChatMessage{Content: responseContent})
	}

	if responseContent == "" {
		if lastError != nil {
			return "", fmt.Errorf("no content generated from %s after retries, last error: %w", cfg.Provider, lastError)
		}
		return "", fmt.Errorf("no content generated from %s", cfg.Provider)
	}

	if !useStreaming {
		tokenMeter.AddIncoming(lib.EstimateTokens(cfg, responseContent))
		// Only display output if not already displayed during MCP processing
		if !contentAlreadyDisplayed {
			spinnerManager.Hide()
			fmt.Fprint(os.Stdout, "\n")
			if len(bufferedToolBlocks) > 0 {
				for _, b := range bufferedToolBlocks {
					fmt.Fprint(os.Stdout, b)
				}
			}
			if cfg.DisableMarkdownFormat {
				fmt.Fprintln(os.Stdout, responseContent)
				fmt.Fprintln(os.Stdout)
			} else {
				w := formatter.NewStreamingWriter(os.Stdout)
				_, _ = w.Write([]byte(responseContent))
				_ = w.Flush()
				_ = w.Close()
				fmt.Fprintln(os.Stdout)
			}
		} else {
			// Content already displayed, just ensure spinner is stopped and add buffered tool blocks
			spinnerManager.Hide()
			if len(bufferedToolBlocks) > 0 {
				for _, b := range bufferedToolBlocks {
					fmt.Fprint(os.Stdout, b)
				}
			}
			fmt.Fprintln(os.Stdout)
		}
	}

	return responseContent, nil
}

// applyRetryDelayWithCountdown applies a retry delay with spinner countdown
func applyRetryDelayWithCountdown(spinnerManager *lib.SpinnerManager, cfg *config.Config, delay time.Duration, attempt int, maxRetries int, outgoingTokens int, messageType string) {
	// Fast path for tests: when SkipWaits is enabled, skip delays entirely
	if cfg != nil && cfg.SkipWaits {
		return
	}
	
	baseMessage := fmt.Sprintf("%s - retrying %s/%s (attempt %d/%d) [↑%s tokens]",
		messageType, cfg.Provider, cfg.Model, attempt, maxRetries, lib.FormatCompactNumber(outgoingTokens))
	
	cancelRetrySpinner := spinnerManager.ShowWithCountdown(lib.SpinnerThrottle, baseMessage, delay)
	time.Sleep(delay)
	cancelRetrySpinner()
}
