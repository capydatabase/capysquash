package squasher

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/capydatabase/capysquash/internal/validation"
)

// runPreFlightValidation lints every input migration with the static rules.
// It returns an error listing every finding, including migrations the
// validator could not check; the caller decides whether that aborts the
// squash (static_validation.strict / squash --strict) or becomes a warning.
func (e *Engine) runPreFlightValidation(ctx context.Context, migrations map[int]string) error {
	var validationErrors []string

	// Sort migration IDs for deterministic order
	var ids []int
	for id := range migrations {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	for _, id := range ids {
		sqlContent := migrations[id]

		violations, err := e.preFlightValidator.Check(sqlContent)
		if err != nil {
			// A rule that failed still leaves the other rules' findings below.
			msg := fmt.Sprintf("Migration %d could not be validated: %v", id, err)
			validationErrors = append(validationErrors, msg)
			e.logger.Warn("%s", msg)
		}

		for _, v := range violations {
			// Format: "File 01 [CODE]: Message"
			msg := fmt.Sprintf("Migration %d [%s]: %s", id, v.Code, v.Message)
			validationErrors = append(validationErrors, msg)

			// Also log immediately for visibility
			e.logger.Warn("%s", msg)
		}
	}

	if len(validationErrors) > 0 {
		return fmt.Errorf("found %d issues: %s", len(validationErrors), strings.Join(validationErrors, "; "))
	}
	return nil
}

// runPostFlightValidation executes the post-flight validator on the final SQL
func (e *Engine) runPostFlightValidation(ctx context.Context, sqlContent string) ([]validation.Violation, error) {
	return e.postFlightValidator.Check(sqlContent)
}
