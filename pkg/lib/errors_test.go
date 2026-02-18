package lib

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseRetryDelay(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		minDelay  time.Duration
		maxDelay  time.Duration
		shouldErr bool
	}{
		{
			name:      "header retry-after-ms",
			err:       errors.New(`429 Too Many Requests, headers: {"retry-after-ms":"1500"}`),
			minDelay:  1400 * time.Millisecond,
			maxDelay:  1600 * time.Millisecond,
			shouldErr: false,
		},
		{
			name:      "header retry-after-seconds",
			err:       errors.New(`429 Too Many Requests, retry-after: 2`),
			minDelay:  1900 * time.Millisecond,
			maxDelay:  2100 * time.Millisecond,
			shouldErr: false,
		},
		{
			name:      "natural language milliseconds",
			err:       errors.New(`Please retry in 2500ms`),
			minDelay:  2400 * time.Millisecond,
			maxDelay:  2600 * time.Millisecond,
			shouldErr: false,
		},
		{
			name:      "natural language seconds decimal",
			err:       errors.New(`Rate limit exceeded. retry after 1.5 seconds`),
			minDelay:  1400 * time.Millisecond,
			maxDelay:  1600 * time.Millisecond,
			shouldErr: false,
		},
		{
			name:      "no parsable delay",
			err:       errors.New(`429 Too Many Requests`),
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay, err := ParseRetryDelay(tt.err)
			if tt.shouldErr {
				if err == nil {
					t.Fatalf("expected parse error, got delay %v", delay)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			if delay < tt.minDelay || delay > tt.maxDelay {
				t.Fatalf("expected delay in [%v,%v], got %v", tt.minDelay, tt.maxDelay, delay)
			}
		})
	}
}

func TestParseRetryDelay_HTTPDate(t *testing.T) {
	when := time.Now().Add(3 * time.Second).UTC().Format(time.RFC1123)
	when = strings.ReplaceAll(when, "UTC", "GMT")
	delay, err := ParseRetryDelay(errors.New(fmt.Sprintf(`retry-after: %s`, when)))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if delay <= 0 {
		t.Fatalf("expected positive delay, got %v", delay)
	}
	if delay > 7*time.Second {
		t.Fatalf("expected delay <= 7s, got %v", delay)
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{name: "429 retryable", err: errors.New("429 Too Many Requests"), retryable: true},
		{name: "503 retryable", err: errors.New("503 Service Unavailable"), retryable: true},
		{name: "network retryable", err: errors.New("network error: failed to reach API server"), retryable: true},
		{name: "401 non-retryable", err: errors.New("401 Unauthorized"), retryable: false},
		{name: "403 non-retryable", err: errors.New("403 Forbidden"), retryable: false},
		{name: "context overflow non-retryable", err: errors.New("maximum context length exceeded"), retryable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryableError(tt.err)
			if got != tt.retryable {
				t.Fatalf("expected retryable=%t, got %t", tt.retryable, got)
			}
		})
	}
}

func TestHTTPStatusCode(t *testing.T) {
	if code := HTTPStatusCode(errors.New("request failed: 502 bad gateway")); code != 502 {
		t.Fatalf("expected 502, got %d", code)
	}
	if code := HTTPStatusCode(errors.New("no status code in text")); code != 0 {
		t.Fatalf("expected 0, got %d", code)
	}
}
