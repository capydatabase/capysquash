package performance

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// StreamingProcessor handles memory-efficient processing of large migration sets
type StreamingProcessor struct {
	batchSize   int
	workerCount int
	memManager  *MemoryManager
	input       chan *MigrationFile
	output      chan *ProcessedFile
	errors      chan error
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	stats       *ProcessingStats
	logger      *utils.Logger
}

// MigrationFile represents a migration file to be processed
type MigrationFile struct {
	Path     string
	Content  []byte
	Sequence int
	Size     int64
}

// ProcessedFile represents a processed migration file
type ProcessedFile struct {
	OriginalFile *MigrationFile
	Migration    *types.Migration
	ProcessTime  time.Duration
	MemoryUsed   int64
	Errors       []error
}

// ProcessingStats tracks processing statistics
type ProcessingStats struct {
	FilesProcessed  int64   `json:"files_processed"`
	FilesSkipped    int64   `json:"files_skipped"`
	FilesErrored    int64   `json:"files_errored"`
	TotalBytes      int64   `json:"total_bytes"`
	ProcessingTime  int64   `json:"processing_time_ms"`
	PeakMemoryUsage int64   `json:"peak_memory_usage"`
	AverageFileSize int64   `json:"average_file_size"`
	ThroughputMBps  float64 `json:"throughput_mbps"`
}

// NewStreamingProcessor creates a new streaming processor
func NewStreamingProcessor(batchSize, workerCount int, memManager *MemoryManager) *StreamingProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	// Create logger with appropriate level
	logger := utils.NewLogger(utils.LogLevelInfo, os.Stdout).WithPrefix("StreamingProcessor")

	return &StreamingProcessor{
		batchSize:   batchSize,
		workerCount: workerCount,
		memManager:  memManager,
		input:       make(chan *MigrationFile, batchSize*2),
		output:      make(chan *ProcessedFile, batchSize*2),
		errors:      make(chan error, workerCount),
		ctx:         ctx,
		cancel:      cancel,
		stats:       &ProcessingStats{},
		logger:      logger,
	}
}

// Start begins the streaming processing
func (sp *StreamingProcessor) Start() {
	// Start worker goroutines
	for i := 0; i < sp.workerCount; i++ {
		sp.wg.Add(1)
		go sp.worker(i)
	}

	// Start error handler
	go sp.errorHandler()
}

// ProcessDirectory processes all migration files in a directory
func (sp *StreamingProcessor) ProcessDirectory(dir string) error {
	startTime := time.Now()

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip non-SQL files
		if !isSQLFile(path) {
			return nil
		}

		// Check if context is cancelled
		select {
		case <-sp.ctx.Done():
			return sp.ctx.Err()
		default:
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			sp.errors <- errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryPerformance, fmt.Sprintf("failed to read file %s", path), nil)
			return nil // Continue processing other files
		}

		// Create migration file object
		migrationFile := &MigrationFile{
			Path:    path,
			Content: content,
			Size:    info.Size(),
		}

		// Check memory constraints
		if !sp.memManager.TrackMemoryUsage(migrationFile.Size) {
			sp.errors <- errors.New(errors.ErrorCodeValidationFailed, errors.CategoryPerformance, fmt.Sprintf("memory limit exceeded for file %s", path), nil)
			atomic.AddInt64(&sp.stats.FilesSkipped, 1)
			return nil
		}

		// Send to processing pipeline
		select {
		case sp.input <- migrationFile:
			atomic.AddInt64(&sp.stats.TotalBytes, migrationFile.Size)
		case <-sp.ctx.Done():
			sp.memManager.ReleaseMemory(migrationFile.Size)
			return sp.ctx.Err()
		}

		return nil
	})

	// Close input channel to signal completion
	close(sp.input)

	// Update processing time
	processingTime := time.Since(startTime)
	atomic.StoreInt64(&sp.stats.ProcessingTime, processingTime.Milliseconds())

	return err
}

// worker processes migration files
func (sp *StreamingProcessor) worker(id int) {
	defer sp.wg.Done()

	for {
		select {
		case migrationFile, ok := <-sp.input:
			if !ok {
				return // Input channel closed
			}

			sp.processFile(migrationFile)

		case <-sp.ctx.Done():
			return
		}
	}
}

// processFile processes a single migration file
func (sp *StreamingProcessor) processFile(migrationFile *MigrationFile) {
	startTime := time.Now()
	var processedFile *ProcessedFile

	defer func() {
		// Release memory
		sp.memManager.ReleaseMemory(migrationFile.Size)

		// Update statistics
		atomic.AddInt64(&sp.stats.FilesProcessed, 1)

		// Send processed file to output
		if processedFile != nil {
			processedFile.ProcessTime = time.Since(startTime)
			select {
			case sp.output <- processedFile:
			case <-sp.ctx.Done():
			}
		}
	}()

	// Parse the migration file
	migration, err := parser.ParseMigrationWithContext(sp.ctx, string(migrationFile.Content), migrationFile.Path)

	processedFile = &ProcessedFile{
		OriginalFile: migrationFile,
		Migration:    migration,
		MemoryUsed:   migrationFile.Size,
	}

	if err != nil {
		processedFile.Errors = []error{err}
		atomic.AddInt64(&sp.stats.FilesErrored, 1)
		sp.errors <- errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryPerformance, fmt.Sprintf("failed to parse file %s", migrationFile.Path), nil)
		return
	}

	// Update peak memory usage
	memStats := sp.memManager.GetMemoryStats()
	for {
		currentPeak := atomic.LoadInt64(&sp.stats.PeakMemoryUsage)
		if memStats.CurrentMemoryBytes <= currentPeak {
			break
		}
		if atomic.CompareAndSwapInt64(&sp.stats.PeakMemoryUsage, currentPeak, memStats.CurrentMemoryBytes) {
			break
		}
	}
}

// errorHandler handles processing errors
func (sp *StreamingProcessor) errorHandler() {
	for {
		select {
		case err := <-sp.errors:
			// Log error using structured logger
			sp.logger.Error("Processing error: %v", err)

		case <-sp.ctx.Done():
			sp.logger.Debug("Error handler shutting down")
			return
		}
	}
}

// GetResults returns a channel of processed files
func (sp *StreamingProcessor) GetResults() <-chan *ProcessedFile {
	return sp.output
}

// GetStats returns current processing statistics
func (sp *StreamingProcessor) GetStats() ProcessingStats {
	stats := *sp.stats
	stats.FilesProcessed = atomic.LoadInt64(&sp.stats.FilesProcessed)
	stats.FilesSkipped = atomic.LoadInt64(&sp.stats.FilesSkipped)
	stats.FilesErrored = atomic.LoadInt64(&sp.stats.FilesErrored)
	stats.TotalBytes = atomic.LoadInt64(&sp.stats.TotalBytes)
	stats.ProcessingTime = atomic.LoadInt64(&sp.stats.ProcessingTime)
	stats.PeakMemoryUsage = atomic.LoadInt64(&sp.stats.PeakMemoryUsage)

	// Calculate derived statistics
	if stats.FilesProcessed > 0 {
		stats.AverageFileSize = stats.TotalBytes / stats.FilesProcessed
	}

	if stats.ProcessingTime > 0 {
		totalMB := float64(stats.TotalBytes) / (1024 * 1024)
		timeSeconds := float64(stats.ProcessingTime) / 1000
		stats.ThroughputMBps = totalMB / timeSeconds
	}

	return stats
}

// Stop gracefully stops the streaming processor
func (sp *StreamingProcessor) Stop() error {
	sp.cancel()
	sp.wg.Wait()
	close(sp.output)
	close(sp.errors)
	return nil
}

// BatchProcessor handles batch processing with configurable batch sizes
type BatchProcessor struct {
	batchSize        int
	memManager       *MemoryManager
	deduplicator     *Deduplicator[*MigrationFile]
	currentBatch     []*MigrationFile
	currentBatchSize int64
	maxBatchSize     int64
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(batchSize int, maxBatchSizeMB int, memManager *MemoryManager) *BatchProcessor {
	dedup := NewDeduplicator[*MigrationFile](func(file *MigrationFile) string {
		return fmt.Sprintf("%s_%d", filepath.Base(file.Path), file.Size)
	})

	return &BatchProcessor{
		batchSize:    batchSize,
		memManager:   memManager,
		deduplicator: dedup,
		maxBatchSize: int64(maxBatchSizeMB) * 1024 * 1024,
	}
}

// AddFile adds a file to the current batch
func (bp *BatchProcessor) AddFile(file *MigrationFile) (bool, error) {
	// Check for duplicates
	if bp.deduplicator.IsDuplicate(file) {
		return false, nil // Skip duplicate
	}

	// Check if adding this file would exceed batch limits
	if len(bp.currentBatch) >= bp.batchSize || bp.currentBatchSize+file.Size > bp.maxBatchSize {
		return true, nil // Batch is full, needs processing
	}

	bp.currentBatch = append(bp.currentBatch, file)
	bp.currentBatchSize += file.Size

	return false, nil
}

// GetCurrentBatch returns the current batch and resets it
func (bp *BatchProcessor) GetCurrentBatch() []*MigrationFile {
	batch := bp.currentBatch
	bp.currentBatch = make([]*MigrationFile, 0, bp.batchSize)
	bp.currentBatchSize = 0
	return batch
}

// HasPendingBatch returns true if there are files in the current batch
func (bp *BatchProcessor) HasPendingBatch() bool {
	return len(bp.currentBatch) > 0
}

// FileStreamReader provides streaming file reading capabilities
type FileStreamReader struct {
	reader     io.Reader
	bufferSize int
	buffer     []byte
	position   int64
	memManager *MemoryManager
}

// NewFileStreamReader creates a new file stream reader
func NewFileStreamReader(reader io.Reader, bufferSizeKB int, memManager *MemoryManager) *FileStreamReader {
	bufferSize := bufferSizeKB * 1024

	return &FileStreamReader{
		reader:     reader,
		bufferSize: bufferSize,
		buffer:     make([]byte, bufferSize),
		memManager: memManager,
	}
}

// ReadChunk reads the next chunk of data
func (fsr *FileStreamReader) ReadChunk() ([]byte, error) {
	n, err := fsr.reader.Read(fsr.buffer)
	if err != nil && err != io.EOF {
		return nil, err
	}

	if n == 0 {
		return nil, io.EOF
	}

	fsr.position += int64(n)
	return fsr.buffer[:n], nil
}

// GetPosition returns the current read position
func (fsr *FileStreamReader) GetPosition() int64 {
	return fsr.position
}

// Helper functions

// isSQLFile checks if a file is a SQL migration file
func isSQLFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".sql" || ext == ".SQL"
}

// ProgressTracker tracks processing progress for large operations
type ProgressTracker struct {
	total      int64
	current    int64
	lastUpdate time.Time
	updateFreq time.Duration
	callback   func(current, total int64, rate float64)
	mu         sync.RWMutex
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(total int64, updateFreq time.Duration, callback func(int64, int64, float64)) *ProgressTracker {
	return &ProgressTracker{
		total:      total,
		lastUpdate: time.Now(),
		updateFreq: updateFreq,
		callback:   callback,
	}
}

// Update updates the progress counter
func (pt *ProgressTracker) Update(increment int64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.current += increment

	// Check if we should update
	now := time.Now()
	if now.Sub(pt.lastUpdate) >= pt.updateFreq {
		rate := float64(pt.current) / now.Sub(pt.lastUpdate.Add(-pt.updateFreq)).Seconds()
		if pt.callback != nil {
			pt.callback(pt.current, pt.total, rate)
		}
		pt.lastUpdate = now
	}
}

// GetProgress returns current progress statistics
func (pt *ProgressTracker) GetProgress() (current, total int64, percentage float64) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	percentage = 0
	if pt.total > 0 {
		percentage = float64(pt.current) / float64(pt.total) * 100
	}

	return pt.current, pt.total, percentage
}
