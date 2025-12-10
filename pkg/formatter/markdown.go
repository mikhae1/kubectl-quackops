package formatter

import (
	"bytes"
	"context"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mikhae1/kubectl-quackops/pkg/style"
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
	// Reset states
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
		// Check for think block markers first
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
			continue // Skip the closing tag
		}

		// Handle empty lines (but skip empty lines around think blocks)
		if len(line) == 0 && i < len(lines)-1 {
			// Skip empty lines that come right after think block start
			if lastWasThinkStart {
				lastWasThinkStart = false
				continue
			}
			// Look ahead for think block end
			nextNonEmptyIdx := i + 1
			for nextNonEmptyIdx < len(lines) && len(lines[nextNonEmptyIdx]) == 0 {
				nextNonEmptyIdx++
			}
			if nextNonEmptyIdx < len(lines) && thinkBlockEndRegex.Match(lines[nextNonEmptyIdx]) {
				continue
			}
			result.WriteByte('\n')
			continue
		}

		lastWasThinkStart = false

		// Check for code block markers
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
			if f.thinkContent.Len() > 0 {
				f.thinkContent.WriteByte('\n')
			}
			f.thinkContent.Write(line)
		} else if f.inCodeBlock {
			// Inside code block, apply italic style
			if f.colorEnabled {
				// Use lipgloss for styling
				styled := lipgloss.NewStyle().Italic(true).Render(string(line))
				result.Write([]byte(styled))
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

			// Define heading style (Blue Bold)
			headingStyle := lipgloss.NewStyle().Foreground(style.ColorBlue).Bold(true)
			return []byte(headingStyle.Render(string(line)))
		}
	}

	// Blockquotes
	if bytes.HasPrefix(line, []byte(">")) {
		return []byte(lipgloss.NewStyle().Foreground(style.ColorGreen).Render(string(line)))
	}

	// Lists (unordered)
	trimmed := bytes.TrimSpace(line)
	if bytes.HasPrefix(trimmed, []byte("- ")) ||
		bytes.HasPrefix(trimmed, []byte("* ")) ||
		bytes.HasPrefix(trimmed, []byte("+ ")) {
		// Find the prefix length in the original line
		prefixLen := len(line) - len(bytes.TrimLeft(line, " \t"))
		bulletIdx := prefixLen

		if bulletIdx+1 < len(line) && (line[bulletIdx] == '-' || line[bulletIdx] == '*' || line[bulletIdx] == '+') && line[bulletIdx+1] == ' ' {
			var result bytes.Buffer
			result.Write(line[:prefixLen])                                                    // Preserve leading whitespace
			result.Write([]byte(lipgloss.NewStyle().Foreground(style.ColorCyan).Render("-"))) // Render bullet as cyan dash
			result.WriteByte(' ')                                                             // One space after bullet
			restOfLine := bytes.TrimLeft(line[bulletIdx+2:], " \t")                           // Skip original spacing
			formattedRest := f.formatInlineElements(restOfLine)                               // Format remaining text
			result.Write(formattedRest)

			return result.Bytes()
		}
	}

	// Lists (ordered)
	if len(trimmed) >= 3 &&
		trimmed[0] >= '0' && trimmed[0] <= '9' &&
		trimmed[1] == '.' &&
		trimmed[2] == ' ' {
		prefixLen := len(line) - len(bytes.TrimLeft(line, " \t"))
		numEnd := prefixLen
		for numEnd < len(line) && line[numEnd] >= '0' && line[numEnd] <= '9' {
			numEnd++
		}

		if numEnd+1 < len(line) && line[numEnd] == '.' && line[numEnd+1] == ' ' {
			var result bytes.Buffer
			result.Write(line[:prefixLen]) // Write leading whitespace
			number := string(line[prefixLen : numEnd+1])
			result.Write([]byte(lipgloss.NewStyle().Foreground(style.ColorCyan).Render(number))) // Color the number and dot
			result.WriteByte(' ')                                                                // One space after number and dot

			// Process the rest of the line
			restOfLine := bytes.TrimLeft(line[numEnd+2:], " \t")
			formattedRest := f.formatInlineElements(restOfLine)
			result.Write(formattedRest)

			return result.Bytes()
		}
	}

	// Handle inline formatting for normal text
	return f.formatInlineElements(line)
}

// formatInlineElements formats inline Markdown elements
func (f *MarkdownFormatter) formatInlineElements(line []byte) []byte {
	if len(line) == 0 {
		return line
	}

	text := string(line)

	// Process code spans first
	codeStyle := lipgloss.NewStyle().Foreground(style.ColorCyan).Bold(true)

	text = doubleBacktickCodeRegex.ReplaceAllStringFunc(text, func(match string) string {
		content := doubleBacktickCodeRegex.FindStringSubmatch(match)[1]
		return "`" + codeStyle.Render(content) + "`"
	})

	text = singleBacktickCodeRegex.ReplaceAllStringFunc(text, func(match string) string {
		content := singleBacktickCodeRegex.FindStringSubmatch(match)[1]
		return "`" + lipgloss.NewStyle().Foreground(style.ColorCyan).Render(content) + "`"
	})

	// Process links [text](url)
	linkStyle := lipgloss.NewStyle().Foreground(style.ColorBlue).Underline(true)
	text = linkRegex.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkRegex.FindStringSubmatch(match)
		linkText := parts[1]
		url := parts[2]

		if linkText != "" && url != "" {
			return linkStyle.Render(fmtWrapper(linkText, url))
		} else if linkText != "" {
			return linkStyle.Render(linkText)
		}
		return match
	})

	// Process bold text
	boldStyle := lipgloss.NewStyle().Bold(true)
	text = boldRegex.ReplaceAllStringFunc(text, func(match string) string {
		content := boldRegex.FindStringSubmatch(match)[1]
		return boldStyle.Render(content)
	})

	// Process italic text
	italicStyle := lipgloss.NewStyle().Italic(true)
	text = italicRegex.ReplaceAllStringFunc(text, func(match string) string {
		content := italicRegex.FindStringSubmatch(match)[1]
		return italicStyle.Render(content)
	})

	// Process quoted strings
	return f.colorizeQuotedStrings([]byte(text))
}

func fmtWrapper(text, url string) string {
	// Simple helper since we can't use Sprintf inside Render easily without creating string first
	return text + " (" + url + ")"
}

// colorizeQuotedStrings processes text to colorize content in double or single quotes
func (f *MarkdownFormatter) colorizeQuotedStrings(text []byte) []byte {
	var result bytes.Buffer

	doubleQuoteRegex := regexp.MustCompile(`"([^"\\]|\\.)*"`)
	singleQuoteRegex := regexp.MustCompile(`(^|\s)('([^'\\]|\\.)*')`)

	doubleQuoteStyle := lipgloss.NewStyle().Foreground(style.ColorGreen)
	singleQuoteStyle := lipgloss.NewStyle().Foreground(style.ColorCyan)

	// Process double-quoted strings
	lastIndex := 0
	for _, match := range doubleQuoteRegex.FindAllIndex(text, -1) {
		result.Write(text[lastIndex:match[0]])
		quotedText := text[match[0]:match[1]]
		result.Write([]byte(doubleQuoteStyle.Render(string(quotedText))))
		lastIndex = match[1]
	}
	remainingText := text[lastIndex:]

	// Process single-quoted strings in match
	// ... (This logic needs to be robust, reusing existing logic pattern)
	// For simplicity, we can do a simplified streaming replace if regexes are non-overlapping enough.
	// But let's stick to the previous iterative approach.

	var finalResult bytes.Buffer
	// Reset loop for remaining text
	lastIndex = 0
	for _, match := range singleQuoteRegex.FindAllSubmatch(remainingText, -1) {
		fullMatch := match[0]
		leadingSpace := match[1]
		quotedContent := match[2]

		// Find index of fullMatch in remainingText starting from lastIndex
		idx := bytes.Index(remainingText[lastIndex:], fullMatch) + lastIndex

		finalResult.Write(remainingText[lastIndex:idx])
		finalResult.Write(leadingSpace)
		finalResult.Write([]byte(singleQuoteStyle.Render(string(quotedContent))))

		lastIndex = idx + len(fullMatch)
	}

	// Append whatever is left from remainingText
	if lastIndex < len(remainingText) {
		finalResult.Write(remainingText[lastIndex:])
	}

	// Merge buffers
	result.Write(finalResult.Bytes())
	return result.Bytes()
}

// renderThinkBlock formats think block content with brown borders and dimmed text
func renderThinkBlock(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}

	maxLineLen := 120
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 20 {
		maxLineLen = width - 10
	}

	// Styles
	borderColor := lipgloss.NewStyle().Foreground(style.ColorYellow)
	headerColor := lipgloss.NewStyle().Bold(true)
	dimColor := lipgloss.NewStyle().Faint(true)

	var result strings.Builder

	header := borderColor.Render("╭─ ") + headerColor.Render("thinking...")
	result.WriteString(header)
	result.WriteString("\n")

	lines := strings.Split(strings.TrimSpace(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if len(line) > maxLineLen {
			for len(line) > maxLineLen {
				breakPoint := maxLineLen
				for i := maxLineLen - 1; i > maxLineLen/2 && i >= 0; i-- {
					if i < len(line) && line[i] == ' ' {
						breakPoint = i
						break
					}
				}

				wrappedLine := line[:breakPoint]
				result.WriteString(borderColor.Render("│ "))
				result.WriteString(dimColor.Render(wrappedLine))
				result.WriteString("\n")
				line = strings.TrimLeft(line[breakPoint:], " ")
			}
			if len(line) > 0 {
				result.WriteString(borderColor.Render("│ "))
				result.WriteString(dimColor.Render(line))
				result.WriteString("\n")
			}
		} else {
			result.WriteString(borderColor.Render("│ "))
			result.WriteString(dimColor.Render(line))
			result.WriteString("\n")
		}
	}

	result.WriteString(borderColor.Render("╰"))
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
	formatted := w.formatter.ProcessChunk(p)
	if len(formatted) > 0 {
		_, err = w.outWriter.Write(formatted)
		if err != nil {
			return 0, err
		}
	}

	if len(p) > 0 && p[len(p)-1] == '\n' {
		w.lineStart = true
	} else {
		w.lineStart = false
	}

	return len(p), nil
}

// Flush flushes any remaining content
func (w *StreamingWriter) Flush() error {
	formatted := w.formatter.Flush()
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
