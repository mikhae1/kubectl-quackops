package llm

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mikhae1/kubectl-quackops/pkg/animator"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/formatter"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/tmc/langchaingo/llms"
)

// Define RequestFunc type for easier mocking in tests
type RequestFunc func(cfg *config.Config, prompt string, stream bool, history bool) (string, error)

// Request sends a request to the LLM provider
var Request RequestFunc = func(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	truncPrompt := prompt
	// Rude truncation of the prompt if it exceeds the maximum token length
	if len(truncPrompt) > cfg.MaxTokens*2 {
		truncPrompt = truncPrompt[:cfg.MaxTokens*2] + "..."
	}

	logger.Log("llmIn", "[%s/%s]: %s", cfg.Provider, cfg.Model, truncPrompt)
	logger.Log("llmIn", "History: %v messages, %d tokens", len(cfg.ChatMessages), lib.CountTokens("", cfg.ChatMessages))

	// Create a spinner for LLM response
	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Suffix = fmt.Sprintf(" Waiting for %s/%s response...", cfg.Provider, cfg.Model)
	s.Color("green", "bold")

	// Start spinner if not streaming (for streaming, we'll show the output directly)
	if !stream {
		s.Start()
		defer s.Stop()
	}

	var err error
	var answer string
	switch cfg.Provider {
	case "ollama":
		answer, err = ollamaRequestWithChat(cfg, truncPrompt, stream, history)
	case "openai", "deepseek":
		answer, err = openaiRequestWithChat(cfg, truncPrompt, stream, history)
	case "google":
		answer, err = googleRequestWithChat(cfg, truncPrompt, stream, history)
	case "anthropic":
		answer, err = anthropicRequestWithChat(cfg, truncPrompt, stream, history)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", cfg.Provider)
	}

	logger.Log("llmOut", "[%s@%s]: %s", cfg.Provider, cfg.Model, answer)
	return answer, err
}

// Helper function to check if a prompt exists in chat history
func PromptExistsInHistory(messages []llms.ChatMessage, prompt string) bool {
	for _, msg := range messages {
		if strings.Contains(msg.GetContent(), prompt) {
			return true
		}
	}
	return false
}

// ManageChatThreadContext manages the context window of the chat thread
func ManageChatThreadContext(chatMessages []llms.ChatMessage, maxTokens int) {
	if chatMessages == nil {
		return
	}

	// If the token length exceeds the context window, remove the oldest message in loop
	threadLen := lib.CountTokens("", chatMessages)
	if threadLen > maxTokens {
		logger.Log("warn", "Thread should be truncated: %d messages, %d tokens", len(chatMessages), threadLen)

		// Create a spinner for history trimming
		s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
		s.Suffix = " Trimming conversation history..."
		s.Color("yellow", "bold")
		s.Start()
		defer s.Stop()

		// Truncate the thread if it exceeds the maximum token length
		for lib.CountTokens("", chatMessages) > maxTokens && len(chatMessages) > 0 {
			// Remove the most irrelevant message: find oldest AI answer and remove it
			foundAIMessage := false
			for i, message := range chatMessages {
				if message.GetType() == llms.ChatMessageTypeAI {
					chatMessages = append(chatMessages[:i], chatMessages[i+1:]...)
					foundAIMessage = true
					break
				}
			}

			// Fallback: if no AI message found, remove the oldest message
			if !foundAIMessage {
				chatMessages = chatMessages[1:]
			}

			logger.Log("info", "Thread after truncation: tokens: %d, messages: %v", lib.CountTokens("", chatMessages), len(chatMessages))
			// Brief pause to show spinner movement
			time.Sleep(50 * time.Millisecond)
		}
	}

	logger.Log("info", "\nThread: %d messages, %d tokens", len(chatMessages), lib.CountTokens("", chatMessages))
}

// createStreamingCallback creates a callback function for streaming LLM responses with optional Markdown formatting
func createStreamingCallback(cfg *config.Config, spinner *spinner.Spinner) (func(ctx context.Context, chunk []byte) error, func()) {
	var spinnerStopped sync.Once
	var mdWriter *formatter.StreamingWriter
	var animWriter *animator.TypewriterWriter

	// Create writers based on configuration
	var outputWriter io.Writer = os.Stdout

	// Add typewriter animation if enabled
	if !cfg.DisableAnimation {
		animWriter = animator.NewTypewriterWriter(outputWriter)
		outputWriter = animWriter
	}

	// Add markdown formatting if enabled
	if !cfg.DisableMarkdownFormat {
		// Create a streaming writer that formats Markdown
		mdWriter = formatter.NewStreamingWriter(outputWriter)
	}

	// Create cleanup function for the writers
	cleanup := func() {
		if mdWriter != nil {
			if err := mdWriter.Flush(); err != nil {
				logger.Log("err", "Error flushing markdown writer: %v", err)
			}
			if err := mdWriter.Close(); err != nil {
				logger.Log("err", "Error closing markdown writer: %v", err)
			}
		}

		if animWriter != nil {
			if err := animWriter.Flush(); err != nil {
				logger.Log("err", "Error flushing animator writer: %v", err)
			}
			if err := animWriter.Close(); err != nil {
				logger.Log("err", "Error closing animator writer: %v", err)
			}
		}

		fmt.Println()
	}

	// Callback function for processing chunks
	callback := func(ctx context.Context, chunk []byte) error {
		// Stop spinner on first chunk
		spinnerStopped.Do(func() {
			spinner.Stop()
			fmt.Print("\r") // Clear the line
		})

		// Process the chunk with Markdown formatting if enabled
		if !cfg.DisableMarkdownFormat && mdWriter != nil {
			_, err := mdWriter.Write(chunk)
			return err
		}

		// If Markdown is disabled but animation is enabled
		if cfg.DisableMarkdownFormat && !cfg.DisableAnimation && animWriter != nil {
			_, err := animWriter.Write(chunk)
			return err
		}

		// Default: write the chunk directly to stdout
		fmt.Print(string(chunk))
		return nil
	}

	return callback, cleanup
}
