package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ergochat/readline"
	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/completer"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/exec"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/llm"
	"github.com/mikhae1/kubectl-quackops/pkg/llm/metadata"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/mikhae1/kubectl-quackops/pkg/mcp"
	"github.com/mikhae1/kubectl-quackops/pkg/style"
	"github.com/mikhae1/kubectl-quackops/pkg/version"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func NewRootCmd(streams genericiooptions.IOStreams) *cobra.Command {
	cfg := config.LoadConfig()
	showEnv := false
	cmd := &cobra.Command{
		Use:          "kubectl-quackops",
		Short:        "QuackOps is a plugin for managing Kubernetes cluster using AI",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showEnv {
				printEnvVarsHelp()
				return nil
			}
			return runQuackOps(cfg, os.Args)(cmd, args)
		},
	}

	cmd.Flags().StringVarP(&cfg.Provider, "provider", "p", cfg.Provider, "LLM model provider (e.g., 'ollama', 'openai', 'azopenai', 'google', 'anthropic')")
	cmd.Flags().StringVarP(&cfg.Model, "model", "m", cfg.Model, "LLM model to use")
	cmd.Flags().StringVarP(&cfg.OllamaApiURL, "api-url", "u", cfg.OllamaApiURL, "URL for LLM API, used with 'ollama' provider")
	cmd.Flags().BoolVarP(&cfg.SafeMode, "safe-mode", "s", cfg.SafeMode, "Enable safe mode to prevent executing commands without confirmation")
	cmd.Flags().IntVarP(&cfg.Retries, "retries", "r", cfg.Retries, "Number of retries for kubectl commands")
	cmd.Flags().IntVarP(&cfg.Timeout, "timeout", "t", cfg.Timeout, "Timeout for kubectl commands in seconds")
	cmd.Flags().IntVarP(&cfg.UserMaxTokens, "max-tokens", "x", cfg.UserMaxTokens, "Maximum number of tokens in LLM context window (override; >0 disables auto-detect)")
	cmd.Flags().BoolVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose, "Enable verbose output")
	cmd.Flags().BoolVarP(&cfg.DisableSecretFilter, "disable-secrets-filter", "c", cfg.DisableSecretFilter, "Disable filtering sensitive data in secrets from being sent to LLMs")
	cmd.Flags().BoolVarP(&cfg.DisableMarkdownFormat, "disable-markdown", "d", cfg.DisableMarkdownFormat, "Disable Markdown formatting and colorization of LLM outputs (by default, responses are formatted with Markdown)")
	cmd.Flags().BoolVarP(&cfg.DisableAnimation, "disable-animation", "a", cfg.DisableAnimation, "Disable typewriter animation effect for LLM outputs")
	cmd.Flags().IntVarP(&cfg.MaxCompletions, "max-completions", "", cfg.MaxCompletions, "Maximum number of completions to display")
	cmd.Flags().BoolVarP(&cfg.DisableHistory, "disable-history", "", cfg.DisableHistory, "Disable storing prompt history in a file")
	cmd.Flags().StringVarP(&cfg.HistoryFile, "history-file", "", cfg.HistoryFile, "Path to the history file (default: ~/.quackops/history)")
	cmd.Flags().StringVarP(&cfg.KubectlBinaryPath, "kubectl-path", "k", cfg.KubectlBinaryPath, "Path to kubectl binary")
	// MCP flags
	cmd.Flags().BoolVarP(&cfg.MCPClientEnabled, "mcp-client", "", cfg.MCPClientEnabled, "Enable MCP client mode to use external MCP servers for tools")
	cmd.Flags().StringVarP(&cfg.MCPConfigPath, "mcp-config", "", cfg.MCPConfigPath, "Comma-separated MCP client config paths; tries each in order and falls back to ~/.config/quackops/mcp.yaml then ~/.quackops/mcp.json")
	cmd.Flags().IntVarP(&cfg.MCPToolTimeout, "mcp-tool-timeout", "", cfg.MCPToolTimeout, "Timeout in seconds for MCP tool calls")
	cmd.Flags().BoolVarP(&cfg.MCPStrict, "mcp-strict", "", cfg.MCPStrict, "Strict MCP mode: do not fall back to local execution when MCP fails")
	cmd.Flags().BoolVarP(&cfg.MCPLogEnabled, "mcp-log", "", cfg.MCPLogEnabled, "Enable logging of MCP server stdio to a file (env QU_MCP_LOG)")
	cmd.Flags().StringVarP(&cfg.MCPLogFile, "mcp-log-file", "", cfg.MCPLogFile, "MCP stdio log file path (overwritten at start)")
	cmd.Flags().StringVarP(&cfg.MCPLogFormat, "mcp-log-format", "", cfg.MCPLogFormat, "MCP log format: jsonl (default), text, or yaml (env QU_MCP_LOG_FORMAT)")
	// Diagnostics flags
	cmd.Flags().BoolVarP(&cfg.EnableBaseline, "enable-baseline", "", cfg.EnableBaseline, "Enable baseline diagnostic pack before LLM")
	cmd.Flags().IntVarP(&cfg.EventsWindowMinutes, "events-window-minutes", "", cfg.EventsWindowMinutes, "Events time window in minutes for summarization")
	cmd.Flags().BoolVarP(&cfg.EventsWarningsOnly, "events-warn-only", "", cfg.EventsWarningsOnly, "Include only Warning events in summaries")
	cmd.Flags().IntVarP(&cfg.LogsTail, "logs-tail", "", cfg.LogsTail, "Tail lines for log aggregation when triggered by playbooks")
	cmd.Flags().BoolVarP(&cfg.LogsAllContainers, "logs-all-containers", "", cfg.LogsAllContainers, "Aggregate logs from all containers when collecting logs")
	cmd.Flags().IntVarP(&cfg.ThrottleRequestsPerMinute, "throttle-rpm", "", cfg.ThrottleRequestsPerMinute, "Maximum number of LLM requests per minute")
	cmd.Flags().BoolVarP(&showEnv, "show-env", "", false, "Show information about environment variables used by the application")

	// Add env subcommand
	envCmd := &cobra.Command{
		Use:   "env",
		Short: "Show information about environment variables used by the application",
		Run: func(cmd *cobra.Command, args []string) {
			printEnvVarsHelp()
		},
	}
	cmd.AddCommand(envCmd)

	return cmd
}

// printEnvVarsHelp prints information about environment variables used by the application
func printEnvVarsHelp() {
	// Colors for readability - using style package
	titleStyle := style.Warning
	bodyStyle := style.Info

	fmt.Println()
	fmt.Println(titleStyle.Render("ENVIRONMENT VARIABLES:"))
	fmt.Println(bodyStyle.Render("See the Environment Variables section in README.md for the full list, defaults, and descriptions."))

	// Show currently set env vars as a convenience
	fmt.Println()
	fmt.Println(titleStyle.Render("CURRENTLY SET (detected in environment):"))
	var any bool
	for _, e := range os.Environ() {
		// Only show variables from this project's QU_* or provider API keys
		if strings.HasPrefix(e, "QU_") {
			fmt.Printf("  %s\n", e)
			any = true
		}
	}
	if !any {
		fmt.Println("  (none)")
	}
	fmt.Println()
}

// runQuackOps is the main function for the QuackOps command
func runQuackOps(cfg *config.Config, args []string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		logger.InitLoggers(os.Stderr, 0)

		// Apply auto-detection after CLI flags are parsed
		cfg.ConfigDetectMaxTokens()

		// Start MCP client mode if enabled
		if cfg.MCPClientEnabled {
			_ = mcp.Start(cfg)
			defer mcp.Stop()
		}

		if err := startChatSession(cfg, args); err != nil {
			fmt.Printf("Error processing commands: %v\n", err)
			return err
		}
		return nil
	}
}

// startChatSession runs the main chat session loop
func startChatSession(cfg *config.Config, args []string) error {
	cfg.StoredUserCmdResults = nil

	if len(args) > 0 {
		userPrompt := strings.TrimSpace(args[0])
		if userPrompt != "" {
			return processUserPrompt(cfg, userPrompt, "", 1)
		}
	}

	rlConfig := &readline.Config{
		Prompt:                 lib.FormatContextPrompt(cfg, false),
		EOFPrompt:              "exit",
		AutoComplete:           completer.NewShellAutoCompleter(cfg), // Use the new completer
		DisableAutoSaveHistory: true,                                 // manage history manually
	}

	// Set up history directory but don't set HistoryFile in readline config
	// We'll manage history manually to avoid conflicts
	if !cfg.DisableHistory && cfg.HistoryFile != "" {
		// Ensure the history file directory exists
		historyDir := filepath.Dir(cfg.HistoryFile)
		if err := os.MkdirAll(historyDir, 0755); err != nil {
			fmt.Printf("Warning: could not create history file directory: %v\n", err)
		}
		// Don't set rlConfig.HistoryFile - we manage it manually
	}

	var rl *readline.Instance

	// Helper function to switch to edit mode history
	switchToEditMode := func() {
		if cfg.DisableHistory || cfg.HistoryFile == "" || rl == nil {
			return
		}

		// Read main history file
		data, err := os.ReadFile(cfg.HistoryFile)
		if err != nil {
			return // main history doesn't exist yet
		}

		// Reset current history completely
		rl.ResetHistory()

		// Load only prefixed commands without prefixes
		lines := strings.Split(string(data), "\n")
		prefix := cfg.CommandPrefix
		if strings.TrimSpace(prefix) == "" {
			prefix = "!"
		}

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" && (strings.HasPrefix(line, prefix+" ") || strings.HasPrefix(line, prefix)) {
				// Remove prefix for display in edit mode
				clean := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				if clean != "" {
					_ = rl.SaveToHistory(clean)
				}
			}
		}
	}

	// Helper function to switch to normal mode history
	switchToNormalMode := func() {
		if cfg.DisableHistory || cfg.HistoryFile == "" || rl == nil {
			return
		}

		// Read main history file
		data, err := os.ReadFile(cfg.HistoryFile)
		if err != nil {
			return // main history doesn't exist yet
		}

		// Reset current history completely
		rl.ResetHistory()

		// Load all history entries
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				_ = rl.SaveToHistory(line)
			}
		}
	}
	// Capture ESC and ! for edit mode toggle
	rlConfig.FuncFilterInputRune = func(r rune) (rune, bool) {
		// Toggle edit mode with ! key press
		if r == '!' {
			cfg.EditMode = !cfg.EditMode
			if rl != nil {
				if cfg.EditMode {
					// Switch to edit mode: load filtered history
					rl.SetPrompt(lib.FormatEditPromptWith(cfg))
					switchToEditMode()
				} else {
					// Switch to normal mode: load full history
					rl.SetPrompt(lib.FormatContextPrompt(cfg, false))
					switchToNormalMode()
				}
				rl.Refresh()
			}
			return 0, false // swallow prefix
		}

		// Exit edit mode with ESC
		if r == readline.CharEsc && cfg.EditMode {
			cfg.EditMode = false
			if rl != nil {
				rl.SetPrompt(lib.FormatContextPrompt(cfg, false))
				switchToNormalMode()
				rl.Refresh()
			}
			return 0, false // swallow ESC
		}

		// Toggle detailed output of last command with Ctrl-R (ASCII 18)
		if r == 18 { // Ctrl-R
			if rl != nil && len(cfg.SessionHistory) > 0 {
				lastEvent := cfg.SessionHistory[len(cfg.SessionHistory)-1]

				// Clear screen
				lib.CoolClearEffect(cfg)

				// Re-render the session event in verbose mode
				fmt.Print(mcp.RenderSessionEvent(lastEvent, true, cfg))

				// Refresh prompt
				rl.Refresh()
			}
			return 0, false // swallow Ctrl-R
		}

		return r, true
	}

	// Avoid recomputing prompt on every keystroke to prevent latency
	rlConfig.Listener = func(line []rune, pos int, key rune) ([]rune, int, bool) {
		// Backup ESC handling
		if cfg.EditMode && key == readline.CharEsc {
			cfg.EditMode = false
			rl.SetPrompt(lib.FormatContextPrompt(cfg, false))
			rl.Refresh()
			return line, pos, true
		}

		// Update prompt on ENTER or INTERRUPT only
		if key == readline.CharEnter || key == readline.CharInterrupt {
			if cfg.EditMode {
				rl.SetPrompt(lib.FormatEditPromptWith(cfg))
			} else {
				rl.SetPrompt(lib.FormatContextPrompt(cfg, false))
			}
			rl.Refresh()
		}

		return line, pos, false
	}

	rl, err := readline.NewFromConfig(rlConfig)
	if err != nil {
		return fmt.Errorf("failed to create interactive prompt instance: %w", err)
	}

	// Initialize history on startup - load in normal mode
	switchToNormalMode()

	cleanupAndExit := func(message string, exitCode int) {
		cleanupFunc := func() {
			if rl != nil {
				rl.Close()
			}
		}
		lib.CleanupAndExit(cfg, lib.CleanupOptions{Message: message, ExitCode: exitCode, CleanupFunc: cleanupFunc})
	}

	defer cleanupAndExit("", -1) // just cleanup

	printWelcomeBanner(cfg)
	printWelcomeBanner(cfg)
	if cfg.MCPClientEnabled {
		info := style.Info
		dim := style.Debug
		accent := style.Success
		servers := mcp.Servers(cfg)
		tools := mcp.Tools(cfg)
		srvStr := "none"
		if len(servers) > 0 {
			srvStr = strings.Join(servers, ", ")
		}
		if srvStr != "none" {
			srvStr = accent.Render(srvStr)
		}
		line := fmt.Sprintf("on · servers:[%s] · tools:%d · strict:%t", srvStr, len(tools), cfg.MCPStrict)
		fmt.Println(dim.Render("MCP:") + " " + info.Render(line))
	}

	// Chat loop
	// Track the last displayed token counters in the prompt so we can animate to new values
	lastDisplayedOutgoingTokens := cfg.LastOutgoingTokens
	lastDisplayedIncomingTokens := cfg.LastIncomingTokens

	// Allow cancelling any in-progress prompt counter animation before starting a new one
	var promptAnimStop chan struct{}
	var lastTextPrompt string
	var userMsgCount int
	for {
		userPrompt, err := rl.ReadLine()
		if err != nil { // io.EOF is returned on Ctrl-C
			cleanupAndExit("Exiting...", 0)
			return nil // Ensure we exit immediately
		}

		userPrompt = strings.TrimSpace(userPrompt)
		if userPrompt == "" {
			continue
		}

		// Centralized slash command handling
		if strings.HasPrefix(strings.ToLower(userPrompt), "/") {
			handled, action := handleSlashCommand(cfg, userPrompt)
			if handled {
				// Apply additional UI state for clear/reset
				if action == "clear" {
					lastDisplayedOutgoingTokens = 0
					lastDisplayedIncomingTokens = 0
					lastTextPrompt = ""
					userMsgCount = 0
					lib.CoolClearEffect(cfg)
					rl.SetPrompt(lib.FormatContextPrompt(cfg, false))
					rl.Refresh()
				}
				continue
			}
		}

		switch strings.ToLower(userPrompt) {
		case "bye", "exit", "quit", "/bye", "/exit", "/quit", "/q":
			cleanupAndExit("🦆...quack!", 0)
		}

		// Do not save raw input here. History saving is handled after processing to apply rules.

		if !strings.HasPrefix(userPrompt, cfg.CommandPrefix) {
			lastTextPrompt = userPrompt
			userMsgCount++
		}

		logger.Log("info", "Processing prompt (editMode=%t, safeMode=%t, baseline=%t)", cfg.EditMode, cfg.SafeMode, cfg.EnableBaseline)
		// Remember input characteristics and original text
		wasEditMode := cfg.EditMode
		originalUserPrompt := userPrompt
		prefix := cfg.CommandPrefix
		if strings.TrimSpace(prefix) == "" {
			prefix = "!"
		}
		wasPrefixed := strings.HasPrefix(originalUserPrompt, prefix)
		wasCommand := wasEditMode || wasPrefixed

		err = processUserPrompt(cfg, userPrompt, lastTextPrompt, userMsgCount)
		if err != nil {
			return err
		}

		// Unified history saving: store all prompts and commands with prefixes in main history file
		// Also save MCP prompts with queries (e.g., "/code-mode check issues")
		isMCPPromptQuery := false
		if cfg.MCPClientEnabled && strings.HasPrefix(originalUserPrompt, "/") {
			_, query, isPrompt := completer.IsMCPPrompt(cfg, originalUserPrompt)
			isMCPPromptQuery = isPrompt && query != ""
		}
		if (!strings.HasPrefix(originalUserPrompt, "/") || isMCPPromptQuery) && !cfg.DisableHistory && cfg.HistoryFile != "" {
			var entryToSave string

			if wasCommand {
				// For commands, check if successful before saving
				success := false
				if len(cfg.StoredUserCmdResults) > 0 {
					last := cfg.StoredUserCmdResults[len(cfg.StoredUserCmdResults)-1]
					success = (last.Err == nil) && (strings.TrimSpace(last.Cmd) != "")
				}
				if success {
					// Save command with prefix to main history file
					entryToSave = originalUserPrompt
					if !strings.HasPrefix(entryToSave, prefix) {
						entryToSave = prefix + " " + entryToSave
					}
				}
			} else {
				// Save non-command prompts as-is
				entryToSave = originalUserPrompt
			}

			if entryToSave != "" {
				// Only save to main history file - don't save to readline session to avoid duplicates
				f, err := os.OpenFile(cfg.HistoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err == nil {
					_, _ = f.WriteString(entryToSave + "\n")
					_ = f.Close()

					// Reload the appropriate history to include the new entry
					// Note: If we're in edit mode, we still reload edit mode to show the new command
					// When user switches to normal mode later, they'll see the prefixed version
					if cfg.EditMode {
						switchToEditMode()
					} else {
						switchToNormalMode()
					}
				}
			}
		}

		// Update prompt after processing
		if cfg.EditMode {
			// In edit mode, disable token-counter animation and show simple edit prompt
			if promptAnimStop != nil {
				close(promptAnimStop)
				// Prevent double-close on subsequent iterations
				promptAnimStop = nil
			}
			rl.SetPrompt(lib.FormatEditPromptWith(cfg))
			rl.Refresh()
		} else {
			// Animate the prompt counter to the latest token values
			if promptAnimStop != nil {
				close(promptAnimStop)
				// Prevent double-close on subsequent iterations
				promptAnimStop = nil
			}
			promptAnimStop = make(chan struct{})
			animatePromptCounter(rl, cfg, lastDisplayedOutgoingTokens, lastDisplayedIncomingTokens, false, promptAnimStop)
			// Update last-displayed snapshot to the current targets
			lastDisplayedOutgoingTokens = cfg.LastOutgoingTokens
			lastDisplayedIncomingTokens = cfg.LastIncomingTokens
		}
	}
}

// Centralized slash command handler
// Returns (handled, action, promptName, userQuery) where:
//   - handled: true if this was a slash command that was fully processed
//   - action: the action type (e.g., "help", "clear", "prompt_query")
//   - For MCP prompts with queries, returns (false, "prompt_query", promptName, userQuery)
//     to indicate the caller should process as an MCP prompt injection
func handleSlashCommand(cfg *config.Config, userPrompt string) (bool, string) {
	lowered := strings.ToLower(strings.TrimSpace(userPrompt))
	if !strings.HasPrefix(lowered, "/") {
		return false, ""
	}

	body := style.Info
	accent := config.Colors.Accent
	dim := config.Colors.Dim
	info := config.Colors.Info
	warn := config.Colors.Warn

	switch lowered {
	case "/help", "/h", "/?":
		printInlineHelp(cfg)
		return true, "help"
	case "/version":
		fmt.Println(info.Render(version.Version))
		return true, "version"
	case "/reset":
		cfg.ChatMessages = nil
		cfg.StoredUserCmdResults = nil
		cfg.SelectedPrompt = ""
		fmt.Println(body.Render("Context reset"))
		return true, "reset"
	case "/clear":
		cfg.ChatMessages = nil
		cfg.StoredUserCmdResults = nil
		cfg.LastOutgoingTokens = 0
		cfg.LastIncomingTokens = 0
		cfg.SelectedPrompt = ""
		fmt.Println(accent.Render("🦆 Context cleared!"))
		return true, "clear"
	case "/mcp":
		if cfg.MCPClientEnabled {
			printMCPDetails(cfg)
		} else {
			fmt.Println(dim.Render("MCP client: ") + warn.Render("disabled"))
		}
		return true, "mcp"
	case "/model", "/models":
		// Show current model if no arguments, or launch interactive selector
		prov := strings.ToUpper(strings.TrimSpace(cfg.Provider))
		if prov == "" {
			prov = "DEFAULT"
		}
		m := strings.TrimSpace(cfg.Model)
		if m == "" {
			m = "auto"
		}

		// Check if there are any additional arguments (for future extension)
		// For now, always launch the interactive selector
		fmt.Printf("%s\n", body.Render(fmt.Sprintf("Current: %s/%s", prov, m)))
		fmt.Println(dim.Render("Launching interactive model selector..."))

		// Create model selector and launch interactive selection
		selector := lib.NewModelSelector(cfg)
		selectedModel, err := selector.SelectModel()
		if err != nil {
			if strings.Contains(err.Error(), "cancelled") {
				fmt.Println(dim.Render("Model selection cancelled."))
			} else {
				fmt.Printf("%s %v\n", warn.Render("Error selecting model:"), err)
			}
			return true, "model"
		}

		// Update configuration with selected model
		cfg.Model = selectedModel
		fmt.Printf("%s %s\n", body.Render("Model updated to:"), config.Colors.Model.Render(selectedModel))

		// Auto-detect max tokens for the new model
		cfg.ConfigDetectMaxTokens()

		return true, "model"
	case "/servers":
		if cfg.MCPClientEnabled {
			list := mcp.Servers(cfg)
			if len(list) == 0 {
				fmt.Println(dim.Render("No MCP servers configured"))
			} else {
				fmt.Println(accent.Render("MCP servers:"))
				for _, s := range list {
					fmt.Printf(" - %s\n", info.Render(s))
				}
			}
		} else {
			fmt.Println(dim.Render("MCP client: ") + warn.Render("disabled"))
		}
		return true, "servers"
	case "/tools":
		if cfg.MCPClientEnabled {
			toolInfos := mcp.GetToolInfos(cfg)
			if len(toolInfos) == 0 {
				fmt.Println(dim.Render("No MCP tools discovered"))
			} else {
				fmt.Printf("%s\n", accent.Render(fmt.Sprintf("MCP tools (%d):", len(toolInfos))))
				for _, tool := range toolInfos {
					// Truncate description if too long
					desc := tool.Description
					maxLen := 320
					if len(desc) > maxLen {
						desc = desc[:maxLen] + "..."
					}
					fmt.Printf(" - %s: %s\n", accent.Render(tool.Name), body.Render(desc))
				}
			}
		} else {
			fmt.Println(dim.Render("MCP client: ") + warn.Render("disabled"))
		}
		return true, "tools"
	case "/prompts":
		if cfg.MCPClientEnabled {
			printMCPPrompts(cfg)
		} else {
			fmt.Println(dim.Render("MCP client: ") + warn.Render("disabled"))
		}
		return true, "prompts"
	case "/history":

		if len(cfg.SessionHistory) == 0 {
			fmt.Println(dim.Render("No history available for this session."))
		} else {
			fmt.Println(accent.Render("Session History:"))
			for _, event := range cfg.SessionHistory {
				fmt.Print(mcp.RenderSessionEvent(event, true, cfg))
				fmt.Println(dim.Render(strings.Repeat("-", 40)))
			}
		}
		return true, "history"
	default:
		// Check for MCP prompt with user query (e.g., "/code-mode check issues")
		if cfg.MCPClientEnabled && strings.HasPrefix(lowered, "/") {
			promptName, userQuery, isPrompt := completer.IsMCPPrompt(cfg, userPrompt)
			if isPrompt {
				if userQuery != "" {
					// This is a prompt with a query - don't handle it here,
					// let processUserPrompt handle the injection
					cfg.SelectedPrompt = promptName
					return false, "prompt_query"
				}
				// Just the prompt name, show details
				if handled := handleMCPDynamicPrompt(cfg, lowered); handled {
					return true, "prompt"
				}
			}
		}
		fmt.Printf("%s %s\n", warn.Render("Unknown command:"), body.Render(userPrompt))
		fmt.Println(body.Render("Type /help for available commands."))
		return true, "unknown"
	}
}

// printWelcomeBanner renders a styled welcome message inspired by modern AI CLIs
// with a branded header, active configuration summary, and quick tips.
func printWelcomeBanner(cfg *config.Config) {
	// Helpers for formatting
	ansiRe := regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")
	stripANSI := func(s string) string { return ansiRe.ReplaceAllString(s, "") }
	visibleWidth := func(s string) int { return len([]rune(stripANSI(s))) }
	padRight := func(s string, width int) string {
		diff := width - visibleWidth(s)
		if diff > 0 {
			return s + strings.Repeat(" ", diff)
		}
		return s
	}

	// Rainbow sequence reserved (not used in mono mode)
	// We keep this structure but populate with lipgloss colors if needed for rainbow mode
	// defaults are just placeholders here as we use mono palette below
	rainbow := []lipgloss.Style{
		lipgloss.NewStyle().Foreground(style.ColorRed),
		lipgloss.NewStyle().Foreground(style.ColorYellow),
		lipgloss.NewStyle().Foreground(style.ColorGreen),
		lipgloss.NewStyle().Foreground(style.ColorCyan),
		lipgloss.NewStyle().Foreground(style.ColorBlue),
		lipgloss.NewStyle().Foreground(style.ColorPink),
	}
	_ = rainbow

	// Duck ASCII art disabled: keep left column empty to left-align banner text
	leftLines := []string{}
	// Compute left column width for alignment using uncolored version
	maxLeft := 0
	if len(leftLines) > 0 {
		// Compute max width with ANSI stripped
		for _, ln := range leftLines {
			if w := visibleWidth(ln); w > maxLeft {
				maxLeft = w
			}
		}
	}

	// Build hero banner (right column)
	// brand coloring not used for mono gradient mode
	// brand := color.New(color.FgHiYellow, color.Bold)
	dim := config.Colors.Dim
	shadow := config.Colors.Shadow
	info := config.Colors.Info
	ok := config.Colors.Ok
	warn := config.Colors.Warn
	magenta := config.Colors.Model
	accent := config.Colors.Accent

	// (plain title kept for reference but not used)

	provider := strings.ToUpper(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "DEFAULT"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "auto"
	}
	// Plain strings are used for computing rainbow offsets; we will use the live cfg values directly
	apiPlain := ""
	if cfg.OllamaApiURL != "" {
		apiPlain = fmt.Sprintf("LLM API: %s", cfg.OllamaApiURL)
	}
	// Safe/history plain strings are not needed directly here

	// QUACKOPS ASCII art (right column)
	quackopsArt := []string{
		" ██████╗██╗   ██╗█████╗ ████████╗  ██╗██████╗██████╗███████╗    ",
		"██╔═══████║   ████╔══████╔════██║ ██╔██╔═══████╔══████╔════╝    ",
		"██║   ████║   ███████████║    █████╔╝██║   ████████╔███████╗    ",
		"██║▄▄ ████║   ████╔══████║    ██╔═██╗██║   ████╔═══╝╚════██║    ",
		"╚██████╔╚██████╔██║  ██╚████████║  ██╚██████╔██║    ███████║    ",
		" ╚══▀▀═╝ ╚═════╝╚═╝  ╚═╝╚═════╚═╝  ╚═╝╚═════╝╚═╝    ╚══════╝    ",
	}

	// Single-palette gradient (cyan family) for ASCII art (duck + QUACKOPS) & horizontal line
	monoPalette := config.Colors.Gradient
	gradientizeMono := func(text string, start int) string {
		if text == "" {
			return text
		}
		var b strings.Builder
		idx := start
		for _, r := range text {
			c := monoPalette[idx%len(monoPalette)]
			b.WriteString(c.Render(string(r)))
			idx++
		}
		return b.String()
	}

	// Checkerboard (chess) pattern colorizer for hero text
	// Corner/line characters like ╔ ╗ ╚ ╝ are rendered with a dim shadow color
	// and excluded from the chess alternation (do not advance the chess column).
	chessColorize := func(text string, row int) string {
		if text == "" {
			return text
		}

		isShadowRune := func(r rune) bool {
			switch r {
			case '\u2554', '\u2557', '\u255A', '\u255D', '═', '║':
				return true
			default:
				return false
			}
		}

		var b strings.Builder
		col := 0
		for _, r := range text {
			if isShadowRune(r) {
				b.WriteString(shadow.Render(string(r)))
				// do not advance col; excluded from chess pattern
				continue
			}
			c := monoPalette[(row+col)%len(monoPalette)]
			b.WriteString(c.Render(string(r)))
			col++
		}
		return b.String()
	}

	// Side-by-side render: left duck, right QUACKOPS art
	gap := "   "
	lines := len(leftLines)
	if len(quackopsArt) > lines {
		lines = len(quackopsArt)
	}
	// Determine right width for later horizontal line
	maxRight := 0
	for _, ln := range quackopsArt {
		if w := visibleWidth(ln); w > maxRight {
			maxRight = w
		}
	}
	for i := 0; i < lines; i++ {
		left := ""
		if i < len(leftLines) {
			left = padRight(leftLines[i], maxLeft)
		} else if maxLeft > 0 {
			left = strings.Repeat(" ", maxLeft)
		}
		right := ""
		if i < len(quackopsArt) {
			// Apply chess (checkerboard) pattern instead of stripes
			right = chessColorize(quackopsArt[i], i)
		}
		if maxLeft > 0 {
			// Colorize duck (left) with mono palette for consistency
			fmt.Println(gradientizeMono(left, 0) + gap + right)
		} else {
			fmt.Println(right)
		}
	}

	// Gather Kubernetes context details and render directly under banner
	ctxName, _, ctxErr := lib.GetKubeContextInfo(cfg)
	indent := ""
	if maxLeft > 0 {
		indent = strings.Repeat(" ", maxLeft) + gap
	}
	if ctxErr != nil {
		fmt.Println(indent + dim.Render("Using Kubernetes context:") + " " + warn.Render(fmt.Sprintf("unavailable (%v)", ctxErr)))
	} else if ctxName != "" {
		fmt.Println(indent + dim.Render("Using Kubernetes context:") + " " + info.Render(fmt.Sprintf("%s", ctxName)))
	}

	// Horizontal gradient line under the ASCII art (aligned under right column)
	if maxRight > 0 {
		line := strings.Repeat("─", maxRight)
		colored := gradientizeMono(line, 0)
		fmt.Println(indent + colored)
	}

	// Non-rainbow info lines (useful details) - compact one-line with tokens
	llmStyled := dim.Render("LLM:") + " " + accent.Render(fmt.Sprintf("%s", provider)) + dim.Render(" · ") + magenta.Render(fmt.Sprintf("%s", model))

	// Fancy tokens/budget line showing max and reservations
	effective := lib.EffectiveMaxTokens(cfg)
	limit := effective
	if limit <= 0 {
		limit = 4096
	}
	inputReserve := int(float64(limit) * float64(cfg.InputTokenReservePercent) / 100.0)
	if inputReserve < cfg.MinInputTokenReserve {
		inputReserve = cfg.MinInputTokenReserve
	}
	mcpReserve := 0
	if cfg.MCPClientEnabled {
		mcpReserve = len(mcp.Tools(cfg))*200 + cfg.MCPMaxToolCalls*1000
	}
	totalReserve := inputReserve + mcpReserve
	outBudget := limit - totalReserve
	if outBudget < cfg.MinOutputTokens {
		outBudget = cfg.MinOutputTokens
	}
	// Build colored line: "LLM: PROVIDER · model · max 32.8k ↑ in 6.6k ↓ out 26.2k"
	tokens_info := llmStyled +
		dim.Render(" · ") +
		dim.Render("max ") + ok.Render(lib.FormatCompactNumber(limit)) +
		dim.Render("/") + accent.Render("↑") + info.Render(lib.FormatCompactNumber(inputReserve)) +
		dim.Render("/") + accent.Render("↓") + info.Render(lib.FormatCompactNumber(outBudget))
	fmt.Println(indent + tokens_info)
	if apiPlain != "" {
		fmt.Println(indent + dim.Render("API:") + " " + info.Render(fmt.Sprintf("%s", cfg.OllamaApiURL)))
	}
	fmt.Println(indent + dim.Render("Safe mode:") + " " + func() string {
		if cfg.SafeMode {
			return ok.Render("enabled")
		}
		return warn.Render("disabled")
	}() + " " + dim.Render("· History:") + " " + func() string {
		if cfg.DisableHistory {
			return warn.Render("disabled")
		}
		return ok.Render("enabled") + info.Render(fmt.Sprintf(" (%s)", cfg.HistoryFile))
	}())

	// Tips for getting started
	fmt.Println()
	fmt.Println(indent + accent.Render("Getting started:"))
	fmt.Println(indent + info.Render("- ") + dim.Render("Ask questions:") + " " + info.Render("find pod issues in nginx namespace"))
	fmt.Println(indent + info.Render("- ") + dim.Render("Run commands:") + " " + info.Render(cfg.CommandPrefix+" kubectl get events -A"))
	fmt.Println(indent + info.Render("- ") + dim.Render("Type: ") + ok.Render("/help") + " " + info.Render("for more information"))
	fmt.Println()
}

// animatePromptCounter gradually updates the prompt token counters from previous values
// to the current cfg.LastOutgoingTokens/LastIncomingTokens, similar to a spinner animation.
// It runs asynchronously and can be cancelled via stopCh.
func animatePromptCounter(rl *readline.Instance, cfg *config.Config, fromOutgoing int, fromIncoming int, isCommand bool, stopCh chan struct{}) {
	targetOutgoing := cfg.LastOutgoingTokens
	targetIncoming := cfg.LastIncomingTokens

	// Only animate increases; for decreases or no change, update once
	if targetOutgoing <= fromOutgoing && targetIncoming <= fromIncoming {
		rl.SetPrompt(lib.FormatContextPrompt(cfg, isCommand))
		rl.Refresh()
		return
	}

	// Derive animation timing from spinner timeout so users can tune via env/flags
	baseMs := cfg.SpinnerTimeout
	if baseMs <= 0 {
		baseMs = 300
	}
	// Target overall duration at ~2x spinner tick
	totalDuration := time.Duration(baseMs) * time.Millisecond * 2
	steps := 24 // keep smoothness constant; tick scales with spinner timeout
	if steps < 1 {
		steps = 1
	}
	tick := totalDuration / time.Duration(steps)

	outgoingDelta := targetOutgoing - fromOutgoing
	incomingDelta := targetIncoming - fromIncoming
	if outgoingDelta < 0 {
		outgoingDelta = 0
	}
	if incomingDelta < 0 {
		incomingDelta = 0
	}

	go func() {
		cancelled := false
		defer func() {
			if cancelled {
				return
			}
			// Ensure final values are shown
			cfg.LastOutgoingTokens = targetOutgoing
			cfg.LastIncomingTokens = targetIncoming
			rl.SetPrompt(lib.FormatContextPrompt(cfg, isCommand))
			rl.Refresh()
		}()

		for i := 1; i <= steps; i++ {
			select {
			case <-stopCh:
				cancelled = true
				return
			default:
			}

			curOutgoing := fromOutgoing + (outgoingDelta*i)/steps
			curIncoming := fromIncoming + (incomingDelta*i)/steps
			cfg.LastOutgoingTokens = curOutgoing
			cfg.LastIncomingTokens = curIncoming
			rl.SetPrompt(lib.FormatContextPrompt(cfg, isCommand))
			rl.Refresh()
			time.Sleep(tick)
		}
	}()
}

func processUserPrompt(cfg *config.Config, userPrompt string, lastTextPrompt string, userMsgCount int) error {
	var augPrompt string
	var err error

	// Centralized slash command handling
	handled, action := handleSlashCommand(cfg, userPrompt)
	if handled {
		return nil
	}

	// Highlight MCP prompt even when embedded mid-line
	var inlinePromptName, inlineServerName, inlineQuery string
	var inlineFound bool
	if action != "prompt_query" {
		inlinePromptName, inlineServerName, inlineQuery, inlineFound = completer.FindMCPPromptWithQuery(cfg, userPrompt)
		if inlineFound {
			fmt.Println(lib.FormatInputWithPrompt(userPrompt, inlinePromptName, inlineServerName))
		}
	}

	// Check if this is an MCP prompt with a user query
	var mcpPromptContent string
	var actualUserQuery string
	var promptServer string
	var hasPrompt bool
	if action == "prompt_query" && cfg.SelectedPrompt != "" {
		// Extract the user query part (after the prompt name)
		_, actualUserQuery, _ = completer.IsMCPPrompt(cfg, userPrompt)
		promptServer = completer.GetMCPPromptServer(cfg, userPrompt)
		hasPrompt = true

		logger.Log("debug", "[MCP Prompt] Detected prompt: '%s' from server '%s'", cfg.SelectedPrompt, promptServer)
		logger.Log("debug", "[MCP Prompt] User query: '%s'", actualUserQuery)

		// Display with yellow background highlighting for the prompt part (/$server/$prompt)
		fmt.Println(lib.FormatInputWithPrompt(userPrompt, cfg.SelectedPrompt, promptServer))
	} else if inlineFound && strings.TrimSpace(inlineQuery) != "" {
		cfg.SelectedPrompt = inlinePromptName
		actualUserQuery = strings.TrimSpace(inlineQuery)
		promptServer = inlineServerName
		hasPrompt = true

		logger.Log("debug", "[MCP Prompt] Detected inline prompt: '%s' from server '%s'", cfg.SelectedPrompt, promptServer)
		logger.Log("debug", "[MCP Prompt] User query: '%s'", actualUserQuery)
	}

	if hasPrompt && cfg.SelectedPrompt != "" {
		// Set the prompt server for tool filtering during LLM chat
		cfg.MCPPromptServer = promptServer
		logger.Log("debug", "[MCP Prompt] Set MCPPromptServer to '%s' for tool filtering", promptServer)

		// Check if prompt has arguments and log them
		promptArgs := mcp.GetPromptArgs(cfg, cfg.SelectedPrompt)
		if len(promptArgs) > 0 {
			logger.Log("debug", "[MCP Prompt] Prompt '%s' defines %d arguments:", cfg.SelectedPrompt, len(promptArgs))
			for _, arg := range promptArgs {
				reqStr := "optional"
				if arg.Required {
					reqStr = "required"
				}
				logger.Log("debug", "[MCP Prompt]   - %s (%s): %s", arg.Name, reqStr, arg.Description)
			}
		} else {
			logger.Log("debug", "[MCP Prompt] Prompt '%s' has no arguments defined", cfg.SelectedPrompt)
		}

		// Fetch the prompt content from MCP server
		// Pass the user query so it can be injected into prompt arguments per MCP spec
		logger.Log("debug", "[MCP Prompt] Calling GetPrompt for '%s' with userQuery='%s'", cfg.SelectedPrompt, actualUserQuery)
		promptMessages, err := mcp.GetPromptContent(cfg, cfg.SelectedPrompt, nil, actualUserQuery)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", color.YellowString("Warning: Failed to fetch prompt content: %v", err))
			logger.Log("debug", "[MCP Prompt] Error fetching prompt: %v", err)
		} else {
			// Build prompt content from MCP messages
			var promptBuilder strings.Builder
			logger.Log("debug", "[MCP Prompt] Received %d messages from prompt", len(promptMessages))
			for i, msg := range promptMessages {
				logger.Log("debug", "[MCP Prompt]   Message %d: role=%s, content_len=%d", i+1, msg.Role, len(msg.Content))
				if msg.Content != "" {
					promptBuilder.WriteString(msg.Content)
					promptBuilder.WriteString("\n")
				}
			}
			mcpPromptContent = strings.TrimSpace(promptBuilder.String())
			logger.Log("debug", "[MCP Prompt] Final prompt content length: %d chars", len(mcpPromptContent))
			logger.Log("info", "Injected MCP prompt '%s' content (%d chars)", cfg.SelectedPrompt, len(mcpPromptContent))
		}

		// Use the actual user query for processing
		userPrompt = actualUserQuery
		cfg.SelectedPrompt = "" // Clear after use
	}

	// Edit mode: treat input as command without requiring prefix
	if cfg.EditMode || strings.HasPrefix(userPrompt, cfg.CommandPrefix) {
		effectiveCmd := userPrompt
		if cfg.EditMode && !strings.HasPrefix(userPrompt, cfg.CommandPrefix) {
			// In edit mode, normalize to "<prefix> <cmd>" so it is stored with prefix in history
			effectiveCmd = cfg.CommandPrefix + " " + userPrompt
		}
		// Execute the command and store the result; do not run LLM
		cmdResults, err := exec.ExecDiagCmds(cfg, []string{effectiveCmd})
		if err != nil {
			fmt.Println(color.HiRedString(err.Error()))
		}
		if len(cmdResults) > 0 {
			cfg.StoredUserCmdResults = append(cfg.StoredUserCmdResults, cmdResults...)
		}
		return nil
	}

	// Non-command user prompts
	if userMsgCount%2 == 1 || lib.EffectiveMaxTokens(cfg) > 16000 {
		if len(cfg.StoredUserCmdResults) > 0 {
			// Use stored command results instead of running diagnostic commands
			augPrompt, err = llm.CreateAugPromptFromCmdResults(cfg, userPrompt, cfg.StoredUserCmdResults)
			// Clear stored results after using them
			cfg.StoredUserCmdResults = nil
		} else {
			// No stored commands, retrieve diagnostic commands as before
			augPrompt, err = llm.RetrieveRAG(cfg, userPrompt, lastTextPrompt, userMsgCount)
		}

		if err != nil {
			if lib.IsUserCancel(err) {
				fmt.Fprintln(os.Stderr, color.YellowString("(cancelled)"))
				return nil
			}
			logger.Log("err", "Error retrieving RAG: %v", err)
		}
	}

	if augPrompt == "" {
		augPrompt = userPrompt
	}

	// Build role-separated prompts using MessageBuilder
	mb := llm.NewMessageBuilder()

	// System prompt: MCP prompt content + MCP tools instructions
	if mcpPromptContent != "" {
		mb.SetMCPPrompt(mcpPromptContent)
	}

	if cfg.MCPClientEnabled {
		mcpInstructions := buildMCPSystemInstructions(cfg)
		if mcpInstructions != "" {
			mb.AddSystemInstruction(mcpInstructions)
		}
		logger.Log("info", "Added MCP tool instructions to system prompt")
	}

	// User prompt: RAG context + user query
	mb.SetContextData(augPrompt)
	mb.LogRoleSummary(cfg)

	// Print a minimal verbose trace for tests when verbose is enabled
	if cfg.Verbose {
		fmt.Fprintln(os.Stderr, "Processing prompt (verbose mode)")
	}

	systemContent, userContent := mb.Build(cfg)
	_, err = llm.RequestWithSystem(cfg, systemContent, userContent, true, true)
	if err != nil {
		// Check if this is a 429 rate limit error - don't exit interactive mode for these
		if lib.Is429Error(err) {
			logger.Log("info", "Rate limit error in interactive mode - continuing chat session")
			// The error details have already been displayed by the Chat function
			return nil
		}
		// User pressed ESC: keep interactive session alive and skip retries (handled upstream)
		if lib.IsUserCancel(err) {
			fmt.Fprintln(os.Stderr, color.YellowString("(cancelled)"))
			return nil
		}
		return fmt.Errorf("error requesting LLM: %w", err)
	}

	llm.ManageChatThreadContext(cfg, cfg.ChatMessages, lib.EffectiveMaxTokens(cfg))

	// Clear prompt server filter after LLM request completes
	cfg.MCPPromptServer = ""

	return nil
}

// printMCPDetails prints detailed information about the MCP configuration
func printMCPDetails(cfg *config.Config) {
	titleStyle := style.Title
	info := config.Colors.Info
	dim := config.Colors.Dim
	warn := config.Colors.Warn
	serverStyle := config.Colors.Command
	toolStyle := config.Colors.Label

	fmt.Println()
	fmt.Println(titleStyle.Render("MCP CONFIGURATION:"))

	// Show servers
	servers := mcp.Servers(cfg)
	if len(servers) == 0 {
		fmt.Println("  " + dim.Render("Servers:") + " " + warn.Render("None configured"))
	} else {
		fmt.Println("  " + dim.Render("Servers:") + " " + info.Render(fmt.Sprintf("%d active", len(servers))))
		for _, s := range servers {
			fmt.Printf("    - %s\n", serverStyle.Render(s))
		}
	}
	fmt.Println("  " + dim.Render("Strict mode:") + " " + info.Render(fmt.Sprintf("%t", cfg.MCPStrict)))

	// Show tools with descriptions
	toolInfos := mcp.GetToolInfos(cfg)
	if len(toolInfos) == 0 {
		fmt.Println("  " + dim.Render("Tools:") + " " + warn.Render("None discovered"))
	} else {
		fmt.Println("  " + dim.Render("Tools:") + " " + info.Render(fmt.Sprintf("%d available", len(toolInfos))))
	}
	if cfg.Verbose && len(toolInfos) > 0 {
		fmt.Println()
		fmt.Println(dim.Render("  Tool Details:"))
		for _, t := range toolInfos {
			fmt.Printf("    - %s: %s\n", toolStyle.Render(t.Name), info.Render(t.Description))
		}
	}
	fmt.Println()
}

// printMCPPrompts prints available prompts from MCP servers
func printMCPPrompts(cfg *config.Config) {
	prompts := mcp.GetPromptInfos(cfg)

	dim := config.Colors.Dim
	accent := config.Colors.Accent
	info := config.Colors.Info
	serverStyle := config.Colors.Command

	if len(prompts) == 0 {
		fmt.Println(dim.Render("No MCP prompts discovered"))
		return
	}

	fmt.Println(accent.Render(fmt.Sprintf("MCP Prompts (%d):", len(prompts))))
	// Group by server
	byServer := make(map[string][]mcp.PromptInfo)
	for _, p := range prompts {
		byServer[p.Server] = append(byServer[p.Server], p)
	}

	for server, pl := range byServer {
		fmt.Printf("  %s:\n", serverStyle.Render(server))
		for _, p := range pl {
			desc := p.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Printf("    - %s: %s\n", accent.Render(p.Name), info.Render(desc))
		}
	}
}

// handleMCPDynamicPrompt shows details for a specific prompt when invoked as /$server/$prompt
func handleMCPDynamicPrompt(cfg *config.Config, lowered string) bool {
	path := strings.TrimPrefix(lowered, "/")
	if path == "" {
		return false
	}

	// Parse /$server/$prompt format
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		return false
	}

	serverName := parts[0]
	promptName := parts[1]
	if serverName == "" || promptName == "" {
		return false
	}

	promptInfos := mcp.GetPromptInfos(cfg)
	for _, pi := range promptInfos {
		if strings.EqualFold(pi.Server, serverName) && strings.EqualFold(pi.Name, promptName) {
			renderPromptDetails(pi)
			return true
		}
	}
	return false
}

func renderPromptDetails(pi mcp.PromptInfo) {
	accent := config.Colors.Accent
	titleStyle := style.Title
	descStyle := style.ColorCyan // Assuming ColorCyan is a color, need to wrap in style if we want Render
	descSt := lipgloss.NewStyle().Foreground(descStyle)
	labelStyle := config.Colors.Label
	dim := config.Colors.Dim
	reqStyle := config.Colors.Warn
	optStyle := dim

	// Format: /$server/$prompt
	promptPath := "/" + pi.Server + "/" + pi.Name

	fmt.Println()
	fmt.Printf("%s", accent.Render(promptPath))
	if pi.Title != "" && !strings.EqualFold(pi.Title, pi.Name) {
		fmt.Printf(" — %s", titleStyle.Render(pi.Title))
	}
	fmt.Println()
	if pi.Description != "" {
		fmt.Println(descSt.Render(pi.Description))
	}
	fmt.Println(dim.Render("Server: ") + labelStyle.Render(pi.Server))
	if len(pi.Arguments) > 0 {
		fmt.Println(dim.Render("Arguments:"))
		for _, arg := range pi.Arguments {
			if arg == nil {
				continue
			}
			argLine := fmt.Sprintf("  - %s", labelStyle.Render(arg.Name))
			if arg.Required {
				argLine += " " + reqStyle.Render("(required)")
			} else {
				argLine += " " + optStyle.Render("(optional)")
			}
			if arg.Description != "" {
				argLine += ": " + descSt.Render(arg.Description)
			}
			fmt.Println(argLine)
		}
		// Show usage hint with arguments
		fmt.Println()
		fmt.Print(dim.Render("Usage: "))
		usageLine := accent.Render(promptPath) + " "
		for i, arg := range pi.Arguments {
			if arg == nil {
				continue
			}
			if i > 0 {
				usageLine += " "
			}
			if arg.Required {
				usageLine += reqStyle.Render(fmt.Sprintf("<%s>", arg.Name))
			} else {
				usageLine += optStyle.Render(fmt.Sprintf("[%s]", arg.Name))
			}
		}
		fmt.Println(usageLine)
	} else {
		// No arguments - show simple usage
		fmt.Println()
		fmt.Print(dim.Render("Usage: "))
		fmt.Println(accent.Render(promptPath) + " " + dim.Render("<your query>"))
	}
	fmt.Println()
}

// buildMCPSystemInstructions builds system-level MCP instructions without user content
func buildMCPSystemInstructions(cfg *config.Config) string {
	toolInfos := mcp.GetToolInfos(cfg)
	if len(toolInfos) == 0 {
		logger.Log("info", "No MCP tools available for system instructions")
		return ""
	}

	connectedServers := mcp.GetConnectedServerNames(cfg)

	var sb strings.Builder
	sb.WriteString("## Available MCP Tools for Diagnostics\n")
	sb.WriteString("You have access to MCP (Model Context Protocol) tools for real-time Kubernetes cluster diagnostics.\n\n")

	if len(connectedServers) > 0 {
		sb.WriteString(fmt.Sprintf("**Connected MCP Servers:** %s\n\n", strings.Join(connectedServers, ", ")))
	}

	// Include type definitions if available
	typeDefContent, err := mcp.GetTypeDefinitions(cfg)
	if err != nil {
		logger.Log("debug", "Failed to get type definitions: %v", err)
	}
	if typeDefContent != "" {
		sb.WriteString("## MCP Type Definitions\n")
		sb.WriteString("```typescript\n")
		sb.WriteString(typeDefContent)
		sb.WriteString("\n```\n\n")
		logger.Log("info", "Included MCP type definitions (%d bytes)", len(typeDefContent))
	}

	resourceInfos := mcp.GetResourceInfos(cfg)
	if len(resourceInfos) > 0 {
		logger.Log("debug", "[MCP] Available resources (%d):", len(resourceInfos))
		for _, ri := range resourceInfos {
			logger.Log("debug", "[MCP]   - %s (URI: %s, MIME: %s, Server: %s)", ri.Name, ri.URI, ri.MIMEType, ri.Server)
		}
	}

	sb.WriteString("**Instructions for Tool Usage:**\n")
	sb.WriteString("- These tools can be called automatically to gather real-time diagnostics\n")
	sb.WriteString("- When analyzing Kubernetes issues, use relevant MCP tools to get current state information\n")
	sb.WriteString("- Tool results will be automatically fed back into the analysis\n")

	logger.Log("info", "Built MCP system instructions (%d tools, %d resources)", len(toolInfos), len(resourceInfos))
	return sb.String()
}

// addMCPToolsToPrompt enriches the prompt with available MCP tools information for better RAG integration
// Deprecated: Use buildMCPSystemInstructions + MessageBuilder for role-separated prompts
func addMCPToolsToPrompt(cfg *config.Config, prompt string) string {
	mcpInstructions := buildMCPSystemInstructions(cfg)
	if mcpInstructions == "" {
		return prompt
	}

	var mcpContext strings.Builder
	mcpContext.WriteString("\n\n")
	mcpContext.WriteString(mcpInstructions)
	mcpContext.WriteString("\n---\n\n")
	if strings.Contains(prompt, "## User Query") {
		mcpContext.WriteString(prompt)
	} else {
		mcpContext.WriteString("## User Query\n")
		mcpContext.WriteString(prompt)
	}

	return mcpContext.String()
}

// showCostEstimation displays cost estimation information before exit
func showCostEstimation(cfg *config.Config) {
	if cfg.Model == "" {
		return // No model selected
	}

	// Create metadata service directly
	metadataService := metadata.NewMetadataService(cfg.ModelMetadataTimeout, cfg.ModelMetadataCacheTTL)

	baseURL := config.GetProviderBaseURL(cfg)
	models, err := metadataService.GetModelList(cfg.Provider, baseURL)
	if err != nil {
		return // Can't fetch pricing data
	}

	// Find current model in the list
	var currentModel *metadata.ModelMetadata
	for _, model := range models {
		if model.ID == cfg.Model {
			currentModel = model
			break
		}
	}

	if currentModel == nil || (currentModel.InputPrice == 0 && currentModel.OutputPrice == 0) {
		return // No pricing data available
	}

	// Calculate cost summary
	summary := lib.CalculateTotalCost(
		cfg.LastOutgoingTokens,
		cfg.LastIncomingTokens,
		currentModel.InputPrice,
		currentModel.OutputPrice,
		cfg.Model,
	)

	// Format and display the cost estimation
	costDisplay := lib.FormatTotalCostDisplay(summary)
	if costDisplay != "" {
		fmt.Println()
		fmt.Println(costDisplay)
		fmt.Println()
	}
}

// printInlineHelp prints quick usage information for interactive mode
func printInlineHelp(cfg *config.Config) {
	title := style.Title
	body := style.Info
	accent := config.Colors.Accent
	label := config.Colors.Dim
	prefix := cfg.CommandPrefix
	if strings.TrimSpace(prefix) == "" {
		prefix = "!"
	}

	fmt.Println()
	title.Render("Tips for getting started:")
	fmt.Println(body.Render(fmt.Sprintf("- Ask questions, or run commands with the configured prefix (default %q).", prefix)))
	fmt.Println(body.Render(fmt.Sprintf("- Example: %s kubectl get events -A", prefix)))
	fmt.Println(body.Render("- Type 'exit', 'quit', or 'bye' to leave"))
	fmt.Println()

	title.Render("Available Commands:")
	// Display slash commands from config
	for _, cmd := range cfg.SlashCommands {
		// Show primary command with description
		fmt.Printf("%s - %s\n", accent.Render(cmd.Primary), body.Render(cmd.Description))

		// Show variations if there are multiple commands
		if len(cmd.Commands) > 1 {
			var variations []string
			for _, variation := range cmd.Commands {
				if variation != cmd.Primary {
					variations = append(variations, variation)
				}
			}
			if len(variations) > 0 {
				fmt.Printf("    %s (%s)\n", label.Render("Aliases:"), body.Render(strings.Join(variations, ", ")))
			}
		}
	}
	fmt.Println()

	title.Render("Shell Commands:")
	fmt.Println(body.Render(fmt.Sprintf("- Press %s to toggle command mode; type %s again to exit command mode", prefix, prefix)))
	fmt.Println(body.Render("- Press tab for shell auto-completion"))
	fmt.Println()

	title.Render("Examples (cluster diagnostics):")
	fmt.Println(body.Render("- Pods: 'find any issues with pods'"))
	fmt.Println(body.Render("- Ingress: 'why is my ingress not routing traffic properly to backend services?'"))
	fmt.Println(body.Render("- Performance: 'identify pods consuming excessive CPU or memory in the production namespace'"))
	fmt.Println(body.Render("- Security: 'check for overly permissive RBAC settings in my cluster'"))
	fmt.Println(body.Render("- Dependencies: 'analyze the connection between my failing deployments and their dependent configmaps'"))
	fmt.Println(body.Render("- Events: 'summarize recent Warning events in kube-system and suggest next steps'"))
	fmt.Println(body.Render("- Networking: 'debug DNS resolution problems inside pods in staging'"))
	fmt.Println(body.Render("- Rollouts: 'find deployments stuck due to failed rollouts and why'"))
	fmt.Println()
}
