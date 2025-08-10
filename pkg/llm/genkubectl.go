package llm

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
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
	augPrompt := kubectlPrompt + "\n\nIssue description: " + prompt + "\n\nProvide commands as a plain list without descriptions or backticks."

	// Create a spinner for command generation
	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Suffix = " Generating diagnostic commands..."
	s.Color("blue", "bold")
	s.Start()
	defer s.Stop()

	// Execute request without updating the conversation history
	// Preference: ask for commands as plain lines (current behavior). Future: instruct JSON and parse.
	response, err := Request(cfg, augPrompt, false, false)

	if err != nil {
		return nil, fmt.Errorf("error requesting kubectl diagnostics: %w", err)
	}

	// Extract valid kubectl commands using regex pattern
	commandsPattern := strings.Join(cfg.AllowedKubectlCmds, "|")
	rePattern := `kubectl\s(?:` + commandsPattern + `)\s?[^` + "`" + `%#\n]*`
	re := regexp.MustCompile(rePattern)

	matches := re.FindAllString(response, -1)
	if matches == nil {
		return nil, errors.New("no valid kubectl commands found in response")
	}

	// Remove template commands with placeholders and duplicates
	anonCmdRe := regexp.MustCompile(`.*<[A-Za-z_-]+>.*`)
	var filteredCmds []string
	for _, match := range matches {
		trimCmd := strings.TrimSpace(match)
		// Skip empty commands, commands with placeholders, and duplicates
		if trimCmd == "" || anonCmdRe.MatchString(trimCmd) || slices.Contains(filteredCmds, trimCmd) {
			continue
		}
		// Extra validation: ensure command has at least kubectl and one argument
		parts := strings.Fields(trimCmd)
		if len(parts) >= 2 && parts[0] == "kubectl" {
			filteredCmds = append(filteredCmds, trimCmd)
		}
	}

	// Apply compaction to remove any empty strings and duplicates
	filteredCmds = slices.Compact(filteredCmds)

	// Log and return error if no valid commands found after filtering
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
