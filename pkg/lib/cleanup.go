package lib

import (
	"fmt"
	"os"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/llm/metadata"
)

// CleanupOptions defines options for CleanupAndExit behavior.
type CleanupOptions struct {
	Message     string
	ExitCode    int
	CleanupFunc func()
}

// CleanupAndExit performs common cleanup, optionally shows cost estimation, and exits when ExitCode >= 0.
// If ExitCode < 0, it performs cleanup only (no exit). Always restores cursor visibility.
func CleanupAndExit(cfg *config.Config, opts CleanupOptions) {
	if opts.CleanupFunc != nil {
		opts.CleanupFunc()
	}

	// Always restore cursor visibility
	fmt.Print("\033[?25h")

	// Show cost estimation only for successful normal exits
	if opts.ExitCode == 0 && cfg != nil && (cfg.LastOutgoingTokens > 0 || cfg.LastIncomingTokens > 0) {
		showCostEstimation(cfg)
	}

	if opts.Message != "" {
		fmt.Println(opts.Message)
	}

	if opts.ExitCode >= 0 {
		os.Exit(opts.ExitCode)
	}
}

// showCostEstimation displays cost estimation information before exit.
func showCostEstimation(cfg *config.Config) {
	if cfg == nil || cfg.Model == "" {
		return
	}

	metadataService := metadata.NewMetadataService(cfg.ModelMetadataTimeout, cfg.ModelMetadataCacheTTL)
	baseURL := config.GetProviderBaseURL(cfg)
	models, err := metadataService.GetModelList(cfg.Provider, baseURL)
	if err != nil {
		return
	}

	var currentModel *metadata.ModelMetadata
	for _, model := range models {
		if model.ID == cfg.Model {
			currentModel = model
			break
		}
	}

	if currentModel == nil || (currentModel.InputPrice == 0 && currentModel.OutputPrice == 0) {
		return
	}

	summary := CalculateTotalCost(
		cfg.LastOutgoingTokens,
		cfg.LastIncomingTokens,
		currentModel.InputPrice,
		currentModel.OutputPrice,
		cfg.Model,
	)

	costDisplay := FormatTotalCostDisplay(summary)
	if costDisplay != "" {
		fmt.Println()
		fmt.Println(costDisplay)
		fmt.Println()
	}
}
