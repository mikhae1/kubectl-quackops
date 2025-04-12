package formatter

import (
	"bytes"
	"context"
	"io"
	"regexp"

	"github.com/fatih/color"
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

	// Code block markers (```language)
	codeBlockMarkerRegex = regexp.MustCompile(`(^|\s)` + "```" + `([a-zA-Z0-9_+-]*)`)

	// Numbers and number-letter combinations (like "2m", "500Mi", "(329 days)")
)

// MarkdownFormatter handles streaming Markdown formatting
type MarkdownFormatter struct {
	buffer       bytes.Buffer // Buffer for accumulating text
	colorEnabled bool
	inCodeBlock  bool // Track if we're inside a code block
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
			// This is a code block content chunk without delimiters or newlines
			// We shouldn't automatically add a newline, as it might break code lines
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
	return result
}

// formatMarkdown parses and formats Markdown content
func (f *MarkdownFormatter) formatMarkdown(content []byte) []byte {
	if !f.colorEnabled {
		return content
	}

	// Use the simplest approach that works: convert full lines
	lines := bytes.Split(content, []byte("\n"))
	var result bytes.Buffer

	for i, line := range lines {
		if len(line) == 0 && i < len(lines)-1 {
			result.WriteByte('\n')
			continue
		}

		// Check for code block markers (```) with optional language specification
		if codeBlockMarkerRegex.Match(line) {
			f.inCodeBlock = !f.inCodeBlock
			// Use a different color for code block delimiter
			result.Write(line)
			if i < len(lines)-1 {
				result.WriteByte('\n')
			}
			continue
		}

		// If we're inside a code block, don't process Markdown
		if f.inCodeBlock {
			result.Write([]byte(color.New(color.Italic).Sprint(string(line))))
		} else {
			// Normal Markdown processing
			formattedLine := f.formatLine(line)
			result.Write(formattedLine)
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
		bulletPos := prefixLen + 1 // +1 for the bullet character itself

		// Color only the bullet and process the rest of the line for other formatting
		if bulletPos < len(line) {
			var result bytes.Buffer
			result.Write(line[:prefixLen])                                                                 // Write leading whitespace
			result.Write([]byte(color.New(color.FgMagenta).Sprint(string(line[prefixLen : prefixLen+1])))) // Color the bullet
			result.Write([]byte(" "))                                                                      // Add a space after the bullet

			// Process the rest of the line for inline elements
			restOfLine := line[bulletPos:]
			formattedRest := f.formatInlineElements(restOfLine)
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
			result.Write(line[:prefixLen])                                                              // Write leading whitespace
			result.Write([]byte(color.New(color.FgMagenta).Sprint(string(line[prefixLen : numEnd+1])))) // Color the number and dot

			// Process the rest of the line for inline elements
			restOfLine := line[numEnd+2:] // +2 to skip the dot and space
			formattedRest := f.formatInlineElements(restOfLine)
			result.Write([]byte(" ")) // Add the space back
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
		return "``" + color.New(color.FgHiCyan).Sprint(content) + "``"
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

// StreamingWriter is a writer that processes Markdown chunks in a streaming manner
type StreamingWriter struct {
	formatter  *MarkdownFormatter
	outWriter  io.Writer
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// NewStreamingWriter creates a new writer for streaming markdown formatting
func NewStreamingWriter(outWriter io.Writer, options ...FormatterOption) *StreamingWriter {
	ctx, cancel := context.WithCancel(context.Background())

	return &StreamingWriter{
		formatter:  NewMarkdownFormatter(options...),
		outWriter:  outWriter,
		ctx:        ctx,
		cancelFunc: cancel,
	}
}

// Write implements io.Writer interface for streaming processing
func (w *StreamingWriter) Write(p []byte) (n int, err error) {
	// Process the chunk
	formatted := w.formatter.ProcessChunk(p)

	// If we have formatted output, write it
	if len(formatted) > 0 {
		_, err = w.outWriter.Write(formatted)
		if err != nil {
			return 0, err
		}
	}

	// Return the original length to indicate we processed all bytes
	return len(p), nil
}

// Flush flushes any remaining content
func (w *StreamingWriter) Flush() error {
	// Process any remaining content in the buffer
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
