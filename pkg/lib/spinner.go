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
	SpinnerDiagnostic: {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}, // Modern dots
	SpinnerLLM:        {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}, // Modern dots
	SpinnerGeneration: {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}, // Modern dots
	SpinnerRAG:        {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}, // Modern dots
	SpinnerThrottle:   {"⏳", "⌛"},                                         // Keep hourglass for throttling
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
	Frames         []string
	Interval       time.Duration
	Suffix         string
	Writer         io.Writer
	GradientColors []*config.ANSIColor
	colorize       func(string) string
	stopCh         chan struct{}
	doneCh         chan struct{}
	mutex          sync.Mutex
	running        bool
	frameIdx       int
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
	var cs []config.ColorAttribute
	for _, a := range attrs {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "bold":
			cs = append(cs, config.ColorBold)
		case "faint", "dim":
			cs = append(cs, config.ColorFaint)
		case "cyan":
			cs = append(cs, config.ColorFgCyan)
		case "green":
			cs = append(cs, config.ColorFgGreen)
		case "blue":
			cs = append(cs, config.ColorFgBlue)
		case "yellow":
			cs = append(cs, config.ColorFgYellow)
		case "magenta":
			cs = append(cs, config.ColorFgMagenta)
		case "red":
			cs = append(cs, config.ColorFgRed)
		default:
			// ignore unknown
		}
	}
	c := config.NewColor(cs...)
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

				var colored string
				if len(s.GradientColors) > 0 {
					// Cycle through gradient colors
					c := s.GradientColors[s.frameIdx%len(s.GradientColors)]
					colored = c.Sprint(frame)
				} else {
					colored = s.colorize(frame)
				}

				s.frameIdx++
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
		if sm.context != nil && sm.context.cancel != nil {
			// Cancel the old animation context so background goroutines exit
			sm.context.cancel()
		}
		sm.activeSpinner.Stop()
		// Clear the line for clean transition
		fmt.Fprint(os.Stderr, "\r\033[2K")
		sm.isActive = false
		sm.activeSpinner = nil
		sm.context = nil
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
	// For modern spinners with spotlight animation, use fast refresh (80ms)
	// to ensure smooth text animation without skipped frames.
	switch spinnerType {
	case SpinnerLLM, SpinnerDiagnostic, SpinnerGeneration, SpinnerRAG:
		spinnerSpeed = 80 * time.Millisecond // Fast refresh for smooth animation
	case SpinnerThrottle:
		spinnerSpeed = time.Duration(float64(spinnerSpeed) * 2.0) // Much slower for throttling
	}

	sm.activeSpinner = NewSpinner(charset, spinnerSpeed)
	sm.activeSpinner.Suffix = " " + message
	sm.activeSpinner.Writer = os.Stderr

	// Apply colors based on spinner type
	// Apply colors based on spinner type
	switch spinnerType {
	case SpinnerDiagnostic:
		sm.activeSpinner.GradientColors = config.Colors.SpinnerDiag
	case SpinnerLLM:
		sm.activeSpinner.GradientColors = config.Colors.SpinnerLLM
	case SpinnerGeneration:
		sm.activeSpinner.GradientColors = config.Colors.SpinnerDiag
	case SpinnerRAG:
		sm.activeSpinner.GradientColors = config.Colors.SpinnerDiag
	case SpinnerThrottle:
		sm.activeSpinner.Color("yellow", "bold")
	}

	sm.activeSpinner.Start()
	sm.isActive = true

	// Spotlight animation for text
	if sm.cfg != nil && !sm.cfg.DisableAnimation {
		// Apply to all LLM and Diagnostic spinners for a consistent modern feel
		if spinnerType == SpinnerLLM || spinnerType == SpinnerDiagnostic || spinnerType == SpinnerGeneration || spinnerType == SpinnerRAG {
			ctx := sm.context.ctx
			// Use a pointer to the message so we can pick up updates
			// Note: In this simple implementation, we'll just read the current message from context if available,
			// but since `base` is captured by value, we need a way to see updates.
			// The `Update` method changes `sm.context.Message`.

			go func() {
				spotLR := 0 // Left-to-right wave position
				spotRL := 0 // Right-to-left wave position (starts at 0, moves backward)
				// Comet width: head(2) + lead(1) + trail(~7) = 10
				width := 10
				// Fast ticker for smooth neon comet sweep
				ticker := time.NewTicker(70 * time.Millisecond)
				defer ticker.Stop()

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						sm.mutex.Lock()
						if !sm.isActive || sm.activeSpinner == nil || sm.context == nil {
							sm.mutex.Unlock()
							return
						}
						// Always use the latest message from context
						currentMsg := sm.context.Message
						sm.mutex.Unlock()

						// Calculate next frame with dual waves
						formatted, nextLR, nextRL := dualWaveFormat(currentMsg, spotLR, spotRL, width)
						spotLR = nextLR
						spotRL = nextRL

						sm.mutex.Lock()
						if sm.isActive && sm.activeSpinner != nil {
							sm.activeSpinner.Suffix = " " + formatted
						}
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
			// Cancel the context to stop the animation goroutine
			if sm.context.cancel != nil {
				sm.context.cancel()
			}
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

	// Gradient spotlight logic:
	// Background: Dim (Dark Gray)
	// Edges of window: Primary (White)
	// Center of window: Info (Bold White)

	// Define window structure: [ ... dim ... | edge | center | edge | ... dim ... ]
	// Window width should be at least 3 for this effect to work well.

	inWindow := func(i int) bool {
		if end <= n {
			return i >= pos && i < end
		}
		return i >= pos || i < (end%n)
	}

	// Helper to check if index is in the "center" of the window (inner 50%)
	inCenter := func(i int) bool {
		// Calculate relative position within the window
		rel := i - pos
		if rel < 0 {
			rel += n
		}

		// Center is the middle 30% of the window (narrower center for more fade)
		margin := width * 35 / 100
		return rel >= margin && rel < (width-margin)
	}

	for i := 0; i < n; i++ {
		ch := string(runes[i])
		if inWindow(i) {
			if inCenter(i) {
				// Center: Bright/Bold
				b.WriteString(config.Colors.Info.Sprint(ch))
			} else {
				// Edges: Normal White
				b.WriteString(config.Colors.Primary.Sprint(ch))
			}
		} else {
			// Background: Dimmed
			b.WriteString(config.Colors.Dim.Sprint(ch))
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

// dualWaveFormat renders a spring-like neon comet with eased motion.
// Slow at edges, fast and wide in the middle, bounces smoothly.
// posLR = animation step (0 to totalSteps), posRL = direction (1 or -1)
func dualWaveFormat(message string, posLR int, posRL int, _ int) (string, int, int) {
	if message == "" {
		return message, 0, 0
	}

	const stopSeq = "(ESC to cancel)"

	// Stop animation before token counts or explicit stop sequence
	stopIdx := strings.Index(message, stopSeq)

	// Detect token count bracket e.g., " [↑5.2k tokens"
	tokenIdx := -1
	if tokensWord := strings.Index(message, " tokens"); tokensWord >= 0 {
		if open := strings.LastIndex(message[:tokensWord], "["); open >= 0 {
			tokenIdx = open
		}
	}
	if tokenIdx >= 0 && (stopIdx < 0 || tokenIdx < stopIdx) {
		stopIdx = tokenIdx
	}

	var (
		cleanMessage string
		tailDimmed   string
	)

	if stopIdx >= 0 {
		head := message[:stopIdx]
		tail := message[stopIdx:]
		cleanMessage = stripAnsiColors(head)
		tailDimmed = config.Colors.Info.Sprint(stripAnsiColors(tail))
	} else {
		cleanMessage = stripAnsiColors(message)
	}

	runes := []rune(cleanMessage)
	n := len(runes)
	if n == 0 {
		return tailDimmed, 0, 0
	}

	// Spring animation parameters
	const totalSteps = 60 // steps per half-cycle (left-to-right or right-to-left)
	minWidth := 3         // compressed at edges
	maxWidth := 14        // stretched in the middle
	if maxWidth > n {
		maxWidth = n
	}
	if minWidth > maxWidth {
		minWidth = maxWidth
	}

	// Direction: 1 = moving right, -1 = moving left
	dir := 1
	if posRL < 0 {
		dir = -1
	}

	// Current step in animation
	step := posLR
	if step < 0 {
		step = 0
	}
	if step > totalSteps {
		step = totalSteps
	}

	// Progress 0.0 to 1.0 through current half-cycle
	progress := float64(step) / float64(totalSteps)

	// Ease-in-out using sine curve: slow-fast-slow
	// Maps linear progress to eased position
	eased := (1.0 - cosine(progress*3.14159265)) / 2.0

	// Calculate position: 0 at left edge, n-1 at right edge
	// For right movement: eased goes 0→1, position goes left→right
	// For left movement: eased goes 0→1, position goes right→left
	var leadPos int
	if dir > 0 {
		leadPos = int(eased * float64(n-1))
	} else {
		leadPos = int((1.0 - eased) * float64(n-1))
	}

	// Dynamic width: widest in middle (high velocity), narrow at edges
	// Velocity is derivative of eased position, highest at progress=0.5
	velocity := sine(progress * 3.14159265) // 0 at edges, 1 at middle
	width := minWidth + int(velocity*float64(maxWidth-minWidth))
	if width < minWidth {
		width = minWidth
	}
	if width > n {
		width = n
	}

	// Comet structure
	headLen := 2
	leadLen := 1
	trailLen := width - headLen - leadLen
	if trailLen < 1 {
		trailLen = 1
	}

	// Neon palette
	cometHead := config.Colors.SpinnerHead
	cometLead := config.Colors.SpinnerLead
	cometTrail := config.Colors.SpinnerTrail
	bgColor := config.Colors.SpinnerBg

	// Calculate tail position from lead position
	var tailPos int
	if dir > 0 {
		tailPos = leadPos - width + 1
	} else {
		tailPos = leadPos + width - 1
	}

	var b strings.Builder
	b.Grow(len(cleanMessage) * 4)

	for i := 0; i < n; i++ {
		ch := string(runes[i])

		// Determine segment for this character
		var seg int // -1=bg, 0=trail, 1=head, 2=lead
		var relPos int

		if dir > 0 {
			// Moving right: tail at low idx, lead at high idx
			if i < tailPos || i > leadPos {
				seg = -1
			} else {
				relPos = i - tailPos
				if relPos < trailLen {
					seg = 0
				} else if relPos < trailLen+headLen {
					seg = 1
				} else {
					seg = 2
				}
			}
		} else {
			// Moving left: lead at low idx, tail at high idx
			if i < leadPos || i > tailPos {
				seg = -1
			} else {
				relPos = tailPos - i
				if relPos < trailLen {
					seg = 0
				} else if relPos < trailLen+headLen {
					seg = 1
				} else {
					seg = 2
				}
			}
		}

		switch seg {
		case 2:
			b.WriteString(cometLead.Sprint(ch))
		case 1:
			headIdx := (step + i) % len(cometHead)
			b.WriteString(cometHead[headIdx].Sprint(ch))
		case 0:
			depth := trailLen - 1 - relPos
			if depth < 0 {
				depth = 0
			}
			trailIdx := (depth * len(cometTrail)) / trailLen
			if trailIdx >= len(cometTrail) {
				trailIdx = len(cometTrail) - 1
			}
			b.WriteString(cometTrail[trailIdx].Sprint(ch))
		default:
			b.WriteString(bgColor.Sprint(ch))
		}
	}

	// Advance animation
	nextStep := step + 1
	nextDir := dir
	if nextStep > totalSteps {
		// Bounce: reverse direction, reset step
		nextStep = 0
		nextDir = -dir
	}

	if stopIdx >= 0 {
		return b.String() + tailDimmed, nextStep, nextDir
	}

	return b.String(), nextStep, nextDir
}

// sine returns sin(x) using Taylor series approximation
func sine(x float64) float64 {
	// Normalize to [-π, π]
	for x > 3.14159265 {
		x -= 2 * 3.14159265
	}
	for x < -3.14159265 {
		x += 2 * 3.14159265
	}
	x3 := x * x * x
	x5 := x3 * x * x
	x7 := x5 * x * x
	return x - x3/6 + x5/120 - x7/5040
}

// cosine returns cos(x) using Taylor series approximation
func cosine(x float64) float64 {
	// Normalize to [-π, π]
	for x > 3.14159265 {
		x -= 2 * 3.14159265
	}
	for x < -3.14159265 {
		x += 2 * 3.14159265
	}
	x2 := x * x
	x4 := x2 * x2
	x6 := x4 * x2
	return 1 - x2/2 + x4/24 - x6/720
}
