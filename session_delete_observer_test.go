package orm

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
)

type (
	DobParent struct {
		TModel `table:"name('dob_parent')"`
		Id     int64  `field:"pk autoincr title('ID')"`
		Name   string `field:"varchar() size(64)"`
	}
	DobChild struct {
		TModel   `table:"name('dob_child')"`
		Id       int64  `field:"pk autoincr title('ID')"`
		Name     string `field:"varchar() size(64)"`
		ParentId int64  `field:"many2one(dob_parent) required ondelete('cascade')"`
	}
)

func setupDeleteObserverOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := testDataSource(t, filepath.Join(t.TempDir(), "dob.db"))
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(DobParent), new(DobChild)); err != nil {
		t.Fatal(err)
	}
	return o
}

// 观察者要听到主记录**和**被级联删掉的子行：级联绕过子模型的 Delete 覆写直接发 SQL，
// 挂在子行上（按 res_model/res_id 指过去）的东西只能靠这一次通知清掉。
func TestDeleteObserver_ReportsMainAndCascadedRows(t *testing.T) {
	o := setupDeleteObserverOrm(t)
	p1 := firstId(t, mustCreate(t, o, "dob.parent", map[string]any{"name": "p1"}))
	p2 := firstId(t, mustCreate(t, o, "dob.parent", map[string]any{"name": "p2"}))
	c1 := firstId(t, mustCreate(t, o, "dob.child", map[string]any{"name": "c1", "parent_id": p1}))
	c2 := firstId(t, mustCreate(t, o, "dob.child", map[string]any{"name": "c2", "parent_id": p1}))
	mustCreate(t, o, "dob.child", map[string]any{"name": "c3", "parent_id": p2})

	got := map[string][]string{}
	tables := map[string]string{}
	o.SetDeleteObserver(func(s *TSession, model, table string, ids []any) error {
		if s == nil {
			return errors.New("nil session")
		}
		for _, id := range ids {
			got[model] = append(got[model], fmt.Sprint(id))
		}
		tables[model] = table
		return nil
	})
	defer o.SetDeleteObserver(nil)

	if _, err := o.Model("dob.parent").Delete(p1); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, v := range got {
		sort.Strings(v)
	}
	if want := []string{fmt.Sprint(p1)}; fmt.Sprint(got["dob.parent"]) != fmt.Sprint(want) {
		t.Fatalf("主记录：要 %v，实得 %v", want, got["dob.parent"])
	}
	wantKids := []string{fmt.Sprint(c1), fmt.Sprint(c2)}
	sort.Strings(wantKids)
	if fmt.Sprint(got["dob.child"]) != fmt.Sprint(wantKids) {
		t.Fatalf("级联子行：要 %v，实得 %v（c3 属于 p2，不该在里面）", wantKids, got["dob.child"])
	}
	if tables["dob.parent"] != "dob_parent" || tables["dob.child"] != "dob_child" {
		t.Fatalf("表名不对：%v", tables)
	}
}

// 观察者失败＝这次删除失败（调用方据此回滚）。
func TestDeleteObserver_ErrorFailsTheDelete(t *testing.T) {
	o := setupDeleteObserverOrm(t)
	p := firstId(t, mustCreate(t, o, "dob.parent", map[string]any{"name": "p"}))
	boom := errors.New("boom")
	o.SetDeleteObserver(func(*TSession, string, string, []any) error { return boom })
	defer o.SetDeleteObserver(nil)

	if _, err := o.Model("dob.parent").Delete(p); !errors.Is(err, boom) {
		t.Fatalf("观察者的错误要原样传出来，实得 %v", err)
	}
}

// 没装观察者时行为不变（也不报错）。
func TestDeleteObserver_NoneInstalled(t *testing.T) {
	o := setupDeleteObserverOrm(t)
	p := firstId(t, mustCreate(t, o, "dob.parent", map[string]any{"name": "p"}))
	if n, err := o.Model("dob.parent").Delete(p); err != nil || n != 1 {
		t.Fatalf("delete: n=%d err=%v", n, err)
	}
}
