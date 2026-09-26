package utils

import (
	"io"
	"testing"
)

// The TUI swaps the default logger while engine goroutines may be logging through it; the swap and
// the reads must not race (run under -race).
func TestDefaultLoggerSwapIsRaceFree(t *testing.T) {
	prev := GetDefaultLogger()
	defer SetDefaultLogger(prev)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			_ = GetDefaultLogger()
		}
	}()
	for range 1000 {
		SetDefaultLogger(NewLogger(LogLevelInfo, io.Discard))
	}
	<-done
}
