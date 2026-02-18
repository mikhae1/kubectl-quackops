package lib

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"errors"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

var httpStatusCodePattern = regexp.MustCompile(`\b([1-5]\d{2})\b`)

// Is429Error checks if the error is a 429 rate limit error
func Is429Error(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") || strings.Contains(errStr, "too many requests")
}

// HTTPStatusCode extracts an HTTP status code from an error string, returning 0 when absent.
func HTTPStatusCode(err error) int {
	if err == nil {
		return 0
	}
	matches := httpStatusCodePattern.FindStringSubmatch(err.Error())
	if len(matches) < 2 {
		return 0
	}
	code, parseErr := strconv.Atoi(matches[1])
	if parseErr != nil {
		return 0
	}
	return code
}

// IsRetryableError classifies whether an LLM/provider error is worth retrying.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if IsUserCancel(err) {
		return false
	}
	if Is429Error(err) {
		return true
	}

	status := HTTPStatusCode(err)
	if status != 0 {
		switch status {
		case 408, 409, 425, 429, 500, 502, 503, 504:
			return true
		default:
			if status >= 400 && status < 500 {
				return false
			}
		}
	}

	es := strings.ToLower(err.Error())
	nonRetryableMarkers := []string{
		"invalid credentials",
		"invalid api key",
		"unauthorized",
		"forbidden",
		"insufficient credits",
		"bad request",
		"not found",
		"model not found",
		"context length exceeded",
		"context window exceeded",
		"maximum context length",
	}
	for _, marker := range nonRetryableMarkers {
		if strings.Contains(es, marker) {
			return false
		}
	}

	retryableMarkers := []string{
		"network error",
		"no such host",
		"connection refused",
		"connection reset",
		"timeout",
		"timed out",
		"temporarily unavailable",
		"service unavailable",
		"tls handshake timeout",
		"resource exhausted",
		"overloaded",
		"try again",
		"eof",
	}
	for _, marker := range retryableMarkers {
		if strings.Contains(es, marker) {
			return true
		}
	}

	// Default to retryable to preserve current behavior for unknown transient errors.
	return true
}

// ParseRetryDelay attempts to extract retry delay from 429 error messages
// Returns the parsed delay and nil error on success, or zero duration and error on failure
func ParseRetryDelay(err error) (time.Duration, error) {
	if err == nil {
		return 0, fmt.Errorf("no error provided to parse")
	}

	errStr := err.Error()

	// Parse provider header-style hints first (retry-after-ms, retry-after seconds/date).
	if delay, ok := parseRetryHeaderDelay(errStr); ok {
		return delay, nil
	}

	// Patterns to match natural-language retry delay information.
	patterns := []struct {
		pattern string
		unit    time.Duration
	}{
		{pattern: `retry.*after\s+(\d+(?:\.\d+)?)\s*ms`, unit: time.Millisecond},
		{pattern: `wait\s+(\d+(?:\.\d+)?)\s*ms`, unit: time.Millisecond},
		{pattern: `retry.*in\s+(\d+(?:\.\d+)?)\s*ms`, unit: time.Millisecond},
		{pattern: `retry.*after\s+(\d+(?:\.\d+)?)\s*second`, unit: time.Second},
		{pattern: `wait\s+(\d+(?:\.\d+)?)\s*second`, unit: time.Second},
		{pattern: `retry.*in\s+(\d+(?:\.\d+)?)\s*second`, unit: time.Second},
		{pattern: `try.*again.*in\s+(\d+(?:\.\d+)?)\s*second`, unit: time.Second},
		{pattern: `please.*retry.*after\s+(\d+(?:\.\d+)?)\s*second`, unit: time.Second},
	}

	for _, p := range patterns {
		re := regexp.MustCompile(`(?i)` + p.pattern)
		if matches := re.FindStringSubmatch(errStr); len(matches) > 1 {
			amount, parseErr := strconv.ParseFloat(matches[1], 64)
			if parseErr == nil && amount > 0 {
				return time.Duration(amount * float64(p.unit)), nil
			}
		}
	}

	// Return error if no specific delay found - let caller decide fallback strategy
	return 0, fmt.Errorf("could not parse retry delay from error message: %s", errStr)
}

func parseRetryHeaderDelay(errStr string) (time.Duration, bool) {
	if strings.TrimSpace(errStr) == "" {
		return 0, false
	}

	retryAfterMS := regexp.MustCompile(`(?i)["']?retry-after-ms["']?\s*[:=]\s*["']?([0-9]+(?:\.[0-9]+)?)["']?`)
	if matches := retryAfterMS.FindStringSubmatch(errStr); len(matches) > 1 {
		ms, err := strconv.ParseFloat(matches[1], 64)
		if err == nil && ms > 0 {
			return time.Duration(ms * float64(time.Millisecond)), true
		}
	}

	retryAfterDate := regexp.MustCompile(`(?i)["']?retry-after["']?\s*[:=]\s*["']?([A-Za-z]{3},\s*\d{1,2}\s+[A-Za-z]{3}\s+\d{4}\s+\d{2}:\d{2}:\d{2}\s+(?:GMT|UTC|[+-]\d{4}))["']?`)
	if matches := retryAfterDate.FindStringSubmatch(errStr); len(matches) > 1 {
		value := strings.TrimSpace(matches[1])
		layouts := []string{time.RFC1123, time.RFC1123Z}
		for _, layout := range layouts {
			when, err := time.Parse(layout, value)
			if err != nil {
				continue
			}
			delay := time.Until(when)
			if delay > 0 {
				return delay, true
			}
			break
		}
	}

	retryAfterSeconds := regexp.MustCompile(`(?i)["']?retry-after["']?\s*[:=]\s*["']?([0-9]+(?:\.[0-9]+)?)["']?`)
	if matches := retryAfterSeconds.FindStringSubmatch(errStr); len(matches) > 1 {
		seconds, err := strconv.ParseFloat(matches[1], 64)
		if err == nil && seconds > 0 {
			return time.Duration(seconds * float64(time.Second)), true
		}
	}

	return 0, false
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
	fmt.Printf("\n%s\n", config.Colors.Error.Sprint("⚠️  Rate Limit Exceeded"))
	fmt.Printf("Provider: %s\n", config.Colors.Provider.Sprint(cfg.Provider))
	fmt.Printf("Model: %s\n", config.Colors.Model.Sprint(cfg.Model))

	// Only show retry information if retries are enabled
	if maxRetries > 0 {
		if parseErr == nil {
			fmt.Printf("Retry after: %s (parsed from provider response)\n", config.Colors.Warn.Sprint(delay.String()))
		} else {
			fmt.Printf("Retry strategy: exponential backoff (couldn't parse provider delay)\n")
			logger.Log("debug", "Failed to parse retry delay: %v", parseErr)
		}
	}

	fmt.Printf("Raw error: %s\n", config.Colors.Dim.Sprint(err.Error()))

	fmt.Printf("\n%s\n", config.Colors.Warn.Sprint("💡 Suggestions:"))
	fmt.Printf("  • Wait for the retry period to expire\n")
	fmt.Printf("  • Consider using a different model or provider\n")
	fmt.Printf("  • Check your API quota and billing status\n")
	fmt.Printf("  • Enable throttling with %s flag\n", config.Colors.AccentAlt.Sprint("--throttle-rpm"))
	fmt.Printf("\n")
}

// GetErrorMessage preserves the original error and appends a known code hint.
func GetErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	errorStr := err.Error()

	// Extract HTTP status code from error message
	code := HTTPStatusCode(err)
	if code == 0 {
		// No recognizable HTTP code; return the original error
		return errorStr
	}

	var hint string
	switch code {
	case 400:
		hint = "Bad Request (invalid or missing params)"
	case 401:
		hint = "Invalid credentials (OAuth session expired, disabled/invalid API key)"
	case 402:
		hint = "Your account or API key has insufficient credits. Add more credits and retry the request."
	case 403:
		hint = "Your chosen model requires moderation and your input was flagged"
	case 408:
		hint = "Your request timed out"
	case 502:
		hint = "Your chosen model is down or we received an invalid response from it"
	case 503:
		hint = "There is no available model provider that meets your routing requirements"
	default:
		// Unrecognized code; return original error as-is
		return errorStr
	}

	return fmt.Sprintf("%s (%d: %s)", errorStr, code, hint)
}

// UserCancelError indicates that the user explicitly cancelled the in-flight operation (e.g., by pressing ESC)
type UserCancelError struct {
	Reason string
}

func (e *UserCancelError) Error() string {
	if strings.TrimSpace(e.Reason) != "" {
		return e.Reason
	}
	return "user cancelled request"
}

// NewUserCancelError creates a new user cancel error with an optional reason
func NewUserCancelError(reason string) error {
	return &UserCancelError{Reason: reason}
}

// IsUserCancel returns true if error is a user cancellation
func IsUserCancel(err error) bool {
	if err == nil {
		return false
	}
	var uce *UserCancelError
	if errors.As(err, &uce) {
		return true
	}
	// Fallback: some providers may return context cancellation messages
	es := strings.ToLower(err.Error())
	return strings.Contains(es, "context canceled") || strings.Contains(es, "canceled by user")
}
