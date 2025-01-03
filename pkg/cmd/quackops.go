package cmd

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/google/generative-ai-go/genai"
	"github.com/henomis/lingoose/llm/anthropic"
	"github.com/henomis/lingoose/llm/ollama"
	"github.com/henomis/lingoose/llm/openai"
	"github.com/henomis/lingoose/thread"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
	"github.com/spf13/cobra"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
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

	cmd.Flags().StringVarP(&cfg.Provider, "provider", "p", cfg.Provider, "LLM model provider (e.g., 'ollama', 'openai', 'google', anthropic)")
	cmd.Flags().StringVarP(&cfg.Model, "model", "m", cfg.Model, "LLM model to use")
	cmd.Flags().StringVarP(&cfg.ApiURL, "api-url", "u", cfg.ApiURL, "URL for LLM API, used with 'ollama' provider")
	cmd.Flags().BoolVarP(&cfg.SafeMode, "safe-mode", "s", cfg.SafeMode, "Enable safe mode to prevent executing commands without confirmation")
	cmd.Flags().IntVarP(&cfg.Retries, "retries", "r", cfg.Retries, "Number of retries for kubectl commands")
	cmd.Flags().IntVarP(&cfg.Timeout, "timeout", "t", cfg.Timeout, "Timeout for kubectl commands in seconds")
	cmd.Flags().IntVarP(&cfg.MaxTokens, "max-tokens", "x", cfg.MaxTokens, "Maximum number of tokens in LLM context window")
	cmd.Flags().BoolVarP(&cfg.Verbose, "verbose", "v", cfg.Verbose, "Enable verbose output")
	cmd.Flags().BoolVarP(&cfg.DisableSecretFilter, "disable-secrets-filter", "c", cfg.DisableSecretFilter, "Disable filtering sensitive data in secrets from being sent to LLMs")

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

func processCommands(cfg *config.Config, args []string) error {
	if len(args) > 0 {
		userPrompt := strings.TrimSpace(args[0])
		if userPrompt != "" {
			return processUserPrompt(cfg, userPrompt, "", 1)
		}
	}

	rl, err := readline.New("> ")
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

	manageChatThreadContext(cfg.ChatThread, cfg.MaxTokens)
	return nil
}

// retrieveRAG retrieves the data for RAG
func retrieveRAG(cfg *config.Config, prompt string, lastTextPrompt string) (augPrompt string, err error) {
	var cmdResults []CmdRes
	if strings.HasPrefix(prompt, "$") {
		cmdResults, err = execDiagCmds(cfg, []string{prompt})
	} else {
		// retrieving kubectl commands and executing them
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

	augRes := ""
	for _, cmd := range cmdResults {
		if cmd.Err != nil {
			continue
		}

		if !cfg.DisableSecretFilter {
			cmd.Out = filterSensitiveData(cmd.Out)
		}

		ctx := "Command: " + cmd.Cmd + "\nOutput: " + cmd.Out + "\n\n"
		// TODO: it seems LLM could handle long context better than just truncating it
		// truncate the context if it exceeds the maximum token length
		if len(tokenize(ctx)) > cfg.MaxTokens*2 {
			ctx = ctx[:cfg.MaxTokens*2] + "..."
		}
		augRes += ctx
	}

	userPrompt := prompt
	if strings.HasPrefix(prompt, "$") {
		userPrompt = lastTextPrompt
	}

	format := ""
	if cfg.Provider == "google" {
		format = "Format output for text terminals and do not use Markdown."
	}

	if len(augRes) > 0 {
		augPrompt = fmt.Sprintf("Here are the commands and outputs:\n%s\n###\nYou are an experienced Kubernetes administrator.\nYour task is: '%s'.\n%s", augRes, userPrompt, format)
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
func manageChatThreadContext(chatThread *thread.Thread, maxTokens int) {
	if chatThread == nil {
		return
	}

	// If the token length exceeds the context window, remove the oldest message in loop
	calculateThreadTokenLength := func(chatThread *thread.Thread) int {
		threadLen := 0
		for _, message := range chatThread.Messages {
			for _, content := range message.Contents {
				if content.Type == thread.ContentTypeText {
					tokens := tokenize(content.Data.(string))
					threadLen += len(tokens)
				}
			}
		}
		return threadLen
	}

	threadLen := calculateThreadTokenLength(chatThread)
	if threadLen > maxTokens {
		logger.Log("warn", "Thread should be truncated: %d messages, %d tokens", chatThread.CountMessages(), threadLen)
	}

	// Truncate the thread if it exceeds the maximum token length
	for calculateThreadTokenLength(chatThread) > maxTokens && len(chatThread.Messages) > 0 {
		// Remove the oldest message
		chatThread.Messages = chatThread.Messages[1:]
		logger.Log("info", "Thread after truncation: tokens: %d, messages: %v", calculateThreadTokenLength(chatThread), chatThread.CountMessages())
	}

	logger.Log("info", "\nThread: %d messages, %d tokens", chatThread.CountMessages(), calculateThreadTokenLength(chatThread))
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

	var err error
	var answer string
	switch cfg.Provider {
	case "ollama":
		answer, err = ollamaRequestWithThread(cfg, truncPrompt, stream)
	case "openai":
		answer, err = openaiRequestWithThread(cfg, truncPrompt, stream)
	case "google":
		answer, err = googleRequestWithThread(cfg, truncPrompt, stream)
	case "anthropic":
		answer, err = anthropicRequestWithThread(cfg, truncPrompt, stream)
	default:
		return "", fmt.Errorf("unsupported AI provider: %s", cfg.Provider)
	}

	logger.Log("llmOut", "[%s@%s]: %s", cfg.Provider, cfg.Model, answer)
	return answer, err
}

func openaiRequestWithThread(cfg *config.Config, prompt string, stream bool) (string, error) {
	client := openai.New().
		WithModel(openai.Model(cfg.Model))

	if stream {
		client = client.WithStream(true, func(s string) {
			fmt.Print(s)
		})
	}

	cfg.ChatThread.AddMessage(thread.NewUserMessage().AddContent(thread.NewTextContent(prompt)))

	err := client.Generate(context.Background(), cfg.ChatThread)
	if err != nil {
		return "", fmt.Errorf("openai text generation failed: %w", err)
	}

	msg := ""
	for _, content := range cfg.ChatThread.LastMessage().Contents {
		if content.Type == thread.ContentTypeText {
			msg += content.Data.(string)
		} else {
			return "", errors.New("invalid openai message type")
		}
	}
	if stream {
		fmt.Println() // Add newline after streaming
	}
	return msg, nil
}

func anthropicRequestWithThread(cfg *config.Config, prompt string, stream bool) (string, error) {
	client := anthropic.New().
		WithModel(cfg.Model)

	if stream {
		client = client.WithStream(func(s string) {
			if s != anthropic.EOS {
				fmt.Print(s)
			} else {
				fmt.Println()
			}
		})
	}

	// Create a message and add it to the thread
	cfg.ChatThread.AddMessage(thread.NewUserMessage().AddContent(thread.NewTextContent(prompt)))

	err := client.Generate(context.Background(), cfg.ChatThread)
	if err != nil {
		return "", fmt.Errorf("anthropic text generation failed: %w", err)
	}

	var response string
	lastMessage := cfg.ChatThread.LastMessage()
	if lastMessage != nil && len(lastMessage.Contents) > 0 {
		for _, content := range lastMessage.Contents {
			if content.Type == thread.ContentTypeText {
				response += content.Data.(string)
			} else {
				return "", errors.New("invalid anthropic message type")
			}
		}
	}

	return response, nil
}

func ollamaRequestWithThread(cfg *config.Config, prompt string, stream bool) (string, error) {
	client := ollama.New().
		WithModel(cfg.Model).
		WithEndpoint(cfg.ApiURL)

	if stream {
		client = client.WithStream(func(s string) {
			fmt.Print(s)
		})
	}

	cfg.ChatThread.AddMessage(thread.NewUserMessage().AddContent(thread.NewTextContent(prompt)))

	err := client.Generate(context.Background(), cfg.ChatThread)
	if err != nil {
		return "", fmt.Errorf("ollama text generation failed: %w", err)
	}

	msg := ""
	for _, content := range cfg.ChatThread.LastMessage().Contents {
		if content.Type == thread.ContentTypeText {
			msg += content.Data.(string)
		} else {
			return "", errors.New("invalid ollama message type")
		}
	}
	if stream {
		fmt.Println() // Add newline after streaming
	}
	return msg, nil
}

func googleRequestWithThread(cfg *config.Config, prompt string, stream bool) (string, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GOOGLE_API_KEY")))
	if err != nil {
		return "", fmt.Errorf("failed to create Google GenAI client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel(cfg.Model)
	cs := model.StartChat()

	// Add the user message to the thread before making the request
	cfg.ChatThread.AddMessage(thread.NewUserMessage().AddContent(thread.NewTextContent(prompt)))

	if stream {
		iter := cs.SendMessageStream(ctx, genai.Text(prompt))
		var response string
		for {
			resp, err := iter.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				return "", fmt.Errorf("failed to get response: %w", err)
			}
			if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
				continue
			}
			chunk := resp.Candidates[0].Content.Parts[0].(genai.Text)
			fmt.Print(string(chunk))
			response += string(chunk)
		}
		fmt.Println() // Add newline after streaming

		// Add the assistant's response to the thread
		if response != "" {
			cfg.ChatThread.AddMessage(thread.NewAssistantMessage().AddContent(thread.NewTextContent(response)))
		}
		return response, nil
	}

	// Non-streaming response
	resp, err := cs.SendMessage(ctx, genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("failed to get response: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no response from model")
	}

	response := string(resp.Candidates[0].Content.Parts[0].(genai.Text))

	// Add the assistant's response to the thread
	if response != "" {
		cfg.ChatThread.AddMessage(thread.NewAssistantMessage().AddContent(thread.NewTextContent(response)))
	}
	return response, nil
}

// getKubectlCmds retrieves kubectl commands based on the user input
func getKubectlCmds(cfg *config.Config, prompt string) ([]string, error) {
	dynamicPrompt := generateKubectlPrompt(cfg, prompt)

	shortPrompt := "You are Kubernetes administrator. List safe, " +
		"read-only `kubectl` commands that can help monitor or diagnose the Kubernetes cluster."

	// Check if longPrompt exists in the chatThread history
	augPrompt := dynamicPrompt
	if strings.Contains(cfg.ChatThread.String(), dynamicPrompt) {
		augPrompt = shortPrompt
		logger.Log("info", "Using short prompt")
	}
	augPrompt += "\nProblem description: " + prompt

	// Don't stream diagnostic requests
	response, err := llmRequest(cfg, augPrompt, false)
	if err != nil {
		return nil, fmt.Errorf("error requesting kubectl command: %w", err)
	}

	// Join the allowed commands into a regex pattern
	commandsPattern := strings.Join(cfg.AllowedKubectlCmds, "|")
	rePattern := `kubectl\s(?:` + commandsPattern + `)\s?[^` + "`" + `%#\n]*`
	re := regexp.MustCompile(rePattern)

	matches := re.FindAllString(response, -1)
	if matches == nil {
		return nil, errors.New("no valid kubectl commands found")
	}

	anonCmdRe, _ := regexp.Compile(`.*<[A-Za-z_-]+>.*`)
	var filteredCmds []string
	for _, match := range matches {
		trimCmd := strings.TrimSpace(match)
		// check and remove commands with <resource> <name> format
		if !anonCmdRe.MatchString(trimCmd) && !slices.Contains(filteredCmds, trimCmd) {
			filteredCmds = append(filteredCmds, trimCmd)
		}
	}

	logger.Log("info", "Kubectl commands: \"%v\"", strings.Join(filteredCmds, ", "))
	return filteredCmds, nil
}

// generateKubectlPrompt generates a dynamic prompt based on the user input
func generateKubectlPrompt(cfg *config.Config, prompt string) string {
	// Function to create command strings prefixed with "kubectl"
	createCommand := func(cmd string) string {
		return "kubectl " + cmd
	}

	basePrompt := "You're experienced Kubernetes administrator. List safe read-only `kubectl` commands that can help monitor or diagnose the Kubernetes cluster."

	// Add dynamic content based on the analysis of the prompt
	p := strings.ToLower(prompt)
	useDefaultCmds := true

	for _, kp := range cfg.KubectlPrompts {
		if kp.MatchRe.MatchString(p) {
			basePrompt += kp.Prompt
			if !kp.UseDefaultCmds {
				kubectlCmds := make([]string, len(kp.AllowedKubectls))
				for i, cmd := range kp.AllowedKubectls {
					kubectlCmds[i] = createCommand(cmd)
				}
				basePrompt += " like: " + strings.Join(kubectlCmds, ", ")
				useDefaultCmds = kp.UseDefaultCmds
			}
		}
	}

	if useDefaultCmds {
		var defaultKubectlCmds []string
		for _, cmd := range cfg.AllowedKubectlCmds {
			defaultKubectlCmds = append(defaultKubectlCmds, createCommand(cmd))
		}
		basePrompt += "\nExamples: " + strings.Join(defaultKubectlCmds, ", ") + "."
	}

	// Mention that commands should be formatted properly and non-destructive
	enhancedPrompt := basePrompt + "\nEnsure commands are formatted on separate lines without any descriptions. " +
		"Use real resource names. and avoid commands that modify the cluster. "

	return enhancedPrompt
}

func execDiagCmds(cfg *config.Config, commands []string) ([]CmdRes, error) {
	var wg sync.WaitGroup
	results := make([]CmdRes, len(commands))

	for i, command := range commands {
		if cfg.SafeMode && !strings.HasPrefix(command, "$") {
			fmt.Printf("\nExecute '%s' (y/N)?", command)
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(input)

			if strings.ToLower(input) != "y" {
				fmt.Println("Skipping...")
				continue
			}
		}

		wg.Add(1)
		go func(idx int, cmd string) {
			defer wg.Done()
			results[idx] = execKubectlCmd(cfg, cmd)
		}(i, command)
	}

	wg.Wait()

	var err error
	for _, res := range results {
		logger.Log("in", "$ %s", res.Cmd)
		if res.Out != "" {
			logger.Log("out", res.Out)
		}
		if res.Err != nil {
			logger.Log("err", "%v", res.Err)
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
