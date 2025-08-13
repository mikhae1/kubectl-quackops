package formatter

import (
	"bytes"
	"testing"
)

func TestMarkdownFormatter_ProcessChunk(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{
			name:   "heading chunks",
			chunks: []string{"# He", "ading", "\n"},
			want:   "# Heading\n",
		},
		{
			name:   "bold across chunks",
			chunks: []string{"This is **bo", "ld** text", "\n"},
			want:   "This is **bold** text\n",
		},
		{
			name:   "code block across chunks",
			chunks: []string{"```\n", "code\n", "block\n", "```\n"},
			want:   "```\ncode\nblock\n```\n",
		},
		{
			name:   "multiple lines in chunks",
			chunks: []string{"Line 1\nLi", "ne 2\nLine 3\n"},
			want:   "Line 1\nLine 2\nLine 3\n",
		},
		{
			name:   "single backtick code",
			chunks: []string{"Text with `co", "de` in it", "\n"},
			want:   "Text with `code` in it\n",
		},
		{
			name:   "double backtick code",
			chunks: []string{"Text with ``co", "de`` in it", "\n"},
			want:   "Text with ``code`` in it\n",
		},
		{
			name:   "mixed backtick code",
			chunks: []string{"Single `code` and double ``co", "de`` here", "\n"},
			want:   "Single `code` and double ``code`` here\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewMarkdownFormatter(WithColorDisabled())
			var output bytes.Buffer

			// Process chunks
			for _, chunk := range tt.chunks {
				formatted := f.ProcessChunk([]byte(chunk))
				if formatted != nil {
					output.Write(formatted)
				}
			}

			// Flush remaining content
			flush := f.Flush()
			if flush != nil {
				output.Write(flush)
			}

			got := output.String()
			if got != tt.want {
				t.Errorf("ProcessChunk() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStreamingWriter(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   string
	}{
		{
			name:   "simple paragraph",
			chunks: []string{"This is a ", "simple ", "paragraph.\n"},
			want:   "This is a simple paragraph.\n",
		},
		{
			name:   "with markdown elements",
			chunks: []string{"# Heading\n", "**Bold** and *italic*\n", "- List item\n"},
			want:   "# Heading\n**Bold** and *italic*\n- List item\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			writer := NewStreamingWriter(&output, WithColorDisabled())

			// Write chunks
			for _, chunk := range tt.chunks {
				n, err := writer.Write([]byte(chunk))
				if err != nil {
					t.Errorf("StreamingWriter.Write() error = %v", err)
					return
				}
				if n != len(chunk) {
					t.Errorf("StreamingWriter.Write() wrote %d bytes, want %d", n, len(chunk))
				}
			}

			// Flush and close
			if err := writer.Flush(); err != nil {
				t.Errorf("StreamingWriter.Flush() error = %v", err)
				return
			}
			if err := writer.Close(); err != nil {
				t.Errorf("StreamingWriter.Close() error = %v", err)
				return
			}

			got := output.String()
			if got != tt.want {
				t.Errorf("StreamingWriter output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormattedMarkdown(t *testing.T) {
	// For visual testing (no color check)
	testCases := []struct {
		name     string
		markdown string
	}{
		{
			name:     "headings",
			markdown: "# Heading 1\n## Heading 2\n### Heading 3\n",
		},
		{
			name:     "text formatting",
			markdown: "**Bold text** and *italic text*\n",
		},
		{
			name:     "lists",
			markdown: "- Item 1\n- Item 2\n  - Nested item\n\n1. Numbered item 1\n2. Numbered item 2\n",
		},
		{
			name:     "code",
			markdown: "Inline `code` and:\n```\ncode block\n```\n",
		},
		{
			name:     "blockquote",
			markdown: "> This is a blockquote\n> With multiple lines\n",
		},
		{
			name:     "links",
			markdown: "This is a [link](https://example.com)\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Crash-only test; visual verification is manual
			f := NewMarkdownFormatter()
			formatted := f.ProcessChunk([]byte(tc.markdown))
			if formatted == nil {
				t.Fatal("expected formatted output, got nil")
			}
		})
	}
}

func TestCodeBlockMarkerRegex(t *testing.T) {
	// Use the regex from markdown.go
	regex := codeBlockMarkerRegex

	testCases := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "Basic code block",
			input:    "```",
			expected: true,
		},
		{
			name:     "Code block with language",
			input:    "```go",
			expected: true,
		},
		{
			name:     "Code block with hyphenated language",
			input:    "```bash-script",
			expected: true,
		},
		{
			name:     "Code block with space",
			input:    " ```bash",
			expected: true,
		},
		{
			name:     "Not a code block - indent",
			input:    "not```bash",
			expected: false,
		},
		{
			name:     "Special chars in lang",
			input:    "```c++",
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := regex.MatchString(tc.input)
			if result != tc.expected {
				t.Errorf("expected %v, got %v for input: %s", tc.expected, result, tc.input)
			}
		})
	}
}
