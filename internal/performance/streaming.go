package performance

import (
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"

)

// MigrationFile represents a migration file to be processed
type MigrationFile struct {
	Path     string
	Content  []byte
	Sequence int
	Size     int64
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
