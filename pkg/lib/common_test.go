package lib

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	testCases := []struct {
		name      string
		input     string
		minTokens int // Use min/max range instead of specific expected count
		maxTokens int
	}{
		{
			name:      "empty string",
			input:     "",
			minTokens: 0,
			maxTokens: 0,
		},
		{
			name:      "simple text",
			input:     "Hello, world!",
			minTokens: 2,
			maxTokens: 5, // Allow for implementation differences
		},
		{
			name:      "longer text",
			input:     "This is a longer piece of text that should be tokenized into multiple tokens by the tokenizer.",
			minTokens: 10,
			maxTokens: 20, // Allow for implementation differences
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tokens := Tokenize(tc.input)

			// For empty string, we should get 0 tokens
			if tc.input == "" && len(tokens) != 0 {
				t.Errorf("Expected 0 tokens for empty string, got %d", len(tokens))
			}

			// For non-empty strings, we should get at least one token
			if tc.input != "" && len(tokens) == 0 {
				t.Errorf("Expected at least one token for non-empty string, got 0")
			}

			// Check token count is within expected range
			if len(tokens) < tc.minTokens || len(tokens) > tc.maxTokens {
				t.Errorf("Expected between %d and %d tokens, got %d",
					tc.minTokens, tc.maxTokens, len(tokens))
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	testCases := []struct {
		name     string
		input    int64 // milliseconds
		expected string
	}{
		{
			name:     "zero milliseconds",
			input:    0,
			expected: "0ms",
		},
		{
			name:     "small milliseconds",
			input:    123,
			expected: "123ms",
		},
		{
			name:     "just under a second",
			input:    999,
			expected: "999ms",
		},
		{
			name:     "one second",
			input:    1000,
			expected: "1.0s",
		},
		{
			name:     "one and a half seconds",
			input:    1500,
			expected: "1.5s",
		},
		{
			name:     "large number of seconds",
			input:    12345,
			expected: "12.3s",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatDuration(tc.input)

			if result != tc.expected {
				t.Errorf("Expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}
