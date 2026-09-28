package orm

import (
	"context"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

// 带按公司区分字段的模型上做 upsert（数据文件导入：OnConflict{UpdateAll}）。
type cdUpsertProbe struct {
	TModel `table:"name('cd_upsert_probe')"`
	Id     int64  `field:"pk autoincr"`
	Code   string `field:"varchar(16) unique"`
	Name   string `field:"varchar(32)"`
	Days   int64  `field:"int(64) company_dependent default(1)"`
	Flag   bool   `field:"bool() company_dependent"`
}

// 2026-09-29 真栈：purchase 给 res.partner 加了按公司区分的收货提醒 / 提醒天数之后，新租户一个
// 都建不出来 —— registry 的种子数据对 partner_root 走 upsert，UpdateAll 把**没给的列**（默认值、
// 布尔零值补位）也算进了 DO UPDATE，撞上「upsert 不能整列覆盖按公司区分的列」那道闸，整笔拒。
//
// 没显式给的按公司区分列不参与冲突更新（它们本来就不该被一次导入改掉，更不能整列覆盖掉各公司的
// 值）；显式给了的照旧报错。
func TestCompanyDependent_Postgres_UpsertSkipsUnsetCompanyColumns(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm_addfield", SSLMode: "disable"}
	cleaner, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	_, err = cleaner.Exec(`DROP TABLE IF EXISTS public.cd_upsert_probe CASCADE`)
	cleaner.Close()
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer o.Close()
	names, err := o.SyncModel("test", new(cdUpsertProbe))
	if err != nil || len(names) != 1 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	m := names[0]
	in5 := func() *TSession { return o.Model(m).WithContext(WithCompanyId(context.Background(), 5)) }

	if _, err := in5().Create(map[string]any{"code": "x", "name": "a", "days": 7, "flag": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := in5().OnConflict(&OnConflict{UpdateAll: true}).Create(map[string]any{"code": "x", "name": "b"}); err != nil {
		t.Fatalf("没给按公司区分的列的 upsert 应成功: %v", err)
	}
	ds2, err := in5().Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	if ds2.Count() != 1 {
		t.Fatalf("upsert 应更新原行而不是新建，实得 %d 行", ds2.Count())
	}
	r := ds2.Record()
	if r.FieldByName("name").AsString() != "b" || r.FieldByName("days").AsInteger() != 7 || !r.FieldByName("flag").AsBoolean() {
		t.Fatalf("普通列应更新、公司 5 那一格不动：name=%s days=%d flag=%v",
			r.FieldByName("name").AsString(), r.FieldByName("days").AsInteger(), r.FieldByName("flag").AsBoolean())
	}

	_, err = in5().OnConflict(&OnConflict{UpdateAll: true}).Create(map[string]any{"code": "x", "name": "c", "days": 9})
	if err == nil || !strings.Contains(err.Error(), "company_dependent") {
		t.Fatalf("显式给了按公司区分的列仍应报错（整列覆盖会抹掉别的公司），实得 %v", err)
	}
}
