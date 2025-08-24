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
	"time"
)

// ModelMetadata holds information about a model's capabilities
type ModelMetadata struct {
	ID                  string `json:"id"`
	ContextLength       int    `json:"context_length"`
	MaxTokens           int    `json:"max_tokens"`
	MaxCompletionTokens int    `json:"max_completion_tokens"`
}

// OpenRouterModelsResponse represents the OpenRouter API models response
type OpenRouterModelsResponse struct {
	Data []struct {
		ID                  string `json:"id"`
		Name                string `json:"name"`
		ContextLength       int    `json:"context_length"`
		MaxCompletionTokens int    `json:"max_completion_tokens"`
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
	} `json:"data"`
}

// MetadataService provides model metadata detection
type MetadataService struct {
	httpClient *http.Client
	cache      map[string]*ModelMetadata
	cacheTTL   time.Duration
}

// NewMetadataService creates a new metadata service
func NewMetadataService(timeout time.Duration, cacheTTL time.Duration) *MetadataService {
	return &MetadataService{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		cache:    make(map[string]*ModelMetadata),
		cacheTTL: cacheTTL,
	}
}

// GetModelContextLength retrieves the context length for a given model
func (ms *MetadataService) GetModelContextLength(provider, model, baseURL string) (int, error) {
	cacheKey := fmt.Sprintf("%s:%s:%s", provider, model, baseURL)

	// Check cache first
	if cached, exists := ms.cache[cacheKey]; exists {
		return cached.ContextLength, nil
	}

	var metadata *ModelMetadata
	var err error

	switch provider {
	case "openai":
		if isOpenRouterURL(baseURL) {
			metadata, err = ms.fetchOpenRouterMetadata(model, baseURL)
		} else {
			metadata, err = ms.fetchOpenAIMetadata(model, baseURL)
		}
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
	ms.cache[cacheKey] = metadata

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

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization if available
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response OpenRouterModelsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Find the requested model
	for _, modelData := range response.Data {
		if modelData.ID == model || strings.HasSuffix(modelData.ID, "/"+model) {
			return &ModelMetadata{
				ID:                  modelData.ID,
				ContextLength:       modelData.ContextLength,
				MaxCompletionTokens: modelData.MaxCompletionTokens,
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

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authorization if available
	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response OpenAIModelsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Find the requested model
	for _, modelData := range response.Data {
		if modelData.ID == model {
			return &ModelMetadata{
				ID:            modelData.ID,
				ContextLength: modelData.MaxTokens,
				MaxTokens:     modelData.MaxTokens,
			}, nil
		}
	}

	return nil, fmt.Errorf("model %s not found", model)
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
	if apiKey := os.Getenv("GOOGLE_API_KEY"); apiKey != "" {
		if strings.Contains(apiURL, "?") {
			apiURL += "&key=" + apiKey
		} else {
			apiURL += "?key=" + apiKey
		}
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var gm GoogleModelResponse
	if err := json.Unmarshal(body, &gm); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
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

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	// Required version header
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	var r anthropicModelsResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
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

	req, err := http.NewRequestWithContext(context.Background(), "POST", apiURL, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch model: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	var r ollamaShowResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
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

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response OpenRouterModelsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var models []*ModelMetadata
	for _, modelData := range response.Data {
		models = append(models, &ModelMetadata{
			ID:                  modelData.ID,
			ContextLength:       modelData.ContextLength,
			MaxCompletionTokens: modelData.MaxCompletionTokens,
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

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response OpenAIModelsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var models []*ModelMetadata
	for _, modelData := range response.Data {
		models = append(models, &ModelMetadata{
			ID:            modelData.ID,
			ContextLength: modelData.MaxTokens,
			MaxTokens:     modelData.MaxTokens,
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

	if apiKey := os.Getenv("GOOGLE_API_KEY"); apiKey != "" {
		if strings.Contains(apiURL, "?") {
			apiURL += "&key=" + apiKey
		} else {
			apiURL += "?key=" + apiKey
		}
	}

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Google returns a different structure for the models list
	var response struct {
		Models []GoogleModelResponse `json:"models"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
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
		modelID := model.Name
		if strings.HasPrefix(modelID, "models/") {
			modelID = strings.TrimPrefix(modelID, "models/")
		}

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

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var r anthropicModelsResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
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
		Name         string `json:"name"`
		Model        string `json:"model"`
		ModifiedAt   string `json:"modified_at"`
		Size         int64  `json:"size"`
		Digest       string `json:"digest"`
		Details      map[string]interface{} `json:"details"`
	} `json:"models"`
}

// fetchOllamaModelList fetches the list of available models from Ollama
func (ms *MetadataService) fetchOllamaModelList(baseURL string) ([]*ModelMetadata, error) {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	serverURL := strings.TrimSuffix(baseURL, "/api")
	apiURL := strings.TrimSuffix(serverURL, "/") + "/api/tags"

	req, err := http.NewRequestWithContext(context.Background(), "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ms.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var r OllamaModelsResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
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
