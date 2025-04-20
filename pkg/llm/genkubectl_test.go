package llm

import (
	"os"
	"regexp"
	"testing"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/tmc/langchaingo/llms"
)

func init() {
	// Initialize logger to avoid nil pointer dereference
	logger.InitLoggers(os.Stderr, 0)
}

// MockRequest simulates the Request function for testing
func mockRequest(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	// Add messages to chat history only if history is enabled
	if history {
		humanMessage := llms.HumanChatMessage{Content: prompt}
		cfg.ChatMessages = append(cfg.ChatMessages, humanMessage)
	}

	// Add a mock response
	mockResponse := "Here are some kubectl commands:\nkubectl get pods\nkubectl get services\nkubectl describe pod my-pod"

	// Add response to chat history only if history is enabled
	if history {
		cfg.ChatMessages = append(cfg.ChatMessages, llms.AIChatMessage{Content: mockResponse})
	}

	return mockResponse, nil
}

// TestGenKubectlCmdsDoesNotUpdateHistory tests that GenKubectlCmds doesn't add diagnostic
// messages to the chat history
func TestGenKubectlCmdsDoesNotUpdateHistory(t *testing.T) {
	// Save the original Request function
	originalRequest := Request
	// Replace with mock for testing
	Request = mockRequest
	// Restore the original function when test is done
	defer func() { Request = originalRequest }()

	// Create a config with initial chat messages
	cfg := &config.Config{
		ChatMessages: []llms.ChatMessage{
			llms.HumanChatMessage{Content: "Hello, I need help with my Kubernetes cluster"},
			llms.AIChatMessage{Content: "I'll help you with your Kubernetes cluster. What seems to be the issue?"},
		},
		AllowedKubectlCmds:  []string{"get", "describe"},
		KubectlStartPrompt:  "You are a Kubernetes expert",
		KubectlShortPrompt:  "Generate kubectl commands",
		KubectlFormatPrompt: "Format nicely",
		SpinnerTimeout:      80,
		// Add default values for required fields that are used in GenKubectlCmds
		KubectlPrompts: []config.KubectlPrompt{
			{
				MatchRe:         regexp.MustCompile("pod|pods"),
				Prompt:          "Focus on pod-related issues",
				AllowedKubectls: []string{"get pods", "describe pod"},
				UseDefaultCmds:  true,
			},
		},
	}

	// Record the initial number of messages
	initialMessageCount := len(cfg.ChatMessages)

	// Call the function being tested
	_, err := GenKubectlCmds(cfg, "I need to check my pods and services", 1)
	if err != nil {
		t.Fatalf("GenKubectlCmds returned an error: %v", err)
	}

	// Verify that chat history wasn't modified
	if len(cfg.ChatMessages) != initialMessageCount {
		t.Errorf("Chat history was modified. Expected %d messages, got %d",
			initialMessageCount, len(cfg.ChatMessages))
	}

	// Additional verification - make sure the last message is still the original AI response
	lastMessage := cfg.ChatMessages[len(cfg.ChatMessages)-1].GetContent()
	expectedLastMessage := "I'll help you with your Kubernetes cluster. What seems to be the issue?"
	if lastMessage != expectedLastMessage {
		t.Errorf("Last message in chat history changed. Expected: %q, got: %q",
			expectedLastMessage, lastMessage)
	}
}
