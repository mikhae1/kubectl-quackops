package llm

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

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
