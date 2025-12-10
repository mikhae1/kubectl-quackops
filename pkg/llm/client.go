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

// RequestWithSystemFunc type for system-prompt-aware requests
type RequestWithSystemFunc func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error)

// Request sends a request to the LLM provider
var Request RequestFunc = func(cfg *config.Config, prompt string, stream bool, history bool) (string, error) {
	return RequestWithSystem(cfg, "", prompt, stream, history)
}

// RequestWithSystem sends a request with separate system and user prompts
var RequestWithSystem RequestWithSystemFunc = func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
	truncUserPrompt := userPrompt
	// Rude truncation of the prompt if it exceeds the maximum token length
	maxWin := lib.EffectiveMaxTokens(cfg)
	if len(truncUserPrompt) > maxWin*2 {
		truncUserPrompt = truncUserPrompt[:maxWin*2] + "..."
	}

	// Role-aware debug logging with token counts
	systemTok := lib.EstimateTokens(cfg, systemPrompt)
	userTok := lib.EstimateTokens(cfg, userPrompt)
	historyTok := lib.CountTokens("", cfg.ChatMessages)

	if systemPrompt != "" {
		logger.Log("debug", "[Request] Roles: system=%d tok, user=%d tok, history=%d msg (%d tok)",
			systemTok, userTok, len(cfg.ChatMessages), historyTok)
		systemPreview := summarizeSystemPrompt(systemPrompt, 120)
		logger.Log("llmIn", "[%s/%s] System (%d tok): %s", cfg.Provider, cfg.Model, systemTok, systemPreview)
		logger.Log("llmIn", "[%s/%s] User (%d tok): %s", cfg.Provider, cfg.Model, userTok, truncUserPrompt)
		// Full context debug logging (excluding history)
		logMultiline("llmSys", systemPrompt)
		logMultiline("llmUser", userPrompt)
	} else {
		logger.Log("debug", "[Request] Roles: user=%d tok, history=%d msg (%d tok)",
			userTok, len(cfg.ChatMessages), historyTok)
		logger.Log("llmIn", "[%s/%s] User (%d tok): %s", cfg.Provider, cfg.Model, userTok, truncUserPrompt)
		// Full user prompt debug logging
		logMultiline("llmUser", userPrompt)
	}
	logger.Log("llmIn", "History: %d messages, %d tokens", len(cfg.ChatMessages), historyTok)

	// Spinner lifecycle and throttling are managed inside Chat().

	var err error
	var answer string
	switch cfg.Provider {
	case "ollama":
		answer, err = ollamaRequestWithChatSystem(cfg, systemPrompt, truncUserPrompt, stream, history)
	case "openai":
		answer, err = openaiRequestWithChatSystem(cfg, systemPrompt, truncUserPrompt, stream, history)
	case "azopenai":
		answer, err = azOpenAIRequestWithChatSystem(cfg, systemPrompt, truncUserPrompt, stream, history)
	case "google":
		answer, err = googleRequestWithChatSystem(cfg, systemPrompt, truncUserPrompt, stream, history)
	case "anthropic":
		answer, err = anthropicRequestWithChatSystem(cfg, systemPrompt, truncUserPrompt, stream, history)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", cfg.Provider)
	}

	logger.Log("llmOut", "[%s@%s]: %s", cfg.Provider, cfg.Model, answer)
	return answer, err
}

// logMultiline logs each non-empty line of content
func logMultiline(level string, content string) {
	if content == "" {
		return
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			logger.Log(level, "%s", line)
		}
	}
}

// summarizeSystemPrompt extracts a meaningful preview from a system prompt
func summarizeSystemPrompt(systemPrompt string, maxLen int) string {
	if systemPrompt == "" {
		return "(empty)"
	}

	// Extract section headers (lines starting with ## or #)
	var headers []string
	lines := strings.Split(systemPrompt, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			headers = append(headers, strings.TrimPrefix(trimmed, "## "))
		} else if strings.HasPrefix(trimmed, "# ") {
			headers = append(headers, strings.TrimPrefix(trimmed, "# "))
		}
	}

	if len(headers) > 0 {
		summary := strings.Join(headers, " | ")
		if len(summary) > maxLen {
			return summary[:maxLen-3] + "..."
		}
		return summary
	}

	// Fallback: first meaningful line
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "```") {
			if len(trimmed) > maxLen {
				return trimmed[:maxLen-3] + "..."
			}
			return trimmed
		}
	}

	return "(system prompt)"
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
	if cfg == nil || chatMessages == nil || maxTokens <= 0 {
		return
	}

	// If the token length exceeds the context window, remove the oldest message in loop
	threadLen := lib.CountTokensWithConfig(cfg, "", chatMessages)
	if threadLen > maxTokens {
		logger.Log("warn", "Thread should be truncated: %d messages, %d tokens", len(chatMessages), threadLen)

		// Create spinner for history trimming using SpinnerManager
		spinnerManager := lib.GetSpinnerManager(cfg)
		cancelTrimSpinner := spinnerManager.ShowThrottle("✂️ "+config.Colors.Info.Render("Trimming")+" "+config.Colors.Dim.Render("conversation history..."), time.Second*2)
		defer cancelTrimSpinner()

		// Truncate the thread if it exceeds the maximum token length
		for lib.CountTokensWithConfig(cfg, "", chatMessages) > maxTokens && len(chatMessages) > 0 {
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

			logger.Log("info", "Thread after truncation: tokens: %d, messages: %v", lib.CountTokensWithConfig(cfg, "", chatMessages), len(chatMessages))
			// Brief pause to show spinner movement
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Persist any trimming back to the shared chat history
	cfg.ChatMessages = chatMessages

	logger.Log("info", "\nThread: %d messages, %d tokens", len(cfg.ChatMessages), lib.CountTokensWithConfig(cfg, "", cfg.ChatMessages))
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
