package completer

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/mcp"
)

// execCommand is a variable that allows for mocking exec.Command in tests
var execCommand = exec.Command

// shellAutoCompleter is an implementation of readline.AutoCompleter
type shellAutoCompleter struct {
	Cfg *config.Config
}

// NewShellAutoCompleter creates a new shellAutoCompleter instance
func NewShellAutoCompleter(cfg *config.Config) *shellAutoCompleter {
	return &shellAutoCompleter{
		Cfg: cfg,
	}
}

// Do implements the AutoCompleter interface for tab completion
func (c *shellAutoCompleter) Do(line []rune, pos int) (newLine [][]rune, length int) {
	// Handle potential index out of bounds
	if pos > len(line) {
		pos = len(line)
	}

	lineStr := string(line[:pos])

	// Handle slash commands autocomplete first - works in all modes
	if strings.HasPrefix(lineStr, "/") {
		return c.CompleteSlashCommands(lineStr)
	}

	// Only enable shell completion for command prefix mode or persistent edit mode
	if len(lineStr) == 0 {
		if !c.Cfg.EditMode {
			return [][]rune{}, 0
		}
	} else {
		prefix := c.Cfg.CommandPrefix
		if strings.TrimSpace(prefix) == "" {
			prefix = "$"
		}
		if string(lineStr[0]) != prefix && !c.Cfg.EditMode {
			return [][]rune{}, 0
		}
	}

	// Remove the command prefix when present; in edit mode the prefix is omitted
	if !c.Cfg.EditMode {
		prefix := c.Cfg.CommandPrefix
		if strings.TrimSpace(prefix) == "" {
			prefix = "$"
		}
		lineStr = strings.TrimPrefix(lineStr, prefix)
	}
	lineStr = strings.TrimLeft(lineStr, " ")

	// If empty after prefix, suggest common commands
	if strings.TrimSpace(lineStr) == "" {
		return c.CompleteFirst(lineStr)
	}

	// Parse respecting quotes
	words := ParseCommandLine(lineStr)
	if len(words) == 0 {
		return [][]rune{}, 0
	}

	// Word being completed (may be empty)
	incomplete := ""

	// If line ends with a space, completing a new word
	if len(lineStr) > 0 && lineStr[len(lineStr)-1] == ' ' {
		words = append(words, "")
		incomplete = ""
	} else {
		// Otherwise, complete the last word
		if len(words) > 0 {
			incomplete = words[len(words)-1]
			words = words[:len(words)-1]
		}
	}

	// Determine the command being used
	if len(words) == 0 {
		// Initial command completion
		return c.CompleteFirst(incomplete)
	} else {
		// Command-specific completion
		completions, pos := c.CompleteCli(words, incomplete)
		// Only fallback to shell completion if no completions were found
		if len(completions) == 0 {
			return c.CompleteShell(incomplete)
		}
		return completions, pos
	}
}

// CompleteFirst completes the initial command after $
func (c *shellAutoCompleter) CompleteFirst(prefix string) ([][]rune, int) {
	if prefix == "" {
		return [][]rune{}, 0
	}

	// Prevent command injection by sanitizing the prefix
	sanitizedPrefix := sanitizeInput(prefix)
	if sanitizedPrefix != prefix {
		// Reject if sanitization changed string
		return [][]rune{}, 0
	}

	// Query bash for command completions
	command := fmt.Sprintf("compgen -c %s", sanitizedPrefix)
	output, err := execCommand("bash", "-c", command).Output()
	if err != nil {
		return [][]rune{}, 0
	}

	completions := [][]rune{}
	seen := make(map[string]bool)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}

		if strings.HasPrefix(line, prefix) {
			// Add a space after command names
			completions = append(completions, []rune(line[len(prefix):]+" "))
			seen[line] = true
		}

		// Limit the number of completions
		if len(completions) >= c.Cfg.MaxCompletions {
			break
		}
	}

	return completions, len(prefix)
}

// sanitizeInput prevents command injection in shell commands
func sanitizeInput(input string) string {
	// Remove potentially harmful characters
	re := regexp.MustCompile(`[;&|()<>$\` + "`" + `\\]`)
	return re.ReplaceAllString(input, "")
}

// escapeShellArg properly escapes an argument for shell use
func escapeShellArg(arg string) string {
	// The most reliable way to escape a shell argument is to wrap in single quotes
	// and escape any single quotes inside the argument
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}

// CompleteCli provides completions for various CLI tools like kubectl, helm, and docker
func (c *shellAutoCompleter) CompleteCli(words []string, lastWord string) (completions [][]rune, pos int) {
	if len(words) == 0 {
		return [][]rune{}, 0
	}

	command := words[0]

	// Validate command is safe
	// if !isValidCommand(command) {
	// 	return [][]rune{}, 0
	// }

	// Build the arguments for completion
	cmdArgs := []string{}
	if len(words) > 1 {
		cmdArgs = words[1:]
	}
	if lastWord == "" {
		cmdArgs = cmdArgs[:len(cmdArgs)-1]
	}

	// Safely construct the completion command
	var completeCmd string
	if len(cmdArgs) > 0 {
		// Escape each argument individually
		escapedArgs := make([]string, len(cmdArgs))
		for i, arg := range cmdArgs {
			escapedArgs[i] = escapeShellArg(arg)
		}
		completeCmd = fmt.Sprintf("%s __complete %s %s",
			escapeShellArg(command),
			strings.Join(escapedArgs, " "),
			escapeShellArg(lastWord))
	} else {
		completeCmd = fmt.Sprintf("%s __complete %s",
			escapeShellArg(command),
			escapeShellArg(lastWord))
	}

	// Execute the completion command with explicit shell and arguments
	cmd := execCommand("sh", "-c", completeCmd)
	output, err := cmd.Output()

	if err != nil {
		// Fallback to shell completion on error
		return c.CompleteShell(lastWord)
	}

	// Parse completions
	return c.parseCompletionOutput(string(output), lastWord)
}

// isValidCommand checks if a command is safe to execute
func isValidCommand(cmd string) bool {
	// Only allow alphanumeric characters, dashes, and underscores
	re := regexp.MustCompile(`^[a-zA-Z0-9\-_.]+$`)
	return re.MatchString(cmd)
}

// parseCompletionOutput processes command completion results
func (c *shellAutoCompleter) parseCompletionOutput(output string, lastWord string) ([][]rune, int) {
	completions := [][]rune{}

	for _, line := range strings.Split(output, "\n") {
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
					// Skip special directive line
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
		if len(completions) >= c.Cfg.MaxCompletions {
			break
		}
	}

	return completions, len(lastWord)
}

// escapePathForShell properly escapes a path for safe shell usage
func escapePathForShell(path string) string {
	// List of special characters to escape
	specialChars := []string{" ", "(", ")", "[", "]", "&", ";", "|", "<", ">", "*", "?", "$", "\\", "`", "'", "\""}
	result := path

	for _, char := range specialChars {
		result = strings.Replace(result, char, "\\"+char, -1)
	}

	return result
}

// CompleteShell provides filename completions using shell's compgen
func (c *shellAutoCompleter) CompleteShell(lastWord string) ([][]rune, int) {
	// If lastWord is empty, show files in current directory
	if lastWord == "" {
		lastWord = "./"
	}

	// Escape lastWord for shell
	escapedLastWord := escapePathForShell(lastWord)

	// Use compgen -f for file completions
	command := fmt.Sprintf("compgen -f -- %s", escapedLastWord)
	output, err := execCommand("bash", "-c", command).Output()
	if err != nil {
		return [][]rune{}, 0
	}

	// Extract directory part for relative paths
	dirPrefix := ""
	if lastIndex := strings.LastIndex(lastWord, "/"); lastIndex != -1 {
		dirPrefix = lastWord[:lastIndex+1]
	}

	return c.processFileCompletions(string(output), lastWord, dirPrefix)
}

// processFileCompletions processes file completion results
func (c *shellAutoCompleter) processFileCompletions(output string, lastWord string, dirPrefix string) ([][]rune, int) {
	completions := [][]rune{}

	for _, line := range strings.Split(output, "\n") {
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
			// Prepend directory for relative paths
			fullPath = dirPrefix + line
		}

		// Add trailing slash for directories
		isDir := false
		stat, err := os.Stat(fullPath)
		if err == nil && stat.IsDir() {
			isDir = true
		}

		// Only return the part to add after lastWord
		suffix := ""
		baseFilename := line
		if lastSlash := strings.LastIndex(line, "/"); lastSlash != -1 {
			baseFilename = line[lastSlash+1:]
		}

		pathToCompare := strings.TrimPrefix(lastWord, dirPrefix)
		if strings.HasPrefix(baseFilename, pathToCompare) {
			// For directories, add trailing slash
			if isDir {
				suffix = "/"
			}

			// Return only the completion suffix
			toAppend := line[len(pathToCompare):] + suffix
			if toAppend != "" {
				completions = append(completions, []rune(toAppend))
			}

			// Limit the number of completions
			if len(completions) >= c.Cfg.MaxCompletions {
				break
			}
		}
	}

	return completions, len(lastWord) - len(dirPrefix)
}

// ParseCommandLine splits a command line into tokens, respecting quotes
func ParseCommandLine(line string) []string {
	var tokens []string
	var current strings.Builder
	var inSingleQuotes bool
	var inDoubleQuotes bool
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

		if char == '\'' && !inDoubleQuotes {
			inSingleQuotes = !inSingleQuotes
			continue
		}

		if char == '"' && !inSingleQuotes {
			inDoubleQuotes = !inDoubleQuotes
			continue
		}

		if char == ' ' && !inSingleQuotes && !inDoubleQuotes {
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

// CompleteSlashCommands provides autocomplete for slash commands
func (c *shellAutoCompleter) CompleteSlashCommands(input string) ([][]rune, int) {
	// Use slash commands from configuration
	slashCommands := c.Cfg.SlashCommands
	var promptInfos []mcp.PromptInfo
	if c.Cfg.MCPClientEnabled {
		promptInfos = mcp.GetPromptInfos(c.Cfg)
	}

	if input == "/" {
		// Show primary commands when just "/" is typed for cleaner menu
		var completions [][]rune
		seen := make(map[string]bool)
		for _, cmdInfo := range slashCommands {
			remaining := cmdInfo.Primary[1:] // Remove the leading "/"
			if !seen[remaining] {
				completions = append(completions, []rune(remaining))
				seen[remaining] = true
			}
		}
		// Show MCP server names as completion options
		for _, pi := range promptInfos {
			server := strings.TrimSpace(pi.Server)
			if server == "" {
				continue
			}
			if seen[server] {
				continue
			}
			completions = append(completions, []rune(server+"/"))
			seen[server] = true
			if len(completions) >= c.Cfg.MaxCompletions {
				break
			}
		}
		return completions, 1
	}

	// Filter commands that match the input (check all variations)
	var completions [][]rune
	seen := make(map[string]bool)

	for _, cmdInfo := range slashCommands {
		for _, cmd := range cmdInfo.Commands {
			if strings.HasPrefix(cmd, input) {
				// Return the remaining part of the command
				remaining := cmd[len(input):]
				if remaining != "" && !seen[remaining] {
					completions = append(completions, []rune(remaining))
					seen[remaining] = true
				}
			}
		}
	}

	// Include MCP prompts in format /$server/$prompt with space suffix for user query
	for _, pi := range promptInfos {
		server := strings.TrimSpace(pi.Server)
		name := strings.TrimSpace(pi.Name)
		if server == "" || name == "" {
			continue
		}
		// Format: /$server/$prompt
		promptCmd := "/" + server + "/" + name
		if strings.HasPrefix(promptCmd, input) {
			remaining := promptCmd[len(input):]
			if remaining != "" && !seen[remaining] {
				// Add space after prompt name to allow user to type query
				completions = append(completions, []rune(remaining+" "))
				seen[remaining] = true
			}
		}
		if len(completions) >= c.Cfg.MaxCompletions {
			break
		}
	}

	return completions, len(input)
}

// IsMCPPrompt checks if the input starts with an MCP prompt in format /$server/$prompt
// Returns the prompt name, server name, and remaining text if found
func IsMCPPrompt(cfg *config.Config, input string) (promptName string, userQuery string, isPrompt bool) {
	if !cfg.MCPClientEnabled || !strings.HasPrefix(input, "/") {
		return "", input, false
	}

	// Extract potential server/prompt path (format: /$server/$prompt query)
	trimmed := strings.TrimPrefix(input, "/")

	// Split by space first to separate the prompt path from the query
	pathAndQuery := strings.SplitN(trimmed, " ", 2)
	if len(pathAndQuery) == 0 {
		return "", input, false
	}

	promptPath := pathAndQuery[0]
	if promptPath == "" {
		return "", input, false
	}

	// Split the path by "/" to get server and prompt name
	pathParts := strings.SplitN(promptPath, "/", 2)
	if len(pathParts) != 2 {
		return "", input, false
	}

	serverName := pathParts[0]
	candidateName := pathParts[1]
	if serverName == "" || candidateName == "" {
		return "", input, false
	}

	// Check if it matches an MCP prompt from the specified server
	promptInfos := mcp.GetPromptInfos(cfg)
	for _, pi := range promptInfos {
		if strings.EqualFold(pi.Server, serverName) && strings.EqualFold(pi.Name, candidateName) {
			userQuery = ""
			if len(pathAndQuery) > 1 {
				userQuery = strings.TrimSpace(pathAndQuery[1])
			}
			return pi.Name, userQuery, true
		}
	}

	return "", input, false
}

// GetMCPPromptServer extracts the server name from an MCP prompt input
// Format: /$server/$prompt query
func GetMCPPromptServer(cfg *config.Config, input string) string {
	if !cfg.MCPClientEnabled || !strings.HasPrefix(input, "/") {
		return ""
	}

	trimmed := strings.TrimPrefix(input, "/")
	pathAndQuery := strings.SplitN(trimmed, " ", 2)
	if len(pathAndQuery) == 0 {
		return ""
	}

	pathParts := strings.SplitN(pathAndQuery[0], "/", 2)
	if len(pathParts) < 1 {
		return ""
	}

	return pathParts[0]
}
