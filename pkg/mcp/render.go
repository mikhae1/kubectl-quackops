package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/formatter"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
)

// FormatToolCallBlock builds a colored block for an MCP tool call and its output and returns it as a string.
// Callers can decide when to print it to avoid interfering with other stdout streams.
func FormatToolCallBlock(toolName string, args map[string]any, output string) string {
	cfg := config.LoadConfig()
	maxLineLen := cfg.ToolOutputMaxLineLen
	maxLines := cfg.ToolOutputMaxLines

	// Prepare args content
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
	argsBlock := strings.Join(argsLines, "\n")

	// Create render block using shared functionality
	block := &lib.RenderBlock{
		Title:      "MCP Tool: " + toolName,
		MaxLineLen: maxLineLen,
		MaxLines:   maxLines,
		Sections: []lib.RenderSection{
			{Label: "Args", Content: argsBlock},
			{Label: "Output", Content: output},
		},
	}

	return block.Format()
}

// FormatToolCallVerbose formats an MCP tool call in verbose mode similar to diagnostic commands.
// This format is best suited for saving full logs.
func FormatToolCallVerbose(toolName string, args map[string]any, output string) string {
	dim := color.New(color.Faint).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	// Prepare args content
	argJSON := "{}"
	if args != nil {
		if b, err := json.MarshalIndent(args, "", "  "); err == nil {
			argJSON = string(b)
		}
	}

	var result strings.Builder

	// Print tool call header similar to diagnostic command format
	result.WriteString(bold("\n$ mcp_tool_call " + toolName))
	result.WriteString("\n")

	// Print args section with config colors
	result.WriteString(dim("-- ") + config.Colors.Label.Sprint("Args:") + "\n")
	for _, line := range strings.Split(argJSON, "\n") {
		result.WriteString(dim("-- "+line) + "\n")
	}

	// Print output section with config colors
	result.WriteString(dim("-- ") + config.Colors.Label.Sprint("Output:") + "\n")
	for _, line := range strings.Split(output, "\n") {
		result.WriteString(dim("-- "+line) + "\n")
	}

	return result.String()
}

// RenderToolCallBlock prints a colored block for an MCP tool call and its output immediately to stdout.
// Prefer FormatToolCallBlock when you need to control ordering relative to other output.
func RenderToolCallBlock(toolName string, args map[string]any, output string) {
	block := FormatToolCallBlock(toolName, args, output)
	fmt.Fprint(os.Stdout, block)
}

// RenderSessionEvent formats a session event for display.
// If verbose is true, tool outputs are shown in full using FormatToolCallVerbose.
// If verbose is false, tool outputs are formatted using FormatToolCallBlock (potentially truncated).
func RenderSessionEvent(event config.SessionEvent, verbose bool, cfg *config.Config) string {
	var sb strings.Builder

	// Format timestamp
	ts := event.Timestamp.Format("15:04:05")
	sb.WriteString(config.Colors.Dim.Sprintf("[%s] ", ts))

	// Format user prompt
	sb.WriteString(config.Colors.Primary.Sprintf("❯ %s\n", event.UserPrompt))

	// Format tool calls
	for _, tc := range event.ToolCalls {
		if verbose {
			sb.WriteString(FormatToolCallVerbose(tc.Name, tc.Args, tc.Result))
		} else {
			sb.WriteString(FormatToolCallBlock(tc.Name, tc.Args, tc.Result))
		}
	}

	// Format AI response
	if event.AIResponse != "" {
		if !verbose && cfg.DisableMarkdownFormat {
			sb.WriteString(event.AIResponse + "\n")
		} else {
			// In verbose/history mode, we want to visually separate the response
			// Re-use formatting logic or just print plain if simple
			// For now, let's keep it simple but clear
			sb.WriteString("\n")
			if !cfg.DisableMarkdownFormat {
				// Use the MarkdownFormatter to render the response with syntax highlighting
				f := formatter.NewMarkdownFormatter(
					formatter.WithColorEnabled(true),
				)
				// Process the entire response at once
				formatted := f.ProcessChunk([]byte(event.AIResponse))
				// Process any remaining content (flush)
				formatted = append(formatted, f.Flush()...)

				sb.Write(formatted)
			} else {
				sb.WriteString(event.AIResponse)
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}
