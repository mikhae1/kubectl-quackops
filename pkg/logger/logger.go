package logger

import (
	"io"
	"log"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Debug flag
var DEBUG = os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1"

// logWriter struct to override Write method
type logWriter struct {
	logger *log.Logger
	prefix string
	style  lipgloss.Style
}

func (l *logWriter) Write(p []byte) (int, error) {
	if !DEBUG {
		return len(p), nil
	}

	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue // Skip empty lines
		}
		// Colorize the prefix and the line
		// Use lipgloss to render the prefix + line
		// Wait, the original code colorized (prefix + line).
		msg := l.style.Render(l.prefix + line)
		l.logger.Println(msg)
	}
	return len(p), nil
}

// LoggerMap holds different loggers
var LoggerMap = map[string]*log.Logger{}

// initLogger initializes a new logger with a given prefix and lipgloss style
func initLogger(level string, prefix string, style lipgloss.Style, flags int, output io.Writer) {
	logger := log.New(&logWriter{
		logger: log.New(output, "", flags),
		prefix: prefix,
		style:  style,
	}, "", 0)

	LoggerMap[level] = logger
}

// InitLoggers initializes all loggers with different levels and colors
func InitLoggers(output io.Writer, flags int) {
	// Define styles locally to avoid circular dependency with pkg/style if possible,
	// or use simple colors.
	// Since logger is a low-level package, it should probably avoid depending on `pkg/style` if `pkg/style` depends on logging (it doesn't seems so).
	// But `pkg/style` imports `lipgloss`.
	// Let's use raw lipgloss styles here to be safe and self-contained.

	initLogger("info", "INFO: ", lipgloss.NewStyle().Foreground(lipgloss.Color("86")), flags, output)    // Cyan
	initLogger("debug", "DEBUG: ", lipgloss.NewStyle().Foreground(lipgloss.Color("240")), flags, output) // Dark Gray
	initLogger("warn", "WARN: ", lipgloss.NewStyle().Foreground(lipgloss.Color("220")), flags, output)   // Yellow
	initLogger("err", "ERR: ", lipgloss.NewStyle().Foreground(lipgloss.Color("196")), flags, output)     // Red

	// Custom loggers
	initLogger("llmIn", "[LLM] > ", lipgloss.NewStyle().Foreground(lipgloss.Color("33")), flags, output)        // Blue
	initLogger("llmOut", "[LLM] < ", lipgloss.NewStyle().Foreground(lipgloss.Color("27")), flags, output)       // Blueish
	initLogger("llmSys", "[LLM:SYS] > ", lipgloss.NewStyle().Foreground(lipgloss.Color("201")), flags, output)  // Magenta
	initLogger("llmUser", "[LLM:USER] > ", lipgloss.NewStyle().Foreground(lipgloss.Color("45")), flags, output) // Cyan
	initLogger("in", "> ", lipgloss.NewStyle().Foreground(lipgloss.Color("46")), flags, output)                 // Green
	initLogger("out", "< ", lipgloss.NewStyle().Foreground(lipgloss.Color("34")), flags, output)                // Dark Green
}

// Log function to use defined loggers
func Log(level, format string, a ...interface{}) {
	lgr, ok := LoggerMap[level]
	if !ok || lgr == nil {
		// Lazily initialize loggers to avoid panics in tests or early calls
		if len(LoggerMap) == 0 {
			InitLoggers(os.Stderr, 0)
		}
		lgr = LoggerMap[level]
	}
	if lgr == nil {
		// Fallback to info logger for unknown levels
		lgr = LoggerMap["info"]
	}
	lgr.Printf(format, a...)
}
