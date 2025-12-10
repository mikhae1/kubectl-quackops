package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/formatter"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/mikhae1/kubectl-quackops/pkg/mcp"
	"github.com/tmc/langchaingo/llms"
)

// Chat orchestrates a chat completion with the provided llms.Model, handling
// history, streaming, retries, token accounting, and MCP tool calls.
func Chat(cfg *config.Config, client llms.Model, prompt string, stream bool, history bool) (string, error) {
	return ChatWithSystemPrompt(cfg, client, "", prompt, stream, history)
}

// ChatWithSystemPrompt orchestrates a chat completion with separate system and user prompts.
// The systemPrompt is added as a system message before the user prompt.
func ChatWithSystemPrompt(cfg *config.Config, client llms.Model, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
	// Add system message to history if provided (only once at the start of conversation)
	if systemPrompt != "" && len(cfg.ChatMessages) == 0 {
		systemMessage := llms.SystemChatMessage{Content: systemPrompt}
		if history {
			cfg.ChatMessages = append(cfg.ChatMessages, systemMessage)
		}
		logger.Log("debug", "[Chat] Added system prompt (%d chars)", len(systemPrompt))
	}

	humanMessage := llms.HumanChatMessage{Content: userPrompt}
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
		messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, userPrompt))
	}

	// Log role distribution for debugging
	LogOutboundMessages(messages, cfg)

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
		var llmTools []llms.Tool
		if cfg.MCPPromptServer != "" {
			// When a prompt is active, filter tools to the same server as the prompt
			llmTools = mcp.DiscoverLangchainToolsForServer(cfg, cfg.MCPPromptServer)
			logger.Log("info", "Filtering MCP tools for prompt server '%s'", cfg.MCPPromptServer)
		} else {
			llmTools = mcp.DiscoverLangchainTools(cfg)
		}
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

	outgoingTokens := lib.CountTokensWithConfig(cfg, userPrompt, cfg.ChatMessages)
	cfg.LastOutgoingTokens = outgoingTokens
	cfg.LastIncomingTokens = 0

	var tokenMeter *lib.TokenMeter
	// Strict MCP prints final output non-streaming; skip live meter to avoid extra counter line.
	if !(cfg.MCPClientEnabled && cfg.MCPStrict) {
		tokenMeter = lib.NewTokenMeter(cfg, outgoingTokens)
	}

	spinnerManager := lib.GetSpinnerManager(cfg)
	message := fmt.Sprintf("Waiting for %s/%s... %s %s"+config.Colors.Dim.Sprint(" (ESC to cancel)"),
		config.Colors.Provider.Sprint(cfg.Provider), config.Colors.Model.Sprint(cfg.Model), config.Colors.Output.Sprint("[")+config.Colors.Label.Sprint("↑"+lib.FormatCompactNumber(outgoingTokens)), config.Colors.Output.Sprint("tokens]"))
	cancelSpinner := spinnerManager.ShowLLM(message)

	var stopOnce sync.Once

	var callbackFn func(ctx context.Context, chunk []byte) error
	var cleanupFn func()
	var contentAlreadyDisplayed bool
	var displayedContent string

	// For MCP-enabled configurations, we need to handle tool calls synchronously
	// So we disable streaming initially and re-enable it for final responses
	useStreaming := stream && !cfg.MCPClientEnabled

	// Track tool calls for session history
	var sessionToolCalls []config.ToolCallData

	maxRetries := cfg.Retries
	var responseContent string
	var bufferedToolBlocks []string

	// onCtrlR is called when the user presses Ctrl-R during a blocking operation.
	// It clears the screen and redraws the full session history + current partial state.
	onCtrlR := func() {
		// 1. Hide spinner temporarily
		wasActive := spinnerManager.IsActive()
		var currentSpinnerMsg string
		var currentSpinnerType lib.SpinnerType
		if wasActive {
			if ctx := spinnerManager.GetContext(); ctx != nil {
				currentSpinnerMsg = ctx.Message
				currentSpinnerType = ctx.Type
			}
			spinnerManager.Hide()
		}

		// 2. Clear screen with cool effect
		lib.CoolClearEffect(cfg)

		// 3. Print full history (verbose)
		if len(cfg.SessionHistory) > 0 {
			fmt.Println(config.Colors.Accent.Sprint("Session History:"))
			for _, event := range cfg.SessionHistory {
				fmt.Print(mcp.RenderSessionEvent(event, true, cfg))
				fmt.Println(config.Colors.Dim.Sprint(strings.Repeat("-", 40)))
			}
		}

		// 4. Print current partial turn
		// We construct a temporary event representing the current state
		currentEvent := config.SessionEvent{
			Timestamp:  time.Now(),
			UserPrompt: userPrompt,
			ToolCalls:  sessionToolCalls,
			// AIResponse is not yet available/complete, but we can show what we have
			AIResponse: responseContent,
		}

		// If we have content displayed already, use that
		if contentAlreadyDisplayed {
			currentEvent.AIResponse = displayedContent
		}

		fmt.Println(config.Colors.Accent.Sprint("Current Interaction:"))
		fmt.Print(mcp.RenderSessionEvent(currentEvent, true, cfg))

		// 5. Restore spinner
		if wasActive {
			// Add a small delay/newline to separate history from spinner?
			// RenderSessionEvent ends with newline usually.
			spinnerManager.Show(currentSpinnerType, currentSpinnerMsg)
		}
	}

	// startEscBreaker starts a raw-input watcher to cancel the context on standalone ESC.
	startEscBreaker := func(cancel func()) func() {
		return lib.StartEscWatcher(cancel, spinnerManager, cfg, onCtrlR)
	}

	if useStreaming {
		onFirstChunk := func() {
			stopOnce.Do(func() {
				spinnerManager.Hide()
				fmt.Fprint(os.Stdout, "\n")
			})
		}

		callbackFn, cleanupFn = CreateStreamingCallback(cfg, spinnerManager, nil, onFirstChunk)
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

	// sessionToolCalls declared above

	resp, responseContent, err := generateWithRetries(cfg, spinnerManager, client, messages, generateOptions, message, outgoingTokens, maxRetries, startEscBreaker)
	if err != nil {
		return "", err
	}

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
				// Hide spinner before displaying content - can't use stopOnce because
				// spinner is re-shown for each LLM call in the MCP tool loop
				hideSpinnerWithLeadingNewline(spinnerManager)
				if !cfg.SuppressContentPrint {
					printContentFormatted(cfg, choice.Content, false)
				}
				contentAlreadyDisplayed = true
				displayedContent = choice.Content
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
					customMessage := fmt.Sprintf("🔧 %s %s %s...", config.Colors.Info.Sprint("Processing"), config.Colors.Dim.Sprint("MCP tool call:"), config.Colors.Accent.Sprint(fmt.Sprintf("%d of %d", toolCallCount+1, maxToolCalls)))
					if err := applyThrottleDelayWithCustomMessageManager(cfg, spinnerManager, customMessage); err != nil {
						if lib.IsUserCancel(err) {
							return "", lib.NewUserCancelError("canceled by user")
						}
						return "", err
					}

					logger.Log("info", "Executing MCP tool: %s with args: %v", tc.FunctionCall.Name, args)
					toolResult, callErr := mcp.ExecuteTool(cfg, tc.FunctionCall.Name, args)
					if callErr != nil {
						logger.Log("warn", "MCP tool %s failed: %v", tc.FunctionCall.Name, callErr)
						toolResult = fmt.Sprintf("Error executing tool '%s': %v", tc.FunctionCall.Name, callErr)
					} else {
						logger.Log("info", "MCP tool %s executed successfully, result length: %d", tc.FunctionCall.Name, len(toolResult))
					}

					// Record tool call for history
					sessionToolCalls = append(sessionToolCalls, config.ToolCallData{
						Name:   tc.FunctionCall.Name,
						Args:   args,
						Result: toolResult,
					})

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
						// Hide spinner before printing tool block to avoid visual overlap
						// Note: Can't use stopOnce here because spinner is re-shown for each LLM call
						hideSpinnerWithLeadingNewline(spinnerManager)
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
				if err := applyThrottleDelayWithSpinnerManager(cfg, spinnerManager); err != nil {
					if lib.IsUserCancel(err) {
						return "", lib.NewUserCancelError("canceled by user")
					}
					return "", err
				}

				var err error
				resp, responseContent, err = generateWithRetries(cfg, spinnerManager, client, messages, generateOptions, message, outgoingTokens, maxRetries, startEscBreaker)
				if err != nil {
					return "", err
				}
				// Continue loop to check if model requests more tools
				continue
			}
			break
		}
	}

	if history && responseContent != "" {
		cfg.ChatMessages = append(cfg.ChatMessages, llms.AIChatMessage{Content: responseContent})
	}

	if responseContent == "" {
		return "", fmt.Errorf("no content generated from %s", cfg.Provider)
	}

	if !useStreaming {
		if tokenMeter != nil {
			tokenMeter.AddIncoming(lib.EstimateTokens(cfg, responseContent))
		}
		spinnerManager.Hide()

		if cfg.MCPClientEnabled {
			printToolBlocks(bufferedToolBlocks)
			if responseContent != "" && responseContent != displayedContent {
				fmt.Fprint(os.Stdout, "\n")
				if !cfg.SuppressContentPrint {
					printContentFormatted(cfg, responseContent, true)
				}
			} else {
				fmt.Fprintln(os.Stdout)
			}
		} else {
			if !contentAlreadyDisplayed {
				fmt.Fprint(os.Stdout, "\n")
				printToolBlocks(bufferedToolBlocks)
				if !cfg.SuppressContentPrint {
					printContentFormatted(cfg, responseContent, true)
				}
			} else {
				printToolBlocks(bufferedToolBlocks)
				fmt.Fprintln(os.Stdout)
			}
		}
	}

	// Record session event
	if !history {
		// Only record if history is disabled (meaning this is a direct interaction, not a recursive one?)
		// Wait, history param usually means "add to LLM context history".
		// We want to record the SESSION interacton regardless of LLM context strategy.
		// Actually, standard chat uses history=true.
	}

	// Always record the session event
	cfg.SessionHistory = append(cfg.SessionHistory, config.SessionEvent{
		Timestamp:  time.Now(),
		UserPrompt: userPrompt,
		ToolCalls:  sessionToolCalls,
		AIResponse: responseContent,
	})

	return responseContent, nil
}

// printContentFormatted prints content using markdown streaming writer unless disabled.
func printContentFormatted(cfg *config.Config, content string, trailingBlankLine bool) {
	if cfg.DisableMarkdownFormat && !cfg.SuppressContentPrint {
		fmt.Fprintln(os.Stdout, content)
		if trailingBlankLine {
			fmt.Fprintln(os.Stdout)
		}
		return
	}
	w := formatter.NewStreamingWriter(os.Stdout)
	_, _ = w.Write([]byte(content))
	_ = w.Flush()
	_ = w.Close()
	if trailingBlankLine {
		fmt.Fprintln(os.Stdout)
	}
}

// printToolBlocks prints any buffered MCP tool blocks.
func printToolBlocks(blocks []string) {
	if len(blocks) == 0 {
		return
	}
	for _, b := range blocks {
		fmt.Fprint(os.Stdout, b)
	}
}

// hideSpinnerWithLeadingNewline hides spinner and adds a leading newline.
func hideSpinnerWithLeadingNewline(spinnerManager *lib.SpinnerManager) {
	spinnerManager.Hide()
	fmt.Fprint(os.Stdout, "\n")
}

// applyRetryDelayWithCountdown applies a retry delay with spinner countdown. It accepts the ESC watcher starter.
func applyRetryDelayWithCountdown(
	spinnerManager *lib.SpinnerManager,
	cfg *config.Config,
	delay time.Duration,
	attempt int,
	maxRetries int,
	outgoingTokens int,
	messageType string,
	startEscBreaker func(cancel func()) func(),
) error {
	// Fast path for tests: when SkipWaits is enabled, skip delays entirely
	if cfg != nil && cfg.SkipWaits {
		return nil
	}

	baseMessage := fmt.Sprintf("%s - retrying %s/%s %s %s",
		config.Colors.Warn.Sprint(messageType), config.Colors.Provider.Sprint(cfg.Provider), config.Colors.Model.Sprint(cfg.Model),
		config.Colors.Dim.Sprint(fmt.Sprintf("(attempt %d/%d)", attempt, maxRetries)), config.Colors.Info.Sprint("[↑"+lib.FormatCompactNumber(outgoingTokens)+" tokens]"))

	// Start spinner with initial message
	cancelRetrySpinner := spinnerManager.Show(lib.SpinnerThrottle, baseMessage)

	// Apply cancellable delay with countdown and Ctrl-C handling
	ctx, cancel := context.WithCancel(context.Background())
	stopEsc := startEscBreaker(cancel)

	// Manual countdown loop with cancellation support
	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer stopEsc()
	defer cancel() // Ensure context is cancelled when function exits
	defer cancelRetrySpinner()

	for {
		select {
		case <-ctx.Done():
			return lib.NewUserCancelError("canceled by user")
		case <-ticker.C:
			elapsed := time.Since(start)
			remaining := delay - elapsed
			if remaining <= 0 {
				// Countdown finished normally
				return nil
			}
			// Update spinner with countdown
			countdownMessage := fmt.Sprintf("%s (%.1fs remaining)", baseMessage, remaining.Seconds())
			spinnerManager.Update(countdownMessage)
		}
	}
}

// generateWithRetries centralizes the retry, backoff, spinner, and throttling logic
// for LLM content generation requests.
func generateWithRetries(
	cfg *config.Config,
	spinnerManager *lib.SpinnerManager,
	client llms.Model,
	messages []llms.MessageContent,
	generateOptions []llms.CallOption,
	spinnerMessage string,
	outgoingTokens int,
	maxRetries int,
	startEscBreaker func(cancel func()) func(),
) (*llms.ContentResponse, string, error) {
	backoffFactor := 3.0
	initialBackoff := 10.0

	var lastError error
	var responseContent string
	var resp *llms.ContentResponse

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			var delay time.Duration
			var messageType string

			if lib.Is429Error(lastError) {
				retryDelay, parseErr := lib.ParseRetryDelay(lastError)
				if parseErr == nil {
					delay = retryDelay
					messageType = "Rate limited"
					logger.Log("info", "Retrying after parsed rate limit delay: %v (attempt %d/%d)", delay, attempt, maxRetries)
				} else {
					delay = lib.CalculateExponentialBackoff(attempt, initialBackoff, backoffFactor)
					messageType = "Rate limited"
					logger.Log("info", "Failed to parse 429 retry delay, using exponential backoff: %v", parseErr)
					logger.Log("info", "Retrying 429 error with backoff in %v (attempt %d/%d)", delay, attempt, maxRetries)
				}
			} else {
				delay = lib.CalculateExponentialBackoff(attempt, initialBackoff, backoffFactor)
				messageType = "Retrying"
				logger.Log("info", "Retrying in %v (attempt %d/%d)", delay, attempt, maxRetries)
			}

			if err := applyRetryDelayWithCountdown(spinnerManager, cfg, delay, attempt, maxRetries, outgoingTokens, messageType, startEscBreaker); err != nil {
				if lib.IsUserCancel(err) {
					return nil, "", lib.NewUserCancelError("canceled by user")
				}
				return nil, "", err
			}
			spinnerManager.ShowLLM(spinnerMessage)
		}

		if err := applyThrottleDelayWithSpinnerManager(cfg, spinnerManager); err != nil {
			if lib.IsUserCancel(err) {
				return nil, "", lib.NewUserCancelError("canceled by user")
			}
			return nil, "", err
		}
		if !spinnerManager.IsActive() {
			spinnerManager.ShowLLM(spinnerMessage)
		}

		if len(messages) == 0 {
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, ""))
		}

		ctx, cancel := context.WithCancel(context.Background())
		stopEsc := startEscBreaker(cancel)
		r, err := client.GenerateContent(ctx, messages, generateOptions...)
		stopEsc()
		if err != nil {
			if ctx.Err() == context.Canceled || lib.IsUserCancel(err) {
				return nil, "", lib.NewUserCancelError("canceled by user")
			}
			lastError = err
			retriesLeft := maxRetries - attempt

			if lib.Is429Error(err) {
				if retriesLeft == 0 {
					lib.Display429Error(err, cfg, maxRetries)
				} else {
					logger.Log("debug", "429 Rate limit error - will retry with delay (%d retries left)", retriesLeft)
					continue
				}
			} else if retriesLeft > 0 {
				errorMsg := lib.GetErrorMessage(err)
				if errorMsg != "" {
					fmt.Printf("%s\n", config.Colors.Error.Sprint(errorMsg))
				} else {
					fmt.Printf("%s\n", config.Colors.Error.Sprint(err.Error()))
				}
				continue
			}

			if attempt == maxRetries {
				errorMsg := lib.GetErrorMessage(err)
				if errorMsg != "" {
					return nil, "", fmt.Errorf("AI still returning error after %d retries (%s): %w", maxRetries, errorMsg, err)
				}
				return nil, "", fmt.Errorf("AI still returning error after %d retries: %w", maxRetries, err)
			}
			errorMsg := lib.GetErrorMessage(err)
			if errorMsg != "" {
				return nil, "", fmt.Errorf("%s: %w", errorMsg, err)
			}
			return nil, "", err
		}

		updateResponseTime()

		resp = r
		if resp != nil && len(resp.Choices) > 0 {
			responseContent = resp.Choices[0].Content
			cfg.LastIncomingTokens = lib.EstimateTokens(cfg, responseContent)
		}

		hasToolCalls := resp != nil && len(resp.Choices) > 0 && len(resp.Choices[0].ToolCalls) > 0
		if responseContent == "" && !hasToolCalls {
			if attempt < maxRetries {
				logger.Log("warn", "Received empty content from %s/%s", cfg.Provider, cfg.Model)
				lastError = fmt.Errorf("no content generated from %s", cfg.Provider)
				continue
			}
		}
		break
	}

	if resp == nil && responseContent == "" {
		if lastError != nil {
			return nil, "", fmt.Errorf("no content generated from %s after retries, last error: %w", cfg.Provider, lastError)
		}
		return nil, "", fmt.Errorf("no content generated from %s", cfg.Provider)
	}

	return resp, responseContent, nil
}
