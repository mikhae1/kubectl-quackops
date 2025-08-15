package llm

import (
	"testing"
	"time"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

func TestCalculateThrottleDelay(t *testing.T) {
	// Reset global throttling state for test
	throttleMutex.Lock()
	lastRequestTime = time.Time{}
	requestCount = 0
	windowStartTime = time.Time{}
	throttleMutex.Unlock()

	tests := []struct {
		name                    string
		requestsPerMinute       int
		delayOverride           time.Duration
		expectedDelayRange      [2]time.Duration // min, max
		consecutiveRequestCount int
	}{
		{
			name:                    "No throttling",
			requestsPerMinute:       0,
			delayOverride:          0,
			expectedDelayRange:     [2]time.Duration{0, 0},
			consecutiveRequestCount: 1,
		},
		{
			name:                    "Delay override",
			requestsPerMinute:       60,
			delayOverride:          500 * time.Millisecond,
			expectedDelayRange:     [2]time.Duration{500 * time.Millisecond, 500 * time.Millisecond},
			consecutiveRequestCount: 1,
		},
		{
			name:                    "60 requests per minute - first request",
			requestsPerMinute:       60,
			delayOverride:          0,
			expectedDelayRange:     [2]time.Duration{0, 0},
			consecutiveRequestCount: 1,
		},
		{
			name:                    "60 requests per minute - second request",
			requestsPerMinute:       60,
			delayOverride:          0,
			expectedDelayRange:     [2]time.Duration{900 * time.Millisecond, 1100 * time.Millisecond}, // ~1 second
			consecutiveRequestCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global throttling state for each test
			throttleMutex.Lock()
			lastRequestTime = time.Time{}
			requestCount = 0
			windowStartTime = time.Time{}
			throttleMutex.Unlock()

			cfg := &config.Config{
				ThrottleRequestsPerMinute: tt.requestsPerMinute,
				ThrottleDelayOverride:     tt.delayOverride,
			}

			for i := 0; i < tt.consecutiveRequestCount; i++ {
				delay := calculateThrottleDelay(cfg)
				
				// For the last request, check if delay is in expected range
				if i == tt.consecutiveRequestCount-1 {
					if delay < tt.expectedDelayRange[0] || delay > tt.expectedDelayRange[1] {
						t.Errorf("calculateThrottleDelay() = %v, expected range [%v, %v]", 
							delay, tt.expectedDelayRange[0], tt.expectedDelayRange[1])
					}
				}
			}
		})
	}
}

func TestFirstRequestNoThrottling(t *testing.T) {
	// Reset global throttling state
	throttleMutex.Lock()
	lastRequestTime = time.Time{}
	requestCount = 0
	windowStartTime = time.Time{}
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 60, // 1 request per second
		ThrottleDelayOverride:     0,
	}

	// First request should never be throttled
	delay := calculateThrottleDelay(cfg)
	if delay != 0 {
		t.Errorf("First request should not be throttled, got delay: %v", delay)
	}
}

func TestNoThrottlingAfterLongDelay(t *testing.T) {
	// Reset global throttling state
	throttleMutex.Lock()
	lastRequestTime = time.Time{}
	requestCount = 0
	windowStartTime = time.Time{}
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 60, // 1 request per second
		ThrottleDelayOverride:     0,
	}

	// First request
	delay := calculateThrottleDelay(cfg)
	if delay != 0 {
		t.Errorf("First request should not be throttled, got delay: %v", delay)
	}

	// Simulate a long delay (2 seconds) by manually setting lastRequestTime
	throttleMutex.Lock()
	lastRequestTime = time.Now().Add(-2 * time.Second)
	throttleMutex.Unlock()

	// Second request after long delay should not be throttled
	delay = calculateThrottleDelay(cfg)
	if delay != 0 {
		t.Errorf("Request after long delay should not be throttled, got delay: %v", delay)
	}
}

func TestThrottlingOnlyWhenNeeded(t *testing.T) {
	// Reset global throttling state
	throttleMutex.Lock()
	lastRequestTime = time.Time{}
	requestCount = 0
	windowStartTime = time.Time{}
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 120, // 2 requests per second (500ms interval)
		ThrottleDelayOverride:     0,
	}

	// First request - no throttling
	delay := calculateThrottleDelay(cfg)
	if delay != 0 {
		t.Errorf("First request should not be throttled, got delay: %v", delay)
	}

	// Immediate second request - should be throttled
	delay = calculateThrottleDelay(cfg)
	if delay <= 0 {
		t.Errorf("Immediate second request should be throttled, got delay: %v", delay)
	}

	// Verify delay is approximately 500ms
	expectedDelay := 500 * time.Millisecond
	tolerance := 50 * time.Millisecond
	if delay < expectedDelay-tolerance || delay > expectedDelay+tolerance {
		t.Errorf("Throttle delay = %v, expected ~%v", delay, expectedDelay)
	}
}

func TestApplyThrottleDelay(t *testing.T) {
	// Reset global throttling state for test
	throttleMutex.Lock()
	lastRequestTime = time.Time{}
	requestCount = 0
	windowStartTime = time.Time{}
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 120, // 2 requests per second
		ThrottleDelayOverride:     0,
	}

	start := time.Now()
	
	// First request should have no delay
	applyThrottleDelay(cfg)
	firstDuration := time.Since(start)
	
	// Second request should have ~500ms delay
	start = time.Now()
	applyThrottleDelay(cfg)
	secondDuration := time.Since(start)

	// First request should be very fast (< 10ms)
	if firstDuration > 10*time.Millisecond {
		t.Errorf("First request took too long: %v", firstDuration)
	}

	// Second request should take approximately 500ms (allow some tolerance)
	expectedDelay := 500 * time.Millisecond
	tolerance := 100 * time.Millisecond
	if secondDuration < expectedDelay-tolerance || secondDuration > expectedDelay+tolerance {
		t.Errorf("Second request delay = %v, expected ~%v", secondDuration, expectedDelay)
	}
}

func TestApplyThrottleDelayWithSpinner(t *testing.T) {
	// Reset global throttling state for test
	throttleMutex.Lock()
	lastRequestTime = time.Time{}
	requestCount = 0
	windowStartTime = time.Time{}
	throttleMutex.Unlock()

	cfg := &config.Config{
		ThrottleRequestsPerMinute: 120, // 2 requests per second
		ThrottleDelayOverride:     0,
	}

	// Test with nil spinner (should work like regular function)
	start := time.Now()
	applyThrottleDelayWithSpinner(cfg, nil)
	firstDuration := time.Since(start)

	// First request should be very fast (< 10ms)
	if firstDuration > 10*time.Millisecond {
		t.Errorf("First request with nil spinner took too long: %v", firstDuration)
	}

	// Test with actual spinner - second request should have delay
	start = time.Now()
	applyThrottleDelayWithSpinner(cfg, nil) // Still nil for test simplicity
	secondDuration := time.Since(start)

	// Second request should take approximately 500ms (allow some tolerance)
	expectedDelay := 500 * time.Millisecond
	tolerance := 100 * time.Millisecond
	if secondDuration < expectedDelay-tolerance || secondDuration > expectedDelay+tolerance {
		t.Errorf("Second request delay with spinner = %v, expected ~%v", secondDuration, expectedDelay)
	}
}