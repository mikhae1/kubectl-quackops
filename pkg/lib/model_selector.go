package lib

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/llm/metadata"
)

// ModelSelector provides an interactive model selection interface
type ModelSelector struct {
	cfg             *config.Config
	metadataService *metadata.MetadataService
}

// NewModelSelector creates a new model selector instance
func NewModelSelector(cfg *config.Config) *ModelSelector {
	return &ModelSelector{
		cfg:             cfg,
		metadataService: metadata.NewMetadataService(cfg.ModelMetadataTimeout, cfg.ModelMetadataCacheTTL),
	}
}

// SelectModel launches an interactive model selection menu
func (ms *ModelSelector) SelectModel() (string, error) {
	baseURL := ms.getBaseURL()

	// Fetch models from provider
	models, err := ms.metadataService.GetModelList(ms.cfg.Provider, baseURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch models for provider %s: %w", ms.cfg.Provider, err)
	}

	if len(models) == 0 {
		return "", fmt.Errorf("no models available for provider %s", ms.cfg.Provider)
	}

	// Sort models by name for consistent ordering
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	// Create readline instance with custom completer
	completer := &modelCompleter{models: models}
	rl, err := readline.NewEx(&readline.Config{
		Prompt:       "Type to search models: ",
		AutoComplete: completer,
		EOFPrompt:    "exit",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create interactive prompt: %w", err)
	}
	defer rl.Close()

	fmt.Printf("\nAvailable models for provider '%s':\n", strings.ToUpper(ms.cfg.Provider))
	fmt.Println("Type to search, or press Tab to see all models. Press Ctrl+C to cancel.")
	fmt.Println()

	for {
		line, err := rl.Readline()
		if err != nil { // This includes Ctrl+C, EOF
			return "", fmt.Errorf("selection cancelled")
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if user typed a number (for direct selection)
		if num, err := strconv.Atoi(line); err == nil && num >= 1 && num <= len(models) {
			return ms.selectModel(models[num-1])
		}

		lowerCleaned := strings.ToLower(line)

		// Exact (case-insensitive) match
		for _, model := range models {
			if strings.ToLower(model.ID) == lowerCleaned {
				return ms.selectModel(model)
			}
		}

		// If input contains a full model ID anywhere (e.g., "2<id> (context: 1M)")
		var containsIDMatches []*metadata.ModelMetadata
		for _, model := range models {
			if strings.Contains(lowerCleaned, strings.ToLower(model.ID)) {
				containsIDMatches = append(containsIDMatches, model)
			}
		}
		if len(containsIDMatches) == 1 {
			return ms.selectModel(containsIDMatches[0])
		}
		if len(containsIDMatches) > 1 {
			// Prefer the longest ID to disambiguate
			longest := containsIDMatches[0]
			for _, m := range containsIDMatches[1:] {
				if len(m.ID) > len(longest.ID) {
					longest = m
				}
			}
			return ms.selectModel(longest)
		}

		// Fallback: partial match search
		var partialMatches []*metadata.ModelMetadata
		for _, model := range models {
			if strings.Contains(strings.ToLower(model.ID), lowerCleaned) {
				partialMatches = append(partialMatches, model)
			}
		}
		if len(partialMatches) == 1 {
			return ms.selectModel(partialMatches[0])
		}
		if len(partialMatches) > 1 {
			fmt.Printf("Multiple matches found:\n")
			for i, model := range partialMatches {
				fmt.Printf("  %d. %s (context: %s)\n", i+1, model.ID, FormatCompactNumber(model.ContextLength))
			}
			fmt.Printf("Please be more specific or type a number to select.\n")
			continue
		}

		fmt.Printf("No model found matching '%s'. Try a different search or press Tab to see all models.\n", line)
	}
}

// getBaseURL determines the base URL for the current provider
func (ms *ModelSelector) getBaseURL() string {
	switch ms.cfg.Provider {
	case "openai":
		if baseURL := os.Getenv("QU_OPENAI_BASE_URL"); baseURL != "" {
			return baseURL
		}
		if strings.Contains(ms.cfg.Model, "/") || strings.Contains(ms.cfg.Model, "openrouter") {
			return "https://openrouter.ai/api/v1"
		}
		return "https://api.openai.com"
	case "google":
		if baseURL := os.Getenv("QU_GOOGLE_BASE_URL"); baseURL != "" {
			return baseURL
		}
		return "https://generativelanguage.googleapis.com"
	case "anthropic":
		if baseURL := os.Getenv("QU_ANTHROPIC_BASE_URL"); baseURL != "" {
			return baseURL
		}
		return "https://api.anthropic.com"
	case "ollama":
		if baseURL := ms.cfg.OllamaApiURL; baseURL != "" {
			return baseURL
		}
		return "http://localhost:11434"
	default:
		return ""
	}
}

// selectModel prints selection info and returns the model ID
func (ms *ModelSelector) selectModel(model *metadata.ModelMetadata) (string, error) {
	fmt.Printf("Selected: %s (context: %s)\n", model.ID, FormatCompactNumber(model.ContextLength))
	return model.ID, nil
}

// modelCompleter implements readline.AutoCompleter for model search
type modelCompleter struct {
	models []*metadata.ModelMetadata
}

// Do implements the AutoCompleter interface
func (mc *modelCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	lineStr := strings.ToLower(string(line[:pos]))

	// If line is empty, show numbered list of all models
	if lineStr == "" {
		return mc.formatCompletions(mc.models[:min(20, len(mc.models))], false), 0
	}

	// Find models that start with the typed text (prefix matches)
	var prefixMatches []*metadata.ModelMetadata
	var containsMatches []*metadata.ModelMetadata

	for _, model := range mc.models {
		modelLower := strings.ToLower(model.ID)
		if strings.HasPrefix(modelLower, lineStr) {
			prefixMatches = append(prefixMatches, model)
		} else if strings.Contains(modelLower, lineStr) {
			containsMatches = append(containsMatches, model)
		}
	}

	var completions [][]rune

	// Show prefix matches first (suffix only)
	for _, model := range prefixMatches {
		if len(completions) >= 15 {
			break
		}
		remaining := model.ID[len(lineStr):]
		if remaining != "" {
			completions = append(completions, []rune(mc.formatCompletion(remaining, model.ContextLength)))
		}
	}

	// Then add substring matches (full IDs)
	for _, model := range containsMatches {
		if len(completions) >= 15 {
			break
		}
		completions = append(completions, []rune(mc.formatCompletion(model.ID, model.ContextLength)))
	}

	if len(completions) >= 15 {
		completions = append(completions, []rune(" ... (more matches available)"))
	}

	return completions, len(lineStr)
}

// formatCompletion formats a single completion with context info
func (mc *modelCompleter) formatCompletion(modelText string, contextLength int) string {
	dimmed := color.New(color.FgHiBlack)
	contextInfo := dimmed.Sprintf("·%s", FormatCompactNumber(contextLength))
	return modelText + contextInfo
}

// formatCompletions formats multiple completions, optionally with context
func (mc *modelCompleter) formatCompletions(models []*metadata.ModelMetadata, withContext bool) [][]rune {
	var completions [][]rune
	for _, model := range models {
		if withContext {
			completions = append(completions, []rune(mc.formatCompletion(model.ID, model.ContextLength)))
		} else {
			completions = append(completions, []rune(model.ID))
		}
	}
	return completions
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
