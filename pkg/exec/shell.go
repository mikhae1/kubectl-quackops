package exec

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

// ExecShellCommand executes a shell command with context and timeout
// Returns the combined output and any error encountered
func ExecShellCommand(cfg *config.Config, command string) (string, error) {
	// Use the provided timeout for the command execution
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	logger.Log("info", "Executing shell command with %d second timeout: %s", cfg.Timeout, command)

	// Create a new command
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	// Capture output
	output, err := cmd.CombinedOutput()

	// Handle timeout case
	if ctx.Err() == context.DeadlineExceeded {
		timeoutMsg := fmt.Sprintf("\n*** COMMAND TIMED OUT AFTER %d SECONDS ***\n", cfg.Timeout)
		if len(output) > 0 {
			// Append timeout message to partial output
			output = append(output, []byte(timeoutMsg)...)
		} else {
			output = []byte(timeoutMsg)
		}
		err = fmt.Errorf("command timed out after %d seconds", cfg.Timeout)
	}

	// Process command output
	outputStr := string(output)

	// Log the command result
	if err != nil {
		logger.Log("err", "Shell command failed: %v\nOutput: %s", err, outputStr)
	} else {
		logger.Log("info", "Shell command succeeded\nOutput: %s", outputStr)
	}

	return strings.TrimSpace(outputStr), err
}
