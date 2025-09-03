package llm

import (
	"testing"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

func TestFixedDelayMode(t *testing.T) {
	// Reset global throttling state for test
	throttleMutex.Lock()
	lastResponseTime = time.Time{}
	requestTimestamps = nil
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 60, // This should be ignored when delay override is set
		ThrottleDelayOverride:     2 * time.Second,
	}

	// In fixed delay mode, EVERY request should be delayed by the override amount
	for i := 0; i < 5; i++ {
		delay := calculateThrottleDelay(cfg)
		if delay != 2*time.Second {
			t.Errorf("Fixed delay mode: request %d should have 2s delay, got: %v", i+1, delay)
		}
	}
}

func TestBurstMode(t *testing.T) {
	// Reset global throttling state for test
	throttleMutex.Lock()
	lastResponseTime = time.Time{}
	requestTimestamps = nil
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 3, // Allow 3 requests per minute
		ThrottleDelayOverride:     0, // No fixed delay
	}

	// First 3 requests should not be delayed (burst)
	for i := 0; i < 3; i++ {
		delay := calculateThrottleDelay(cfg)
		if delay != 0 {
			t.Errorf("Burst mode: request %d should not be delayed, got: %v", i+1, delay)
		}
	}

	// 4th request should be delayed until window expires
	delay := calculateThrottleDelay(cfg)
	if delay == 0 {
		t.Errorf("Burst mode: 4th request should be delayed after burst limit reached")
	}
}

func TestThrottlingDisabled(t *testing.T) {
	// Reset global throttling state for test
	throttleMutex.Lock()
	lastResponseTime = time.Time{}
	requestTimestamps = nil
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 0, // Disabled
		ThrottleDelayOverride:     0, // No fixed delay
	}

	// Multiple requests should have no delay when throttling is disabled
	for i := 0; i < 5; i++ {
		delay := calculateThrottleDelay(cfg)
		if delay != 0 {
			t.Errorf("Disabled throttling: request %d should not be delayed, got: %v", i+1, delay)
		}
	}
}

func TestFixedDelayIgnoresBurstSettings(t *testing.T) {
	// Reset global throttling state
	throttleMutex.Lock()
	lastResponseTime = time.Time{}
	requestTimestamps = nil
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 1000,                   // High burst setting
		ThrottleDelayOverride:     500 * time.Millisecond, // Fixed delay takes precedence
	}

	// Even with high burst setting, fixed delay should always apply
	for i := 0; i < 3; i++ {
		delay := calculateThrottleDelay(cfg)
		if delay != 500*time.Millisecond {
			t.Errorf("Fixed delay should override burst settings, got: %v", delay)
		}
	}
}

func TestBurstWindowExpiry(t *testing.T) {
	// Reset global throttling state
	throttleMutex.Lock()
	lastResponseTime = time.Time{}
	requestTimestamps = nil
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 2, // Allow 2 requests per minute
		ThrottleDelayOverride:     0, // Use burst mode
	}

	// Use up the burst allowance
	for i := 0; i < 2; i++ {
		delay := calculateThrottleDelay(cfg)
		if delay != 0 {
			t.Errorf("Burst mode: request %d should not be delayed, got: %v", i+1, delay)
		}
	}

	// Simulate window expiry by setting old timestamps
	throttleMutex.Lock()
	for i := range requestTimestamps {
		requestTimestamps[i] = time.Now().Add(-61 * time.Second)
	}
	throttleMutex.Unlock()

	// Next request should not be throttled (new window)
	delay := calculateThrottleDelay(cfg)
	if delay != 0 {
		t.Errorf("Burst mode: request after window expiry should not be delayed, got: %v", delay)
	}
}
