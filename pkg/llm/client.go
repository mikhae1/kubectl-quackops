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

const autoCompactSummaryHeader = "## Compact Memory"

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

	// Prefer summarization-based compaction as the thread approaches the context limit.
	threadLen := lib.CountTokensWithConfig(cfg, "", chatMessages)
	if cfg.AutoCompactEnabled {
		triggerPercent := cfg.AutoCompactTriggerPercent
		if triggerPercent <= 0 || triggerPercent > 100 {
			triggerPercent = 95
		}
		triggerTokens := int(float64(maxTokens) * float64(triggerPercent) / 100.0)
		if triggerTokens < 1 {
			triggerTokens = maxTokens
		}

		if threadLen >= triggerTokens {
			logger.Log("info", "Auto-compact triggered: %d tokens >= %d token threshold", threadLen, triggerTokens)

			targetPercent := cfg.AutoCompactTargetPercent
			if targetPercent <= 0 || targetPercent >= triggerPercent {
				targetPercent = 60
			}
			targetTokens := int(float64(maxTokens) * float64(targetPercent) / 100.0)
			if targetTokens < 1 {
				targetTokens = maxTokens / 2
			}
			keepMessages := cfg.AutoCompactKeepMessages
			if keepMessages < 0 {
				keepMessages = 8
			}

			spinnerManager := lib.GetSpinnerManager(cfg)
			cancelCompactSpinner := spinnerManager.ShowThrottle("🧠 "+config.Colors.Info.Sprint("Compacting")+" "+config.Colors.Dim.Sprint("conversation history..."), time.Second*2)
			compacted, compactedOK := compactChatHistory(cfg, chatMessages, targetTokens, keepMessages)
			cancelCompactSpinner()
			if compactedOK {
				chatMessages = compacted
				threadLen = lib.CountTokensWithConfig(cfg, "", chatMessages)
				logger.Log("info", "Auto-compact complete: %d messages, %d tokens", len(chatMessages), threadLen)

				if threadLen > maxTokens {
					logger.Log("warn", "Auto-compact still above context limit (%d > %d); retrying with aggressive compaction", threadLen, maxTokens)
					cancelAggressiveSpinner := spinnerManager.ShowThrottle("🧠 "+config.Colors.Info.Sprint("Compacting")+" "+config.Colors.Dim.Sprint("more aggressively..."), time.Second*2)
					aggressive, aggressiveOK := compactChatHistory(cfg, chatMessages, maxTokens, 0)
					cancelAggressiveSpinner()
					if aggressiveOK {
						chatMessages = aggressive
						threadLen = lib.CountTokensWithConfig(cfg, "", chatMessages)
						logger.Log("info", "Aggressive auto-compact complete: %d messages, %d tokens", len(chatMessages), threadLen)
					}
				}
			} else {
				logger.Log("warn", "Auto-compact attempt failed; falling back to trimming if still over context limit")
			}
		}
	}

	// Fallback trim if context still exceeds max tokens.
	threadLen = lib.CountTokensWithConfig(cfg, "", chatMessages)
	if threadLen > maxTokens {
		logger.Log("warn", "Thread should be truncated: %d messages, %d tokens", len(chatMessages), threadLen)

		// Create spinner for history trimming using SpinnerManager
		spinnerManager := lib.GetSpinnerManager(cfg)
		cancelTrimSpinner := spinnerManager.ShowThrottle("✂️ "+config.Colors.Info.Sprint("Trimming")+" "+config.Colors.Dim.Sprint("conversation history..."), time.Second*2)
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

func compactChatHistory(cfg *config.Config, chatMessages []llms.ChatMessage, targetTokens int, keepMessages int) ([]llms.ChatMessage, bool) {
	if cfg == nil || len(chatMessages) == 0 {
		return nil, false
	}
	if keepMessages < 0 {
		keepMessages = 0
	}

	leadSystems := make([]llms.ChatMessage, 0, len(chatMessages))
	nonSystemStart := 0
	for i, msg := range chatMessages {
		if msg.GetType() != llms.ChatMessageTypeSystem {
			nonSystemStart = i
			break
		}
		leadSystems = append(leadSystems, msg)
		nonSystemStart = i + 1
	}

	nonSystem := chatMessages[nonSystemStart:]
	if len(nonSystem) == 0 {
		return nil, false
	}

	keep := keepMessages
	if keep >= len(nonSystem) {
		if len(nonSystem) > 1 {
			keep = len(nonSystem) - 1
		} else {
			keep = 0
		}
		logger.Log("debug", "Auto-compact adjusted keep-messages from %d to %d for %d non-system messages", keepMessages, keep, len(nonSystem))
	}
	summaryCandidates := nonSystem[:len(nonSystem)-keep]
	if len(summaryCandidates) == 0 {
		return nil, false
	}

	var previousCompactSummary string
	for _, s := range leadSystems {
		content := strings.TrimSpace(s.GetContent())
		if strings.HasPrefix(content, autoCompactSummaryHeader) {
			previousCompactSummary = strings.TrimSpace(strings.TrimPrefix(content, autoCompactSummaryHeader))
		}
	}

	transcript := buildCompactTranscript(previousCompactSummary, summaryCandidates)
	if strings.TrimSpace(transcript) == "" {
		return nil, false
	}

	summary, err := summarizeCompactTranscript(cfg, transcript)
	if err != nil {
		logger.Log("warn", "Auto-compact summarization failed: %v", err)
		return nil, false
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		logger.Log("warn", "Auto-compact summarization returned empty content")
		return nil, false
	}

	newMessages := make([]llms.ChatMessage, 0, 1+keep)
	newMessages = append(newMessages, llms.SystemChatMessage{Content: autoCompactSummaryHeader + "\n" + summary})
	newMessages = append(newMessages, nonSystem[len(nonSystem)-keep:]...)

	oldTokens := lib.CountTokensWithConfig(cfg, "", chatMessages)
	newTokens := lib.CountTokensWithConfig(cfg, "", newMessages)
	if targetTokens > 0 && newTokens > targetTokens && keep > 0 {
		logger.Log("warn", "Auto-compact over target after first pass (%d > %d); dropping kept tail messages", newTokens, targetTokens)
		for newTokens > targetTokens && keep > 0 {
			keep--
			newMessages = make([]llms.ChatMessage, 0, 1+keep)
			newMessages = append(newMessages, llms.SystemChatMessage{Content: autoCompactSummaryHeader + "\n" + summary})
			if keep > 0 {
				newMessages = append(newMessages, nonSystem[len(nonSystem)-keep:]...)
			}
			newTokens = lib.CountTokensWithConfig(cfg, "", newMessages)
		}
	}

	if targetTokens > 0 && newTokens > targetTokens {
		budgetForSummary := targetTokens
		if keep > 0 {
			keptTailTokens := lib.CountTokensWithConfig(cfg, "", nonSystem[len(nonSystem)-keep:])
			budgetForSummary = targetTokens - keptTailTokens
		}
		if budgetForSummary < 24 {
			budgetForSummary = 24
		}
		shrunk := truncateToTokenBudget(cfg, summary, budgetForSummary)
		if shrunk != summary {
			summary = shrunk
			newMessages = make([]llms.ChatMessage, 0, 1+keep)
			newMessages = append(newMessages, llms.SystemChatMessage{Content: autoCompactSummaryHeader + "\n" + summary})
			if keep > 0 {
				newMessages = append(newMessages, nonSystem[len(nonSystem)-keep:]...)
			}
			newTokens = lib.CountTokensWithConfig(cfg, "", newMessages)
			logger.Log("info", "Auto-compact summary shrunk to fit budget: %d tokens", newTokens)
		}
	}
	if newTokens >= oldTokens {
		logger.Log("warn", "Auto-compact was not effective (%d -> %d tokens)", oldTokens, newTokens)
		return nil, false
	}
	if targetTokens > 0 && newTokens > targetTokens {
		logger.Log("warn", "Auto-compact reduced tokens but not enough (%d target, got %d)", targetTokens, newTokens)
	}

	return newMessages, true
}

func buildCompactTranscript(previousSummary string, messages []llms.ChatMessage) string {
	var b strings.Builder
	if strings.TrimSpace(previousSummary) != "" {
		b.WriteString("Previous compact memory:\n")
		b.WriteString(strings.TrimSpace(previousSummary))
		b.WriteString("\n\n")
	}

	for _, msg := range messages {
		content := strings.TrimSpace(msg.GetContent())
		if content == "" {
			continue
		}
		role := "assistant"
		switch msg.GetType() {
		case llms.ChatMessageTypeSystem:
			role = "system"
		case llms.ChatMessageTypeHuman:
			role = "user"
		case llms.ChatMessageTypeAI:
			role = "assistant"
		case llms.ChatMessageTypeTool:
			role = "tool"
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n\n")
	}

	return strings.TrimSpace(b.String())
}

func summarizeCompactTranscript(cfg *config.Config, transcript string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("nil config")
	}

	systemPrompt := "You are a conversation memory compressor for Kubernetes troubleshooting sessions. Keep only durable facts needed for future turns."
	userPrompt := "Summarize the conversation transcript into concise memory with these sections:\n- Current objective\n- Confirmed findings\n- Commands and tool outcomes\n- Open questions/next checks\n\nRules:\n- Keep it factual and compact\n- Include concrete names (namespaces, pods, deployments) when present\n- Exclude chit-chat and repetition\n- Limit to about 12 short bullets\n\nTranscript:\n" + transcript

	origSpinnerMsg := cfg.SpinnerMessageOverride
	origSuppressContent := cfg.SuppressContentPrint
	origSuppressTools := cfg.SuppressToolPrint

	cfg.SpinnerMessageOverride = "Compacting conversation context…"
	cfg.SuppressContentPrint = true
	cfg.SuppressToolPrint = true

	summary, err := RequestWithSystem(cfg, systemPrompt, userPrompt, false, false)

	cfg.SpinnerMessageOverride = origSpinnerMsg
	cfg.SuppressContentPrint = origSuppressContent
	cfg.SuppressToolPrint = origSuppressTools

	if err != nil {
		return "", err
	}
	return strings.TrimSpace(summary), nil
}

func truncateToTokenBudget(cfg *config.Config, text string, budgetTokens int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || budgetTokens <= 0 {
		return trimmed
	}
	if lib.EstimateTokens(cfg, trimmed) <= budgetTokens {
		return trimmed
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return trimmed
	}

	left := 0
	right := len(parts)
	best := ""
	for left <= right {
		mid := (left + right) / 2
		candidate := strings.Join(parts[:mid], " ")
		tokenCount := lib.EstimateTokens(cfg, candidate)
		if tokenCount <= budgetTokens {
			best = candidate
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	best = strings.TrimSpace(best)
	if best == "" {
		if len(parts) > 0 {
			return parts[0]
		}
		return ""
	}
	return best
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
