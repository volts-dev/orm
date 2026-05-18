package orm

import (
	"context"
	"strings"
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

// Phase 3 — resolver 返回 schema 时，构造 TRemoteModelObject 注册进 osv。
func TestFreeze_RemoteResolve_Success(t *testing.T) {
	resolver := &mockRemoteResolver{
		schemas: map[string]*ModelSchema{
			"sys.user": {
				Name:    "sys.user",
				IdField: "id",
				Fields: []FieldSchema{
					{Name: "id", TypeName: "int", SqlType: "INT8"},
					{Name: "name", TypeName: "chars", SqlType: "VARCHAR(255)"},
				},
				SourceNode: "tcp://base:9000",
			},
		},
	}
	osv := &TOsv{resolver: resolver}
	osv.models.Store("website.page", &TModelObject{name: "website.page"})
	osv.markPending(fieldRef{
		fromModel: "website.page", fieldName: "AuthorId", toModel: "sys.user", fieldType: TYPE_M2O,
	})

	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze must succeed when resolver returns schema, got: %v", err)
	}
	if resolver.lookupCalled != 1 {
		t.Fatalf("expected 1 LookupSchema call, got %d", resolver.lookupCalled)
	}

	obj, ok := osv.models.Load("sys.user")
	if !ok {
		t.Fatal("sys.user must be registered in osv after Phase 3")
	}
	if _, isRemote := obj.(*TRemoteModelObject); !isRemote {
		t.Fatalf("expected *TRemoteModelObject for sys.user, got %T", obj)
	}
}

func TestFreeze_RemoteResolve_NotFound_NonStrict(t *testing.T) {
	resolver := &mockRemoteResolver{schemas: map[string]*ModelSchema{}}
	osv := &TOsv{resolver: resolver}
	osv.models.Store("website.page", &TModelObject{name: "website.page"})
	osv.markPending(fieldRef{
		fromModel: "website.page", fieldName: "AuthorId", toModel: "sys.user", fieldType: TYPE_M2O,
	})

	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatalf("non-strict mode must not error on unresolved remote, got: %v", err)
	}
	if len(osv.unresolvedRefs) != 1 {
		t.Fatalf("expected 1 unresolvedRef, got %d", len(osv.unresolvedRefs))
	}
}

// 同一远程 model 被多个 ref 引用时，应当只 LookupSchema 一次（去重）。
func TestFreeze_RemoteResolve_Dedup(t *testing.T) {
	resolver := &mockRemoteResolver{
		schemas: map[string]*ModelSchema{
			"sys.user": {Name: "sys.user", IdField: "id"},
		},
	}
	osv := &TOsv{resolver: resolver}
	osv.models.Store("website.page", &TModelObject{name: "website.page"})
	osv.models.Store("website.comment", &TModelObject{name: "website.comment"})
	osv.markPending(fieldRef{fromModel: "website.page", fieldName: "AuthorId", toModel: "sys.user", fieldType: TYPE_M2O})
	osv.markPending(fieldRef{fromModel: "website.comment", fieldName: "AuthorId", toModel: "sys.user", fieldType: TYPE_M2O})

	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze failed: %v", err)
	}
	if resolver.lookupCalled != 1 {
		t.Fatalf("expected 1 LookupSchema call (dedup), got %d", resolver.lookupCalled)
	}
}

func TestRegisterModel_RejectsAfterFreeze(t *testing.T) {
	osv := &TOsv{}
	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	model := &TModel{name: "test.late"}
	model.obj = &TModelObject{name: "test.late"}
	if err := osv.RegisterModel("", model); err != ErrOsvFrozen {
		t.Fatalf("expected ErrOsvFrozen after Freeze, got %v", err)
	}
}

// Phase 4 — strict mode 下 unresolved 必须报错，错误信息列出所有 ref。
func TestFreeze_StrictMode_FailsOnUnresolved(t *testing.T) {
	osv := &TOsv{strictMode: true}
	osv.models.Store("website.page", &TModelObject{name: "website.page"})
	osv.markPending(fieldRef{
		fromModel: "website.page", fieldName: "X", toModel: "missing.model", fieldType: TYPE_M2O,
	})

	err := osv.Freeze(context.Background())
	if err == nil {
		t.Fatal("strict mode must return error on unresolved refs")
	}
	if !strings.Contains(err.Error(), "missing.model") {
		t.Fatalf("error must name the unresolved model, got: %v", err)
	}
}

func TestFreeze_StrictMode_BatchesAllUnresolved(t *testing.T) {
	osv := &TOsv{strictMode: true}
	osv.models.Store("test.a", &TModelObject{name: "test.a"})
	osv.markPending(fieldRef{fromModel: "test.a", fieldName: "X1", toModel: "missing.one", fieldType: TYPE_M2O})
	osv.markPending(fieldRef{fromModel: "test.a", fieldName: "X2", toModel: "missing.two", fieldType: TYPE_M2O})

	err := osv.Freeze(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing.one") || !strings.Contains(err.Error(), "missing.two") {
		t.Fatalf("error must list all unresolved refs, got: %v", err)
	}
}

// Phase 4 — One2One 指向远程 model 必须被拒绝。
func TestFreeze_One2OneRemote_Rejected(t *testing.T) {
	resolver := &mockRemoteResolver{
		schemas: map[string]*ModelSchema{
			"res.partner": {Name: "res.partner", IdField: "id"},
		},
	}
	osv := &TOsv{resolver: resolver, strictMode: true}
	osv.models.Store("test.invoice", &TModelObject{name: "test.invoice"})
	osv.markPending(fieldRef{
		fromModel: "test.invoice", fieldName: "PartnerId", toModel: "res.partner", fieldType: TYPE_O2O,
	})

	err := osv.Freeze(context.Background())
	if err == nil {
		t.Fatal("One2One→remote must be rejected")
	}
	if !strings.Contains(err.Error(), "One2One") || !strings.Contains(err.Error(), "res.partner") {
		t.Fatalf("error must mention One2One restriction, got: %v", err)
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
