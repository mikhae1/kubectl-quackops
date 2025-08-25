package llm

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// openaiRequestWithChat sends a request to OpenAI or compatible provider
func openaiRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) { // Set OpenAI client options
	llmOptions := []openai.Option{
		openai.WithModel(cfg.Model),
	}

	// Support custom OpenAI-compatible base URL
	if baseURL := config.GetOpenAIBaseURL(); baseURL != "" {
		llmOptions = append(llmOptions, openai.WithBaseURL(baseURL))
		// Disable streaming for custom base URLs to improve compatibility with non-standard SSE implementations
		stream = false
	}

	// Create OpenAI client
	client, err := openai.New(llmOptions...)
	if err != nil {
		return "", fmt.Errorf("failed to create OpenAI client: %w", err)
	}

	return Chat(cfg, client, prompt, stream, history)
}

// anthropicRequestWithChat sends a request to Anthropic
func anthropicRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	// Create Anthropic client
	client, err := anthropic.New()
	if err != nil {
		return "", fmt.Errorf("failed to create Anthropic client: %w", err)
	}

	return Chat(cfg, client, prompt, stream, history)
}

// ollamaRequestWithChat sends a request to Ollama
func ollamaRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	// Ensure API URL does not end with /api
	serverURL := strings.TrimSuffix(cfg.OllamaApiURL, "/api")

	// Create Ollama client
	client, err := ollama.New(
		ollama.WithModel(cfg.Model),
		ollama.WithServerURL(serverURL),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Ollama client: %w", err)
	}

	return Chat(cfg, client, prompt, stream, history)
}

// googleRequestWithChat sends a request to Google AI
func googleRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	// Prefer custom client that builds genai schemas with Items for arrays
	ctx := context.Background()
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey != "" {
		if custom, err := New(ctx, apiKey, cfg.Model); err == nil {
			return Chat(cfg, custom, prompt, stream, history)
		}
	}
	// Fallback to stock googleai client
	client, err := googleai.New(ctx,
		googleai.WithAPIKey(apiKey),
		googleai.WithDefaultModel(cfg.Model),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Google AI client: %w", err)
	}
	return Chat(cfg, client, prompt, stream, history)
}
