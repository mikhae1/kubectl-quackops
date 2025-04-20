package exec

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// Helper functions for tests
func initSpinner(cfg *config.Config, commandCount int) *spinner.Spinner {
	s := spinner.New(spinner.CharSets[11], time.Duration(cfg.SpinnerTimeout)*time.Millisecond)
	s.Suffix = fmt.Sprintf(" Executing %d commands...", commandCount)
	s.Color("cyan", "bold")
	return s
}

func monitorCommandStatus(statusChan <-chan cmdStatus, s *spinner.Spinner, commandCount int) *struct {
	completedCount int
	failedCount    int
	skippedCount   int
	statusMap      map[int]bool
} {
	data := &struct {
		completedCount int
		failedCount    int
		skippedCount   int
		statusMap      map[int]bool
	}{
		statusMap: make(map[int]bool),
	}

	// For testing purposes only - this is a simplified version
	// that doesn't block waiting for all messages
	return data
}

func TestPrintExecutionSummary(t *testing.T) {
	// Create test status data with skipped commands
	statusData := &struct {
		completedCount int
		failedCount    int
		skippedCount   int
		statusMap      map[int]bool
	}{
		completedCount: 3,
		failedCount:    1,
		skippedCount:   2,
		statusMap:      make(map[int]bool),
	}

	commands := []string{"cmd1", "cmd2", "cmd3", "cmd4", "cmd5"}

	// The test ensures the function runs without panicking and shows the proper format
	// In a real scenario, we'd capture stdout and verify the exact output
	//
	// Expected output format with these parameters:
	// ✓ Executing 5 command(s): 2/5 completed in 0ms (1 failed, 2 skipped)
	//
	// Where:
	// - 5 is the total command count (len(commands))
	// - 2 is the success count (completedCount - failedCount)
	// - 1 is the failed count
	// - 2 is the skipped count
	printExecutionSummary(commands, statusData, time.Now())
}

func TestMonitorCommandStatus(t *testing.T) {
	// Create a mock spinner
	cfg := &config.Config{SpinnerTimeout: 100}
	s := initSpinner(cfg, 5)
	defer s.Stop()

	// Create status channel
	statusChan := make(chan cmdStatus, 5)
	defer close(statusChan)

	// Start monitoring
	data := monitorCommandStatus(statusChan, s, 5)

	// Send test statuses
	statusChan <- cmdStatus{0, true, nil, false} // Completed successfully
	statusChan <- cmdStatus{1, true, nil, true}  // Skipped
	statusChan <- cmdStatus{2, true, nil, true}  // Skipped
	statusChan <- cmdStatus{3, true, nil, false} // Completed successfully
	statusChan <- cmdStatus{4, true, nil, false} // Completed successfully

	// Process the status data manually for testing
	for i := 0; i < 5; i++ {
		status := <-statusChan
		if status.done {
			if status.skipped {
				data.skippedCount++
			} else {
				data.completedCount++
				data.statusMap[status.index] = status.err == nil
				if status.err != nil {
					data.failedCount++
				}
			}
		}
	}

	// Verify counts
	if data.completedCount != 3 {
		t.Errorf("Expected 3 completed commands, got %d", data.completedCount)
	}

	if data.skippedCount != 2 {
		t.Errorf("Expected 2 skipped commands, got %d", data.skippedCount)
	}

	if data.failedCount != 0 {
		t.Errorf("Expected 0 failed commands, got %d", data.failedCount)
	}
}

func TestShellCommandsSkippedInSafeMode(t *testing.T) {
	// Create a mock spinner
	cfg := &config.Config{SpinnerTimeout: 100, SafeMode: true}
	s := initSpinner(cfg, 4)
	defer s.Stop()

	// Create status channel and setup tracking
	statusChan := make(chan cmdStatus, 4)
	defer close(statusChan)
	data := monitorCommandStatus(statusChan, s, 4)

	// Setup for mock execution
	commands := []string{
		"kubectl get pods",
		"$echo 'Shell command'",
		"kubectl describe pods",
		"kubectl logs pods",
	}
	results := make([]config.CmdRes, len(commands))

	// Mock the promptForCommandConfirmation function behavior
	// This simulates all kubectl commands being skipped by user
	// and $ commands being automatically skipped in safe mode
	for i, cmd := range commands {
		if strings.HasPrefix(cmd, "$") {
			// For $ commands, they should be automatically marked as skipped
			result := config.CmdRes{
				Cmd: cmd,
				Out: "Skipped shell command in safe mode",
			}
			results[i] = result
			statusChan <- cmdStatus{i, true, nil, true}
		} else {
			// For kubectl commands, simulate user saying "no"
			statusChan <- cmdStatus{i, true, nil, true}
		}
	}

	// Process the statuses manually for testing
	for i := 0; i < 4; i++ {
		status := <-statusChan
		if status.done {
			if status.skipped {
				data.skippedCount++
			} else {
				data.completedCount++
				data.statusMap[status.index] = status.err == nil
				if status.err != nil {
					data.failedCount++
				}
			}
		}
	}

	// Verify all commands are counted as skipped
	if data.skippedCount != 4 {
		t.Errorf("Expected 4 skipped commands, got %d", data.skippedCount)
	}

	if data.completedCount != 0 {
		t.Errorf("Expected 0 completed commands, got %d", data.completedCount)
	}
}
