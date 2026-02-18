package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

var executeMCPTool = mcp.ExecuteTool

type mcpPreparedCall struct {
	ToolCall   llms.ToolCall
	Args       map[string]any
	Signature  string
	Iteration  int
	MaxRounds  int
	CacheAllow bool
}

type mcpExecutedCall struct {
	Prepared    mcpPreparedCall
	ToolResult  string
	CallErr     error
	CacheHit    bool
	ArtifactRef string
	ArtifactSHA string
}

type mcpArtifactRef struct {
	Path string
	SHA  string
}

// Chat orchestrates a chat completion with the provided llms.Model, handling
// history, streaming, retries, token accounting, and MCP tool calls.
func Chat(cfg *config.Config, client llms.Model, prompt string, stream bool, history bool) (string, error) {
	return ChatWithSystemPrompt(cfg, client, "", prompt, stream, history)
}

// ChatWithSystemPrompt orchestrates a chat completion with separate system and user prompts.
// The systemPrompt is added as a system message before the user prompt.
func ChatWithSystemPrompt(cfg *config.Config, client llms.Model, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
	if cfg == nil {
		cfg = config.LoadConfig()
		if cfg == nil {
			return "", fmt.Errorf("nil config")
		}
	}
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
	provider := cfg.Provider
	model := cfg.Model
	message := fmt.Sprintf("Waiting for %s/%s... %s %s"+config.Colors.Dim.Sprint(" (ESC to cancel)"),
		config.Colors.Provider.Sprint(provider), config.Colors.Model.Sprint(model), config.Colors.Output.Sprint("[")+config.Colors.Label.Sprint("↑"+lib.FormatCompactNumber(outgoingTokens)), config.Colors.Output.Sprint("tokens]"))
	if strings.TrimSpace(cfg.SpinnerMessageOverride) != "" {
		message = strings.TrimSpace(cfg.SpinnerMessageOverride)
	}
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
		onCtrlT := func() {
			if spinnerManager != nil {
				spinnerManager.ToggleDetailsHidden()
			}
		}
		return lib.StartEscWatcher(cancel, spinnerManager, cfg, onCtrlR, onCtrlT)
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
	resetMCPLoopMetrics(cfg)

	if cfg.MCPClientEnabled {
		toolCallCount := 0
		maxToolCalls := cfg.MCPMaxToolCalls
		maxToolCallsTotal := cfg.MCPMaxToolCallsTotal
		maxToolResultBudgetBytes := cfg.MCPToolResultBudgetBytes
		stallThreshold := cfg.MCPStallThreshold
		totalToolCalls := 0
		totalToolResultBytes := 0
		lastToolPlanFingerprint := ""
		repeatedToolPlanCount := 0
		planFingerprintHistory := make([]string, 0, 16)
		toolCallSignatureCounts := make(map[string]int)
		uniqueToolSignatures := make(map[string]struct{})
		toolResultCache := make(map[string]string)
		toolArtifactCache := make(map[string]mcpArtifactRef)
		seenEvidenceHashes := make(map[string]struct{})
		cacheHitCount := 0
		repeatedToolCalls := 0
		noProgressRounds := 0
		finalStopReason := ""
		for {
			if resp == nil || len(resp.Choices) == 0 {
				break
			}
			choice := resp.Choices[0]

			// Display content if available and not already displayed
			if choice.Content != "" && !contentAlreadyDisplayed {
				// Pause spinner while printing, then restore so progress stays visible at bottom.
				printWithSpinnerPaused(spinnerManager, func() {
					if !cfg.SuppressContentPrint {
						printContentFormatted(cfg, choice.Content, false)
					}
				})
				contentAlreadyDisplayed = true
				displayedContent = choice.Content
			}

			if len(choice.ToolCalls) > 0 {
				stopReason := ""
				roundNewEvidence := 0

				if maxToolCalls > 0 && toolCallCount >= maxToolCalls {
					stopReason = fmt.Sprintf("maximum MCP tool-call iteration limit reached (%d)", maxToolCalls)
				}

				if stopReason == "" {
					planFingerprint := toolCallPlanFingerprint(choice.ToolCalls)
					if planFingerprint != "" {
						if stallThreshold > 0 {
							if planFingerprint == lastToolPlanFingerprint {
								repeatedToolPlanCount++
							} else {
								lastToolPlanFingerprint = planFingerprint
								repeatedToolPlanCount = 0
							}
							if repeatedToolPlanCount >= stallThreshold {
								stopReason = fmt.Sprintf("MCP tool loop stalled: identical tool plan repeated %d times", repeatedToolPlanCount+1)
							}
						}
						if stopReason == "" && cfg.MCPLoopCycleThreshold > 0 {
							if cycleDistance, ok := detectToolPlanCycle(planFingerprintHistory, planFingerprint, cfg.MCPLoopCycleThreshold); ok {
								stopReason = fmt.Sprintf("MCP tool loop cycling: plan repeated after %d round(s)", cycleDistance)
							}
						}
						planFingerprintHistory = append(planFingerprintHistory, planFingerprint)
						if len(planFingerprintHistory) > 16 {
							planFingerprintHistory = planFingerprintHistory[len(planFingerprintHistory)-16:]
						}
					}
				}

				if stopReason == "" && maxToolCallsTotal > 0 {
					remainingCalls := maxToolCallsTotal - totalToolCalls
					if remainingCalls <= 0 {
						stopReason = fmt.Sprintf("MCP total tool-call budget exhausted (%d)", maxToolCallsTotal)
					} else if len(choice.ToolCalls) > remainingCalls {
						logger.Log("warn", "MCP total tool-call budget allows only %d more call(s); truncating current tool-call batch", remainingCalls)
						choice.ToolCalls = choice.ToolCalls[:remainingCalls]
					}
				}

				if stopReason != "" {
					logger.Log("warn", "%s", stopReason)
					finalStopReason = stopReason
					var ferr error
					resp, responseContent, ferr = finalizeAfterToolLoopStop(cfg, spinnerManager, client, messages, generateOptions, message, outgoingTokens, maxRetries, startEscBreaker, stopReason)
					if ferr != nil {
						return "", ferr
					}
					if strings.TrimSpace(responseContent) == "" {
						responseContent = fmt.Sprintf("Stopped MCP tool execution: %s", stopReason)
					}
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

				preparedCalls := make([]mcpPreparedCall, 0, len(choice.ToolCalls))
				for _, tc := range choice.ToolCalls {
					if maxToolCallsTotal > 0 && totalToolCalls+len(preparedCalls) >= maxToolCallsTotal {
						stopReason = fmt.Sprintf("MCP total tool-call budget exhausted (%d)", maxToolCallsTotal)
						logger.Log("warn", "%s", stopReason)
						break
					}
					if maxToolResultBudgetBytes > 0 && totalToolResultBytes >= maxToolResultBudgetBytes {
						stopReason = fmt.Sprintf("MCP tool-result budget exhausted (%d bytes)", maxToolResultBudgetBytes)
						logger.Log("warn", "%s", stopReason)
						break
					}

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

					signature := toolCallSignature(tc.FunctionCall.Name, args)
					if signature != "" {
						if _, seen := uniqueToolSignatures[signature]; seen {
							repeatedToolCalls++
						} else {
							uniqueToolSignatures[signature] = struct{}{}
						}
						if cfg.MCPToolRepeatLimit > 0 && toolCallSignatureCounts[signature] >= cfg.MCPToolRepeatLimit {
							stopReason = fmt.Sprintf("MCP tool repeat limit reached for %s (%d)", tc.FunctionCall.Name, cfg.MCPToolRepeatLimit)
							logger.Log("warn", "%s", stopReason)
							break
						}
						toolCallSignatureCounts[signature]++
					}

					emitPlanProgress(PlanProgressEvent{
						Kind:          PlanProgressToolStarted,
						ToolName:      tc.FunctionCall.Name,
						Iteration:     toolCallCount + 1,
						MaxIterations: maxToolCalls,
					})

					preparedCalls = append(preparedCalls, mcpPreparedCall{
						ToolCall:   tc,
						Args:       args,
						Signature:  signature,
						Iteration:  toolCallCount + 1,
						MaxRounds:  maxToolCalls,
						CacheAllow: cfg.MCPCacheToolResults,
					})
				}

				executedCalls, batchErr := executePreparedMCPCalls(cfg, spinnerManager, preparedCalls, toolResultCache)
				if batchErr != nil {
					if lib.IsUserCancel(batchErr) {
						return "", lib.NewUserCancelError("canceled by user")
					}
					return "", batchErr
				}

				for _, executed := range executedCalls {
					tc := executed.Prepared.ToolCall
					args := executed.Prepared.Args
					toolResult := executed.ToolResult
					callErr := executed.CallErr
					signature := executed.Prepared.Signature
					if executed.CacheHit {
						cacheHitCount++
					}

					if callErr != nil {
						emitPlanProgress(PlanProgressEvent{
							Kind:          PlanProgressToolFailed,
							ToolName:      tc.FunctionCall.Name,
							Iteration:     executed.Prepared.Iteration,
							MaxIterations: executed.Prepared.MaxRounds,
							Err:           callErr.Error(),
						})
					} else {
						emitPlanProgress(PlanProgressEvent{
							Kind:          PlanProgressToolCompleted,
							ToolName:      tc.FunctionCall.Name,
							Iteration:     executed.Prepared.Iteration,
							MaxIterations: executed.Prepared.MaxRounds,
						})
					}

					artifactPath := executed.ArtifactRef
					artifactSHA := executed.ArtifactSHA
					if signature != "" {
						if cachedArtifact, ok := toolArtifactCache[signature]; ok && cachedArtifact.Path != "" {
							artifactPath = cachedArtifact.Path
							artifactSHA = cachedArtifact.SHA
						}
					}
					if artifactPath == "" {
						if persistedPath, persistedSHA, persistErr := persistToolResultArtifact(cfg, tc.FunctionCall.Name, args, toolResult); persistErr != nil {
							logger.Log("warn", "Failed to persist MCP tool output artifact for %s: %v", tc.FunctionCall.Name, persistErr)
						} else {
							artifactPath = persistedPath
							artifactSHA = persistedSHA
							if signature != "" && artifactPath != "" {
								toolArtifactCache[signature] = mcpArtifactRef{Path: artifactPath, SHA: artifactSHA}
							}
						}
					}
					displayToolResult := appendArtifactReference(toolResult, artifactPath, artifactSHA)

					// Record tool call for history
					sessionToolCalls = append(sessionToolCalls, config.ToolCallData{
						Name:           tc.FunctionCall.Name,
						Args:           args,
						Result:         displayToolResult,
						ResultBytes:    len(toolResult),
						ArtifactPath:   artifactPath,
						ArtifactSHA256: artifactSHA,
					})

					totalToolCalls++
					totalToolResultBytes += len(toolResult)
					if stopReason == "" && maxToolResultBudgetBytes > 0 && totalToolResultBytes >= maxToolResultBudgetBytes {
						stopReason = fmt.Sprintf("MCP tool-result budget reached (%d/%d bytes)", totalToolResultBytes, maxToolResultBudgetBytes)
						logger.Log("warn", "%s", stopReason)
					}

					evidenceHash := hashToolEvidence(tc.FunctionCall.Name, args, toolResult)
					if _, seen := seenEvidenceHashes[evidenceHash]; !seen {
						seenEvidenceHashes[evidenceHash] = struct{}{}
						roundNewEvidence++
					}

					if stopReason != "" {
						continue
					}

					modelToolResult := compactToolResultForModel(cfg, tc.FunctionCall.Name, toolResult, artifactPath, artifactSHA)

					// Update response timestamp for throttling calculations after MCP tool execution
					updateResponseTime()

					var block string
					if cfg.Verbose {
						block = mcp.FormatToolCallVerbose(tc.FunctionCall.Name, args, displayToolResult)
					} else {
						block = mcp.FormatToolCallBlock(tc.FunctionCall.Name, args, displayToolResult)
					}

					if useStreaming {
						bufferedToolBlocks = append(bufferedToolBlocks, block)
					} else {
						if shouldPrintToolBlocks(cfg, spinnerManager) {
							printWithSpinnerPaused(spinnerManager, func() {
								fmt.Fprint(os.Stdout, block)
							})
						}
					}

					toolMsg := llms.MessageContent{
						Role: llms.ChatMessageTypeTool,
						Parts: []llms.ContentPart{llms.ToolCallResponse{
							ToolCallID: tc.ID,
							Name:       tc.FunctionCall.Name,
							Content:    modelToolResult,
						}},
					}
					messages = append(messages, toolMsg)
				}

				toolCallCount++

				if stopReason == "" && cfg.MCPNoProgressThreshold > 0 {
					if roundNewEvidence == 0 {
						noProgressRounds++
						if noProgressRounds >= cfg.MCPNoProgressThreshold {
							stopReason = fmt.Sprintf("MCP tool loop made no progress for %d round(s)", noProgressRounds)
						}
					} else {
						noProgressRounds = 0
					}
				}

				if stopReason != "" {
					finalStopReason = stopReason
					var ferr error
					resp, responseContent, ferr = finalizeAfterToolLoopStop(cfg, spinnerManager, client, messages, generateOptions, message, outgoingTokens, maxRetries, startEscBreaker, stopReason)
					if ferr != nil {
						return "", ferr
					}
					if strings.TrimSpace(responseContent) == "" {
						responseContent = fmt.Sprintf("Stopped MCP tool execution: %s", stopReason)
					}
					break
				}

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
		cfg.LastMCPToolCallsTotal = totalToolCalls
		cfg.LastMCPUniqueToolCalls = len(uniqueToolSignatures)
		cfg.LastMCPRepeatedToolCalls = repeatedToolCalls
		cfg.LastMCPCacheHits = cacheHitCount
		cfg.LastMCPStopReason = finalStopReason
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
			if shouldPrintToolBlocks(cfg, spinnerManager) {
				printToolBlocks(bufferedToolBlocks)
			}
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
				if shouldPrintToolBlocks(cfg, spinnerManager) {
					printToolBlocks(bufferedToolBlocks)
				}
				if !cfg.SuppressContentPrint {
					printContentFormatted(cfg, responseContent, true)
				}
			} else {
				if shouldPrintToolBlocks(cfg, spinnerManager) {
					printToolBlocks(bufferedToolBlocks)
				}
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

func finalizeAfterToolLoopStop(
	cfg *config.Config,
	spinnerManager *lib.SpinnerManager,
	client llms.Model,
	messages []llms.MessageContent,
	generateOptions []llms.CallOption,
	spinnerMessage string,
	outgoingTokens int,
	maxRetries int,
	startEscBreaker func(cancel func()) func(),
	stopReason string,
) (*llms.ContentResponse, string, error) {
	note := "Tool execution has been stopped due to policy/budget limits. " +
		"Use available evidence to provide the best final answer. Do not request additional tool calls. " +
		"Stop reason: " + strings.TrimSpace(stopReason)
	finalMessages := append(messages, llms.TextParts(llms.ChatMessageTypeSystem, note))
	return generateWithRetries(cfg, spinnerManager, client, finalMessages, generateOptions, spinnerMessage, outgoingTokens, maxRetries, startEscBreaker)
}

func executePreparedMCPCalls(
	cfg *config.Config,
	spinnerManager *lib.SpinnerManager,
	prepared []mcpPreparedCall,
	toolResultCache map[string]string,
) ([]mcpExecutedCall, error) {
	if len(prepared) == 0 {
		return nil, nil
	}
	results := make([]mcpExecutedCall, len(prepared))
	signatureLeader := make(map[string]int, len(prepared))
	duplicateOf := make(map[int]int)
	uniqueIndices := make([]int, 0, len(prepared))

	for idx, item := range prepared {
		if item.CacheAllow && item.Signature != "" {
			if leader, ok := signatureLeader[item.Signature]; ok {
				duplicateOf[idx] = leader
				continue
			}
			signatureLeader[item.Signature] = idx
		}
		uniqueIndices = append(uniqueIndices, idx)
	}

	uniquePrepared := make([]mcpPreparedCall, 0, len(uniqueIndices))
	for _, idx := range uniqueIndices {
		uniquePrepared = append(uniquePrepared, prepared[idx])
	}
	uniqueResults, err := executePreparedMCPCallsUnique(cfg, spinnerManager, uniquePrepared, toolResultCache)
	if err != nil {
		return nil, err
	}
	for idx, originalIdx := range uniqueIndices {
		results[originalIdx] = uniqueResults[idx]
	}
	for duplicateIdx, leaderIdx := range duplicateOf {
		leaderResult := results[leaderIdx]
		dedupedResult := leaderResult
		dedupedResult.Prepared = prepared[duplicateIdx]
		dedupedResult.CacheHit = true
		results[duplicateIdx] = dedupedResult
	}
	if len(duplicateOf) > 0 {
		logger.Log("debug", "MCP round deduplicated %d repeated tool call(s)", len(duplicateOf))
	}
	return results, nil
}

func executePreparedMCPCallsUnique(
	cfg *config.Config,
	spinnerManager *lib.SpinnerManager,
	prepared []mcpPreparedCall,
	toolResultCache map[string]string,
) ([]mcpExecutedCall, error) {
	if len(prepared) == 0 {
		return nil, nil
	}
	parallel := cfg.MCPParallelToolCalls
	if parallel <= 0 {
		parallel = 1
	}
	if parallel > len(prepared) {
		parallel = len(prepared)
	}
	results := make([]mcpExecutedCall, len(prepared))
	var cacheMu sync.RWMutex

	if parallel == 1 {
		for idx, item := range prepared {
			executed, err := executePreparedMCPCall(cfg, spinnerManager, item, toolResultCache, &cacheMu, true)
			if err != nil {
				return nil, err
			}
			results[idx] = executed
		}
		return results, nil
	}

	customMessage := fmt.Sprintf(
		"🔧 %s %s %s...",
		config.Colors.Info.Sprint("Processing"),
		config.Colors.Dim.Sprint("MCP tool batch:"),
		config.Colors.Accent.Sprint(fmt.Sprintf("%d calls", len(prepared))),
	)
	if err := applyThrottleDelayWithCustomMessageManager(cfg, spinnerManager, customMessage); err != nil {
		return nil, err
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < parallel; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				item := prepared[idx]
				executed, err := executePreparedMCPCall(cfg, spinnerManager, item, toolResultCache, &cacheMu, false)
				if err != nil {
					executed = mcpExecutedCall{
						Prepared:   item,
						ToolResult: fmt.Sprintf("Error executing tool '%s': %v", item.ToolCall.FunctionCall.Name, err),
						CallErr:    err,
					}
				}
				results[idx] = executed
			}
		}()
	}
	for idx := range prepared {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
	return results, nil
}

func executePreparedMCPCall(
	cfg *config.Config,
	spinnerManager *lib.SpinnerManager,
	prepared mcpPreparedCall,
	toolResultCache map[string]string,
	cacheMu *sync.RWMutex,
	withThrottle bool,
) (mcpExecutedCall, error) {
	if prepared.CacheAllow && prepared.Signature != "" {
		cacheMu.RLock()
		cached, ok := toolResultCache[prepared.Signature]
		cacheMu.RUnlock()
		if ok {
			logger.Log("debug", "MCP tool cache hit: %s", prepared.ToolCall.FunctionCall.Name)
			return mcpExecutedCall{
				Prepared:   prepared,
				ToolResult: cached,
				CacheHit:   true,
			}, nil
		}
	}

	if withThrottle {
		customMessage := fmt.Sprintf(
			"🔧 %s %s %s...",
			config.Colors.Info.Sprint("Processing"),
			config.Colors.Dim.Sprint("MCP tool call:"),
			config.Colors.Accent.Sprint(fmt.Sprintf("%d of %d", prepared.Iteration, prepared.MaxRounds)),
		)
		if err := applyThrottleDelayWithCustomMessageManager(cfg, spinnerManager, customMessage); err != nil {
			return mcpExecutedCall{}, err
		}
	}

	logger.Log("info", "Executing MCP tool: %s with args: %v", prepared.ToolCall.FunctionCall.Name, prepared.Args)
	toolResult, callErr := executeMCPTool(cfg, prepared.ToolCall.FunctionCall.Name, prepared.Args)
	if callErr != nil {
		logger.Log("warn", "MCP tool %s failed: %v", prepared.ToolCall.FunctionCall.Name, callErr)
		toolResult = fmt.Sprintf("Error executing tool '%s': %v", prepared.ToolCall.FunctionCall.Name, callErr)
	}
	if callErr == nil && prepared.CacheAllow && prepared.Signature != "" {
		cacheMu.Lock()
		toolResultCache[prepared.Signature] = toolResult
		cacheMu.Unlock()
	}

	return mcpExecutedCall{
		Prepared:   prepared,
		ToolResult: toolResult,
		CallErr:    callErr,
	}, nil
}

func toolCallPlanFingerprint(toolCalls []llms.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	var b strings.Builder
	for _, tc := range toolCalls {
		if tc.FunctionCall == nil {
			continue
		}
		b.WriteString(strings.TrimSpace(tc.FunctionCall.Name))
		b.WriteString("|")
		args := strings.Join(strings.Fields(tc.FunctionCall.Arguments), "")
		b.WriteString(args)
		b.WriteString(";")
	}
	return b.String()
}

func resetMCPLoopMetrics(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.LastMCPToolCallsTotal = 0
	cfg.LastMCPUniqueToolCalls = 0
	cfg.LastMCPRepeatedToolCalls = 0
	cfg.LastMCPCacheHits = 0
	cfg.LastMCPStopReason = ""
}

func detectToolPlanCycle(history []string, current string, maxDistance int) (int, bool) {
	if current == "" || maxDistance <= 1 || len(history) == 0 {
		return 0, false
	}
	if maxDistance > len(history) {
		maxDistance = len(history)
	}
	for distance := 2; distance <= maxDistance; distance++ {
		if history[len(history)-distance] == current {
			return distance, true
		}
	}
	return 0, false
}

func toolCallSignature(toolName string, args map[string]any) string {
	trimmedName := strings.TrimSpace(toolName)
	if trimmedName == "" {
		return ""
	}
	if args == nil {
		return trimmedName + "|{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return trimmedName + "|" + fmt.Sprintf("%v", args)
	}
	return trimmedName + "|" + string(b)
}

func hashToolEvidence(toolName string, args map[string]any, result string) string {
	material := toolCallSignature(toolName, args) + "\n" + strings.TrimSpace(result)
	sum := sha256.Sum256([]byte(material))
	return fmt.Sprintf("%x", sum[:])
}

func persistToolResultArtifact(cfg *config.Config, toolName string, args map[string]any, result string) (string, string, error) {
	if cfg == nil {
		return "", "", nil
	}
	maxChars := cfg.MCPToolResultMaxCharsForModel
	trimmed := strings.TrimSpace(result)
	if maxChars <= 0 || trimmed == "" || len(trimmed) <= maxChars {
		return "", "", nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", "", nil
	}

	dir := filepath.Join(home, ".quackops", "tool-output")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}

	now := time.Now().UTC()
	signature := toolCallSignature(toolName, args)
	sum := sha256.Sum256([]byte(signature + "\n" + trimmed))
	digest := hex.EncodeToString(sum[:])
	filename := fmt.Sprintf("%s-%s-%s.log", sanitizeForFilename(toolName), now.Format("20060102T150405.000000000Z"), digest[:12])
	fullpath := filepath.Join(dir, filename)

	var payload strings.Builder
	payload.WriteString(fmt.Sprintf("# tool=%s\n", strings.TrimSpace(toolName)))
	payload.WriteString(fmt.Sprintf("# timestamp=%s\n", now.Format(time.RFC3339Nano)))
	payload.WriteString(fmt.Sprintf("# signature=%s\n", signature))
	payload.WriteString(fmt.Sprintf("# sha256=%s\n\n", digest))
	payload.WriteString(result)
	if !strings.HasSuffix(result, "\n") {
		payload.WriteString("\n")
	}

	if err := os.WriteFile(fullpath, []byte(payload.String()), 0o644); err != nil {
		return "", "", err
	}
	return fullpath, digest, nil
}

func appendArtifactReference(result string, artifactPath string, artifactSHA string) string {
	if strings.TrimSpace(artifactPath) == "" {
		return result
	}
	note := fmt.Sprintf("[full tool output saved: %s]", artifactPath)
	if strings.TrimSpace(artifactSHA) != "" {
		note = fmt.Sprintf("[full tool output saved: %s sha256=%s]", artifactPath, artifactSHA)
	}
	if strings.Contains(result, note) {
		return result
	}
	base := strings.TrimRight(result, "\n")
	if strings.TrimSpace(base) == "" {
		return note
	}
	return base + "\n\n" + note
}

func sanitizeForFilename(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(strings.ToLower(name)) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "tool"
	}
	return out
}

func compactToolResultForModel(cfg *config.Config, toolName string, result string, artifactPath string, artifactSHA string) string {
	if cfg == nil {
		return result
	}
	maxChars := cfg.MCPToolResultMaxCharsForModel
	if maxChars <= 0 {
		return result
	}
	trimmed := strings.TrimSpace(result)
	if trimmed == "" || len(trimmed) <= maxChars {
		return result
	}

	maxLines := 12
	if cfg.ToolOutputMaxLines > 0 && cfg.ToolOutputMaxLines < maxLines {
		maxLines = cfg.ToolOutputMaxLines
	}
	if maxLines < 4 {
		maxLines = 4
	}

	maxCols := 160
	if cfg.ToolOutputMaxLineLen > 0 {
		maxCols = cfg.ToolOutputMaxLineLen
	}
	if maxCols < 60 {
		maxCols = 60
	}
	if maxCols > 200 {
		maxCols = 200
	}

	lines := strings.Split(trimmed, "\n")
	sum := sha256.Sum256([]byte(trimmed))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[tool output truncated for model: tool=%s bytes=%d lines=%d sha256=%x]\n", strings.TrimSpace(toolName), len(trimmed), len(lines), sum[:6]))
	if strings.TrimSpace(artifactPath) != "" {
		if strings.TrimSpace(artifactSHA) != "" {
			b.WriteString(fmt.Sprintf("[full tool output saved: %s sha256=%s]\n", artifactPath, artifactSHA))
		} else {
			b.WriteString(fmt.Sprintf("[full tool output saved: %s]\n", artifactPath))
		}
	}
	for i, line := range lines {
		if i >= maxLines {
			b.WriteString("...\n")
			break
		}
		line = strings.TrimRight(line, "\r")
		if len(line) > maxCols {
			line = line[:maxCols] + "..."
		}
		b.WriteString(line)
		b.WriteString("\n")
		if b.Len() >= maxChars {
			break
		}
	}

	out := strings.TrimSpace(b.String())
	if len(out) <= maxChars {
		return out
	}
	if maxChars <= 3 {
		return out[:maxChars]
	}
	return out[:maxChars-3] + "..."
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

func shouldPrintToolBlocks(cfg *config.Config, spinnerManager *lib.SpinnerManager) bool {
	if cfg.SuppressToolPrint {
		return false
	}
	if cfg.HideToolBlocksWhenDetailsHidden && spinnerManager != nil && spinnerManager.DetailsHidden() {
		return false
	}
	return true
}

func printWithSpinnerPaused(spinnerManager *lib.SpinnerManager, fn func()) {
	if spinnerManager == nil || fn == nil {
		if fn != nil {
			fn()
		}
		return
	}
	ctx := spinnerManager.GetContext()
	if ctx == nil {
		fn()
		return
	}
	spinnerManager.Hide()
	fmt.Fprint(os.Stdout, "\n")
	fn()
	spinnerManager.Show(ctx.Type, ctx.Message)
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
	if cfg.SkipWaits {
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

			if retryDelay, parseErr := lib.ParseRetryDelay(lastError); parseErr == nil {
				delay = retryDelay
				if lib.Is429Error(lastError) {
					messageType = "Rate limited"
				} else {
					messageType = "Retrying"
				}
				logger.Log("info", "Retrying after parsed delay: %v (attempt %d/%d)", delay, attempt, maxRetries)
			} else {
				if lib.Is429Error(lastError) {
					delay = lib.CalculateExponentialBackoff(attempt, initialBackoff, backoffFactor)
					messageType = "Rate limited"
					logger.Log("info", "Failed to parse retry delay for 429, using exponential backoff: %v", parseErr)
					logger.Log("info", "Retrying 429 error with backoff in %v (attempt %d/%d)", delay, attempt, maxRetries)
				} else {
					delay = lib.CalculateExponentialBackoff(attempt, initialBackoff, backoffFactor)
					messageType = "Retrying"
					logger.Log("info", "Retrying in %v (attempt %d/%d)", delay, attempt, maxRetries)
				}
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
			retryable := lib.IsRetryableError(err)

			if !retryable {
				errorMsg := lib.GetErrorMessage(err)
				if errorMsg != "" {
					return nil, "", fmt.Errorf("%s: %w", errorMsg, err)
				}
				return nil, "", err
			}

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
