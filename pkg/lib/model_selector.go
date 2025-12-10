package lib

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ergochat/readline"
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

// formatPriceColored formats a price with color based on cost level
func formatPriceColored(price float64) string {
	if price == 0.0 {
		return config.Colors.Primary.Render("Free")
	}

	priceStr := FormatPrice(price)

	// Color based on price level
	if price > 0.00001 { // >$10/1M tokens
		return config.Colors.Error.Render(priceStr)
	} else if price > 0.000001 { // >$1/1M tokens
		return config.Colors.Warn.Render(priceStr)
	} else {
		return config.Colors.Primary.Render(priceStr)
	}
}

// formatColoredPricingDisplay creates a colored pricing display with fancy colored arrows
func formatColoredPricingDisplay(inputPrice, outputPrice float64) string {
	var coloredParts []string

	if inputPrice > 0.0 {
		// Use cyan/blue for input arrow to represent "input/incoming"
		fancyInputArrow := config.Colors.Info.Render("↑")
		coloredPrice := formatPriceColored(inputPrice)
		coloredParts = append(coloredParts, fancyInputArrow+coloredPrice)
	}

	if outputPrice > 0.0 {
		// Use magenta/purple for output arrow to represent "output/outgoing"
		fancyOutputArrow := config.Colors.Accent.Render("↓")
		coloredPrice := formatPriceColored(outputPrice)
		coloredParts = append(coloredParts, fancyOutputArrow+coloredPrice)
	}

	if len(coloredParts) == 0 {
		return ""
	}

	// Use dim color for the separator slash to keep it subtle
	separator := config.Colors.Dim.Render("/")
	return strings.Join(coloredParts, separator)
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
	rl, err := readline.NewFromConfig(&readline.Config{
		Prompt:       config.Colors.Info.Render("Choose a model") + " (or press Tab): ",
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
		line, err := rl.ReadLine()
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
			fmt.Printf("%s\n", config.Colors.Info.Render("Multiple matches found:"))
			for i, model := range partialMatches {
				// Format the number with accent color
				numStr := config.Colors.Info.Render(fmt.Sprintf("%d.", i+1))

				// Format model ID with info color
				modelStr := config.Colors.Model.Render(model.ID)

				contextStr := fmt.Sprintf("· %s", FormatCompactNumber(model.ContextLength))

				// Add pricing info if available
				pricingStr := ""
				if model.PricePretty != "" && model.PricePretty != "Free" {
					coloredPricing := formatColoredPricingDisplay(model.InputPrice, model.OutputPrice)
					if coloredPricing != "" {
						pricingStr = fmt.Sprintf("· %s", coloredPricing)
					}
				}

				if model.Description != "" {
					// Wrap long descriptions
					description := TrimText(model.Description, 80)
					descStr := config.Colors.Dim.Render(" - " + description)
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
			fmt.Printf("%s\n", config.Colors.Dim.Render("Please be more specific or type a number to select."))
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
	selectedStr := config.Colors.Info.Render("Selected:")
	modelStr := config.Colors.Accent.Render(model.ID)
	contextStr := config.Colors.Primary.Render(fmt.Sprintf("(context: %s)", FormatCompactNumber(model.ContextLength)))

	if model.Description != "" {
		descStr := " - " + config.Colors.Light.Render(model.Description)
		fmt.Printf("%s %s %s%s\n", selectedStr, modelStr, contextStr, descStr)
	} else {
		fmt.Printf("%s %s %s\n", selectedStr, modelStr, contextStr)
	}

	// Show detailed pricing information if available
	if model.PricePretty != "" && model.PricePretty != "Free" {
		// Create detailed pricing format with individual colored prices
		var priceParts []string
		if model.InputPrice > 0 {
			inputColored := formatPriceColored(model.InputPrice)
			priceParts = append(priceParts, fmt.Sprintf("Input: %s/1M tokens", inputColored))
		}
		if model.OutputPrice > 0 {
			outputColored := formatPriceColored(model.OutputPrice)
			priceParts = append(priceParts, fmt.Sprintf("Output: %s/1M tokens", outputColored))
		}

		if len(priceParts) > 0 {
			detailedPricing := strings.Join(priceParts, ", ")
			fmt.Printf("%s %s\n", config.Colors.Accent.Render("Pricing:"), detailedPricing)
		}
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
			completions = append(completions, []rune(mc.formatCompletion(remaining, model.ContextLength, model.PricePretty)))
		}
	}

	// Then add substring matches (full IDs)
	for _, model := range containsMatches {
		if len(completions) >= 15 {
			break
		}
		completions = append(completions, []rune(mc.formatCompletion(model.ID, model.ContextLength, model.PricePretty)))
	}

	if len(completions) >= 15 {
		completions = append(completions, []rune(" ... (more matches available)"))
	}

	return completions, len(lineStr)
}

// formatCompletion formats a single completion with context and pricing info
func (mc *modelCompleter) formatCompletion(modelText string, contextLength int, pricePretty string) string {
	contextInfo := config.Colors.Dim.Render(fmt.Sprintf("·%s", FormatCompactNumber(contextLength)))

	// Add pricing info if available
	if pricePretty != "" {
		pricingInfo := config.Colors.Dim.Render(fmt.Sprintf("·%s", pricePretty))
		return modelText + contextInfo + pricingInfo
	}

	return modelText + contextInfo
}

// formatCompletions formats multiple completions, optionally with context
func (mc *modelCompleter) formatCompletions(models []*metadata.ModelMetadata, withContext bool) [][]rune {
	var completions [][]rune
	for _, model := range models {
		if withContext {
			completions = append(completions, []rune(mc.formatCompletion(model.ID, model.ContextLength, model.PricePretty)))
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
