package lib

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/pkoukk/tiktoken-go"
	"github.com/tmc/langchaingo/llms"
)

// tokenEncodingCache caches encoders by name to avoid repeated construction
var tokenEncodingCache sync.Map // map[string]*tiktoken.Tiktoken

// GetEncodingForModel returns a best-effort token encoding name for a provider/model.
// Note: This is an approximation. For non-OpenAI providers we fall back to cl100k_base.
func GetEncodingForModel(provider string, model string) string {
	normalizedProvider := strings.ToLower(provider)
	normalizedModel := strings.ToLower(model)

	// Prefer o200k_base for newer OpenAI models (gpt-4o family, o3/o4 variants)
	if normalizedProvider == "openai" || normalizedProvider == "deepseek" {
		if strings.Contains(normalizedModel, "gpt-4o") ||
			strings.Contains(normalizedModel, "o3") || strings.Contains(normalizedModel, "o4") ||
			strings.Contains(normalizedModel, "-mini") {
			return "o200k_base"
		}
		return "cl100k_base"
	}

	// Ollama models often mimic LLaMA/others; Anthropic/Google use different tokenization.
	// We still estimate using cl100k_base unless otherwise overridden by heuristics below.
	return "cl100k_base"
}

// getEncoder loads or returns a cached encoder by encoding name.
func getEncoder(encoding string) *tiktoken.Tiktoken {
	if enc, ok := tokenEncodingCache.Load(encoding); ok {
		return enc.(*tiktoken.Tiktoken)
	}
	tke, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		// Fallback to cl100k_base if requested encoding is not available
		tke, _ = tiktoken.GetEncoding("cl100k_base")
	}
	if tke != nil {
		tokenEncodingCache.Store(encoding, tke)
	}
	return tke
}

// charPerTokenHeuristic returns approximate characters-per-token by provider/model family.
// This is used to improve estimates when the encoding library under/over-estimates for non-OpenAI models.
func charPerTokenHeuristic(provider string, model string) float64 {
	p := strings.ToLower(provider)
	m := strings.ToLower(model)
	// Reasonable defaults gathered from public references and empirical averages
	// OpenAI (GPT family): ~4 chars/token
	if p == "openai" || p == "deepseek" {
		if strings.Contains(m, "gpt-4o") || strings.Contains(m, "o3") || strings.Contains(m, "o4") {
			return 4.2
		}
		return 4.0
	}
	// Anthropic (Claude family): slightly fewer chars/token on average
	if p == "anthropic" {
		return 3.8
	}
	// Google Gemini: roughly similar; many reports around 3.7–4.1
	if p == "google" || strings.Contains(m, "gemini") {
		return 3.9
	}
	// Ollama (LLaMA/Mistral families): often near 3.5–4.0
	if p == "ollama" || strings.Contains(m, "llama") || strings.Contains(m, "mistral") {
		return 3.6
	}
	// Fallback
	return 4.0
}

// TokenizeWithEncoding tokenizes text with a specific encoding.
func TokenizeWithEncoding(encoding string, text string) []string {
	tke := getEncoder(encoding)
	if tke == nil {
		return []string{text}
	}
	tokens := tke.Encode(text, nil, nil)
	tokenStrs := make([]string, len(tokens))
	for i, t := range tokens {
		tokenStrs[i] = fmt.Sprintf("%d", t)
	}
	return tokenStrs
}

// EstimateTokens estimates token count for the given text using provider/model heuristics.
func EstimateTokens(cfg *config.Config, text string) int {
	if text == "" {
		return 0
	}
	enc := GetEncodingForModel(cfg.Provider, cfg.Model)
	// Encoding-based estimate
	encTokens := len(TokenizeWithEncoding(enc, text))
	// Character-based heuristic to better match non-OpenAI tokenizers
	chars := float64(len([]rune(text)))
	cpt := charPerTokenHeuristic(cfg.Provider, cfg.Model)
	charTokens := int(math.Ceil(chars / cpt))
	if charTokens > encTokens {
		return charTokens
	}
	return encTokens
}

// CountTokensWithConfig estimates token count for text and messages based on cfg.
func CountTokensWithConfig(cfg *config.Config, text string, messages []llms.ChatMessage) int {
	tokenCount := 0
	if text != "" {
		tokenCount += EstimateTokens(cfg, text)
	}
	if len(messages) > 0 {
		for _, message := range messages {
			tokenCount += EstimateTokens(cfg, message.GetContent())
		}
	}
	return tokenCount
}

// AtomicInt is a small helper for safe concurrent counters.
type AtomicInt struct{ v int64 }

func (a *AtomicInt) Add(delta int) { atomic.AddInt64(&a.v, int64(delta)) }
func (a *AtomicInt) Set(value int) { atomic.StoreInt64(&a.v, int64(value)) }
func (a *AtomicInt) Load() int     { return int(atomic.LoadInt64(&a.v)) }
func (a *AtomicInt) Reset()        { atomic.StoreInt64(&a.v, 0) }

// EstimateExpectedIncomingTokens predicts the likely size of the completion
// before any tokens are streamed. This is a heuristic tuned per provider.
func EstimateExpectedIncomingTokens(cfg *config.Config, outgoing int) int {
	window := cfg.MaxTokens
	if window <= 0 {
		window = 4096
	}
	// Half the window is a common cap for completions
	baseLimit := int(math.Min(float64(window/2), 8192))

	p := strings.ToLower(cfg.Provider)
	m := strings.ToLower(cfg.Model)
	type provHeur struct {
		base  int
		ratio float64
	}
	heur := provHeur{base: 512, ratio: 0.5}

	switch {
	case p == "openai" || p == "deepseek":
		heur = provHeur{base: 1024, ratio: 0.55}
		if strings.Contains(m, "gpt-4o") || strings.Contains(m, "o3") || strings.Contains(m, "o4") {
			heur = provHeur{base: 1536, ratio: 0.6}
		}
	case p == "anthropic":
		heur = provHeur{base: 1024, ratio: 0.6}
	case p == "google" || strings.Contains(m, "gemini"):
		heur = provHeur{base: 1536, ratio: 0.6}
	case p == "ollama" || strings.Contains(m, "llama") || strings.Contains(m, "mistral"):
		heur = provHeur{base: 768, ratio: 0.5}
	}

	byRatio := int(math.Ceil(float64(outgoing) * heur.ratio))
	predicted := byRatio
	if predicted < heur.base {
		predicted = heur.base
	}
	if predicted > baseLimit {
		predicted = baseLimit
	}
	if predicted < 128 {
		predicted = 128
	}
	return predicted
}
