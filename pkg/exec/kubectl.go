package exec

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

// cmdStatus represents the status of a command execution
type cmdStatus struct {
	index   int
	done    bool
	err     error
	skipped bool
}

// ExecDiagCmds executes diagnostic commands and returns their results
func ExecDiagCmds(cfg *config.Config, commands []string) ([]config.CmdRes, error) {
	logger.Log("info", "ExecDiagCmds: %d command(s)", len(commands))

	// In strict MCP mode, only allow explicit user commands (those starting with the prefix).
	// Otherwise, accept the provided command list as-is (includes LLM-generated kubectl).
	if cfg.MCPClientEnabled && cfg.MCPStrict {
		filtered := make([]string, 0, len(commands))
		skippedCount := 0
		for _, c := range commands {
			cTrim := strings.TrimSpace(c)
			prefix := "!"
			if strings.TrimSpace(cfg.CommandPrefix) != "" {
				prefix = cfg.CommandPrefix
			}
			if strings.HasPrefix(cTrim, prefix) {
				filtered = append(filtered, c)
			} else {
				skippedCount++
			}
		}
		if skippedCount > 0 {
			logger.Log("warn", "Strict MCP mode: skipping %d non-$ command(s)", skippedCount)
		}
		commands = filtered
	}

	// If no commands remain after filtering, return early.
	if len(commands) == 0 {
		return []config.CmdRes{}, nil
	}

	// Track start time and preallocate results
	startTime := time.Now()
	results := make([]config.CmdRes, len(commands))

	// Simple counters for status tracking
	completed, failed, skipped := 0, 0, 0
	statusMap := make(map[int]bool)

	// Status channel to collect execution updates
	statusChan := make(chan cmdStatus, len(commands))

	// Spinner for progress feedback using SpinnerManager
	spinnerManager := lib.GetSpinnerManager(cfg)
	cancelSpinner := spinnerManager.ShowDiagnostic(fmt.Sprintf("Executing %s %s...", config.Colors.Info.Sprint(fmt.Sprintf("%d", len(commands))), "kubectl commands"))
	defer cancelSpinner()

	// Briefly show the kubectl diagnostics plan as a bullet list (left bullets)
	// to make execution trace transparent. Only list kubectl commands.
	spinnerManager.Hide()
	printKubectlDiagnosticsList(cfg, commands)

	// In safe mode, ask once for all commands using single-key confirmation (ESC=no)
	var proceedAll bool = true
	if cfg.SafeMode {
		spinnerManager.Hide()
		key := lib.ReadSingleKey(fmt.Sprintf("Proceed with executing %d command(s) (y/N/edit)? ", len(commands)))
		switch key {
		case 'y':
			proceedAll = true
		case 'e':
			// Allow the user to quickly toggle which commands will run
			commands = editCommandSelection(cfg, commands)
			if len(commands) == 0 {
				proceedAll = false
			}
		default:
			// includes 'n', Enter, ESC, anything else
			proceedAll = false
		}
	}

	// Start status monitoring goroutine and ensure we wait for it to finish
	var statusWG sync.WaitGroup
	statusWG.Add(1)
	go func() {
		defer statusWG.Done()
		for status := range statusChan {
			if status.done {
				if status.skipped {
					skipped++
				} else {
					completed++
					statusMap[status.index] = status.err == nil
					if status.err != nil {
						failed++
					}
				}

				spinnerManager.Update(fmt.Sprintf("⚡ Executing %s %s... %s completed",
					config.Colors.Info.Sprint(fmt.Sprintf("%d", len(commands))), config.Colors.Dim.Sprint("kubectl commands"), config.Colors.Ok.Sprint(fmt.Sprintf("%d/%d", completed, len(commands)))))
			}
		}
	}()

	// Execute commands based on mode
	firstCommandCompletedMutex := &sync.Mutex{}
	firstCommandCompleted := false

	if cfg.SafeMode {
		// In safe mode, we already asked once; execute sequentially without per-command prompts
		if !proceedAll {
			// Mark all as skipped and return early
			for i := range commands {
				statusChan <- cmdStatus{i, true, nil, true}
			}
		} else {
			executeCommandsSequentially(cfg, commands, results, statusChan, &firstCommandCompleted, firstCommandCompletedMutex, spinnerManager, false)
		}
	} else {
		executeCommandsParallel(cfg, commands, results, statusChan, &firstCommandCompleted, firstCommandCompletedMutex)
	}

	// Close the status channel and wait for status updates to finish processing
	close(statusChan)
	statusWG.Wait()

	// Print execution summary
	statusData := struct {
		completedCount int
		failedCount    int
		skippedCount   int
		statusMap      map[int]bool
	}{
		completedCount: completed,
		failedCount:    failed,
		skippedCount:   skipped,
		statusMap:      statusMap,
	}
	printExecutionSummary(commands, &statusData, startTime)

	// Process execution results and collect errors
	err := processResults(cfg, results)

	return results, err
}

// executeCommandsSequentially runs commands one by one with confirmation in safe mode
func executeCommandsSequentially(
	cfg *config.Config,
	commands []string,
	results []config.CmdRes,
	statusChan chan<- cmdStatus,
	firstCommandCompleted *bool,
	firstCommandCompletedMutex *sync.Mutex,
	spinnerManager *lib.SpinnerManager,
	askPerCommand bool,
) {
	for i, command := range commands {
		// In safe mode, optionally ask for per-command confirmation
		if askPerCommand {
			if !promptForCommandConfirmation(command, i, statusChan, spinnerManager) {
				// Skip this command if not confirmed
				continue
			}
		}

		// Execute the command
		cmdStart := time.Now()
		results[i] = ExecKubectlCmd(cfg, command)
		cmdDuration := time.Since(cmdStart)

		// Send status update
		statusChan <- cmdStatus{i, true, results[i].Err, false}

		// Print command result
		printCommandResult(command, results[i].Err, cmdDuration, firstCommandCompleted, firstCommandCompletedMutex)

		// Render diagnostic result if not in verbose mode and not a shell command
		if !cfg.Verbose && !strings.HasPrefix(strings.TrimSpace(command), cfg.CommandPrefix) {
			renderDiagnosticResult(cfg, command, results[i].Out, results[i].Err)
		}

		if results[i].Err != nil {
			logger.Log("warn", "Command failed: %s", command)
		}
	}
}

// promptForCommandConfirmation asks for user confirmation in safe mode
func promptForCommandConfirmation(command string, index int, statusChan chan<- cmdStatus, spinnerManager *lib.SpinnerManager) bool {
	// Stop spinner to show the confirmation prompt
	spinnerManager.Hide()
	fmt.Printf("\nExecute '%s' (y/N)? ", command)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	lower := strings.ToLower(input)
	// Treat ESC anywhere in the input as a "no" (requires Enter in canonical mode)
	escPressed := strings.Contains(lower, "\x1b")
	isYes := lower == "y" || lower == "yes"

	if !isYes || escPressed {
		fmt.Println("Skipping...")
		statusChan <- cmdStatus{index, true, nil, true}

		// Only restart spinner if there are more commands to process
		if index < cap(statusChan)-1 {
			spinnerManager.ShowDiagnostic(config.Colors.Info.Sprint("Executing kubectl commands..."))
		}
		return false
	}

	// Restart spinner for command execution
	spinnerManager.ShowDiagnostic(config.Colors.Info.Sprint("Executing kubectl commands..."))
	return true
}

// printKubectlDiagnosticsList prints a bullet list of kubectl commands
// so the user can see exactly what diagnostic steps will run.
func printKubectlDiagnosticsList(cfg *config.Config, commands []string) {
	if len(commands) == 0 {
		return
	}

	// Determine command prefix used for shell commands
	prefix := cfg.CommandPrefix
	if strings.TrimSpace(prefix) == "" {
		prefix = "!"
	}

	// Skip suggestions when user is in command/edit mode
	allPrefixed := true
	for _, c := range commands {
		if !strings.HasPrefix(strings.TrimSpace(c), prefix) {
			allPrefixed = false
			break
		}
	}
	if cfg.EditMode || allPrefixed {
		return
	}

	// Collect kubectl-only commands (strip shell prefix first)
	var kubectlCmds []string
	for _, c := range commands {
		cTrim := strings.TrimSpace(c)
		if strings.HasPrefix(cTrim, prefix) {
			cTrim = strings.TrimSpace(strings.TrimPrefix(cTrim, prefix))
		}
		if strings.HasPrefix(cTrim, "kubectl") {
			kubectlCmds = append(kubectlCmds, cTrim)
		}
	}

	if len(kubectlCmds) == 0 {
		return
	}

	// Render using simple Markdown bullets to leverage left-bullet styling downstream
	fmt.Println()
	fmt.Println(config.Colors.Bold.Sprint("Suggested kubectl commands for diagnostics:"))
	for _, kc := range kubectlCmds {
		fmt.Printf("- %s\n", kc)
	}
}

// editCommandSelection lets the user select a subset of commands to run.
// Minimal multi-select: prints each command with an index and [x]/[ ] checkbox.
// Keys: j/k to move, space to toggle, a to toggle all, enter to accept, ESC to cancel.
func editCommandSelection(cfg *config.Config, commands []string) []string {
	if len(commands) == 0 {
		return commands
	}

	// Prepare display list by stripping shell prefix and keeping only kubectl and prefixed shell commands
	prefix := cfg.CommandPrefix
	if strings.TrimSpace(prefix) == "" {
		prefix = "!"
	}

	// Items to display, and mapping back to original indices
	type item struct {
		idx int
		cmd string
	}
	var items []item
	for i, c := range commands {
		cTrim := strings.TrimSpace(c)
		if strings.HasPrefix(cTrim, prefix) {
			// Keep user shell commands as-is (after prefix) for selection too
			show := strings.TrimSpace(strings.TrimPrefix(cTrim, prefix))
			items = append(items, item{idx: i, cmd: show})
			continue
		}
		if strings.HasPrefix(cTrim, "kubectl") {
			items = append(items, item{idx: i, cmd: cTrim})
		}
	}
	if len(items) == 0 {
		return nil
	}

	selected := make([]bool, len(items))
	for i := range selected {
		selected[i] = true
	}

	cursor := 0

	printedLines := 0
	clearScreen := func() {
		if printedLines > 0 {
			for i := 0; i < printedLines; i++ {
				fmt.Print("\033[1A\033[2K")
			}
		}
		printedLines = 0
	}

	redraw := func() {
		clearScreen()
		header := config.Colors.Bold.Sprint("Select commands (Up/Down move, space toggle, a all, Enter accept, ESC cancel, Ctrl-C cancel):")
		lines := make([]string, 0, len(items)+1)
		lines = append(lines, header)
		for i, it := range items {
			box := "[ ]"
			if selected[i] {
				box = "[x]"
			}
			pointer := "  "
			if i == cursor {
				pointer = config.Colors.Command.Sprint("➤ ")
			}
			lines = append(lines, fmt.Sprintf("%s- %s %s", pointer, box, it.cmd))
		}

		for _, ln := range lines {
			fmt.Println(ln)
		}
		printedLines = len(lines)
	}

	redraw()

	// Raw key loop using ReadKey (supports arrows)
	for {
		key := lib.ReadKey("")
		// ctrl-c in raw mode returns byte 3
		if len(key) == 1 && key[0] == 3 {
			clearScreen()
			return nil
		}
		switch key {
		case "esc":
			clearScreen()
			return nil
		case "enter":
			clearScreen()
			var out []string
			// Build final command list preserving original strings
			for i, it := range items {
				if selected[i] {
					out = append(out, commands[it.idx])
				}
			}
			return out
		case "down":
			if cursor < len(items)-1 {
				cursor++
				redraw()
			}
		case "up":
			if cursor > 0 {
				cursor--
				redraw()
			}
		case "a":
			// Toggle all
			all := true
			for _, s := range selected {
				if !s {
					all = false
					break
				}
			}
			for i := range selected {
				selected[i] = !all
			}
			redraw()
		case "space":
			selected[cursor] = !selected[cursor]
			redraw()
		default:
			// ignore other keys
		}
	}
}

// executeCommandsParallel runs commands concurrently in non-safe mode
func executeCommandsParallel(
	cfg *config.Config,
	commands []string,
	results []config.CmdRes,
	statusChan chan<- cmdStatus,
	firstCommandCompleted *bool,
	firstCommandCompletedMutex *sync.Mutex,
) {
	var wg sync.WaitGroup
	for i, command := range commands {
		wg.Add(1)
		go func(idx int, cmd string) {
			defer wg.Done()

			cmdStart := time.Now()
			results[idx] = ExecKubectlCmd(cfg, cmd)
			cmdDuration := time.Since(cmdStart)

			// Send status update
			statusChan <- cmdStatus{idx, true, results[idx].Err, false}

			// Print command result
			printCommandResult(cmd, results[idx].Err, cmdDuration, firstCommandCompleted, firstCommandCompletedMutex)

			// Render diagnostic result if not in verbose mode and not a shell command
			if !cfg.Verbose && !strings.HasPrefix(strings.TrimSpace(cmd), cfg.CommandPrefix) {
				renderDiagnosticResult(cfg, cmd, results[idx].Out, results[idx].Err)
			}

			if results[idx].Err != nil {
				logger.Log("warn", "Command failed: %s", cmd)
			}
		}(i, command)
	}

	wg.Wait()
}

// printCommandResult formats and prints the result of a command execution with enhanced styling
func printCommandResult(
	command string,
	cmdErr error,
	cmdDuration time.Duration,
	firstCommandCompleted *bool,
	firstCommandCompletedMutex *sync.Mutex,
) {
	checkmark := config.Colors.Ok.Sprint("✓")
	cmdLabel := config.Colors.Info.Sprint("running:")
	if cmdErr != nil {
		checkmark = config.Colors.Error.Sprint("✗")
	}

	firstCommandCompletedMutex.Lock()
	isFirstCommand := !(*firstCommandCompleted)
	if isFirstCommand {
		*firstCommandCompleted = true
	}
	firstCommandCompletedMutex.Unlock()

	if isFirstCommand {
		fmt.Println() // Add newline before first command output
	}

	fmt.Printf("\n%s %s %s %s\n",
		checkmark,
		cmdLabel,
		config.Colors.Command.Sprint(command),
		config.Colors.Dim.Sprintf("in %s", lib.FormatDuration(cmdDuration.Milliseconds())))
}

// renderDiagnosticResult renders command output with enhanced formatting
func renderDiagnosticResult(cfg *config.Config, command string, output string, cmdErr error) {
	if cfg.Verbose || output == "" {
		return // Skip rendering in verbose mode or if no output
	}

	// Skip rendering for commands with errors in non-verbose mode
	if cmdErr != nil {
		return
	}

	// Skip rendering for unhelpful results
	if isUnhelpfulResult(output) {
		return
	}

	block := formatDiagnosticBlock(cfg, command, output, cmdErr)
	fmt.Print(block)
}

// isUnhelpfulResult checks if the output contains common unhelpful messages
func isUnhelpfulResult(output string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(output))
	unhelpfulPatterns := []string{
		"no resources found",
		"no objects passed to",
		"no such file or directory",
		"connection refused",
		"not found",
		"not available",
		"not connected",
		"not authorized",
	}

	for _, pattern := range unhelpfulPatterns {
		if strings.Contains(trimmed, pattern) {
			return true
		}
	}

	// Also skip very short outputs (likely not useful)
	if len(strings.TrimSpace(output)) < 20 {
		return true
	}

	return false
}

// formatDiagnosticBlock builds a colored block for diagnostic command output using shared lib functionality
func formatDiagnosticBlock(cfg *config.Config, command string, output string, cmdErr error) string {
	maxLineLen := cfg.ToolOutputMaxLineLen
	maxLines := cfg.DiagnosticResultMaxLines
	if maxLines <= 0 {
		maxLines = 5 // fallback default
	}

	// Create render block using shared functionality
	block := &lib.RenderBlock{
		Title:      command,
		MaxLineLen: maxLineLen,
		MaxLines:   maxLines,
		Sections: []lib.RenderSection{
			{Content: output},
		},
	}

	return block.Format()
}

// printExecutionSummary displays the execution result summary with enhanced formatting
func printExecutionSummary(commands []string, statusData *struct {
	completedCount int
	failedCount    int
	skippedCount   int
	statusMap      map[int]bool
}, startTime time.Time) {
	// Add an extra newline for better separation of summary
	fmt.Println()

	// Print summary with improved formatting
	totalDuration := time.Since(startTime)
	successCount := statusData.completedCount - statusData.failedCount
	totalCommands := len(commands)

	// Choose color based on results
	var summaryColor *config.ANSIColor
	var checkmark string
	if statusData.failedCount > 0 {
		summaryColor = config.Colors.Warn
		checkmark = config.Colors.Warn.Sprint("✓")
	} else {
		summaryColor = config.Colors.Ok
		checkmark = config.Colors.Ok.Sprint("✓")
	}

	// Build status info
	skippedInfo := ""
	if statusData.skippedCount > 0 {
		skippedInfo = fmt.Sprintf(" (%d skipped)", statusData.skippedCount)
	}

	failedInfo := ""
	if statusData.failedCount > 0 {
		failedInfo = fmt.Sprintf(" (%d failed)", statusData.failedCount)
	}

	fmt.Printf("%s %s %s\n",
		checkmark,
		config.Colors.Info.Sprintf("Executing %d command(s):", totalCommands),
		summaryColor.Sprintf("%d/%d completed in %s%s%s",
			successCount,
			totalCommands,
			config.Colors.Dim.Sprint(lib.FormatDuration(totalDuration.Milliseconds())),
			failedInfo,
			skippedInfo))
}

// processResults processes command results and collects errors
func processResults(cfg *config.Config, results []config.CmdRes) error {
	var err error
	for _, res := range results {
		if !cfg.Verbose {
			logger.Log("in", cfg.CommandPrefix+" %s", res.Cmd)
			if res.Out != "" && !strings.HasPrefix(res.Cmd, cfg.CommandPrefix) {
				logger.Log("out", "%s", res.Out)
			}
		}
		if res.Err != nil {
			if !cfg.Verbose && !strings.HasPrefix(res.Cmd, cfg.CommandPrefix) {
				logger.Log("err", "%v", res.Err)
			}
			if err == nil {
				err = res.Err
			} else {
				err = fmt.Errorf("%v; %w", res.Err, err)
			}
		}
	}
	return err
}

// ExecKubectlCmd executes a kubectl command and returns its result
func ExecKubectlCmd(cfg *config.Config, command string) (result config.CmdRes) {
	// Trim the command to avoid empty commands
	command = strings.TrimSpace(command)
	result.Cmd = command

	// Reject empty commands
	if command == "" {
		result.Err = fmt.Errorf("empty command provided")
		result.Out = "No command provided"
		return result
	}

	// Check if it's a kubectl command and replace with custom binary path if needed
	isKubectlCmd := strings.HasPrefix(command, "kubectl")
	prefix := "!"
	if cfg != nil && strings.TrimSpace(cfg.CommandPrefix) != "" {
		prefix = cfg.CommandPrefix
	}
	isShellCmd := strings.HasPrefix(command, prefix)

	if !isKubectlCmd && !isShellCmd {
		result.Err = fmt.Errorf("invalid command: %s (must start with 'kubectl' or '$')", command)
		return result
	}

	var envBlockedList []string
	var envBlocked = os.Getenv("QU_KUBECTL_BLOCKED_CMDS_EXTRA")
	if envBlocked != "" {
		envBlockedList = strings.Split(envBlocked, ",")
	}

	blacklist := append(cfg.BlockedKubectlCmds, envBlockedList...)

	if isShellCmd {
		command = strings.TrimSpace(strings.TrimPrefix(command, prefix))
	} else if isKubectlCmd {
		// Check if command contains blocked kubectl operations
		for _, cmd := range blacklist {
			if strings.HasPrefix(command, "kubectl "+cmd) || strings.Contains(command, " "+cmd+" ") {
				result.Err = fmt.Errorf("command '%s' is not allowed", command)
				return result
			}
		}

		// Replace 'kubectl' with the configured binary path
		if cfg.KubectlBinaryPath != "kubectl" {
			command = strings.Replace(command, "kubectl", cfg.KubectlBinaryPath, 1)
		}
	}

	// Use the provided timeout for the command execution
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	logger.Log("info", "Executing command with %d second timeout: %s", cfg.Timeout, command)

	// Create command with process group setup
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	// New process group on Unix systems
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Setup separate goroutine to enforce timeout
	var timedOut bool
	var mu sync.Mutex
	timeoutTimer := time.AfterFunc(time.Duration(cfg.Timeout)*time.Second, func() {
		mu.Lock()
		timedOut = true
		mu.Unlock()

		// Force kill the process group
		if cmd.Process != nil {
			logger.Log("warn", "Command timed out after %d seconds, forcefully terminating: %s", cfg.Timeout, command)
			pgid := cmd.Process.Pid
			// Kill the entire process group
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	})
	defer timeoutTimer.Stop()

	// Capture output
	output, err := cmd.CombinedOutput()

	mu.Lock()
	isTimedOut := timedOut
	mu.Unlock()

	// Handle timeout case
	if isTimedOut || ctx.Err() == context.DeadlineExceeded {
		timeoutMsg := fmt.Sprintf("\n*** COMMAND TIMED OUT AFTER %d SECONDS ***\n", cfg.Timeout)
		if len(output) > 0 {
			// Append timeout message to partial output
			output = append(output, []byte(timeoutMsg)...)
		} else {
			output = []byte(timeoutMsg)
		}
		err = fmt.Errorf("command timed out after %d seconds", cfg.Timeout)
	}

	if err != nil {
		result.Err = fmt.Errorf("error executing command '%s': %w", command, err)
		// Include output in the result even if there was an error
		result.Out = string(output)
	} else {
		// Set output, but use placeholder if it's empty
		if len(output) == 0 {
			result.Out = fmt.Sprintf("No output from command: %s", command)
		} else {
			result.Out = string(output)
		}
	}

	// Print command output for interactive commands (those with $ prefix),
	// always in edit mode, or when verbose mode is enabled
	if cfg.Verbose || cfg.EditMode || (cfg != nil && strings.HasPrefix(result.Cmd, cfg.CommandPrefix)) {
		dim := config.Colors.ThinkDim.SprintFunc()
		bold := config.Colors.Bold.SprintFunc()

		prefix := "!"
		if cfg != nil && strings.TrimSpace(cfg.CommandPrefix) != "" {
			prefix = cfg.CommandPrefix
		}
		fmt.Println(bold("\n" + prefix + " " + result.Cmd))
		for _, line := range strings.Split(result.Out, "\n") {
			fmt.Println(dim("-- " + line))
		}
	}

	return result
}
