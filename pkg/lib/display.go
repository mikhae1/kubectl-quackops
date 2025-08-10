package lib

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/tmc/langchaingo/llms"
)

// KubeCtxInfo shows the user which Kubernetes context is currently active
func KubeCtxInfo(cfg *config.Config) error {
	// Execute the context command directly without going through the normal flow
	// that shows output to the user
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	// Get current context
	contextCmd := exec.CommandContext(ctx, cfg.KubectlBinaryPath, "config", "current-context")
	contextOutput, err := contextCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error getting current context: %w", err)
	}
	ctxName := strings.TrimSpace(string(contextOutput))

	// Get cluster info
	clusterCmd := exec.CommandContext(ctx, cfg.KubectlBinaryPath, "cluster-info")
	clusterOutput, err := clusterCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error getting cluster info: %w", err)
	}

	info := strings.TrimSpace(string(clusterOutput))
	if info == "" {
		fmt.Println(color.HiRedString("Current Kubernetes context is empty or not set."))
	} else {
		infoLines := strings.Split(info, "\n")
		fmt.Printf(color.HiYellowString("Using Kubernetes context")+": %s\n%s", ctxName, strings.Join(infoLines[:len(infoLines)-1], "\n"))
	}

	return nil
}

// CountTokens counts the token usage for a given text string and/or chat messages
// The function combines both tokens from text and the given messages
func CountTokens(text string, messages []llms.ChatMessage) int {
	tokenCount := 0

	// Count tokens in the provided text if it's not empty
	if text != "" {
		tokenCount = len(Tokenize(text))
	}

	// Count tokens in the provided messages
	if len(messages) > 0 {
		for _, message := range messages {
			tokenCount += len(Tokenize(message.GetContent()))
		}
	}

	return tokenCount
}

// CalculateContextPercentage calculates the percentage of context window used
func CalculateContextPercentage(cfg *config.Config) float64 {
	if cfg.MaxTokens == 0 {
		return 0.0
	}

	// Model-aware estimation
	currentTokens := CountTokensWithConfig(cfg, "", cfg.ChatMessages)
	percentage := (float64(currentTokens) / float64(cfg.MaxTokens)) * 100

	// Cap at 100% to avoid display issues
	if percentage > 100 {
		percentage = 100
	}

	return percentage
}

// FormatContextPrompt formats the prompt with context percentage
func FormatContextPrompt(cfg *config.Config, isCommand bool) string {
	percentage := CalculateContextPercentage(cfg)

	// Choose color based on context usage
	var contextColor *color.Color
	if percentage < 50 {
		contextColor = color.New(color.FgGreen)
	} else if percentage < 80 {
		contextColor = color.New(color.FgYellow)
	} else {
		contextColor = color.New(color.FgRed)
	}

	// Build compact context indicator with colored arrows and compact numbers
	// Example: [3%|↑2.9k|↓2.0k]
	leftBracket := color.New(color.FgHiBlack).Sprint("[")
	rightBracket := color.New(color.FgHiBlack).Sprint("]")
	pctStr := contextColor.Sprintf("%.0f%%", percentage)
	sep := color.New(color.FgHiBlack).Sprint("|")

	tokenStr := ""
	if cfg.LastOutgoingTokens > 0 || cfg.LastIncomingTokens > 0 {
		up := color.New(color.FgHiYellow, color.Bold).Sprint("↑")
		down := color.New(color.FgHiCyan, color.Bold).Sprint("↓")
		outNum := color.New(color.FgHiYellow).Sprint(FormatCompactNumber(cfg.LastOutgoingTokens))
		inNum := color.New(color.FgHiCyan).Sprint(FormatCompactNumber(cfg.LastIncomingTokens))
		tokenStr = fmt.Sprintf("%s%s%s%s%s", sep, up, outNum, sep, down+inNum)
	}
	contextStr := fmt.Sprintf("%s%s%s%s", leftBracket, pctStr, tokenStr, rightBracket)

	// Format the main prompt
	var promptStr string
	if isCommand {
		promptStr = color.New(color.FgHiRed, color.Bold).Sprint("$ ❯ ")
	} else {
		promptStr = color.New(color.Bold).Sprint("❯ ")
	}

	return contextStr + " " + promptStr
}
