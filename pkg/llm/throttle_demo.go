package llm

import (
	"time"

	"github.com/briandowns/spinner"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// DemoThrottling demonstrates the cool throttling messages in action
// This is for demonstration purposes and not included in tests
func DemoThrottling() {
	cfg := &config.Config{
		ThrottleRequestsPerMinute: 12, // 5-second delays for demo
		SpinnerTimeout:            100,
	}

	// Create a demo spinner
	s := spinner.New(spinner.CharSets[11], 100*time.Millisecond)
	s.Suffix = " Preparing LLM request..."
	s.Color("green", "bold")
	s.Start()

	// Simulate first request (no delay)
	applyThrottleDelayWithSpinner(cfg, s)
	s.Suffix = " First request completed!"
	time.Sleep(500 * time.Millisecond)

	// Simulate second request (should trigger throttling)
	s.Suffix = " Preparing second LLM request..."
	applyThrottleDelayWithSpinner(cfg, s)
	s.Suffix = " Second request completed!"
	time.Sleep(500 * time.Millisecond)

	s.Stop()
}