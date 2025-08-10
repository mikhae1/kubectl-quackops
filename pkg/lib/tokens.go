package lib

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	gl "cloud.google.com/go/ai/generativelanguage/apiv1beta"
	glpb "cloud.google.com/go/ai/generativelanguage/apiv1beta/generativelanguagepb"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/pkoukk/tiktoken-go"
	"github.com/tmc/langchaingo/llms"
	"google.golang.org/api/option"
)

// tokenEncodingCache caches encoders by name to avoid repeated construction
var tokenEncodingCache sync.Map // map[string]*tiktoken.Tiktoken
// Cache for Google model token limits to avoid repeated API calls
var googleModelLimitsCache sync.Map // map[string]modelLimits

type modelLimits struct {
	inputTokens  int
	outputTokens int
}

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
	// Prefer the larger of encoding-based and char-heuristic to avoid underestimates
	if charTokens > encTokens {
		return charTokens
	}
	return encTokens
}

// CountTokensWithConfig estimates token count for text and messages based on cfg.
func CountTokensWithConfig(cfg *config.Config, text string, messages []llms.ChatMessage) int {
	if cfg == nil {
		return 0
	}

	// Avoid double-counting when the last message equals the provided text
	includeText := text != ""
	if includeText && len(messages) > 0 {
		last := strings.TrimSpace(messages[len(messages)-1].GetContent())
		if strings.TrimSpace(text) == last {
			includeText = false
		}
	}

	// Prefer Google's exact CountTokens when using Gemini and API key is available
	p := strings.ToLower(cfg.Provider)
	if (p == "google" || strings.Contains(strings.ToLower(cfg.Model), "gemini")) && os.Getenv("GOOGLE_API_KEY") != "" {
		if total, err := googleCountTokens(cfg, text, messages, includeText); err == nil && total > 0 {
			return total
		}
		// fall back to heuristic below on error
	}

	tokenCount := 0
	if includeText {
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

// EffectiveMaxTokens returns the effective maximum input token window for the
// current provider/model, falling back to cfg.MaxTokens when unknown.
// For Google Gemini models, we align to commonly published context sizes.
// This is used for display and budgeting heuristics only and does not change
// provider API limits.
func EffectiveMaxTokens(cfg *config.Config) int {
	if cfg == nil {
		return 0
	}
	configured := cfg.MaxTokens
	provider := strings.ToLower(cfg.Provider)
	model := strings.ToLower(cfg.Model)

	// Google Gemini: Prefer querying model info for accurate token limits when possible
	if (provider == "google" || strings.Contains(model, "gemini")) && os.Getenv("GOOGLE_API_KEY") != "" {
		if inLimit, _, err := googleGetModelTokenLimits(cfg); err == nil && inLimit > 0 {
			if configured <= 0 || configured > inLimit {
				return inLimit
			}
			return configured
		}
		// If API not available, fall back to default heuristic for Gemini
		if strings.Contains(model, "1.5") {
			switch {
			case strings.Contains(model, "pro"):
				if configured <= 0 || configured > 2097152 {
					return 2097152
				}
			case strings.Contains(model, "flash"):
				if configured <= 0 || configured > 1048576 {
					return 1048576
				}
			}
		}
		if configured <= 0 {
			return 128000
		}
	}

	if configured <= 0 {
		return 4096
	}
	return configured
}

// googleGetModelTokenLimits fetches model InputTokenLimit and OutputTokenLimit from the
// Google Generative Language API and caches the result.
func googleGetModelTokenLimits(cfg *config.Config) (int, int, error) {
	if cfg == nil {
		return 0, 0, fmt.Errorf("nil cfg")
	}
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return 0, 0, fmt.Errorf("missing GOOGLE_API_KEY")
	}

	modelName := cfg.Model
	if !strings.Contains(modelName, "/") {
		modelName = "models/" + modelName
	}
	cacheKey := strings.ToLower(modelName)
	if v, ok := googleModelLimitsCache.Load(cacheKey); ok {
		ml := v.(modelLimits)
		return ml.inputTokens, ml.outputTokens, nil
	}

	ctx := context.Background()
	mc, err := gl.NewModelRESTClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return 0, 0, err
	}
	defer mc.Close()

	resp, err := mc.GetModel(ctx, &glpb.GetModelRequest{Name: modelName})
	if err != nil || resp == nil {
		if err == nil {
			err = fmt.Errorf("nil GetModel response")
		}
		return 0, 0, err
	}
	in := int(resp.InputTokenLimit)
	out := int(resp.OutputTokenLimit)
	googleModelLimitsCache.Store(cacheKey, modelLimits{inputTokens: in, outputTokens: out})
	return in, out, nil
}

// googleCountTokens tries to use the Gemini CountTokens API to get exact token
// counts for a combination of messages and optional extra text. If includeText
// is false, the text argument is ignored. On any error, an error is returned so
// callers can gracefully fall back to heuristics.
func googleCountTokens(cfg *config.Config, text string, messages []llms.ChatMessage, includeText bool) (int, error) {
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		return 0, fmt.Errorf("missing GOOGLE_API_KEY")
	}

	ctx := context.Background()
	client, err := gl.NewGenerativeRESTClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return 0, err
	}
	defer client.Close()

	modelName := cfg.Model
	if !strings.Contains(modelName, "/") {
		modelName = "models/" + modelName
	}

	// Build the content list in the same order we send messages to LLM
	var contents []*glpb.Content
	for _, msg := range messages {
		var role string
		switch msg.GetType() {
		case llms.ChatMessageTypeSystem:
			// The API accepts arbitrary role strings; use "system" for clarity
			role = "system"
		case llms.ChatMessageTypeAI:
			role = "model"
		default:
			role = "user"
		}
		contents = append(contents, &glpb.Content{
			Role: role,
			Parts: []*glpb.Part{
				{Data: &glpb.Part_Text{Text: msg.GetContent()}},
			},
		})
	}

	if includeText && strings.TrimSpace(text) != "" {
		contents = append(contents, &glpb.Content{
			Role: "user",
			Parts: []*glpb.Part{
				{Data: &glpb.Part_Text{Text: text}},
			},
		})
	}

	req := &glpb.CountTokensRequest{
		Model: modelName,
		GenerateContentRequest: &glpb.GenerateContentRequest{
			Model:    modelName,
			Contents: contents,
		},
	}

	resp, err := client.CountTokens(ctx, req)
	if err != nil || resp == nil {
		if err == nil {
			err = fmt.Errorf("nil count tokens response")
		}
		return 0, err
	}
	return int(resp.TotalTokens), nil
}
