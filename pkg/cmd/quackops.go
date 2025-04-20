package cmd

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/completer"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/exec"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/llm"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
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
	cmd.Flags().StringVarP(&cfg.HistoryFile, "history-file", "", cfg.HistoryFile, "Path to the history file (default: ~/.quackops_history)")
	cmd.Flags().StringVarP(&cfg.KubectlBinaryPath, "kubectl-path", "k", cfg.KubectlBinaryPath, "Path to kubectl binary")
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

	// Setup colors for better readability
	titleColor := color.New(color.FgHiYellow, color.Bold)
	keyColor := color.New(color.FgHiCyan, color.Bold)
	typeColor := color.New(color.FgHiMagenta)
	defaultColor := color.New(color.FgHiGreen)
	currentColor := color.New(color.FgHiWhite, color.Bold)

	fmt.Println()
	titleColor.Println("ENVIRONMENT VARIABLES:")
	fmt.Println()

	// Get the keys and sort them for consistent output
	keys := make([]string, 0, len(envVars))
	for k := range envVars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Track which environment variables are currently set
	var setVars []string

	// Print each environment variable
	for _, key := range keys {
		info := envVars[key]
		keyColor.Printf("  %s\n", key)
		fmt.Printf("    Description: %s\n", info.Description)
		typeColor.Printf("    Type: %s\n", info.Type)
		defaultColor.Printf("    Default: %v\n", info.DefaultValue)

		// Check if the environment variable is set
		if val, exists := os.LookupEnv(key); exists {
			currentColor.Printf("    Current: %s\n", val)
			setVars = append(setVars, key)
		}

		fmt.Println()
	}

	fmt.Println("These environment variables can be set before running the command or passed as arguments with the format KEY=VALUE.")

	// Display summary of currently set environment variables
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

		// Display the current K8s context so user can confirm before proceeding
		if err := lib.KubeCtxInfo(cfg); err != nil {
			fmt.Printf("Warning: could not retrieve current Kubernetes context: %v\n", err)
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
		Prompt:       color.New(color.Bold).Sprint("❯ "),
		EOFPrompt:    "exit",
		AutoComplete: completer.NewShellAutoCompleter(cfg), // Use the new completer
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
	// Add listener to handle $ at beginning of line
	rlConfig.SetListener(func(line []rune, pos int, key rune) ([]rune, int, bool) {
		// Check if the line has $ after the current key event
		hasCommandPrefix := len(line) > 0 && line[0] == '$'

		// Update prompt based on current state
		if hasCommandPrefix {
			rl.SetPrompt(color.New(color.FgHiRed, color.Bold).Sprint("$ ❯ "))
		} else {
			rl.SetPrompt(color.New(color.Bold).Sprint("❯ "))
		}

		// Always reset on Enter or Interrupt
		if key == readline.CharEnter || key == readline.CharInterrupt {
			rl.SetPrompt(color.New(color.Bold).Sprint("❯ "))
		}

		return line, pos, false
	})

	rl, err := readline.NewEx(rlConfig)
	if err != nil {
		log.Fatalf("Failed to create interactive prompt instance: %v", err)
	}
	defer rl.Close()

	var hello = "Tell me what you need! Use '$' prefix to run commands or type 'bye' to exit."
	decodedArt, _ := base64.StdEncoding.DecodeString(cfg.DuckASCIIArt)
	fmt.Println(string(decodedArt) + hello)

	// Chat loop
	var lastTextPrompt string
	var userMsgCount int
	for {
		userPrompt, err := rl.Readline()
		if err != nil { // io.EOF is returned on Ctrl-C
			fmt.Println("Exiting...")
			break
		}

		userPrompt = strings.TrimSpace(userPrompt)
		if userPrompt == "" {
			continue
		}

		switch strings.ToLower(userPrompt) {
		case "bye", "exit", "quit":
			fmt.Println("🦆" + "...quack!")
			return nil
		}

		if !strings.HasPrefix(userPrompt, "$") {
			lastTextPrompt = userPrompt
			userMsgCount++
		}

		err = processUserPrompt(cfg, userPrompt, lastTextPrompt, userMsgCount)
		if err != nil {
			return err
		}
	}

	return nil
}

func processUserPrompt(cfg *config.Config, userPrompt string, lastTextPrompt string, userMsgCount int) error {
	var augPrompt string
	var err error

	// If the prompt starts with $, execute the command and store the result
	// We don't run LLM query after command execution
	if strings.HasPrefix(userPrompt, "$") {
		// Execute the command
		cmdResults, err := exec.ExecDiagCmds(cfg, []string{userPrompt})
		if err != nil {
			fmt.Println(color.HiRedString(err.Error()))
		}
		// Store the command results for the next user prompt
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

	_, err = llm.Request(cfg, augPrompt, true, true)
	if err != nil {
		return fmt.Errorf("error requesting LLM: %w", err)
	}

	llm.ManageChatThreadContext(cfg.ChatMessages, cfg.MaxTokens)
	return nil
}
