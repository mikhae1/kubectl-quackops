package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/exec"
	"github.com/mikhae1/kubectl-quackops/pkg/filter"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

// RetrieveRAG retrieves the data for RAG
func RetrieveRAG(cfg *config.Config, prompt string, lastTextPrompt string, userMsgCount int) (augPrompt string, err error) {
	var cmdResults []config.CmdRes
	var cmds []string

	// First get the kubectl commands without showing the spinner
	for i := 0; i < cfg.Retries; i++ {
		cmds, err = GenKubectlCmds(cfg, prompt, userMsgCount)
		if err != nil {
			logger.Log("warn", "Error retrieving kubectl commands: %v", err)
			continue
		}
		if len(cmds) > 0 {
			break
		}
	}

	if len(cmds) == 0 {
		return "", fmt.Errorf("no valid kubectl commands found")
	}

	// Create a spinner for diagnostic information gathering only if we're not in safe mode
	// In safe mode, the spinner will be managed by execDiagCmds for each command
	var s *spinner.Spinner
	if !cfg.SafeMode {
		s = spinner.New(spinner.CharSets[11], 10*time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
		s.Suffix = " Gathering diagnostic information..."
		s.Color("cyan", "bold")
		s.Start()
		defer s.Stop()
	}

	// Execute the diagnostic commands (no need for slices.Compact as cmds is already filtered)
	cmdResults, err = exec.ExecDiagCmds(cfg, cmds)
	if err != nil {
		logger.Log("warn", "Error executing diagnostic commands: %v", err)
	}

	// Check if we have valid results
	if len(cmdResults) == 0 {
		return "", fmt.Errorf("no valid command results found")
	}

	augPrompt = formatCommandResultsForRAG(cfg, prompt, cmdResults)
	return augPrompt, err
}

// CreateAugPromptFromCmdResults formats command results for RAG in the same way as retrieveRAG
func CreateAugPromptFromCmdResults(cfg *config.Config, prompt string, cmdResults []config.CmdRes) (augPrompt string, err error) {
	if len(cmdResults) == 0 {
		return "", fmt.Errorf("no command results provided")
	}

	augPrompt = formatCommandResultsForRAG(cfg, prompt, cmdResults)
	return augPrompt, nil
}

// formatCommandResultsForRAG formats command results for RAG
func formatCommandResultsForRAG(cfg *config.Config, prompt string, cmdResults []config.CmdRes) string {
	// Process command outputs
	var contextBuilder strings.Builder
	var validCommandCount int

	// Format each command result for context
	for _, cmd := range cmdResults {
		// Skip commands with empty command strings or empty outputs
		if strings.TrimSpace(cmd.Cmd) == "" || strings.TrimSpace(cmd.Out) == "" {
			continue
		}

		// Filter sensitive data if enabled
		output := cmd.Out
		if !cfg.DisableSecretFilter {
			output = filter.SensitiveData(output)
		}

		// Format the command and its output
		contextBuilder.WriteString("Command: ")
		contextBuilder.WriteString(cmd.Cmd)
		contextBuilder.WriteString("\n\nOutput:\n")

		// Include error information for timeout errors or append normal output
		if cmd.Err != nil {
			if strings.Contains(cmd.Err.Error(), "timed out") {
				// If it's a timeout error, use both the output and error message
				contextBuilder.WriteString(output)
				validCommandCount++
			}
			// Skip other types of errors
			continue
		} else {
			// Normal output for successful commands
			contextBuilder.WriteString(output)
			validCommandCount++
		}

		contextBuilder.WriteString("\n\n---\n\n")
	}

	// If no valid commands were processed, return an empty string
	if validCommandCount == 0 {
		return ""
	}

	contextData := contextBuilder.String()

	// If the context is too large, use semantic trimming via embeddings
	if len(lib.Tokenize(contextData)) > int(float64(cfg.MaxTokens)/5) {
		sections := strings.Split(contextData, "\n\n---\n\n")
		ctx := context.Background()

		// Get embedder for semantic search
		logger.Log("info", "Init embedder for semantic search...")
		embedder, err := GetEmbedder(cfg)
		if err != nil {
			logger.Log("warn", "Error creating embedder: %v", err)
			// Improved fallback: trim all sections proportionally instead of just taking the first section
			trimmedSections := trimAllSectionsProportionally(sections, cfg.MaxTokens/5)
			contextData = strings.Join(trimmedSections, "\n\n---\n\n")
		} else {
			// Use the embedder to semantically select the most relevant sections
			selected := TrimSectionsWithEmbeddings(ctx, embedder, sections, prompt, cfg.MaxTokens/2)
			if len(selected) > 0 {
				contextData = strings.Join(selected, "\n\n---\n\n")
			} else {
				// Improved fallback: trim all sections proportionally instead of just taking the first section
				trimmedSections := trimAllSectionsProportionally(sections, cfg.MaxTokens/3)
				contextData = strings.Join(trimmedSections, "\n\n---\n\n")
			}
		}
	}

	// Customize output format based on the provider
	outputFormat := ""
	if !cfg.DisableMarkdownFormat {
		outputFormat = cfg.MarkdownFormatPrompt
	} else {
		outputFormat = cfg.PlainFormatPrompt
	}

	// Construct the final prompt with clear instructions
	var augPrompt string
	if len(contextData) > 0 {
		augPrompt = fmt.Sprintf(cfg.DiagnosticAnalysisPrompt, contextData, prompt, outputFormat)
	}

	return augPrompt
}

// trimAllSectionsProportionally reduces the size of all sections proportionally to fit within maxTokens
// It preserves the command and the beginning of each output which is often the most important part
func trimAllSectionsProportionally(sections []string, maxTokens int) []string {
	if len(sections) == 0 {
		return sections
	}

	// Count total tokens across all sections
	totalTokens := 0
	sectionTokens := make([]int, len(sections))
	for i, section := range sections {
		tokens := len(lib.Tokenize(section))
		sectionTokens[i] = tokens
		totalTokens += tokens
	}

	// If we're already under the limit, return as is
	if totalTokens <= maxTokens {
		return sections
	}

	// Calculate how much we need to trim (as a percentage)
	reductionRatio := float64(maxTokens) / float64(totalTokens)

	// Create a new slice for our trimmed sections
	trimmedSections := make([]string, len(sections))

	for i, section := range sections {
		// Get target token count for this section
		targetTokens := int(float64(sectionTokens[i]) * reductionRatio)
		if targetTokens < 10 { // Preserve at least a minimal amount
			targetTokens = 10
		}

		// Split into command and output parts
		parts := strings.SplitN(section, "\n\nOutput:\n", 2)

		if len(parts) == 2 {
			// We have a command and output
			command := parts[0]
			output := parts[1]

			// Check if command part itself exceeds our target
			commandTokens := len(lib.Tokenize(command))
			if commandTokens >= targetTokens {
				// If the command itself is too long, preserve it but trim it
				if targetTokens < 5 {
					targetTokens = 5 // Absolute minimum
				}
				commandRunes := []rune(command)
				if len(commandRunes) > targetTokens*4 { // Rough approximation of tokens to characters
					trimmedSections[i] = string(commandRunes[:targetTokens*4]) + "..."
				} else {
					trimmedSections[i] = command
				}
			} else {
				// Preserve the command and trim the output
				// We don't need to explicitly calculate the target token count for output

				// If output has more lines, get the first N lines that fit
				outputLines := strings.Split(output, "\n")

				// Start with the command
				trimmedSection := command + "\n\nOutput:\n"

				// Add as many lines as we can
				currentTokens := commandTokens + 3 // +3 for "\n\nOutput:\n"
				outputTokens := 0
				linesAdded := 0

				for _, line := range outputLines {
					lineTokens := len(lib.Tokenize(line + "\n"))
					if currentTokens+lineTokens > targetTokens {
						// We can't add more lines without exceeding the target
						break
					}

					trimmedSection += line + "\n"
					currentTokens += lineTokens
					outputTokens += lineTokens
					linesAdded++
				}

				// If we added lines and there are more we couldn't add, indicate truncation
				if linesAdded > 0 && linesAdded < len(outputLines) {
					trimmedSection += "...\n[Output truncated for length]"
				}

				trimmedSections[i] = trimmedSection
			}
		} else {
			// No clear command/output separation, just trim by characters as a fallback
			runes := []rune(section)
			charLimit := targetTokens * 4 // Very rough approximation of tokens to characters
			if len(runes) > charLimit {
				trimmedSections[i] = string(runes[:charLimit]) + "...\n[Content truncated for length]"
			} else {
				trimmedSections[i] = section
			}
		}
	}

	return trimmedSections
}
