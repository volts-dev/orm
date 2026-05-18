package orm

import (
	"context"
	"testing"
)

func TestOsvMarkPending(t *testing.T) {
	osv := &TOsv{}
	osv.markPending(fieldRef{
		fromModel: "website.page",
		fieldName: "AuthorId",
		toModel:   "sys.user",
		fieldType: TYPE_M2O,
	})
	osv.markPending(fieldRef{
		fromModel: "website.page",
		fieldName: "TagIds",
		toModel:   "website.tag",
		fieldType: TYPE_M2M,
	})

	if got := len(osv.pendingRefs); got != 2 {
		t.Fatalf("expected 2 pending refs, got %d", got)
	}
	if osv.pendingRefs[0].toModel != "sys.user" {
		t.Fatalf("unexpected first ref: %+v", osv.pendingRefs[0])
	}
}

// 验证 One2One 字段引用未注册 model 不再 Fatalf，而是记录到 pendingRefs。
func TestOne2OneInitDoesNotFatalOnMissingModel(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registration must not panic on missing related model, got: %v", r)
		}
	}()

	osv := &TOsv{}
	orm := &TOrm{osv: osv}
	osv.orm = orm

	field := &TField{}
	field.SetName("PartnerId")
	field.Base().modelName = "test.invoice"

	model := newMinimalTestModel("test.invoice")
	ctx := &TTagContext{
		Orm:    orm,
		Field:  field,
		Model:  model,
		Params: []string{"res.partner"},
	}

	o2o := &TOne2OneField{}
	o2o.Init(ctx)

	if got := len(osv.pendingRefs); got != 1 {
		t.Fatalf("expected 1 pending ref recorded, got %d", got)
	}
	// toModel goes through fmtModelName/TitleCasedName — exact form depends on those.
	// Just verify it's non-empty and roughly recognizable.
	if osv.pendingRefs[0].toModel == "" {
		t.Fatal("pendingRefs[0].toModel must not be empty")
	}
	if osv.pendingRefs[0].fieldType != TYPE_O2O {
		t.Fatalf("expected fieldType TYPE_O2O, got %q", osv.pendingRefs[0].fieldType)
	}
	if osv.pendingRefs[0].fromModel != "test.invoice" {
		t.Fatalf("expected fromModel=test.invoice, got %q", osv.pendingRefs[0].fromModel)
	}
}

func newMinimalTestModel(name string) *TModel {
	m := &TModel{name: name}
	m.obj = &TModelObject{name: name}
	return m
}

func TestWithRemoteResolver_AppliedToConfig(t *testing.T) {
	cfg := newConfig(WithRemoteResolver(&mockRemoteResolver{}))
	if cfg.RemoteResolver == nil {
		t.Fatal("WithRemoteResolver did not set cfg.RemoteResolver")
	}
}

func TestWithStrictModelResolution_AppliedToConfig(t *testing.T) {
	cfg := newConfig(WithStrictModelResolution(true))
	if !cfg.StrictModelResolution {
		t.Fatal("WithStrictModelResolution(true) did not set cfg.StrictModelResolution")
	}
}

// Phase 2 — A↔B 互相 m2o，注册顺序任意，Freeze 后都能成功解析。
func TestFreeze_LocalCircularDeps(t *testing.T) {
	osv := &TOsv{}

	osv.models.Store("test.a", &TModelObject{name: "test.a"})
	osv.markPending(fieldRef{
		fromModel: "test.a", fieldName: "BId", toModel: "test.b", fieldType: TYPE_M2O,
	})
	osv.models.Store("test.b", &TModelObject{name: "test.b"})
	osv.markPending(fieldRef{
		fromModel: "test.b", fieldName: "AId", toModel: "test.a", fieldType: TYPE_M2O,
	})

	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze must succeed for resolvable local circular deps, got: %v", err)
	}
	if len(osv.pendingRefs) != 0 {
		t.Fatalf("pendingRefs must be empty after Freeze, got %d", len(osv.pendingRefs))
	}
	if len(osv.unresolvedRefs) != 0 {
		t.Fatalf("unresolvedRefs must be empty when all targets are local, got %d", len(osv.unresolvedRefs))
	}
	if !osv.frozen {
		t.Fatal("Freeze must set frozen=true on success")
	}
}

// 不带 resolver 且非 strict 模式下，缺失 model 进 unresolvedRefs，Freeze 不报错。
func TestFreeze_NoResolver_NonStrict_KeepsUnresolved(t *testing.T) {
	osv := &TOsv{}
	osv.models.Store("test.a", &TModelObject{name: "test.a"})
	osv.markPending(fieldRef{
		fromModel: "test.a", fieldName: "Xid", toModel: "missing.model", fieldType: TYPE_M2O,
	})

	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatalf("non-strict mode must not error on missing model, got: %v", err)
	}
	if len(osv.unresolvedRefs) != 1 {
		t.Fatalf("expected 1 unresolvedRef, got %d", len(osv.unresolvedRefs))
	}
}

// 第二次 Freeze 应当幂等（重复调用不重复处理）。
func TestFreeze_Idempotent(t *testing.T) {
	osv := &TOsv{}
	osv.models.Store("test.a", &TModelObject{name: "test.a"})
	osv.markPending(fieldRef{
		fromModel: "test.a", fieldName: "Self", toModel: "test.a", fieldType: TYPE_M2O,
	})
	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatalf("second Freeze must be a no-op, got: %v", err)
	}
}

func TestOsvMarkPendingConcurrent(t *testing.T) {
	osv := &TOsv{}
	const N = 100
	done := make(chan struct{})
	for i := 0; i < N; i++ {
		go func(i int) {
			osv.markPending(fieldRef{
				fromModel: "m",
				fieldName: "f",
				toModel:   "t",
				fieldType: TYPE_M2O,
			})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < N; i++ {
		<-done
	}
	if got := len(osv.pendingRefs); got != N {
		t.Fatalf("expected %d refs after concurrent markPending, got %d", N, got)
	}
}
