package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

func TestMockLLMClient_BasicFunctionality(t *testing.T) {
	// Test GenerateContent
	responses := []MockResponse{
		{
			Content:    "Hello, this is a mock response!",
			TokensUsed: 45,
		},
	}

	client := NewMockLLMClient(responses)
	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "Test prompt"),
	}

	result, err := client.GenerateContent(context.Background(), messages)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(result.Choices) == 0 {
		t.Fatalf("Expected at least one choice in response")
	}

	if result.Choices[0].Content != "Hello, this is a mock response!" {
		t.Errorf("Expected 'Hello, this is a mock response!', got '%s'", result.Choices[0].Content)
	}

	// Test Call method (legacy) with a fresh client
	callResponses := []MockResponse{
		{
			Content:    "Hello from Call method!",
			TokensUsed: 30,
		},
	}

	callClient := NewMockLLMClient(callResponses)
	callResult, err := callClient.Call(context.Background(), "Test call prompt")

	if err != nil {
		t.Fatalf("Unexpected error from Call: %v", err)
	}

	if callResult != "Hello from Call method!" {
		t.Errorf("Expected 'Hello from Call method!', got '%s'", callResult)
	}
}

func TestMockLLMClient_MultipleResponses(t *testing.T) {
	responses := []MockResponse{
		{Content: "First response", TokensUsed: 20},
		{Content: "Second response", TokensUsed: 25},
		{Content: "Third response", TokensUsed: 30},
	}

	client := NewMockLLMClient(responses)

	for i, expected := range []string{"First response", "Second response", "Third response"} {
		result, err := client.Call(context.Background(), "Test prompt "+string(rune(i+49)))
		if err != nil {
			t.Fatalf("Unexpected error on call %d: %v", i+1, err)
		}
		if result != expected {
			t.Errorf("Call %d: expected '%s', got '%s'", i+1, expected, result)
		}
	}

	// Fourth call should fail
	_, err := client.Call(context.Background(), "Should fail")
	if err == nil {
		t.Errorf("Expected error after exhausting mock responses")
	}
}

func TestMockLLMClient_ErrorResponse(t *testing.T) {
	responses := []MockResponse{
		{Error: fmt.Errorf("mock error occurred")},
	}

	client := NewMockLLMClient(responses)

	_, err := client.Call(context.Background(), "Test prompt")
	if err == nil {
		t.Errorf("Expected error but got none")
	}

	if !strings.Contains(err.Error(), "mock error occurred") {
		t.Errorf("Expected error message to contain 'mock error occurred', got: %s", err.Error())
	}
}

func TestMockLLMClient_CallHistory(t *testing.T) {
	responses := []MockResponse{
		{Content: "Response 1", TokensUsed: 20},
		{Content: "Response 2", TokensUsed: 25},
	}

	client := NewMockLLMClient(responses)

	// Make first call
	_, err := client.Call(context.Background(), "First prompt")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Make second call
	_, err = client.Call(context.Background(), "Second prompt")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Check call history
	history := client.GetCallHistory()
	if len(history) != 2 {
		t.Errorf("Expected 2 calls in history, got %d", len(history))
	}

	// Verify first call
	if len(history[0].Messages) == 0 {
		t.Errorf("Expected messages in first call history entry")
	}

	// Test reset functionality
	client.Reset()
	history = client.GetCallHistory()
	if len(history) != 0 {
		t.Errorf("Expected empty history after reset, got %d entries", len(history))
	}

	if client.currentResponse != 0 {
		t.Errorf("Expected currentResponse to be reset to 0, got %d", client.currentResponse)
	}
}

func TestMockRequestFunc_Integration(t *testing.T) {
	// Test that MockRequestFunc works correctly
	responses := []MockResponse{
		{Content: "Mock request response", TokensUsed: 40},
	}

	cfg := CreateTestConfig()
	mockRequestFunc := MockRequestFunc(responses)

	result, err := mockRequestFunc(cfg, "Test prompt", false, false)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result != "Mock request response" {
		t.Errorf("Expected 'Mock request response', got '%s'", result)
	}
}

func TestTestScenarios(t *testing.T) {
	// Test that our test scenario helpers work
	scenarios := TestScenarios{}

	// Test simple success
	simpleSuccess := scenarios.SimpleSuccess()
	if len(simpleSuccess) != 1 {
		t.Errorf("Expected 1 response in SimpleSuccess, got %d", len(simpleSuccess))
	}

	// Test streaming response
	streaming := scenarios.StreamingResponse()
	if len(streaming) != 1 {
		t.Errorf("Expected 1 response in StreamingResponse, got %d", len(streaming))
	}
	if len(streaming[0].StreamingChunks) == 0 {
		t.Errorf("Expected streaming chunks in StreamingResponse")
	}

	// Test error response
	errorResp := scenarios.ErrorResponse()
	if len(errorResp) != 1 {
		t.Errorf("Expected 1 response in ErrorResponse, got %d", len(errorResp))
	}
	if errorResp[0].Error == nil {
		t.Errorf("Expected error in ErrorResponse")
	}

	// Test multiple responses
	multiple := scenarios.MultipleResponses()
	if len(multiple) != 3 {
		t.Errorf("Expected 3 responses in MultipleResponses, got %d", len(multiple))
	}
}