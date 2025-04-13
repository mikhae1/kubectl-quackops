package cmd

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/briandowns/spinner"
	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/animator"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/formatter"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/spf13/cobra"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

type CmdRes struct {
	Cmd string
	Out string
	Err error
}

func NewRootCmd(streams genericiooptions.IOStreams) *cobra.Command {
	cfg := config.LoadConfig()
	cmd := &cobra.Command{
		Use:          "kubectl-quackops",
		Short:        "QuackOps is a plugin for managing Kubernetes cluster using AI",
		SilenceUsage: true,
		RunE:         runQuackOps(cfg, os.Args),
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

	return cmd
}

func runQuackOps(cfg *config.Config, args []string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		logger.InitLoggers(os.Stderr, 0)

		// Display the current K8s context so user can confirm before proceeding
		if err := displayCurrentContext(cfg); err != nil {
			fmt.Printf("Warning: could not retrieve current Kubernetes context: %v\n", err)
		}

		if err := processCommands(cfg, args); err != nil {
			fmt.Printf("Error processing commands: %v\n", err)
			return err
		}
		return nil
	}
}

// displayCurrentContext shows the user which Kubernetes context is currently active
func displayCurrentContext(cfg *config.Config) error {
	res, err := execDiagCmds(cfg, []string{"$kubectl config current-context"})
	if err != nil {
		return err
	}
	ctxName := strings.TrimSpace(res[0].Out)

	cmdRes := execKubectlCmd(cfg, "kubectl cluster-info")
	if cmdRes.Err != nil {
		return cmdRes.Err
	}

	info := strings.TrimSpace(cmdRes.Out)
	if info == "" {
		fmt.Println(color.HiRedString("Current Kubernetes context is empty or not set."))
	} else {
		infoLines := strings.Split(info, "\n")
		fmt.Printf(color.HiYellowString("Using Kubernetes context")+": %s\n%s", ctxName, strings.Join(infoLines[:len(infoLines)-1], "\n"))
	}

	return nil
}

// customCompleter is an implementation of readline.AutoCompleter
type customCompleter struct {
	cfg *config.Config
}

// Do implements the AutoCompleter interface for tab completion
func (c *customCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	lineStr := string(line[:pos])

	// Only enable completion for $ mode
	if len(lineStr) == 0 || lineStr[0] != '$' {
		return [][]rune{}, 0
	}

	// Remove the $ prefix for completion processing
	lineStr = strings.TrimPrefix(lineStr, "$")
	lineStr = strings.TrimSpace(lineStr)

	// If the line is empty after the $, complete with common commands
	if lineStr == "" {
		return c.completeInitialCommands()
	}

	// Parse the command line, respecting quotes
	words := parseCommandLine(lineStr)
	if len(words) == 0 {
		return [][]rune{}, 0
	}

	// Get the word being completed (which might be empty if we're at a space)
	incomplete := ""

	// If the line ends with a space, we're completing a new word
	if len(lineStr) > 0 && lineStr[len(lineStr)-1] == ' ' {
		words = append(words, "")
		incomplete = ""
	} else {
		// Otherwise, we're completing the last word
		incomplete = words[len(words)-1]
		words = words[:len(words)-1]
	}

	// Determine the command being used
	if len(words) == 0 {
		// Initial command completion
		return c.completeCommand(incomplete)
	} else {
		// Command-specific completion
		command := words[0]
		if command == "kubectl" || command == "helm" || command == "docker" {
			return handleCmdCompletion(c.cfg, words, incomplete)
		} else {
			return handleShellCompletion(c.cfg, incomplete)
		}
	}
}

// parseCommandLine splits a command line into tokens, respecting quotes
func parseCommandLine(line string) []string {
	var tokens []string
	var current strings.Builder
	var inQuotes bool
	var escapeNext bool

	for _, char := range line {
		if escapeNext {
			current.WriteRune(char)
			escapeNext = false
			continue
		}

		if char == '\\' {
			escapeNext = true
			continue
		}

		if char == '"' || char == '\'' {
			inQuotes = !inQuotes
			continue
		}

		if char == ' ' && !inQuotes {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(char)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// completeInitialCommands provides common command completions after the $ prefix
func (c *customCompleter) completeInitialCommands() ([][]rune, int) {
	commands := []string{
		"kubectl ",
		"helm ",
		"docker ",
		"ls ",
		"cd ",
		"grep ",
		"cat ",
		"echo ",
		"find ",
	}

	completions := make([][]rune, len(commands))
	for i, cmd := range commands {
		completions[i] = []rune(cmd)
	}

	return completions, 0
}

// completeCommand completes the initial command after $
func (c *customCompleter) completeCommand(prefix string) ([][]rune, int) {
	if prefix == "" {
		return c.completeInitialCommands()
	}

	// Get command completions from bash
	command := fmt.Sprintf("compgen -c %s", prefix)
	output, err := exec.Command("bash", "-c", command).Output()
	if err != nil {
		return [][]rune{}, 0
	}

	completions := [][]rune{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, prefix) {
			// Add a space after command names
			completions = append(completions, []rune(line[len(prefix):]+" "))
		}
	}

	return completions, len(prefix)
}

func processCommands(cfg *config.Config, args []string) error {
	if len(args) > 0 {
		userPrompt := strings.TrimSpace(args[0])
		if userPrompt != "" {
			return processUserPrompt(cfg, userPrompt, "", 1)
		}
	}

	// Create a more modern prompt
	rlConfig := &readline.Config{
		Prompt:       color.New(color.Bold).Sprint("❯ "),
		EOFPrompt:    "exit",
		AutoComplete: &customCompleter{cfg: cfg}, // Pass the config to the custom completer
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

	if userMsgCount%2 == 1 || strings.HasPrefix(userPrompt, "$") || cfg.MaxTokens > 16000 {
		augPrompt, err = retrieveRAG(cfg, userPrompt, lastTextPrompt)
		if err != nil {
			if strings.HasPrefix(userPrompt, "$") {
				fmt.Println(color.HiRedString(err.Error()))
			} else {
				logger.Log("err", "Error retrieving RAG: %v", err)
			}
		}
	}

	if augPrompt == "" {
		augPrompt = userPrompt
	}

	_, err = llmRequest(cfg, augPrompt, true)
	if err != nil {
		return fmt.Errorf("error requesting LLM: %w", err)
	}

	manageChatThreadContext(cfg.ChatMessages, cfg.MaxTokens)
	return nil
}

// retrieveRAG retrieves the data for RAG
func retrieveRAG(cfg *config.Config, prompt string, lastTextPrompt string) (augPrompt string, err error) {
	var cmdResults []CmdRes

	// Create a spinner for diagnostic information gathering
	s := spinner.New(spinner.CharSets[11], 10*time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Suffix = " Gathering diagnostic information..."
	s.Color("cyan", "bold")
	s.Start()
	defer s.Stop()

	if strings.HasPrefix(prompt, "$") {
		// Direct command execution
		cmdResults, err = execDiagCmds(cfg, []string{prompt})
	} else {
		// Retrieve and execute relevant kubectl commands based on the user's query
		for i := 0; i < cfg.Retries; i++ {
			var cmds []string
			cmds, err = getKubectlCmds(cfg, prompt)
			if err != nil {
				logger.Log("warn", "Error retrieving kubectl commands: %v", err)
				continue
			}

			cmdResults, err = execDiagCmds(cfg, slices.Compact(cmds))
			if len(cmdResults) == 0 {
				logger.Log("warn", "No results found, retrying... %d/%d", i, cfg.Retries)
				continue
			}
			break
		}
	}

	// Process command outputs
	var contextBuilder strings.Builder

	// Format each command result for context
	for _, cmd := range cmdResults {
		if cmd.Err != nil {
			continue
		}

		// Filter sensitive data if enabled
		output := cmd.Out
		if !cfg.DisableSecretFilter {
			output = filterSensitiveData(output)
		}

		// Format the command and its output
		contextBuilder.WriteString("Command: ")
		contextBuilder.WriteString(cmd.Cmd)
		contextBuilder.WriteString("\n\nOutput:\n")
		contextBuilder.WriteString(output)
		contextBuilder.WriteString("\n\n---\n\n")
	}

	contextData := contextBuilder.String()

	// If the context is too large, intelligently truncate it rather than just cutting it off
	if len(tokenize(contextData)) > cfg.MaxTokens*2 {
		// Split into sections by command
		sections := strings.Split(contextData, "\n\n---\n\n")

		// Keep the first and last sections (usually most important)
		var truncatedBuilder strings.Builder
		if len(sections) >= 2 {
			truncatedBuilder.WriteString(sections[0])
			truncatedBuilder.WriteString("\n\n---\n\n")

			// Add a note about truncation
			truncatedBuilder.WriteString("(Some command outputs were omitted for brevity)\n\n---\n\n")

			// Add the last section
			truncatedBuilder.WriteString(sections[len(sections)-1])
		} else {
			// If there's only one section, truncate it
			truncatedBuilder.WriteString(contextData[:cfg.MaxTokens*2])
			truncatedBuilder.WriteString("\n...(output truncated)...")
		}

		contextData = truncatedBuilder.String()
	}

	// Determine the actual user prompt
	userPrompt := prompt
	if strings.HasPrefix(prompt, "$") {
		userPrompt = lastTextPrompt
	}

	// Customize output format based on the provider
	outputFormat := ""
	if !cfg.DisableMarkdownFormat {
		outputFormat = "Format your response using Markdown, including headings, lists, and code blocks for improved readability in a terminal environment."
	} else {
		outputFormat = "Provide a clear, concise analysis that is easy to read in a terminal environment."
	}

	// Construct the final prompt with clear instructions
	if len(contextData) > 0 {
		augPrompt = fmt.Sprintf(`# Kubernetes Diagnostic Analysis

## Command Outputs
%s

## Task
%s

## Guidelines
- You are an experienced Kubernetes administrator with deep expertise in diagnostics
- Analyze the command outputs above and provide insights on the issue
- Identify potential problems or anomalies in the cluster state
- Suggest next steps or additional commands if needed
- %s

`, contextData, userPrompt, outputFormat)
	}

	return augPrompt, err
}

// Hide sensitive data from Kubectl outputs
func filterSensitiveData(input string) string {
	var data map[string]interface{}

	// Try to parse as JSON, if fails, try YAML
	inputType := ""
	if err := json.Unmarshal([]byte(input), &data); err == nil {
		inputType = "json"
	} else if err := yaml.Unmarshal([]byte(input), &data); err == nil {
		inputType = "yaml"
	} else {
		// Handle plain text for describe commands
		return filterDescribeOutput(input)
	}

	// Check if the kind is "Secret" or "ConfigMap"
	if kind, ok := data["kind"].(string); ok && (kind == "Secret" || kind == "ConfigMap") {
		// Check for data or stringData fields
		for _, field := range []string{"data", "stringData"} {
			if _, found := data[field]; found {
				section := data[field].(map[string]interface{})
				newSection := make(map[string]interface{})
				for key, val := range section {
					if strVal, ok := val.(string); ok && strVal != "" {
						newSection[key] = "***FILTERED***"
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
		logger.Log("warn", "Error marshaling data: %v", err)
		return input // Return the original input if marshaling fails
	}

	return string(output)
}

// filterDescribeOutput filters sections from the kubectl describe output for Data section
func filterDescribeOutput(input string) string {
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

// manageChatThreadContext manages the context window of the chat thread
func manageChatThreadContext(chatMessages []llms.ChatMessage, maxTokens int) {
	if chatMessages == nil {
		return
	}

	// If the token length exceeds the context window, remove the oldest message in loop
	calculateThreadTokenLength := func(messages []llms.ChatMessage) int {
		threadLen := 0
		for _, message := range messages {
			tokens := tokenize(message.GetContent())
			threadLen += len(tokens)
		}
		return threadLen
	}

	threadLen := calculateThreadTokenLength(chatMessages)
	if threadLen > maxTokens {
		logger.Log("warn", "Thread should be truncated: %d messages, %d tokens", len(chatMessages), threadLen)
	}

	// Truncate the thread if it exceeds the maximum token length
	for calculateThreadTokenLength(chatMessages) > maxTokens && len(chatMessages) > 0 {
		// Remove the oldest message
		chatMessages = chatMessages[1:]
		logger.Log("info", "Thread after truncation: tokens: %d, messages: %v", calculateThreadTokenLength(chatMessages), len(chatMessages))
	}

	logger.Log("info", "\nThread: %d messages, %d tokens", len(chatMessages), calculateThreadTokenLength(chatMessages))
}

// tokenize approximates tokenization by splitting on whitespace and punctuation.
func tokenize(text string) []string {
	re := regexp.MustCompile(`[\w']+|[.,!?;]`)
	tokens := re.FindAllString(text, -1)
	return tokens
}

func llmRequest(cfg *config.Config, prompt string, stream bool) (string, error) {
	truncPrompt := prompt
	if len(truncPrompt) > cfg.MaxTokens*2 {
		truncPrompt = truncPrompt[:cfg.MaxTokens*2] + "..."
	}

	logger.Log("llmIn", "[%s/%s]: %s", cfg.Provider, cfg.Model, truncPrompt)

	// Create a spinner for LLM response
	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Suffix = fmt.Sprintf(" Waiting for %s/%s response...", cfg.Provider, cfg.Model)
	s.Color("green", "bold")

	// Start spinner if not streaming (for streaming, we'll show the output directly)
	if !stream {
		s.Start()
		defer s.Stop()
	}

	var err error
	var answer string
	switch cfg.Provider {
	case "ollama":
		answer, err = ollamaRequestWithChat(cfg, truncPrompt, stream)
	case "openai", "deepseek":
		answer, err = openaiRequestWithChat(cfg, truncPrompt, stream)
	case "google":
		answer, err = googleRequestWithChat(cfg, truncPrompt, stream)
	case "anthropic":
		answer, err = anthropicRequestWithChat(cfg, truncPrompt, stream)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", cfg.Provider)
	}

	logger.Log("llmOut", "[%s@%s]: %s", cfg.Provider, cfg.Model, answer)
	return answer, err
}

// createStreamingCallback creates a callback function for streaming LLM responses with optional Markdown formatting
func createStreamingCallback(cfg *config.Config, spinner *spinner.Spinner) (func(ctx context.Context, chunk []byte) error, func()) {
	var spinnerStopped sync.Once
	var mdWriter *formatter.StreamingWriter
	var animWriter *animator.TypewriterWriter

	// Create writers based on configuration
	var outputWriter io.Writer = os.Stdout

	// Add typewriter animation if enabled
	if !cfg.DisableAnimation {
		animWriter = animator.NewTypewriterWriter(outputWriter)
		outputWriter = animWriter
	}

	// Add markdown formatting if enabled
	if !cfg.DisableMarkdownFormat {
		// Create a streaming writer that formats Markdown
		mdWriter = formatter.NewStreamingWriter(outputWriter)
	}

	// Create cleanup function for the writers
	cleanup := func() {
		if mdWriter != nil {
			if err := mdWriter.Flush(); err != nil {
				logger.Log("err", "Error flushing markdown writer: %v", err)
			}
			if err := mdWriter.Close(); err != nil {
				logger.Log("err", "Error closing markdown writer: %v", err)
			}
		}

		if animWriter != nil {
			if err := animWriter.Flush(); err != nil {
				logger.Log("err", "Error flushing animator writer: %v", err)
			}
			if err := animWriter.Close(); err != nil {
				logger.Log("err", "Error closing animator writer: %v", err)
			}
		}
	}

	// Callback function for processing chunks
	callback := func(ctx context.Context, chunk []byte) error {
		// Stop spinner on first chunk
		spinnerStopped.Do(func() {
			spinner.Stop()
			fmt.Print("\r") // Clear the line
		})

		// Process the chunk with Markdown formatting if enabled
		if !cfg.DisableMarkdownFormat && mdWriter != nil {
			_, err := mdWriter.Write(chunk)
			return err
		}

		// If Markdown is disabled but animation is enabled
		if cfg.DisableMarkdownFormat && !cfg.DisableAnimation && animWriter != nil {
			_, err := animWriter.Write(chunk)
			return err
		}

		// Default: write the chunk directly to stdout
		fmt.Print(string(chunk))
		return nil
	}

	return callback, cleanup
}

func openaiRequestWithChat(cfg *config.Config, prompt string, stream bool) (string, error) {
	ctx := context.Background()

	// Set OpenAI client options
	llmOptions := []openai.Option{
		openai.WithModel(cfg.Model),
	}

	if cfg.Provider == "deepseek" {
		llmOptions = append(llmOptions, openai.WithBaseURL(cfg.ApiURL))
	}

	// Create OpenAI client
	client, err := openai.New(llmOptions...)
	if err != nil {
		return "", fmt.Errorf("failed to create OpenAI client: %w", err)
	}

	// Use the common LLM request handler
	return handleLLMRequest(ctx, cfg, client, prompt, stream, nil)
}

func anthropicRequestWithChat(cfg *config.Config, prompt string, stream bool) (string, error) {
	ctx := context.Background()

	// Create Anthropic client
	client, err := anthropic.New()
	if err != nil {
		return "", fmt.Errorf("failed to create Anthropic client: %w", err)
	}

	// Use the common LLM request handler
	return handleLLMRequest(ctx, cfg, client, prompt, stream, []llms.CallOption{
		llms.WithModel(cfg.Model),
	})
}

func ollamaRequestWithChat(cfg *config.Config, prompt string, stream bool) (string, error) {
	ctx := context.Background()

	// Make sure the API URL is properly formatted - it should not end with /api
	serverURL := cfg.ApiURL
	if strings.HasSuffix(serverURL, "/api") {
		serverURL = strings.TrimSuffix(serverURL, "/api")
	}

	// Create Ollama client
	client, err := ollama.New(
		ollama.WithModel(cfg.Model),
		ollama.WithServerURL(serverURL),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Ollama client: %w", err)
	}

	// Use the common LLM request handler
	return handleLLMRequest(ctx, cfg, client, prompt, stream, nil)
}

func googleRequestWithChat(cfg *config.Config, prompt string, stream bool) (string, error) {
	ctx := context.Background()

	// Create GoogleAI client
	client, err := googleai.New(ctx,
		googleai.WithAPIKey(os.Getenv("GOOGLE_API_KEY")),
		googleai.WithDefaultModel(cfg.Model),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create Google AI client: %w", err)
	}

	// Use the common LLM request handler
	return handleLLMRequest(ctx, cfg, client, prompt, stream, []llms.CallOption{
		llms.WithMaxTokens(cfg.MaxTokens / 2),
	})
}

// handleLLMRequest handles common LLM request logic for all providers
func handleLLMRequest(ctx context.Context, cfg *config.Config, client llms.Model, prompt string, stream bool, baseOptions []llms.CallOption) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Create a human message from the prompt
	humanMessage := llms.HumanChatMessage{Content: prompt}

	// Add to chat history
	cfg.ChatMessages = append(cfg.ChatMessages, humanMessage)

	// Prepare options for generation
	options := baseOptions
	var cleanupFn func()

	if stream {
		// Create a spinner for streaming that stops on first chunk
		s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
		s.Suffix = fmt.Sprintf(" Waiting for %s/%s response...", cfg.Provider, cfg.Model)
		s.Color("green", "bold")
		s.Start()

		// Create streaming callback with markdown formatting support
		callbackFn, cleanup := createStreamingCallback(cfg, s)
		cleanupFn = cleanup
		options = append(options, llms.WithStreamingFunc(callbackFn))
	}

	// Generate response
	response, err := llms.GenerateFromSinglePrompt(ctx, client, prompt, options...)

	// Call cleanup after stream is complete
	if stream && cleanupFn != nil {
		defer cleanupFn()
	}

	if err != nil {
		return "", fmt.Errorf("%s text generation failed: %w", cfg.Provider, err)
	}

	// Add the response to the chat history
	cfg.ChatMessages = append(cfg.ChatMessages, llms.AIChatMessage{Content: response})

	if stream {
		fmt.Println() // Add newline after streaming
	}
	return response, nil
}

// Helper function to check if a prompt exists in chat history
func promptExistsInHistory(messages []llms.ChatMessage, prompt string) bool {
	for _, msg := range messages {
		if strings.Contains(msg.GetContent(), prompt) {
			return true
		}
	}
	return false
}

// getKubectlCmds retrieves kubectl commands based on the user input
func getKubectlCmds(cfg *config.Config, prompt string) ([]string, error) {
	// Generate a context-aware prompt based on user input
	systemPrompt := generateKubectlPrompt(cfg, prompt)

	// Use a shorter prompt for repeated interactions to avoid repetition
	shortPrompt := "As a Kubernetes expert, provide safe, read-only kubectl commands to diagnose the following issue."

	// Check if long prompt exists in the chat history to avoid repetition
	finalPrompt := systemPrompt
	if promptExistsInHistory(cfg.ChatMessages, systemPrompt) {
		finalPrompt = shortPrompt
		logger.Log("info", "Using condensed prompt for context efficiency")
	}

	// Construct the final prompt with clear instructions
	augPrompt := finalPrompt + "\n\nIssue description: " + prompt + "\n\nProvide commands as a plain list without descriptions or backticks."

	// Create a spinner for command generation
	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Suffix = " Generating diagnostic commands..."
	s.Color("blue", "bold")
	s.Start()
	defer s.Stop()

	// Execute LLM request without streaming for diagnostic requests
	response, err := llmRequest(cfg, augPrompt, false)
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
		if !anonCmdRe.MatchString(trimCmd) && !slices.Contains(filteredCmds, trimCmd) {
			filteredCmds = append(filteredCmds, trimCmd)
		}
	}

	logger.Log("info", "Generated kubectl commands: \"%v\"", strings.Join(filteredCmds, ", "))
	return filteredCmds, nil
}

// generateKubectlPrompt generates a context-aware prompt based on the user's query
func generateKubectlPrompt(cfg *config.Config, prompt string) string {
	// Function to create formatted command strings
	createCommand := func(cmd string) string {
		return "kubectl " + cmd
	}

	// Core system prompt with clear role and purpose
	systemPrompt := `You are an expert Kubernetes administrator specializing in cluster diagnostics.

Task: Analyze the user's issue and provide appropriate kubectl commands for diagnostics.

Guidelines:
- Provide only safe, read-only commands that will not modify cluster state
- Commands should be specific and target the exact resources relevant to the issue
- Focus on commands that provide the most useful diagnostic information
- Include namespace flags where appropriate (-n or --all-namespaces/-A)
- Prefer commands that give comprehensive information (e.g., -o wide, --show-labels)
`

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
	systemPrompt += `

Output format:
- Provide only the exact commands to run (no markdown formatting)
- One command per line
- Do not include explanations or descriptions
- Use only actual resource names, not placeholders
- Never include destructive commands that modify cluster state
`

	return systemPrompt
}

func execDiagCmds(cfg *config.Config, commands []string) ([]CmdRes, error) {
	var wg sync.WaitGroup
	results := make([]CmdRes, len(commands))
	startTime := time.Now()

	// Create channels to track progress
	statusChan := make(chan struct {
		index int
		done  bool
		err   error
	}, len(commands))

	// Create a spinner for execution status - using a more visually appealing spinner (dots)
	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Suffix = fmt.Sprintf(" Executing %d commands...", len(commands))
	s.Color("cyan", "bold")
	s.Start()

	// Track command status
	completedCount := 0
	failedCount := 0
	statusMap := make(map[int]bool)
	firstCommandCompleted := false

	// Start a goroutine to update the spinner message
	go func() {
		for status := range statusChan {
			if status.done {
				completedCount++
				statusMap[status.index] = status.err == nil
				if status.err != nil {
					failedCount++
				}
			}

			s.Suffix = fmt.Sprintf(" Executing %d commands... %d/%d completed",
				len(commands), completedCount, len(commands))
		}
	}()

	for i, command := range commands {
		if cfg.SafeMode && !strings.HasPrefix(command, "$") {
			// Stop spinner temporarily to show the confirmation prompt
			s.Stop()
			fmt.Printf("\nExecute '%s' (y/N)?", command)
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			// Restart spinner after confirmation
			if completedCount < len(commands) {
				s.Start()
			}

			if strings.ToLower(input) != "y" {
				fmt.Println("Skipping...")
				statusChan <- struct {
					index int
					done  bool
					err   error
				}{i, true, nil}
				continue
			}
		}

		wg.Add(1)
		go func(idx int, cmd string) {
			defer wg.Done()
			cmdStart := time.Now()
			results[idx] = execKubectlCmd(cfg, cmd)
			cmdDuration := time.Since(cmdStart)

			// Send status update
			statusChan <- struct {
				index int
				done  bool
				err   error
			}{idx, true, results[idx].Err}

			// Print command result immediately with improved formatting
			if !cfg.Verbose {
				checkmark := color.GreenString("✓")
				cmdLabel := color.HiWhiteString("running:")
				if results[idx].Err != nil {
					checkmark = color.RedString("✗")
				}

				if !firstCommandCompleted {
					fmt.Println() // Add newline before first command output
					firstCommandCompleted = true
				}

				fmt.Printf("%s %s %s %s\n",
					checkmark,
					cmdLabel,
					color.CyanString(cmd),
					color.HiBlackString("in %dms", cmdDuration.Milliseconds()))
			}
		}(i, command)
	}

	wg.Wait()
	close(statusChan)
	s.Stop()

	// Print summary with improved formatting
	totalDuration := time.Since(startTime)
	successCount := completedCount - failedCount
	summaryColor := color.HiGreenString
	if failedCount > 0 {
		summaryColor = color.HiYellowString
	}

	fmt.Printf("%s %s %s\n",
		color.GreenString("✓"),
		color.HiWhiteString("Executing %d command(s):", len(commands)),
		summaryColor("%d/%d completed in %s (%d failed)",
			successCount,
			len(commands),
			color.HiBlackString("%dms", totalDuration.Milliseconds()),
			failedCount))

	var err error
	for _, res := range results {
		if !cfg.Verbose {
			logger.Log("in", "$ %s", res.Cmd)
			if res.Out != "" {
				logger.Log("out", res.Out)
			}
		}
		if res.Err != nil {
			if !cfg.Verbose {
				logger.Log("err", "%v", res.Err)
			}
			if err == nil {
				err = res.Err
			} else {
				err = fmt.Errorf("%v; %w", res.Err, err)
			}
		}
	}

	return results, err
}

func execKubectlCmd(cfg *config.Config, command string) (result CmdRes) {
	result.Cmd = command

	if !strings.HasPrefix(command, "kubectl") && !strings.HasPrefix(command, "$") {
		result.Err = fmt.Errorf("invalid command: %s", command)
		return result
	}

	var envBlockedList []string
	var envBlocked = os.Getenv("QU_KUBECTL_BLOCKED_CMDS_EXTRA")
	if envBlocked != "" {
		envBlockedList = strings.Split(envBlocked, ",")
	}

	blacklist := append(cfg.BlockedKubectlCmds, envBlockedList...)

	if strings.HasPrefix(command, "$") {
		command = strings.TrimSpace(strings.TrimPrefix(command, "$"))
	} else {
		for _, cmd := range blacklist {
			if strings.HasPrefix(command, cmd) || strings.Contains(command, " "+cmd+" ") {
				result.Err = fmt.Errorf("command '%s' is not allowed", command)
				return result
			}
		}
	}

	// Use the provided timeout for the command execution
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	logger.Log("info", "Executing command: %s", command)
	output, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
	if err != nil {
		result.Err = fmt.Errorf("error executing command '%s': %w, output: %s", command, err, string(output))
	} else {
		result.Out = string(output)
	}

	// Only print verbose output when requested
	if cfg.Verbose {
		dim := color.New(color.Faint).SprintFunc()
		bold := color.New(color.Bold).SprintFunc()

		fmt.Println(bold("\n$ " + result.Cmd))
		for _, line := range strings.Split(result.Out, "\n") {
			fmt.Println(dim("-- " + line))
		}
	}

	return result
}

// handleCmdCompletion provides completions for various CLI tools like kubectl, helm, and docker
func handleCmdCompletion(cfg *config.Config, words []string, lastWord string) ([][]rune, int) {
	command := words[0]

	// Build the __complete command with all arguments except the last one
	var cmdArgs []string
	if len(words) > 1 {
		cmdArgs = words[1:]
	} else {
		cmdArgs = []string{}
	}

	// Send the last incomplete command as a quoted argument
	completeCmd := fmt.Sprintf("%s __complete %s \"%s\"", command, strings.Join(cmdArgs, " "), lastWord)

	// Execute the completion command
	output, err := exec.Command("sh", "-c", completeCmd).Output()
	// if output start with lastWord plus any space characters, run compleCmd with lastWord as ""
	if strings.HasPrefix(string(output), lastWord) {
		completeCmd = fmt.Sprintf("%s __complete %s %s \"\"", command, strings.Join(cmdArgs, " "), lastWord)
		output, err = exec.Command("sh", "-c", completeCmd).Output()
		lastWord = ""
	}

	// Try fallback methods if direct completion fails
	if err != nil {
		// Try fallback to bash completion
		fallbackCmd := fmt.Sprintf("%s completion bash | bash -c 'COMP_LINE=\"%s %s\" COMP_POINT=%d source /dev/stdin && echo \"${COMPREPLY[@]}\"'",
			command,
			command,
			strings.Join(append(cmdArgs, lastWord), " "),
			len(command+" "+strings.Join(append(cmdArgs, lastWord), " ")))

		output, err = exec.Command("sh", "-c", fallbackCmd).Output()
		if err != nil {
			// Fall back to file completion if both methods fail
			return handleShellCompletion(cfg, lastWord)
		}
	}

	// Parse completions
	completions := [][]rune{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Handle special completion output with :+number to omit from completions
		if strings.Contains(line, ":") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				// Check if the second part starts with a number
				if _, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					// This is a special directive line, skip it
					continue
				}
			}
		}

		// Skip special completions that start with underscore, like _activeHelp_
		if strings.HasPrefix(line, "_") {
			continue
		}

		// Split by tab character if present (for descriptions)
		parts := strings.Split(line, "\t")
		suggestion := parts[0]

		// Skip suggestions that start with underscore
		if strings.HasPrefix(suggestion, "_") {
			continue
		}

		// Only use the part that should be added
		if strings.HasPrefix(suggestion, lastWord) {
			suggestion = suggestion[len(lastWord):]
			if suggestion != "" {
				completions = append(completions, []rune(suggestion))
			}
		} else {
			// If it doesn't start with lastWord, just add it as is
			completions = append(completions, []rune(suggestion))
		}

		// Limit the number of completions
		if len(completions) >= cfg.MaxCompletions {
			break
		}
	}

	return completions, len(lastWord)
}

// handleShellCompletion provides filename completions using shell's compgen
func handleShellCompletion(cfg *config.Config, lastWord string) ([][]rune, int) {
	// If lastWord is empty, show files in current directory
	if lastWord == "" {
		lastWord = "./"
	}

	// Escape any special characters in the lastWord
	escapedLastWord := strings.Replace(lastWord, " ", "\\ ", -1)
	escapedLastWord = strings.Replace(escapedLastWord, "(", "\\(", -1)
	escapedLastWord = strings.Replace(escapedLastWord, ")", "\\)", -1)
	escapedLastWord = strings.Replace(escapedLastWord, "[", "\\[", -1)
	escapedLastWord = strings.Replace(escapedLastWord, "]", "\\]", -1)

	// Use compgen -f to get file completions
	command := fmt.Sprintf("compgen -f -- %s", escapedLastWord)
	output, err := exec.Command("bash", "-c", command).Output()
	if err != nil {
		return [][]rune{}, 0
	}

	// Extract the directory part to properly handle relative paths
	dirPrefix := ""
	if lastIndex := strings.LastIndex(lastWord, "/"); lastIndex != -1 {
		dirPrefix = lastWord[:lastIndex+1]
	}

	// Parse completions
	completions := [][]rune{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip files that start with underscore
		if strings.HasPrefix(line, "_") {
			continue
		}

		// Get full path if needed
		fullPath := line
		if dirPrefix != "" && !strings.HasPrefix(line, "/") {
			// This is a relative path result, so prepend the directory
			fullPath = dirPrefix + line
		}

		// Add trailing slash for directories
		isDir := false
		stat, err := os.Stat(fullPath)
		if err == nil && stat.IsDir() {
			isDir = true
		}

		// Only return the part that should be added after the lastWord
		suffix := ""
		baseFilename := line
		if lastSlash := strings.LastIndex(line, "/"); lastSlash != -1 {
			baseFilename = line[lastSlash+1:]
		}

		if strings.HasPrefix(baseFilename, strings.TrimPrefix(lastWord, dirPrefix)) {
			// For directories, add a trailing slash
			if isDir {
				suffix = "/"
			}

			// Return only the part to be completed
			toAppend := line[len(strings.TrimPrefix(lastWord, dirPrefix)):] + suffix
			if toAppend != "" {
				completions = append(completions, []rune(toAppend))
			}

			// Limit the number of completions
			if len(completions) >= cfg.MaxCompletions {
				break
			}
		}
	}

	return completions, len(lastWord) - len(dirPrefix)
}
