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

	"github.com/briandowns/spinner"
	"github.com/fatih/color"
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
	logger.Log("info", "Executing diagnostic commands: %v", commands)

	// Initialize tracking variables
	startTime := time.Now()
	results := make([]config.CmdRes, len(commands))

	// Simple counters for status tracking
	completed, failed, skipped := 0, 0, 0
	statusMap := make(map[int]bool)

	// Status channel to collect execution updates
	statusChan := make(chan cmdStatus, len(commands))
	defer close(statusChan)

	// Initialize spinner
	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Suffix = fmt.Sprintf(" Executing %d commands...", len(commands))
	s.Color("cyan", "bold")
	s.Start()
	defer s.Stop()

	// Start status monitoring goroutine
	go func() {
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

				s.Suffix = fmt.Sprintf(" Executing %d commands... %d/%d completed",
					len(commands), completed, len(commands))
			}
		}
	}()

	// Execute commands based on mode
	firstCommandCompletedMutex := &sync.Mutex{}
	firstCommandCompleted := false

	if cfg.SafeMode {
		executeCommandsSequentially(cfg, commands, results, statusChan, &firstCommandCompleted, firstCommandCompletedMutex, s)
	} else {
		executeCommandsParallel(cfg, commands, results, statusChan, &firstCommandCompleted, firstCommandCompletedMutex)
	}

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
	s *spinner.Spinner,
) {
	for i, command := range commands {
		if !strings.HasPrefix(command, "$") {
			if !promptForCommandConfirmation(command, i, statusChan, s) {
				// Skip this command if not confirmed
				continue
			}
		} else {
			// For commands with $ prefix, mark them as skipped in safe mode
			// These are shell commands which are not executed in safe mode
			result := config.CmdRes{
				Cmd: command,
				Out: "Skipped shell command in safe mode",
			}
			results[i] = result
			statusChan <- cmdStatus{i, true, nil, true}
			continue
		}

		// Execute the command
		cmdStart := time.Now()
		results[i] = ExecKubectlCmd(cfg, command)
		cmdDuration := time.Since(cmdStart)

		// Send status update
		statusChan <- cmdStatus{i, true, results[i].Err, false}

		// Print command result
		printCommandResult(command, results[i].Err, cmdDuration, firstCommandCompleted, firstCommandCompletedMutex)
	}
}

// promptForCommandConfirmation asks for user confirmation in safe mode
func promptForCommandConfirmation(command string, index int, statusChan chan<- cmdStatus, s *spinner.Spinner) bool {
	// Stop spinner to show the confirmation prompt
	s.Stop()
	fmt.Printf("\nExecute '%s' (y/N)? ", command)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if strings.ToLower(input) != "y" {
		fmt.Println("Skipping...")
		statusChan <- cmdStatus{index, true, nil, true}

		// Only restart spinner if there are more commands to process
		if index < cap(statusChan)-1 {
			s.Start()
		}
		return false
	}

	// Restart spinner for command execution
	s.Start()
	return true
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
		}(i, command)
	}

	wg.Wait()
}

// printCommandResult formats and prints the result of a command execution
func printCommandResult(
	command string,
	cmdErr error,
	cmdDuration time.Duration,
	firstCommandCompleted *bool,
	firstCommandCompletedMutex *sync.Mutex,
) {
	checkmark := color.GreenString("✓")
	cmdLabel := color.HiWhiteString("running:")
	if cmdErr != nil {
		checkmark = color.RedString("✗")
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

	fmt.Printf("%s %s %s %s\n",
		checkmark,
		cmdLabel,
		color.CyanString(command),
		color.HiBlackString("in %s", lib.FormatDuration(cmdDuration.Milliseconds())))
}

// printExecutionSummary displays the execution result summary
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

	summaryColor := color.HiGreenString
	if statusData.failedCount > 0 {
		summaryColor = color.HiYellowString
	}

	skippedInfo := ""
	if statusData.skippedCount > 0 {
		skippedInfo = fmt.Sprintf(", %d skipped", statusData.skippedCount+1)
	}

	fmt.Printf("%s %s %s\n",
		color.GreenString("✓"),
		color.HiWhiteString("Executing %d command(s):", totalCommands),
		summaryColor("%d/%d completed in %s (%d failed%s)",
			successCount,
			totalCommands,
			color.HiBlackString(lib.FormatDuration(totalDuration.Milliseconds())),
			statusData.failedCount,
			skippedInfo))
}

// processResults processes command results and collects errors
func processResults(cfg *config.Config, results []config.CmdRes) error {
	var err error
	for _, res := range results {
		if !cfg.Verbose {
			logger.Log("in", "$ %s", res.Cmd)
			if res.Out != "" && !strings.HasPrefix(res.Cmd, "$") {
				logger.Log("out", res.Out)
			}
		}
		if res.Err != nil {
			if !cfg.Verbose && !strings.HasPrefix(res.Cmd, "$") {
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

	// Return early if the command is empty
	if command == "" {
		result.Err = fmt.Errorf("empty command provided")
		result.Out = "No command provided"
		return result
	}

	// Check if it's a kubectl command and replace with custom binary path if needed
	isKubectlCmd := strings.HasPrefix(command, "kubectl")
	isShellCmd := strings.HasPrefix(command, "$")

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
		command = strings.TrimSpace(strings.TrimPrefix(command, "$"))
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

	// Create a new command with process group setup
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	// Create a new process group on Unix systems
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

	// Print command output for interactive commands (those with $ prefix)
	// or always when verbose mode is enabled
	if cfg.Verbose || strings.HasPrefix(result.Cmd, "$") {
		dim := color.New(color.Faint).SprintFunc()
		bold := color.New(color.Bold).SprintFunc()

		fmt.Println(bold("\n$ " + result.Cmd))
		for _, line := range strings.Split(result.Out, "\n") {
			fmt.Println(dim("-- " + line))
		}
	}

	return result
}
