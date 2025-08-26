package lib

import (
	"fmt"
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
	baseURL := config.GetProviderBaseURL(ms.cfg)

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
		Prompt:       config.Colors.Info.Sprint("Choose a model") + " (or press Tab): ",
		AutoComplete: completer,
		EOFPrompt:    "exit",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create interactive prompt: %w", err)
	}
	defer rl.Close()

	fmt.Printf("\nAvailable models for provider '%s':\n", strings.ToUpper(ms.cfg.Provider))
	fmt.Println("Type and press Enter to search, press Tab for auto-complete. Press Ctrl+C to cancel.")
	fmt.Println()

	var lastPartialMatches []*metadata.ModelMetadata

	for {
		line, err := rl.Readline()
		if err != nil { // This includes Ctrl+C, EOF
			return "", fmt.Errorf("selection cancelled")
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if user typed a number (for direct selection from last displayed list)
		if num, err := strconv.Atoi(line); err == nil {
			if len(lastPartialMatches) > 0 && num >= 1 && num <= len(lastPartialMatches) {
				return ms.selectModel(lastPartialMatches[num-1])
			} else if num >= 1 && num <= len(models) {
				return ms.selectModel(models[num-1])
			}
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
			lastPartialMatches = partialMatches // Store for numeric selection
			fmt.Printf("%s\n", config.Colors.Info.Sprint("Multiple matches found:"))
			for i, model := range partialMatches {
				// Format the number with accent color
				numStr := config.Colors.Info.Sprintf("%d.", i+1)

				// Format model ID with info color
				modelStr := config.Colors.Model.Sprint(model.ID)

				contextStr := fmt.Sprintf("· %s", FormatCompactNumber(model.ContextLength))

				// Add pricing info if available
				pricingStr := ""
				pricing := FormatPricingInfo(model.PromptPrice, model.CompletionPrice, false)
				if pricing != "" {
					// Color pricing based on cost level
					var pricingColor *color.Color
					if model.PromptPrice > 0.00001 { // >$10/1M tokens
						pricingColor = config.Colors.Error
					} else if model.PromptPrice > 0.000001 { // >$1/1M tokens
						pricingColor = config.Colors.Warn
					} else {
						pricingColor = config.Colors.Primary
					}
					pricingStr = pricingColor.Sprintf("· %s", pricing)
				}

				if model.Description != "" {
					// Wrap long descriptions
					description := TrimText(model.Description, 80)
					descStr := config.Colors.Dim.Sprint(" - " + description)
					if pricingStr != "" {
						fmt.Printf("  %s %s %s %s%s\n", numStr, modelStr, contextStr, pricingStr, descStr)
					} else {
						fmt.Printf("  %s %s %s%s\n", numStr, modelStr, contextStr, descStr)
					}
				} else {
					if pricingStr != "" {
						fmt.Printf("  %s %s %s %s\n", numStr, modelStr, contextStr, pricingStr)
					} else {
						fmt.Printf("  %s %s %s\n", numStr, modelStr, contextStr)
					}
				}
			}
			fmt.Printf("%s\n", config.Colors.Dim.Sprint("Please be more specific or type a number to select."))
			continue
		} else {
			lastPartialMatches = nil // Clear if no partial matches
		}

		fmt.Printf("No model found matching '%s'. Try a different search or press Tab to see all models.\n", line)
	}
}

// getBaseURL centralized in config.GetProviderBaseURL

// selectModel prints selection info and returns the model ID
func (ms *ModelSelector) selectModel(model *metadata.ModelMetadata) (string, error) {
	// Format with colors
	selectedStr := config.Colors.Info.Sprint("Selected:")
	modelStr := config.Colors.Accent.Sprint(model.ID)
	contextStr := config.Colors.Primary.Sprintf("(context: %s)", FormatCompactNumber(model.ContextLength))

	if model.Description != "" {
		descStr := " - " + config.Colors.Light.Sprint(model.Description)
		fmt.Printf("%s %s %s%s\n", selectedStr, modelStr, contextStr, descStr)
	} else {
		fmt.Printf("%s %s %s\n", selectedStr, modelStr, contextStr)
	}

	// Show detailed pricing information if available
	pricingInfo := FormatPricingInfo(model.PromptPrice, model.CompletionPrice, true)
	if pricingInfo != "" {
		var pricingColor *color.Color
		if model.PromptPrice > 0.00001 { // >$10/1M tokens
			pricingColor = config.Colors.Warn
		} else if model.PromptPrice > 0.000001 { // >$1/1M tokens
			pricingColor = config.Colors.Info
		} else {
			pricingColor = config.Colors.Ok
		}
		fmt.Printf("%s %s\n", config.Colors.Dim.Sprint("Pricing:"), pricingColor.Sprint(pricingInfo))
	}

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
		return mc.formatCompletions(mc.models[:min(20, len(mc.models))], true), 0
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
			completions = append(completions, []rune(mc.formatCompletion(remaining, model.ContextLength, model.PromptPrice, model.CompletionPrice)))
		}
	}

	// Then add substring matches (full IDs)
	for _, model := range containsMatches {
		if len(completions) >= 15 {
			break
		}
		completions = append(completions, []rune(mc.formatCompletion(model.ID, model.ContextLength, model.PromptPrice, model.CompletionPrice)))
	}

	if len(completions) >= 15 {
		completions = append(completions, []rune(" ... (more matches available)"))
	}

	return completions, len(lineStr)
}

// formatCompletion formats a single completion with context and pricing info
func (mc *modelCompleter) formatCompletion(modelText string, contextLength int, promptPrice, completionPrice float64) string {
	contextInfo := config.Colors.Dim.Sprintf("·%s", FormatCompactNumber(contextLength))

	// Add pricing info if available
	pricing := FormatPricingInfo(promptPrice, completionPrice, false)
	if pricing != "" {
		pricingInfo := config.Colors.Dim.Sprintf("·%s", pricing)
		return modelText + contextInfo + pricingInfo
	}

	return modelText + contextInfo
}

// formatCompletions formats multiple completions, optionally with context
func (mc *modelCompleter) formatCompletions(models []*metadata.ModelMetadata, withContext bool) [][]rune {
	var completions [][]rune
	for _, model := range models {
		if withContext {
			completions = append(completions, []rune(mc.formatCompletion(model.ID, model.ContextLength, model.PromptPrice, model.CompletionPrice)))
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
