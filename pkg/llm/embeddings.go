package llm

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"

	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// GoogleEmbedder is a wrapper around the GoogleAI client that provides embedding functionality
type GoogleEmbedder struct {
	client *googleai.GoogleAI
	model  string
}

// EmbedDocuments implements the Embedder interface for GoogleEmbedder
func (g *GoogleEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	// Use the native embedding functionality of googleai package
	embeddings, err := g.client.CreateEmbedding(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("error creating embeddings: %w", err)
	}
	return embeddings, nil
}

// EmbedQuery implements the Embedder interface for GoogleEmbedder
func (g *GoogleEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	// Use the CreateEmbedding function with a single text
	embeddings, err := g.client.CreateEmbedding(ctx, []string{text})
	if err != nil {
		return nil, fmt.Errorf("error creating query embedding: %w", err)
	}

	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return embeddings[0], nil
}

// GetEmbedder creates an embedder based on the provider configuration
func GetEmbedder(cfg *config.Config) (embeddings.Embedder, error) {
	logger.Log("info", "Creating embedder for %s provider", cfg.Provider)

	// Provider-specific embedding handling
	switch cfg.Provider {
	case "openai":
		// OpenAI has the best embedding models
		if os.Getenv("OPENAI_API_KEY") != "" {
			// Set the API key as an env var which is how langchaingo accesses it
			openaiOpts := []openai.Option{
				openai.WithModel(cfg.EmbeddingModel), // Use the configured embedding model
			}
			if baseURL := os.Getenv("QU_OPENAI_BASE_URL"); baseURL != "" {
				openaiOpts = append(openaiOpts, openai.WithBaseURL(baseURL))
			}
			embedClient, err := openai.New(openaiOpts...)
			if err != nil {
				logger.Log("warn", "Failed to create OpenAI embedder: %v", err)
			} else {
				logger.Log("info", "Using OpenAI embeddings model: %s", cfg.EmbeddingModel)
				return embeddings.NewEmbedder(embedClient)
			}
		}

	case "ollama":
		// Ollama has built-in embedding capability
		serverURL := strings.TrimSuffix(cfg.OllamaApiURL, "/api")
		// Try to use a dedicated embedding model if available, otherwise fall back to the configured model
		embeddingModels := strings.Split(cfg.OllamaEmbeddingModels, ",")

		// First try using a dedicated embedding model
		for _, embModel := range embeddingModels {
			embModel = strings.TrimSpace(embModel)
			if embModel == "" {
				continue
			}

			ollamaClient, err := ollama.New(
				ollama.WithModel(embModel),
				ollama.WithServerURL(serverURL),
			)
			if err == nil {
				logger.Log("info", "Using Ollama embeddings model: %s", embModel)
				return embeddings.NewEmbedder(ollamaClient)
			}
		}

		// Fall back to using the configured model
		ollamaClient, err := ollama.New(
			ollama.WithModel(cfg.Model),
			ollama.WithServerURL(serverURL),
		)
		if err == nil {
			logger.Log("info", "Using Ollama LLM model for embeddings: %s", cfg.Model)
			return embeddings.NewEmbedder(ollamaClient)
		}

		logger.Log("warn", "Failed to create Ollama embedder: %v", err)

	case "google":
		// Google has embedding models through their embedding API
		if os.Getenv("GOOGLE_API_KEY") != "" {
			// Initialize Google AI client
			ctx := context.Background()

			// Use the configured embedding model
			embeddingModel := cfg.EmbeddingModel
			googleClient, err := googleai.New(ctx,
				googleai.WithAPIKey(os.Getenv("GOOGLE_API_KEY")),
				googleai.WithDefaultEmbeddingModel(embeddingModel),
			)

			if err != nil {
				logger.Log("warn", "Failed to create Google AI client: %v", err)
			} else {
				logger.Log("info", "Using Google AI embedding model: %s", embeddingModel)
				// Return custom Google embedder
				return &GoogleEmbedder{
					client: googleClient,
					model:  embeddingModel,
				}, nil
			}
		} else {
			logger.Log("warn", "Google API key not found in environment")
		}

	case "anthropic":
		// Anthropic doesn't have specialized embedding models in langchaingo yet
		// We'll fall back to other providers
		logger.Log("info", "Anthropic embedding model not directly supported, falling back")
	}

	// Fallback logic - try each major embedding provider in order

	// 1. Try OpenAI's dedicated embedding model as primary fallback
	if os.Getenv("OPENAI_API_KEY") != "" {
		openaiOpts := []openai.Option{openai.WithModel(cfg.EmbeddingModel)}
		if baseURL := os.Getenv("QU_OPENAI_BASE_URL"); baseURL != "" {
			openaiOpts = append(openaiOpts, openai.WithBaseURL(baseURL))
		}
		embedClient, err := openai.New(openaiOpts...)
		if err == nil {
			logger.Log("info", "Using fallback OpenAI embeddings model: %s", cfg.EmbeddingModel)
			return embeddings.NewEmbedder(embedClient)
		}
		logger.Log("warn", "Failed to create fallback OpenAI embedder: %v", err)
	}

	// 2. Try Google AI as another fallback if available
	if os.Getenv("GOOGLE_API_KEY") != "" {
		ctx := context.Background()
		embeddingModel := cfg.EmbeddingModel
		googleClient, err := googleai.New(ctx,
			googleai.WithAPIKey(os.Getenv("GOOGLE_API_KEY")),
			googleai.WithDefaultEmbeddingModel(embeddingModel),
		)

		if err == nil {
			logger.Log("info", "Using fallback Google AI embedding model: %s", embeddingModel)
			return &GoogleEmbedder{
				client: googleClient,
				model:  embeddingModel,
			}, nil
		}
		logger.Log("warn", "Failed to create fallback Google AI embedder: %v", err)
	}

	// 3. Try Ollama as another fallback option
	if cfg.OllamaApiURL != "" {
		serverURL := strings.TrimSuffix(cfg.OllamaApiURL, "/api")
		// Try standard embedding models
		embeddingModels := strings.Split(cfg.OllamaEmbeddingModels, ",")

		for _, embModel := range embeddingModels {
			embModel = strings.TrimSpace(embModel)
			if embModel == "" {
				continue
			}

			ollamaClient, err := ollama.New(
				ollama.WithModel(embModel),
				ollama.WithServerURL(serverURL),
			)
			if err == nil {
				logger.Log("info", "Using fallback Ollama embeddings model: %s", embModel)
				return embeddings.NewEmbedder(ollamaClient)
			}
		}

		// Try with Llama3 if available as a last resort
		for _, model := range []string{"llama3.1", "llama3", "llama2"} {
			ollamaClient, err := ollama.New(
				ollama.WithModel(model),
				ollama.WithServerURL(serverURL),
			)
			if err == nil {
				logger.Log("info", "Using fallback Ollama LLM model for embeddings: %s", model)
				return embeddings.NewEmbedder(ollamaClient)
			}
		}
	}

	// 4. Last resort - if we couldn't create any embedder, use a simple implementation
	// that performs basic word matching
	logger.Log("warn", "No suitable embedder found, using simple keyword matcher")
	return createSimpleEmbedder(), nil
}

// createSimpleEmbedder creates a basic embedder that uses word overlap
// as a fallback when proper embedding models aren't available
func createSimpleEmbedder() embeddings.Embedder {
	return &simpleEmbedder{}
}

// simpleEmbedder implements a basic embedding approach using word frequency
// as a fallback when no proper embedding models are available
type simpleEmbedder struct{}

// EmbedDocuments implements the Embedder interface for simpleEmbedder
func (s *simpleEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		// Simple frequency-based fallback embedding
		embeddings[i] = createSimpleEmbedding(text)
	}
	return embeddings, nil
}

// EmbedQuery implements the Embedder interface for simpleEmbedder
func (s *simpleEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	return createSimpleEmbedding(text), nil
}

// createSimpleEmbedding: 300-dim frequency-based vector (fallback)
func createSimpleEmbedding(text string) []float32 {
	// 300 dimensions
	embedding := make([]float32, 300)

	// Normalize and tokenize text
	text = strings.ToLower(text)
	words := strings.Fields(text)

	// Count word frequencies
	wordFreq := make(map[string]int)
	for _, word := range words {
		wordFreq[word]++
	}

	// Hash-based projection; not semantically meaningful, but consistent
	for word, freq := range wordFreq {
		// Simple hash of the word
		var hash uint32
		for i, char := range word {
			hash += uint32(char) * uint32(i+1)
		}

		// Use the hash to determine which dimensions to update
		dim1 := hash % 100
		dim2 := (hash / 100) % 100
		dim3 := (hash / 10000) % 100

		// Update the dimensions based on frequency
		embedding[dim1] += float32(freq)
		embedding[dim2] += float32(freq) * 0.5
		embedding[dim3] += float32(freq) * 0.25
	}

	// Normalize the embedding vector
	var sum float32
	for _, val := range embedding {
		sum += val * val
	}

	if sum > 0 {
		length := float32(math.Sqrt(float64(sum)))
		for i := range embedding {
			embedding[i] /= length
		}
	}

	return embedding
}

// trimSectionsWithLangChainGo uses langchaingo embeddings to select the most semantically relevant sections within a token budget.
func TrimSectionsWithEmbeddings(ctx context.Context, embedder embeddings.Embedder, sections []string, prompt string, maxTokens int) []string {
	// Embed the query prompt
	promptEmb, err := embedder.EmbedQuery(ctx, prompt)
	if err != nil {
		// Fallback to the first section if embedding fails
		logger.Log("warn", "Error embedding prompt: %v", err)
		return sections[:1]
	}

	type scoredSection struct {
		text  string
		score float64
		index int // Keep track of original index for stable sorting of equal scores
	}

	var scored []scoredSection
	// Score each section by cosine similarity to the prompt
	for i, sec := range sections {
		emb, err := embedder.EmbedQuery(ctx, sec)
		if err != nil {
			logger.Log("warn", "Error embedding section %d: %v", i, err)
			continue
		}
		score := lib.CosineSimilarity(promptEmb, emb)
		scored = append(scored, scoredSection{text: sec, score: score, index: i})
	}

	// Sort sections by descending score (and by original index if scores are equal)
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].index < scored[j].index // Stable sort
		}
		return scored[i].score > scored[j].score
	})

	var selected []string
	tokenCount := 0

	// First pass: add the most relevant sections until we hit the token limit
	for _, s := range scored {
		tokens := len(lib.Tokenize(s.text))
		if tokenCount+tokens > maxTokens {
			break
		}
		selected = append(selected, s.text)
		tokenCount += tokens
	}

	// If we couldn't add any sections due to token limits, at least include the most relevant one
	if len(selected) == 0 && len(scored) > 0 {
		selected = append(selected, scored[0].text)
	}

	return selected
}
