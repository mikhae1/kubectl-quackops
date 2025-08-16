package llm

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

var (
	// Global throttling state
	throttleMutex     sync.Mutex
	lastResponseTime  time.Time
	requestCount      int
	windowStartTime   time.Time
	requestTimestamps []time.Time // Track individual request timestamps in the current window
)

// calculateThrottleDelay calculates the delay needed based on configuration
// Two modes:
// 1. Fixed delay mode: when QU_THROTTLE_DELAY_OVERRIDE_MS is set, apply that delay between all calls
// 2. Burst mode: when QU_THROTTLE_REQUESTS_PER_MINUTE is set, use burst-then-wait logic
func calculateThrottleDelay(cfg *config.Config) time.Duration {
	throttleMutex.Lock()
	defer throttleMutex.Unlock()

	now := time.Now()

	// Mode 1: Fixed delay override - apply delay between all calls
	if cfg.ThrottleDelayOverride > 0 {
		logger.Log("info", "Using fixed delay throttling: %v between all calls", cfg.ThrottleDelayOverride)
		return cfg.ThrottleDelayOverride
	}

	// Mode 2: Burst throttling - allow N requests immediately, then wait for window
	if cfg.ThrottleRequestsPerMinute <= 0 {
		// Throttling disabled
		return 0
	}

	maxRequestsPerMinute := cfg.ThrottleRequestsPerMinute

	// Clean up old timestamps that are outside the 60-second window
	cutoff := now.Add(-time.Minute)
	var validTimestamps []time.Time
	for _, ts := range requestTimestamps {
		if ts.After(cutoff) {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	requestTimestamps = validTimestamps

	// If this is the first request or we have room in the current minute window
	if len(requestTimestamps) < maxRequestsPerMinute {
		// Add this request timestamp to the window
		requestTimestamps = append(requestTimestamps, now)
		logger.Log("info", "Burst throttling: request %d of %d in current minute window - no delay",
			len(requestTimestamps), maxRequestsPerMinute)
		return 0
	}

	// We've hit the limit for this minute window
	// Find the oldest timestamp in our window and calculate when we can make the next request
	oldestTimestamp := requestTimestamps[0]
	for _, ts := range requestTimestamps {
		if ts.Before(oldestTimestamp) {
			oldestTimestamp = ts
		}
	}

	// Calculate when the oldest request will be 2*60 seconds old for safety
	nextAvailableTime := oldestTimestamp.Add(time.Minute)
	delay := 2 * nextAvailableTime.Sub(now)

	if delay <= 0 {
		// This shouldn't happen due to our cleanup above, but just in case
		requestTimestamps = append(requestTimestamps, now)
		logger.Log("info", "Burst throttling: window expired, allowing request - no delay")
		return 0
	}

	logger.Log("info", "Burst throttling: %d/%d requests in minute window, delaying %v until window expires",
		len(requestTimestamps), maxRequestsPerMinute, delay)

	return delay
}

// Cool throttling messages to display during rate limiting
var throttleMessages = []string{
	"🐌 Taking it slow to respect API limits...",
	"⏳ Pacing requests like a zen master...",
	"🎯 Maintaining optimal request velocity...",
	"🌊 Riding the rate limit waves smoothly...",
	"⚡ Throttling at light speed...",
	"🔄 Cycling through the cool-down period...",
	"🎭 Performing the ancient art of patience...",
	"🚦 Waiting for the green light from rate limits...",
	"⏱️ Syncing with the rhythm of the API...",
	"🎪 Balancing on the tightrope of rate limits...",
	"🌟 Stardust settling before next request...",
	"🎵 Dancing to the beat of throttled requests...",
	"🧘 Meditating between API calls...",
	"🎪 The rate limit circus is in town...",
	"⚖️ Keeping the request-response karma balanced...",
}

// applyThrottleDelay applies the calculated delay before making an LLM request with cool spinner messages
func applyThrottleDelay(cfg *config.Config) {
	applyThrottleDelayWithSpinner(cfg, nil)
}

// applyThrottleDelayWithSpinner applies throttling delay with enhanced spinner messaging
func applyThrottleDelayWithSpinner(cfg *config.Config, s *spinner.Spinner) {
	applyThrottleDelayWithCustomMessage(cfg, s, "")
}

// applyThrottleDelayWithCustomMessage applies throttling delay with a custom message or random if empty
func applyThrottleDelayWithCustomMessage(cfg *config.Config, s *spinner.Spinner, customMessage string) {
	delay := calculateThrottleDelay(cfg)
	if delay > 0 {
		logger.Log("info", "Applying throttle delay: %v", delay)

		if s != nil {
			// Use custom message or pick a random cool message
			var message string
			if customMessage != "" {
				message = customMessage
			} else {
				message = throttleMessages[rand.Intn(len(throttleMessages))]
			}
			originalSuffix := s.Suffix

			// Show throttling message with countdown
			start := time.Now()
			for {
				elapsed := time.Since(start)
				remaining := delay - elapsed
				if remaining <= 0 {
					break
				}

				s.Suffix = fmt.Sprintf(" %s (%.1fs remaining)", message, remaining.Seconds())
				time.Sleep(100 * time.Millisecond)
			}

			// Restore original spinner message
			s.Suffix = originalSuffix
		} else {
			// Fallback to simple sleep if no spinner provided
			time.Sleep(delay)
		}

		// Note: lastResponseTime will be updated separately when the response is received
	}
}

// updateResponseTime updates the last response timestamp for throttling calculations
// In the new burst-then-wait system, this mainly serves as a record-keeping function
func updateResponseTime() {
	throttleMutex.Lock()
	defer throttleMutex.Unlock()
	lastResponseTime = time.Now()
	logger.Log("debug", "Updated throttling response timestamp: %v", lastResponseTime)
}
