package logger

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestInitLoggers(t *testing.T) {
	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Initialize loggers with our buffer
	InitLoggers(&buf, 0)

	// Verify that all expected loggers are created
	requiredLoggers := []string{
		"info", "warn", "err",
		"llmIn", "llmOut", "in", "out",
	}

	for _, loggerName := range requiredLoggers {
		logger, exists := LoggerMap[loggerName]
		if !exists {
			t.Errorf("Logger '%s' was not created", loggerName)
			continue
		}

		if logger == nil {
			t.Errorf("Logger '%s' is nil", loggerName)
		}
	}
}

func TestLog(t *testing.T) {
	// Save original DEBUG env var
	originalDebug := os.Getenv("DEBUG")

	// Enable debug mode for testing
	os.Setenv("DEBUG", "true")
	DEBUG = true

	// Cleanup after test
	defer func() {
		if originalDebug != "" {
			os.Setenv("DEBUG", originalDebug)
		} else {
			os.Unsetenv("DEBUG")
		}
		DEBUG = originalDebug == "true" || originalDebug == "1"
	}()

	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Initialize loggers with our buffer
	InitLoggers(&buf, 0)

	// Test logging with various levels
	testCases := []struct {
		level       string
		format      string
		args        []interface{}
		shouldMatch string
	}{
		{"info", "Test info message: %s", []interface{}{"detail"}, "INFO: Test info message: detail"},
		{"warn", "Warning: %d issues", []interface{}{42}, "WARN: Warning: 42 issues"},
		{"err", "Error occurred: %v", []interface{}{"file not found"}, "ERR: Error occurred: file not found"},
		{"llmIn", "LLM input: %s", []interface{}{"prompt text"}, "[LLM] > LLM input: prompt text"},
		{"llmOut", "LLM response: %s", []interface{}{"generated text"}, "[LLM] < LLM response: generated text"},
		{"in", "User input: %s", []interface{}{"command"}, "> User input: command"},
		{"out", "Output: %s", []interface{}{"result"}, "< Output: result"},
		{"unknown", "Unknown level message", []interface{}{}, "INFO: Unknown level message"}, // Should default to info
	}

	for _, tc := range testCases {
		t.Run(tc.level, func(t *testing.T) {
			// Clear the buffer
			buf.Reset()

			// Call the Log function
			Log(tc.level, tc.format, tc.args...)

			// Get the log output
			output := buf.String()

			// Verify the output contains the expected string
			if !strings.Contains(output, tc.shouldMatch) {
				t.Errorf("Expected log to contain '%s', got '%s'", tc.shouldMatch, output)
			}
		})
	}
}

func TestDebugFlag(t *testing.T) {
	// Save original DEBUG env var
	originalDebug := os.Getenv("DEBUG")

	// Cleanup after test
	defer func() {
		if originalDebug != "" {
			os.Setenv("DEBUG", originalDebug)
		} else {
			os.Unsetenv("DEBUG")
		}
		DEBUG = originalDebug == "true" || originalDebug == "1"
	}()

	// Create a buffer to capture log output
	var buf bytes.Buffer

	// Test with DEBUG disabled
	os.Unsetenv("DEBUG")
	DEBUG = false
	InitLoggers(&buf, 0)

	Log("info", "This shouldn't be logged")
	if buf.Len() > 0 {
		t.Errorf("Expected empty buffer when DEBUG is disabled, got: %s", buf.String())
	}

	// Test with DEBUG enabled
	os.Setenv("DEBUG", "true")
	DEBUG = true
	InitLoggers(&buf, 0)

	Log("info", "This should be logged")
	if buf.Len() == 0 {
		t.Errorf("Expected log output when DEBUG is enabled, got empty buffer")
	}
}

func TestLogWriter(t *testing.T) {
	// Save original DEBUG env var
	originalDebug := os.Getenv("DEBUG")

	// Enable debug mode for testing
	os.Setenv("DEBUG", "true")
	DEBUG = true

	// Cleanup after test
	defer func() {
		if originalDebug != "" {
			os.Setenv("DEBUG", originalDebug)
		} else {
			os.Unsetenv("DEBUG")
		}
		DEBUG = originalDebug == "true" || originalDebug == "1"
	}()

	// Initialize loggers with a temp buffer to ensure LoggerMap is populated
	tempBuf := bytes.Buffer{}
	InitLoggers(&tempBuf, 0)

	// Create a custom logWriter
	writer := &logWriter{
		logger: LoggerMap["info"], // Use the initialized logger
		prefix: "TEST: ",
		color: func(a ...interface{}) string {
			// Return the input without color for testing
			if len(a) == 0 {
				return ""
			}
			return a[0].(string)
		},
	}

	// Test writing a multi-line message
	message := "Line 1\nLine 2\nLine 3"
	n, err := writer.Write([]byte(message))

	// Verify no error and correct byte count
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if n != len(message) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(message), n)
	}
}
