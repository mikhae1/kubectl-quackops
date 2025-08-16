package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
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
		result.WriteString(dim("-- " + line) + "\n")
	}
	
	// Print output section with config colors
	result.WriteString(dim("-- ") + config.Colors.Label.Sprint("Output:") + "\n")
	for _, line := range strings.Split(output, "\n") {
		result.WriteString(dim("-- " + line) + "\n")
	}
	
	return result.String()
}

// RenderToolCallBlock prints a colored block for an MCP tool call and its output immediately to stdout.
// Prefer FormatToolCallBlock when you need to control ordering relative to other output.
func RenderToolCallBlock(toolName string, args map[string]any, output string) {
	block := FormatToolCallBlock(toolName, args, output)
	fmt.Fprint(os.Stdout, block)
}

