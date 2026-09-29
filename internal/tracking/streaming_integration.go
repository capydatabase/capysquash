package tracking

// StreamingTracker is the tracker of a streaming squash: a UnifiedTracker in
// streaming mode that the caller feeds one migration at a time, in history
// order, and releases the processed migrations of on Stop.
type StreamingTracker struct {
	tracker *UnifiedTracker
}

// NewStreamingTracker creates a tracker in streaming mode.
func NewStreamingTracker() *StreamingTracker {
	tracker := NewTracker()
	tracker.EnableStreamingMode()
	return &StreamingTracker{tracker: tracker}
}

// GetTracker returns the underlying unified tracker.
func (st *StreamingTracker) GetTracker() *UnifiedTracker {
	return st.tracker
}

// Stop releases the processed migrations the tracker still holds.
func (st *StreamingTracker) Stop() error {
	st.tracker.ClearProcessedMigrations()
	return nil
}
