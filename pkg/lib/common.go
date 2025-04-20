package lib

import (
	"fmt"

	"github.com/pkoukk/tiktoken-go"
)

// Tokenize uses tiktoken-go to perform proper tokenization compatible with LLMs.
func Tokenize(text string) []string {
	encoding := "cl100k_base" // Default encoding used by many LLMs
	tke, err := tiktoken.GetEncoding(encoding)
	if err != nil {
		// Fallback to simple split if there's an error
		return []string{text}
	}

	tokens := tke.Encode(text, nil, nil)

	// Convert the uint32 tokens to strings for backward compatibility
	tokenStrs := make([]string, len(tokens))
	for i, t := range tokens {
		tokenStrs[i] = fmt.Sprintf("%d", t)
	}

	return tokenStrs
}

// FormatDuration formats a duration in milliseconds to a human-readable string
// showing seconds if the duration is more than 1000ms
func FormatDuration(milliseconds int64) string {
	if milliseconds >= 1000 {
		seconds := float64(milliseconds) / 1000.0
		return fmt.Sprintf("%.1fs", seconds)
	}
	return fmt.Sprintf("%dms", milliseconds)
}
