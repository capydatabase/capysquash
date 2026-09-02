// Package utils provides public utility functions for pgsquash.
//
// This package exports commonly needed utility functionality for external tools
// using pgsquash as a library.
package utils

import (
	"io"

	internal_utils "github.com/capy-base/pgsquash-engine/internal/utils"
)

// Logger provides structured logging functionality.
// It wraps the internal logger implementation for external use.
type Logger = internal_utils.Logger

// LogLevel represents logging verbosity levels.
type LogLevel = internal_utils.LogLevel

// Log levels available for configuration
const (
	LogLevelDebug = internal_utils.LogLevelDebug
	LogLevelInfo  = internal_utils.LogLevelInfo
	LogLevelWarn  = internal_utils.LogLevelWarn
	LogLevelError = internal_utils.LogLevelError
	LogLevelFatal = internal_utils.LogLevelFatal
)

// NewLogger creates a new logger instance with the specified level and output.
//
// Parameters:
//   - level: The minimum log level to output (Debug, Info, Warning, Error)
//   - output: Where to write log messages (typically os.Stdout or os.Stderr)
//
// Example:
//
//	logger := utils.NewLogger(utils.LogLevelInfo, os.Stdout)
//	logger.Info("Application started")
func NewLogger(level LogLevel, output io.Writer) *Logger {
	return internal_utils.NewLogger(level, output)
}

// SetDefaultLogger sets the global default logger instance.
// This logger will be used throughout the pgsquash-engine.
//
// Should be called during application initialization before any other operations.
//
// Example:
//
//	logger := utils.NewLogger(utils.LogLevelInfo, os.Stdout)
//	utils.SetDefaultLogger(logger)
func SetDefaultLogger(logger *Logger) {
	internal_utils.SetDefaultLogger(logger)
}

// GetDefaultLogger returns the global default logger instance.
// Returns nil if SetDefaultLogger has not been called.
//
// Example:
//
//	logger := utils.GetDefaultLogger()
//	if logger != nil {
//	    logger.Info("Using default logger")
//	}
func GetDefaultLogger() *Logger {
	return internal_utils.GetDefaultLogger()
}
