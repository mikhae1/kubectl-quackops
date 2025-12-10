package lib

import (
	"fmt"
	"os"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
	"github.com/mikhae1/kubectl-quackops/pkg/style"
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

	up := style.Warning.Bold(true).Render("↑")
	down := style.Info.Bold(true).Render("↓")
	sep := style.Debug.Render("|")
	leftBracket := style.Debug.Render("[")
	rightBracket := style.Debug.Render("]")
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
	var pctStyle lipgloss.Style
	switch {
	case pct < 50:
		pctStyle = style.Success
	case pct < 80:
		pctStyle = style.Warning
	default:
		pctStyle = style.Error
	}
	pctStr := pctStyle.Render(fmt.Sprintf("%.0f%%", pct))

	outNum := style.Warning.Render(FormatCompactNumber(tm.outgoing))
	inNum := style.Info.Render(FormatCompactNumber(tm.incoming.Load()))
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
