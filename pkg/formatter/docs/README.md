# Markdown Streaming Formatter

This package provides real-time Markdown formatting for streaming text output, particularly designed for LLM responses.

## Features

- Processes Markdown syntax in streaming text chunks
- Colorizes Markdown elements (headings, bold, italic, lists, etc.)
- Handles incomplete Markdown across chunk boundaries
- Line-by-line processing with efficient buffering
- Preserves text alignment while adding ANSI color codes
- Option to disable colorization

## Usage

### Basic Usage

```go
import (
    "os"
    "github.com/mikhae1/kubectl-quackops/pkg/formatter"
)

// Create a streaming writer that outputs to stdout
writer := formatter.NewStreamingWriter(os.Stdout)
defer writer.Close()

// Process text chunks as they arrive
writer.Write([]byte("# This is a heading\n"))
writer.Write([]byte("This is **bold** text and *italic* text\n"))

// Don't forget to flush any pending content
writer.Flush()
```

### Disable Colorization

```go
// Create a writer with color disabled
writer := formatter.NewStreamingWriter(os.Stdout, formatter.WithColorDisabled())
```

### Integration with LLM Streaming

```go
// Example callback for LLM streaming
callback := func(ctx context.Context, chunk []byte) error {
    _, err := writer.Write(chunk)
    return err
}

// Use the callback with your LLM client
options := []llms.CallOption{
    llms.WithStreamingFunc(callback),
}
```

## Supported Markdown Elements

- **Headings** (`#`, `##`, `###`, etc.)
- **Bold** (`**text**`)
- **Italic** (`*text*`)
- **Code spans** (`` `code` ``)
- **Code blocks** (triple backticks)
- **Blockquotes** (`> text`)
- **Lists** (ordered and unordered)
- **Links** (`[text](url)`)

## How It Works

1. Text chunks are accumulated in a buffer
2. Complete lines (ending with newlines) are processed and formatted
3. Markdown syntax is identified and colorized using ANSI escape codes
4. Formatted text is returned while keeping incomplete lines in the buffer
5. When streaming is complete, any remaining content is flushed

## Implementation Details

The formatter uses the [goldmark](https://github.com/yuin/goldmark) library for parsing Markdown and [fatih/color](https://github.com/fatih/color) for ANSI color formatting.
