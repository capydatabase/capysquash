package tracking

import (
	"github.com/capy-base/pgsquash-engine/internal/metadata"
)

// Tracker is an alias for UnifiedTracker to maintain API compatibility
type Tracker = UnifiedTracker

// NewTracker creates a new unified tracker (alias for NewUnifiedTracker)
func NewTracker() *Tracker {
	return NewUnifiedTracker()
}

// NewTrackerWithMetadata creates a tracker with metadata manager integration
func NewTrackerWithMetadata(metaMgr *metadata.MetadataManager) *Tracker {
	return NewUnifiedTrackerWithMetadata(metaMgr)
}

// ===== CONSOLIDATION INTERFACES =====
// These interfaces are defined in the parent package to avoid import cycles.
// Implementations are in the consolidation/ subdirectory.

// ConsolidationRule interface for consolidation rules used by the squasher
type ConsolidationRule interface {
	CanApply(lifecycle *ObjectLifecycle) bool
	Apply(lifecycle *ObjectLifecycle, engine ConsolidationEngine) (*ConsolidationResult, error)
	Risk() RiskLevel
}

// ConsolidationEngine interface for the engine that applies consolidation rules
type ConsolidationEngine interface {
	GetTracker() *Tracker
	GetConfig() any // This would be the actual config type
}
