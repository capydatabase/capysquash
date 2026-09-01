package harness

import "testing"

func TestErrFeatureLockedSubscription(t *testing.T) {
	err := NewErrFeatureLockedSubscription("ai_harness", "pro", "https://capysquash.dev/billing")
	if !IsErrFeatureLocked(err) {
		t.Fatal("expected IsErrFeatureLocked to return true")
	}

	featureErr, ok := err.(*ErrFeatureLocked)
	if !ok {
		t.Fatal("expected error type *ErrFeatureLocked")
	}

	if featureErr.Code != FeatureLockSubscriptionRequired {
		t.Fatalf("unexpected code: %s", featureErr.Code)
	}
	if featureErr.Reason != ReasonSubscriptionRequired {
		t.Fatalf("unexpected reason: %s", featureErr.Reason)
	}
	if featureErr.RequiredPlan != "pro" {
		t.Fatalf("unexpected required plan: %s", featureErr.RequiredPlan)
	}
}

func TestErrFeatureLockedLogin(t *testing.T) {
	err := NewErrFeatureLockedLogin("ai_harness", "https://capysquash.dev/billing")
	if !IsErrFeatureLocked(err) {
		t.Fatal("expected IsErrFeatureLocked to return true")
	}

	featureErr, ok := err.(*ErrFeatureLocked)
	if !ok {
		t.Fatal("expected error type *ErrFeatureLocked")
	}

	if featureErr.Code != FeatureLockLoginRequired {
		t.Fatalf("unexpected code: %s", featureErr.Code)
	}
	if featureErr.Reason != ReasonLoginRequired {
		t.Fatalf("unexpected reason: %s", featureErr.Reason)
	}
}
