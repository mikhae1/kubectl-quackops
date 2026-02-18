package llm

import (
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/tmc/langchaingo/llms"
)

func TestTurnProcessor_StateTransitions_NoTools(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.SkipWaits = true

	initialResp := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{Content: "final answer without tools"},
		},
	}

	transitions := make([]string, 0, 4)
	processor := NewTurnProcessor(TurnProcessorParams{
		Cfg:                 cfg,
		Client:              NewMockLLMClient(nil),
		StartEscBreaker:     func(cancel func()) func() { return func() {} },
		Response:            initialResp,
		ResponseContent:     "final answer without tools",
		UseStreaming:        false,
		ContentAlreadyShown: false,
		DisplayedContent:    "",
		BufferedToolBlocks:  nil,
		SessionToolCalls:    nil,
		StateTransitionHookFn: func(from, to TurnProcessorState) {
			transitions = append(transitions, string(from)+"->"+string(to))
		},
	})

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := processor.Process()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if result.ResponseContent != "final answer without tools" {
		t.Fatalf("unexpected response content: %q", result.ResponseContent)
	}
	if len(result.SessionToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(result.SessionToolCalls))
	}
	if got, want := strings.Join(transitions, ","), "plan->done"; got != want {
		t.Fatalf("unexpected state transitions: got %q want %q", got, want)
	}
}

func TestTurnProcessor_StateTransitions_ToolRound(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.SkipWaits = true
	cfg.MCPMaxToolCalls = 5
	cfg.MCPMaxToolCallsTotal = 10
	cfg.MCPToolResultBudgetBytes = 0
	cfg.MCPStallThreshold = 0
	cfg.MCPNoProgressThreshold = 0
	cfg.MCPCacheToolResults = true

	initialResp := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				ToolCalls: []llms.ToolCall{
					{ID: "tp-1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}},
				},
			},
		},
	}

	mockClient := NewMockLLMClient([]MockResponse{
		{Content: "final response after one tool round"},
	})

	var callCount int32
	origExecute := executeMCPTool
	t.Cleanup(func() { executeMCPTool = origExecute })
	executeMCPTool = func(cfg *config.Config, toolName string, args map[string]any) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "tool-output", nil
	}

	transitions := make([]string, 0, 8)
	processor := NewTurnProcessor(TurnProcessorParams{
		Cfg:             cfg,
		Client:          mockClient,
		Messages:        []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "diagnose")},
		StartEscBreaker: func(cancel func()) func() { return func() {} },
		Response:        initialResp,
		UseStreaming:    false,
		StateTransitionHookFn: func(from, to TurnProcessorState) {
			transitions = append(transitions, string(from)+"->"+string(to))
		},
	})

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := processor.Process()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if callCount != 1 {
		t.Fatalf("expected one MCP tool execution, got %d", callCount)
	}
	if !strings.Contains(result.ResponseContent, "final response after one tool round") {
		t.Fatalf("unexpected final response: %q", result.ResponseContent)
	}
	if len(result.SessionToolCalls) != 1 {
		t.Fatalf("expected 1 recorded tool call, got %d", len(result.SessionToolCalls))
	}
	wantTransitions := "plan->execute_tools,execute_tools->integrate,integrate->plan,plan->done"
	if got := strings.Join(transitions, ","); got != wantTransitions {
		t.Fatalf("unexpected state transitions: got %q want %q", got, wantTransitions)
	}
}

func TestTurnProcessor_StateTransitions_Finalize(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.SkipWaits = true
	cfg.MCPMaxToolCalls = 5
	cfg.MCPMaxToolCallsTotal = 10
	cfg.MCPToolResultBudgetBytes = 1
	cfg.MCPStallThreshold = 0
	cfg.MCPNoProgressThreshold = 0

	initialResp := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				ToolCalls: []llms.ToolCall{
					{ID: "tp-fin-1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}},
				},
			},
		},
	}

	mockClient := NewMockLLMClient([]MockResponse{
		{Content: "finalized response"},
	})

	origExecute := executeMCPTool
	t.Cleanup(func() { executeMCPTool = origExecute })
	executeMCPTool = func(cfg *config.Config, toolName string, args map[string]any) (string, error) {
		return "tool-output-that-exceeds-budget", nil
	}

	transitions := make([]string, 0, 8)
	processor := NewTurnProcessor(TurnProcessorParams{
		Cfg:             cfg,
		Client:          mockClient,
		Messages:        []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, "diagnose")},
		StartEscBreaker: func(cancel func()) func() { return func() {} },
		Response:        initialResp,
		UseStreaming:    false,
		StateTransitionHookFn: func(from, to TurnProcessorState) {
			transitions = append(transitions, string(from)+"->"+string(to))
		},
	})

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := processor.Process()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if result == nil {
		t.Fatalf("expected non-nil result")
	}
	if !strings.Contains(result.ResponseContent, "finalized response") {
		t.Fatalf("unexpected final response: %q", result.ResponseContent)
	}
	history := mockClient.GetCallHistory()
	if len(history) == 0 {
		t.Fatalf("expected at least one LLM call during finalize")
	}
	finalMessages := history[len(history)-1].Messages
	if len(finalMessages) == 0 {
		t.Fatalf("expected finalize request messages")
	}
	if finalMessages[0].Role != llms.ChatMessageTypeSystem {
		t.Fatalf("expected finalize request to start with system note, got first role %q", finalMessages[0].Role)
	}
	for i, msg := range finalMessages {
		if msg.Role == llms.ChatMessageTypeSystem && i != 0 {
			t.Fatalf("found non-leading system message at index %d", i)
		}
	}
	if !strings.Contains(contentPartsToText(finalMessages[0].Parts), "Do not request additional tool calls") {
		t.Fatalf("expected finalize system note to be present in first message")
	}
	if !strings.Contains(cfg.LastMCPStopReason, "tool-result budget") {
		t.Fatalf("expected stop reason to mention tool-result budget, got %q", cfg.LastMCPStopReason)
	}
	wantTransitions := "plan->execute_tools,execute_tools->integrate,integrate->finalize,finalize->done"
	if got := strings.Join(transitions, ","); got != wantTransitions {
		t.Fatalf("unexpected state transitions: got %q want %q", got, wantTransitions)
	}
}

func contentPartsToText(parts []llms.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if textPart, ok := part.(llms.TextContent); ok {
			b.WriteString(textPart.Text)
		}
	}
	return b.String()
}
