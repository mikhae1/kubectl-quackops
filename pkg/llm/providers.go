package llm

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/tmc/langchaingo/llms"
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

	if cfg.Provider == "deepseek" {
		llmOptions = append(llmOptions, openai.WithBaseURL(cfg.ApiURL))
	}

	// Create OpenAI client
	client, err := openai.New(llmOptions...)
	if err != nil {
		return "", fmt.Errorf("failed to create OpenAI client: %w", err)
	}

	return HandleLLMRequest(cfg, client, prompt, stream, history)
}

// anthropicRequestWithChat sends a request to Anthropic
func anthropicRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	// Create Anthropic client
	client, err := anthropic.New()
	if err != nil {
		return "", fmt.Errorf("failed to create Anthropic client: %w", err)
	}

	return HandleLLMRequest(cfg, client, prompt, stream, history)
}

// ollamaRequestWithChat sends a request to Ollama
func ollamaRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	// Make sure the API URL is properly formatted - it should not end with /api
	serverURL := strings.TrimSuffix(cfg.ApiURL, "/api")

	// Create Ollama client
	client, err := ollama.New(
		ollama.WithModel(cfg.Model),
		ollama.WithServerURL(serverURL),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Ollama client: %w", err)
	}

	return HandleLLMRequest(cfg, client, prompt, stream, history)
}

// googleRequestWithChat sends a request to Google AI
func googleRequestWithChat(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	// Create GoogleAI client
	ctx := context.Background()
	client, err := googleai.New(ctx,
		googleai.WithAPIKey(os.Getenv("GOOGLE_API_KEY")),
		googleai.WithDefaultModel(cfg.Model),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Google AI client: %w", err)
	}

	return HandleLLMRequest(cfg, client, prompt, stream, history)
}

// handleLLMRequestNoHistoryUpdate is a modified version of handleLLMRequest that doesn't add the human message
// because it's already added in googleRequestWithChat
func HandleLLMRequest(cfg *config.Config, client llms.Model, prompt string, stream bool, history bool) (string, error) {
	humanMessage := llms.HumanChatMessage{Content: prompt}
	if history {
		cfg.ChatMessages = append(cfg.ChatMessages, humanMessage)
	}

	// Convert chat messages to MessageContent format required by GenerateContent
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

	// Log initialization
	logger.Log("info", "Sending request to %s/%s with %d messages in history", cfg.Provider, cfg.Model, len(messages))

	// Prepare options for the LLM request
	generateOptions := []llms.CallOption{}

	// Add temperature if configured
	if cfg.Temperature > 0 {
		generateOptions = append(generateOptions, llms.WithTemperature(cfg.Temperature))
	}

	// Add max tokens using effective window to be friendly with providers like Gemini
	if cfg.MaxTokens > 0 {
		effective := lib.EffectiveMaxTokens(cfg)
		limit := cfg.MaxTokens
		if effective > 0 && cfg.MaxTokens > effective {
			limit = effective
		}
		generateOptions = append(generateOptions, llms.WithMaxTokens(limit))
	}

	// Compute outgoing tokens and start spinner with dynamic token display
	outgoingTokens := lib.CountTokensWithConfig(cfg, prompt, cfg.ChatMessages)
	cfg.LastOutgoingTokens = outgoingTokens
	cfg.LastIncomingTokens = 0

	// Initialize real-time token meter to track incoming tokens too
	tokenMeter := lib.NewTokenMeter(cfg, outgoingTokens)

	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Color("green", "bold")
	s.Writer = os.Stderr
	s.Suffix = fmt.Sprintf(" Waiting for %s/%s response...",
		cfg.Provider, cfg.Model)

	// Start spinner also for streaming; we'll stop it on the first chunk to avoid
	// interfering with streamed stdout output
	var stopOnce sync.Once
	if stream {
		s.Start()
	} else {
		s.Start()
		defer s.Stop()
	}

	// If streaming, refresh spinner suffix periodically to show live incoming tokens while waiting
	// We avoid printing the separate meter during the wait to keep UI compact.
	// No spinner updates during streaming to prevent overlap with stdout

	// Callback function for streaming
	var callbackFn func(ctx context.Context, chunk []byte) error
	var cleanupFn func()

	if stream {
		// Define action to perform on the very first streamed chunk: stop spinner and clear the line
		onFirstChunk := func() {
			stopOnce.Do(func() {
				s.Stop()
				// Clear spinner line from stderr and move output to a fresh stdout line
				fmt.Fprint(os.Stderr, "\r\033[2K")
				fmt.Fprint(os.Stdout, "\n")
			})
		}

		// Create streaming callback without live token meter rendering to avoid overlap
		callbackFn, cleanupFn = createStreamingCallback(cfg, s, nil, onFirstChunk)
		defer cleanupFn()

		// Ensure spinner is stopped even if we error before receiving any chunks
		defer func() {
			stopOnce.Do(func() {
				s.Stop()
				fmt.Fprint(os.Stderr, "\r\033[2K")
			})
		}()

		// Add streaming option
		generateOptions = append(generateOptions, llms.WithStreamingFunc(callbackFn))
	}

	// Use retry logic
	maxRetries := cfg.Retries
	backoffFactor := 3.0
	initialBackoff := 10.0 // seconds
	originalSuffix := s.Suffix
	var responseContent string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoffTime := initialBackoff * math.Pow(backoffFactor, float64(attempt-1))
			// Add some jitter to avoid thundering herd problem
			jitter := (0.5 + rand.Float64()) // 0.5-1.5
			sleepTime := time.Duration(backoffTime * jitter * float64(time.Second))
			retrySeconds := backoffTime * jitter

			logger.Log("info", "Retrying in %.2f seconds (attempt %d/%d)", retrySeconds, attempt, maxRetries)

			// Update spinner message to show retry status
			s.Suffix = fmt.Sprintf(" Retrying %s/%s... (attempt %d/%d)", cfg.Provider, cfg.Model, attempt, maxRetries)

			// Show countdown for the retry
			countdownStart := time.Now()
			for {
				elapsed := time.Since(countdownStart)
				remaining := sleepTime - elapsed
				if remaining <= 0 {
					break
				}

				s.Suffix = fmt.Sprintf(" Retrying %s/%s in %.1fs... (attempt %d/%d)",
					cfg.Provider, cfg.Model, remaining.Seconds(), attempt, maxRetries)
				time.Sleep(100 * time.Millisecond) // Update roughly 10 times per second
			}

			// Reset spinner message after retry sleep
			s.Suffix = originalSuffix
		}

		// Generate content using client and options
		if len(messages) == 0 {
			// Google AI requires at least one message, add a default human message
			messages = append(messages, llms.TextParts(llms.ChatMessageTypeHuman, prompt))
		}

		resp, err := client.GenerateContent(context.Background(), messages, generateOptions...)
		if err != nil {
			if attempt < maxRetries {
				// Print the error with color
				fmt.Printf("%s\n", color.RedString(err.Error()))
				continue
			}

			if attempt == maxRetries {
				return "", fmt.Errorf("AI still returning error after %d retries: %w", maxRetries, err)
			}
			return "", err
		}

		// Extract text from the response
		if resp != nil && len(resp.Choices) > 0 {
			responseContent = resp.Choices[0].Content
			// Update last incoming tokens to show in prompt after completion
			cfg.LastIncomingTokens = lib.EstimateTokens(cfg, responseContent)
		}
		break
	}

	// Add the response to the chat history only if history is enabled
	if history && responseContent != "" {
		cfg.ChatMessages = append(cfg.ChatMessages, llms.AIChatMessage{Content: responseContent})
	}

	// Extract response content
	if responseContent == "" {
		return "", fmt.Errorf("no content generated from %s", cfg.Provider)
	}

	// For non-streaming flows, update incoming tokens once at the end
	if !stream {
		tokenMeter.AddIncoming(lib.EstimateTokens(cfg, responseContent))
	}

	return responseContent, nil
}
