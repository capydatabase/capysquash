// Package fileutil provides file I/O utilities with consistent error handling and permissions.
package fileutil

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// WriteSQL writes SQL content to a file with standard permissions (0644).
// Creates parent directories if they don't exist.
//
// This is the standard function for writing migration SQL files, squashed output, etc.
//
// Example:
//
//	err := WriteSQL("/path/to/migration.sql", "CREATE TABLE users (...);")
func WriteSQL(path string, content string) error {
	return WriteFile(path, []byte(content), 0644)
}

// WriteConfig writes configuration data to a file with standard permissions (0644).
// Creates parent directories if they don't exist.
//
// This is used for writing pgsquash.config.json and other config files.
//
// Example:
//
//	err := WriteConfig("/path/to/config.json", jsonBytes)
func WriteConfig(path string, data []byte) error {
	return WriteFile(path, data, 0644)
}

// WriteFile writes data to a file with the specified permissions.
// Creates parent directories if they don't exist.
//
// This is the low-level function used by WriteSQL and WriteConfig.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("failed to create directory %s", dir),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	// Write file
	if err := os.WriteFile(path, data, perm); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("failed to write file %s", path),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return nil
}

// WriteWithBackup writes content to a file after creating a backup of the existing file.
// If the file doesn't exist, no backup is created.
// Backup filename format: <original>.backup
//
// Example:
//
//	WriteWithBackup("/path/to/file.sql", []byte("new content"))
//	// Creates: /path/to/file.sql.backup (if file existed)
func WriteWithBackup(path string, content []byte) error {
	// Check if file exists
	if _, err := os.Stat(path); err == nil {
		// File exists, create backup
		backupPath := path + ".backup"

		existingContent, err := os.ReadFile(path)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to read existing file for backup",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}

		if err := os.WriteFile(backupPath, existingContent, 0644); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to create backup",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
	}

	// Write new content
	return WriteFile(path, content, 0644)
}

// AppendToFile appends content to an existing file, or creates it if it doesn't exist.
// Adds a newline before the appended content if the file already has content.
func AppendToFile(path string, content []byte) error {
	// Check if file exists and has content
	needsNewline := false
	if stat, err := os.Stat(path); err == nil && stat.Size() > 0 {
		needsNewline = true
	}

	// Open file for appending (create if doesn't exist)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to open file for appending",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			// Log but don't fail on close error
			fmt.Printf("Warning: failed to close file %s: %v\n", path, err)
		}
	}()

	// Add newline separator if needed
	if needsNewline {
		if _, err := f.Write([]byte("\n")); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to write newline separator",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
	}

	// Write content
	if _, err := f.Write(content); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to append content",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return nil
}

// EnsureDir ensures a directory exists, creating it if necessary.
// Uses 0755 permissions for created directories.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("failed to create directory %s", path),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}
	return nil
}

// FileExists checks if a file exists at the given path.
// Returns true if the file exists and is not a directory.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DirExists checks if a directory exists at the given path.
// Returns true if the directory exists.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
