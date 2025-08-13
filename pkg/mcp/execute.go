package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// ExecuteTool executes an MCP tool and pretty-prints JSON responses when possible.
func ExecuteTool(cfg *config.Config, toolName string, args map[string]any) (string, error) {
	result, err := CallToolByName(cfg, toolName, args)
	if err != nil {
		return "", fmt.Errorf("MCP tool execution failed: %w", err)
	}
	trimmed := strings.TrimSpace(result)
	if trimmed != "" && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
		var jsonData any
		if err := json.Unmarshal([]byte(trimmed), &jsonData); err == nil {
			if formatted, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
				return string(formatted), nil
			}
		}
	}
	return result, nil
}
