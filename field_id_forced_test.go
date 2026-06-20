package orm

import "testing"

// forcedIdModel 使用 id() 主键（TIdField），用于验证 Create 在调用方显式提供主键时
// 保留该 id 而非用雪花覆盖——这是 vectors SSN 契约 res_user.id == sys_user.id 的前提。
type forcedIdModel struct {
	TModel `table:"name('forced_id_model')"`
	Id     int64  `field:"id() pk"`
	Name   string `field:"varchar() size(64)"`
}

func setupForcedIdOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(forcedIdModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

// 显式提供主键时 Create 必须保留该 id（不被雪花覆盖），并能按该 id 读回。
func TestCreate_HonorsCallerSuppliedId(t *testing.T) {
	o := setupForcedIdOrm(t)

	const wantId int64 = 7700123456789

	ids, err := o.Model("forced_id_model").Create(map[string]any{"id": wantId, "name": "ssn-user"})
	if err != nil {
		t.Fatalf("Create with explicit id: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("expected returned id")
	}
	if got := toInt64(ids[0]); got != wantId {
		t.Fatalf("returned id = %d, want %d (caller-supplied id was overwritten)", got, wantId)
	}

	// 按强制 id 读回，确认行确实以该主键落库。
	ds, err := o.Model("forced_id_model").Where("id=?", wantId).Read()
	if err != nil {
		t.Fatalf("Read by forced id: %v", err)
	}
	if ds.Count() != 1 {
		t.Fatalf("expected 1 row at id=%d, got %d", wantId, ds.Count())
	}
	if name := ds.Record().FieldByName("name").AsString(); name != "ssn-user" {
		t.Fatalf("name = %q, want ssn-user", name)
	}
}

// 不提供主键时仍按原行为生成一个非零雪花 id（且与显式路径互不影响）。
func TestCreate_GeneratesIdWhenNotSupplied(t *testing.T) {
	o := setupForcedIdOrm(t)

	ids, err := o.Model("forced_id_model").Create(map[string]any{"name": "auto"})
	if err != nil {
		t.Fatalf("Create without id: %v", err)
	}
	if len(ids) == 0 || toInt64(ids[0]) == 0 {
		t.Fatalf("expected generated non-zero id, got %v", ids)
	}
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}
