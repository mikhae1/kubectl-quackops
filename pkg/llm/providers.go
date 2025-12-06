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
func openaiRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	return openaiRequestWithChatSystem(cfg, "", prompt, stream, history)
}

// openaiRequestWithChatSystem sends a request to OpenAI with separate system/user prompts
func openaiRequestWithChatSystem(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
	llmOptions := []openai.Option{
		openai.WithModel(cfg.Model),
	}

	// Support custom OpenAI-compatible base URL
	if baseURL := config.GetOpenAIBaseURL(); baseURL != "" {
		llmOptions = append(llmOptions, openai.WithBaseURL(baseURL))
		// Only disable streaming for known problematic endpoints
		if strings.Contains(baseURL, "openrouter.ai") {
			stream = false
		}
	}

	client, err := openai.New(llmOptions...)
	if err != nil {
		return "", fmt.Errorf("failed to create OpenAI client: %w", err)
	}

	return ChatWithSystemPrompt(cfg, client, systemPrompt, userPrompt, stream, history)
}

// azOpenAIRequestWithChat sends a request to Azure OpenAI
func azOpenAIRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	return azOpenAIRequestWithChatSystem(cfg, "", prompt, stream, history)
}

// azOpenAIRequestWithChatSystem sends a request to Azure OpenAI with separate system/user prompts
func azOpenAIRequestWithChatSystem(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
	llmOptions := []openai.Option{
		openai.WithAPIType(openai.APITypeAzure),
		openai.WithModel(cfg.Model),
		openai.WithAPIVersion(cfg.AzOpenAIAPIVersion),
	}

	if baseURL := config.GetAzOpenAIBaseURL(); baseURL != "" {
		llmOptions = append(llmOptions, openai.WithBaseURL(baseURL))
	}

	if apiKey := config.GetAzOpenAIAPIKey(); apiKey != "" {
		llmOptions = append(llmOptions, openai.WithToken(apiKey))
	}

	if cfg.EmbeddingModel != "" {
		llmOptions = append(llmOptions, openai.WithEmbeddingModel(cfg.EmbeddingModel))
	}

	client, err := openai.New(llmOptions...)
	if err != nil {
		return "", fmt.Errorf("failed to create Azure OpenAI client: %w", err)
	}

	return ChatWithSystemPrompt(cfg, client, systemPrompt, userPrompt, stream, history)
}

// anthropicRequestWithChat sends a request to Anthropic
func anthropicRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	return anthropicRequestWithChatSystem(cfg, "", prompt, stream, history)
}

// anthropicRequestWithChatSystem sends a request to Anthropic with separate system/user prompts
func anthropicRequestWithChatSystem(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
	client, err := anthropic.New()
	if err != nil {
		return "", fmt.Errorf("failed to create Anthropic client: %w", err)
	}

	return ChatWithSystemPrompt(cfg, client, systemPrompt, userPrompt, stream, history)
}

// ollamaRequestWithChat sends a request to Ollama
func ollamaRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	return ollamaRequestWithChatSystem(cfg, "", prompt, stream, history)
}

// ollamaRequestWithChatSystem sends a request to Ollama with separate system/user prompts
func ollamaRequestWithChatSystem(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
	serverURL := strings.TrimSuffix(cfg.OllamaApiURL, "/api")

	client, err := ollama.New(
		ollama.WithModel(cfg.Model),
		ollama.WithServerURL(serverURL),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Ollama client: %w", err)
	}

	return ChatWithSystemPrompt(cfg, client, systemPrompt, userPrompt, stream, history)
}

// googleRequestWithChat sends a request to Google AI
func googleRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	return googleRequestWithChatSystem(cfg, "", prompt, stream, history)
}

// googleRequestWithChatSystem sends a request to Google AI with separate system/user prompts
func googleRequestWithChatSystem(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
	ctx := context.Background()
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey != "" {
		if custom, err := New(ctx, apiKey, cfg.Model); err == nil {
			return ChatWithSystemPrompt(cfg, custom, systemPrompt, userPrompt, stream, history)
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
	return ChatWithSystemPrompt(cfg, client, systemPrompt, userPrompt, stream, history)
}
