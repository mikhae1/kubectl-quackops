package logger

import (
	"io"
	"log"
	"os"
	"strings"

	"github.com/mikhae1/kubectl-quackops/pkg/config"
)

// Debug flag
var DEBUG = os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1"

// logWriter struct to override Write method
type logWriter struct {
	logger *log.Logger
	prefix string
	color  func(a ...interface{}) string
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
		l.logger.Println(l.color(l.prefix + line))
	}
	return len(p), nil
}

// LoggerMap holds different loggers
var LoggerMap = map[string]*log.Logger{}

// initLogger initializes a new logger with a given prefix and color function
func initLogger(level string, prefix string, colorFunc func(a ...interface{}) string, flags int, output io.Writer) {
	logger := log.New(&logWriter{
		logger: log.New(output, "", flags),
		prefix: prefix,
		color:  colorFunc,
	}, "", 0)

	LoggerMap[level] = logger
}

// InitLoggers initializes all loggers with different levels and colors
func InitLoggers(output io.Writer, flags int) {
	initLogger("info", "INFO: ", config.Colors.AccentAlt.SprintFunc(), flags, output)
	initLogger("debug", "DEBUG: ", config.Colors.Dim.SprintFunc(), flags, output)
	initLogger("warn", "WARN: ", config.Colors.Warn.SprintFunc(), flags, output)
	initLogger("err", "ERR: ", config.Colors.Error.SprintFunc(), flags, output)

	// Custom loggers
	initLogger("llmIn", "[LLM] > ", config.Colors.Output.SprintFunc(), flags, output)
	initLogger("llmOut", "[LLM] < ", config.Colors.Label.SprintFunc(), flags, output)
	initLogger("llmSys", "[LLM:SYS] > ", config.Colors.Header.SprintFunc(), flags, output)
	initLogger("llmUser", "[LLM:USER] > ", config.Colors.Accent.SprintFunc(), flags, output)
	initLogger("in", "> ", config.Colors.Light.SprintFunc(), flags, output)
	initLogger("out", "< ", config.Colors.Ok.SprintFunc(), flags, output)
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
