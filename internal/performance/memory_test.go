package performance

import "testing"

// The peak survives releases: it is the high-water mark, not current usage.
func TestMemoryManagerTracksPeakAcrossReleases(t *testing.T) {
	mm := NewMemoryManager(64)

	if !mm.TrackMemoryUsage(3 << 20) {
		t.Fatal("tracking 3MB under a 64MB limit failed")
	}
	if !mm.TrackMemoryUsage(2 << 20) {
		t.Fatal("tracking 2MB more under a 64MB limit failed")
	}
	mm.ReleaseMemory(4 << 20)
	if !mm.TrackMemoryUsage(1 << 20) {
		t.Fatal("tracking 1MB after release failed")
	}

	stats := mm.GetMemoryStats()
	if stats.CurrentMemoryBytes != 2<<20 {
		t.Fatalf("current = %d, want %d", stats.CurrentMemoryBytes, 2<<20)
	}
	if stats.PeakMemoryBytes != 5<<20 {
		t.Fatalf("peak = %d, want %d", stats.PeakMemoryBytes, 5<<20)
	}
}

// A rejected allocation (over the limit) does not move the peak.
func TestMemoryManagerPeakIgnoresRejectedAllocations(t *testing.T) {
	mm := NewMemoryManager(1)

	if mm.TrackMemoryUsage(2 << 20) {
		t.Fatal("tracking 2MB under a 1MB limit succeeded")
	}
	if got := mm.GetMemoryStats().PeakMemoryBytes; got != 0 {
		t.Fatalf("peak = %d after a rejected allocation, want 0", got)
	}
}
