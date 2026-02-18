package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/mikhae1/kubectl-quackops/pkg/mcp"
	"github.com/tmc/langchaingo/llms"
)

// TurnProcessorState represents a single stage in MCP tool-loop processing.
type TurnProcessorState string

const (
	TurnProcessorStatePlan         TurnProcessorState = "plan"
	TurnProcessorStateExecuteTools TurnProcessorState = "execute_tools"
	TurnProcessorStateIntegrate    TurnProcessorState = "integrate"
	TurnProcessorStateFinalize     TurnProcessorState = "finalize"
	TurnProcessorStateDone         TurnProcessorState = "done"
)

// TurnProcessorParams provides dependencies and initial state.
type TurnProcessorParams struct {
	Cfg             *config.Config
	SpinnerManager  *lib.SpinnerManager
	Client          llms.Model
	Messages        []llms.MessageContent
	GenerateOptions []llms.CallOption
	SpinnerMessage  string
	OutgoingTokens  int
	MaxRetries      int
	StartEscBreaker func(cancel func()) func()

	Response              *llms.ContentResponse
	ResponseContent       string
	UseStreaming          bool
	ContentAlreadyShown   bool
	DisplayedContent      string
	BufferedToolBlocks    []string
	SessionToolCalls      []config.ToolCallData
	StateTransitionHookFn func(from, to TurnProcessorState)
}

// TurnProcessorResult carries final MCP loop state back to Chat.
type TurnProcessorResult struct {
	Response            *llms.ContentResponse
	ResponseContent     string
	BufferedToolBlocks  []string
	SessionToolCalls    []config.ToolCallData
	ContentAlreadyShown bool
	DisplayedContent    string
}

// TurnProcessor executes MCP tool rounds using an explicit state machine.
type TurnProcessor struct {
	cfg             *config.Config
	spinnerManager  *lib.SpinnerManager
	client          llms.Model
	messages        []llms.MessageContent
	generateOptions []llms.CallOption
	spinnerMessage  string
	outgoingTokens  int
	maxRetries      int
	startEscBreaker func(cancel func()) func()

	resp          *llms.ContentResponse
	response      string
	useStreaming  bool
	contentShown  bool
	displayedText string
	toolBlocks    []string
	toolCalls     []config.ToolCallData

	state               TurnProcessorState
	stateTransitionHook func(from, to TurnProcessorState)

	choice           llms.ContentChoice
	preparedCalls    []mcpPreparedCall
	executedCalls    []mcpExecutedCall
	stopReason       string
	roundNewEvidence int

	toolCallCount           int
	maxToolCalls            int
	maxToolCallsTotal       int
	maxToolResultBudget     int
	stallThreshold          int
	totalToolCalls          int
	totalToolResultBytes    int
	lastPlanFingerprint     string
	repeatedPlanCount       int
	planFingerprintHistory  []string
	toolCallSignatureCounts map[string]int
	uniqueToolSignatures    map[string]struct{}
	toolResultCache         map[string]string
	toolArtifactCache       map[string]mcpArtifactRef
	seenEvidenceHashes      map[string]struct{}
	cacheHitCount           int
	repeatedToolCalls       int
	noProgressRounds        int
	finalStopReason         string
}

func NewTurnProcessor(params TurnProcessorParams) *TurnProcessor {
	spinnerManager := params.SpinnerManager
	if spinnerManager == nil && params.Cfg != nil {
		spinnerManager = lib.GetSpinnerManager(params.Cfg)
	}

	return &TurnProcessor{
		cfg:                     params.Cfg,
		spinnerManager:          spinnerManager,
		client:                  params.Client,
		messages:                params.Messages,
		generateOptions:         params.GenerateOptions,
		spinnerMessage:          params.SpinnerMessage,
		outgoingTokens:          params.OutgoingTokens,
		maxRetries:              params.MaxRetries,
		startEscBreaker:         params.StartEscBreaker,
		resp:                    params.Response,
		response:                params.ResponseContent,
		useStreaming:            params.UseStreaming,
		contentShown:            params.ContentAlreadyShown,
		displayedText:           params.DisplayedContent,
		toolBlocks:              params.BufferedToolBlocks,
		toolCalls:               params.SessionToolCalls,
		state:                   TurnProcessorStatePlan,
		stateTransitionHook:     params.StateTransitionHookFn,
		maxToolCalls:            params.Cfg.MCPMaxToolCalls,
		maxToolCallsTotal:       params.Cfg.MCPMaxToolCallsTotal,
		maxToolResultBudget:     params.Cfg.MCPToolResultBudgetBytes,
		stallThreshold:          params.Cfg.MCPStallThreshold,
		planFingerprintHistory:  make([]string, 0, 16),
		toolCallSignatureCounts: make(map[string]int),
		uniqueToolSignatures:    make(map[string]struct{}),
		toolResultCache:         make(map[string]string),
		toolArtifactCache:       make(map[string]mcpArtifactRef),
		seenEvidenceHashes:      make(map[string]struct{}),
	}
}

func (tp *TurnProcessor) Process() (*TurnProcessorResult, error) {
	if tp == nil {
		return nil, fmt.Errorf("nil turn processor")
	}
	for tp.state != TurnProcessorStateDone {
		var (
			next TurnProcessorState
			err  error
		)
		switch tp.state {
		case TurnProcessorStatePlan:
			next, err = tp.handlePlanState()
		case TurnProcessorStateExecuteTools:
			next, err = tp.handleExecuteState()
		case TurnProcessorStateIntegrate:
			next, err = tp.handleIntegrateState()
		case TurnProcessorStateFinalize:
			next, err = tp.handleFinalizeState()
		default:
			return nil, fmt.Errorf("unknown turn-processor state: %s", tp.state)
		}
		if err != nil {
			return nil, err
		}
		tp.setState(next)
	}

	tp.updateMetrics()
	return &TurnProcessorResult{
		Response:            tp.resp,
		ResponseContent:     tp.response,
		BufferedToolBlocks:  tp.toolBlocks,
		SessionToolCalls:    tp.toolCalls,
		ContentAlreadyShown: tp.contentShown,
		DisplayedContent:    tp.displayedText,
	}, nil
}

func (tp *TurnProcessor) setState(next TurnProcessorState) {
	if tp.stateTransitionHook != nil && tp.state != next {
		tp.stateTransitionHook(tp.state, next)
	}
	tp.state = next
}

func (tp *TurnProcessor) handlePlanState() (TurnProcessorState, error) {
	if tp.resp == nil || len(tp.resp.Choices) == 0 {
		return TurnProcessorStateDone, nil
	}
	tp.choice = *tp.resp.Choices[0]

	if tp.choice.Content != "" && !tp.contentShown {
		printWithSpinnerPaused(tp.spinnerManager, func() {
			if !tp.cfg.SuppressContentPrint {
				printContentFormatted(tp.cfg, tp.choice.Content, false)
			}
		})
		tp.contentShown = true
		tp.displayedText = tp.choice.Content
	}

	if len(tp.choice.ToolCalls) == 0 {
		return TurnProcessorStateDone, nil
	}

	tp.stopReason = ""
	tp.roundNewEvidence = 0

	if tp.maxToolCalls > 0 && tp.toolCallCount >= tp.maxToolCalls {
		tp.stopReason = fmt.Sprintf("maximum MCP tool-call iteration limit reached (%d)", tp.maxToolCalls)
	}

	if tp.stopReason == "" {
		planFingerprint := toolCallPlanFingerprint(tp.choice.ToolCalls)
		if planFingerprint != "" {
			if tp.stallThreshold > 0 {
				if planFingerprint == tp.lastPlanFingerprint {
					tp.repeatedPlanCount++
				} else {
					tp.lastPlanFingerprint = planFingerprint
					tp.repeatedPlanCount = 0
				}
				if tp.repeatedPlanCount >= tp.stallThreshold {
					tp.stopReason = fmt.Sprintf("MCP tool loop stalled: identical tool plan repeated %d times", tp.repeatedPlanCount+1)
				}
			}
			if tp.stopReason == "" && tp.cfg.MCPLoopCycleThreshold > 0 {
				if cycleDistance, ok := detectToolPlanCycle(tp.planFingerprintHistory, planFingerprint, tp.cfg.MCPLoopCycleThreshold); ok {
					tp.stopReason = fmt.Sprintf("MCP tool loop cycling: plan repeated after %d round(s)", cycleDistance)
				}
			}
			tp.planFingerprintHistory = append(tp.planFingerprintHistory, planFingerprint)
			if len(tp.planFingerprintHistory) > 16 {
				tp.planFingerprintHistory = tp.planFingerprintHistory[len(tp.planFingerprintHistory)-16:]
			}
		}
	}

	if tp.stopReason == "" && tp.maxToolCallsTotal > 0 {
		remainingCalls := tp.maxToolCallsTotal - tp.totalToolCalls
		if remainingCalls <= 0 {
			tp.stopReason = fmt.Sprintf("MCP total tool-call budget exhausted (%d)", tp.maxToolCallsTotal)
		} else if len(tp.choice.ToolCalls) > remainingCalls {
			logger.Log("warn", "MCP total tool-call budget allows only %d more call(s); truncating current tool-call batch", remainingCalls)
			tp.choice.ToolCalls = tp.choice.ToolCalls[:remainingCalls]
		}
	}

	if tp.stopReason != "" {
		logger.Log("warn", "%s", tp.stopReason)
		tp.finalStopReason = tp.stopReason
		return TurnProcessorStateFinalize, nil
	}

	logger.Log("info", "Processing MCP tool call: iteration %d of %d...", tp.toolCallCount+1, tp.maxToolCalls)

	assistantParts := make([]llms.ContentPart, 0, len(tp.choice.ToolCalls))
	for i := range tp.choice.ToolCalls {
		tc := tp.choice.ToolCalls[i]
		if tc.FunctionCall == nil {
			logger.Log("warn", "Tool call %s has no function call data", tc.ID)
			continue
		}
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("tool_%s_%d", tc.FunctionCall.Name, time.Now().UnixNano())
			logger.Log("debug", "Generated missing tool call ID: %s", tc.ID)
		}
		assistantParts = append(assistantParts, tc)
		tp.choice.ToolCalls[i] = tc
	}
	if len(assistantParts) > 0 {
		assistantMsg := llms.MessageContent{Role: llms.ChatMessageTypeAI, Parts: assistantParts}
		tp.messages = append(tp.messages, assistantMsg)
	}

	tp.preparedCalls = make([]mcpPreparedCall, 0, len(tp.choice.ToolCalls))
	for _, tc := range tp.choice.ToolCalls {
		if tp.maxToolCallsTotal > 0 && tp.totalToolCalls+len(tp.preparedCalls) >= tp.maxToolCallsTotal {
			tp.stopReason = fmt.Sprintf("MCP total tool-call budget exhausted (%d)", tp.maxToolCallsTotal)
			logger.Log("warn", "%s", tp.stopReason)
			break
		}
		if tp.maxToolResultBudget > 0 && tp.totalToolResultBytes >= tp.maxToolResultBudget {
			tp.stopReason = fmt.Sprintf("MCP tool-result budget exhausted (%d bytes)", tp.maxToolResultBudget)
			logger.Log("warn", "%s", tp.stopReason)
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
			if _, seen := tp.uniqueToolSignatures[signature]; seen {
				tp.repeatedToolCalls++
			} else {
				tp.uniqueToolSignatures[signature] = struct{}{}
			}
			if tp.cfg.MCPToolRepeatLimit > 0 && tp.toolCallSignatureCounts[signature] >= tp.cfg.MCPToolRepeatLimit {
				tp.stopReason = fmt.Sprintf("MCP tool repeat limit reached for %s (%d)", tc.FunctionCall.Name, tp.cfg.MCPToolRepeatLimit)
				logger.Log("warn", "%s", tp.stopReason)
				break
			}
			tp.toolCallSignatureCounts[signature]++
		}

		emitPlanProgress(PlanProgressEvent{
			Kind:          PlanProgressToolStarted,
			ToolName:      tc.FunctionCall.Name,
			Iteration:     tp.toolCallCount + 1,
			MaxIterations: tp.maxToolCalls,
		})

		tp.preparedCalls = append(tp.preparedCalls, mcpPreparedCall{
			ToolCall:   tc,
			Args:       args,
			Signature:  signature,
			Iteration:  tp.toolCallCount + 1,
			MaxRounds:  tp.maxToolCalls,
			CacheAllow: tp.cfg.MCPCacheToolResults,
		})
	}

	return TurnProcessorStateExecuteTools, nil
}

func (tp *TurnProcessor) handleExecuteState() (TurnProcessorState, error) {
	executedCalls, batchErr := executePreparedMCPCalls(tp.cfg, tp.spinnerManager, tp.preparedCalls, tp.toolResultCache)
	if batchErr != nil {
		if lib.IsUserCancel(batchErr) {
			return TurnProcessorStateDone, lib.NewUserCancelError("canceled by user")
		}
		return TurnProcessorStateDone, batchErr
	}
	tp.executedCalls = executedCalls
	return TurnProcessorStateIntegrate, nil
}

func (tp *TurnProcessor) handleIntegrateState() (TurnProcessorState, error) {
	for _, executed := range tp.executedCalls {
		tc := executed.Prepared.ToolCall
		args := executed.Prepared.Args
		toolResult := executed.ToolResult
		callErr := executed.CallErr
		signature := executed.Prepared.Signature
		if executed.CacheHit {
			tp.cacheHitCount++
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
			if cachedArtifact, ok := tp.toolArtifactCache[signature]; ok && cachedArtifact.Path != "" {
				artifactPath = cachedArtifact.Path
				artifactSHA = cachedArtifact.SHA
			}
		}
		if artifactPath == "" {
			if persistedPath, persistedSHA, persistErr := persistToolResultArtifact(tp.cfg, tc.FunctionCall.Name, args, toolResult); persistErr != nil {
				logger.Log("warn", "Failed to persist MCP tool output artifact for %s: %v", tc.FunctionCall.Name, persistErr)
			} else {
				artifactPath = persistedPath
				artifactSHA = persistedSHA
				if signature != "" && artifactPath != "" {
					tp.toolArtifactCache[signature] = mcpArtifactRef{Path: artifactPath, SHA: artifactSHA}
				}
			}
		}
		displayToolResult := appendArtifactReference(toolResult, artifactPath, artifactSHA)

		tp.toolCalls = append(tp.toolCalls, config.ToolCallData{
			Name:           tc.FunctionCall.Name,
			Args:           args,
			Result:         displayToolResult,
			ResultBytes:    len(toolResult),
			ArtifactPath:   artifactPath,
			ArtifactSHA256: artifactSHA,
		})

		tp.totalToolCalls++
		tp.totalToolResultBytes += len(toolResult)
		if tp.stopReason == "" && tp.maxToolResultBudget > 0 && tp.totalToolResultBytes >= tp.maxToolResultBudget {
			tp.stopReason = fmt.Sprintf("MCP tool-result budget reached (%d/%d bytes)", tp.totalToolResultBytes, tp.maxToolResultBudget)
			logger.Log("warn", "%s", tp.stopReason)
		}

		evidenceHash := hashToolEvidence(tc.FunctionCall.Name, args, toolResult)
		if _, seen := tp.seenEvidenceHashes[evidenceHash]; !seen {
			tp.seenEvidenceHashes[evidenceHash] = struct{}{}
			tp.roundNewEvidence++
		}

		if tp.stopReason != "" {
			continue
		}

		modelToolResult := compactToolResultForModel(tp.cfg, tc.FunctionCall.Name, toolResult, artifactPath, artifactSHA)
		updateResponseTime()

		var block string
		if tp.cfg.Verbose {
			block = mcp.FormatToolCallVerbose(tc.FunctionCall.Name, args, displayToolResult)
		} else {
			block = mcp.FormatToolCallBlock(tc.FunctionCall.Name, args, displayToolResult)
		}

		if tp.useStreaming {
			tp.toolBlocks = append(tp.toolBlocks, block)
		} else if shouldPrintToolBlocks(tp.cfg, tp.spinnerManager) {
			printWithSpinnerPaused(tp.spinnerManager, func() {
				fmt.Fprint(os.Stdout, block)
			})
		}

		toolMsg := llms.MessageContent{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{llms.ToolCallResponse{
				ToolCallID: tc.ID,
				Name:       tc.FunctionCall.Name,
				Content:    modelToolResult,
			}},
		}
		tp.messages = append(tp.messages, toolMsg)
	}

	tp.toolCallCount++

	if tp.stopReason == "" && tp.cfg.MCPNoProgressThreshold > 0 {
		if tp.roundNewEvidence == 0 {
			tp.noProgressRounds++
			if tp.noProgressRounds >= tp.cfg.MCPNoProgressThreshold {
				tp.stopReason = fmt.Sprintf("MCP tool loop made no progress for %d round(s)", tp.noProgressRounds)
			}
		} else {
			tp.noProgressRounds = 0
		}
	}

	if tp.stopReason != "" {
		tp.finalStopReason = tp.stopReason
		return TurnProcessorStateFinalize, nil
	}

	if err := applyThrottleDelayWithSpinnerManager(tp.cfg, tp.spinnerManager); err != nil {
		if lib.IsUserCancel(err) {
			return TurnProcessorStateDone, lib.NewUserCancelError("canceled by user")
		}
		return TurnProcessorStateDone, err
	}

	resp, responseContent, err := generateWithRetries(tp.cfg, tp.spinnerManager, tp.client, tp.messages, tp.generateOptions, tp.spinnerMessage, tp.outgoingTokens, tp.maxRetries, tp.startEscBreaker)
	if err != nil {
		return TurnProcessorStateDone, err
	}
	tp.resp = resp
	tp.response = responseContent
	return TurnProcessorStatePlan, nil
}

func (tp *TurnProcessor) handleFinalizeState() (TurnProcessorState, error) {
	if strings.TrimSpace(tp.finalStopReason) == "" {
		tp.finalStopReason = tp.stopReason
	}
	resp, responseContent, ferr := finalizeAfterToolLoopStop(
		tp.cfg,
		tp.spinnerManager,
		tp.client,
		tp.messages,
		tp.generateOptions,
		tp.spinnerMessage,
		tp.outgoingTokens,
		tp.maxRetries,
		tp.startEscBreaker,
		tp.finalStopReason,
	)
	if ferr != nil {
		return TurnProcessorStateDone, ferr
	}
	tp.resp = resp
	tp.response = responseContent
	if strings.TrimSpace(tp.response) == "" {
		tp.response = fmt.Sprintf("Stopped MCP tool execution: %s", tp.finalStopReason)
	}
	return TurnProcessorStateDone, nil
}

func (tp *TurnProcessor) updateMetrics() {
	if tp.cfg == nil {
		return
	}
	tp.cfg.LastMCPToolCallsTotal = tp.totalToolCalls
	tp.cfg.LastMCPUniqueToolCalls = len(tp.uniqueToolSignatures)
	tp.cfg.LastMCPRepeatedToolCalls = tp.repeatedToolCalls
	tp.cfg.LastMCPCacheHits = tp.cacheHitCount
	tp.cfg.LastMCPStopReason = tp.finalStopReason
}
