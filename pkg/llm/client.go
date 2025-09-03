package llm

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
	maxWin := lib.EffectiveMaxTokens(cfg)
	if len(truncPrompt) > maxWin*2 {
		truncPrompt = truncPrompt[:maxWin*2] + "..."
	}

	logger.Log("llmIn", "[%s/%s]: %s", cfg.Provider, cfg.Model, truncPrompt)
	logger.Log("llmIn", "History: %v messages, %d tokens", len(cfg.ChatMessages), lib.CountTokens("", cfg.ChatMessages))

	// Spinner lifecycle and throttling are managed inside Chat().

	var err error
	var answer string
	switch cfg.Provider {
	case "ollama":
		answer, err = ollamaRequestWithChat(cfg, truncPrompt, stream, history)
	case "openai":
		answer, err = openaiRequestWithChat(cfg, truncPrompt, stream, history)
	case "azopenai":
		answer, err = azOpenAIRequestWithChat(cfg, truncPrompt, stream, history)
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
func ManageChatThreadContext(cfg *config.Config, chatMessages []llms.ChatMessage, maxTokens int) {
	if chatMessages == nil {
		return
	}

	// If the token length exceeds the context window, remove the oldest message in loop
	threadLen := lib.CountTokens("", chatMessages)
	if threadLen > maxTokens {
		logger.Log("warn", "Thread should be truncated: %d messages, %d tokens", len(chatMessages), threadLen)

		// Create spinner for history trimming using SpinnerManager
		spinnerManager := lib.GetSpinnerManager(cfg)
		cancelTrimSpinner := spinnerManager.ShowThrottle("✂️ "+config.Colors.Info.Sprint("Trimming")+" "+config.Colors.Dim.Sprint("conversation history..."), time.Second*2)
		defer cancelTrimSpinner()

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

// WriteNormalizedChunk writes chunk to the provided writer while normalizing line breaks.
// It ensures that intermediate newlines are written as CRLF (\r\n) to keep rendering consistent
// across different terminal environments, while avoiding an extra newline at the end.
func WriteNormalizedChunk(w io.Writer, chunk []byte) error {
	if w == nil || len(chunk) == 0 {
		return nil
	}

	chunkStr := string(chunk)
	if strings.Contains(chunkStr, "\n") {
		lines := strings.Split(chunkStr, "\n")
		for i, line := range lines {
			if i < len(lines)-1 {
				if _, err := w.Write([]byte(line + "\r\n")); err != nil {
					return err
				}
			} else {
				if _, err := w.Write([]byte(line)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	_, err := w.Write(chunk)
	return err
}

// CreateStreamingCallback creates a callback function for streaming LLM responses with optional Markdown formatting
func CreateStreamingCallback(cfg *config.Config, spinnerManager *lib.SpinnerManager, meter *lib.TokenMeter, onFirstChunk func()) (func(ctx context.Context, chunk []byte) error, func()) {
	var meterTicker *time.Ticker
	var meterDone chan struct{}
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
		outputWriter = mdWriter
	}

	// Create cleanup function for the writers
	cleanup := func() {
		if meterTicker != nil {
			close(meterDone)
			meterTicker.Stop()
		}
		if meter != nil {
			meter.Clear()
		}
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
		// For streaming mode we do not use a spinner at all
		if onFirstChunk != nil {
			onFirstChunk()
		}
		// Token meter disabled during streaming to prevent ANSI interference

		// Accumulate incoming tokens silently; if chunk is code block or markdown fence, still count
		if meter != nil && len(chunk) > 0 {
			delta := lib.EstimateTokens(cfg, string(chunk))
			meter.AddIncomingSilent(delta)
			// Keep prompt's last incoming tokens updated live
			cfg.LastIncomingTokens += delta
		}

		// Unified write path using the final outputWriter pipeline
		return WriteNormalizedChunk(outputWriter, chunk)
	}

	return callback, cleanup
}

// RequestSilent suppresses any printing while returning the model's response
func RequestSilent(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	// Force non-streaming and temporarily disable markdown formatting to avoid writes
	origDisableMD := cfg.DisableMarkdownFormat
	cfg.DisableMarkdownFormat = true
	origSuppressContentPrint := cfg.SuppressContentPrint
	cfg.SuppressContentPrint = true
	defer func() { cfg.DisableMarkdownFormat = origDisableMD; cfg.SuppressContentPrint = origSuppressContentPrint }()

	// Run normal request path with stream=false (no streaming callback prints)
	return Request(cfg, prompt, false, history)
}
