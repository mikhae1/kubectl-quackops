package completer

import (
	"os"
	"os/exec"
	"reflect"
	"testing"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// originalExecCommand stores the original exec.Command function for tests
var originalExecCommand = execCommand

// mockExecCommand creates a mock for exec.Command
func mockExecCommand(command string, args ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--", command}
	cs = append(cs, args...)
	cmd := exec.Command(os.Args[0], cs...)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd
}

// TestHelperProcess is not a real test. It's used as a helper process
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(2)
	}

	// Mock specific command responses
	if args[0] == "bash" && args[1] == "-c" {
		if args[2] == "compgen -c ku" {
			// Mock command completion
			os.Stdout.WriteString("kubectl\nkubectl-quackops\nkubens\nkubectx\n")
		} else if args[2] == "compgen -f -- test" {
			// Mock file completion
			os.Stdout.WriteString("test.go\ntest_dir/\n")
		}
	} else if args[0] == "sh" && args[1] == "-c" && args[2] == "'kubectl' __complete 'get' 'po'" {
		// Mock kubectl completion
		os.Stdout.WriteString("pods\npodmonitors\n")
	}

	os.Exit(0)
}

func TestParseCommandLine(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "Simple command",
			input:    "kubectl get pods",
			expected: []string{"kubectl", "get", "pods"},
		},
		{
			name:     "Command with quotes",
			input:    "echo \"hello world\"",
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "Command with single quotes",
			input:    "echo 'hello world'",
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "Command with escaped quotes",
			input:    "echo \\\"hello world\\\"",
			expected: []string{"echo", "\"hello", "world\""},
		},
		{
			name:     "Command with mixed quotes",
			input:    "echo \"hello 'world'\"",
			expected: []string{"echo", "hello 'world'"},
		},
		{
			name:     "Empty string",
			input:    "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCommandLine(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ParseCommandLine(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Safe input",
			input:    "kubectl",
			expected: "kubectl",
		},
		{
			name:     "Input with semicolon",
			input:    "kubectl;ls",
			expected: "kubectlls",
		},
		{
			name:     "Input with pipe",
			input:    "kubectl|grep",
			expected: "kubectlgrep",
		},
		{
			name:     "Input with dollar",
			input:    "echo $HOME",
			expected: "echo HOME",
		},
		{
			name:     "Input with backtick",
			input:    "echo `ls`",
			expected: "echo ls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeInput(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeInput(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestEscapeShellArg(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple argument",
			input:    "pods",
			expected: "'pods'",
		},
		{
			name:     "Argument with spaces",
			input:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "Argument with single quote",
			input:    "O'Reilly",
			expected: "'O'\\''Reilly'",
		},
		{
			name:     "Argument with special chars",
			input:    "file with $special;chars",
			expected: "'file with $special;chars'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeShellArg(tt.input)
			if result != tt.expected {
				t.Errorf("escapeShellArg(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsValidCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "Valid command",
			input:    "kubectl",
			expected: true,
		},
		{
			name:     "Command with dash",
			input:    "kubectl-quackops",
			expected: true,
		},
		{
			name:     "Command with underscore",
			input:    "kube_ctl",
			expected: true,
		},
		{
			name:     "Command with dot",
			input:    "kubectl.exe",
			expected: true,
		},
		{
			name:     "Command with space",
			input:    "kubectl get",
			expected: false,
		},
		{
			name:     "Command with semicolon",
			input:    "kubectl;ls",
			expected: false,
		},
		{
			name:     "Empty command",
			input:    "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidCommand(tt.input)
			if result != tt.expected {
				t.Errorf("isValidCommand(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShellAutoCompleter_Do(t *testing.T) {
	// Replace exec.Command with mock
	execCommand = mockExecCommand
	defer func() { execCommand = originalExecCommand }()

	cfg := &config.Config{
		MaxCompletions: 10,
	}
	completer := NewShellAutoCompleter(cfg)

	tests := []struct {
		name            string
		line            string
		pos             int
		wantCompletions bool
	}{
		{
			name:            "Complete command",
			line:            "$ku",
			pos:             3,
			wantCompletions: true,
		},
		{
			name:            "Not in shell mode",
			line:            "ku",
			pos:             2,
			wantCompletions: false,
		},
		{
			name:            "Empty line",
			line:            "$",
			pos:             1,
			wantCompletions: false,
		},
		{
			name:            "Position past end",
			line:            "$ku",
			pos:             5, // Past end
			wantCompletions: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			completions, _ := completer.Do([]rune(tt.line), tt.pos)
			hasCompletions := len(completions) > 0

			if hasCompletions != tt.wantCompletions {
				t.Errorf("Do(%q, %d) got completions: %v, want: %v",
					tt.line, tt.pos, hasCompletions, tt.wantCompletions)
			}
		})
	}
}

func TestEscapePathForShell(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple path",
			input:    "test.txt",
			expected: "test.txt",
		},
		{
			name:     "Path with spaces",
			input:    "my file.txt",
			expected: "my\\\\ file.txt",
		},
		{
			name:     "Path with special chars",
			input:    "file(1).txt",
			expected: "file\\\\(1\\\\).txt",
		},
		{
			name:     "Path with multiple special chars",
			input:    "file with $pecial & chars.txt",
			expected: "file\\\\ with\\\\ \\\\$pecial\\\\ \\\\&\\\\ chars.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapePathForShell(tt.input)
			if result != tt.expected {
				t.Errorf("escapePathForShell(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
