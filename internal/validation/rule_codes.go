package validation

// Canonical rule identifiers for static validation.
//
// Format:
//
//	CSQ.<DOMAIN>.<RULE_NAME>
//
// This keeps rule IDs namespaced and stable across CLI/API/IDE surfaces.
const (
	RuleCodeBreakingDropColumn        = "CSQ.BREAKING.DROP_COLUMN"
	RuleCodeBreakingDropTable         = "CSQ.BREAKING.DROP_TABLE"
	RuleCodeBreakingRenameColumn      = "CSQ.BREAKING.RENAME_COLUMN"
	RuleCodeBreakingRenameTable       = "CSQ.BREAKING.RENAME_TABLE"
	RuleCodeBreakingTypeChange        = "CSQ.BREAKING.TYPE_CHANGE"
	RuleCodeSafetyConcurrentIndex     = "CSQ.SAFETY.CONCURRENT_INDEX"
	RuleCodeSafetyMissingWhere        = "CSQ.SAFETY.MISSING_WHERE"
	RuleCodeSafetyConstraintNotValid  = "CSQ.SAFETY.CONSTRAINT_NOT_VALID"
	RuleCodeSafetyConstraintFlow      = "CSQ.SAFETY.CONSTRAINT_VALIDATE_FLOW"
	RuleCodeHygienePreferText         = "CSQ.HYGIENE.PREFER_TEXT"
	RuleCodeHygienePreferBigInt       = "CSQ.HYGIENE.PREFER_BIGINT"
	RuleCodeMetaUnusedIgnoreDirective = "CSQ.META.UNUSED_IGNORE_DIRECTIVE"
)
