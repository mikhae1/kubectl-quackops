package lib

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/tmc/langchaingo/llms"
	"golang.org/x/term"
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

// GetKubeContextInfo returns the current Kubernetes context name and cluster-info
// lines without printing them. The trailing diagnostic hint line from
// `kubectl cluster-info` is omitted to keep the output concise.
func GetKubeContextInfo(cfg *config.Config) (string, []string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	// Get current context
	contextCmd := exec.CommandContext(ctx, cfg.KubectlBinaryPath, "config", "current-context")
	contextOutput, err := contextCmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("error getting current context: %w", err)
	}
	ctxName := strings.TrimSpace(string(contextOutput))

	// Get cluster info
	clusterCmd := exec.CommandContext(ctx, cfg.KubectlBinaryPath, "cluster-info")
	clusterOutput, err := clusterCmd.CombinedOutput()
	if err != nil {
		return ctxName, nil, fmt.Errorf("error getting cluster info: %w", err)
	}

	info := strings.TrimSpace(string(clusterOutput))
	if info == "" {
		return ctxName, nil, nil
	}
	lines := strings.Split(info, "\n")
	if len(lines) > 0 {
		// Drop the last line which usually contains the 'cluster-info dump' hint
		lines = lines[:len(lines)-1]
	}
	return ctxName, lines, nil
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
	if cfg == nil {
		return 0.0
	}

	// Prefer the most recent outgoing tokens for the last request; fall back to
	// history-only estimate when not available (e.g., initial prompt render).
	currentTokens := cfg.LastOutgoingTokens
	if currentTokens <= 0 && len(cfg.ChatMessages) > 0 {
		// Avoid a potentially heavy initial token count on the first render
		currentTokens = CountTokensWithConfig(cfg, "", cfg.ChatMessages)
	}

	maxWindow := EffectiveMaxTokens(cfg)
	if maxWindow <= 0 {
		return 0.0
	}

	percentage := (float64(currentTokens) / float64(maxWindow)) * 100

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
		contextColor = color.New(color.FgHiCyan)
	} else if percentage < 80 {
		contextColor = color.New(color.FgYellow)
	} else {
		contextColor = color.New(color.FgHiRed)
	}

	// Build compact context indicator with colored arrows and compact numbers
	// Example: [3%|↑2.9k|↓2.0k]
	leftBracket := color.New(color.FgHiBlack).Sprint("[")
	rightBracket := color.New(color.FgHiBlack).Sprint("]")
	pctStr := contextColor.Sprintf("%3.0f%%", percentage)
	sep := color.New(color.FgHiBlack).Sprint("|")

	tokenStr := ""
	if cfg.LastOutgoingTokens > 0 || cfg.LastIncomingTokens > 0 {
		up := color.New(color.FgHiBlue, color.Bold).Sprint("↑")
		down := color.New(color.FgHiGreen, color.Bold).Sprint("↓")
		outNum := contextColor.Sprint(FormatCompactNumber(cfg.LastOutgoingTokens))
		inNum := contextColor.Sprint(FormatCompactNumber(cfg.LastIncomingTokens))
		tokenStr = fmt.Sprintf("%s%s%s%s%s", sep, up, outNum, sep, down+inNum)
	}
	contextStr := fmt.Sprintf("%s%s%s%s", leftBracket, pctStr, tokenStr, rightBracket)

	// Format the main prompt
	var promptStr string
	if isCommand {
		prefix := "$"
		if cfg != nil && strings.TrimSpace(cfg.CommandPrefix) != "" {
			prefix = cfg.CommandPrefix
		}
		promptStr = color.New(color.FgHiRed, color.Bold).Sprint(prefix + " ❯ ")
	} else {
		promptStr = color.New(color.Bold).Sprint("❯ ")
	}

	return contextStr + " " + promptStr
}

// FormatEditPrompt returns the edit-mode prompt string without any token counter
// or context indicator. This is used when the user toggles persistent edit mode
// by pressing '$'.
func FormatEditPromptWith(cfg *config.Config) string {
	prefix := "$"
	if cfg != nil && strings.TrimSpace(cfg.CommandPrefix) != "" {
		prefix = cfg.CommandPrefix
	}
	return color.New(color.FgHiRed, color.Bold).Sprint(prefix + " ❯ ")
}

// getTerminalSize returns the width and height of the terminal
func getTerminalSize() (int, int) {
	// Try to get terminal size using golang.org/x/term
	if width, height, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		return width, height
	}

	// Fallback: try environment variables
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if width, err := strconv.Atoi(cols); err == nil {
			if rows := os.Getenv("LINES"); rows != "" {
				if height, err := strconv.Atoi(rows); err == nil {
					return width, height
				}
			}
		}
	}

	// Fallback: try tput command
	if cmd := exec.Command("tput", "cols"); cmd != nil {
		if output, err := cmd.Output(); err == nil {
			if width, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
				if cmd := exec.Command("tput", "lines"); cmd != nil {
					if output, err := cmd.Output(); err == nil {
						if height, err := strconv.Atoi(strings.TrimSpace(string(output))); err == nil {
							return width, height
						}
					}
				}
			}
		}
	}

	// Final fallback to reasonable defaults
	return 80, 24
}

// FormatPromptHighlight formats an MCP prompt path with yellow background
// for visual distinction in the input line
// Format: /$server/$prompt
func FormatPromptHighlight(promptPath string) string {
	if promptPath == "" {
		return ""
	}
	// Yellow background with black text for high visibility
	promptColor := color.New(color.BgYellow, color.FgBlack, color.Bold)
	if !strings.HasPrefix(promptPath, "/") {
		promptPath = "/" + promptPath
	}
	return promptColor.Sprint(promptPath)
}

// FormatInputWithPrompt formats user input, highlighting the prompt part if present
// Returns the formatted string for display
// Now handles format /$server/$prompt
func FormatInputWithPrompt(input string, promptName string, serverName string) string {
	if promptName == "" {
		return input
	}

	// Build the full prompt path
	var promptPath string
	if serverName != "" {
		promptPath = "/" + serverName + "/" + promptName
	} else {
		promptPath = "/" + promptName
	}

	// Find the prompt in the input and highlight it
	if !strings.HasPrefix(strings.ToLower(input), strings.ToLower(promptPath)) {
		return input
	}

	// Split into prompt and user query parts
	rest := input[len(promptPath):]
	highlighted := FormatPromptHighlight(promptPath)

	return highlighted + rest
}

// CoolClearEffect creates an animated clearing effect for the terminal
func CoolClearEffect(cfg *config.Config) {
	if cfg.DisableAnimation {
		// Just clear immediately if animations are disabled
		fmt.Print("\033[2J\033[H")
		return
	}

	// Get actual terminal dimensions
	width, height := getTerminalSize()

	// Colors for the effect
	cyan := color.New(color.FgHiCyan)
	blue := color.New(color.FgHiBlue)
	magenta := color.New(color.FgHiMagenta)
	colors := []*color.Color{cyan, blue, magenta}

	// Matrix-style clearing effect
	fmt.Print("\033[2J") // Clear screen first

	// Animate clearing from top to bottom with colored "dust"
	for row := 0; row < height; row++ {
		fmt.Printf("\033[%d;1H", row+1) // Move cursor to row

		// Create a line of random characters fading away
		for col := 0; col < width; col++ {
			if rand.Float32() < 0.3 { // 30% chance to show a character
				char := []rune("⠁⠂⠄⡀⢀⠠⠐⠈")[rand.Intn(8)] // Braille dots for effect
				c := colors[rand.Intn(len(colors))]
				fmt.Print(c.Sprint(string(char)))
			} else {
				fmt.Print(" ")
			}
		}

		time.Sleep(time.Millisecond * 25) // Speed of the effect
	}

	// Final clear and show completion message with duck
	fmt.Print("\033[2J\033[H")

	// Show a quick "CLEARED" message with duck centered on screen
	duck := color.New(color.FgHiYellow)
	cleared := color.New(color.FgHiGreen, color.Bold)

	// Center the message
	centerRow := height / 2
	message := "🦆 CLEARED 🦆"
	centerCol := (width - len(message)) / 2
	if centerCol < 0 {
		centerCol = 0
	}

	fmt.Printf("\033[%d;%dH", centerRow, centerCol)
	fmt.Print(duck.Sprint("🦆 ") + cleared.Sprint("CLEARED") + duck.Sprint(" 🦆"))
	time.Sleep(time.Millisecond * 500)

	// Final clear
	fmt.Print("\033[2J\033[H")
}
