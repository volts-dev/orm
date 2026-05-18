package orm

import (
	"context"
)

// fieldRef records a relational field that needs target-model resolution.
// Collected during model registration (Phase 1) and processed by Freeze
// in Phases 2 (LocalResolve), 3 (RemoteResolve) and 4 (Verify).
type fieldRef struct {
	fromModel string // owning model name, e.g. "website.page"
	fieldName string // field name on owning model, e.g. "AuthorId"
	toModel   string // related model name, e.g. "sys.user"
	fieldType string // TYPE_O2O / TYPE_M2O / TYPE_O2M / TYPE_M2M
}

// markPending records a deferred reference for later resolution by Freeze.
// Safe for concurrent use.
func (self *TOsv) markPending(ref fieldRef) {
	self.pendingLock.Lock()
	self.pendingRefs = append(self.pendingRefs, ref)
	self.pendingLock.Unlock()
}

// Freeze ends the model registration phase and resolves all pending field refs.
// Filled in by Task 4 (Phase 2) and Task 7 (Phase 3).
func (self *TOsv) Freeze(ctx context.Context) error {
	return nil
}
