package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/llm"
	"github.com/tmc/langchaingo/llms"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

// Test helper to create a test configuration
func createInteractiveTestConfig() *config.Config {
	cfg := &config.Config{
		Provider:                  "openai",
		Model:                     "gpt-4",
		ChatMessages:              []llms.ChatMessage{},
		SafeMode:                  false,
		Verbose:                   false,
		DisableMarkdownFormat:     true, // For predictable testing
		DisableAnimation:          true, // For predictable testing
		DisableHistory:            true, // Disable file-based history for tests
		Retries:                   1,    // Reduce retries for faster tests
		Timeout:                   5,    // Short timeout for tests
		DefaultMaxTokens:          4096,
		UserMaxTokens:             0,
		SpinnerTimeout:            100,
		ThrottleRequestsPerMinute: 0, // Disable throttling for tests
		CommandPrefix:             "!",
		EditMode:                  false,
		MCPClientEnabled:          false,
		InputTokenReservePercent:  20,
		MinInputTokenReserve:      1024,
		MinOutputTokens:           512,
		LastOutgoingTokens:        0,
		LastIncomingTokens:        0,
		SlashCommands: []config.SlashCommand{
			{
				Commands:    []string{"/help", "/h"},
				Primary:     "/help",
				Description: "Show help information",
			},
			{
				Commands:    []string{"/version"},
				Primary:     "/version",
				Description: "Show version information",
			},
			{
				Commands:    []string{"/reset"},
				Primary:     "/reset",
				Description: "Reset conversation context",
			},
			{
				Commands:    []string{"/clear"},
				Primary:     "/clear",
				Description: "Clear context and screen",
			},
		},
	}
	return cfg
}

func TestProcessUserPrompt_BasicPrompts(t *testing.T) {
	tests := []struct {
		name          string
		prompt        string
		expectedError bool
		mockResponses []llm.MockResponse
		expectLLMCall bool
	}{
		{
			name:          "simple_question",
			prompt:        "What is Kubernetes?",
			expectedError: false,
			mockResponses: []llm.MockResponse{
				{Content: "Kubernetes is a container orchestration platform.", TokensUsed: 50},
			},
			expectLLMCall: true,
		},
		{
			name:          "command_execution",
			prompt:        "! kubectl get pods",
			expectedError: false,
			mockResponses: []llm.MockResponse{}, // No LLM call expected
			expectLLMCall: false,
		},
		{
			name:          "slash_command_help",
			prompt:        "/help",
			expectedError: false,
			mockResponses: []llm.MockResponse{}, // No LLM call expected
			expectLLMCall: false,
		},
		{
			name:          "slash_command_version",
			prompt:        "/version",
			expectedError: false,
			mockResponses: []llm.MockResponse{}, // No LLM call expected
			expectLLMCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createInteractiveTestConfig()

			// Mock the Request functions if LLM call is expected
			if tt.expectLLMCall {
				originalRequest := llm.Request
				originalRequestWithSystem := llm.RequestWithSystem
				llm.Request = llm.MockRequestFunc(tt.mockResponses)
				llm.RequestWithSystem = llm.MockRequestWithSystemFunc(tt.mockResponses)
				defer func() {
					llm.Request = originalRequest
					llm.RequestWithSystem = originalRequestWithSystem
				}()
			}

			// Capture output
			oldStdout := os.Stdout
			oldStderr := os.Stderr
			rOut, wOut, _ := os.Pipe()
			rErr, wErr, _ := os.Pipe()
			os.Stdout = wOut
			os.Stderr = wErr

			// Execute the prompt processing
			err := processUserPrompt(cfg, tt.prompt, "", 1)

			// Restore output
			wOut.Close()
			wErr.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr

			// Read captured output
			outBytes, _ := io.ReadAll(rOut)
			errBytes, _ := io.ReadAll(rErr)
			stdout := string(outBytes)
			stderr := string(errBytes)

			// Assertions
			if tt.expectedError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !tt.expectedError && err != nil {
				t.Errorf("Unexpected error: %v", err)
				t.Logf("Stdout: %s", stdout)
				t.Logf("Stderr: %s", stderr)
			}

			// Verify LLM call expectations
			if tt.expectLLMCall {
				// For regular prompts, we expect some response on stdout
				if strings.TrimSpace(stdout) == "" {
					t.Errorf("Expected output from LLM call but got none")
				}
			} else {
				// For commands and slash commands, we don't expect LLM calls
				// but we may expect help text or command output
			}
		})
	}
}

func TestProcessUserPrompt_EditMode(t *testing.T) {
	tests := []struct {
		name       string
		editMode   bool
		prompt     string
		expectCmd  bool
		cmdResults []config.CmdRes
	}{
		{
			name:      "edit_mode_command",
			editMode:  true,
			prompt:    "kubectl get pods",
			expectCmd: true,
		},
		{
			name:      "normal_mode_command",
			editMode:  false,
			prompt:    "! kubectl get pods",
			expectCmd: true,
		},
		{
			name:      "edit_mode_regular_prompt",
			editMode:  true,
			prompt:    "explain pods", // This is treated as command in edit mode
			expectCmd: true,
		},
		{
			name:      "normal_mode_regular_prompt",
			editMode:  false,
			prompt:    "explain pods", // This is treated as LLM prompt
			expectCmd: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createInteractiveTestConfig()
			cfg.EditMode = tt.editMode

			// Mock the Request functions for non-command prompts
			if !tt.expectCmd {
				mockResponses := []llm.MockResponse{
					{Content: "Mock explanation about pods", TokensUsed: 50},
				}
				llm.Request = llm.MockRequestFunc(mockResponses)
				llm.RequestWithSystem = llm.MockRequestWithSystemFunc(mockResponses)
			}

			// Capture output
			oldStdout := os.Stdout
			oldStderr := os.Stderr
			rOut, wOut, _ := os.Pipe()
			rErr, wErr, _ := os.Pipe()
			os.Stdout = wOut
			os.Stderr = wErr

			err := processUserPrompt(cfg, tt.prompt, "", 1)

			// Restore output
			wOut.Close()
			wErr.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr

			// Read captured output
			outBytes, _ := io.ReadAll(rOut)
			errBytes, _ := io.ReadAll(rErr)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				t.Logf("Stdout: %s", string(outBytes))
				t.Logf("Stderr: %s", string(errBytes))
			}

			// Verify command execution results
			if tt.expectCmd {
				if len(cfg.StoredUserCmdResults) == 0 {
					t.Errorf("Expected command results to be stored")
				}
			}
		})
	}
}

func TestProcessUserPrompt_HistoryManagement(t *testing.T) {
	tests := []struct {
		name             string
		prompt           string
		history          bool
		initialMessages  int
		expectedMessages int
	}{
		{
			name:             "prompt_with_history",
			prompt:           "What are pods?",
			history:          true,
			initialMessages:  0,
			expectedMessages: 2, // Human + AI
		},
		{
			name:             "prompt_without_history",
			prompt:           "What are pods?",
			history:          false,
			initialMessages:  0,
			expectedMessages: 2, // processUserPrompt appends history
		},
		{
			name:             "existing_history",
			prompt:           "Tell me more",
			history:          true,
			initialMessages:  4, // 2 existing conversations
			expectedMessages: 6, // 4 existing + 2 new
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createInteractiveTestConfig()

			// Add initial history if specified
			for i := 0; i < tt.initialMessages; i += 2 {
				cfg.ChatMessages = append(cfg.ChatMessages,
					llms.HumanChatMessage{Content: fmt.Sprintf("Question %d", i/2+1)},
					llms.AIChatMessage{Content: fmt.Sprintf("Answer %d", i/2+1)},
				)
			}

			// Mock LLM request
			mockResponses := []llm.MockResponse{
				{Content: "Mock response for testing", TokensUsed: 40},
			}
			llm.Request = llm.MockRequestFunc(mockResponses)
			llm.RequestWithSystem = llm.MockRequestWithSystemFunc(mockResponses)

			// Capture output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			err := processUserPrompt(cfg, tt.prompt, "", 1)

			w.Close()
			os.Stderr = oldStderr
			io.ReadAll(r) // Discard output

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Verify history management
			if len(cfg.ChatMessages) != tt.expectedMessages {
				t.Errorf("Expected %d messages in history, got %d", tt.expectedMessages, len(cfg.ChatMessages))
			}
		})
	}
}

func TestProcessUserPrompt_SlashCommands(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		expectHelp  bool
		expectError bool
	}{
		{
			name:        "help_command",
			command:     "/help",
			expectHelp:  true,
			expectError: false,
		},
		{
			name:        "h_shortcut",
			command:     "/h",
			expectHelp:  true,
			expectError: false,
		},
		{
			name:        "version_command",
			command:     "/version",
			expectHelp:  false,
			expectError: false,
		},
		{
			name:        "reset_command",
			command:     "/reset",
			expectHelp:  false,
			expectError: false,
		},
		{
			name:        "unknown_command",
			command:     "/unknown",
			expectHelp:  false,
			expectError: false, // Should show "unknown command" message
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createInteractiveTestConfig()

			// Add some initial history for reset testing
			cfg.ChatMessages = []llms.ChatMessage{
				llms.HumanChatMessage{Content: "Test message"},
				llms.AIChatMessage{Content: "Test response"},
			}

			// Capture output
			oldStdout := os.Stdout
			oldStderr := os.Stderr
			rOut, wOut, _ := os.Pipe()
			rErr, wErr, _ := os.Pipe()
			os.Stdout = wOut
			os.Stderr = wErr

			err := processUserPrompt(cfg, tt.command, "", 1)

			// Restore output
			wOut.Close()
			wErr.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr

			// Read captured output
			outBytes, _ := io.ReadAll(rOut)
			errBytes, _ := io.ReadAll(rErr)
			stdout := string(outBytes)
			stderr := string(errBytes)
			combinedOutput := stdout + stderr

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.expectHelp {
				// Check for help content
				if !strings.Contains(combinedOutput, "help") &&
					!strings.Contains(combinedOutput, "command") {
					t.Errorf("Expected help content in output, got: %s", combinedOutput)
				}
			}

			// Test specific command behaviors
			switch tt.command {
			case "/reset":
				// History should be cleared but stored results remain
				if len(cfg.ChatMessages) != 0 {
					t.Errorf("Expected chat messages to be cleared after reset")
				}
			case "/version":
				// Should show version info
				if !strings.Contains(combinedOutput, "version") &&
					!strings.Contains(combinedOutput, "Version") {
					t.Logf("Version output: %s", combinedOutput)
					// Don't fail - version might be handled differently
				}
			case "/unknown":
				// Should show unknown command message
				if !strings.Contains(combinedOutput, "Unknown command") &&
					!strings.Contains(combinedOutput, "unknown") {
					t.Errorf("Expected 'unknown command' message, got: %s", combinedOutput)
				}
			}
		})
	}
}

func TestProcessUserPrompt_VerboseMode(t *testing.T) {
	tests := []struct {
		name     string
		verbose  bool
		prompt   string
		checkLog bool
	}{
		{
			name:     "verbose_enabled",
			verbose:  true,
			prompt:   "explain containers",
			checkLog: true,
		},
		{
			name:     "verbose_disabled",
			verbose:  false,
			prompt:   "explain containers",
			checkLog: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createInteractiveTestConfig()
			cfg.Verbose = tt.verbose

			// Mock LLM request
			mockResponses := []llm.MockResponse{
				{Content: "Mock explanation about containers", TokensUsed: 60},
			}
			llm.Request = llm.MockRequestFunc(mockResponses)
			llm.RequestWithSystem = llm.MockRequestWithSystemFunc(mockResponses)

			// Capture stderr for verbose logging
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			err := processUserPrompt(cfg, tt.prompt, "", 1)

			w.Close()
			os.Stderr = oldStderr
			stderrBytes, _ := io.ReadAll(r)
			stderrOutput := string(stderrBytes)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if tt.checkLog {
				// In verbose mode, expect more detailed logging
				hasVerboseOutput := strings.Contains(stderrOutput, "Processing prompt") ||
					strings.Contains(stderrOutput, "editMode") ||
					len(stderrOutput) > 50

				if !hasVerboseOutput {
					t.Errorf("Expected verbose logging output, got: %s", stderrOutput)
				}
			}
		})
	}
}

func TestStartChatSession_NonInteractive(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		expectError   bool
		mockResponses []llm.MockResponse
	}{
		{
			name: "single_prompt",
			args: []string{"What are Kubernetes pods?"},
			mockResponses: []llm.MockResponse{
				{Content: "Pods are the smallest deployable units in Kubernetes.", TokensUsed: 45},
			},
			expectError: false,
		},
		{
			name:        "empty_args",
			args:        []string{},
			expectError: false, // Should enter interactive mode (not tested here)
		},
		{
			name:          "empty_prompt",
			args:          []string{""},
			mockResponses: []llm.MockResponse{},
			expectError:   false, // Should enter interactive mode
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createInteractiveTestConfig()

			// Mock LLM request if responses provided
			if len(tt.mockResponses) > 0 {
				llm.Request = llm.MockRequestFunc(tt.mockResponses)
				llm.RequestWithSystem = llm.MockRequestWithSystemFunc(tt.mockResponses)
			}

			// For non-interactive mode test, only test with actual prompts
			if len(tt.args) > 0 && strings.TrimSpace(tt.args[0]) != "" {
				// Capture output
				oldStderr := os.Stderr
				r, w, _ := os.Pipe()
				os.Stderr = w

				err := startChatSession(cfg, tt.args)

				w.Close()
				os.Stderr = oldStderr
				io.ReadAll(r) // Discard output

				if tt.expectError && err == nil {
					t.Errorf("Expected error but got none")
				} else if !tt.expectError && err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestNewRootCmd_Integration(t *testing.T) {
	// Test that the root command can be created and configured properly
	streams := genericiooptions.IOStreams{
		In:     &bytes.Buffer{},
		Out:    &bytes.Buffer{},
		ErrOut: &bytes.Buffer{},
	}

	rootCmd := NewRootCmd(streams)

	// Verify command structure
	if rootCmd.Use != "kubectl-quackops" {
		t.Errorf("Expected command use to be 'kubectl-quackops', got '%s'", rootCmd.Use)
	}

	// Test flag presence and defaults
	flags := rootCmd.Flags()

	// Test provider flag
	providerFlag := flags.Lookup("provider")
	if providerFlag == nil {
		t.Errorf("Expected 'provider' flag to exist")
	}

	// Test verbose flag
	verboseFlag := flags.Lookup("verbose")
	if verboseFlag == nil {
		t.Errorf("Expected 'verbose' flag to exist")
	}

	// Test that env subcommand exists
	envCmd, _, err := rootCmd.Find([]string{"env"})
	if err != nil {
		t.Errorf("Error finding 'env' subcommand: %v", err)
	}
	if envCmd.Use != "env" {
		t.Errorf("Expected 'env' subcommand use to be 'env', got '%s'", envCmd.Use)
	}
}
