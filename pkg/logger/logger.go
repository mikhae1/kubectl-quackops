package logger

import (
	"io"
	"log"
	"os"
	"strings"

	"github.com/fatih/color"
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
	initLogger("info", "INFO: ", color.New(color.FgCyan).SprintFunc(), flags, output)
	initLogger("warn", "WARN: ", color.New(color.FgYellow).SprintFunc(), flags, output)
	initLogger("err", "ERR: ", color.New(color.FgRed).SprintFunc(), flags, output)

	// Custom loggers
	initLogger("llmIn", "[LLM] > ", color.New(color.FgHiBlue).SprintFunc(), flags, output)
	initLogger("llmOut", "[LLM] < ", color.New(color.FgBlue).SprintFunc(), flags, output)
	initLogger("in", "> ", color.New(color.FgHiGreen).SprintFunc(), flags, output)
	initLogger("out", "< ", color.New(color.FgGreen).SprintFunc(), flags, output)
}

// Log function to use defined loggers
func Log(level, format string, a ...interface{}) {
	logger, ok := LoggerMap[level]
	if !ok {
		logger = LoggerMap["info"]
	}
	logger.Printf(format, a...)
}
