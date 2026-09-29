package buildinfo

import "testing"

func TestSetStampsVersionAndIgnoresEmptyValues(t *testing.T) {
	prevVersion, prevDate, prevCommit := version, buildDate, gitCommit
	t.Cleanup(func() { version, buildDate, gitCommit = prevVersion, prevDate, prevCommit })

	Set("1.2.3", "2026-09-29", "abc123")
	Set("", "", "")

	if got := Version(); got != "1.2.3" {
		t.Fatalf("Version() = %q, want 1.2.3", got)
	}
	if got := BuildDate(); got != "2026-09-29" {
		t.Fatalf("BuildDate() = %q, want 2026-09-29", got)
	}
	if got := GitCommit(); got != "abc123" {
		t.Fatalf("GitCommit() = %q, want abc123", got)
	}
}

func TestVersionFallsBackWithoutStamp(t *testing.T) {
	prev := version
	t.Cleanup(func() { version = prev })
	version = ""

	// A test binary has no module version, so the fallback is "dev"; a
	// release binary is always stamped. Either way it is never empty.
	if got := Version(); got == "" {
		t.Fatal("Version() is empty without a stamp")
	}
}
