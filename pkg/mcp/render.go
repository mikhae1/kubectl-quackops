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

// RenderToolCallBlock prints a colored block for an MCP tool call and its output immediately to stdout.
// Prefer FormatToolCallBlock when you need to control ordering relative to other output.
func RenderToolCallBlock(toolName string, args map[string]any, output string) {
	block := FormatToolCallBlock(toolName, args, output)
	fmt.Fprint(os.Stdout, block)
}

