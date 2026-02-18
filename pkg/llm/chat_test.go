package llm

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/tmc/langchaingo/llms"
)

func TestChat_BasicFunctionality(t *testing.T) {
	tests := []struct {
		name          string
		cfg           *config.Config
		prompt        string
		stream        bool
		history       bool
		mockResponses []MockResponse
		expectedText  string
		expectError   bool
	}{
		{
			name:    "simple_success",
			cfg:     CreateTestConfig(),
			prompt:  "Hello, how are you?",
			stream:  false,
			history: false,
			mockResponses: []MockResponse{
				{
					Content:    "I'm doing well, thank you for asking!",
					TokensUsed: 45,
				},
			},
			expectedText: "I'm doing well, thank you for asking!",
			expectError:  false,
		},
		{
			name:    "with_history",
			cfg:     CreateTestConfigWithHistory(),
			prompt:  "What did I ask before?",
			stream:  false,
			history: true,
			mockResponses: []MockResponse{
				{
					Content:    "You previously asked about Kubernetes pods",
					TokensUsed: 40,
				},
			},
			expectedText: "You previously asked about Kubernetes pods",
			expectError:  false,
		},
		{
			name:          "streaming_mode",
			cfg:           CreateTestConfig(),
			prompt:        "Tell me about containers",
			stream:        true,
			history:       false,
			mockResponses: TestScenarios{}.StreamingResponse(),
			expectedText:  "This is a streaming response.",
			expectError:   false,
		},
		{
			name:          "error_handling",
			cfg:           CreateTestConfig(),
			prompt:        "This should fail",
			stream:        false,
			history:       false,
			mockResponses: TestScenarios{}.ErrorResponse(),
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock client
			mockClient := NewMockLLMClient(tt.mockResponses)

			// Capture output
			oldStdout := os.Stdout
			oldStderr := os.Stderr
			rOut, wOut, _ := os.Pipe()
			rErr, wErr, _ := os.Pipe()
			os.Stdout = wOut
			os.Stderr = wErr

			// Execute chat
			result, err := Chat(tt.cfg, mockClient, tt.prompt, tt.stream, tt.history)

			// Restore output
			wOut.Close()
			wErr.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr

			// Read captured output
			outBytes, _ := io.ReadAll(rOut)
			errBytes, _ := io.ReadAll(rErr)
			stdout := string(outBytes)
			stderr := string(errBytes)

			// Assertions
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					t.Logf("Stdout: %s", stdout)
					t.Logf("Stderr: %s", stderr)
				}
				if !strings.Contains(result, tt.expectedText) {
					t.Errorf("Expected result to contain '%s', got '%s'", tt.expectedText, result)
				}
			}

			// Verify history management
			if tt.history && !tt.expectError {
				if len(tt.cfg.ChatMessages) == 0 {
					t.Errorf("Expected chat messages to be added to history")
				}
			}

			// Verify token accounting
			if !tt.expectError {
				if tt.cfg.LastOutgoingTokens <= 0 {
					t.Errorf("Expected LastOutgoingTokens to be set")
				}
			}
		})
	}
}

func TestChat_RetryLogic(t *testing.T) {
	tests := []struct {
		name             string
		cfg              *config.Config
		mockResponses    []MockResponse
		expectedRetries  int
		expectFinalError bool
	}{
		{
			name: "successful_retry",
			cfg:  CreateTestConfigWithRetries(2),
			mockResponses: []MockResponse{
				{Error: fmt.Errorf("temporary failure")},
				{Content: "Success after retry", TokensUsed: 30},
			},
			expectedRetries:  1,
			expectFinalError: false,
		},
		{
			name: "max_retries_exceeded",
			cfg:  CreateTestConfigWithRetries(1),
			mockResponses: []MockResponse{
				{Error: fmt.Errorf("first failure")},
				{Error: fmt.Errorf("second failure")},
			},
			expectedRetries:  1,
			expectFinalError: true,
		},
		{
			name: "rate_limit_handling",
			cfg:  CreateTestConfigWithRetries(2),
			mockResponses: []MockResponse{
				{SimulateRateLimit: true},
				{Content: "Success after rate limit", TokensUsed: 25},
			},
			expectedRetries:  1,
			expectFinalError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := NewMockLLMClient(tt.mockResponses)

			// Capture output to avoid spinner noise in tests
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			result, err := Chat(tt.cfg, mockClient, "test prompt", false, false)

			w.Close()
			os.Stderr = oldStderr
			io.ReadAll(r) // Discard captured output

			callHistory := mockClient.GetCallHistory()

			if tt.expectFinalError {
				if err == nil {
					t.Errorf("Expected final error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result == "" {
					t.Errorf("Expected non-empty result")
				}
			}

			expectedCalls := tt.expectedRetries + 1
			if len(callHistory) != expectedCalls {
				t.Errorf("Expected %d calls, got %d", expectedCalls, len(callHistory))
			}
		})
	}
}

func TestChat_TokenManagement(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *config.Config
		prompt       string
		expectedKeys []string
	}{
		{
			name:         "token_limits",
			cfg:          CreateTestConfigWithTokens(1000, 100, 50),
			prompt:       "Test token management",
			expectedKeys: []string{"max_tokens"},
		},
		{
			name:         "no_token_limits",
			cfg:          CreateTestConfigWithTokens(0, 0, 0),
			prompt:       "No limits test",
			expectedKeys: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := NewMockLLMClient(TestScenarios{}.SimpleSuccess())

			// Capture stderr to avoid spinner output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			_, err := Chat(tt.cfg, mockClient, tt.prompt, false, false)

			w.Close()
			os.Stderr = oldStderr
			io.ReadAll(r)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify that token accounting was performed
			if tt.cfg.LastOutgoingTokens <= 0 {
				t.Errorf("Expected LastOutgoingTokens to be set")
			}

			// Check that options were passed correctly (would need to inspect mock)
			callHistory := mockClient.GetCallHistory()
			if len(callHistory) == 0 {
				t.Errorf("Expected at least one call to be made")
			}
		})
	}
}

func TestChat_StreamingBehavior(t *testing.T) {
	tests := []struct {
		name            string
		stream          bool
		mockResponses   []MockResponse
		expectStreaming bool
	}{
		{
			name:            "streaming_enabled",
			stream:          true,
			mockResponses:   TestScenarios{}.StreamingResponse(),
			expectStreaming: true,
		},
		{
			name:            "streaming_disabled",
			stream:          false,
			mockResponses:   TestScenarios{}.SimpleSuccess(),
			expectStreaming: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CreateTestConfig()
			mockClient := NewMockLLMClient(tt.mockResponses)

			// Capture all output
			oldStdout := os.Stdout
			oldStderr := os.Stderr
			rOut, wOut, _ := os.Pipe()
			rErr, wErr, _ := os.Pipe()
			os.Stdout = wOut
			os.Stderr = wErr

			result, err := Chat(cfg, mockClient, "test streaming", tt.stream, false)

			wOut.Close()
			wErr.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr

			outBytes, _ := io.ReadAll(rOut)
			errBytes, _ := io.ReadAll(rErr)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				t.Logf("Stdout: %s", string(outBytes))
				t.Logf("Stderr: %s", string(errBytes))
			}

			if result == "" {
				t.Errorf("Expected non-empty result")
			}

			// Verify streaming behavior was applied
			callHistory := mockClient.GetCallHistory()
			if len(callHistory) == 0 {
				t.Errorf("Expected at least one call")
			}

			// The mock should record whether streaming was requested.
			if callHistory[0].Stream != tt.expectStreaming {
				t.Errorf("Expected streaming=%t, got %t", tt.expectStreaming, callHistory[0].Stream)
			}
		})
	}
}

func TestChat_VerboseMode(t *testing.T) {
	tests := []struct {
		name      string
		verbose   bool
		expectLog bool
	}{
		{
			name:      "verbose_enabled",
			verbose:   true,
			expectLog: true,
		},
		{
			name:      "verbose_disabled",
			verbose:   false,
			expectLog: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CreateTestConfigWithOptions(map[string]interface{}{
				"verbose": tt.verbose,
			})

			mockClient := NewMockLLMClient(TestScenarios{}.SimpleSuccess())

			// Capture stderr for log output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			result, err := Chat(cfg, mockClient, "test verbose", false, false)

			w.Close()
			os.Stderr = oldStderr
			stderrBytes, _ := io.ReadAll(r)
			stderrOutput := string(stderrBytes)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if result == "" {
				t.Errorf("Expected non-empty result")
			}

			// In verbose mode, we expect more logging output
			hasDetailedOutput := len(stderrOutput) > 0

			if tt.expectLog && !hasDetailedOutput {
				t.Errorf("Expected verbose logging output in stderr, got: %s", stderrOutput)
			}
		})
	}
}

func TestChat_HistoryManagement(t *testing.T) {
	tests := []struct {
		name            string
		history         bool
		initialMessages []llms.ChatMessage
		expectedCount   int
	}{
		{
			name:            "history_enabled",
			history:         true,
			initialMessages: []llms.ChatMessage{},
			expectedCount:   2, // human + ai
		},
		{
			name:            "history_disabled",
			history:         false,
			initialMessages: []llms.ChatMessage{},
			expectedCount:   0, // no messages added
		},
		{
			name:    "existing_history",
			history: true,
			initialMessages: []llms.ChatMessage{
				llms.HumanChatMessage{Content: "Previous question"},
				llms.AIChatMessage{Content: "Previous answer"},
			},
			expectedCount: 4, // 2 existing + 2 new
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CreateTestConfig()
			cfg.ChatMessages = tt.initialMessages

			mockClient := NewMockLLMClient(TestScenarios{}.SimpleSuccess())

			// Capture output to avoid noise
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			_, err := Chat(cfg, mockClient, "test history", false, tt.history)

			w.Close()
			os.Stderr = oldStderr
			io.ReadAll(r)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(cfg.ChatMessages) != tt.expectedCount {
				t.Errorf("Expected %d messages in history, got %d", tt.expectedCount, len(cfg.ChatMessages))
			}
		})
	}
}

func TestChat_MCPTotalToolBudget(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.MCPMaxToolCalls = 10
	cfg.MCPMaxToolCallsTotal = 1
	cfg.MCPToolResultBudgetBytes = 0
	cfg.MCPStallThreshold = 0

	mockClient := NewMockLLMClient([]MockResponse{
		{
			ToolCalls: []llms.ToolCall{
				{ID: "t1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}},
				{ID: "t2", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_events", Arguments: `{"namespace":"default"}`}},
			},
		},
		{Content: "Final answer after tool budget"},
	})

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := Chat(cfg, mockClient, "check cluster", false, false)

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !strings.Contains(result, "Final answer after tool budget") {
		t.Fatalf("expected budget flow to reach final answer, got %q", result)
	}
	if len(mockClient.GetCallHistory()) != 2 {
		t.Fatalf("expected 2 LLM calls (tool + final), got %d", len(mockClient.GetCallHistory()))
	}
}

func TestChat_MCPStallDetection(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.MCPMaxToolCalls = 10
	cfg.MCPMaxToolCallsTotal = 20
	cfg.MCPToolResultBudgetBytes = 0
	cfg.MCPStallThreshold = 1 // Stop on first repeated identical tool plan

	stalledToolCall := llms.ToolCall{ID: "stall-1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}}
	mockClient := NewMockLLMClient([]MockResponse{
		{ToolCalls: []llms.ToolCall{stalledToolCall}},
		{ToolCalls: []llms.ToolCall{stalledToolCall}},
		{Content: "Final answer after stall detection"},
	})

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := Chat(cfg, mockClient, "diagnose repeated calls", false, false)

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !strings.Contains(result, "Final answer after stall detection") {
		t.Fatalf("expected stall flow to produce final answer, got %q", result)
	}
	if len(mockClient.GetCallHistory()) != 3 {
		t.Fatalf("expected 3 LLM calls (tool, repeated tool, forced final), got %d", len(mockClient.GetCallHistory()))
	}
}

func TestChat_MCPCacheToolResults(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.MCPMaxToolCalls = 10
	cfg.MCPMaxToolCallsTotal = 20
	cfg.MCPToolResultBudgetBytes = 0
	cfg.MCPStallThreshold = 0
	cfg.MCPCacheToolResults = true

	repeatedToolCall := llms.ToolCall{ID: "cache-1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}}
	mockClient := NewMockLLMClient([]MockResponse{
		{ToolCalls: []llms.ToolCall{repeatedToolCall}},
		{ToolCalls: []llms.ToolCall{repeatedToolCall}},
		{Content: "Final answer after cache"},
	})

	callCount := 0
	origExecute := executeMCPTool
	executeMCPTool = func(cfg *config.Config, toolName string, args map[string]any) (string, error) {
		callCount++
		return "cached-tool-result", nil
	}
	defer func() { executeMCPTool = origExecute }()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := Chat(cfg, mockClient, "diagnose with cache", false, false)

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !strings.Contains(result, "Final answer after cache") {
		t.Fatalf("expected cached flow to produce final answer, got %q", result)
	}
	if callCount != 1 {
		t.Fatalf("expected MCP tool to execute once with cache enabled, got %d", callCount)
	}
	if cfg.LastMCPCacheHits < 1 {
		t.Fatalf("expected at least one MCP cache hit, got %d", cfg.LastMCPCacheHits)
	}
}

func TestChat_MCPToolRepeatLimit(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.MCPMaxToolCalls = 10
	cfg.MCPMaxToolCallsTotal = 20
	cfg.MCPToolResultBudgetBytes = 0
	cfg.MCPStallThreshold = 0
	cfg.MCPToolRepeatLimit = 1
	cfg.MCPCacheToolResults = false

	repeatedToolCall := llms.ToolCall{ID: "repeat-1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}}
	mockClient := NewMockLLMClient([]MockResponse{
		{ToolCalls: []llms.ToolCall{repeatedToolCall}},
		{ToolCalls: []llms.ToolCall{repeatedToolCall}},
		{Content: "Final answer after repeat limit"},
	})

	callCount := 0
	origExecute := executeMCPTool
	executeMCPTool = func(cfg *config.Config, toolName string, args map[string]any) (string, error) {
		callCount++
		return "tool-result", nil
	}
	defer func() { executeMCPTool = origExecute }()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := Chat(cfg, mockClient, "diagnose with repeat limit", false, false)

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !strings.Contains(result, "Final answer after repeat limit") {
		t.Fatalf("expected repeat-limit flow to produce final answer, got %q", result)
	}
	if callCount != 1 {
		t.Fatalf("expected MCP tool to execute once before repeat limit, got %d", callCount)
	}
	if !strings.Contains(cfg.LastMCPStopReason, "repeat limit") {
		t.Fatalf("expected stop reason to mention repeat limit, got %q", cfg.LastMCPStopReason)
	}
}

func TestExecutePreparedMCPCalls_ParallelExecution(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.SkipWaits = true
	cfg.MCPParallelToolCalls = 3

	prepared := []mcpPreparedCall{
		{
			ToolCall: llms.ToolCall{ID: "p1", FunctionCall: &llms.FunctionCall{Name: "tool_one"}},
			Args:     map[string]any{"id": 1},
		},
		{
			ToolCall: llms.ToolCall{ID: "p2", FunctionCall: &llms.FunctionCall{Name: "tool_two"}},
			Args:     map[string]any{"id": 2},
		},
		{
			ToolCall: llms.ToolCall{ID: "p3", FunctionCall: &llms.FunctionCall{Name: "tool_three"}},
			Args:     map[string]any{"id": 3},
		},
	}

	origExecute := executeMCPTool
	t.Cleanup(func() { executeMCPTool = origExecute })

	var active int32
	var maxActive int32
	executeMCPTool = func(cfg *config.Config, toolName string, args map[string]any) (string, error) {
		cur := atomic.AddInt32(&active, 1)
		for {
			prev := atomic.LoadInt32(&maxActive)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxActive, prev, cur) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return toolName + "_ok", nil
	}

	results, err := executePreparedMCPCalls(cfg, nil, prepared, map[string]string{})
	if err != nil {
		t.Fatalf("executePreparedMCPCalls returned error: %v", err)
	}
	if len(results) != len(prepared) {
		t.Fatalf("expected %d results, got %d", len(prepared), len(results))
	}
	if maxActive < 2 {
		t.Fatalf("expected parallel execution (max concurrency >= 2), got %d", maxActive)
	}
	if results[0].ToolResult != "tool_one_ok" || results[1].ToolResult != "tool_two_ok" || results[2].ToolResult != "tool_three_ok" {
		t.Fatalf("expected output order to match tool call order, got %#v", results)
	}
}

func TestExecutePreparedMCPCalls_DeduplicatesSameRoundSignatures(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.SkipWaits = true
	cfg.MCPParallelToolCalls = 3

	prepared := []mcpPreparedCall{
		{
			ToolCall:   llms.ToolCall{ID: "d1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods"}},
			Args:       map[string]any{"namespace": "default"},
			Signature:  "kubectl_get_pods|{\"namespace\":\"default\"}",
			CacheAllow: true,
		},
		{
			ToolCall:   llms.ToolCall{ID: "d2", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods"}},
			Args:       map[string]any{"namespace": "default"},
			Signature:  "kubectl_get_pods|{\"namespace\":\"default\"}",
			CacheAllow: true,
		},
		{
			ToolCall:   llms.ToolCall{ID: "d3", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods"}},
			Args:       map[string]any{"namespace": "default"},
			Signature:  "kubectl_get_pods|{\"namespace\":\"default\"}",
			CacheAllow: true,
		},
	}

	origExecute := executeMCPTool
	t.Cleanup(func() { executeMCPTool = origExecute })

	var callCount int32
	executeMCPTool = func(cfg *config.Config, toolName string, args map[string]any) (string, error) {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(25 * time.Millisecond)
		return "dedup-result", nil
	}

	results, err := executePreparedMCPCalls(cfg, nil, prepared, map[string]string{})
	if err != nil {
		t.Fatalf("executePreparedMCPCalls returned error: %v", err)
	}
	if len(results) != len(prepared) {
		t.Fatalf("expected %d results, got %d", len(prepared), len(results))
	}
	if callCount != 1 {
		t.Fatalf("expected duplicate signatures to execute once, got %d executions", callCount)
	}
	for idx, executed := range results {
		if executed.ToolResult != "dedup-result" {
			t.Fatalf("unexpected tool result at index %d: %q", idx, executed.ToolResult)
		}
		if executed.Prepared.ToolCall.ID != prepared[idx].ToolCall.ID {
			t.Fatalf("expected result %d to keep original tool call ID %q, got %q", idx, prepared[idx].ToolCall.ID, executed.Prepared.ToolCall.ID)
		}
	}
	if !results[1].CacheHit || !results[2].CacheHit {
		t.Fatalf("expected duplicate signatures to be reported as cache hits")
	}
}

func TestChat_MCPDedupesDuplicateToolCallsInRound(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.MCPMaxToolCalls = 5
	cfg.MCPMaxToolCallsTotal = 10
	cfg.MCPToolResultBudgetBytes = 0
	cfg.MCPStallThreshold = 0
	cfg.MCPCacheToolResults = true
	cfg.MCPParallelToolCalls = 3
	cfg.SkipWaits = true

	mockClient := NewMockLLMClient([]MockResponse{
		{
			ToolCalls: []llms.ToolCall{
				{ID: "dup-1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}},
				{ID: "dup-2", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}},
			},
		},
		{Content: "Final answer after same-round dedupe"},
	})

	origExecute := executeMCPTool
	t.Cleanup(func() { executeMCPTool = origExecute })

	var callCount int32
	executeMCPTool = func(cfg *config.Config, toolName string, args map[string]any) (string, error) {
		atomic.AddInt32(&callCount, 1)
		return "dedup-tool-result", nil
	}

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := Chat(cfg, mockClient, "diagnose same-round duplicates", false, false)

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !strings.Contains(result, "Final answer after same-round dedupe") {
		t.Fatalf("expected dedupe flow to produce final answer, got %q", result)
	}
	if callCount != 1 {
		t.Fatalf("expected same-round duplicate tool calls to execute once, got %d", callCount)
	}
	if cfg.LastMCPCacheHits < 1 {
		t.Fatalf("expected at least one MCP cache hit due to dedupe, got %d", cfg.LastMCPCacheHits)
	}
	if len(cfg.SessionHistory) == 0 {
		t.Fatalf("expected session history to be populated")
	}
	last := cfg.SessionHistory[len(cfg.SessionHistory)-1]
	if len(last.ToolCalls) != 2 {
		t.Fatalf("expected both tool calls to be recorded, got %d", len(last.ToolCalls))
	}
}

func TestChat_PersistsLargeToolResultArtifact(t *testing.T) {
	cfg := CreateTestConfig()
	cfg.MCPClientEnabled = true
	cfg.MCPMaxToolCalls = 5
	cfg.MCPMaxToolCallsTotal = 10
	cfg.MCPToolResultBudgetBytes = 0
	cfg.MCPStallThreshold = 0
	cfg.MCPToolResultMaxCharsForModel = 32
	cfg.SkipWaits = true

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	mockClient := NewMockLLMClient([]MockResponse{
		{
			ToolCalls: []llms.ToolCall{
				{ID: "artifact-1", FunctionCall: &llms.FunctionCall{Name: "kubectl_get_pods", Arguments: `{"namespace":"default"}`}},
			},
		},
		{Content: "Final answer after artifact persistence"},
	})

	origExecute := executeMCPTool
	t.Cleanup(func() { executeMCPTool = origExecute })
	executeMCPTool = func(cfg *config.Config, toolName string, args map[string]any) (string, error) {
		return strings.Repeat("pod-data-line\n", 128), nil
	}

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	result, err := Chat(cfg, mockClient, "inspect pods", false, false)

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	io.ReadAll(rOut)
	io.ReadAll(rErr)

	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if !strings.Contains(result, "Final answer after artifact persistence") {
		t.Fatalf("unexpected final result: %q", result)
	}
	if len(cfg.SessionHistory) == 0 {
		t.Fatalf("expected session history to include tool call")
	}
	last := cfg.SessionHistory[len(cfg.SessionHistory)-1]
	if len(last.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call recorded, got %d", len(last.ToolCalls))
	}
	tc := last.ToolCalls[0]
	if strings.TrimSpace(tc.ArtifactPath) == "" {
		t.Fatalf("expected artifact path to be recorded, got empty")
	}
	if strings.TrimSpace(tc.ArtifactSHA256) == "" {
		t.Fatalf("expected artifact sha to be recorded, got empty")
	}
	if tc.ResultBytes <= cfg.MCPToolResultMaxCharsForModel {
		t.Fatalf("expected raw result bytes to exceed truncation threshold, got %d", tc.ResultBytes)
	}
	if !strings.Contains(tc.Result, "full tool output saved") {
		t.Fatalf("expected tool call result to mention artifact path, got %q", tc.Result)
	}
	if _, statErr := os.Stat(tc.ArtifactPath); statErr != nil {
		t.Fatalf("expected artifact file at %s, stat error: %v", tc.ArtifactPath, statErr)
	}
}

// Helper functions for test configuration

func CreateTestConfigWithHistory() *config.Config {
	cfg := CreateTestConfig()
	cfg.ChatMessages = []llms.ChatMessage{
		llms.HumanChatMessage{Content: "Tell me about Kubernetes pods"},
		llms.AIChatMessage{Content: "Kubernetes pods are the smallest deployable units..."},
	}
	return cfg
}

func CreateTestConfigWithRetries(retries int) *config.Config {
	cfg := CreateTestConfig()
	cfg.Retries = retries
	return cfg
}

func CreateTestConfigWithTokens(defaultMax, userMax, minOutput int) *config.Config {
	cfg := CreateTestConfig()
	cfg.DefaultMaxTokens = defaultMax
	cfg.UserMaxTokens = userMax
	cfg.MinOutputTokens = minOutput
	return cfg
}

// Benchmark tests for performance validation

func BenchmarkChat_SimpleRequest(b *testing.B) {
	cfg := CreateTestConfig()
	mockClient := NewMockLLMClient(TestScenarios{}.SimpleSuccess())

	// Suppress output for cleaner benchmarks
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mockClient.Reset()
		_, err := Chat(cfg, mockClient, "benchmark test", false, false)
		if err != nil {
			b.Fatalf("Unexpected error: %v", err)
		}
	}

	w.Close()
	os.Stderr = oldStderr
	io.ReadAll(r) // Discard output
}

func BenchmarkChat_WithHistory(b *testing.B) {
	cfg := CreateTestConfigWithHistory()

	// Add substantial history
	for i := 0; i < 50; i++ {
		cfg.ChatMessages = append(cfg.ChatMessages,
			llms.HumanChatMessage{Content: fmt.Sprintf("Question %d", i)},
			llms.AIChatMessage{Content: fmt.Sprintf("Answer %d", i)},
		)
	}

	mockClient := NewMockLLMClient(TestScenarios{}.SimpleSuccess())

	// Suppress output for cleaner benchmarks
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mockClient.Reset()
		_, err := Chat(cfg, mockClient, "benchmark with history", false, true)
		if err != nil {
			b.Fatalf("Unexpected error: %v", err)
		}
	}

	w.Close()
	os.Stderr = oldStderr
	io.ReadAll(r) // Discard output
}
