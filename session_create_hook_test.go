package orm

import (
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

// CreateHookModel 扮演 vectors 的业务模型：age 是「缺省 777、显式值只认 1..100」的列
// （对应 company_id：缺省当前公司、显式值只认会话的公司工作集）。
type CreateHookModel struct {
	TModel `table:"name('create_hook_model')"`
	Id     int64  `field:"pk autoincr title('ID')"`
	Name   string `field:"varchar() size(64)"`
	Age    int    `field:"int()"`
}

func (m *CreateHookModel) BeforeCreateValues(_ *TSession, rec *dataset.TRecordSet) ([]string, error) {
	if v := utils.ToInt64(rec.GetByField("age")); v >= 1 && v <= 100 {
		return nil, nil
	}
	rec.SetByField("age", 777)
	return []string{"age"}, nil
}

func setupCreateHookOrm(t *testing.T) *TOrm {
	t.Helper()
	o, err := New(WithDataSource(&TDataSource{DbType: "sqlite", DbName: ":memory:"}))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(CreateHookModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func ageOf(t *testing.T, o *TOrm, name string) int64 {
	t.Helper()
	ds, err := o.Model("create.hook.model").Where("name=?", name).Limit(1).Read()
	if err != nil {
		t.Fatalf("read %q: %v", name, err)
	}
	if ds.Count() != 1 {
		t.Fatalf("read %q: %d rows", name, ds.Count())
	}
	return ds.FieldByName("age").AsInteger()
}

// TestCreate_ValuesHook_KeepsAllowedExplicitValue 是这个钩子存在的理由：
// 显式给了、且在允许范围内的值必须原样落库，不能像 Set 那样被一律覆盖。
func TestCreate_ValuesHook_KeepsAllowedExplicitValue(t *testing.T) {
	o := setupCreateHookOrm(t)
	if _, err := o.Model("create.hook.model").Create(map[string]any{"name": "explicit", "age": 5}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := ageOf(t, o, "explicit"); got != 5 {
		t.Fatalf("explicit allowed value: age=%d, want 5 (hook must not overwrite it)", got)
	}
}

func TestCreate_ValuesHook_FillsMissingAndReplacesDisallowed(t *testing.T) {
	o := setupCreateHookOrm(t)
	if _, err := o.Model("create.hook.model").Create(
		map[string]any{"name": "missing"},
		map[string]any{"name": "outside", "age": 500},
		map[string]any{"name": "inside", "age": 42},
	); err != nil {
		t.Fatalf("create: %v", err)
	}
	for name, want := range map[string]int64{"missing": 777, "outside": 777, "inside": 42} {
		if got := ageOf(t, o, name); got != want {
			t.Errorf("%s: age=%d, want %d (the hook runs once per row, on that row's own values)", name, got, want)
		}
	}
}

// 钩子排在 Sets 之后：看到的是 Set 过的值（vectors 的 tenant_id / create_id 仍走 Set）。
func TestCreate_ValuesHook_RunsAfterSets(t *testing.T) {
	o := setupCreateHookOrm(t)
	if _, err := o.Model("create.hook.model").Set("age", 7).Create(map[string]any{"name": "set-in", "age": 500}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := ageOf(t, o, "set-in"); got != 7 {
		t.Fatalf("Set value inside the range: age=%d, want 7", got)
	}
	if _, err := o.Model("create.hook.model").Set("age", 900).Create(map[string]any{"name": "set-out"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := ageOf(t, o, "set-out"); got != 777 {
		t.Fatalf("Set value outside the range: age=%d, want 777", got)
	}
}

// 只管建，不管改：更新里"没给"就是"不动"，给了什么就写什么。
func TestCreate_ValuesHook_NotCalledOnWrite(t *testing.T) {
	o := setupCreateHookOrm(t)
	id, err := o.Model("create.hook.model").Create(map[string]any{"name": "w", "age": 5})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := o.Model("create.hook.model").Ids(id...).Write(map[string]any{"age": 500}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := ageOf(t, o, "w"); got != 500 {
		t.Fatalf("write: age=%d, want 500 (hook must not run on Write)", got)
	}
}
