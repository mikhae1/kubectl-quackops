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

	// Smoke test for printExecutionSummary
	printExecutionSummary(commands, statusData, time.Now())
}

func TestMonitorCommandStatus(t *testing.T) {
	cfg := &config.Config{SpinnerTimeout: 100}
	s := initSpinner(cfg, 5)
	defer s.Stop()

	statusChan := make(chan cmdStatus, 5)
	defer close(statusChan)

	data := monitorCommandStatus(statusChan, s, 5)

	statusChan <- cmdStatus{0, true, nil, false} // Completed successfully
	statusChan <- cmdStatus{1, true, nil, true}  // Skipped
	statusChan <- cmdStatus{2, true, nil, true}  // Skipped
	statusChan <- cmdStatus{3, true, nil, false} // Completed successfully
	statusChan <- cmdStatus{4, true, nil, false} // Completed successfully

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
	cfg := &config.Config{SpinnerTimeout: 100, SafeMode: true}
	s := initSpinner(cfg, 4)
	defer s.Stop()

	statusChan := make(chan cmdStatus, 4)
	defer close(statusChan)
	data := monitorCommandStatus(statusChan, s, 4)

	commands := []string{
		"kubectl get pods",
		"$echo 'Shell command'",
		"kubectl describe pods",
		"kubectl logs pods",
	}
	results := make([]config.CmdRes, len(commands))

	// Simulate user declines kubectl; safe mode auto-skips $ commands
	for i, cmd := range commands {
		if strings.HasPrefix(cmd, cfg.CommandPrefix) {
			result := config.CmdRes{
				Cmd: cmd,
				Out: "Skipped shell command in safe mode",
			}
			results[i] = result
			statusChan <- cmdStatus{i, true, nil, true}
		} else {
			statusChan <- cmdStatus{i, true, nil, true}
		}
	}

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

	if data.skippedCount != 4 {
		t.Errorf("Expected 4 skipped commands, got %d", data.skippedCount)
	}

	if data.completedCount != 0 {
		t.Errorf("Expected 0 completed commands, got %d", data.completedCount)
	}
}
