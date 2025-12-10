package themes

import (
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// Colors exposes the currently active UI palette.
var Colors = newDraculaTheme()

// DefaultName is the fallback theme.
const DefaultName = "dracula"

var activeName = DefaultName

var registry = map[string]*config.UIColorRoles{
	"dracula": newDraculaTheme(),
	"cyanide": newCyanideTheme(),
}

// Apply switches the active theme and returns the applied name (with fallback).
func Apply(name string) string {
	normalized := normalize(name)
	palette, ok := registry[normalized]
	if !ok {
		normalized = DefaultName
		palette = registry[normalized]
	}

	Colors = clonePalette(palette)
	config.Colors = Colors
	activeName = normalized
	return normalized
}

// Names returns available theme names sorted alphabetically.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get returns a palette by name if it exists.
func Get(name string) (*config.UIColorRoles, bool) {
	p, ok := registry[normalize(name)]
	if !ok {
		return nil, false
	}
	return clonePalette(p), true
}

// Active returns the current palette name and colors.
func Active() (string, *config.UIColorRoles) {
	return activeName, Colors
}

func normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func clonePalette(src *config.UIColorRoles) *config.UIColorRoles {
	if src == nil {
		return nil
	}
	cloned := *src
	return &cloned
}

// Dracula-inspired palette mapped to 256-color indexes for consistent UI.
const (
	draculaBg         = 236 // #282a36
	draculaSurface    = 237 // #44475a
	draculaText       = 255 // #f8f8f2
	draculaComment    = 61  // #6272a4
	draculaCyan       = 117 // #8be9fd
	draculaGreen      = 84  // #50fa7b
	draculaOrange     = 215 // #ffb86c
	draculaPink       = 205 // #ff79c6
	draculaPurple     = 141 // #bd93f9
	draculaPurpleMid  = 135 // #af87ff
	draculaPurpleDark = 99  // #875fd7
	draculaRed        = 203 // #ff5555
	draculaYellow     = 228 // #f1fa8c
)

func draculaFg(idx int, extra ...color.Attribute) *color.Color {
	attrs := []color.Attribute{color.Attribute(38), color.Attribute(5), color.Attribute(idx)}
	attrs = append(attrs, extra...)
	return color.New(attrs...)
}

func draculaFgBg(fgIdx, bgIdx int, extra ...color.Attribute) *color.Color {
	attrs := []color.Attribute{
		color.Attribute(38), color.Attribute(5), color.Attribute(fgIdx),
		color.Attribute(48), color.Attribute(5), color.Attribute(bgIdx),
	}
	attrs = append(attrs, extra...)
	return color.New(attrs...)
}

func newDraculaTheme() *config.UIColorRoles {
	return &config.UIColorRoles{
		Primary:   draculaFg(draculaText),
		Accent:    draculaFg(draculaPink, color.Bold),
		AccentAlt: draculaFg(draculaCyan, color.Bold),
		Info:      draculaFg(draculaText, color.Bold),
		InfoAlt:   draculaFg(draculaText),
		Dim:       draculaFg(draculaComment),
		DimItalic: draculaFg(draculaComment, color.Italic),
		Shadow:    draculaFg(draculaBg, color.Faint),
		Light:     draculaFg(draculaGreen),
		Faint:     draculaFg(draculaSurface),
		Bold:      draculaFg(draculaText, color.Bold),
		Italic:    draculaFg(draculaText, color.Italic),
		Underline: draculaFg(draculaText, color.Underline),

		Ok:    draculaFg(draculaGreen, color.Bold),
		Warn:  draculaFg(draculaYellow, color.Bold),
		Error: draculaFg(draculaRed, color.Bold),

		Provider: draculaFg(draculaPink),
		Model:    draculaFg(draculaCyan),
		Command:  draculaFg(draculaYellow),

		Header:    draculaFg(draculaPurple, color.Bold),
		Border:    draculaFg(draculaComment),
		Label:     draculaFg(draculaCyan),
		Output:    draculaFg(draculaText),
		Highlight: draculaFgBg(draculaBg, draculaPink, color.Bold),

		Heading:        draculaFg(draculaPink, color.Bold),
		Blockquote:     draculaFg(draculaCyan),
		ListBullet:     draculaFg(draculaPurple),
		ListNumber:     draculaFg(draculaCyan),
		InlineCode:     draculaFg(draculaOrange),
		InlineCodeBold: draculaFg(draculaOrange, color.Bold),
		Link:           draculaFg(draculaCyan, color.Underline),
		QuoteDouble:    draculaFg(draculaGreen),
		QuoteSingle:    draculaFg(draculaPink),
		ThinkBorder:    draculaFg(draculaPurple),
		ThinkHeader:    draculaFg(draculaText, color.Bold),
		ThinkDim:       draculaFg(draculaComment, color.Faint),
		TruncateLine:   draculaFg(draculaComment),
		TruncateItalic: draculaFg(draculaComment, color.Italic),
		Ellipsis:       draculaFg(draculaSurface),

		PromptCommand: draculaFg(draculaPink, color.Bold),
		PromptDefault: draculaFg(draculaPink, color.Bold),
		ContextLow:    draculaFg(draculaGreen),
		ContextMid:    draculaFg(draculaYellow),
		ContextHigh:   draculaFg(draculaRed),
		TokenUp:       draculaFg(draculaPink, color.Bold),
		TokenDown:     draculaFg(draculaGreen, color.Bold),
		TokenSep:      draculaFg(draculaComment),
		TokenBracket:  draculaFg(draculaComment),
		TokenOut:      draculaFg(draculaPink),
		TokenIn:       draculaFg(draculaGreen),

		Gradient: []*color.Color{
			draculaFg(draculaPurple),
			draculaFg(draculaPurpleDark),
		},
		GradientAlt: []*color.Color{
			draculaFg(draculaPurpleMid),
			draculaFg(draculaPurple),
		},
		EffectGlitch: []*color.Color{
			draculaFg(draculaPink),
			draculaFg(draculaCyan),
			draculaFg(draculaYellow),
			draculaFg(draculaText),
		},
		SpinnerDiag: []*color.Color{
			draculaFg(draculaCyan),
			draculaFg(draculaPurple),
			draculaFg(draculaPink),
		},
		SpinnerLLM: []*color.Color{
			draculaFg(draculaPink),
			draculaFg(draculaPurple),
			draculaFg(draculaCyan),
		},
		SpinnerHead: []*color.Color{
			draculaFg(draculaPink, color.Bold),
			draculaFg(draculaCyan, color.Bold),
		},
		SpinnerLead: draculaFg(draculaText, color.Bold),
		SpinnerTrail: []*color.Color{
			draculaFg(draculaPurple),
			draculaFg(draculaPink),
			draculaFg(draculaComment),
		},
		SpinnerBg: draculaFg(draculaSurface),
		Rainbow: []*color.Color{
			draculaFg(draculaPink),
			draculaFg(draculaPurple),
			draculaFg(draculaCyan),
			draculaFg(draculaGreen),
			draculaFg(draculaYellow),
			draculaFg(draculaRed),
		},
	}
}

func newCyanideTheme() *config.UIColorRoles {
	primary := color.New(color.Reset)
	accent := color.New(color.FgHiCyan, color.Bold)
	accentAlt := color.New(color.FgCyan, color.Bold)
	info := color.New(color.FgHiWhite, color.Bold)
	infoAlt := color.New(color.FgHiWhite)
	dim := color.New(color.FgHiBlack)
	dimItalic := color.New(color.FgHiBlack, color.Italic)
	shadow := color.New(color.FgHiBlack, color.Faint)
	light := color.New(color.FgHiGreen)
	faint := color.New(color.FgHiBlack, color.Faint)
	bold := color.New(color.Bold)
	italic := color.New(color.Italic)
	underline := color.New(color.Underline)
	ok := color.New(color.FgHiGreen, color.Bold)
	warn := color.New(color.FgHiYellow, color.Bold)
	errColor := color.New(color.FgHiRed, color.Bold)
	provider := color.New(color.FgHiMagenta)
	model := color.New(color.FgMagenta)
	command := color.New(color.FgHiBlue)
	header := color.New(color.FgHiMagenta, color.Bold)
	border := color.New(color.FgHiBlack)
	label := color.New(color.FgBlue)
	output := color.New(color.FgHiBlue)
	highlight := color.New(color.FgHiCyan, color.Bold)
	inline := color.New(color.FgHiYellow)
	inlineBold := color.New(color.FgHiYellow, color.Bold)
	link := color.New(color.FgHiCyan, color.Underline)
	quoteDouble := color.New(color.FgHiGreen)
	quoteSingle := color.New(color.FgHiMagenta)
	thinkBorder := color.New(color.FgHiBlue)
	thinkHeader := color.New(color.FgHiWhite, color.Bold)
	thinkDim := color.New(color.FgHiBlack, color.Faint)
	truncateLine := dim
	truncateItalic := color.New(color.FgHiBlack, color.Italic)
	ellipsis := color.New(color.FgHiBlack)

	gradient := []*color.Color{
		color.New(color.FgHiCyan),
		color.New(color.FgCyan),
	}

	gradientAlt := []*color.Color{
		color.New(color.FgHiBlue),
		color.New(color.FgHiCyan),
	}

	spinnerPalette := []*color.Color{
		color.New(color.FgHiCyan),
		color.New(color.FgHiBlue),
		color.New(color.FgHiMagenta),
	}

	return &config.UIColorRoles{
		Primary:   primary,
		Accent:    accent,
		AccentAlt: accentAlt,
		Info:      info,
		InfoAlt:   infoAlt,
		Dim:       dim,
		DimItalic: dimItalic,
		Shadow:    shadow,
		Light:     light,
		Faint:     faint,
		Bold:      bold,
		Italic:    italic,
		Underline: underline,

		Ok:    ok,
		Warn:  warn,
		Error: errColor,

		Provider: provider,
		Model:    model,
		Command:  command,

		Header:    header,
		Border:    border,
		Label:     label,
		Output:    output,
		Highlight: highlight,

		Heading:        header,
		Blockquote:     accentAlt,
		ListBullet:     command,
		ListNumber:     label,
		InlineCode:     inline,
		InlineCodeBold: inlineBold,
		Link:           link,
		QuoteDouble:    quoteDouble,
		QuoteSingle:    quoteSingle,
		ThinkBorder:    thinkBorder,
		ThinkHeader:    thinkHeader,
		ThinkDim:       thinkDim,
		TruncateLine:   truncateLine,
		TruncateItalic: truncateItalic,
		Ellipsis:       ellipsis,

		PromptCommand: accent,
		PromptDefault: accent,
		ContextLow:    ok,
		ContextMid:    warn,
		ContextHigh:   errColor,
		TokenUp:       accent,
		TokenDown:     ok,
		TokenSep:      dim,
		TokenBracket:  dim,
		TokenOut:      accent,
		TokenIn:       ok,

		Gradient:    gradient,
		GradientAlt: gradientAlt,
		EffectGlitch: []*color.Color{
			accent,
			provider,
			warn,
			info,
		},
		SpinnerDiag: spinnerPalette,
		SpinnerLLM: []*color.Color{
			accent,
			provider,
			accentAlt,
		},
		SpinnerHead: []*color.Color{
			accent,
			accentAlt,
		},
		SpinnerLead: info,
		SpinnerTrail: []*color.Color{
			accent,
			accentAlt,
			dim,
		},
		SpinnerBg: shadow,
		Rainbow: []*color.Color{
			accent,
			provider,
			accentAlt,
			light,
			warn,
			errColor,
		},
	}
}
