package lib

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/logger"
)

// SpinnerType defines different spinner contexts and their visual styles
type SpinnerType int

const (
	SpinnerDiagnostic SpinnerType = iota // For kubectl diagnostic commands
	SpinnerLLM                           // For LLM API calls
	SpinnerGeneration                    // For command generation
	SpinnerRAG                           // For RAG operations
	SpinnerThrottle                      // For throttling delays
)

// Modern spinner character sets optimized for terminal rendering
var spinnerCharSets = map[SpinnerType][]string{
	SpinnerDiagnostic: {"⢹", "⢺", "⢼", "⣸", "⣇", "⡧", "⡗", "⡏"}, // Braille dots - smooth diagnostic progression
	SpinnerLLM:        {"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}, // Wave animation for LLM
	SpinnerGeneration: {"◐", "◑"},                               // Quarter circle rotation for generation
	SpinnerRAG:        {"◜", "◝", "◞", "◟"},                     // Smooth corners for RAG
	SpinnerThrottle:   {"⏳", "⌛"},                               // Simple hourglass for throttling
}

// SpinnerContext represents an active spinner operation
type SpinnerContext struct {
	Type      SpinnerType
	Message   string
	ctx       context.Context
	cancel    context.CancelFunc
	startTime time.Time
}

// SpinnerManager provides thread-safe, coordinated spinner management
type SpinnerManager struct {
	mutex         sync.RWMutex
	activeSpinner *spinner.Spinner
	context       *SpinnerContext
	isActive      bool
	cfg           *config.Config
}

// Global spinner manager instance
var globalSpinnerManager *SpinnerManager
var spinnerManagerOnce sync.Once

// GetSpinnerManager returns the global spinner manager instance
func GetSpinnerManager(cfg *config.Config) *SpinnerManager {
	spinnerManagerOnce.Do(func() {
		globalSpinnerManager = &SpinnerManager{
			cfg: cfg,
		}
	})
	// Update config reference if provided
	if cfg != nil {
		globalSpinnerManager.mutex.Lock()
		globalSpinnerManager.cfg = cfg
		globalSpinnerManager.mutex.Unlock()
	}
	return globalSpinnerManager
}

// Show starts or updates the spinner with the given type and message
func (sm *SpinnerManager) Show(spinnerType SpinnerType, message string) context.CancelFunc {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	// If there's an active spinner, transition gracefully
	if sm.isActive && sm.activeSpinner != nil {
		sm.activeSpinner.Stop()
		// Clear the line for clean transition
		fmt.Fprint(os.Stderr, "\r\033[2K")
	}

	// Create context for this spinner operation
	ctx, cancel := context.WithCancel(context.Background())
	sm.context = &SpinnerContext{
		Type:      spinnerType,
		Message:   message,
		ctx:       ctx,
		cancel:    cancel,
		startTime: time.Now(),
	}

	// Create new spinner with modern character set
	charset := spinnerCharSets[spinnerType]
	spinnerSpeed := time.Duration(sm.cfg.SpinnerTimeout) * time.Millisecond

	// Adjust speed for different spinner types
	switch spinnerType {
	case SpinnerLLM:
		spinnerSpeed = time.Duration(float64(spinnerSpeed) * 1.2) // Slightly slower for LLM
	case SpinnerThrottle:
		spinnerSpeed = time.Duration(float64(spinnerSpeed) * 2.0) // Much slower for throttling
	}

	sm.activeSpinner = spinner.New(charset, spinnerSpeed)
	sm.activeSpinner.Suffix = " " + message
	sm.activeSpinner.Writer = os.Stderr

	// Apply colors based on spinner type
	switch spinnerType {
	case SpinnerDiagnostic:
		sm.activeSpinner.Color("cyan", "bold")
	case SpinnerLLM:
		sm.activeSpinner.Color("green", "bold")
	case SpinnerGeneration:
		sm.activeSpinner.Color("blue", "bold")
	case SpinnerRAG:
		sm.activeSpinner.Color("cyan", "bold")
	case SpinnerThrottle:
		sm.activeSpinner.Color("yellow", "bold")
	}

	sm.activeSpinner.Start()
	sm.isActive = true

	logger.Log("debug", "Started %v spinner: %s", spinnerType, message)

	// Return a cancel function that properly cleans up
	return func() {
		sm.Hide()
		cancel()
	}
}

// Update changes the message of the currently active spinner
func (sm *SpinnerManager) Update(message string) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()

	if sm.isActive && sm.activeSpinner != nil && sm.context != nil {
		sm.activeSpinner.Suffix = " " + message
		sm.context.Message = message
	}
}

// Hide stops the current spinner and clears the line
func (sm *SpinnerManager) Hide() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	if sm.isActive && sm.activeSpinner != nil {
		sm.activeSpinner.Stop()
		// Clear spinner line for clean output
		fmt.Fprint(os.Stderr, "\r\033[2K")
		sm.isActive = false

		if sm.context != nil {
			duration := time.Since(sm.context.startTime)
			logger.Log("debug", "Stopped %v spinner after %v: %s",
				sm.context.Type, duration, sm.context.Message)
		}

		sm.activeSpinner = nil
		sm.context = nil
	}
}

// IsActive returns whether a spinner is currently active
func (sm *SpinnerManager) IsActive() bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return sm.isActive
}

// GetContext returns the current spinner context if active
func (sm *SpinnerManager) GetContext() *SpinnerContext {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	if sm.context != nil {
		// Return a copy to avoid race conditions
		contextCopy := *sm.context
		return &contextCopy
	}
	return nil
}

// ShowWithCountdown displays a spinner with a countdown timer
func (sm *SpinnerManager) ShowWithCountdown(spinnerType SpinnerType, baseMessage string, duration time.Duration) context.CancelFunc {
	cancel := sm.Show(spinnerType, baseMessage)

	// Start countdown in a separate goroutine
	go func() {
		start := time.Now()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-sm.context.ctx.Done():
				return
			case <-ticker.C:
				elapsed := time.Since(start)
				remaining := duration - elapsed
				if remaining <= 0 {
					return
				}

				countdownMsg := fmt.Sprintf("%s (%.1fs remaining)", baseMessage, remaining.Seconds())
				sm.Update(countdownMsg)
			}
		}
	}()

	return cancel
}

// ShowDiagnostic is a convenience method for diagnostic operations
func (sm *SpinnerManager) ShowDiagnostic(message string) context.CancelFunc {
	return sm.Show(SpinnerDiagnostic, message)
}

// ShowLLM is a convenience method for LLM operations
func (sm *SpinnerManager) ShowLLM(message string) context.CancelFunc {
	return sm.Show(SpinnerLLM, message)
}

// ShowGeneration is a convenience method for generation operations
func (sm *SpinnerManager) ShowGeneration(message string) context.CancelFunc {
	return sm.Show(SpinnerGeneration, message)
}

// ShowRAG is a convenience method for RAG operations
func (sm *SpinnerManager) ShowRAG(message string) context.CancelFunc {
	return sm.Show(SpinnerRAG, message)
}

// ShowThrottle is a convenience method for throttling operations with countdown
func (sm *SpinnerManager) ShowThrottle(message string, duration time.Duration) context.CancelFunc {
	return sm.ShowWithCountdown(SpinnerThrottle, message, duration)
}
