package filter

import (
	"encoding/json"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// SensitiveData hides sensitive data from Kubectl outputs
func SensitiveData(input string) string {
	var data map[string]interface{}

	// Try to parse as JSON, if fails, try YAML
	inputType := ""
	if err := json.Unmarshal([]byte(input), &data); err == nil {
		inputType = "json"
	} else if err := yaml.Unmarshal([]byte(input), &data); err == nil {
		inputType = "yaml"
	} else {
		// Handle plain text for describe commands
		input = DescribeOutput(input)
		// Apply pattern filtering after handling describe output
		return PatternSanitizer(input)
	}

	// Check if the kind is "Secret" or "ConfigMap"
	if kind, ok := data["kind"].(string); ok {
		// Check for data or stringData fields
		for _, field := range []string{"data", "stringData"} {
			if _, found := data[field]; found {
				section := data[field].(map[string]interface{})
				newSection := make(map[string]interface{})
				for key, val := range section {
					if strVal, ok := val.(string); ok && strVal != "" {
						// For ConfigMaps, apply pattern sanitization instead of full filtering
						if kind == "ConfigMap" {
							newSection[key] = PatternSanitizer(strVal)
						} else {
							// For Secrets, completely filter the values
							newSection[key] = "***FILTERED***"
						}
					} else {
						newSection[key] = val
					}
				}
				data[field] = newSection
			}
		}
	}

	// Serialize back
	var output []byte
	var err error
	if inputType == "yaml" {
		output, err = yaml.Marshal(data)
	} else if inputType == "json" {
		output, err = json.Marshal(data)
	}

	if err != nil {
		return PatternSanitizer(input) // Apply pattern filtering before returning original input
	}

	// Apply pattern filtering after handling structured data
	return PatternSanitizer(string(output))
}

// DescribeOutput filters sections from the kubectl describe output for Data section
func DescribeOutput(input string) string {
	var isConfigHeader = func(lines []string, index int) bool {
		if index+1 < len(lines) {
			return strings.HasPrefix(strings.TrimSpace(lines[index]), "Name:") &&
				strings.HasPrefix(strings.TrimSpace(lines[index+1]), "Namespace:")
		}
		return false
	}

	var isSectionHeader = func(lines []string, index int) bool {
		if index+1 < len(lines) {
			return lines[index+1] == "===="
		}
		return false
	}

	var filteredOutput []string
	edit := false

	lines := strings.Split(input, "\n")
	i := 0
	for i < len(lines) {
		if isConfigHeader(lines, i) {
			edit = true
		}

		if edit && isSectionHeader(lines, i) && strings.HasPrefix(lines[i], "Data") {
			// Append the field name, the '====', and the filter placeholder
			filteredOutput = append(filteredOutput, lines[i], lines[i+1], "***FILTERED***", "")
			i += 2 // Move past the header and '===='
			// Skip the content under the current header
			for i < len(lines) && !isSectionHeader(lines, i) && !isConfigHeader(lines, i) {
				i++
			}
		} else {
			// Add non-filtered lines directly to the output
			filteredOutput = append(filteredOutput, lines[i])
			i++
		}
	}

	return strings.Join(filteredOutput, "\n")
}

// PatternSanitizer filters sensitive information based on common patterns
func PatternSanitizer(input string) string {
	// Define patterns for sensitive information with their replacements
	patternReplacements := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		// Passwords
		{
			regexp.MustCompile(`(?i)(password|passwd)\s*[=:]\s*[^\s]+`),
			"$1=***FILTERED***",
		},
		// YAML formatted values (with or without quotes)
		{
			regexp.MustCompile(`(?i)(DB_PASS|password|passwd|secret):\s*['"]?([^'\"\s]+)['"]?`),
			"$1: '***FILTERED***'",
		},
		// Usernames with credentials
		{
			regexp.MustCompile(`(?i)(username|user)\s*[=:]\s*[^\s]+`),
			"$1=***FILTERED***",
		},
		// API keys and tokens
		{
			regexp.MustCompile(`(?i)(api[_-]?key|token|secret|auth)\s*[=:]\s*[^\s]+`),
			"$1=***FILTERED***",
		},
		// Connection strings with underscore
		{
			regexp.MustCompile(`(?i)(connection_string|conn_string|jdbc_url)\s*[=:]\s*[^\s]+`),
			"$1=***FILTERED***",
		},
		// Connection strings with space
		{
			regexp.MustCompile(`(?i)(connection|conn)\s*[=:]\s*[^\s]+`),
			"$1=***FILTERED***",
		},
		// Generic credential pattern
		{
			regexp.MustCompile(`(?i)(credential|cred)\s*[=:]\s*[^\s]+`),
			"$1=***FILTERED***",
		},
		// Bearer tokens
		{
			regexp.MustCompile(`(?i)(bearer)\s+[a-zA-Z0-9_\-\.]+`),
			"$1 ***FILTERED***",
		},
		// Basic auth
		{
			regexp.MustCompile(`(?i)(basic)\s+[a-zA-Z0-9+/=]+`),
			"$1 ***FILTERED***",
		},
	}

	result := input
	for _, pr := range patternReplacements {
		result = pr.pattern.ReplaceAllString(result, pr.replacement)
	}

	return result
}

// FilterCommand filters sensitive information from kubectl commands
// This is used to sanitize commands before logging or displaying them
func FilterCommand(command string) string {
	if command == "" {
		return command
	}

	// Define patterns for sensitive command parameters with their replacements
	cmdPatternReplacements := []struct {
		pattern     *regexp.Regexp
		replacement string
	}{
		// From-literal with sensitive data
		{
			regexp.MustCompile(`(?i)--from-literal[= ]([a-zA-Z0-9_-]+)=([^\s]+)`),
			"--from-literal=$1=***FILTERED***",
		},
		// From-literal with quotes
		{
			regexp.MustCompile(`(?i)--from-literal[= ]"([a-zA-Z0-9_-]+)=([^\"]+)"`),
			"--from-literal=\"$1=***FILTERED***\"",
		},
		// From-literal with single quotes
		{
			regexp.MustCompile(`(?i)--from-literal[= ]'([a-zA-Z0-9_-]+)=([^\']+)'`),
			"--from-literal='$1=***FILTERED***'",
		},
		// Environment variables assignments
		{
			regexp.MustCompile(`(?i)(?:^|\s)([A-Za-z0-9_]+)=(password|secret|key|token|credential|username|apikey)`),
			"$1=***FILTERED***",
		},
		// Common sensitive data patterns in kubectl commands
		{
			regexp.MustCompile(`(?i)(username|password|token|secret|api[_-]?key)[=: ]([^\s]+)`),
			"$1=***FILTERED***",
		},
	}

	result := command
	for _, pr := range cmdPatternReplacements {
		result = pr.pattern.ReplaceAllString(result, pr.replacement)
	}

	// Apply standard pattern sanitization as a second pass for anything missed
	return PatternSanitizer(result)
}
