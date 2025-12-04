package lib

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
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

// Spinner is a minimal terminal spinner with color and suffix support.
type Spinner struct {
	Frames   []string
	Interval time.Duration
	Suffix   string
	Writer   io.Writer
	colorize func(string) string
	stopCh   chan struct{}
	doneCh   chan struct{}
	mutex    sync.Mutex
	running  bool
	frameIdx int
}

// NewSpinner creates a new Spinner instance.
func NewSpinner(charset []string, interval time.Duration) *Spinner {
	if len(charset) == 0 {
		charset = []string{"-", "\\", "|", "/"}
	}
	s := &Spinner{
		Frames:   charset,
		Interval: interval,
		Writer:   os.Stderr,
		colorize: func(s string) string { return s },
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	return s
}

// Color sets simple color attributes by name using fatih/color. Unknown values are ignored.
func (s *Spinner) Color(attrs ...string) {
	var cs []color.Attribute
	for _, a := range attrs {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "bold":
			cs = append(cs, color.Bold)
		case "faint", "dim":
			cs = append(cs, color.Faint)
		case "cyan":
			cs = append(cs, color.FgCyan)
		case "green":
			cs = append(cs, color.FgGreen)
		case "blue":
			cs = append(cs, color.FgBlue)
		case "yellow":
			cs = append(cs, color.FgYellow)
		case "magenta":
			cs = append(cs, color.FgMagenta)
		case "red":
			cs = append(cs, color.FgRed)
		default:
			// ignore unknown
		}
	}
	c := color.New(cs...)
	s.mutex.Lock()
	s.colorize = func(str string) string { return c.Sprint(str) }
	s.mutex.Unlock()
}

// Start begins rendering the spinner until Stop is called.
func (s *Spinner) Start() {
	s.mutex.Lock()
	if s.running {
		s.mutex.Unlock()
		return
	}
	// Reinitialize channels to allow restart after Stop
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.running = true
	// Hide cursor while spinner is active
	_, _ = fmt.Fprint(s.Writer, "\x1b[?25l")
	s.mutex.Unlock()

	go func() {
		t := time.NewTicker(s.Interval)
		defer t.Stop()
		defer close(s.doneCh)
		for {
			select {
			case <-s.stopCh:
				return
			case <-t.C:
				s.mutex.Lock()
				frame := s.Frames[s.frameIdx%len(s.Frames)]
				s.frameIdx++
				colored := s.colorize(frame)
				out := "\r" + colored
				if s.Suffix != "" {
					out += " " + s.Suffix
				}
				_, _ = fmt.Fprint(s.Writer, out)
				s.mutex.Unlock()
			}
		}
	}()
}

// Stop stops the spinner. Caller may clear the line if needed.
func (s *Spinner) Stop() {
	s.mutex.Lock()
	if !s.running {
		s.mutex.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mutex.Unlock()
	<-s.doneCh
	// Show cursor again after spinner stops
	_, _ = fmt.Fprint(s.Writer, "\x1b[?25h")
}

// SpinnerManager provides thread-safe, coordinated spinner management
type SpinnerManager struct {
	mutex         sync.RWMutex
	activeSpinner *Spinner
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

	sm.activeSpinner = NewSpinner(charset, spinnerSpeed)
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

	// Spotlight animation for "waiting for" messages
	if sm.cfg != nil && !sm.cfg.DisableAnimation {
		lower := strings.ToLower(message)
		if strings.Contains(lower, "waiting for") {
			ctx := sm.context.ctx
			base := message
			go func() {
				spot := 0
				width := 6
				ticker := time.NewTicker(200 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						formatted, next := spotlightFormatWithStop(base, spot, width, "...")
						spot = next
						sm.mutex.Lock()
						if !sm.isActive || sm.activeSpinner == nil {
							sm.mutex.Unlock()
							return
						}
						sm.activeSpinner.Suffix = " " + formatted
						sm.mutex.Unlock()
					}
				}
			}()
		}
	}

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

// stripAnsiColors removes ANSI color codes from a string
func stripAnsiColors(text string) string {
	// Regular expression to match ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiRegex.ReplaceAllString(text, "")
}

// spotlightFormat applies a moving highlight window across the message.
// Highlighted runes use Info color, the rest are dimmed. Supports wrap-around.
// Colors are reset before applying spotlight animation to avoid conflicts.
func spotlightFormat(message string, position int, width int) (string, int) {
	if message == "" {
		return message, 0
	}

	// Strip existing ANSI color codes to prevent conflicts with spotlight animation
	cleanMessage := stripAnsiColors(message)
	return spotlightFormatClean(cleanMessage, position, width)
}

// spotlightFormatClean applies spotlight formatting to a message that has already been cleaned of ANSI colors
func spotlightFormatClean(cleanMessage string, position int, width int) (string, int) {
	if cleanMessage == "" {
		return cleanMessage, 0
	}

	runes := []rune(cleanMessage)
	n := len(runes)
	if n == 0 {
		return cleanMessage, 0
	}
	if width <= 0 {
		width = 1
	}
	if width > n {
		width = n
	}

	pos := position % n
	end := pos + width

	var b strings.Builder
	b.Grow(len(cleanMessage) + 16)

	inWindow := func(i int) bool {
		if end <= n {
			return i >= pos && i < end
		}
		return i >= pos || i < (end%n)
	}

	for i := 0; i < n; i++ {
		ch := string(runes[i])
		if inWindow(i) {
			b.WriteString(config.Colors.Info.Sprint(ch))
		} else {
			b.WriteString(config.Colors.Primary.Sprint(ch))
		}
	}

	next := (pos + 1) % n
	return b.String(), next
}

// spotlightFormatWithStop applies spotlight like spotlightFormat but stops coloring at the first occurrence
// of stopSeq. Everything from stopSeq onward remains unmodified to avoid broken ANSI coloring.
// Colors are reset only for the animated part, preserving colors in the tail.
func spotlightFormatWithStop(message string, position int, width int, stopSeq string) (string, int) {
	if stopSeq == "" {
		return spotlightFormat(message, position, width)
	}

	// Find stop boundary in the original message to preserve tail colors
	stopIdxByte := strings.Index(message, stopSeq)
	if stopIdxByte < 0 {
		return spotlightFormat(message, position, width)
	}

	stopIdxByte += len(stopSeq)

	// Split into animated and tail parts
	head := message[:stopIdxByte]
	tail := message[stopIdxByte:]

	// Strip colors only from the head part for spotlight animation
	cleanHead := stripAnsiColors(head)

	// Apply spotlight only to the clean head part
	headFormatted, next := spotlightFormatClean(cleanHead, position, width)

	// Preserve original colors in the tail
	return headFormatted + tail, next
}
