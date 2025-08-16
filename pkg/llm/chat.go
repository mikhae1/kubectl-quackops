package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
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

	logger.Log("info", "Sending request to %s/%s with %d messages in history", cfg.Provider, cfg.Model, len(messages))

	generateOptions := []llms.CallOption{}

	if cfg.Temperature > 0 {
		generateOptions = append(generateOptions, llms.WithTemperature(cfg.Temperature))
	}

	if cfg.MaxTokens > 0 {
		effective := lib.EffectiveMaxTokens(cfg)
		limit := cfg.MaxTokens
		if effective > 0 && cfg.MaxTokens > effective {
			limit = effective
		}
		generateOptions = append(generateOptions, llms.WithMaxTokens(limit))
	}

	mcpToolsExposed := false
	if cfg.MCPClientEnabled {
		llmTools := mcp.DiscoverLangchainTools(cfg)
		if len(llmTools) > 0 {
			generateOptions = append(generateOptions, llms.WithTools(llmTools))
			generateOptions = append(generateOptions, llms.WithToolChoice("auto"))
			logger.Log("info", "Exposed %d MCP tools to model: %v", len(llmTools), mcp.ExtractToolNames(llmTools))
			mcpToolsExposed = true
		}
	}

	outgoingTokens := lib.CountTokensWithConfig(cfg, prompt, cfg.ChatMessages)
	cfg.LastOutgoingTokens = outgoingTokens
	cfg.LastIncomingTokens = 0

	tokenMeter := lib.NewTokenMeter(cfg, outgoingTokens)

	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Color("green", "bold")
	s.Writer = os.Stderr
	s.Suffix = fmt.Sprintf(" Waiting for %s/%s response...",
		cfg.Provider, cfg.Model)

	var stopOnce sync.Once
	s.Start()

	var callbackFn func(ctx context.Context, chunk []byte) error
	var cleanupFn func()

	originalStream := stream
	useStreaming := stream && !(cfg.MCPClientEnabled && mcpToolsExposed)

	if useStreaming {
		onFirstChunk := func() {
			stopOnce.Do(func() {
				s.Stop()
				fmt.Fprint(os.Stderr, "\r\033[2K")
				fmt.Fprint(os.Stdout, "\n")
			})
		}

		callbackFn, cleanupFn = createStreamingCallback(cfg, s, nil, onFirstChunk)
		defer cleanupFn()

		defer func() {
			stopOnce.Do(func() {
				s.Stop()
				fmt.Fprint(os.Stderr, "\r\033[2K")
			})
		}()

		generateOptions = append(generateOptions, llms.WithStreamingFunc(callbackFn))
	} else {
		defer s.Stop()
	}

	maxRetries := cfg.Retries
	backoffFactor := 3.0
	initialBackoff := 10.0
	originalSuffix := s.Suffix
	var responseContent string
	var bufferedToolBlocks []string
	var lastError error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoffTime := initialBackoff * math.Pow(backoffFactor, float64(attempt-1))
			jitter := (0.5 + rand.Float64())
			sleepTime := time.Duration(backoffTime * jitter * float64(time.Second))
			retrySeconds := backoffTime * jitter

			logger.Log("info", "Retrying in %.2f seconds (attempt %d/%d)", retrySeconds, attempt, maxRetries)

			s.Suffix = fmt.Sprintf(" Retrying %s/%s... (attempt %d/%d)", cfg.Provider, cfg.Model, attempt, maxRetries)

			countdownStart := time.Now()
			for {
				elapsed := time.Since(countdownStart)
				remaining := sleepTime - elapsed
				if remaining <= 0 {
					break
				}

				s.Suffix = fmt.Sprintf(" Retrying %s/%s in %.1fs... (attempt %d/%d)",
					cfg.Provider, cfg.Model, remaining.Seconds(), attempt, maxRetries)
				time.Sleep(100 * time.Millisecond)
			}

			s.Suffix = originalSuffix
		}

		// Apply throttling delay before making the request (including retries)
		applyThrottleDelayWithSpinner(cfg, s)

		if len(messages) == 0 {
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, prompt))
		}

		resp, err := client.GenerateContent(context.Background(), messages, generateOptions...)
		if err != nil {
			lastError = err
			if attempt < maxRetries {
				fmt.Printf("%s\n", color.RedString(err.Error()))
				continue
			}

			if attempt == maxRetries {
				return "", fmt.Errorf("AI still returning error after %d retries: %w", maxRetries, err)
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
				if len(choice.ToolCalls) > 0 {
					if toolCallCount >= maxToolCalls {
						logger.Log("warn", "Maximum MCP tool call limit (%d) reached, stopping tool execution", maxToolCalls)
						break
					}

					logger.Log("info", "Processing MCP tool call: iteration %d of %d...", toolCallCount+1, maxToolCalls)

					for _, tc := range choice.ToolCalls {
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
						applyThrottleDelayWithCustomMessage(cfg, s, customMessage)

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
						} else if originalStream {
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
					applyThrottleDelayWithSpinner(cfg, s)

					resp, err = client.GenerateContent(context.Background(), messages, generateOptions...)
					if err != nil {
						lastError = err
						if attempt < maxRetries {
							fmt.Printf("%s\n", color.RedString(err.Error()))
							continue
						}
						if attempt == maxRetries {
							return "", fmt.Errorf("AI still returning error after %d retries post-tool: %w", maxRetries, err)
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
		if originalStream {
			s.Stop()
			fmt.Fprint(os.Stderr, "\r\033[2K")
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
		}
	}

	return responseContent, nil
}
