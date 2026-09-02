package performance

import (
	"container/list"
	"runtime"
	"sync"
	"time"

	"github.com/capy-base/pgsquash-engine/internal/types"
)

// MemoryManager handles memory-efficient processing with deduplication and size limits
type MemoryManager struct {
	maxMemoryBytes int64
	currentMemory  int64
	statementPool  *sync.Pool
	builderPool    *sync.Pool
	parseCache     *LRUCache[string, *types.Statement]
	mu             sync.RWMutex
	gcThreshold    int64
	lastGC         time.Time
}

// NewMemoryManager creates a memory manager with the specified limits
func NewMemoryManager(maxMemoryMB int) *MemoryManager {
	maxBytes := int64(maxMemoryMB) * 1024 * 1024

	mm := &MemoryManager{
		maxMemoryBytes: maxBytes,
		gcThreshold:    maxBytes / 4, // Trigger GC at 25% memory usage
		lastGC:         time.Now(),
		parseCache:     NewLRUCache[string, *types.Statement](1000), // Cache 1000 parsed statements
	}

	// Initialize object pools
	mm.statementPool = &sync.Pool{
		New: func() any {
			return &types.Statement{}
		},
	}

	mm.builderPool = &sync.Pool{
		New: func() any {
			b := make([]byte, 0, 1024) // Pre-allocate 1KB buffers
			return &b
		},
	}

	return mm
}

// GetStatement retrieves a pooled statement object
func (mm *MemoryManager) GetStatement() *types.Statement {
	stmt := mm.statementPool.Get().(*types.Statement)
	// Reset the statement
	*stmt = types.Statement{}
	return stmt
}

// PutStatement returns a statement to the pool
func (mm *MemoryManager) PutStatement(stmt *types.Statement) {
	if stmt != nil {
		mm.statementPool.Put(stmt)
	}
}

// GetBuffer retrieves a pooled byte buffer
func (mm *MemoryManager) GetBuffer() []byte {
	buf := mm.builderPool.Get().(*[]byte)
	return (*buf)[:0] // Reset length to 0
}

// PutBuffer returns a buffer to the pool
func (mm *MemoryManager) PutBuffer(buf []byte) {
	if cap(buf) <= 64*1024 { // Only pool buffers <= 64KB
		buf = buf[:0]
		mm.builderPool.Put(&buf)
	}
}

// TrackMemoryUsage updates current memory usage
func (mm *MemoryManager) TrackMemoryUsage(size int64) bool {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if mm.currentMemory+size > mm.maxMemoryBytes {
		return false // Would exceed memory limit
	}

	mm.currentMemory += size

	// Trigger GC if needed
	if mm.currentMemory > mm.gcThreshold && time.Since(mm.lastGC) > 30*time.Second {
		mm.triggerGC()
	}

	return true
}

// ReleaseMemory decreases tracked memory usage
func (mm *MemoryManager) ReleaseMemory(size int64) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.currentMemory -= size
	if mm.currentMemory < 0 {
		mm.currentMemory = 0
	}
}

// triggerGC performs garbage collection and cache cleanup
func (mm *MemoryManager) triggerGC() {
	// Clear parse cache to free memory
	mm.parseCache.Clear()

	// Force garbage collection
	runtime.GC()
	mm.lastGC = time.Now()

	// Update current memory based on actual usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	mm.currentMemory = int64(memStats.Alloc)
}

// GetMemoryStats returns current memory usage statistics
func (mm *MemoryManager) GetMemoryStats() MemoryStats {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return MemoryStats{
		MaxMemoryBytes:     mm.maxMemoryBytes,
		CurrentMemoryBytes: mm.currentMemory,
		SystemMemoryBytes:  int64(memStats.Alloc),
		CacheSize:          mm.parseCache.Size(),
		CacheHitRate:       mm.parseCache.HitRate(),
	}
}

// MemoryStats provides memory usage information
type MemoryStats struct {
	MaxMemoryBytes     int64   `json:"max_memory_bytes"`
	CurrentMemoryBytes int64   `json:"current_memory_bytes"`
	SystemMemoryBytes  int64   `json:"system_memory_bytes"`
	CacheSize          int     `json:"cache_size"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
}

// LRUCache provides memory-efficient caching with least-recently-used eviction
type LRUCache[K comparable, V any] struct {
	capacity int
	items    map[K]*list.Element
	lru      *list.List
	hits     int64
	misses   int64
	mu       sync.RWMutex
}

// CacheItem represents a cached item
type CacheItem[K comparable, V any] struct {
	Key   K
	Value V
}

// NewLRUCache creates a new LRU cache with the specified capacity
func NewLRUCache[K comparable, V any](capacity int) *LRUCache[K, V] {
	return &LRUCache[K, V]{
		capacity: capacity,
		items:    make(map[K]*list.Element),
		lru:      list.New(),
	}
}

// Get retrieves an item from the cache
func (c *LRUCache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, exists := c.items[key]; exists {
		c.hits++
		c.lru.MoveToFront(element)
		return element.Value.(*CacheItem[K, V]).Value, true
	}

	c.misses++
	var zero V
	return zero, false
}

// Put adds an item to the cache
func (c *LRUCache[K, V]) Put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, exists := c.items[key]; exists {
		c.lru.MoveToFront(element)
		element.Value.(*CacheItem[K, V]).Value = value
		return
	}

	if c.lru.Len() >= c.capacity {
		// Evict least recently used item
		oldest := c.lru.Back()
		if oldest != nil {
			c.lru.Remove(oldest)
			delete(c.items, oldest.Value.(*CacheItem[K, V]).Key)
		}
	}

	item := &CacheItem[K, V]{Key: key, Value: value}
	element := c.lru.PushFront(item)
	c.items[key] = element
}

// Size returns the current number of items in the cache
func (c *LRUCache[K, V]) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lru.Len()
}

// HitRate returns the cache hit rate
func (c *LRUCache[K, V]) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	if total == 0 {
		return 0.0
	}
	return float64(c.hits) / float64(total)
}

// Clear removes all items from the cache
func (c *LRUCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lru.Init()
	c.items = make(map[K]*list.Element)
	c.hits = 0
	c.misses = 0
}

// Deduplicator handles duplicate statement detection and removal
type Deduplicator[T any] struct {
	seen     map[string]bool
	hashFunc func(T) string
	mu       sync.RWMutex
}

// NewDeduplicator creates a new deduplicator with the specified hash function
func NewDeduplicator[T any](hashFunc func(T) string) *Deduplicator[T] {
	return &Deduplicator[T]{
		seen:     make(map[string]bool),
		hashFunc: hashFunc,
	}
}

// IsDuplicate checks if an item has been seen before
func (d *Deduplicator[T]) IsDuplicate(item T) bool {
	hash := d.hashFunc(item)

	d.mu.RLock()
	exists := d.seen[hash]
	d.mu.RUnlock()

	if exists {
		return true
	}

	d.mu.Lock()
	d.seen[hash] = true
	d.mu.Unlock()

	return false
}

// Reset clears the deduplicator state
func (d *Deduplicator[T]) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen = make(map[string]bool)
}

// GetStats returns deduplication statistics
func (d *Deduplicator[T]) GetStats() DeduplicationStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return DeduplicationStats{
		UniqueItems: len(d.seen),
	}
}

// DeduplicationStats provides deduplication information
type DeduplicationStats struct {
	UniqueItems int `json:"unique_items"`
}

// MemoryOptimizedStatement wraps a statement with memory optimization features
type MemoryOptimizedStatement struct {
	*types.Statement
	memoryManager *MemoryManager
	size          int64
}

// NewMemoryOptimizedStatement creates a statement with memory tracking
func NewMemoryOptimizedStatement(mm *MemoryManager, stmt *types.Statement) *MemoryOptimizedStatement {
	size := estimateStatementSize(stmt)
	if !mm.TrackMemoryUsage(size) {
		return nil // Memory limit exceeded
	}

	return &MemoryOptimizedStatement{
		Statement:     stmt,
		memoryManager: mm,
		size:          size,
	}
}

// Release frees the memory used by this statement
func (mos *MemoryOptimizedStatement) Release() {
	if mos.memoryManager != nil && mos.size > 0 {
		mos.memoryManager.ReleaseMemory(mos.size)
		mos.memoryManager.PutStatement(mos.Statement)
		mos.Statement = nil
		mos.size = 0
	}
}

// estimateStatementSize estimates the memory usage of a statement
func estimateStatementSize(stmt *types.Statement) int64 {
	if stmt == nil {
		return 0
	}

	size := int64(len(stmt.SQL))
	size += int64(len(stmt.ObjectName) + len(stmt.Schema))
	size += int64(len(stmt.Comments) * 50) // Rough estimate for comments
	size += int64(len(stmt.Dependencies) * 30)
	size += 200 // Base structure overhead

	return size
}
