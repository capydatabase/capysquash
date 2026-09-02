package squasher

import (
	"fmt"

	"github.com/capy-base/pgsquash-engine/internal/errors"
)

// ValidSafetyLevels returns all valid SafetyLevel values
func ValidSafetyLevels() []SafetyLevel {
	return []SafetyLevel{
		Conservative,
		Standard,
		Aggressive,
		Paranoid,
	}
}

// IsValidSafetyLevel checks if a SafetyLevel is valid
func IsValidSafetyLevel(level SafetyLevel) bool {
	switch level {
	case Conservative, Standard, Aggressive, Paranoid:
		return true
	default:
		return false
	}
}

// ValidateSafetyLevel returns an error if the SafetyLevel is invalid
func ValidateSafetyLevel(level SafetyLevel) error {
	if !IsValidSafetyLevel(level) {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("invalid safety level: %s (valid: conservative, standard, aggressive, paranoid)", level),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Use one of: conservative, standard, aggressive, paranoid")
	}
	return nil
}

// ParseSafetyLevel converts a string to SafetyLevel with validation
func ParseSafetyLevel(s string) (SafetyLevel, error) {
	level := SafetyLevel(s)
	if !IsValidSafetyLevel(level) {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("invalid safety level: %s (valid: conservative, standard, aggressive, paranoid)", level),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Use one of: conservative, standard, aggressive, paranoid")
	}
	return level, nil
}
