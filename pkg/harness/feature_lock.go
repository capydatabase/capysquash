package harness

import (
	"errors"
	"fmt"
	"strings"
)

type FeatureLockCode string

const (
	FeatureLockSubscriptionRequired FeatureLockCode = "FEATURE_LOCKED_SUBSCRIPTION_REQUIRED"
	FeatureLockLoginRequired        FeatureLockCode = "FEATURE_LOCKED_LOGIN_REQUIRED"
)

type FeatureLockReason string

const (
	ReasonSubscriptionRequired FeatureLockReason = "subscription_required"
	ReasonLoginRequired        FeatureLockReason = "login_required"
)

// FeatureName identifies a managed feature across the CAPYSQUASH surface
// (engine CLI, API, platform). The engine is the single source of truth for
// feature identity and catalog metadata.
type FeatureName string

const (
	// FeatureAIHarness is the managed AI harness orchestration feature.
	FeatureAIHarness FeatureName = "ai_harness"
)

// Availability models and entitlement metadata for the feature catalog.
const (
	// AvailabilityPaidManaged marks features executed only by CAPYSQUASH
	// managed services for paid, authenticated subjects.
	AvailabilityPaidManaged = "paid_managed"

	// PlanPro is the plan required for paid managed features.
	PlanPro = "pro"

	// BillingUpgradeURL is the canonical upgrade/billing URL.
	BillingUpgradeURL = "https://capysquash.dev/billing"
)

// FeatureCatalogEntry describes a managed feature's availability contract.
type FeatureCatalogEntry struct {
	Name              FeatureName `json:"name"`
	AvailabilityModel string      `json:"availability_model"`
	EnabledLocally    bool        `json:"enabled_locally"`
	RequiresAuth      bool        `json:"requires_auth"`
	RequiredPlan      string      `json:"required_plan"`
	UpgradeURL        string      `json:"upgrade_url"`
	Description       string      `json:"description"`
}

// FeatureCatalog returns the canonical catalog of managed features. This is
// the single source for the ai_harness availability tuple (name, plan,
// availability model, billing URL); consumers (CLI `features` command, API
// capability endpoints) must derive their feature listings from it instead of
// keeping local copies.
func FeatureCatalog() []FeatureCatalogEntry {
	return []FeatureCatalogEntry{
		{
			Name:              FeatureAIHarness,
			AvailabilityModel: AvailabilityPaidManaged,
			EnabledLocally:    false,
			RequiresAuth:      true,
			RequiredPlan:      PlanPro,
			UpgradeURL:        BillingUpgradeURL,
			Description:       "Managed AI harness orchestration with policy-gated apply workflow.",
		},
	}
}

// ErrFeatureLocked is the canonical engine-level typed error for paid or auth-gated features.
//
// Callers should use IsErrFeatureLocked to branch behavior.
type ErrFeatureLocked struct {
	Code         FeatureLockCode   `json:"code"`
	Feature      string            `json:"feature"`
	Reason       FeatureLockReason `json:"reason"`
	RequiredPlan string            `json:"required_plan,omitempty"`
	UpgradeURL   string            `json:"upgrade_url,omitempty"`
	Message      string            `json:"message"`
}

func (e *ErrFeatureLocked) Error() string {
	if e == nil {
		return "feature access denied"
	}
	feature := strings.TrimSpace(e.Feature)
	if feature == "" {
		feature = "unknown"
	}
	if strings.TrimSpace(e.RequiredPlan) != "" {
		return fmt.Sprintf("%s: feature '%s' requires plan '%s'", e.Code, feature, e.RequiredPlan)
	}
	return fmt.Sprintf("%s: feature '%s' is locked (%s)", e.Code, feature, e.Reason)
}

func IsErrFeatureLocked(err error) bool {
	if err == nil {
		return false
	}
	var target *ErrFeatureLocked
	return errors.As(err, &target)
}

func NewErrFeatureLockedSubscription(feature, requiredPlan, upgradeURL string) error {
	return &ErrFeatureLocked{
		Code:         FeatureLockSubscriptionRequired,
		Feature:      strings.TrimSpace(feature),
		Reason:       ReasonSubscriptionRequired,
		RequiredPlan: strings.TrimSpace(requiredPlan),
		UpgradeURL:   strings.TrimSpace(upgradeURL),
		Message:      "subscription required",
	}
}

func NewErrFeatureLockedLogin(feature, upgradeURL string) error {
	return &ErrFeatureLocked{
		Code:       FeatureLockLoginRequired,
		Feature:    strings.TrimSpace(feature),
		Reason:     ReasonLoginRequired,
		UpgradeURL: strings.TrimSpace(upgradeURL),
		Message:    "login required",
	}
}
