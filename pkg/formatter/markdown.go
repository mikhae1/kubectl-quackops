package formatter

import (
	"bytes"
	"context"
	"io"
	"regexp"

	"github.com/fatih/color"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// MarkdownFormatter handles streaming Markdown formatting
type MarkdownFormatter struct {
	buffer       bytes.Buffer // Buffer for accumulating text
	colorEnabled bool
	markdown     goldmark.Markdown
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
		markdown:     goldmark.New(),
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
	// Add the new chunk to our buffer
	f.buffer.Write(chunk)

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

	result := f.formatMarkdown(f.buffer.Bytes())
	f.buffer.Reset()
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

		formattedLine := f.formatLine(line)
		result.Write(formattedLine)

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
				return []byte(color.New(color.FgHiBlue, color.Bold).Sprint(string(line)))
			case 2:
				return []byte(color.New(color.FgBlue, color.Bold).Sprint(string(line)))
			default:
				return []byte(color.New(color.FgCyan, color.Bold).Sprint(string(line)))
			}
		}
	}

	// Blockquotes
	if bytes.HasPrefix(line, []byte(">")) {
		return []byte(color.New(color.FgHiGreen).Sprint(string(line)))
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
			result.Write(line[:prefixLen])                                                                   // Write leading whitespace
			result.Write([]byte(color.New(color.FgHiMagenta).Sprint(string(line[prefixLen : prefixLen+1])))) // Color the bullet
			result.Write([]byte(" "))                                                                        // Add a space after the bullet

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
			result.Write(line[:prefixLen])                                                                // Write leading whitespace
			result.Write([]byte(color.New(color.FgHiMagenta).Sprint(string(line[prefixLen : numEnd+1])))) // Color the number and dot

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
	reader := text.NewReader(line)
	doc := f.markdown.Parser().Parse(reader)

	var result bytes.Buffer
	source := line

	// Walk the parsed AST and format inline elements
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n.Kind() {
		case ast.KindText:
			text := n.(*ast.Text)
			// Process the text to colorize quoted strings
			processedText := f.colorizeQuotedStrings(text.Segment.Value(source))
			result.Write(processedText)

		case ast.KindEmphasis:
			em := n.(*ast.Emphasis)
			var content string
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if textNode, ok := c.(*ast.Text); ok {
					content += string(textNode.Segment.Value(source))
				}
			}

			if em.Level == 2 { // Bold
				result.Write([]byte(color.New(color.Bold).Sprint(content)))
			} else { // Italic
				result.Write([]byte(color.New(color.Italic).Sprint(content)))
			}
			return ast.WalkSkipChildren, nil

		case ast.KindCodeSpan:
			var content string
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if textNode, ok := c.(*ast.Text); ok {
					content += string(textNode.Segment.Value(source))
				}
			}
			result.Write([]byte(color.New(color.FgHiYellow).Sprint(content)))
			return ast.WalkSkipChildren, nil

		case ast.KindLink:
			var text string
			var destination string

			if link, ok := n.(*ast.Link); ok {
				destination = string(link.Destination)
			}

			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				if textNode, ok := c.(*ast.Text); ok {
					text += string(textNode.Segment.Value(source))
				}
			}

			if text != "" && destination != "" {
				result.Write([]byte(color.New(color.FgHiBlue, color.Underline).Sprintf("%s (%s)", text, destination)))
			} else if text != "" {
				result.Write([]byte(color.New(color.FgHiBlue, color.Underline).Sprint(text)))
			}
			return ast.WalkSkipChildren, nil
		}

		return ast.WalkContinue, nil
	})

	// If nothing was formatted or there was an error, return the original line
	if result.Len() == 0 {
		return line
	}

	return result.Bytes()
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
		result.Write([]byte(color.New(color.FgHiGreen).Sprint(string(quotedText))))
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
		result.Write([]byte(color.New(color.FgHiCyan).Sprint(string(quotedContent))))

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
