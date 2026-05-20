package orm

import (
	"context"
	"fmt"
	"strings"

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
//
//	2 (LocalResolve)  — bind refs whose target is already registered locally,
//	                    including One2One field inheritance
//	3 (RemoteResolve) — for unresolved refs, ask resolver to LookupSchema and
//	                    register a TRemoteModelObject locally (added in Task 7)
//	4 (Verify)        — strict-mode validation (added in Task 9)
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
		// Local target may be a *TModelObject (normal) or a *TRemoteModelObject
		// (e.g. registered earlier in this same Freeze pass). For local objects,
		// proceed with linkLocal; for remote objects, treat as a Phase 3 hit.
		switch t := obj.(type) {
		case *TModelObject:
			if err := self.linkLocal(ref, t); err != nil {
				return err
			}
		case *TRemoteModelObject:
			if err := self.linkRemote(ref, t); err != nil {
				return err
			}
		default:
			// unknown type — keep pending and let Phase 3 try resolving again
			stillPending = append(stillPending, ref)
		}
	}

	// Phase 3: RemoteResolve
	var unresolved []fieldRef
	for _, ref := range stillPending {
		if self.resolver == nil {
			unresolved = append(unresolved, ref)
			continue
		}
		// 已经在前面循环里注册过的远程 model 直接复用（dedup）
		if obj, ok := self.models.Load(ref.toModel); ok {
			if rm, isRemote := obj.(*TRemoteModelObject); isRemote {
				if err := self.linkRemote(ref, rm); err != nil {
					return err
				}
				continue
			}
		}
		schema, err := self.resolver.LookupSchema(ctx, ref.toModel)
		if err != nil {
			unresolved = append(unresolved, ref)
			continue
		}
		// One2One→remote 不支持（继承语义跨服务无意义）
		if ref.fieldType == TYPE_O2O {
			return fmt.Errorf("orm: One2One field %s.%s cannot reference remote model %q",
				ref.fromModel, ref.fieldName, ref.toModel)
		}
		remote := newRemoteModelObject(schema, self.resolver)
		remote.orm = self.orm
		remote.osv = self
		self.models.Store(ref.toModel, remote)
		if err := self.linkRemote(ref, remote); err != nil {
			return err
		}
	}

	// Phase 4: Verify (strict mode only)
	if self.strictMode && len(unresolved) > 0 {
		names := make([]string, 0, len(unresolved))
		for _, r := range unresolved {
			names = append(names, fmt.Sprintf("%s.%s → %s", r.fromModel, r.fieldName, r.toModel))
		}
		return fmt.Errorf("orm: %d unresolved model references after Freeze: %s",
			len(unresolved), strings.Join(names, "; "))
	}

	self.unresolvedRefs = unresolved

	self.frozen = true
	return nil
}

// linkRemote performs ref binding for a remote target. For M2O/O2M/M2M it's
// currently a no-op because resolution is via name lookup; the target being
// registered in osv as a TRemoteModelObject is sufficient. One2One→remote is
// rejected in Phase 4 (next task).
func (self *TOsv) linkRemote(ref fieldRef, target *TRemoteModelObject) error {
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
