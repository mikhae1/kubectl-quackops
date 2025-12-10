package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/style"
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
		argsLines = append(argsLines[:20], lipgloss.NewStyle().Faint(true).Render("… (args truncated)"))
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
	dim := lipgloss.NewStyle().Faint(true).Render
	bold := lipgloss.NewStyle().Bold(true).Render

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
	result.WriteString(dim("-- ") + config.Colors.Label.Render("Args:") + "\n")
	for _, line := range strings.Split(argJSON, "\n") {
		result.WriteString(dim("-- "+line) + "\n")
	}

	// Print output section with config colors
	result.WriteString(dim("-- ") + config.Colors.Label.Render("Output:") + "\n")
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

// RenderSessionEvent renders a single session event (user or assistant)
func RenderSessionEvent(event config.SessionEvent, isHistory bool, cfg *config.Config) string {
	var sb strings.Builder

	timestampStyle := style.Debug
	toolUseStyle := style.Warning
	toolResultStyle := style.Info

	// Timestamp
	sb.WriteString(timestampStyle.Render(event.Timestamp.Format("15:04:05")) + " ")

	// User Prompt (if present)
	if event.UserPrompt != "" {
		sb.WriteString(style.Title.Render("YOU") + ": ")
		sb.WriteString(event.UserPrompt + "\n")
	}

	// Tool Calls
	if len(event.ToolCalls) > 0 {
		sb.WriteString(toolUseStyle.Render(fmt.Sprintf("Called %d tools:", len(event.ToolCalls))) + "\n")
		for _, tc := range event.ToolCalls {
			// Args is map[string]any, convert to string representation
			argsStr := fmt.Sprintf("%v", tc.Args)
			sb.WriteString("  - " + toolUseStyle.Render(tc.Name) + " " + toolResultStyle.Render(argsStr) + "\n")
			if tc.Result != "" {
				lines := strings.Split(tc.Result, "\n")
				if len(lines) > 3 {
					sb.WriteString("    Result: " + strings.Join(lines[:3], "\n") + "...\n")
				} else {
					sb.WriteString("    Result: " + tc.Result + "\n")
				}
			}
		}
	}

	// AI Response
	if event.AIResponse != "" {
		sb.WriteString(style.Command.Render("DUCK") + ": ")
		sb.WriteString(event.AIResponse + "\n")
	}

	return sb.String()
}
