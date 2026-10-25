package orm

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// 一次性回填在三个库上同一个结局（P5，2026-10-25，见 field_backfill_marker.go）：第一次回填失败，下一次（重启）
// 补字段时补做；做成之后再重启不再做。PG / SQLite 靠建列与回填同一个事务（失败时列也不在）；MySQL 的 DDL 隐式
// 提交，失败时列已在——此前下一次看到「列已存在」就不再回填，侧表里的旧值悄悄丢了。

type bf2Owner struct {
	TModel `table:"name('bf2_owner')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

type bf2Target struct {
	TModel `table:"name('bf2_target')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

func bf2Orm(t *testing.T, ds *TDataSource) (*TOrm, string, string) {
	t.Helper()
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	names, err := o.SyncModel("test", new(bf2Owner), new(bf2Target))
	if err != nil || len(names) != 2 {
		t.Fatalf("SyncModel: %v %v", names, err)
	}
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatal(err)
	}
	return o, names[0], names[1]
}

func TestAddField_FailedBackfillIsRetriedOnce(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "bf2.db"))
	o, owner, target := bf2Orm(t, ds)
	for _, q := range []string{
		`CREATE TABLE bf2_side (owner_id bigint, note varchar(64))`,
		`CREATE TABLE bf2_side_tags (owner_id bigint, target_id bigint)`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	a, err := o.Model(owner).Create(map[string]any{"name": "a"})
	if err != nil {
		t.Fatal(err)
	}
	t1, _ := o.Model(target).Create(map[string]any{"name": "t1"})
	t2, _ := o.Model(target).Create(map[string]any{"name": "t2"})
	if _, err := o.Exec(`INSERT INTO bf2_side (owner_id, note) VALUES (?, 'from side')`, a[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Exec(`INSERT INTO bf2_side_tags (owner_id, target_id) VALUES (?, ?), (?, ?)`, a[0], t1[0], a[0], t2[0]); err != nil {
		t.Fatal(err)
	}

	const noteTag = "varchar(64) title('Note')"
	m2mTag := "many2many(" + target + ",bf2_owner_target_rel,owner_id,target_id) title('Targets')"
	boom := errors.New("boom")
	calls, m2mCalls := 0, 0
	noteFill := func(fail bool) AddFieldOption {
		return WithBackfill(func(s *TSession) error {
			calls++
			if fail {
				return boom
			}
			_, err := s.Exec(`UPDATE bf2_owner SET note = (SELECT x.note FROM bf2_side x WHERE x.owner_id = bf2_owner.id)
				WHERE EXISTS (SELECT 1 FROM bf2_side x WHERE x.owner_id = bf2_owner.id)`)
			return err
		})
	}
	tagFill := func(fail bool) AddFieldOption {
		return WithBackfill(func(s *TSession) error {
			m2mCalls++
			if fail {
				return boom
			}
			_, err := s.Exec(`INSERT INTO bf2_owner_target_rel (owner_id, target_id) SELECT owner_id, target_id FROM bf2_side_tags`)
			return err
		})
	}

	// 第一次：两个回填都失败，错误交回来。
	if _, err := o.AddField(owner, "note", noteTag, noteFill(true)); !errors.Is(err, boom) {
		t.Fatalf("列的回填失败应把错误交回来，实得 %v", err)
	}
	if _, err := o.AddField(owner, "target_ids", m2mTag, tagFill(true)); !errors.Is(err, boom) {
		t.Fatalf("关联表的回填失败应把错误交回来，实得 %v", err)
	}

	// 重启再补：这一次回填成功。
	o2, _, _ := bf2Orm(t, ds)
	if _, err := o2.AddField(owner, "note", noteTag, noteFill(false)); err != nil {
		t.Fatalf("重启后补列: %v", err)
	}
	if _, err := o2.AddField(owner, "target_ids", m2mTag, tagFill(false)); err != nil {
		t.Fatalf("重启后补关联表: %v", err)
	}
	if calls != 2 || m2mCalls != 2 {
		t.Fatalf("失败一次、重试一次：列回填 %d 次、关联表回填 %d 次，应各 2 次", calls, m2mCalls)
	}
	read := func(o *TOrm) string {
		t.Helper()
		ds, err := o.Model(owner).Ids(a[0]).Read()
		if err != nil || ds.Count() != 1 {
			t.Fatalf("read: %v", err)
		}
		return ds.FieldByName("note").AsString()
	}
	if got := read(o2); got != "from side" {
		t.Fatalf("重试的回填没把侧表的值拷过来：%q", got)
	}
	cnt, err := o2.Query(`SELECT count(*) AS n FROM bf2_owner_target_rel WHERE owner_id = ?`, a[0])
	if err != nil {
		t.Fatal(err)
	}
	if n := cnt.FieldByName("n").AsInteger(); n != 2 {
		t.Fatalf("关联表应回填 2 行，实得 %d", n)
	}

	// 用户改了值；再重启补一次：不再回填（否则侧表的旧值盖掉新值）。
	if _, err := o2.Model(owner).Ids(a[0]).Write(map[string]any{"note": "edited"}); err != nil {
		t.Fatal(err)
	}
	o3, _, _ := bf2Orm(t, ds)
	if _, err := o3.AddField(owner, "note", noteTag, noteFill(false)); err != nil {
		t.Fatal(err)
	}
	if _, err := o3.AddField(owner, "target_ids", m2mTag, tagFill(false)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || m2mCalls != 2 {
		t.Fatalf("做成之后再重启不该再回填：列 %d 次、关联表 %d 次", calls, m2mCalls)
	}
	if got := read(o3); got != "edited" {
		t.Fatalf("重启后的回填覆盖了用户改过的值：%q", got)
	}
}
