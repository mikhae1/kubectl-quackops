package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// getAzOpenAIAPIVersion returns the Azure OpenAI API version from environment variables
func getAzOpenAIAPIVersion() string {
	if version := os.Getenv("QU_AZ_OPENAI_API_VERSION"); version != "" {
		return version
	}
	return "2025-05-01" // default
}

// getAzOpenAIAPIKey returns the Azure OpenAI API key from environment variables
func getAzOpenAIAPIKey() string {
	// Check QU_AZ_OPENAI_API_KEY first (primary)
	if apiKey := os.Getenv("QU_AZ_OPENAI_API_KEY"); apiKey != "" {
		return apiKey
	}
	// Check OPENAI_API_KEY as alias
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		return apiKey
	}
	return ""
}

// getGoogleAPIKey returns the Gemini API key from environment variables.
// Supports both GOOGLE_API_KEY and GEMINI_API_KEY.
func getGoogleAPIKey() string {
	if apiKey := os.Getenv("GOOGLE_API_KEY"); apiKey != "" {
		return apiKey
	}
	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		return apiKey
	}
	return ""
}

// ModelMetadata holds information about a model's capabilities
type ModelMetadata struct {
	ID            string  `json:"id"`
	ContextLength int     `json:"context_length"`
	MaxTokens     int     `json:"max_tokens"`
	Description   string  `json:"description,omitempty"`
	InputPrice    float64 `json:"input_price,omitempty"`  // USD per token for input/prompt
	OutputPrice   float64 `json:"output_price,omitempty"` // USD per token for output/completion
	PricePretty   string  `json:"price_pretty,omitempty"` // Formatted price string like "↑$1.25/↓$10.0"
}

// OpenRouterModelsResponse represents the OpenRouter API models response
type OpenRouterModelsResponse struct {
	Data []struct {
		ID                  string `json:"id"`
		Name                string `json:"name"`
		ContextLength       int    `json:"context_length"`
		MaxCompletionTokens int    `json:"max_completion_tokens"`
		Description         string `json:"description,omitempty"`
		Pricing             struct {
			Prompt     string `json:"prompt"`     // USD per token for input
			Completion string `json:"completion"` // USD per token for output
		} `json:"pricing,omitempty"`
	} `json:"data"`
}

// OpenAIModelsResponse represents the OpenAI API models response
type OpenAIModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID        string   `json:"id"`
		Object    string   `json:"object"`
		MaxTokens int      `json:"max_tokens"`
		Features  []string `json:"features"`
		Pricing   *struct {
			Unit       string `json:"unit,omitempty"`       // "token"
			Prompt     string `json:"prompt,omitempty"`     // USD per token for input
			Completion string `json:"completion,omitempty"` // USD per token for output
		} `json:"pricing,omitempty"`
	} `json:"data"`
}

// AzureOpenAIModelsResponse represents the Azure OpenAI API models response
type AzureOpenAIModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID        string                 `json:"id"`
		Object    string                 `json:"object"`
		MaxTokens int                    `json:"max_tokens"`
		Features  map[string]interface{} `json:"features"` // Azure OpenAI returns features as object, not array
		Limits    *struct {
			MaxPromptTokens     int `json:"max_prompt_tokens,omitempty"`
			MaxTotalTokens      int `json:"max_total_tokens,omitempty"`
			MaxCompletionTokens int `json:"max_completion_tokens,omitempty"`
		} `json:"limits,omitempty"`
		Description string `json:"description,omitempty"`
		Pricing     *struct {
			Unit       string `json:"unit,omitempty"`       // "token"
			Prompt     string `json:"prompt,omitempty"`     // USD per token for input
			Completion string `json:"completion,omitempty"` // USD per token for output
		} `json:"pricing,omitempty"`
	} `json:"data"`
}

// extractContextLengthFromLimits extracts context length from Azure OpenAI limits in priority order
func extractContextLengthFromLimits(limits *struct {
	MaxPromptTokens     int `json:"max_prompt_tokens,omitempty"`
	MaxTotalTokens      int `json:"max_total_tokens,omitempty"`
	MaxCompletionTokens int `json:"max_completion_tokens,omitempty"`
}) int {
	if limits == nil {
		return 0
	}
	if limits.MaxCompletionTokens > 0 {
		return limits.MaxCompletionTokens
	}
	if limits.MaxTotalTokens > 0 {
		return limits.MaxTotalTokens
	}
	if limits.MaxPromptTokens > 0 {
		return limits.MaxPromptTokens
	}
	return 0
}

// formatPrice formats a price as a per-1M-tokens price string
func formatPrice(price float64) string {
	if price == 0.0 {
		return "Free"
	}

	var pricePerMillion float64
	// If price is very small (< 0.01), assume it's per-token and convert to per-1M-tokens
	// If price is larger, assume it's already per-1M-tokens
	if price < 0.01 {
		pricePerMillion = price * 1_000_000
	} else {
		pricePerMillion = price
	}

	if pricePerMillion < 0.01 {
		return fmt.Sprintf("$%.3f", pricePerMillion)
	} else if pricePerMillion < 1.0 {
		return fmt.Sprintf("$%.2f", pricePerMillion)
	} else if pricePerMillion < 100.0 {
		return fmt.Sprintf("$%.1f", pricePerMillion)
	} else {
		return fmt.Sprintf("$%.0f", pricePerMillion)
	}
}

// formatPricePretty creates a formatted price string for display with input/output arrows
func formatPricePretty(inputPrice, outputPrice float64) string {
	if inputPrice == 0.0 && outputPrice == 0.0 {
		return "Free"
	}

	var parts []string

	if inputPrice > 0.0 {
		parts = append(parts, "↑"+formatPrice(inputPrice))
	}

	if outputPrice > 0.0 {
		parts = append(parts, "↓"+formatPrice(outputPrice))
	}

	if len(parts) == 0 {
		return "Free"
	}

	return strings.Join(parts, "/")
}

// MetadataService provides model metadata detection
type MetadataService struct {
	httpClient *http.Client
	cache      map[string]cacheEntry
	cacheTTL   time.Duration
	mu         sync.RWMutex
}

// NewMetadataService creates a new metadata service
func NewMetadataService(timeout time.Duration, cacheTTL time.Duration) *MetadataService {
	return &MetadataService{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		cache:    make(map[string]cacheEntry),
		cacheTTL: cacheTTL,
	}
}

type cacheEntry struct {
	metadata  *ModelMetadata
	expiresAt time.Time
}

func (ms *MetadataService) doRequest(method, url string, body io.Reader, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}
	return b, resp.StatusCode, nil
}

// getJSON performs an HTTP request and unmarshals a successful JSON response into target
func (ms *MetadataService) getJSON(method, url string, body io.Reader, headers map[string]string, target any) error {
	b, status, err := ms.doRequest(method, url, body, headers)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("api returned status %d", status)
	}
	if err := json.Unmarshal(b, target); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	return nil
}

// parsePriceString parses a price string into float64, returning 0 on error or empty input
func parsePriceString(s string) float64 {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// GetModelContextLength retrieves the context length for a given model
func (ms *MetadataService) GetModelContextLength(provider, model, baseURL string) (int, error) {
	cacheKey := fmt.Sprintf("%s:%s:%s", provider, model, baseURL)

	// Check cache first
	ms.mu.RLock()
	if cached, exists := ms.cache[cacheKey]; exists {
		if cached.expiresAt.IsZero() || time.Now().Before(cached.expiresAt) {
			ctx := cached.metadata.ContextLength
			ms.mu.RUnlock()
			return ctx, nil
		}
	}
	ms.mu.RUnlock()

	var metadata *ModelMetadata
	var err error

	switch provider {
	case "openai":
		if isOpenRouterURL(baseURL) {
			metadata, err = ms.fetchOpenRouterMetadata(model, baseURL)
		} else {
			metadata, err = ms.fetchOpenAIMetadata(model, baseURL)
		}
	case "azopenai":
		// Azure OpenAI uses the same API structure as OpenAI
		metadata, err = ms.fetchAzureOpenAIMetadata(model, baseURL)
	case "google":
		metadata, err = ms.fetchGoogleMetadata(model, baseURL)
	case "anthropic":
		metadata, err = ms.fetchAnthropicMetadata(model, baseURL)
	case "ollama":
		metadata, err = ms.fetchOllamaMetadata(model, baseURL)
	default:
		return 0, fmt.Errorf("unsupported provider: %s", provider)
	}

	if err != nil {
		return 0, err
	}

	// Cache the result
	ms.mu.Lock()
	var exp time.Time
	if ms.cacheTTL > 0 {
		exp = time.Now().Add(ms.cacheTTL)
	}
	ms.cache[cacheKey] = cacheEntry{metadata: metadata, expiresAt: exp}
	ms.mu.Unlock()

	return metadata.ContextLength, nil
}

// GetModelList retrieves a list of available models from the provider
func (ms *MetadataService) GetModelList(provider, baseURL string) ([]*ModelMetadata, error) {
	switch provider {
	case "openai":
		if isOpenRouterURL(baseURL) {
			return ms.fetchOpenRouterModelList(baseURL)
		} else {
			return ms.fetchOpenAIModelList(baseURL)
		}
	case "azopenai":
		// Azure OpenAI uses the same API structure as OpenAI
		return ms.fetchAzureOpenAIModelList(baseURL)
	case "google":
		return ms.fetchGoogleModelList(baseURL)
	case "anthropic":
		return ms.fetchAnthropicModelList(baseURL)
	case "ollama":
		return ms.fetchOllamaModelList(baseURL)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

// fetchOpenRouterMetadata fetches model metadata from OpenRouter API
func (ms *MetadataService) fetchOpenRouterMetadata(model, baseURL string) (*ModelMetadata, error) {
	// Handle case where baseURL already includes /api/v1
	var apiURL string
	if strings.HasSuffix(baseURL, "/api/v1") {
		apiURL = strings.TrimSuffix(baseURL, "/") + "/models"
	} else {
		apiURL = strings.TrimSuffix(baseURL, "/") + "/api/v1/models"
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	} else if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	var response OpenRouterModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &response); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	// Find the requested model
	for _, modelData := range response.Data {
		if modelData.ID == model || strings.HasSuffix(modelData.ID, "/"+model) {
			// Parse pricing strings to floats
			promptPrice := parsePriceString(modelData.Pricing.Prompt)
			completionPrice := parsePriceString(modelData.Pricing.Completion)

			return &ModelMetadata{
				ID:            modelData.ID,
				ContextLength: modelData.ContextLength,
				MaxTokens:     modelData.ContextLength, // Use context length for max tokens
				InputPrice:    promptPrice,
				OutputPrice:   completionPrice,
				PricePretty:   formatPricePretty(promptPrice, completionPrice),
			}, nil
		}
	}

	return nil, fmt.Errorf("model %s not found", model)
}

// fetchOpenAIMetadata fetches model metadata from OpenAI API
func (ms *MetadataService) fetchOpenAIMetadata(model, baseURL string) (*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	apiURL := strings.TrimSuffix(baseURL, "/") + "/v1/models"

	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	var response OpenAIModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &response); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	// Find the requested model
	for _, modelData := range response.Data {
		if modelData.ID == model {
			// Parse pricing from API or use fallback
			var promptPrice, completionPrice float64
			if modelData.Pricing != nil {
				promptPrice = parsePriceString(modelData.Pricing.Prompt)
				completionPrice = parsePriceString(modelData.Pricing.Completion)
			}

			return &ModelMetadata{
				ID:            modelData.ID,
				ContextLength: modelData.MaxTokens,
				MaxTokens:     modelData.MaxTokens,
				InputPrice:    promptPrice,
				OutputPrice:   completionPrice,
				PricePretty:   formatPricePretty(promptPrice, completionPrice),
			}, nil
		}
	}
	fallback := getDefaultContextLengthForModel(model)
	if fallback > 0 {
		// No API pricing available - leave as zero
		return &ModelMetadata{
			ID:            model,
			ContextLength: fallback,
			MaxTokens:     fallback,
			InputPrice:    0.0,
			OutputPrice:   0.0,
			PricePretty:   formatPricePretty(0.0, 0.0),
		}, nil
	}
	return nil, fmt.Errorf("model %s not found", model)
}

// fetchAzureOpenAIMetadata fetches model metadata from Azure OpenAI API
func (ms *MetadataService) fetchAzureOpenAIMetadata(model, baseURL string) (*ModelMetadata, error) {
	apiVersion := getAzOpenAIAPIVersion()

	if baseURL == "" {
		return nil, fmt.Errorf("azure openai requires a base URL")
	}

	// Azure OpenAI API endpoint format: https://{resource-name}.openai.azure.com/openai/deployments/{deployment-name}/models
	// But for getting model info, we can use the general models endpoint
	apiURL := strings.TrimSuffix(baseURL, "/") + "/openai/models?api-version=" + apiVersion

	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := getAzOpenAIAPIKey(); apiKey != "" {
		headers["api-key"] = apiKey
	}
	var response AzureOpenAIModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &response); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	// Find the requested model (in Azure OpenAI, this is usually the deployment name)
	for _, modelData := range response.Data {
		if modelData.ID == model {
			contextLength := extractContextLengthFromLimits(modelData.Limits)
			if contextLength == 0 && modelData.MaxTokens > 0 {
				contextLength = modelData.MaxTokens
			}
			if contextLength == 0 {
				contextLength = getDefaultContextLengthForModel(modelData.ID)
			}

			// Parse pricing from API or use fallback
			var promptPrice, completionPrice float64
			if modelData.Pricing != nil {
				promptPrice = parsePriceString(modelData.Pricing.Prompt)
				completionPrice = parsePriceString(modelData.Pricing.Completion)
			}

			return &ModelMetadata{
				ID:            modelData.ID,
				ContextLength: contextLength,
				MaxTokens:     contextLength,
				Description:   modelData.Description,
				InputPrice:    promptPrice,
				OutputPrice:   completionPrice,
				PricePretty:   formatPricePretty(promptPrice, completionPrice),
			}, nil
		}
	}

	// If not found by exact match, try to use default context length based on known model families
	contextLength := getDefaultContextLengthForModel(model)

	// No API pricing available - leave as zero
	return &ModelMetadata{
		ID:            model,
		ContextLength: contextLength,
		MaxTokens:     contextLength,
		Description:   "", // No description available for fallback case
		InputPrice:    0.0,
		OutputPrice:   0.0,
		PricePretty:   formatPricePretty(0.0, 0.0),
	}, nil
}

// fetchAzureOpenAIModelList fetches the list of available models from Azure OpenAI
func (ms *MetadataService) fetchAzureOpenAIModelList(baseURL string) ([]*ModelMetadata, error) {
	apiVersion := getAzOpenAIAPIVersion()

	if baseURL == "" {
		return nil, fmt.Errorf("azure openai requires a base URL")
	}

	apiURL := strings.TrimSuffix(baseURL, "/") + "/openai/models?api-version=" + apiVersion

	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := getAzOpenAIAPIKey(); apiKey != "" {
		headers["api-key"] = apiKey
	}
	var response AzureOpenAIModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &response); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	var models []*ModelMetadata
	for _, modelData := range response.Data {
		contextLength := extractContextLengthFromLimits(modelData.Limits)
		if contextLength == 0 && modelData.MaxTokens > 0 {
			contextLength = modelData.MaxTokens
		}
		if contextLength == 0 {
			contextLength = getDefaultContextLengthForModel(modelData.ID)
		}

		// Parse pricing from API or use fallback
		var promptPrice, completionPrice float64
		if modelData.Pricing != nil {
			promptPrice = parsePriceString(modelData.Pricing.Prompt)
			completionPrice = parsePriceString(modelData.Pricing.Completion)
		}

		models = append(models, &ModelMetadata{
			ID:            modelData.ID,
			ContextLength: contextLength,
			MaxTokens:     contextLength,
			Description:   modelData.Description,
			InputPrice:    promptPrice,
			OutputPrice:   completionPrice,
			PricePretty:   formatPricePretty(promptPrice, completionPrice),
		})
	}

	return models, nil
}

// getDefaultContextLengthForModel returns a reasonable default context length based on model name
func getDefaultContextLengthForModel(model string) int {
	modelLower := strings.ToLower(strings.TrimSpace(model))
	// OpenAI - modern families
	if strings.Contains(modelLower, "gpt-5") {
		return 128000
	}
	if strings.Contains(modelLower, "gpt-4o") || strings.Contains(modelLower, "gpt-4.1") {
		return 128000
	}
	if strings.Contains(modelLower, "gpt-4") {
		if strings.Contains(modelLower, "32k") {
			return 32768
		}
		if strings.Contains(modelLower, "turbo") || strings.Contains(modelLower, "preview") || strings.Contains(modelLower, "mini") {
			return 128000
		}
		return 8192
	}
	if strings.Contains(modelLower, "gpt-3.5") {
		if strings.Contains(modelLower, "16k") {
			return 16384
		}
		return 4096
	}
	// Google Gemini
	if strings.Contains(modelLower, "gemini") {
		if strings.Contains(modelLower, "2.5") {
			return 2000000
		}
		if strings.Contains(modelLower, "1.5") {
			if strings.Contains(modelLower, "pro") {
				return 2000000
			}
			return 1000000
		}
		return 1000000
	}
	// Anthropic Claude 3.x
	if strings.Contains(modelLower, "claude") {
		return 200000
	}
	// Meta Llama
	if strings.Contains(modelLower, "llama") {
		if strings.Contains(modelLower, "3.2") || strings.Contains(modelLower, "3.1") {
			return 128000
		}
		if strings.Contains(modelLower, "3") {
			return 8192
		}
		return 4096
	}
	// Mistral
	if strings.Contains(modelLower, "mistral") || strings.Contains(modelLower, "mixtral") {
		if strings.Contains(modelLower, "large") {
			return 128000
		}
		return 32768
	}
	// Qwen
	if strings.Contains(modelLower, "qwen") {
		return 131072
	}
	// GLM (Zhipu/ChatGLM)
	if strings.Contains(modelLower, "glm") {
		return 128000
	}
	// DeepSeek
	if strings.Contains(modelLower, "deepseek") {
		return 131072
	}
	// Cohere Command family (via OpenRouter)
	if strings.Contains(modelLower, "command") {
		return 128000
	}
	// Default for unknown models (modern, conservative)
	return 32768
}

// GoogleModelResponse represents a single Google (Gemini) model response
type GoogleModelResponse struct {
	Name             string `json:"name"`
	InputTokenLimit  int    `json:"inputTokenLimit"`
	OutputTokenLimit int    `json:"outputTokenLimit"`
}

// fetchGoogleMetadata fetches model metadata from Google Generative Language API
func (ms *MetadataService) fetchGoogleMetadata(model, baseURL string) (*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}

	// Normalize model path expected by API: v1beta/models/{model}
	apiModel := model
	if !strings.HasPrefix(apiModel, "models/") {
		apiModel = "models/" + apiModel
	}

	apiURL := strings.TrimSuffix(baseURL, "/") + "/v1beta/" + apiModel

	// Google Generative Language API expects API key via query parameter
	if apiKey := getGoogleAPIKey(); apiKey != "" {
		if strings.Contains(apiURL, "?") {
			apiURL += "&key=" + apiKey
		} else {
			apiURL += "?key=" + apiKey
		}
	}

	headers := map[string]string{"Content-Type": "application/json"}
	var gm GoogleModelResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &gm); err != nil {
		return nil, fmt.Errorf("failed to fetch model: %w", err)
	}

	// Derive context length. Prefer explicit input token limit; if zero, try output, else default.
	contextLen := gm.InputTokenLimit
	if contextLen == 0 {
		contextLen = gm.OutputTokenLimit
	}
	if contextLen == 0 {
		// Fallback to a safe default for Gemini family if limits are missing
		contextLen = 128000
	}

	// The API returns name like "models/gemini-..."; use original model id for ID
	id := model
	if strings.TrimSpace(id) == "" {
		id = gm.Name
	}

	return &ModelMetadata{
		ID:            id,
		ContextLength: contextLen,
		MaxTokens:     contextLen,
	}, nil
}

// --- Anthropic ---

type anthropicModelsResponse struct {
	Data []struct {
		ID               string `json:"id"`
		InputTokenLimit  int    `json:"input_token_limit"`
		OutputTokenLimit int    `json:"output_token_limit"`
		ContextWindow    int    `json:"context_window"`
	} `json:"data"`
}

func (ms *MetadataService) fetchAnthropicMetadata(model, baseURL string) (*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	apiURL := strings.TrimSuffix(baseURL, "/") + "/v1/models"

	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
	}
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		headers["x-api-key"] = apiKey
	}
	var r anthropicModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &r); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	// First try exact match
	for _, m := range r.Data {
		if m.ID == model {
			ctx := m.ContextWindow
			if ctx == 0 {
				if m.InputTokenLimit > 0 {
					ctx = m.InputTokenLimit
				} else if m.OutputTokenLimit > 0 {
					ctx = m.OutputTokenLimit
				}
			}
			if ctx == 0 {
				ctx = 200000
			}
			return &ModelMetadata{ID: m.ID, ContextLength: ctx, MaxTokens: ctx}, nil
		}
	}
	// If not found and model uses -latest alias, resolve to newest dated version
	if strings.HasSuffix(model, "-latest") {
		family := strings.TrimSuffix(model, "-latest")
		bestID := ""
		bestDate := ""
		dateRe := regexp.MustCompile(`^` + regexp.QuoteMeta(family) + `-(\d{8})$`)
		for _, m := range r.Data {
			if strings.HasPrefix(m.ID, family+"-") {
				// Prefer strict date-suffixed IDs
				if sub := dateRe.FindStringSubmatch(m.ID); len(sub) == 2 {
					if sub[1] >= bestDate {
						bestDate = sub[1]
						bestID = m.ID
					}
				} else if bestID == "" {
					// Fallback: take the first prefixed ID if no date match yet
					bestID = m.ID
				}
			}
		}
		if bestID != "" {
			// Find the selected model again to compute context
			for _, m := range r.Data {
				if m.ID == bestID {
					ctx := m.ContextWindow
					if ctx == 0 {
						if m.InputTokenLimit > 0 {
							ctx = m.InputTokenLimit
						} else if m.OutputTokenLimit > 0 {
							ctx = m.OutputTokenLimit
						}
					}
					if ctx == 0 {
						ctx = 200000
					}
					return &ModelMetadata{ID: bestID, ContextLength: ctx, MaxTokens: ctx}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("model %s not found", model)
}

// --- Ollama ---

type ollamaShowResponse struct {
	Model      string         `json:"model"`
	Parameters string         `json:"parameters"`
	ModelInfo  map[string]any `json:"model_info"`
}

func (ms *MetadataService) fetchOllamaMetadata(model, baseURL string) (*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	// Ollama accepts both base and base/api; normalize to base without trailing /api
	serverURL := strings.TrimSuffix(baseURL, "/api")
	apiURL := strings.TrimSuffix(serverURL, "/") + "/api/show"
	payload := map[string]string{"name": model}
	b, _ := json.Marshal(payload)

	headers := map[string]string{"Content-Type": "application/json"}
	var r ollamaShowResponse
	if err := ms.getJSON("POST", apiURL, bytes.NewReader(b), headers, &r); err != nil {
		return nil, fmt.Errorf("failed to fetch model: %w", err)
	}

	// Try multiple places to determine context length
	ctx := 0
	// 1) model_info.num_ctx or similar
	if r.ModelInfo != nil {
		for k, v := range r.ModelInfo {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "num_ctx") || strings.Contains(lk, "context") || strings.Contains(lk, "ctx") {
				switch t := v.(type) {
				case float64:
					if int(t) > ctx {
						ctx = int(t)
					}
				case string:
					if n, err := strconv.Atoi(t); err == nil && n > ctx {
						ctx = n
					}
				}
			}
		}
	}
	// 2) parse parameters string like "num_ctx 8192"
	if ctx == 0 && strings.TrimSpace(r.Parameters) != "" {
		re := regexp.MustCompile(`(?i)num_ctx\s+(\d+)`)
		m := re.FindStringSubmatch(r.Parameters)
		if len(m) == 2 {
			if n, err := strconv.Atoi(m[1]); err == nil {
				ctx = n
			}
		}
	}
	if ctx == 0 {
		ctx = 4096
	}
	id := model
	if strings.TrimSpace(id) == "" && strings.TrimSpace(r.Model) != "" {
		id = r.Model
	}
	return &ModelMetadata{ID: id, ContextLength: ctx, MaxTokens: ctx}, nil
}

// isOpenRouterURL checks if the base URL belongs to OpenRouter
func isOpenRouterURL(baseURL string) bool {
	return strings.Contains(strings.ToLower(baseURL), "openrouter.ai")
}

// fetchOpenRouterModelList fetches the list of available models from OpenRouter
func (ms *MetadataService) fetchOpenRouterModelList(baseURL string) ([]*ModelMetadata, error) {
	var apiURL string
	if strings.HasSuffix(baseURL, "/api/v1") {
		apiURL = strings.TrimSuffix(baseURL, "/") + "/models"
	} else {
		apiURL = strings.TrimSuffix(baseURL, "/") + "/api/v1/models"
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := os.Getenv("OPENROUTER_API_KEY"); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	} else if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	var response OpenRouterModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &response); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	var models []*ModelMetadata
	for _, modelData := range response.Data {
		// Parse pricing strings to floats
		promptPrice := parsePriceString(modelData.Pricing.Prompt)
		completionPrice := parsePriceString(modelData.Pricing.Completion)

		models = append(models, &ModelMetadata{
			ID:            modelData.ID,
			ContextLength: modelData.ContextLength,
			MaxTokens:     modelData.ContextLength, // Use context length for max tokens
			Description:   modelData.Description,
			InputPrice:    promptPrice,
			OutputPrice:   completionPrice,
			PricePretty:   formatPricePretty(promptPrice, completionPrice),
		})
	}

	return models, nil
}

// fetchOpenAIModelList fetches the list of available models from OpenAI
func (ms *MetadataService) fetchOpenAIModelList(baseURL string) ([]*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	apiURL := strings.TrimSuffix(baseURL, "/") + "/v1/models"

	headers := map[string]string{"Content-Type": "application/json"}
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
	var response OpenAIModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &response); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	var models []*ModelMetadata
	for _, modelData := range response.Data {
		// Parse pricing from API or use fallback
		var promptPrice, completionPrice float64
		if modelData.Pricing != nil {
			promptPrice = parsePriceString(modelData.Pricing.Prompt)
			completionPrice = parsePriceString(modelData.Pricing.Completion)
		}

		models = append(models, &ModelMetadata{
			ID:            modelData.ID,
			ContextLength: modelData.MaxTokens,
			MaxTokens:     modelData.MaxTokens,
			InputPrice:    promptPrice,
			OutputPrice:   completionPrice,
			PricePretty:   formatPricePretty(promptPrice, completionPrice),
		})
	}

	return models, nil
}

// fetchGoogleModelList fetches the list of available models from Google
func (ms *MetadataService) fetchGoogleModelList(baseURL string) ([]*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}

	apiURL := strings.TrimSuffix(baseURL, "/") + "/v1beta/models"

	if apiKey := getGoogleAPIKey(); apiKey != "" {
		if strings.Contains(apiURL, "?") {
			apiURL += "&key=" + apiKey
		} else {
			apiURL += "?key=" + apiKey
		}
	}

	headers := map[string]string{"Content-Type": "application/json"}
	// Google returns a different structure for the models list
	var response struct {
		Models []GoogleModelResponse `json:"models"`
	}
	if err := ms.getJSON("GET", apiURL, nil, headers, &response); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	var models []*ModelMetadata
	for _, model := range response.Models {
		contextLen := model.InputTokenLimit
		if contextLen == 0 {
			contextLen = model.OutputTokenLimit
		}
		if contextLen == 0 {
			contextLen = 128000
		}

		// Extract model ID from name (e.g., "models/gemini-pro" -> "gemini-pro")
		modelID := strings.TrimPrefix(model.Name, "models/")

		models = append(models, &ModelMetadata{
			ID:            modelID,
			ContextLength: contextLen,
			MaxTokens:     contextLen,
		})
	}

	return models, nil
}

// fetchAnthropicModelList fetches the list of available models from Anthropic
func (ms *MetadataService) fetchAnthropicModelList(baseURL string) ([]*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	apiURL := strings.TrimSuffix(baseURL, "/") + "/v1/models"

	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
	}
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		headers["x-api-key"] = apiKey
	}
	var r anthropicModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &r); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	var models []*ModelMetadata
	for _, m := range r.Data {
		ctx := m.ContextWindow
		if ctx == 0 {
			if m.InputTokenLimit > 0 {
				ctx = m.InputTokenLimit
			} else if m.OutputTokenLimit > 0 {
				ctx = m.OutputTokenLimit
			}
		}
		if ctx == 0 {
			ctx = 200000
		}

		models = append(models, &ModelMetadata{
			ID:            m.ID,
			ContextLength: ctx,
			MaxTokens:     ctx,
		})
	}

	return models, nil
}

// OllamaModelsResponse represents the Ollama API models list response
type OllamaModelsResponse struct {
	Models []struct {
		Name       string                 `json:"name"`
		Model      string                 `json:"model"`
		ModifiedAt string                 `json:"modified_at"`
		Size       int64                  `json:"size"`
		Digest     string                 `json:"digest"`
		Details    map[string]interface{} `json:"details"`
	} `json:"models"`
}

// fetchOllamaModelList fetches the list of available models from Ollama
func (ms *MetadataService) fetchOllamaModelList(baseURL string) ([]*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	serverURL := strings.TrimSuffix(baseURL, "/api")
	apiURL := strings.TrimSuffix(serverURL, "/") + "/api/tags"

	headers := map[string]string{"Content-Type": "application/json"}
	var r OllamaModelsResponse
	if err := ms.getJSON("GET", apiURL, nil, headers, &r); err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	var models []*ModelMetadata
	for _, m := range r.Models {
		// Use a reasonable default context length for Ollama models
		ctx := 4096

		// Try to get context length from model details if available
		if m.Details != nil {
			for k, v := range m.Details {
				lk := strings.ToLower(k)
				if strings.Contains(lk, "num_ctx") || strings.Contains(lk, "context") {
					switch t := v.(type) {
					case float64:
						if int(t) > ctx {
							ctx = int(t)
						}
					case string:
						if n, err := strconv.Atoi(t); err == nil && n > ctx {
							ctx = n
						}
					}
				}
			}
		}

		models = append(models, &ModelMetadata{
			ID:            m.Name,
			ContextLength: ctx,
			MaxTokens:     ctx,
		})
	}

	return models, nil
}
