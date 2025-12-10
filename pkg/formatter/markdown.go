package formatter

import (
	"bytes"
	"context"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// Regular expressions for inline Markdown elements
var (
	// Bold (**text**)
	boldRegex = regexp.MustCompile(`\*\*([^*]+)\*\*`)

	// Italic (*text*)
	italicRegex = regexp.MustCompile(`\*([^*]+)\*`)

	// Code spans (single and double backticks)
	singleBacktickCodeRegex = regexp.MustCompile("`([^`]+)`")
	doubleBacktickCodeRegex = regexp.MustCompile("``([^`]+)``")

	// Link [text](url)
	linkRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

	// Code block markers (```language) - must be alone on the line
	codeBlockMarkerRegex = regexp.MustCompile("^\\s*```[a-zA-Z0-9_+\\-]*\\s*$")

	// Think block markers (<think> and </think>)
	thinkBlockStartRegex = regexp.MustCompile(`<think>`)
	thinkBlockEndRegex   = regexp.MustCompile(`</think>`)

	// Numbers and number-letter combinations (like "2m", "500Mi", "(329 days)")
)

// MarkdownFormatter handles streaming Markdown formatting
type MarkdownFormatter struct {
	buffer       bytes.Buffer // Buffer for accumulating text
	colorEnabled bool
	inCodeBlock  bool         // Track if we're inside a code block
	inThinkBlock bool         // Track if we're inside a think block
	thinkContent bytes.Buffer // Buffer for accumulating think block content
}

// FormatterOption represents options for configuring the formatter
type FormatterOption func(*MarkdownFormatter)

// WithColorEnabled allows enabling/disabling colorization
func WithColorEnabled(enabled bool) FormatterOption {
	return func(f *MarkdownFormatter) {
		f.colorEnabled = enabled
	}
}

// WithColorDisabled disables color output
func WithColorDisabled() FormatterOption {
	return WithColorEnabled(false)
}

// NewMarkdownFormatter creates a new MarkdownFormatter
func NewMarkdownFormatter(opts ...FormatterOption) *MarkdownFormatter {
	f := &MarkdownFormatter{
		colorEnabled: true,
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// ProcessChunk processes a chunk of text, formats any complete lines,
// and returns the formatted output
func (f *MarkdownFormatter) ProcessChunk(chunk []byte) []byte {
	// Special handling for code blocks
	if f.inCodeBlock && !bytes.Contains(chunk, []byte("```")) {
		// Inside a code block but not at the end marker
		// Check if the chunk already has a newline
		if !bytes.Contains(chunk, []byte("\n")) {
			// Code block chunk without delimiters/newlines; avoid auto-newline
			f.buffer.Write(chunk)
		} else {
			// If it does have a newline, process normally
			f.buffer.Write(chunk)
		}
	} else {
		// Regular processing
		f.buffer.Write(chunk)
	}

	// Find the last newline in the buffer
	bufferData := f.buffer.Bytes()
	lastNewlineIdx := bytes.LastIndexByte(bufferData, '\n')

	// If no newline is found, keep everything in the buffer
	if lastNewlineIdx == -1 {
		return nil
	}

	// Process up to the last newline
	toProcess := bufferData[:lastNewlineIdx+1]
	result := f.formatMarkdown(toProcess)

	// Keep the remainder in the buffer
	f.buffer.Reset()
	if lastNewlineIdx+1 < len(bufferData) {
		f.buffer.Write(bufferData[lastNewlineIdx+1:])
	}

	return result
}

// Flush processes any remaining content in the buffer and returns it
func (f *MarkdownFormatter) Flush() []byte {
	if f.buffer.Len() == 0 {
		return nil
	}

	// Ensure we add a newline to the end of the buffer to process it properly
	if !bytes.HasSuffix(f.buffer.Bytes(), []byte("\n")) {
		f.buffer.WriteString("\n")
	}

	result := f.formatMarkdown(f.buffer.Bytes())
	f.buffer.Reset()
	// Reset code block state after flush
	f.inCodeBlock = false
	f.inThinkBlock = false
	f.thinkContent.Reset()
	return result
}

// formatMarkdown parses and formats Markdown content
func (f *MarkdownFormatter) formatMarkdown(content []byte) []byte {

	// Use the simplest approach that works: convert full lines
	lines := bytes.Split(content, []byte("\n"))
	var result bytes.Buffer

	var lastWasThinkStart bool
	for i, line := range lines {
		// Check for think block markers first, before processing empty lines
		if thinkBlockStartRegex.Match(line) {
			f.inThinkBlock = true
			f.thinkContent.Reset() // Start fresh content accumulation
			lastWasThinkStart = true
			continue // Skip the opening tag, don't render it
		} else if thinkBlockEndRegex.Match(line) {
			f.inThinkBlock = false
			// Render the complete think block with accumulated content
			thinkBlockOutput := renderThinkBlock(f.thinkContent.String())
			// Remove ALL trailing newlines and whitespace from previous content
			resultStr := result.String()
			resultStr = strings.TrimRight(resultStr, " \n\t\r")
			result.Reset()
			// If there's any content before the think block, ensure proper separation
			if len(resultStr) > 0 {
				result.WriteString(resultStr)
				result.WriteString("\n")
			}
			result.WriteString(thinkBlockOutput)
			f.thinkContent.Reset() // Clear the content buffer
			lastWasThinkStart = false
			continue // Skip the closing tag, already handled by renderThinkBlock
		}

		// Handle empty lines (but skip empty lines around think blocks)
		if len(line) == 0 && i < len(lines)-1 {
			// Skip empty lines that come right after think block start
			if lastWasThinkStart {
				lastWasThinkStart = false
				continue
			}
			// Look ahead to see if the next non-empty line is a think block end
			nextNonEmptyIdx := i + 1
			for nextNonEmptyIdx < len(lines) && len(lines[nextNonEmptyIdx]) == 0 {
				nextNonEmptyIdx++
			}
			// If the next non-empty line is a think block end, skip this empty line
			if nextNonEmptyIdx < len(lines) && thinkBlockEndRegex.Match(lines[nextNonEmptyIdx]) {
				continue
			}
			result.WriteByte('\n')
			continue
		}

		// Reset the lastWasThinkStart flag for any non-empty line
		lastWasThinkStart = false

		// Check for code block markers (```) with optional language specification.
		// Only treat as code fence when the line contains nothing else.
		if codeBlockMarkerRegex.Match(bytes.TrimSpace(line)) {
			f.inCodeBlock = !f.inCodeBlock
			// Use a different color for code block delimiter
			result.Write(line)
			if i < len(lines)-1 {
				result.WriteByte('\n')
			}
			continue
		}

		// If we're inside a think block, accumulate content
		if f.inThinkBlock {
			// Accumulate content in the think content buffer
			if f.thinkContent.Len() > 0 {
				f.thinkContent.WriteByte('\n')
			}
			f.thinkContent.Write(line)
		} else if f.inCodeBlock {
			// If we're inside a code block, don't process Markdown
			if f.colorEnabled {
				result.Write([]byte(color.New(color.Italic).Sprint(string(line))))
			} else {
				result.Write(line)
			}
		} else {
			// Normal Markdown processing
			if f.colorEnabled {
				formattedLine := f.formatLine(line)
				result.Write(formattedLine)
			} else {
				result.Write(line)
			}
		}

		if i < len(lines)-1 {
			result.WriteByte('\n')
		}
	}

	return result.Bytes()
}

// formatLine formats a single line of markdown content
func (f *MarkdownFormatter) formatLine(line []byte) []byte {
	if len(line) == 0 {
		return line
	}

	// Format as Markdown with specific syntax highlighting
	// Headings
	if bytes.HasPrefix(line, []byte("#")) {
		level := 0
		for i, c := range line {
			if c == '#' {
				level++
			} else {
				line = line[i:]
				break
			}
		}

		if level >= 1 && level <= 6 {
			// Remove leading whitespace
			line = bytes.TrimLeft(line, " ")

			switch level {
			case 1:
				return []byte(color.New(color.FgBlue, color.Bold).Sprint(string(line)))
			case 2:
				return []byte(color.New(color.FgBlue, color.Bold).Sprint(string(line)))
			default:
				return []byte(color.New(color.FgBlue, color.Bold).Sprint(string(line)))
			}
		}
	}

	// Blockquotes
	if bytes.HasPrefix(line, []byte(">")) {
		return []byte(color.New(color.FgGreen).Sprint(string(line)))
	}

	// Lists (unordered)
	trimmed := bytes.TrimSpace(line)
	if bytes.HasPrefix(trimmed, []byte("- ")) ||
		bytes.HasPrefix(trimmed, []byte("* ")) ||
		bytes.HasPrefix(trimmed, []byte("+ ")) {
		// Find the prefix length in the original line
		prefixLen := len(line) - len(bytes.TrimLeft(line, " \t"))
		bulletIdx := prefixLen

		// Guard against malformed lines
		if bulletIdx+1 < len(line) && (line[bulletIdx] == '-' || line[bulletIdx] == '*' || line[bulletIdx] == '+') && line[bulletIdx+1] == ' ' {
			var result bytes.Buffer
			result.Write(line[:prefixLen])                              // Preserve leading whitespace
			result.Write([]byte(color.New(color.FgHiBlue).Sprint("-"))) // Render bullet as dash
			result.WriteByte(' ')                                       // One space after bullet
			restOfLine := bytes.TrimLeft(line[bulletIdx+2:], " \t")     // Skip original spacing
			formattedRest := f.formatInlineElements(restOfLine)         // Format remaining text
			result.Write(formattedRest)

			return result.Bytes()
		}
	}

	// Lists (ordered) - simple check for "1. ", "2. " etc.
	if len(trimmed) >= 3 &&
		trimmed[0] >= '0' && trimmed[0] <= '9' &&
		trimmed[1] == '.' &&
		trimmed[2] == ' ' {
		// Find how many digits the number has
		prefixLen := len(line) - len(bytes.TrimLeft(line, " \t"))
		numEnd := prefixLen
		for numEnd < len(line) && line[numEnd] >= '0' && line[numEnd] <= '9' {
			numEnd++
		}

		if numEnd+1 < len(line) && line[numEnd] == '.' && line[numEnd+1] == ' ' {
			var result bytes.Buffer
			result.Write(line[:prefixLen])                                                             // Write leading whitespace
			result.Write([]byte(color.New(color.FgHiBlue).Sprint(string(line[prefixLen : numEnd+1])))) // Color the number and dot

			// Process the rest of the line for inline elements
			restOfLine := bytes.TrimLeft(line[numEnd+2:], " \t") // +2 to skip the dot and space
			formattedRest := f.formatInlineElements(restOfLine)
			result.WriteByte(' ') // One space after number and dot
			result.Write(formattedRest)

			return result.Bytes()
		}
	}

	// Handle inline formatting for normal text (bold, italic, code)
	return f.formatInlineElements(line)
}

// formatInlineElements formats inline Markdown elements
func (f *MarkdownFormatter) formatInlineElements(line []byte) []byte {
	if len(line) == 0 {
		return line
	}

	text := string(line)

	// Process code spans first (to avoid issues with other elements inside code)
	// Process double backticks first to avoid conflicts with single backticks
	text = doubleBacktickCodeRegex.ReplaceAllStringFunc(text, func(match string) string {
		content := doubleBacktickCodeRegex.FindStringSubmatch(match)[1]
		return "`" + color.New(color.FgHiCyan, color.Bold).Sprint(content) + "`"
	})

	text = singleBacktickCodeRegex.ReplaceAllStringFunc(text, func(match string) string {
		content := singleBacktickCodeRegex.FindStringSubmatch(match)[1]
		return "`" + color.New(color.FgHiCyan).Sprint(content) + "`"
	})

	// Process links [text](url)
	text = linkRegex.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkRegex.FindStringSubmatch(match)
		linkText := parts[1]
		url := parts[2]

		if linkText != "" && url != "" {
			return color.New(color.FgBlue, color.Underline).Sprintf("%s (%s)", linkText, url)
		} else if linkText != "" {
			return color.New(color.FgBlue, color.Underline).Sprint(linkText)
		}
		return match
	})

	// Process bold text
	text = boldRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Extract the bold content (without asterisks)
		content := boldRegex.FindStringSubmatch(match)[1]
		return color.New(color.Bold).Sprint(content)
	})

	// Process italic text
	text = italicRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Extract the italic content (without asterisks)
		content := italicRegex.FindStringSubmatch(match)[1]
		return color.New(color.Italic).Sprint(content)
	})

	// Process quoted strings
	return f.colorizeQuotedStrings([]byte(text))
}

// colorizeQuotedStrings processes text to colorize content in double or single quotes
func (f *MarkdownFormatter) colorizeQuotedStrings(text []byte) []byte {
	var result bytes.Buffer

	// Regular expressions for matching quoted strings
	doubleQuoteRegex := regexp.MustCompile(`"([^"\\]|\\.)*"`)
	singleQuoteRegex := regexp.MustCompile(`(^|\s)('([^'\\]|\\.)*')`)

	// Process double-quoted strings first
	lastIndex := 0
	for _, match := range doubleQuoteRegex.FindAllIndex(text, -1) {
		// Write unmatched text
		result.Write(text[lastIndex:match[0]])
		// Write colored quoted text
		quotedText := text[match[0]:match[1]]
		result.Write([]byte(color.New(color.FgGreen).Sprint(string(quotedText))))
		lastIndex = match[1]
	}
	// Write remaining text after last double quote match
	remainingText := text[lastIndex:]

	// Process single-quoted strings in the remaining text
	lastIndex = 0
	for _, match := range singleQuoteRegex.FindAllSubmatch(remainingText, -1) {
		// Get the full match and the capture groups
		fullMatch := match[0]
		leadingSpace := match[1]
		quotedContent := match[2]

		// Write unmatched text up to this match
		result.Write(remainingText[lastIndex : bytes.Index(remainingText[lastIndex:], fullMatch)+lastIndex])

		// Write uncolored leading space if present
		result.Write(leadingSpace)

		// Write colored quoted text (which includes the quotes)
		result.Write([]byte(color.New(color.FgCyan).Sprint(string(quotedContent))))

		// Update lastIndex to after this match
		lastIndex = bytes.Index(remainingText[lastIndex:], fullMatch) + lastIndex + len(fullMatch)
	}

	// Write any remaining text
	if lastIndex < len(remainingText) {
		result.Write(remainingText[lastIndex:])
	}

	return result.Bytes()
}

// renderThinkBlock formats think block content with brown borders and dimmed text
func renderThinkBlock(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}

	// Get terminal width for line wrapping
	maxLineLen := 120 // Default fallback
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 20 {
		maxLineLen = width - 10 // Leave margin for borders
	}

	// Brown colors for borders and headers
	borderColor := color.New(color.FgYellow) // Brown/yellow for borders
	headerColor := color.New(color.Bold)     // Bold brown for header
	dimColor := color.New(color.Faint)       // Dimmed text for content

	var result strings.Builder

	// Header with brown styling (no leading newlines)
	header := borderColor.Sprint("╭─ ") + headerColor.Sprint("thinking...")
	result.WriteString(header)
	result.WriteString("\n")

	// Process content with line wrapping and dimming
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Wrap long lines
		if len(line) > maxLineLen {
			for len(line) > maxLineLen {
				// Find a good break point (space before maxLineLen)
				breakPoint := maxLineLen
				for i := maxLineLen - 1; i > maxLineLen/2 && i >= 0; i-- {
					if i < len(line) && line[i] == ' ' {
						breakPoint = i
						break
					}
				}

				wrappedLine := line[:breakPoint]
				result.WriteString(borderColor.Sprint("│ "))
				result.WriteString(dimColor.Sprint(wrappedLine))
				result.WriteString("\n")
				line = strings.TrimLeft(line[breakPoint:], " ")
			}
			// Handle remaining part
			if len(line) > 0 {
				result.WriteString(borderColor.Sprint("│ "))
				result.WriteString(dimColor.Sprint(line))
				result.WriteString("\n")
			}
		} else {
			// Line fits within terminal width
			result.WriteString(borderColor.Sprint("│ "))
			result.WriteString(dimColor.Sprint(line))
			result.WriteString("\n")
		}
	}

	// Footer
	result.WriteString(borderColor.Sprint("╰"))
	result.WriteString("\n")

	return result.String()
}

// StreamingWriter is a writer that processes Markdown chunks in a streaming manner
type StreamingWriter struct {
	formatter  *MarkdownFormatter
	outWriter  io.Writer
	ctx        context.Context
	cancelFunc context.CancelFunc
	lineStart  bool
}

// NewStreamingWriter creates a new writer for streaming markdown formatting
func NewStreamingWriter(outWriter io.Writer, options ...FormatterOption) *StreamingWriter {
	ctx, cancel := context.WithCancel(context.Background())

	return &StreamingWriter{
		formatter:  NewMarkdownFormatter(options...),
		outWriter:  outWriter,
		ctx:        ctx,
		cancelFunc: cancel,
		lineStart:  true,
	}
}

// Write implements io.Writer interface for streaming processing
func (w *StreamingWriter) Write(p []byte) (n int, err error) {
	// Spinner writes to stderr; no trimming required here

	// Process the chunk
	formatted := w.formatter.ProcessChunk(p)

	// If we have formatted output, write it
	if len(formatted) > 0 {
		_, err = w.outWriter.Write(formatted)
		if err != nil {
			return 0, err
		}
	}

	// Track if the next write is at a new line boundary
	if len(p) > 0 && p[len(p)-1] == '\n' {
		w.lineStart = true
	} else {
		w.lineStart = false
	}

	// Report processed byte count
	return len(p), nil
}

// Flush flushes any remaining content
func (w *StreamingWriter) Flush() error {
	// Process remaining buffered content
	formatted := w.formatter.Flush()

	// If we have formatted output, write it
	if len(formatted) > 0 {
		_, err := w.outWriter.Write(formatted)
		return err
	}

	return nil
}

// Close closes the writer and flushes any pending content
func (w *StreamingWriter) Close() error {
	w.cancelFunc()
	return w.Flush()
}
