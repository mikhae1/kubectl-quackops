package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/completer"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/exec"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/llm"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/mikhae1/kubectl-quackops/pkg/mcp"
	"github.com/mikhae1/kubectl-quackops/pkg/version"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// Global cleanup function for signal handling
var globalCleanupFunc func()

// setupGlobalSignalHandling sets up signal handling that can be used throughout the application
func setupGlobalSignalHandling(cleanupFunc func()) {
	globalCleanupFunc = cleanupFunc
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		if globalCleanupFunc != nil {
			globalCleanupFunc()
		}
		// Explicitly restore cursor visibility
		fmt.Print("\033[?25h") // ANSI escape sequence to show cursor
		fmt.Println("\nExiting...")
		os.Exit(0)
	}()
}

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

	cmd.Flags().StringVarP(&cfg.Provider, "provider", "p", cfg.Provider, "LLM model provider (e.g., 'ollama', 'openai', 'google', 'anthropic')")
	cmd.Flags().StringVarP(&cfg.Model, "model", "m", cfg.Model, "LLM model to use")
	cmd.Flags().StringVarP(&cfg.ApiURL, "api-url", "u", cfg.ApiURL, "URL for LLM API, used with 'ollama' provider")
	cmd.Flags().BoolVarP(&cfg.SafeMode, "safe-mode", "s", cfg.SafeMode, "Enable safe mode to prevent executing commands without confirmation")
	cmd.Flags().IntVarP(&cfg.Retries, "retries", "r", cfg.Retries, "Number of retries for kubectl commands")
	cmd.Flags().IntVarP(&cfg.Timeout, "timeout", "t", cfg.Timeout, "Timeout for kubectl commands in seconds")
	cmd.Flags().IntVarP(&cfg.MaxTokens, "max-tokens", "x", cfg.MaxTokens, "Maximum number of tokens in LLM context window")
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
	cmd.Flags().StringVarP(&cfg.MCPConfigPath, "mcp-config", "", cfg.MCPConfigPath, "Path to MCP client configuration file (default: ~/.config/quackops/mcp.yaml)")
	cmd.Flags().IntVarP(&cfg.MCPToolTimeout, "mcp-tool-timeout", "", cfg.MCPToolTimeout, "Timeout in seconds for MCP tool calls")
	cmd.Flags().BoolVarP(&cfg.MCPStrict, "mcp-strict", "", cfg.MCPStrict, "Strict MCP mode: do not fall back to local execution when MCP fails")
	// Diagnostics flags
	cmd.Flags().BoolVarP(&cfg.EnableBaseline, "enable-baseline", "", cfg.EnableBaseline, "Enable baseline diagnostic pack before LLM")
	cmd.Flags().IntVarP(&cfg.EventsWindowMinutes, "events-window-minutes", "", cfg.EventsWindowMinutes, "Events time window in minutes for summarization")
	cmd.Flags().BoolVarP(&cfg.EventsWarningsOnly, "events-warn-only", "", cfg.EventsWarningsOnly, "Include only Warning events in summaries")
	cmd.Flags().IntVarP(&cfg.LogsTail, "logs-tail", "", cfg.LogsTail, "Tail lines for log aggregation when triggered by playbooks")
	cmd.Flags().BoolVarP(&cfg.LogsAllContainers, "logs-all-containers", "", cfg.LogsAllContainers, "Aggregate logs from all containers when collecting logs")
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
	envVars := config.GetEnvVarsInfo()

	// Colors for readability
	titleColor := color.New(color.FgHiYellow, color.Bold)
	keyColor := color.New(color.FgHiCyan, color.Bold)
	typeColor := color.New(color.FgHiMagenta)
	defaultColor := color.New(color.FgHiGreen)
	currentColor := color.New(color.FgHiWhite, color.Bold)

	fmt.Println()
	titleColor.Println("ENVIRONMENT VARIABLES:")
	fmt.Println()

	// Sort keys for consistent output
	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Track currently set environment variables
	var setVars []string

	// Print each environment variable
	for _, key := range keys {
		info := envVars[key]
		keyColor.Printf("  %s\n", key)
		fmt.Printf("    Description: %s\n", info.Description)
		typeColor.Printf("    Type: %s\n", info.Type)
		defaultColor.Printf("    Default: %v\n", info.DefaultValue)

		// If environment variable is set, show it
		if val, exists := os.LookupEnv(key); exists {
			currentColor.Printf("    Current: %s\n", val)
			setVars = append(setVars, key)
		}

		fmt.Println()
	}

	fmt.Println("These environment variables can be set before running the command or passed as arguments with the format KEY=VALUE.")

	// Summary of currently set environment variables
	if len(setVars) > 0 {
		fmt.Println()
		titleColor.Println("CURRENTLY SET ENVIRONMENT VARIABLES:")
		for _, key := range setVars {
			val, _ := os.LookupEnv(key)
			keyColor.Printf("  %s=%s\n", key, val)
		}
	}

	fmt.Println()
}

// runQuackOps is the main function for the QuackOps command
func runQuackOps(cfg *config.Config, args []string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		logger.InitLoggers(os.Stderr, 0)
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

	// Set history file if not disabled
	if !cfg.DisableHistory && cfg.HistoryFile != "" {
		// Ensure the history file directory exists
		historyDir := filepath.Dir(cfg.HistoryFile)
		if err := os.MkdirAll(historyDir, 0755); err != nil {
			fmt.Printf("Warning: could not create history file directory: %v\n", err)
		}
		rlConfig.HistoryFile = cfg.HistoryFile
	}

	var rl *readline.Instance
	// Capture ESC and $ for edit mode toggle
	rlConfig.FuncFilterInputRune = func(r rune) (rune, bool) {
		// Toggle edit mode with $ key press
		if r == '$' {
			cfg.EditMode = !cfg.EditMode
			if rl != nil {
				if cfg.EditMode {
					rl.SetPrompt(lib.FormatEditPromptWith(cfg))
					// Reset edit-mode history cursor on entry
					cfg.EditModeHistoryIndex = len(cfg.EditModeHistory)
				} else {
					rl.SetPrompt(lib.FormatContextPrompt(cfg, false))
					// Reset history index when leaving
					cfg.EditModeHistoryIndex = 0
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
				rl.Refresh()
			}
			return 0, false // swallow ESC
		}

		return r, true
	}

	// Avoid recomputing prompt on every keystroke to prevent latency
	rlConfig.SetListener(func(line []rune, pos int, key rune) ([]rune, int, bool) {
		// Intercept history navigation in edit mode to show successful commands without `$` prefix
		if cfg.EditMode {
			// Up/down arrows
			if key == readline.CharPrev || key == readline.CharNext {
				// Initialize history index lazily
				if cfg.EditModeHistoryIndex == 0 {
					cfg.EditModeHistoryIndex = len(cfg.EditModeHistory)
				}

				if key == readline.CharPrev {
					if cfg.EditModeHistoryIndex > 0 {
						cfg.EditModeHistoryIndex--
					}
				} else if key == readline.CharNext {
					if cfg.EditModeHistoryIndex < len(cfg.EditModeHistory) {
						cfg.EditModeHistoryIndex++
					}
				}

				// Determine replacement line
				var replacement string
				if cfg.EditModeHistoryIndex >= 0 && cfg.EditModeHistoryIndex < len(cfg.EditModeHistory) {
					replacement = cfg.EditModeHistory[cfg.EditModeHistoryIndex]
				} else {
					// Past newest entry: show empty line
					replacement = ""
				}
				// Replace current buffer with replacement
				return []rune(replacement), len([]rune(replacement)), true
			}
		}
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
	})

	rl, err := readline.NewEx(rlConfig)
	if err != nil {
		return fmt.Errorf("failed to create interactive prompt instance: %w", err)
	}

	cleanupAndExit := func(message string, exitCode int) {
		if message != "" {
			fmt.Println(message)
		}
		if rl != nil {
			rl.Close()
		}
		// Explicitly restore cursor visibility
		// fmt.Print("\033[?25h") // ANSI escape sequence to show cursor
		if exitCode >= 0 {
			os.Exit(exitCode)
		}
	}

	defer cleanupAndExit("", -1) // just cleanup

	setupGlobalSignalHandling(func() {
		if rl != nil {
			rl.Close()
		}
	})

	printWelcomeBanner(cfg)
	if cfg.MCPClientEnabled {
		info := color.New(color.FgHiWhite)
		dim := color.New(color.FgHiBlack)
		accent := color.New(color.FgHiCyan)
		servers := mcp.Servers(cfg)
		tools := mcp.Tools(cfg)
		srvStr := "none"
		if len(servers) > 0 {
			srvStr = strings.Join(servers, ", ")
		}
		if srvStr != "none" {
			srvStr = accent.Sprint(srvStr)
		}
		line := fmt.Sprintf("on · servers:[%s] · tools:%d · strict:%t", srvStr, len(tools), cfg.MCPStrict)
		fmt.Println(dim.Sprint("MCP:") + " " + info.Sprint(line))
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
		userPrompt, err := rl.Readline()
		if err != nil { // io.EOF is returned on Ctrl-C
			cleanupAndExit("Exiting...", 0)
		}

		userPrompt = strings.TrimSpace(userPrompt)
		if userPrompt == "" {
			continue
		}

		switch strings.ToLower(userPrompt) {
		case "bye", "exit", "quit", "/bye", "/exit", "/quit", "/q":
			cleanupAndExit("🦆...quack!", 0)
		case "/version":
			fmt.Println(version.Version)
			continue
		case "/mcp":
			if cfg.MCPClientEnabled {
				printMCPDetails(cfg)
			} else {
				fmt.Println("MCP client: disabled")
			}
			continue
		case "/model":
			prov := strings.ToUpper(strings.TrimSpace(cfg.Provider))
			if prov == "" {
				prov = "DEFAULT"
			}
			m := strings.TrimSpace(cfg.Model)
			if m == "" {
				m = "auto"
			}
			fmt.Printf("%s/%s\n", prov, m)
			continue
		case "/reset":
			cfg.ChatMessages = nil
			cfg.StoredUserCmdResults = nil
			fmt.Println("Context reset")
			continue
		case "/clear":
			fmt.Print("\033[2J\033[H")
			continue
		case "/servers":
			if cfg.MCPClientEnabled {
				list := mcp.Servers(cfg)
				if len(list) == 0 {
					fmt.Println("No MCP servers configured")
				} else {
					fmt.Println("MCP servers:")
					for _, s := range list {
						fmt.Printf(" - %s\n", s)
					}
				}
			} else {
				fmt.Println("MCP client: disabled")
			}
			continue
		case "/tools":
			if cfg.MCPClientEnabled {
				toolInfos := mcp.GetToolInfos(cfg)
				if len(toolInfos) == 0 {
					fmt.Println("No MCP tools discovered")
				} else {
					toolColor := color.New(color.FgHiCyan)
					descColor := color.New(color.FgWhite)
					fmt.Printf("MCP tools (%d):\n", len(toolInfos))
					for _, tool := range toolInfos {
						// Truncate description if too long
						desc := tool.Description
						maxLen := 320
						if len(desc) > maxLen {
							desc = desc[:maxLen] + "..."
						}
						fmt.Printf(" - %s: %s\n", toolColor.Sprint(tool.Name), descColor.Sprint(desc))
					}
				}
			} else {
				fmt.Println("MCP client: disabled")
			}
			continue
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
			prefix = "$"
		}
		wasPrefixed := strings.HasPrefix(originalUserPrompt, prefix)
		wasCommand := wasEditMode || wasPrefixed

		err = processUserPrompt(cfg, userPrompt, lastTextPrompt, userMsgCount)
		if err != nil {
			return err
		}

		// Save history according to rules
		if !strings.HasPrefix(originalUserPrompt, "/") {
			if wasEditMode {
				// In edit mode, save only successful commands (persist with prefix),
				// and store in-memory without the prefix for navigation.
				success := false
				if len(cfg.StoredUserCmdResults) > 0 {
					last := cfg.StoredUserCmdResults[len(cfg.StoredUserCmdResults)-1]
					success = (last.Err == nil) && (strings.TrimSpace(last.Cmd) != "")
				}
				if success {
					entry := originalUserPrompt
					if !strings.HasPrefix(entry, prefix) {
						entry = prefix + " " + entry
					}
					// Persist to file if enabled
					if !cfg.DisableHistory && cfg.HistoryFile != "" {
						_ = rl.Operation.SaveHistory(entry)
					}
					// Track in-memory history for edit mode navigation
					clean := strings.TrimSpace(strings.TrimPrefix(entry, prefix))
					cfg.EditModeHistory = append(cfg.EditModeHistory, clean)
					cfg.EditModeHistoryIndex = len(cfg.EditModeHistory)
				}
			} else {
				// Non-edit entries: persist as-is as prompts if enabled
				if !cfg.DisableHistory && cfg.HistoryFile != "" {
					_ = rl.Operation.SaveHistory(originalUserPrompt)
				}
				// If this was a prefixed command executed outside edit mode, include in edit-mode
				// history (in-memory) when successful so it can be recalled while editing.
				if wasCommand {
					success := false
					if len(cfg.StoredUserCmdResults) > 0 {
						last := cfg.StoredUserCmdResults[len(cfg.StoredUserCmdResults)-1]
						success = (last.Err == nil) && (strings.TrimSpace(last.Cmd) != "")
					}
					if success {
						clean := originalUserPrompt
						if strings.HasPrefix(clean, prefix) {
							clean = strings.TrimSpace(strings.TrimPrefix(clean, prefix))
						}
						cfg.EditModeHistory = append(cfg.EditModeHistory, clean)
						cfg.EditModeHistoryIndex = len(cfg.EditModeHistory)
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
	rainbow := []*color.Color{
		color.New(color.FgHiRed),
		color.New(color.FgHiYellow),
		color.New(color.FgHiGreen),
		color.New(color.FgHiCyan),
		color.New(color.FgHiBlue),
		color.New(color.FgHiMagenta),
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
	magenta := config.Colors.Magenta
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
	if cfg.ApiURL != "" {
		apiPlain = fmt.Sprintf("K8s API: %s", cfg.ApiURL)
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
			b.WriteString(c.Sprint(string(r)))
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
				b.WriteString(shadow.Sprint(string(r)))
				// do not advance col; excluded from chess pattern
				continue
			}
			c := monoPalette[(row+col)%len(monoPalette)]
			b.WriteString(c.Sprint(string(r)))
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
		fmt.Println(indent + dim.Sprint("Using Kubernetes context:") + " " + warn.Sprintf("unavailable (%v)", ctxErr))
	} else if ctxName != "" {
		fmt.Println(indent + dim.Sprint("Using Kubernetes context:") + " " + info.Sprintf("%s", ctxName))
	}

	// Horizontal gradient line under the ASCII art (aligned under right column)
	if maxRight > 0 {
		line := strings.Repeat("─", maxRight)
		colored := gradientizeMono(line, 0)
		fmt.Println(indent + colored)
	}

	// Non-rainbow info lines (useful details) - keep original text, but left-aligned
	llmStyled := dim.Sprint("LLM:") + " " + accent.Sprintf("%s", provider) + dim.Sprint(" · ") + magenta.Sprintf("%s", model)
	fmt.Println(indent + llmStyled)
	if apiPlain != "" {
		fmt.Println(indent + dim.Sprint("API:") + " " + info.Sprintf("%s", cfg.ApiURL))
	}
	fmt.Println(indent + dim.Sprint("Safe mode:") + " " + func() string {
		if cfg.SafeMode {
			return ok.Sprint("On")
		}
		return warn.Sprint("Off")
	}())
	fmt.Println(indent + dim.Sprint("History:") + " " + func() string {
		if !cfg.DisableHistory && cfg.HistoryFile != "" {
			return info.Sprintf("%s", cfg.HistoryFile)
		}
		return dim.Sprint("disabled")
	}())

	// Tips for getting started
	fmt.Println()
	fmt.Println(indent + accent.Sprint("Getting started:"))
	fmt.Println(indent + info.Sprint("- ") + dim.Sprint("Ask questions:") + " " + info.Sprint("find pod issues in nginx namespace"))
	fmt.Println(indent + info.Sprint("- ") + dim.Sprint("Run commands:") + " " + info.Sprint(cfg.CommandPrefix+" kubectl get events -A"))
	fmt.Println(indent + info.Sprint("- ") + dim.Sprint("Type: ") + ok.Sprint("/help") + " " + info.Sprint("for more information"))
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

	// Handle slash commands (e.g., /help) before any other processing
	lowered := strings.ToLower(strings.TrimSpace(userPrompt))
	if strings.HasPrefix(lowered, "/") {
		switch lowered {
		case "/help", "/h", "/?":
			printInlineHelp()
			return nil
		default:
			fmt.Printf("Unknown command: %s\n", userPrompt)
			fmt.Println("Type /help for available commands.")
			return nil
		}
	}

	// Edit mode: treat input as command without requiring '$' prefix
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
	if userMsgCount%2 == 1 || cfg.MaxTokens > 16000 {
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
			logger.Log("err", "Error retrieving RAG: %v", err)
		}
	}

	if augPrompt == "" {
		augPrompt = userPrompt
	}

	// Add MCP tool information to the prompt if MCP is enabled
	if cfg.MCPClientEnabled {
		augPrompt = addMCPToolsToPrompt(cfg, augPrompt)
		logger.Log("info", "Enhanced prompt with MCP tool information for better diagnostics")
	}

	_, err = llm.Request(cfg, augPrompt, true, true)
	if err != nil {
		return fmt.Errorf("error requesting LLM: %w", err)
	}

	llm.ManageChatThreadContext(cfg.ChatMessages, cfg.MaxTokens)
	return nil
}

// printMCPDetails displays detailed MCP information with proper formatting
func printMCPDetails(cfg *config.Config) {
	titleColor := color.New(color.FgHiYellow, color.Bold)
	toolColor := color.New(color.FgHiCyan)
	descColor := color.New(color.FgWhite)
	serverColor := color.New(color.FgHiGreen)

	fmt.Println()
	titleColor.Println("MCP Details:")

	// Show servers
	srvs := mcp.Servers(cfg)
	connectedSrvs := mcp.GetConnectedServerNames(cfg)
	connectedMap := make(map[string]bool)
	for _, srv := range connectedSrvs {
		connectedMap[srv] = true
	}

	if len(srvs) == 0 {
		fmt.Println("- servers: none")
	} else {
		fmt.Println("- servers:")
		for _, s := range srvs {
			if connectedMap[s] {
				fmt.Printf("  · %s\n", serverColor.Sprint(s))
			} else {
				fmt.Printf("  · %s (disconnected)\n", color.HiBlackString(s))
			}
		}
	}

	// Show tools with descriptions
	toolInfos := mcp.GetToolInfos(cfg)
	if len(toolInfos) == 0 {
		fmt.Println("- tools: none")
	} else {
		fmt.Printf("- tools (%d):\n", len(toolInfos))
		for _, tool := range toolInfos {
			// Truncate description if too long
			desc := tool.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Printf("  · %s: %s\n", toolColor.Sprint(tool.Name), descColor.Sprint(desc))
		}
	}
	fmt.Println()
}

// addMCPToolsToPrompt enriches the prompt with available MCP tools information for better RAG integration
func addMCPToolsToPrompt(cfg *config.Config, prompt string) string {
	toolInfos := mcp.GetToolInfos(cfg)
	if len(toolInfos) == 0 {
		logger.Log("info", "No MCP tools available for prompt enhancement")
		return prompt
	}

	// Get server information
	connectedServers := mcp.GetConnectedServerNames(cfg)

	// Build enhanced MCP tools context for RAG
	var mcpContext strings.Builder
	mcpContext.WriteString("\n\n## Available MCP Tools for Diagnostics\n")
	mcpContext.WriteString("You have access to the following MCP (Model Context Protocol) tools that can provide real-time Kubernetes cluster diagnostics:\n\n")

	if len(connectedServers) > 0 {
		mcpContext.WriteString(fmt.Sprintf("**Connected MCP Servers:** %s\n\n", strings.Join(connectedServers, ", ")))
	}

	mcpContext.WriteString("**Instructions for Tool Usage:**\n")
	mcpContext.WriteString("- These tools can be called automatically to gather real-time diagnostics\n")
	mcpContext.WriteString("- When analyzing cluster issues, use relevant MCP tools to get current state information\n")
	mcpContext.WriteString("- Tool results will be automatically fed back into the analysis\n")
	if cfg.MCPStrict {
		mcpContext.WriteString("- Operating in strict MCP mode - no local fallbacks\n")
	}
	mcpContext.WriteString("\n")

	mcpContext.WriteString("---\n\n")
	mcpContext.WriteString("## User Query\n")
	mcpContext.WriteString(prompt)

	logger.Log("info", "Using MCP tools in prompt with %d tools", len(toolInfos))
	return mcpContext.String()
}

// printInlineHelp prints quick usage information for interactive mode
func printInlineHelp() {
	title := color.New(color.FgHiYellow, color.Bold)
	body := color.New(color.FgHiWhite)

	fmt.Println()
	title.Println("Tips for getting started:")
	fmt.Println(body.Sprint("- Ask questions, or run commands with the configured prefix (default \"$\")."))
	fmt.Println(body.Sprint("- Example: $ kubectl get events -A"))
	fmt.Println(body.Sprint("- Type 'exit', 'quit', or 'bye' to leave"))
	fmt.Println()
	title.Println("Commands:")
	fmt.Println(body.Sprint("- Press $ to toggle command mode; type $ again to exit command mode"))
	fmt.Println(body.Sprint("- Press tab for shell auto-completion"))
	fmt.Println()
	title.Println("Examples (cluster diagnostics):")
	fmt.Println(body.Sprint("- Pods: 'find any issues with pods'"))
	fmt.Println(body.Sprint("- Ingress: 'why is my ingress not routing traffic properly to backend services?'"))
	fmt.Println(body.Sprint("- Performance: 'identify pods consuming excessive CPU or memory in the production namespace'"))
	fmt.Println(body.Sprint("- Security: 'check for overly permissive RBAC settings in my cluster'"))
	fmt.Println(body.Sprint("- Dependencies: 'analyze the connection between my failing deployments and their dependent configmaps'"))
	fmt.Println(body.Sprint("- Events: 'summarize recent Warning events in kube-system and suggest next steps'"))
	fmt.Println(body.Sprint("- Networking: 'debug DNS resolution problems inside pods in staging'"))
	fmt.Println(body.Sprint("- Rollouts: 'find deployments stuck due to failed rollouts and why'"))
	fmt.Println()
}
