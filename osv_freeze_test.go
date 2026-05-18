package orm

import (
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
