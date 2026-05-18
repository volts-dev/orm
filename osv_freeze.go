package orm

import (
	"context"

	"github.com/volts-dev/utils"
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
//
// Phases:
//   2 (LocalResolve)  — bind refs whose target is already registered locally,
//                       including One2One field inheritance
//   3 (RemoteResolve) — for unresolved refs, ask resolver to LookupSchema and
//                       register a TRemoteModelObject locally (added in Task 7)
//   4 (Verify)        — strict-mode validation (added in Task 9)
//
// Idempotent: calling twice is a no-op (second call returns nil immediately).
func (self *TOsv) Freeze(ctx context.Context) error {
	self.freezeLock.Lock()
	defer self.freezeLock.Unlock()

	if self.frozen {
		return nil
	}

	self.pendingLock.Lock()
	pending := self.pendingRefs
	self.pendingRefs = nil
	self.pendingLock.Unlock()

	// Phase 2: LocalResolve
	var stillPending []fieldRef
	for _, ref := range pending {
		obj, ok := self.models.Load(ref.toModel)
		if !ok {
			stillPending = append(stillPending, ref)
			continue
		}
		if err := self.linkLocal(ref, obj.(*TModelObject)); err != nil {
			return err
		}
	}

	// Phase 3 placeholder — implemented in Task 7
	self.unresolvedRefs = stillPending

	self.frozen = true
	return nil
}

// linkLocal performs the post-Phase-1 wiring for a ref that resolved to a
// local TModelObject. For One2One refs this includes copying parent fields
// into the owning model (the inheritance that Phase 1 used to do eagerly).
//
// For M2O/O2M/M2M, linkLocal is a no-op because resolution is performed lazily
// at runtime via osv.GetModel(name); having the target registered is sufficient.
func (self *TOsv) linkLocal(ref fieldRef, target *TModelObject) error {
	switch ref.fieldType {
	case TYPE_O2O:
		return self.inheritO2OFields(ref, target)
	default:
		return nil
	}
}

// inheritO2OFields is the deferred version of the One2One field inheritance
// previously done eagerly in TOne2OneField.Init. It walks every field on the
// parent (target) model and either:
//   - registers it on the owning model with isInherited=true, or
//   - records it as a "common field" if both sides already have it.
func (self *TOsv) inheritO2OFields(ref fieldRef, target *TModelObject) error {
	ownerObj, ok := self.models.Load(ref.fromModel)
	if !ok {
		return nil // owner missing → nothing to inherit into
	}
	owner := ownerObj.(*TModelObject)

	target.fields.Range(func(key, value any) bool {
		parentField, ok := value.(IField)
		if !ok {
			return true
		}
		fieldName := parentField.Name()
		clonedAny := utils.Clone(parentField)
		newField, ok := clonedAny.(IField)
		if !ok {
			return true
		}
		newField.SetBase(parentField.Base())

		if existingVal, has := owner.fields.Load(fieldName); has {
			existing := existingVal.(IField)
			owner.SetCommonFieldByName(fieldName, target.name, newField)
			owner.SetCommonFieldByName(fieldName, existing.Base().modelName, existing)
		} else {
			newField.Base().isInherited = true
			newField.Base().store = false

			if newField.IsAutoIncrement() {
				owner.AutoIncrementField = fieldName
			}
			// Note: setting owner.idField (which lives on TModel, not
			// TModelObject) was done by the original eager-init path. The
			// model's own primary key is already set during initial
			// registration; we don't need to re-set it here.

			owner.SetField(newField)
		}
		return true
	})
	return nil
}
