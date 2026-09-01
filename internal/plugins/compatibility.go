package plugins

// ResolveConflicts applies priority-based conflict resolution to an arbitrary
// set of plugins and returns the plugins that remain active, ordered by
// priority (highest first).
//
// This is the exact resolution logic the registry applies during
// DiscoverAndInitialize: plugins are visited in descending priority order and
// each surviving plugin excludes every plugin named by its
// GetConflictingPlugins(). It is exported so public API surfaces (pkg/plugins)
// can report compatibility identically to the squashing pipeline.
func ResolveConflicts(candidates []Plugin) []Plugin {
	return NewRegistry().resolveConflicts(candidates)
}
