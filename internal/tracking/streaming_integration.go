package tracking

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/performance"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// StreamingTracker integrates UnifiedTracker with performance streaming capabilities
type StreamingTracker struct {
	tracker            *UnifiedTracker
	streamingProcessor *performance.StreamingProcessor
	results            chan *TrackingResult
	errors             chan error
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup

	// Processing statistics
	stats            *StreamingStats
	progressCallback func(processed, total int64, throughput float64)
}

// TrackingResult represents the result of processing a migration through streaming tracking
type TrackingResult struct {
	Migration     *types.Migration
	ObjectsAdded  int
	EventsCreated int
	ProcessTime   time.Duration
	MemoryUsed    int64
}

// StreamingStats tracks streaming processing statistics
type StreamingStats struct {
	MigrationsProcessed        int64   `json:"migrations_processed"`
	ObjectsTracked             int64   `json:"objects_tracked"`
	EventsCreated              int64   `json:"events_created"`
	ProcessingTime             int64   `json:"processing_time_ms"`
	ThroughputMigrationsPerSec float64 `json:"throughput_migrations_per_sec"`
}

// NewStreamingTracker creates a new streaming tracker with performance integration
func NewStreamingTracker(batchSize, workerCount int, memManager *performance.MemoryManager) *StreamingTracker {
	ctx, cancel := context.WithCancel(context.Background())

	tracker := NewTracker()
	tracker.EnableStreamingMode()

	streamingProcessor := performance.NewStreamingProcessor(batchSize, workerCount, memManager)

	return &StreamingTracker{
		tracker:            tracker,
		streamingProcessor: streamingProcessor,
		results:            make(chan *TrackingResult, batchSize),
		errors:             make(chan error, workerCount),
		ctx:                ctx,
		cancel:             cancel,
		stats:              &StreamingStats{},
	}
}

// SetProgressCallback sets a callback function for progress updates
func (st *StreamingTracker) SetProgressCallback(callback func(processed, total int64, throughput float64)) {
	st.progressCallback = callback
}

// ProcessDirectory processes all migrations in a directory using streaming
func (st *StreamingTracker) ProcessDirectory(dir string) error {
	startTime := time.Now()

	// Start the streaming processor
	st.streamingProcessor.Start()

	// Start our tracking workers
	for i := 0; i < 2; i++ { // Use 2 workers for tracking
		st.wg.Add(1)
		go st.trackingWorker(i)
	}

	// Start progress monitoring
	go st.progressMonitor()

	// Process directory with streaming processor
	err := st.streamingProcessor.ProcessDirectory(dir)
	if err != nil {
		st.cancel()
		st.wg.Wait()
		return errors.Wrap(err, errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"streaming processor failed", map[string]interface{}{"directory": dir})
	}

	// Process all migration results
	go func() {
		defer close(st.results)
		for processedFile := range st.streamingProcessor.GetResults() {
			if processedFile.Migration != nil {
				result := st.processTracking(processedFile)
				select {
				case st.results <- result:
				case <-st.ctx.Done():
					return
				}
			}
		}
	}()

	// Wait for all tracking to complete
	st.wg.Wait()

	// Update final statistics
	processingTime := time.Since(startTime)
	atomic.StoreInt64(&st.stats.ProcessingTime, processingTime.Milliseconds())

	return nil
}

// trackingWorker processes tracking results
func (st *StreamingTracker) trackingWorker(id int) {
	defer st.wg.Done()

	for {
		select {
		case result, ok := <-st.results:
			if !ok {
				return // Channel closed
			}

			// Update statistics
			atomic.AddInt64(&st.stats.MigrationsProcessed, 1)
			atomic.AddInt64(&st.stats.ObjectsTracked, int64(result.ObjectsAdded))
			atomic.AddInt64(&st.stats.EventsCreated, int64(result.EventsCreated))

		case <-st.ctx.Done():
			return
		}
	}
}

// processTracking processes a migration file through the tracker
func (st *StreamingTracker) processTracking(processedFile *performance.ProcessedFile) *TrackingResult {
	startTime := time.Now()

	// Count objects before processing
	objectsBefore := len(st.tracker.GetObjects())

	// Process through tracker (using sequential counter for simplicity)
	sequence := int(atomic.LoadInt64(&st.stats.MigrationsProcessed))
	st.tracker.ProcessMigration(processedFile.Migration, sequence)

	// Count objects after processing
	objectsAfter := len(st.tracker.GetObjects())

	return &TrackingResult{
		Migration:     processedFile.Migration,
		ObjectsAdded:  objectsAfter - objectsBefore,
		EventsCreated: len(processedFile.Migration.Statements), // Approximation
		ProcessTime:   time.Since(startTime),
		MemoryUsed:    processedFile.MemoryUsed,
	}
}

// progressMonitor monitors progress and calls the callback
func (st *StreamingTracker) progressMonitor() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if st.progressCallback != nil {
				processed, _ := st.tracker.GetProcessingStats()
				streamStats := st.streamingProcessor.GetStats()

				// Calculate throughput
				throughput := 0.0
				if streamStats.ProcessingTime > 0 {
					throughput = float64(processed) / (float64(streamStats.ProcessingTime) / 1000.0)
				}

				st.progressCallback(processed, streamStats.FilesProcessed, throughput)
			}

		case <-st.ctx.Done():
			return
		}
	}
}

// GetTracker returns the underlying unified tracker
func (st *StreamingTracker) GetTracker() *UnifiedTracker {
	return st.tracker
}

// GetStreamingStats returns current streaming statistics
func (st *StreamingTracker) GetStreamingStats() StreamingStats {
	stats := *st.stats
	stats.MigrationsProcessed = atomic.LoadInt64(&st.stats.MigrationsProcessed)
	stats.ObjectsTracked = atomic.LoadInt64(&st.stats.ObjectsTracked)
	stats.EventsCreated = atomic.LoadInt64(&st.stats.EventsCreated)
	stats.ProcessingTime = atomic.LoadInt64(&st.stats.ProcessingTime)

	// Calculate throughput
	if stats.ProcessingTime > 0 {
		timeSeconds := float64(stats.ProcessingTime) / 1000.0
		stats.ThroughputMigrationsPerSec = float64(stats.MigrationsProcessed) / timeSeconds
	}

	return stats
}

// GetCombinedStats returns both streaming and processing statistics
func (st *StreamingTracker) GetCombinedStats() (StreamingStats, performance.ProcessingStats) {
	return st.GetStreamingStats(), st.streamingProcessor.GetStats()
}

// Stop gracefully stops the streaming tracker
func (st *StreamingTracker) Stop() error {
	st.cancel()

	// Stop the streaming processor
	if err := st.streamingProcessor.Stop(); err != nil {
		return errors.Wrap(err, errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"failed to stop streaming processor", nil)
	}

	// Wait for our workers
	st.wg.Wait()

	// Close channels
	close(st.errors)

	// Clear processed migrations to free memory
	st.tracker.ClearProcessedMigrations()

	return nil
}

// MemoryOptimizedTracker provides a simplified interface for memory-constrained environments
type MemoryOptimizedTracker struct {
	*StreamingTracker
	memoryThreshold int64 // Memory threshold in bytes
	currentMemory   int64 //nolint:unused // Reserved for future memory tracking
}

// NewMemoryOptimizedTracker creates a tracker optimized for low memory usage
func NewMemoryOptimizedTracker(memoryLimitMB int, batchSize int) *MemoryOptimizedTracker {
	memManager := performance.NewMemoryManager(memoryLimitMB)
	streamingTracker := NewStreamingTracker(batchSize, 2, memManager)

	return &MemoryOptimizedTracker{
		StreamingTracker: streamingTracker,
		memoryThreshold:  int64(memoryLimitMB) * 1024 * 1024,
	}
}

// ProcessWithMemoryConstraints processes migrations with strict memory constraints
func (mot *MemoryOptimizedTracker) ProcessWithMemoryConstraints(dir string) error {
	// Set aggressive memory management
	mot.tracker.EnableStreamingMode()

	// Set progress callback to monitor memory
	mot.SetProgressCallback(func(processed, total int64, throughput float64) {
		memStats := mot.streamingProcessor.GetStats()
		if memStats.PeakMemoryUsage > mot.memoryThreshold {
			// Force garbage collection if memory is high
			mot.tracker.ClearProcessedMigrations()
		}
	})

	return mot.ProcessDirectory(dir)
}
