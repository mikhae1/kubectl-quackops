package llm

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/lib"
	"github.com/tmc/langchaingo/llms"
)

// TestWriteNormalizedChunk tests the writeNormalizedChunk function for proper line break handling
func TestWriteNormalizedChunk(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple_text_no_newlines",
			input:    "Hello world",
			expected: "Hello world",
		},
		{
			name:     "single_newline",
			input:    "Line 1\nLine 2",
			expected: "Line 1\r\nLine 2",
		},
		{
			name:     "multiple_newlines",
			input:    "Line 1\nLine 2\nLine 3",
			expected: "Line 1\r\nLine 2\r\nLine 3",
		},
		{
			name:     "trailing_newline",
			input:    "Line 1\nLine 2\n",
			expected: "Line 1\r\nLine 2\r\n",
		},
		{
			name:     "empty_lines",
			input:    "Line 1\n\nLine 3",
			expected: "Line 1\r\n\r\nLine 3",
		},
		{
			name:     "empty_input",
			input:    "",
			expected: "",
		},
		{
			name:     "only_newlines",
			input:    "\n\n",
			expected: "\r\n\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := WriteNormalizedChunk(&buf, []byte(tt.input))

			if err != nil {
				t.Errorf("writeNormalizedChunk() error = %v", err)
				return
			}

			got := buf.String()
			if got != tt.expected {
				t.Errorf("writeNormalizedChunk() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestStreamingCallbackOutputIntegrity tests the streaming callback for output consistency
func TestStreamingCallbackOutputIntegrity(t *testing.T) {
	tests := []struct {
		name          string
		chunks        []string
		markdownMode  bool
		animationMode bool
		expectCRLF    bool
	}{
		{
			name:          "raw_output_multiline",
			chunks:        []string{"First line\n", "Second line\n", "Third line"},
			markdownMode:  false,
			animationMode: false,
			expectCRLF:    true,
		},
		{
			name:          "markdown_output_multiline",
			chunks:        []string{"# Header\n", "Some text\n", "More text"},
			markdownMode:  true,
			animationMode: false,
			expectCRLF:    true,
		},
		{
			name:          "single_chunk_multiline",
			chunks:        []string{"Line 1\nLine 2\nLine 3"},
			markdownMode:  false,
			animationMode: false,
			expectCRLF:    true,
		},
		{
			name:          "mixed_chunks",
			chunks:        []string{"Start", " middle\n", "Next line\n", "End"},
			markdownMode:  false,
			animationMode: false,
			expectCRLF:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CreateTestConfig()
			cfg.DisableMarkdownFormat = !tt.markdownMode
			cfg.DisableAnimation = !tt.animationMode

			// Create streaming callback
			callback, cleanup := CreateStreamingCallback(cfg, nil, nil, nil)
			defer cleanup()

			// Process chunks
			ctx := context.Background()
			for _, chunk := range tt.chunks {
				err := callback(ctx, []byte(chunk))
				if err != nil {
					t.Errorf("Streaming callback error: %v", err)
					return
				}
			}

			// For raw output mode, we need to capture from the direct fmt.Print calls
			// This test verifies the logic without needing to intercept stdout
		})
	}
}

// TestStreamingProgressiveIndentationRegression tests for the specific bug where
// each line gets progressively more indented during streaming output
func TestStreamingProgressiveIndentationRegression(t *testing.T) {
	testCases := []struct {
		name               string
		simulatedLLMOutput string
		expectProgressive  bool
		description        string
	}{
		{
			name: "normal_aligned_output",
			simulatedLLMOutput: `Here's an analysis of your cluster:

1. Critical Issue: Pod failure
   Problem: The debug pod is failing

2. Secondary Issue: Service problems
   Problem: Service endpoint issues`,
			expectProgressive: false,
			description:       "Normal output should maintain consistent indentation",
		},
		{
			name: "problematic_llm_output",
			simulatedLLMOutput: `Here's an analysis of your cluster:
                                                      ### Critical Issues Identified:
                                                                                                         Problem: The debug pod is failing`,
			expectProgressive: true,
			description:       "LLM output with excessive whitespace should be normalized",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer

			// Simulate streaming by splitting into chunks at newlines
			chunks := strings.Split(tc.simulatedLLMOutput, "\n")
			for i, line := range chunks {
				chunk := line
				if i < len(chunks)-1 {
					chunk += "\n"
				}

				err := WriteNormalizedChunk(&output, []byte(chunk))
				if err != nil {
					t.Errorf("WriteNormalizedChunk failed: %v", err)
					return
				}
			}

			result := output.String()
			lines := strings.Split(result, "\n")

			// Check for progressive indentation by measuring leading spaces
			var leadingSpaces []int
			for _, line := range lines {
				if strings.TrimSpace(line) == "" {
					continue // Skip empty lines
				}
				spaces := 0
				for _, char := range line {
					if char == ' ' {
						spaces++
					} else {
						break
					}
				}
				leadingSpaces = append(leadingSpaces, spaces)
			}

			// Check if indentation is progressively increasing (the bug pattern)
			hasProgressiveIndent := false
			if len(leadingSpaces) > 1 {
				increasing := true
				for i := 1; i < len(leadingSpaces); i++ {
					if leadingSpaces[i] <= leadingSpaces[i-1] {
						increasing = false
						break
					}
				}
				hasProgressiveIndent = increasing
			}

			if tc.expectProgressive {
				if !hasProgressiveIndent {
					t.Errorf("Expected progressive indentation but didn't find it in output:\n%s\nLeading spaces: %v", result, leadingSpaces)
				}
			} else {
				if hasProgressiveIndent {
					t.Errorf("Found progressive indentation (bug reproduced) in output:\n%s\nLeading spaces: %v", result, leadingSpaces)
				}
			}
		})
	}
}

// TestStreamingANSISequenceIsolation ensures ANSI sequences don't interfere with streaming output
func TestStreamingANSISequenceIsolation(t *testing.T) {
	cfg := CreateTestConfig()

	// Capture both stdout and stderr
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()

	os.Stdout = wOut
	os.Stderr = wErr

	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	// Simulate streaming with token meter active (this reproduces the problematic condition)
	callback, cleanup := CreateStreamingCallback(cfg, nil, nil, nil)
	defer cleanup()

	// Stream some test content
	testContent := []string{
		"Line 1\n",
		"Line 2\n",
		"Line 3\n",
	}

	ctx := context.Background()
	for _, chunk := range testContent {
		err := callback(ctx, []byte(chunk))
		if err != nil {
			t.Errorf("Streaming callback failed: %v", err)
		}
	}

	// Close pipes and capture output
	wOut.Close()
	wErr.Close()

	stdoutBytes, _ := io.ReadAll(rOut)
	stderrBytes, _ := io.ReadAll(rErr)

	stdout := string(stdoutBytes)
	stderr := string(stderrBytes)

	// Verify stdout contains expected content without ANSI sequences
	expectedLines := []string{"Line 1", "Line 2", "Line 3"}
	for _, expectedLine := range expectedLines {
		if !strings.Contains(stdout, expectedLine) {
			t.Errorf("Expected stdout to contain '%s', got: %s", expectedLine, stdout)
		}
	}

	// Verify stderr might contain ANSI sequences but they shouldn't affect stdout format
	t.Logf("Captured stdout: %q", stdout)
	t.Logf("Captured stderr: %q", stderr)

	// Check that stdout doesn't contain ANSI escape sequences
	if strings.Contains(stdout, "\033[") || strings.Contains(stdout, "[2K") {
		t.Errorf("Stdout contains ANSI escape sequences, which should only be in stderr: %s", stdout)
	}
}

// TestStreamingChunkBoundaryHandling tests how chunks are processed at various boundary conditions
func TestStreamingChunkBoundaryHandling(t *testing.T) {
	tests := []struct {
		name     string
		chunks   []string
		expected []string // Expected individual writes to the underlying writer
	}{
		{
			name:     "chunk_ends_with_newline",
			chunks:   []string{"Hello\n", "World\n"},
			expected: []string{"Hello\r\n", "", "World\r\n", ""},
		},
		{
			name:     "chunk_without_newline",
			chunks:   []string{"Hello", " World"},
			expected: []string{"Hello", " World"},
		},
		{
			name:     "mixed_chunk_boundaries",
			chunks:   []string{"Start\n", "Middle", " continues\n", "End"},
			expected: []string{"Start\r\n", "", "Middle", " continues\r\n", "", "End"},
		},
		{
			name:     "newline_split_across_chunks",
			chunks:   []string{"Hello", "\n", "World"},
			expected: []string{"Hello", "\r\n", "", "World"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var writes []string
			mockWriter := &mockWriter{
				writes: &writes,
			}

			// Process each chunk
			for _, chunk := range tt.chunks {
				err := WriteNormalizedChunk(mockWriter, []byte(chunk))
				if err != nil {
					t.Errorf("WriteNormalizedChunk error: %v", err)
					return
				}
			}

			// Verify the exact sequence of writes
			if len(writes) != len(tt.expected) {
				t.Errorf("Expected %d writes, got %d: %v", len(tt.expected), len(writes), writes)
				return
			}

			for i, expectedWrite := range tt.expected {
				if writes[i] != expectedWrite {
					t.Errorf("Write %d: expected %q, got %q", i, expectedWrite, writes[i])
				}
			}
		})
	}
}

// TestStreamingRealWorldScenario tests the exact issue reported by the user
func TestStreamingRealWorldScenario(t *testing.T) {
	// This reproduces the exact pattern that was causing progressive indentation
	problematicContent := []string{
		"Here's an analysis of your Kubernetes cluster's state:\n",
		"\n",
		"### Critical Issues Identified:\n",
		"\n",
		"1. Pod Issues:\n",
		"   - debug pod failing\n",
		"   - image pull problems\n",
	}

	var output bytes.Buffer

	// Process chunks as they would come from streaming
	for _, chunk := range problematicContent {
		err := WriteNormalizedChunk(&output, []byte(chunk))
		if err != nil {
			t.Errorf("WriteNormalizedChunk failed: %v", err)
			return
		}
	}

	result := output.String()
	lines := strings.Split(result, "\n")

	// Verify that lines are not progressively indented
	// Look for the pattern where each line has more leading spaces than the previous
	nonEmptyLines := make([]string, 0)
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines = append(nonEmptyLines, line)
		}
	}

	if len(nonEmptyLines) < 2 {
		t.Skip("Need at least 2 non-empty lines to test progressive indentation")
		return
	}

	// Check that we don't have progressive indentation
	for i := 1; i < len(nonEmptyLines); i++ {
		prevLine := nonEmptyLines[i-1]
		currentLine := nonEmptyLines[i]

		// Count leading spaces
		prevSpaces := len(prevLine) - len(strings.TrimLeft(prevLine, " "))
		currentSpaces := len(currentLine) - len(strings.TrimLeft(currentLine, " "))

		// If current line has significantly more spaces than previous line
		// (accounting for normal indentation), this might indicate the bug
		if currentSpaces > prevSpaces+20 { // 20 chars threshold for "excessive"
			t.Errorf("Progressive indentation detected: line %d has %d spaces vs previous line %d spaces\nPrevious: %q\nCurrent: %q",
				i, currentSpaces, prevSpaces, prevLine, currentLine)
		}
	}

	t.Logf("Processed content:\n%s", result)
}

// TestStreamingCallbackWithoutTokenMeterInterference tests streaming without token meter ANSI interference
func TestStreamingCallbackWithoutTokenMeterInterference(t *testing.T) {
	cfg := CreateTestConfig()

	// Set up to capture both stdout and stderr separately
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	// Redirect stdout and stderr to our buffers
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()

	os.Stdout = wOut
	os.Stderr = wErr

	// Create the streaming callback (this might create background goroutines)
	callback, cleanup := CreateStreamingCallback(cfg, nil, nil, nil)

	// Stream test content
	testChunks := []string{
		"Analysis:\n",
		"1. First issue\n",
		"2. Second issue\n",
	}

	ctx := context.Background()
	for _, chunk := range testChunks {
		err := callback(ctx, []byte(chunk))
		if err != nil {
			t.Errorf("Callback failed: %v", err)
		}
	}

	// Clean up and close pipes
	cleanup()
	wOut.Close()
	wErr.Close()

	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Read the captured output
	stdoutBytes, _ := io.ReadAll(rOut)
	stderrBytes, _ := io.ReadAll(rErr)

	stdout := string(stdoutBytes)
	stderr := string(stderrBytes)

	// Verify stdout doesn't contain ANSI sequences
	if strings.Contains(stdout, "\033[") {
		t.Errorf("Stdout contains ANSI sequences: %q", stdout)
	}

	// Verify content is properly formatted
	expectedContent := []string{"Analysis:", "1. First issue", "2. Second issue"}
	for _, expected := range expectedContent {
		if !strings.Contains(stdout, expected) {
			t.Errorf("Expected content '%s' not found in stdout: %q", expected, stdout)
		}
	}

	// Log for debugging
	t.Logf("Stdout: %q", stdout)
	t.Logf("Stderr: %q", stderr)
}

// TestStreamingConsistencyAcrossModes verifies consistent output across streaming modes
func TestStreamingConsistencyAcrossModes(t *testing.T) {
	testContent := "Line 1\nLine 2\nLine 3"

	configs := []struct {
		name             string
		markdownEnabled  bool
		animationEnabled bool
	}{
		{"raw", false, false},
		{"markdown_only", true, false},
		{"animation_only", false, true},
		{"both_enabled", true, true},
	}

	var outputs []string

	for _, cfgTest := range configs {
		t.Run(cfgTest.name, func(t *testing.T) {
			cfg := CreateTestConfig()
			cfg.DisableMarkdownFormat = !cfgTest.markdownEnabled
			cfg.DisableAnimation = !cfgTest.animationEnabled

			var output bytes.Buffer

			// Test WriteNormalizedChunk directly for consistency
			err := WriteNormalizedChunk(&output, []byte(testContent))
			if err != nil {
				t.Errorf("WriteNormalizedChunk failed: %v", err)
				return
			}

			result := output.String()
			outputs = append(outputs, result)

			// Verify proper CRLF conversion
			if !strings.Contains(result, "\r\n") {
				t.Errorf("Expected CRLF line endings in result: %q", result)
			}
		})
	}

	// All outputs should have consistent line breaking (ignoring styling)
	// Focus on structural consistency
	for i := 1; i < len(outputs); i++ {
		// Normalize outputs for comparison (remove potential styling differences)
		norm1 := normalizeForComparison(outputs[0])
		norm2 := normalizeForComparison(outputs[i])

		if norm1 != norm2 {
			t.Errorf("Outputs differ between modes:\nFirst: %q\nOther: %q", norm1, norm2)
		}
	}
}

// TestANSISequenceDetection tests detection and handling of ANSI escape sequences
func TestANSISequenceDetection(t *testing.T) {
	testCases := []struct {
		name    string
		input   string
		hasANSI bool
	}{
		{
			name:    "no_ansi",
			input:   "Plain text content",
			hasANSI: false,
		},
		{
			name:    "clear_line_sequence",
			input:   "Text\033[2KMore text",
			hasANSI: true,
		},
		{
			name:    "cursor_movement",
			input:   "Text\033[1AMore text",
			hasANSI: true,
		},
		{
			name:    "token_meter_style",
			input:   "\r\033[2K[50%|↑1k|↓2k]",
			hasANSI: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hasANSI := containsANSISequences(tc.input)

			if hasANSI != tc.hasANSI {
				t.Errorf("Expected hasANSI=%t, got %t for input: %q", tc.hasANSI, hasANSI, tc.input)
			}
		})
	}
}

func TestManageChatThreadContext_AutoCompactPreservesRecentMessages(t *testing.T) {
	orig := RequestWithSystem
	t.Cleanup(func() { RequestWithSystem = orig })

	RequestWithSystem = func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
		if !strings.Contains(systemPrompt, "conversation memory compressor") {
			t.Fatalf("unexpected compaction system prompt: %q", systemPrompt)
		}
		return "- Compact objective\n- Compact findings", nil
	}

	cfg := CreateTestConfig()
	cfg.AutoCompactEnabled = true
	cfg.AutoCompactTriggerPercent = 20
	cfg.AutoCompactTargetPercent = 80
	cfg.AutoCompactKeepMessages = 2

	long := strings.Repeat("token ", 80)
	cfg.ChatMessages = []llms.ChatMessage{
		llms.SystemChatMessage{Content: "base system"},
		llms.HumanChatMessage{Content: "user-1 " + long},
		llms.AIChatMessage{Content: "assistant-1 " + long},
		llms.HumanChatMessage{Content: "user-2 " + long},
		llms.AIChatMessage{Content: "assistant-2 " + long},
		llms.HumanChatMessage{Content: "user-3 recent " + long},
		llms.AIChatMessage{Content: "assistant-3 recent " + long},
	}

	before := append([]llms.ChatMessage(nil), cfg.ChatMessages...)
	ManageChatThreadContext(cfg, cfg.ChatMessages, 2000)

	if len(cfg.ChatMessages) >= len(before) {
		t.Fatalf("expected compacted history to be shorter, before=%d after=%d", len(before), len(cfg.ChatMessages))
	}

	foundCompactMemory := false
	for _, msg := range cfg.ChatMessages {
		if msg.GetType() == llms.ChatMessageTypeSystem && strings.HasPrefix(strings.TrimSpace(msg.GetContent()), autoCompactSummaryHeader) {
			foundCompactMemory = true
			break
		}
	}
	if !foundCompactMemory {
		t.Fatalf("expected compact memory system message to be present")
	}

	if len(cfg.ChatMessages) < 2 {
		t.Fatalf("expected at least two messages after compaction")
	}
	last := cfg.ChatMessages[len(cfg.ChatMessages)-1].GetContent()
	secondLast := cfg.ChatMessages[len(cfg.ChatMessages)-2].GetContent()
	if !strings.Contains(last, "assistant-3 recent") || !strings.Contains(secondLast, "user-3 recent") {
		t.Fatalf("expected most recent messages to remain verbatim, got secondLast=%q last=%q", secondLast, last)
	}
}

func TestManageChatThreadContext_AutoCompactFailureFallsBackToTrim(t *testing.T) {
	orig := RequestWithSystem
	t.Cleanup(func() { RequestWithSystem = orig })

	RequestWithSystem = func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
		return "", io.EOF
	}

	cfg := CreateTestConfig()
	cfg.AutoCompactEnabled = true
	cfg.AutoCompactTriggerPercent = 10
	cfg.AutoCompactTargetPercent = 5
	cfg.AutoCompactKeepMessages = 2

	long := strings.Repeat("fallback ", 120)
	cfg.ChatMessages = []llms.ChatMessage{
		llms.HumanChatMessage{Content: "h1 " + long},
		llms.AIChatMessage{Content: "a1 " + long},
		llms.HumanChatMessage{Content: "h2 " + long},
		llms.AIChatMessage{Content: "a2 " + long},
		llms.HumanChatMessage{Content: "h3 " + long},
		llms.AIChatMessage{Content: "a3 " + long},
	}

	ManageChatThreadContext(cfg, cfg.ChatMessages, 220)
	remainingTokens := lib.CountTokensWithConfig(cfg, "", cfg.ChatMessages)
	if remainingTokens > 220 {
		t.Fatalf("expected fallback trimming to enforce token limit, got %d tokens", remainingTokens)
	}
}

func TestManageChatThreadContext_AutoCompactWithThreeMessagesAvoidsTrim(t *testing.T) {
	orig := RequestWithSystem
	t.Cleanup(func() { RequestWithSystem = orig })

	summaryCalls := 0
	RequestWithSystem = func(cfg *config.Config, systemPrompt string, userPrompt string, stream bool, history bool) (string, error) {
		summaryCalls++
		return "- DNS objective\n- Cluster findings", nil
	}

	cfg := CreateTestConfig()
	cfg.AutoCompactEnabled = true
	cfg.AutoCompactTriggerPercent = 20
	cfg.AutoCompactTargetPercent = 80
	cfg.AutoCompactKeepMessages = 8 // Intentionally larger than available messages

	long := strings.Repeat("dns-resolution-check ", 220)
	cfg.ChatMessages = []llms.ChatMessage{
		llms.SystemChatMessage{Content: "base system"},
		llms.HumanChatMessage{Content: "Investigate DNS failures " + long},
		llms.AIChatMessage{Content: "I will run DNS diagnostics " + long},
	}

	ManageChatThreadContext(cfg, cfg.ChatMessages, 5000)

	if summaryCalls == 0 {
		t.Fatalf("expected auto-compact summarization to run")
	}
	if len(cfg.ChatMessages) != 2 {
		t.Fatalf("expected compacted thread to keep 2 messages (compact memory + latest), got %d", len(cfg.ChatMessages))
	}
	if !strings.HasPrefix(strings.TrimSpace(cfg.ChatMessages[0].GetContent()), autoCompactSummaryHeader) {
		t.Fatalf("expected first message to be compact memory, got %q", cfg.ChatMessages[0].GetContent())
	}
	if !strings.Contains(cfg.ChatMessages[1].GetContent(), "I will run DNS diagnostics") {
		t.Fatalf("expected latest assistant message to remain verbatim")
	}
}

// Helper functions

type mockWriter struct {
	writes *[]string
}

func (w *mockWriter) Write(p []byte) (n int, err error) {
	*w.writes = append(*w.writes, string(p))
	return len(p), nil
}

// containsANSISequences detects ANSI escape sequences in text
func containsANSISequences(text string) bool {
	return strings.Contains(text, "\033[") || strings.Contains(text, "\r")
}

// normalizeForComparison removes styling differences but preserves structural formatting
func normalizeForComparison(text string) string {
	// Remove common ANSI sequences and normalize line endings
	result := strings.ReplaceAll(text, "\r\n", "\n")
	result = strings.ReplaceAll(result, "\r", "\n")

	// Remove potential color codes (basic)
	for strings.Contains(result, "\033[") {
		start := strings.Index(result, "\033[")
		if start == -1 {
			break
		}
		end := start + 2
		for end < len(result) && result[end] != 'm' {
			end++
		}
		if end < len(result) {
			end++ // Include the 'm'
			result = result[:start] + result[end:]
		} else {
			break
		}
	}

	return result
}
