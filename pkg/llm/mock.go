package llm

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/tmc/langchaingo/llms"
)

// MockResponse represents a predefined response for testing
type MockResponse struct {
	Content           string
	Error             error
	StreamingChunks   []string
	StreamingDelay    time.Duration
	ToolCalls         []llms.ToolCall
	TokensUsed        int
	SimulateRateLimit bool
}

// MockLLMClient implements llms.Model interface for testing
type MockLLMClient struct {
	responses       []MockResponse
	currentResponse int
	streaming       bool
	callHistory     []MockCallRecord
}

// MockCallRecord tracks calls made to the mock client
type MockCallRecord struct {
	Messages []llms.MessageContent
	Options  []llms.CallOption
	Stream   bool
}

// NewMockLLMClient creates a new mock LLM client
func NewMockLLMClient(responses []MockResponse) *MockLLMClient {
	return &MockLLMClient{
		responses:       responses,
		currentResponse: 0,
		callHistory:     make([]MockCallRecord, 0),
	}
}

// Call implements llms.Model interface (legacy method)
func (m *MockLLMClient) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	// Simple implementation for compatibility
	messages := []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, prompt)}
	resp, err := m.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response choices")
	}
	return resp.Choices[0].Content, nil
}

// GenerateContent implements llms.Model interface
func (m *MockLLMClient) GenerateContent(ctx context.Context, messages []llms.MessageContent, options ...llms.CallOption) (*llms.ContentResponse, error) {
	// Determine if a streaming option was provided (best-effort heuristic)
	hasStreamingOpt := false
	for _, opt := range options {
		if opt == nil {
			continue
		}
		typeName := fmt.Sprintf("%T", opt)
		if strings.Contains(typeName, "WithStreamingFunc") {
			hasStreamingOpt = true
			break
		}
	}

	if m.currentResponse >= len(m.responses) {
		return nil, fmt.Errorf("no more mock responses available")
	}

	response := m.responses[m.currentResponse]
	m.currentResponse++

	// Decide streaming flag: either streaming option provided or response has streaming chunks
	streamFlag := hasStreamingOpt || len(response.StreamingChunks) > 0

	// Record the call with computed stream flag
	m.callHistory = append(m.callHistory, MockCallRecord{
		Messages: messages,
		Options:  options,
		Stream:   streamFlag,
	})

	// Simulate rate limiting
	if response.SimulateRateLimit {
		return nil, fmt.Errorf("rate limit exceeded (429): please try again later")
	}

	// Return error if specified
	if response.Error != nil {
		return nil, response.Error
	}

	// Handle streaming
	if streamFlag && len(response.StreamingChunks) > 0 {
		// Simulate streaming by processing chunks
		for range response.StreamingChunks {
			if response.StreamingDelay > 0 {
				time.Sleep(response.StreamingDelay)
			}
			// Streaming callback would be invoked here in a full implementation
		}
	}

	// Build response
	contentResponse := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content:   response.Content,
				ToolCalls: response.ToolCalls,
			},
		},
	}

	return contentResponse, nil
}

// GetCallHistory returns the history of calls made to this mock client
func (m *MockLLMClient) GetCallHistory() []MockCallRecord {
	return m.callHistory
}

// Reset resets the mock client state
func (m *MockLLMClient) Reset() {
	m.currentResponse = 0
	m.callHistory = make([]MockCallRecord, 0)
}

// MockRequestFunc creates a mock version of the Request function
func MockRequestFunc(responses []MockResponse) RequestFunc {
	mockWithSystem := MockRequestWithSystemFunc(responses)
	return func(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
		return mockWithSystem(cfg, "", prompt, stream, history)
	}
}

// MockRequestWithSystemFunc creates a mock version of the RequestWithSystem function
func MockRequestWithSystemFunc(responses []MockResponse) RequestWithSystemFunc {
	currentIndex := 0
	return func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
		// If we run out of responses, cycle back to the first one
		if currentIndex >= len(responses) {
			currentIndex = 0
		}

		if len(responses) == 0 {
			return "Mock response", nil
		}

		response := responses[currentIndex]
		currentIndex++

		// Simulate rate limiting
		if response.SimulateRateLimit {
			return "", fmt.Errorf("rate limit exceeded (429): please try again later")
		}

		// Return error if specified
		if response.Error != nil {
			return "", response.Error
		}

		// Simulate streaming by printing chunks when requested
		if stream && len(response.StreamingChunks) > 0 {
			for _, chunk := range response.StreamingChunks {
				fmt.Fprint(os.Stdout, chunk)
				if response.StreamingDelay > 0 {
					time.Sleep(response.StreamingDelay)
				}
			}
		}

		// Print final content to stdout (tests capture this)
		if response.Content != "" {
			fmt.Fprintln(os.Stdout, response.Content)
		}

		// Update history if requested
		if history {
			if systemPrompt != "" && len(cfg.ChatMessages) == 0 {
				cfg.ChatMessages = append(cfg.ChatMessages, llms.SystemChatMessage{Content: systemPrompt})
			}
			cfg.ChatMessages = append(cfg.ChatMessages, llms.HumanChatMessage{Content: userPrompt})
			cfg.ChatMessages = append(cfg.ChatMessages, llms.AIChatMessage{Content: response.Content})
		}

		return response.Content, nil
	}
}

// MockRequestFuncWithRetries creates a mock that simulates retry behavior within a single request
// This is useful for testing retry logic where multiple responses represent retries within one call
func MockRequestFuncWithRetries(responses []MockResponse) RequestFunc {
	return func(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
		// Simulate retry behavior within a single request using the provided sequence
		var finalContent string
		var lastErr error
		for i := 0; i < len(responses); i++ {
			r := responses[i]
			if r.SimulateRateLimit {
				lastErr = fmt.Errorf("rate limit exceeded (429): please try again later")
				continue
			}
			if r.Error != nil {
				lastErr = r.Error
				continue
			}
			finalContent = r.Content
			break
		}
		if finalContent == "" && lastErr != nil {
			return "", lastErr
		}
		if finalContent == "" {
			finalContent = responses[len(responses)-1].Content
		}

		// Simulate streaming by printing chunks when requested
		if stream && len(responses) > 0 {
			lastResponse := responses[len(responses)-1]
			for _, chunk := range lastResponse.StreamingChunks {
				fmt.Fprint(os.Stdout, chunk)
				if lastResponse.StreamingDelay > 0 {
					time.Sleep(lastResponse.StreamingDelay)
				}
			}
		}

		// Print final content to stdout (tests capture this)
		if finalContent != "" {
			fmt.Fprintln(os.Stdout, finalContent)
		}

		// Update history if requested
		if history {
			cfg.ChatMessages = append(cfg.ChatMessages, llms.HumanChatMessage{Content: prompt})
			cfg.ChatMessages = append(cfg.ChatMessages, llms.AIChatMessage{Content: finalContent})
		}

		return finalContent, nil
	}
}

// TestScenarios provides common test scenarios
type TestScenarios struct{}

func (TestScenarios) SimpleSuccess() []MockResponse {
	return []MockResponse{
		{
			Content:    "This is a successful response from the mock LLM.",
			TokensUsed: 50,
		},
	}
}

func (TestScenarios) StreamingResponse() []MockResponse {
	return []MockResponse{
		{
			Content: "This is a streaming response.",
			StreamingChunks: []string{
				"This is ",
				"a streaming ",
				"response.",
			},
			StreamingDelay: 100 * time.Millisecond,
			TokensUsed:     25,
		},
	}
}

func (TestScenarios) ErrorResponse() []MockResponse {
	return []MockResponse{
		{
			Error: fmt.Errorf("mock LLM error"),
		},
	}
}

func (TestScenarios) RateLimitResponse() []MockResponse {
	return []MockResponse{
		{
			SimulateRateLimit: true,
		},
	}
}

func (TestScenarios) MultipleResponses() []MockResponse {
	return []MockResponse{
		{
			Content:    "First response",
			TokensUsed: 30,
		},
		{
			Content:    "Second response",
			TokensUsed: 40,
		},
		{
			Content:    "Third response",
			TokensUsed: 35,
		},
	}
}

func (TestScenarios) WithToolCalls() []MockResponse {
	return []MockResponse{
		{
			Content: "I need to call a tool.",
			ToolCalls: []llms.ToolCall{
				{
					ID: "tool_call_1",
					FunctionCall: &llms.FunctionCall{
						Name:      "kubectl_get_pods",
						Arguments: `{"namespace": "default"}`,
					},
				},
			},
			TokensUsed: 60,
		},
	}
}

func (TestScenarios) ConversationFlow() []MockResponse {
	return []MockResponse{
		{
			Content:    "Hello! How can I help you with your Kubernetes cluster?",
			TokensUsed: 45,
		},
		{
			Content:    "Let me check your pods. I can see several pods running.",
			TokensUsed: 55,
		},
		{
			Content:    "The issue appears to be with your nginx deployment. Here's what I found...",
			TokensUsed: 70,
		},
	}
}

// MockProvider creates a mock provider function
func MockProvider(responses []MockResponse) func(*config.Config, string, bool, bool) (string, error) {
	mockFunc := MockRequestFunc(responses)
	return func(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
		return mockFunc(cfg, prompt, stream, history)
	}
}

// TestOutputCapture captures stdout/stderr for testing
type TestOutputCapture struct {
	stdout *strings.Builder
	stderr *strings.Builder
	oldOut io.Writer
	oldErr io.Writer
}

func NewTestOutputCapture() *TestOutputCapture {
	return &TestOutputCapture{
		stdout: &strings.Builder{},
		stderr: &strings.Builder{},
	}
}

func (c *TestOutputCapture) Start() {
	// Note: This is a simplified capture - in real tests we'd need to intercept os.Stdout/Stderr
	// For now, we'll rely on the test framework to handle this
}

func (c *TestOutputCapture) Stop() {
	// Restore original outputs
}

func (c *TestOutputCapture) GetStdout() string {
	return c.stdout.String()
}

func (c *TestOutputCapture) GetStderr() string {
	return c.stderr.String()
}

// TestConfig creates test configurations
func CreateTestConfig() *config.Config {
	return &config.Config{
		Provider:                  "openai",
		Model:                     "gpt-4",
		ChatMessages:              []llms.ChatMessage{},
		SafeMode:                  false,
		Verbose:                   false,
		DisableMarkdownFormat:     false,
		DisableAnimation:          true, // Disable for predictable testing
		Retries:                   3,
		Timeout:                   30,
		DefaultMaxTokens:          4096,
		UserMaxTokens:             0,
		SpinnerTimeout:            300,
		ThrottleRequestsPerMinute: 0, // Disable throttling for tests
		InputTokenReservePercent:  20,
		MinInputTokenReserve:      1024,
		MinOutputTokens:           512,
		MCPClientEnabled:          false, // Disable MCP for basic tests
		SkipWaits:                 true,  // Skip delays in tests
		LastOutgoingTokens:        0,
		LastIncomingTokens:        0,
	}
}

// TestConfigWithOptions allows customizing test config
func CreateTestConfigWithOptions(options map[string]interface{}) *config.Config {
	cfg := CreateTestConfig()

	for key, value := range options {
		switch key {
		case "verbose":
			cfg.Verbose = value.(bool)
		case "streaming":
			// This would be handled in the test logic
		case "interactive":
			// This would be handled in the test logic
		case "provider":
			cfg.Provider = value.(string)
		case "model":
			cfg.Model = value.(string)
		case "mcp_enabled":
			cfg.MCPClientEnabled = value.(bool)
		case "safe_mode":
			cfg.SafeMode = value.(bool)
		case "markdown_disabled":
			cfg.DisableMarkdownFormat = value.(bool)
		case "animation_disabled":
			cfg.DisableAnimation = value.(bool)
		}
	}

	return cfg
}

// Helper functions for tests

// AssertContains checks if a string contains a substring
func AssertContains(t interface{}, text, substr string) {
	if !strings.Contains(text, substr) {
		// In real implementation, we'd use t.Errorf
		panic(fmt.Sprintf("Expected '%s' to contain '%s'", text, substr))
	}
}

// AssertNotContains checks if a string does not contain a substring
func AssertNotContains(t interface{}, text, substr string) {
	if strings.Contains(text, substr) {
		// In real implementation, we'd use t.Errorf
		panic(fmt.Sprintf("Expected '%s' to not contain '%s'", text, substr))
	}
}

// AssertEqual checks if two values are equal
func AssertEqual(t interface{}, expected, actual interface{}) {
	if expected != actual {
		panic(fmt.Sprintf("Expected '%v', got '%v'", expected, actual))
	}
}
