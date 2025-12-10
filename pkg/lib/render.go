package lib

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mikhae1/kubectl-quackops/pkg/style"
)

// RenderBlock builds a colored block with gradient borders and content
type RenderBlock struct {
	Title      string
	Sections   []RenderSection
	MaxLineLen int
	MaxLines   int
}

// RenderSection represents a section within a render block
type RenderSection struct {
	Label   string
	Content string
}

// Format builds the complete formatted block as a string
func (rb *RenderBlock) Format() string {
	header := style.Title.Render("╭─ ") + style.Title.Render(rb.Title)

	// Gradient color helper for the left border "│"
	gradientBar := func(i int) string {
		// Use a simpler vertical bar style for now or cycle styles if we had a list of styles
		return style.Debug.Render("│")
	}

	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, header)
	lineIdx := 0

	for _, section := range rb.Sections {
		// Print section label
		if section.Label != "" {
			fmt.Fprintln(&b, gradientBar(lineIdx)+" "+style.SubTitle.Render(section.Label+":"))
			lineIdx++
		}

		// Process section content
		processedContent := rb.processContent(section.Content)
		for _, ln := range strings.Split(processedContent, "\n") {
			// For now fallback to simple coloring or reuse the logic but with lipgloss
			colored := ColorizeKVOrFallback(ln, style.Info, style.Success, style.Info)
			fmt.Fprintln(&b, gradientBar(lineIdx)+" "+colored)
			lineIdx++
		}
	}

	fmt.Fprintln(&b, style.Title.Render("╰"))
	return b.String()
}

// processContent handles line filtering, trimming, and truncation
func (rb *RenderBlock) processContent(content string) string {
	// Filter out empty lines
	allLines := strings.Split(content, "\n")
	var outLines []string
	for _, ln := range allLines {
		trimmed := strings.TrimSpace(ln)
		if trimmed != "" {
			outLines = append(outLines, TrimLine(ln, rb.MaxLineLen))
		}
	}

	// Apply truncation logic
	if rb.MaxLines > 0 && len(outLines) > rb.MaxLines {
		originalCount := len(outLines)
		half := rb.MaxLines / 2
		head := outLines[:half]
		tail := outLines[originalCount-half:]
		truncatedCount := originalCount - rb.MaxLines
		if truncatedCount < 0 {
			truncatedCount = 0
		}

		indent := ""
		if len(head) > 0 {
			indent = LeadingWhitespace(head[len(head)-1])
		}

		above := indent + style.Debug.Render("┈┈┈")
		center := indent + style.Debug.Italic(true).Render(fmt.Sprintf("… (%d lines truncated, press CTRL-R to view) …", truncatedCount))
		below := indent + style.Debug.Render("┈┈┈")

		outLines = append(append(head, above, center, below), tail...)
	}

	return strings.Join(outLines, "\n")
}

// TrimLine trims a line to maxRunes and adds ellipsis if needed
func TrimLine(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return s
	}
	if RuneCount(s) <= maxRunes {
		return s
	}

	// Don't trim error lines - show them in full
	if isErrorContent(s) {
		return s
	}

	runes := []rune(s)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes) + style.Debug.Faint(true).Render(" …")
}

// RuneCount returns the number of runes in a string
func RuneCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// LeadingWhitespace returns the run of leading spaces or tabs from s
func LeadingWhitespace(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		break
	}
	return b.String()
}

// isErrorContent detects if a line contains error-related content
func isErrorContent(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	errorPatterns := []string{
		"error:",
		"error executing",
		"mcp tool execution failed",
		"tool call failed",
		"failed to connect",
		"connection refused",
		"permission denied",
		"unauthorized",
		"forbidden",
		"not found",
		"timeout",
		"unable to",
		"cannot",
		"invalid",
		"unknown flag:",
		"unknown command",
		"usage:",
		"ai still returning error",
		"unexpected status code:",
		"provider returned error",
		"status code: 4", // 400-499 client errors
		"status code: 5", // 500-599 server errors
	}

	for _, pattern := range errorPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// ColorizeKVOrFallback applies mono-palette coloring to JSON-like key/value lines
func ColorizeKVOrFallback(line string, keyStyle lipgloss.Style, valueStyle lipgloss.Style, fallback lipgloss.Style) string {
	colored, ok := ColorizeJSONKeyValueLine(line, keyStyle, valueStyle)
	if ok {
		return colored
	}

	// Check if this line contains error content and colorize accordingly
	if isErrorContent(line) {
		return style.Error.Render(line)
	}

	return fallback.Render(line)
}

// ColorizeJSONKeyValueLine attempts to detect and color a JSON key/value pair on a single line
func ColorizeJSONKeyValueLine(line string, keyStyle lipgloss.Style, valueStyle lipgloss.Style) (string, bool) {
	if line == "" {
		return line, false
	}
	indent := LeadingWhitespace(line)
	rest := line[len(indent):]
	if rest == "" || !strings.HasPrefix(rest, "\"") {
		return line, false
	}

	keyEnd := -1
	bsCount := 0
	for i := 1; i < len(rest); i++ {
		ch := rest[i]
		if ch == '\\' {
			bsCount++
			continue
		}
		if ch == '"' && (bsCount%2 == 0) {
			keyEnd = i
			break
		}
		bsCount = 0
	}
	if keyEnd <= 0 {
		return line, false
	}
	keyToken := rest[:keyEnd+1]
	afterKey := rest[keyEnd+1:]
	colonIdx := strings.IndexRune(afterKey, ':')
	if colonIdx < 0 {
		return line, false
	}
	preColon := afterKey[:colonIdx]
	postColon := afterKey[colonIdx+1:]

	spaceAfter := 0
	for spaceAfter < len(postColon) {
		if postColon[spaceAfter] == ' ' || postColon[spaceAfter] == '\t' {
			spaceAfter++
			continue
		}
		break
	}
	valAndMaybeComma := postColon[spaceAfter:]
	trimRight := strings.TrimRight(valAndMaybeComma, " \t")
	hasComma := strings.HasSuffix(trimRight, ",")
	valuePart := trimRight
	commaSuffix := ""
	if hasComma {
		valuePart = strings.TrimSuffix(trimRight, ",")
		commaSuffix = ","
	}

	if valuePart == "" {
		return line, false
	}
	first := valuePart[0]
	if first == '{' || first == '[' || first == '}' || first == ']' {
		return line, false
	}

	coloredKey := keyStyle.Render(keyToken)
	coloredVal := valueStyle.Render(valuePart)
	colored := indent + coloredKey + preColon + ":" + postColon[:spaceAfter] + coloredVal + commaSuffix
	return colored, true
}

// RenderTerminalWindow renders a modern terminal-like window around the given content.
// Content is line-trimmed and truncated with a centered ellipsis block when exceeding maxLines.
// Colors and accents are sourced from config.Colors to ensure consistent theming.
func RenderTerminalWindow(title string, content string, maxLines int, maxLineLen int) string {
	// Title bar with macOS-like traffic lights and gradient title

	header := style.Debug.Render("╭─ ") + style.Title.Render(title)

	// Prepare body via existing RenderBlock processing (handles trimming and truncation nicely)
	rb := &RenderBlock{
		Title:      "",
		Sections:   []RenderSection{{Label: "", Content: content}},
		MaxLineLen: maxLineLen,
		MaxLines:   maxLines,
	}
	processed := rb.processContent(content)

	// Compose window with vertical borders
	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, header)
	for _, ln := range strings.Split(processed, "\n") {
		if strings.TrimSpace(ln) == "" {
			// Keep empty lines visually minimal but aligned with border
			fmt.Fprintln(&b, style.Debug.Render("│ "))
			continue
		}
		colored := ColorizeKVOrFallback(ln, style.Info, style.Success, style.Code)
		fmt.Fprintln(&b, style.Debug.Render("│ ")+colored)
	}
	fmt.Fprintln(&b, style.Debug.Render("╰"))
	return b.String()
}
