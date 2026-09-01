package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// LogLevel represents the severity of a log message
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelFatal
)

// String returns the string representation of a log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// Logger is a simple structured logger for pgsquash
type Logger struct {
	minLevel LogLevel
	output   io.Writer
	mu       sync.Mutex
	prefix   string
}

// NewLogger creates a new logger with the specified minimum level
func NewLogger(minLevel LogLevel, output io.Writer) *Logger {
	if output == nil {
		output = os.Stdout
	}
	return &Logger{
		minLevel: minLevel,
		output:   output,
	}
}

// WithPrefix creates a new logger with a prefix
func (l *Logger) WithPrefix(prefix string) *Logger {
	return &Logger{
		minLevel: l.minLevel,
		output:   l.output,
		prefix:   prefix,
	}
}

// log formats and writes a log message
func (l *Logger) log(level LogLevel, format string, args ...any) {
	if level < l.minLevel {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)

	var logLine string
	if l.prefix != "" {
		logLine = fmt.Sprintf("[%s] [%s] [%s] %s\n", timestamp, level.String(), l.prefix, message)
	} else {
		logLine = fmt.Sprintf("[%s] [%s] %s\n", timestamp, level.String(), message)
	}

	// Explicitly ignore error as we cannot escalate logging errors further
	_, _ = l.output.Write([]byte(logLine))
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...any) {
	l.log(LogLevelDebug, format, args...)
}

// Info logs an info message
func (l *Logger) Info(format string, args ...any) {
	l.log(LogLevelInfo, format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...any) {
	l.log(LogLevelWarn, format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...any) {
	l.log(LogLevelError, format, args...)
}

// Fatal logs a fatal message
// Deprecated: excessive process termination. Use Error instead and handle returns.
func (l *Logger) Fatal(format string, args ...any) {
	l.log(LogLevelError, format, args...)
}

// StandardLogger returns a stdlib log.Logger that writes to this logger
func (l *Logger) StandardLogger(level LogLevel) *log.Logger {
	return log.New(&logWriter{logger: l, level: level}, "", 0)
}

// logWriter adapts our Logger to io.Writer for stdlib log.Logger compatibility
type logWriter struct {
	logger *Logger
	level  LogLevel
}

func (lw *logWriter) Write(p []byte) (n int, err error) {
	lw.logger.log(lw.level, "%s", string(p))
	return len(p), nil
}

// Global default logger
var defaultLogger = NewLogger(LogLevelInfo, os.Stdout)

// SetDefaultLogger sets the global default logger
func SetDefaultLogger(logger *Logger) {
	defaultLogger = logger
}

// GetDefaultLogger returns the global default logger
func GetDefaultLogger() *Logger {
	return defaultLogger
}
