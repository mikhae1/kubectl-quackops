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
// Shows: [▰▰▰▱▱▱▱▱] 38% ↑2.9k↓2.0k ❯
// The progress bar and context info appear before the prompt chevron
func FormatContextPrompt(cfg *config.Config, isCommand bool) string {
	// Build the compact context indicator (left side, before prompt)
	contextIndicator := FormatContextIndicator(cfg)

	// Format the main prompt
	var promptStr string
	if isCommand {
		prefix := "!"
		if cfg != nil && strings.TrimSpace(cfg.CommandPrefix) != "" {
			prefix = cfg.CommandPrefix
		}
		promptStr = color.New(color.FgHiRed, color.Bold).Sprint(prefix + " ❯ ")
	} else {
		promptStr = color.New(color.Bold).Sprint("❯ ")
	}

	return contextIndicator + " " + promptStr
}

// FormatContextIndicator creates a compact context indicator
// Format: [▰▰▰▱▱▱▱▱] 38% ↑2.9k ↓2.0k
func FormatContextIndicator(cfg *config.Config) string {
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

	// Build progress bar
	progressBar := FormatProgressBar(percentage, 5, contextColor)

	// Build percentage string (2 digits)
	pctStr := contextColor.Sprintf("%2.0f%%", percentage)

	// Build token counts (compact, no decimals)
	tokenStr := ""
	if cfg.LastOutgoingTokens > 0 || cfg.LastIncomingTokens > 0 {
		up := color.New(color.FgHiBlue, color.Bold).Sprint("↑")
		down := color.New(color.FgHiGreen, color.Bold).Sprint("↓")
		outNum := color.New(color.FgHiBlack).Sprint(formatCompact2Digit(cfg.LastOutgoingTokens))
		inNum := color.New(color.FgHiBlack).Sprint(formatCompact2Digit(cfg.LastIncomingTokens))
		tokenStr = fmt.Sprintf(" %s%s%s%s", up, outNum, down, inNum)
	}

	return fmt.Sprintf("%s%s%s", progressBar, pctStr, tokenStr)
}

// formatCompact2Digit formats numbers compactly with max 2 digits (no decimals)
// Examples: 0 -> "0", 950 -> "950", 2910 -> "3k", 1200000 -> "1M"
func formatCompact2Digit(value int) string {
	if value == 0 {
		return "0"
	}
	sign := ""
	n := value
	if n < 0 {
		sign = "-"
		n = -n
	}

	var scaled float64
	var suffix string

	switch {
	case n >= 1_000_000_000_000:
		scaled = float64(n) / 1_000_000_000_000.0
		suffix = "T"
	case n >= 1_000_000_000:
		scaled = float64(n) / 1_000_000_000.0
		suffix = "B"
	case n >= 1_000_000:
		scaled = float64(n) / 1_000_000.0
		suffix = "M"
	case n >= 1_000:
		scaled = float64(n) / 1_000.0
		suffix = "k"
	default:
		return fmt.Sprintf("%s%d", sign, n)
	}

	// Round to nearest integer for compact display
	rounded := int(scaled + 0.5)
	return fmt.Sprintf("%s%d%s", sign, rounded, suffix)
}

// FormatProgressBar creates a visual progress bar for context usage
// Uses ▰ for filled and ▱ for empty segments
func FormatProgressBar(percentage float64, width int, barColor *color.Color) string {
	if width <= 0 {
		width = 8
	}

	filled := int((percentage / 100.0) * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	leftBracket := color.New(color.FgHiBlack).Sprint("[")
	rightBracket := color.New(color.FgHiBlack).Sprint("]")

	var bar strings.Builder
	bar.WriteString(leftBracket)

	for i := 0; i < width; i++ {
		if i < filled {
			bar.WriteString(barColor.Sprint("▰"))
		} else {
			bar.WriteString(color.New(color.FgHiBlack).Sprint("▱"))
		}
	}

	bar.WriteString(rightBracket)
	return bar.String()
}

// visibleLen returns the visible length of a string, stripping ANSI escape codes
func visibleLen(s string) int {
	// Simple ANSI stripping: remove sequences starting with ESC[
	inEscape := false
	length := 0
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		length++
	}
	return length
}

// FormatEditPrompt returns the edit-mode prompt string without any token counter
// or context indicator. This is used when the user toggles persistent edit mode
// by pressing '!'.
func FormatEditPromptWith(cfg *config.Config) string {
	prefix := "!"
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
	promptColor := color.New(color.BgHiYellow, color.FgBlack)
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

	// Find the prompt in the input (case-insensitive) and highlight first occurrence
	lowerInput := strings.ToLower(input)
	lowerPrompt := strings.ToLower(promptPath)
	idx := strings.Index(lowerInput, lowerPrompt)
	if idx == -1 {
		return input
	}

	highlighted := FormatPromptHighlight(promptPath)
	// Reconstruct with the highlighted prompt
	return input[:idx] + highlighted + input[idx+len(promptPath):]
}

// CoolClearEffect creates a snappy glitch-style clear animation
func CoolClearEffect(cfg *config.Config) {
	if cfg.DisableAnimation {
		// Clear screen and scrollback buffer
		fmt.Print("\033[2J\033[3J\033[H")
		return
	}

	width, height := getTerminalSize()
	if width <= 0 || height <= 0 {
		fmt.Print("\033[2J\033[3J\033[H")
		return
	}

	// Constants for the animation
	renderWidth := width
	if renderWidth > 180 {
		renderWidth = 180
	}

	// Glitch characters
	glitchChars := []string{"▓", "▒", "░", "<", ">", "/", "\\", "!", "?", "#", "%", "&", "_", "-", "+", "="}
	rand.Seed(time.Now().UnixNano())

	// Phase 1: Heavy Glitch (very fast)
	// Randomly fill lines with glitch characters to create noise
	fmt.Print("\033[?25l") // Hide cursor
	defer fmt.Print("\033[?25h")

	// Pre-generate some random noise lines
	noiseLines := make([]string, 10)
	for i := 0; i < 10; i++ {
		var b strings.Builder
		for j := 0; j < renderWidth; j++ {
			if rand.Float32() < 0.3 {
				b.WriteString(glitchChars[rand.Intn(len(glitchChars))])
			} else {
				b.WriteString(" ")
			}
		}
		noiseLines[i] = b.String()
	}

	// Flash noise for a brief moment
	colors := []*color.Color{
		color.New(color.FgHiCyan),
		color.New(color.FgHiMagenta),
		color.New(color.FgHiWhite),
		color.New(color.FgHiRed), // Add red for that "critical" glitch feel
	}

	// Frame count for the glitch phase
	glitchFrames := 5
	for i := 0; i < glitchFrames; i++ {
		fmt.Print("\033[H") // Reset cursor to top-left
		for r := 0; r < height; r++ {
			if rand.Float32() < 0.4 { // Only draw on some lines
				lineIdx := rand.Intn(len(noiseLines))
				col := colors[rand.Intn(len(colors))]
				// Shift line randomly
				start := rand.Intn(renderWidth / 2)
				lineBody := noiseLines[lineIdx]
				if start < len(lineBody) {
					lineBody = lineBody[start:]
				}
				fmt.Printf("\033[%d;1H%s", r+1, col.Sprint(lineBody))
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Phase 2: Thinning Lines (Cleanup)
	// Rapidly wipe with thin horizontal lines that disappear
	thinChars := []string{"─", " ", " ", " "} // Mostly empty to "thin out"

	wipeFrames := 8
	for i := 0; i < wipeFrames; i++ {
		fmt.Print("\033[H")
		for r := 0; r < height; r++ {
			// Probability of drawing a line decreases with frames
			if rand.Float32() < (0.8 - float32(i)*0.1) {
				var b strings.Builder
				widthFrac := renderWidth - (i * (renderWidth / wipeFrames)) // Line gets shorter
				if widthFrac < 0 {
					widthFrac = 0
				}

				for j := 0; j < widthFrac; j++ {
					b.WriteString(thinChars[rand.Intn(len(thinChars))])
				}
				col := color.New(color.FgHiBlue) // Cool blue for the wipe
				fmt.Printf("\033[%d;1H%s", r+1, col.Sprint(b.String()))
			} else {
				// Clear the line
				fmt.Printf("\033[%d;1H\033[2K", r+1)
			}
		}
		time.Sleep(15 * time.Millisecond)
	}

	// Phase 3: Empty Frame (ensure clean history)
	// Explicitly clear all lines to remove any "leftovers" from the buffer
	// before the final screen wipe. This helps prevents artifacts in scrollback.
	fmt.Print("\033[H")
	for r := 0; r < height; r++ {
		fmt.Printf("\033[%d;1H\033[2K", r+1)
	}
	// Small pause to let the empty frame register
	time.Sleep(10 * time.Millisecond)

	// Final Clean: Clear screen and scrollback buffer
	// \033[2J clears the entire screen
	// \033[3J clears the scrollback buffer (extension supported by many terminals like iTerm2, xterm, VSCode)
	// \033[H moves cursor to home
	fmt.Print("\033[2J\033[3J\033[H")
}
