package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

// GenKubectlCmds retrieves kubectl commands based on the user input
func GenKubectlCmds(cfg *config.Config, prompt string, userMsgCount int) ([]string, error) {
	// Generate a context-aware prompt based on user input
	kubectlPrompt := ""
	if userMsgCount == 1 {
		// Use a longer prompt for the first user message
		kubectlPrompt = genKubectlPrompt(cfg, prompt)
	} else {
		// Use a shorter prompt for repeated interactions to avoid repetition
		kubectlPrompt = cfg.KubectlShortPrompt
		logger.Log("info", "Using condensed prompt for context efficiency")
	}

	// Construct the final prompt with clear instructions
	var augPromptBuilder strings.Builder
	augPromptBuilder.WriteString(kubectlPrompt)
	augPromptBuilder.WriteString("\n\nIssue description: ")
	augPromptBuilder.WriteString(prompt)
	if cfg.KubectlReturnJSON {
		augPromptBuilder.WriteString("\n\nRespond ONLY with a JSON array of strings (no prose, no code fences). ")
		if cfg.KubectlMaxSuggestions > 0 {
			augPromptBuilder.WriteString(fmt.Sprintf("Return at most %d items.", cfg.KubectlMaxSuggestions))
		}
		augPromptBuilder.WriteString(" Example: [\"kubectl get pods -A -o wide\", \"kubectl get events -A -o json\"].")
	} else {
		augPromptBuilder.WriteString("\n\nProvide commands as a plain list without descriptions or backticks.")
		if cfg.KubectlMaxSuggestions > 0 {
			augPromptBuilder.WriteString(fmt.Sprintf(" Limit to at most %d lines.", cfg.KubectlMaxSuggestions))
		}
	}
	augPrompt := augPromptBuilder.String()

	// Create spinner for command generation using SpinnerManager
	spinnerManager := lib.GetSpinnerManager(cfg)
	cancelSpinner := spinnerManager.ShowGeneration("🛠️ " + config.Colors.Info.Sprint("Generating") + " " + config.Colors.Dim.Sprint("diagnostic commands..."))
	defer cancelSpinner()

	// Execute request without updating the conversation history, silently
	response, err := RequestSilent(cfg, augPrompt, false, false)

	if err != nil {
		return nil, fmt.Errorf("error requesting kubectl diagnostics: %w", err)
	}

	// Helper to filter, validate and cap results
	filterAndValidate := func(cmds []string) []string {
		if len(cmds) == 0 {
			return cmds
		}
		anonCmdRe := regexp.MustCompile(`.*<[A-Za-z_-]+>.*`)
		var filtered []string
		for _, c := range cmds {
			trimCmd := strings.TrimSpace(c)
			if trimCmd == "" || anonCmdRe.MatchString(trimCmd) || slices.Contains(filtered, trimCmd) {
				continue
			}
			parts := strings.Fields(trimCmd)
			if len(parts) < 2 || parts[0] != "kubectl" {
				continue
			}
			filtered = append(filtered, trimCmd)
		}
		filtered = slices.Compact(filtered)
		// Gate by allowed verbs
		if len(filtered) > 0 {
			commandsPattern := strings.Join(cfg.AllowedKubectlCmds, "|")
			rePattern := `^kubectl\s(?:` + commandsPattern + `)\b[^` + "`" + `%\n]*`
			re := regexp.MustCompile(rePattern)
			valid := make([]string, 0, len(filtered))
			for _, c := range filtered {
				if re.MatchString(c) {
					valid = append(valid, c)
				}
			}
			filtered = valid
		}
		if cfg.KubectlMaxSuggestions > 0 && len(filtered) > cfg.KubectlMaxSuggestions {
			filtered = filtered[:cfg.KubectlMaxSuggestions]
		}
		return filtered
	}

	var filteredCmds []string
	if cfg.KubectlReturnJSON {
		// Try to parse the response as JSON array of strings
		var arr []string
		if err := json.Unmarshal([]byte(response), &arr); err != nil {
			// Try to locate a JSON array in the text
			start := strings.Index(response, "[")
			end := strings.LastIndex(response, "]")
			if start >= 0 && end > start {
				var arr2 []string
				sub := response[start : end+1]
				if err := json.Unmarshal([]byte(sub), &arr2); err == nil {
					arr = arr2
				}
			}
		}
		filteredCmds = filterAndValidate(arr)
	}

	// Fallback to regex extraction
	if len(filteredCmds) == 0 {
		commandsPattern := strings.Join(cfg.AllowedKubectlCmds, "|")
		rePattern := `kubectl\s(?:` + commandsPattern + `)\s?[^` + "`" + `%#\n]*`
		re := regexp.MustCompile(rePattern)
		matches := re.FindAllString(response, -1)
		filteredCmds = filterAndValidate(matches)
	}

	if len(filteredCmds) == 0 {
		return nil, errors.New("no valid kubectl commands found after filtering")
	}

	logger.Log("info", "Generated kubectl commands: \"%v\"", strings.Join(filteredCmds, ", "))
	return filteredCmds, nil
}

// genKubectlPrompt generates a context-aware prompt based on the user's query
func genKubectlPrompt(cfg *config.Config, prompt string) string {
	// Function to create formatted command strings
	createCommand := func(cmd string) string {
		return "kubectl " + cmd
	}

	// Core system prompt with clear role and purpose
	systemPrompt := cfg.KubectlStartPrompt

	// Analyze the query to determine the appropriate focus areas
	p := strings.ToLower(prompt)
	useDefaultCmds := true

	// Apply specialized prompt extensions based on detected patterns
	for _, kp := range cfg.KubectlPrompts {
		if kp.MatchRe.MatchString(p) {
			// Add specialized context based on the matched pattern
			systemPrompt += "\n" + strings.TrimSpace(kp.Prompt)

			if !kp.UseDefaultCmds {
				// Add specialized commands for this specific context
				var kubectlCmds []string
				for _, cmd := range kp.AllowedKubectls {
					kubectlCmds = append(kubectlCmds, createCommand(cmd))
				}
				systemPrompt += "\n\nRelevant commands for this scenario: " + strings.Join(kubectlCmds, ", ")
				useDefaultCmds = kp.UseDefaultCmds
			}
		}
	}

	// Add default command examples if no specialized ones were used
	if useDefaultCmds {
		var defaultKubectlCmds []string
		for _, cmd := range cfg.AllowedKubectlCmds {
			defaultKubectlCmds = append(defaultKubectlCmds, createCommand(cmd))
		}
		systemPrompt += "\n\nCommand reference: " + strings.Join(defaultKubectlCmds, ", ")
	}

	// Add formatting instructions
	systemPrompt += cfg.KubectlFormatPrompt

	return systemPrompt
}
