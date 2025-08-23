package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ModelMetadata holds information about a model's capabilities
type ModelMetadata struct {
	ID                    string `json:"id"`
	ContextLength         int    `json:"context_length"`
	MaxTokens             int    `json:"max_tokens"`
	MaxCompletionTokens   int    `json:"max_completion_tokens"`
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

// isOpenRouterURL checks if the base URL belongs to OpenRouter
func isOpenRouterURL(baseURL string) bool {
	return strings.Contains(strings.ToLower(baseURL), "openrouter.ai")
}