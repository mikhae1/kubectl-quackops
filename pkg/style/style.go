package style

import "github.com/charmbracelet/lipgloss"

// Color definitions
var (
	ColorGreen   = lipgloss.Color("#50fa7b")
	ColorYellow  = lipgloss.Color("#f1fa8c")
	ColorRed     = lipgloss.Color("#ff5555")
	ColorMagenta = lipgloss.Color("#ff79c6")
	ColorCyan    = lipgloss.Color("#8be9fd")
	ColorBlue    = lipgloss.Color("#6272a4")
	ColorGray    = lipgloss.Color("#6272a4") // using blueish gray for dim
	ColorWhite   = lipgloss.Color("#f8f8f2")
	ColorBlack   = lipgloss.Color("#282a36")
)

// Semantic Styles
var (
	// Base styles
	Normal = lipgloss.NewStyle().Foreground(ColorWhite)
	Dim    = lipgloss.NewStyle().Foreground(ColorGray)
	Bold   = lipgloss.NewStyle().Bold(true)

	// Status styles
	Success = lipgloss.NewStyle().Foreground(ColorGreen)
	Warning = lipgloss.NewStyle().Foreground(ColorYellow)
	Error   = lipgloss.NewStyle().Foreground(ColorRed)
	Info    = lipgloss.NewStyle().Foreground(ColorCyan)
	Debug   = lipgloss.NewStyle().Foreground(ColorGray).Faint(true)

	// UI Elements
	Title    = lipgloss.NewStyle().Foreground(ColorMagenta).Bold(true)
	SubTitle = lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)
	Header   = lipgloss.NewStyle().Foreground(ColorMagenta).Bold(true).Underline(true)
	Label    = lipgloss.NewStyle().Foreground(ColorBlue)
	Command  = lipgloss.NewStyle().Foreground(ColorYellow)

	// Aliases for compatibility
	Ok   = Success
	Warn = Warning
	Fail = Error
)

// Gradients
var (
	GradientOcean = []lipgloss.Style{
		lipgloss.NewStyle().Foreground(ColorCyan),
		lipgloss.NewStyle().Foreground(ColorBlue),
	}
)

// Palette inspired by charmbracelet/crush and other modern dark themes
var (
	// Base colors
	ColorPurple = lipgloss.Color("#bd93f9") // Dracula Purple-ish / Crush Purple
	ColorPink   = lipgloss.Color("#ff79c6") // Pink
	// ColorCyan    = lipgloss.Color("#8be9fd") // Cyan (already defined above)
	// ColorGreen   = lipgloss.Color("#50fa7b") // Green (already defined above)
	// ColorBlue    = lipgloss.Color("#6272a4") // Blue / Grayish Blue (Dracula Comment) or use a brighter blue (already defined above)
	// ColorRed     = lipgloss.Color("#ff5555") // Red (already defined above)
	ColorOrange = lipgloss.Color("#ffb86c") // Orange
	// ColorYellow  = lipgloss.Color("#f1fa8c") // Yellow (already defined above)
	// ColorGray    = lipgloss.Color("#6272a4") // Gray (already defined above)
	ColorDarkGray = lipgloss.Color("#44475a") // Dark Gray
	// ColorWhite   = lipgloss.Color("#f8f8f2") // White (already defined above)
	// ColorBlack   = lipgloss.Color("#282a36") // Black (already defined above)

	// Semantic Styles (some redefined, some new)
	// Success = lipgloss.NewStyle().Foreground(ColorGreen).Bold(true) // Redefined above
	// Error   = lipgloss.NewStyle().Foreground(ColorRed).Bold(true)   // Redefined above
	// Warning = lipgloss.NewStyle().Foreground(ColorYellow).Bold(true) // Redefined above
	// Info    = lipgloss.NewStyle().Foreground(ColorCyan)             // Redefined above
	// Debug   = lipgloss.NewStyle().Foreground(ColorGray)             // Redefined above

	// UI Elements
	// Title = lipgloss.NewStyle().
	// 	Foreground(ColorPurple).
	// 	Bold(true).
	// 	MarginBottom(1) // Redefined above

	// SubTitle = lipgloss.NewStyle().
	// 		Foreground(ColorPink).
	// 		Bold(true) // Redefined above

	KeyKey = lipgloss.NewStyle().
		Foreground(ColorGray). // Changed from ColorCancel to ColorGray as ColorCancel is defined later
		Bold(true)             // Helper for key bindings keys

	// Command = lipgloss.NewStyle().
	// 	Foreground(ColorOrange).
	// 	Italic(true) // Redefined above

	Code = lipgloss.NewStyle().
		Foreground(ColorCyan).
		Background(ColorDarkGray).
		Padding(0, 1)

	Link = lipgloss.NewStyle().
		Foreground(ColorCyan).
		Underline(true)

		// Spinner
	SpinnerColor = ColorPurple
)

// Helper colors for non-styled string building if needed
const (
	ColorHexPurple = "#bd93f9"
	ColorHexPink   = "#ff79c6"
)

var (
	// Specific application styles
	ContextPrompt = lipgloss.NewStyle().Foreground(ColorGreen)
	ContextItem   = lipgloss.NewStyle().Foreground(ColorCyan)

	// Status indicators
	StatusRunning = lipgloss.NewStyle().Foreground(ColorYellow).SetString("●")
	StatusSuccess = lipgloss.NewStyle().Foreground(ColorGreen).SetString("✔")
	StatusFail    = lipgloss.NewStyle().Foreground(ColorRed).SetString("✖")

	ColorCancel = ColorGray
)
