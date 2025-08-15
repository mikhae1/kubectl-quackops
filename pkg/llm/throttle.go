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
	throttleMutex    sync.Mutex
	lastRequestTime  time.Time
	requestCount     int
	windowStartTime  time.Time
)

// calculateThrottleDelay calculates the delay needed based on requests per minute
// Returns the delay duration that should be applied before making the request
func calculateThrottleDelay(cfg *config.Config) time.Duration {
	throttleMutex.Lock()
	defer throttleMutex.Unlock()

	now := time.Now()

	// If delay override is set, use it directly
	if cfg.ThrottleDelayOverride > 0 {
		logger.Log("info", "Using throttle delay override: %v", cfg.ThrottleDelayOverride)
		lastRequestTime = now // Update last request time even for override
		return cfg.ThrottleDelayOverride
	}

	// If throttling is disabled (0 requests per minute), no delay
	if cfg.ThrottleRequestsPerMinute <= 0 {
		lastRequestTime = now // Update last request time
		return 0
	}

	// Calculate target delay between requests
	targetDelay := time.Minute / time.Duration(cfg.ThrottleRequestsPerMinute)

	// If this is the very first request ever (lastRequestTime is zero), no delay needed
	if lastRequestTime.IsZero() {
		lastRequestTime = now
		windowStartTime = now
		requestCount = 1
		logger.Log("info", "First LLM request - no throttling delay")
		return 0
	}

	// Calculate how much time has passed since the last request
	timeSinceLastRequest := now.Sub(lastRequestTime)
	
	// If enough time has passed since the last request, no additional delay needed
	if timeSinceLastRequest >= targetDelay {
		lastRequestTime = now
		
		// Reset window if more than a minute has passed
		if now.Sub(windowStartTime) >= time.Minute {
			windowStartTime = now
			requestCount = 1
		} else {
			requestCount++
		}
		
		logger.Log("info", "Sufficient time elapsed since last request (%v >= %v) - no throttling delay", 
			timeSinceLastRequest, targetDelay)
		return 0
	}

	// Calculate the required delay
	delay := targetDelay - timeSinceLastRequest
	
	// Update counters (the actual request will happen after the delay)
	// Reset window if more than a minute has passed
	if now.Sub(windowStartTime) >= time.Minute {
		windowStartTime = now
		requestCount = 1
	} else {
		requestCount++
	}
	
	logger.Log("info", "Throttling LLM request: %d requests in window, delaying %v (time since last: %v, target: %v)", 
		requestCount, delay, timeSinceLastRequest, targetDelay)
	
	return delay
}

// Cool throttling messages to display during rate limiting
var throttleMessages = []string{
	"🐌 Taking it slow to respect API limits...",
	"⏳ Pacing requests like a zen master...",
	"🎯 Maintaining optimal request velocity...",
	"🌊 Riding the rate limit waves smoothly...",
	"⚡ Throttling at light speed (ironically)...", 
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
	delay := calculateThrottleDelay(cfg)
	if delay > 0 {
		logger.Log("info", "Applying throttle delay: %v", delay)
		
		if s != nil {
			// Pick a random cool message
			message := throttleMessages[rand.Intn(len(throttleMessages))]
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
		
		// Update last request time after the delay
		throttleMutex.Lock()
		lastRequestTime = time.Now()
		throttleMutex.Unlock()
	}
}