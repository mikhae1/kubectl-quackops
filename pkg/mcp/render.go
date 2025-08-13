package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// FormatToolCallBlock builds a colored block for an MCP tool call and its output and returns it as a string.
// Callers can decide when to print it to avoid interfering with other stdout streams.
func FormatToolCallBlock(toolName string, args map[string]any, output string) string {
	maxLineLen := config.LoadConfig().ToolOutputMaxLineLen
	maxLines := config.LoadConfig().ToolOutputMaxLines
	if cfg := config.LoadConfig(); cfg != nil && cfg.ToolOutputMaxLines > 0 {
		maxLines = cfg.ToolOutputMaxLines
	}

	header := config.Colors.Gradient[0].Sprint("╭─ ") + config.Colors.Gradient[0].Sprint("MCP Tool: ") + config.Colors.Header.Sprint(toolName)

	argJSON := "{}"
	if args != nil {
		if b, err := json.MarshalIndent(args, "", "  "); err == nil {
			argJSON = string(b)
		}
	}
	argsLines := strings.Split(argJSON, "\n")
	if len(argsLines) > 20 {
		argsLines = append(argsLines[:20], color.New(color.Faint).Sprint("… (args truncated)"))
	}
	for i, ln := range argsLines {
		argsLines[i] = trimLine(ln, maxLineLen)
	}
	argsBlock := strings.Join(argsLines, "\n")

	outLines := strings.Split(output, "\n")
	for i, ln := range outLines {
		outLines[i] = trimLine(ln, maxLineLen)
	}
	if maxLines > 0 && len(outLines) > maxLines {
		originalCount := len(outLines)
		half := maxLines / 2
		head := outLines[:half]
		tail := outLines[originalCount-half:]
		truncatedCount := originalCount - maxLines
		if truncatedCount < 0 {
			truncatedCount = 0
		}

		indent := ""
		if len(head) > 0 {
			indent = leadingWhitespace(head[len(head)-1])
		}

		above := indent + color.New(color.FgHiBlack).Sprint("┈┈┈")
		center := indent + color.New(color.FgHiBlack, color.Italic).Sprintf("… (%d lines truncated) …", truncatedCount)
		below := indent + color.New(color.FgHiBlack).Sprint("┈┈┈")

		outLines = append(append(head, above, center, below), tail...)
	}
	outputBlock := strings.Join(outLines, "\n")

	// Gradient color helper for the left border "│"
	gradientBar := func(i int) string {
		palette := config.Colors.Gradient
		if len(palette) == 0 {
			return config.Colors.Border.Sprint("│")
		}
		return palette[i%len(palette)].Sprint("│")
	}

	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, header)
	lineIdx := 0
	fmt.Fprintln(&b, gradientBar(lineIdx)+" "+config.Colors.Label.Sprint("Args:"))
	lineIdx++
	for _, ln := range strings.Split(argsBlock, "\n") {
		colored := colorizeKVOrFallback(ln, config.Colors.Gradient[0], nextMono(config.Colors.Gradient, 0), config.Colors.Label)
		fmt.Fprintln(&b, gradientBar(lineIdx)+" "+colored)
		lineIdx++
	}
	fmt.Fprintln(&b, gradientBar(lineIdx)+" "+config.Colors.Label.Sprint("Output:"))
	lineIdx++
	for _, ln := range strings.Split(outputBlock, "\n") {
		colored := colorizeKVOrFallback(ln, config.Colors.Gradient[0], nextMono(config.Colors.Gradient, 0), config.Colors.Gradient[0])
		fmt.Fprintln(&b, gradientBar(lineIdx)+" "+colored)
		lineIdx++
	}
	fmt.Fprintln(&b, config.Colors.Gradient[0].Sprint("╰─"))
	fmt.Fprintln(&b)
	return b.String()
}

// RenderToolCallBlock prints a colored block for an MCP tool call and its output immediately to stdout.
// Prefer FormatToolCallBlock when you need to control ordering relative to other output.
func RenderToolCallBlock(toolName string, args map[string]any, output string) {
	block := FormatToolCallBlock(toolName, args, output)
	fmt.Fprint(os.Stdout, block)
}

func trimLine(s string, maxRunes int) string {
	if maxRunes <= 0 || s == "" {
		return s
	}
	if runeCount(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes) + color.New(color.Faint).Sprint(" …")
}

func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// leadingWhitespace returns the run of leading spaces or tabs from s.
func leadingWhitespace(s string) string {
	if s == "" {
		return ""
	}
	// Preserve exact indentation characters (spaces vs tabs) as provided.
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

// colorizeKVOrFallback applies mono-palette coloring to JSON-like key/value lines.
// If the line does not look like a JSON key-value, falls back to the provided color.
func colorizeKVOrFallback(line string, keyColor *color.Color, valueColor *color.Color, fallback *color.Color) string {
	colored, ok := colorizeJSONKeyValueLine(line, keyColor, valueColor)
	if ok {
		return colored
	}
	return fallback.Sprint(line)
}

// nextMono returns the next color in a small mono palette sequence starting at offset.
func nextMono(palette []*color.Color, start int) *color.Color {
	if len(palette) == 0 {
		return color.New(color.FgHiWhite)
	}
	return palette[(start+1)%len(palette)]
}

// colorizeJSONKeyValueLine attempts to detect and color a JSON key/value pair on a single line.
// It preserves leading whitespace and trailing commas. Returns false if not a key/value line.
func colorizeJSONKeyValueLine(line string, keyColor *color.Color, valueColor *color.Color) (string, bool) {
	if line == "" {
		return line, false
	}
	// Preserve exact leading whitespace (spaces or tabs)
	indent := leadingWhitespace(line)
	rest := line[len(indent):]
	if rest == "" || !strings.HasPrefix(rest, "\"") {
		return line, false
	}
	// Find closing quote for key, respecting escapes
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
	keyToken := rest[:keyEnd+1] // includes quotes
	afterKey := rest[keyEnd+1:]
	// Expect optional spaces then ':'
	colonIdx := strings.IndexRune(afterKey, ':')
	if colonIdx < 0 {
		return line, false
	}
	preColon := afterKey[:colonIdx]
	postColon := afterKey[colonIdx+1:]
	// Preserve spaces after colon
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
	// Heuristic: color only primitive or string values; leave objects/arrays/braces as-is
	if valuePart == "" {
		return line, false
	}
	first := valuePart[0]
	if first == '{' || first == '[' || first == '}' || first == ']' {
		return line, false
	}
	// Compose colored line
	coloredKey := keyColor.Sprint(keyToken)
	coloredVal := valueColor.Sprint(valuePart)
	// Preserve exact spacing around ':' and after
	colored := indent + coloredKey + preColon + ":" + postColon[:spaceAfter] + coloredVal + commaSuffix
	return colored, true
}
