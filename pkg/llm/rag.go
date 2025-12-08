package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/diag"
	"github.com/mikhae1/kubectl-quackops/pkg/exec"
	"github.com/mikhae1/kubectl-quackops/pkg/filter"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

// RetrieveRAG retrieves the data for RAG
func RetrieveRAG(cfg *config.Config, prompt string, lastTextPrompt string, userMsgCount int) (augPrompt string, err error) {
	var cmdResults []config.CmdRes
	var cmds []string

	// In strict MCP mode (only when MCP client is enabled), skip generating and executing
	// LLM-suggested kubectl commands. We'll rely on baseline (if enabled) and MCP tools only.
	if !(cfg.MCPClientEnabled && cfg.MCPStrict) {
		// First get the kubectl commands without showing the spinner
		maxAttempts := cfg.Retries + 1 // Ensure at least one attempt even if retries=0
		for i := 0; i < maxAttempts; i++ {
			cmds, err = GenKubectlCmds(cfg, prompt, userMsgCount)
			if err != nil {
				if lib.IsUserCancel(err) {
					return "", err
				}
				logger.Log("warn", "Error retrieving kubectl commands: %v", err)
				continue
			}
			if len(cmds) > 0 {
				break
			}
		}
		if len(cmds) == 0 {
			logger.Log("info", "No kubectl diagnostics generated; continuing without user diagnostics")
		}
	}

	// Prepend baseline commands only for the first user query when enabled
	if cfg.EnableBaseline && userMsgCount == 1 {
		base := diag.BaselineCommands(cfg)
		if len(base) > 0 {
			logger.Log("info", "Baseline enabled: running %d command(s)", len(base))
			// Run baseline first and append to results so they can be reused without re-running
			baseRes, _ := exec.ExecDiagCmds(cfg, base)
			if len(baseRes) > 0 {
				// merge baseline results into stored results for prompt assembly below
				cmdResults = append(cmdResults, baseRes...)
				logger.Log("info", "Baseline collected %d result(s)", len(baseRes))
			}
		}
	}

	if !(cfg.MCPClientEnabled && cfg.MCPStrict) && len(cmds) > 0 {
		// Create spinner for diagnostic information gathering only if we're not in safe mode
		// In safe mode, the spinner will be managed by execDiagCmds for each command
		var cancelRAGSpinner func()
		if !cfg.SafeMode {
			spinnerManager := lib.GetSpinnerManager(cfg)
			cancelRAGSpinner = spinnerManager.ShowRAG("🔍 " + config.Colors.Info.Sprint("Gathering") + " " + config.Colors.Dim.Sprint("diagnostic information..."))
			defer cancelRAGSpinner()
		}

		// Execute the diagnostic commands (no need for slices.Compact as cmds is already filtered)
		logger.Log("info", "Executing diagnostic command set: %d command(s)", len(cmds))
		userRes, err := exec.ExecDiagCmds(cfg, cmds)
		if err != nil {
			logger.Log("warn", "Error executing diagnostic commands: %v", err)
		}
		if len(userRes) > 0 {
			cmdResults = append(cmdResults, userRes...)
			logger.Log("info", "Collected %d diagnostic result(s)", len(userRes))
		}
	}

	// Check if we have valid results. In strict mode, absence of results is acceptable.
	if len(cmdResults) == 0 {
		if cfg.MCPClientEnabled && cfg.MCPStrict {
			return "", nil
		}
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
	// Build a set of baseline commands to exclude their raw outputs from the LLM context
	baselineSet := map[string]bool{}
	if cfg.EnableBaseline {
		for _, b := range diag.BaselineCommands(cfg) {
			key := strings.TrimSpace(b)
			if key != "" {
				baselineSet[key] = true
			}
		}
	}

	// Collect raw outputs for non-baseline commands only
	var commandSections []string
	for _, cmd := range cmdResults {
		c := strings.TrimSpace(cmd.Cmd)
		if c == "" || strings.TrimSpace(cmd.Out) == "" {
			continue
		}
		// Skip baseline raw outputs entirely; we'll rely on analyzer findings instead
		if baselineSet[c] {
			continue
		}

		// Filter sensitive data if enabled
		output := cmd.Out
		if !cfg.DisableSecretFilter {
			output = filter.SensitiveData(output)
		}

		// Include only successful outputs or timeouts; skip other errors
		var sb strings.Builder
		sb.WriteString("Command: ")
		sb.WriteString(cmd.Cmd)
		sb.WriteString("\n\nOutput:\n")
		if cmd.Err != nil {
			if strings.Contains(cmd.Err.Error(), "timed out") {
				sb.WriteString(output)
			} else {
				// Skip non-timeout errors
				continue
			}
		} else {
			sb.WriteString(output)
		}
		commandSections = append(commandSections, sb.String())
	}

	// Run light-weight analyzers over collected JSON outputs to produce high-signal findings
	// Extract relevant JSON blobs by command for analyzers
	var podsJSON, svcsJSON, epsJSON, esJSON, eventsJSON, nodesJSON, hpaJSON, readyz, livez string
	var depJSON, ingJSON, pvcJSON, pvJSON string
	for _, cmd := range cmdResults {
		c := strings.TrimSpace(cmd.Cmd)
		if strings.HasPrefix(c, "kubectl get pods ") {
			podsJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get services ") {
			svcsJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get deployments ") {
			depJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get endpointslices ") {
			esJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get endpoints ") {
			epsJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get events ") {
			eventsJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get ingress ") {
			ingJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get nodes ") {
			nodesJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get hpa ") {
			hpaJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get pvc ") {
			pvcJSON = cmd.Out
		} else if strings.HasPrefix(c, "kubectl get pv ") {
			pvJSON = cmd.Out
		} else if strings.Contains(c, "/readyz?verbose") {
			readyz = cmd.Out
		} else if strings.Contains(c, "/livez?verbose") {
			livez = cmd.Out
		}
	}

	findings := make([]diag.Finding, 0, 8)
	logger.Log("info", "Analyzer inputs: pods=%t svcs=%t eps=%t es=%t nodes=%t hpa=%t events=%t dep=%t ing=%t pvc=%t pv=%t readyz=%t livez=%t",
		podsJSON != "", svcsJSON != "", epsJSON != "", esJSON != "", nodesJSON != "", hpaJSON != "",
		eventsJSON != "", depJSON != "", ingJSON != "", pvcJSON != "", pvJSON != "", readyz != "", livez != "")
	if podsJSON != "" {
		findings = append(findings, diag.AnalyzePods(podsJSON)...)
	}
	if svcsJSON != "" || epsJSON != "" || esJSON != "" {
		findings = append(findings, diag.AnalyzeServices(svcsJSON, epsJSON, esJSON)...)
	}
	if depJSON != "" {
		findings = append(findings, diag.AnalyzeDeployments(depJSON)...)
	}
	if ingJSON != "" && svcsJSON != "" {
		findings = append(findings, diag.AnalyzeIngress(ingJSON, svcsJSON)...)
	}
	if nodesJSON != "" {
		findings = append(findings, diag.AnalyzeNodes(nodesJSON)...)
	}
	if hpaJSON != "" {
		findings = append(findings, diag.AnalyzeHPAs(hpaJSON)...)
	}
	if pvcJSON != "" && pvJSON != "" {
		findings = append(findings, diag.AnalyzePVCsPVs(pvcJSON, pvJSON)...)
	}
	findings = append(findings, diag.AnalyzeAPIServerHealth(readyz, "readyz")...)
	findings = append(findings, diag.AnalyzeAPIServerHealth(livez, "livez")...)

	if len(findings) > 0 {
		logger.Log("info", "Analyzers produced %d finding(s)", len(findings))
		// Log up to first 10 findings for debug visibility
		maxLog := 10
		if len(findings) < maxLog {
			maxLog = len(findings)
		}
		for i := 0; i < maxLog; i++ {
			f := findings[i]
			logger.Log("info", "Finding[%d]: kind=%s id=%s severity=%s summary=%s", i, f.Kind, f.ID, f.Severity, f.Summary)
		}

		// Sort findings by priority (highest first) when priority scoring is enabled
		if cfg.EnablePriorityScoring {
			sort.Slice(findings, func(i, j int) bool {
				// Primary sort: priority (descending - higher priority first)
				if findings[i].Priority != findings[j].Priority {
					return findings[i].Priority > findings[j].Priority
				}
				// Secondary sort: severity (error > warn > info)
				severityOrder := map[string]int{"error": 3, "warn": 2, "info": 1}
				return severityOrder[findings[i].Severity] > severityOrder[findings[j].Severity]
			})
			logger.Log("info", "Sorted findings by priority (highest first)")
		}

		// Filter out info-level findings to reduce context bloat
		// Only send actual issues (warn/error) to the LLM
		issuesOnly := make([]diag.Finding, 0, len(findings))
		for _, f := range findings {
			if f.Severity == "warn" || f.Severity == "error" {
				issuesOnly = append(issuesOnly, f)
			}
		}
		if len(issuesOnly) < len(findings) {
			logger.Log("info", "Filtered %d info-level findings, sending %d actual issues to LLM", len(findings)-len(issuesOnly), len(issuesOnly))
			findings = issuesOnly
		}
	} else {
		logger.Log("info", "Analyzers produced no findings")
	}

	// Skip event summaries to save context - they're redundant with analyzer findings
	// Events are already analyzed by other analyzers (pod failures, etc.)

	// Construct context data prioritizing analyzer findings and excluding baseline raw outputs
	var sections []string

	if len(findings) > 0 {
		var fb strings.Builder
		fb.WriteString("## Potential cluster issues found\n")
		fb.WriteString(diag.FormatFindings(findings))
		sections = append(sections, fb.String())
	}
	if len(commandSections) > 0 {
		// Tag the first command section with the common header
		commandSections[0] = "## Command Outputs\n\n" + commandSections[0]
		sections = append(sections, commandSections...)
	}
	contextData := strings.Join(sections, "\n\n---\n\n")

	// If the context is too large, use semantic trimming via embeddings
	if len(lib.Tokenize(contextData)) > int(float64(lib.EffectiveMaxTokens(cfg))/5) {
		sections := strings.Split(contextData, "\n\n---\n\n")
		ctx := context.Background()

		// Get embedder for semantic search
		logger.Log("info", "Init embedder for semantic search...")
		embedder, err := GetEmbedder(cfg)
		if err != nil {
			logger.Log("warn", "Error creating embedder: %v", err)
			// Improved fallback: trim all sections proportionally instead of just taking the first section
			trimmedSections := trimAllSectionsProportionally(sections, lib.EffectiveMaxTokens(cfg)/5)
			contextData = strings.Join(trimmedSections, "\n\n---\n\n")
		} else {
			// Use the embedder to semantically select the most relevant sections
			selected := TrimSectionsWithEmbeddings(ctx, embedder, sections, prompt, lib.EffectiveMaxTokens(cfg)/2)
			if len(selected) > 0 {
				contextData = strings.Join(selected, "\n\n---\n\n")
			} else {
				// Improved fallback: trim all sections proportionally instead of just taking the first section
				trimmedSections := trimAllSectionsProportionally(sections, lib.EffectiveMaxTokens(cfg)/3)
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
