package lib

import (
	"fmt"
	"os"
	"sync"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// TokenMeter renders a compact real-time token counter to stderr.
// It shows outgoing (prompt/history) and incoming (streamed) token counts using arrows.
type TokenMeter struct {
	cfg          *config.Config
	outgoing     int
	incoming     AtomicInt
	mu           sync.Mutex
	lastRendered string
}

// NewTokenMeter creates a token meter with a known outgoing token count.
func NewTokenMeter(cfg *config.Config, outgoing int) *TokenMeter {
	return &TokenMeter{cfg: cfg, outgoing: outgoing}
}

// AddIncoming adds to the incoming token counter and triggers a re-render.
func (tm *TokenMeter) AddIncoming(delta int) {
	if delta <= 0 {
		return
	}
	tm.incoming.Add(delta)
	tm.Render()
}

// AddIncomingSilent updates incoming tokens without rendering.
func (tm *TokenMeter) AddIncomingSilent(delta int) {
	if delta <= 0 {
		return
	}
	tm.incoming.Add(delta)
}

// Outgoing returns the initial outgoing token count.
func (tm *TokenMeter) Outgoing() int { return tm.outgoing }

// Incoming returns the current accumulated incoming token count.
func (tm *TokenMeter) Incoming() int { return tm.incoming.Load() }

// Render redraws the meter on a single stderr line.
func (tm *TokenMeter) Render() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	up := color.New(color.FgHiYellow, color.Bold).Sprint("↑")
	down := color.New(color.FgHiCyan, color.Bold).Sprint("↓")
	sep := color.New(color.FgHiBlack).Sprint("|")
	leftBracket := color.New(color.FgHiBlack).Sprint("[")
	rightBracket := color.New(color.FgHiBlack).Sprint("]")
	// Context usage percentage based on this request's outgoing tokens (prompt + history)
	pct := 0.0
	if tm.cfg != nil {
		maxWindow := EffectiveMaxTokens(tm.cfg)
		if maxWindow > 0 {
			pct = (float64(tm.outgoing) / float64(maxWindow)) * 100
			if pct > 100 {
				pct = 100
			}
		}
	}
	// Color percent similarly to prompt: green <50, yellow <80, red otherwise
	var pctColor *color.Color
	switch {
	case pct < 50:
		pctColor = color.New(color.FgGreen)
	case pct < 80:
		pctColor = color.New(color.FgYellow)
	default:
		pctColor = color.New(color.FgRed)
	}
	pctStr := pctColor.Sprintf("%.0f%%", pct)

	outNum := color.New(color.FgHiYellow).Sprint(FormatCompactNumber(tm.outgoing))
	inNum := color.New(color.FgHiCyan).Sprint(FormatCompactNumber(tm.incoming.Load()))
	// Compact form: [3%|↑2.9k|↓2.0k]
	line := fmt.Sprintf("%s%s%s%s%s%s%s", leftBracket, pctStr, sep, up+outNum, sep, down+inNum, rightBracket)

	// Erase line and rewrite on stderr only
	fmt.Fprint(os.Stderr, "\r\033[2K")
	fmt.Fprint(os.Stderr, line)
	tm.lastRendered = line
}

// Clear erases the meter line from stderr.
func (tm *TokenMeter) Clear() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.lastRendered != "" {
		fmt.Fprint(os.Stderr, "\r\033[2K")
		tm.lastRendered = ""
	}
}
