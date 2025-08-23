package lib

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

// Is429Error checks if the error is a 429 rate limit error
func Is429Error(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "too many requests")
}

// ParseRetryDelay attempts to extract retry delay from 429 error messages
// Returns the parsed delay and nil error on success, or zero duration and error on failure
func ParseRetryDelay(err error) (time.Duration, error) {
	if err == nil {
		return 0, fmt.Errorf("no error provided to parse")
	}

	errStr := err.Error()

	// Patterns to match retry delay information
	patterns := []string{
		`retry.*after\s+(\d+)\s*second`,
		`wait\s+(\d+)\s*second`,
		`retry.*in\s+(\d+)\s*second`,
		`try.*again.*in\s+(\d+)\s*second`,
		`please.*retry.*after\s+(\d+)\s*second`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(`(?i)` + pattern)
		if matches := re.FindStringSubmatch(errStr); len(matches) > 1 {
			if seconds, parseErr := strconv.Atoi(matches[1]); parseErr == nil && seconds > 0 {
				return time.Duration(seconds) * time.Second, nil
			}
		}
	}

	// Return error if no specific delay found - let caller decide fallback strategy
	return 0, fmt.Errorf("could not parse retry delay from error message: %s", errStr)
}

// Display429Error shows enhanced error information for rate limit errors
func Display429Error(err error, cfg *config.Config, maxRetries int) {
	if err == nil {
		return
	}

	// Debug log the raw error message
	logger.Log("debug", "429 Rate limit error received: %s", err.Error())

	delay, parseErr := ParseRetryDelay(err)

	// Display formatted rate limit information
	fmt.Printf("\n%s\n", color.RedString("⚠️  Rate Limit Exceeded"))
	fmt.Printf("Provider: %s\n", color.CyanString(cfg.Provider))
	fmt.Printf("Model: %s\n", color.CyanString(cfg.Model))

	// Only show retry information if retries are enabled
	if maxRetries > 0 {
		if parseErr == nil {
			fmt.Printf("Retry after: %s (parsed from provider response)\n", color.YellowString(delay.String()))
		} else {
			fmt.Printf("Retry strategy: exponential backoff (couldn't parse provider delay)\n")
			logger.Log("debug", "Failed to parse retry delay: %v", parseErr)
		}
	}

	fmt.Printf("Raw error: %s\n", color.HiBlackString(err.Error()))

	fmt.Printf("\n%s\n", color.YellowString("💡 Suggestions:"))
	fmt.Printf("  • Wait for the retry period to expire\n")
	fmt.Printf("  • Consider using a different model or provider\n")
	fmt.Printf("  • Check your API quota and billing status\n")
	fmt.Printf("  • Enable throttling with %s flag\n", color.CyanString("--throttle-rpm"))
	fmt.Printf("\n")
}

// GetErrorMessage returns a simple error message based on HTTP status codes
func GetErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	errorStr := err.Error()

	// Extract HTTP status code from error message
	re := regexp.MustCompile(`\b([4-5]\d{2})\b`)
	matches := re.FindStringSubmatch(errorStr)

	if len(matches) < 2 {
		return ""
	}

	code, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil {
		return ""
	}

	switch code {
	case 400:
		return "Bad Request (invalid or missing params, CORS)"
	case 401:
		return "Invalid credentials (OAuth session expired, disabled/invalid API key)"
	case 402:
		return "Your account or API key has insufficient credits. Add more credits and retry the request."
	case 403:
		return "Your chosen model requires moderation and your input was flagged"
	case 408:
		return "Your request timed out"
	case 502:
		return "Your chosen model is down or we received an invalid response from it"
	case 503:
		return "There is no available model provider that meets your routing requirements"
	default:
		return ""
	}
}
