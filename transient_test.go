package orm

import (
	"context"
	stdErrors "errors"
	"path/filepath"
	"testing"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
)

/*
临时模型：声明（标签 / builder）、清理原语、错误边界。
*/

type trWizard struct {
	TModel     `table:"name('tr_wizard') transient(2)"`
	Id         int64     `field:"pk autoincr"`
	Name       string    `field:"varchar(64)"`
	CreateTime time.Time `field:"datetime() created"`
}

type trNoCreated struct {
	TModel `table:"name('tr_nocreated') transient"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
}

type trPlain struct {
	TModel     `table:"name('tr_plain')"`
	Id         int64     `field:"pk autoincr"`
	Name       string    `field:"varchar(64)"`
	CreateTime time.Time `field:"datetime() created"`
}

type trByBuilder struct {
	TModel     `table:"name('tr_by_builder')"`
	Id         int64     `field:"pk autoincr"`
	Name       string    `field:"varchar(64)"`
	CreateTime time.Time `field:"datetime() created"`
}

func (self *trByBuilder) OnBuildFields() error {
	return self.Builder().TableTransient(0.5).Err()
}

func setupTransient(t *testing.T) *TOrm {
	t.Helper()
	o, err := New(WithDataSource(&TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "tr.db")}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(trWizard), new(trNoCreated), new(trPlain), new(trByBuilder)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

func TestTransient_Declaration(t *testing.T) {
	o := setupTransient(t)
	cases := []struct {
		model     string
		transient bool
		hours     float64
	}{
		{"tr.wizard", true, 2},
		{"tr.nocreated", true, DefaultTransientMaxHours},
		{"tr.plain", false, DefaultTransientMaxHours},
		{"tr.by.builder", true, 0.5},
	}
	for _, c := range cases {
		m, err := o.GetModel(c.model)
		if err != nil {
			t.Fatalf("%s: %v", c.model, err)
		}
		if m.IsTransient() != c.transient || m.TransientMaxHours() != c.hours {
			t.Errorf("%s: transient=%v hours=%v，想要 %v / %v", c.model, m.IsTransient(), m.TransientMaxHours(), c.transient, c.hours)
		}
	}
}

func TestTransient_TagRejectsBadHours(t *testing.T) {
	m := &TModel{name: "x"}
	for _, bad := range []string{"0", "-1", "abc"} {
		if err := tag_table_transient(&TTagContext{Model: m, Params: []string{bad}}); err == nil {
			t.Errorf("transient(%s) 应报错而不是静默落回默认值", bad)
		}
	}
	if err := tag_table_transient(&TTagContext{Model: m, Params: []string{"'1.5'"}}); err != nil || m.transientMaxHours != 1.5 {
		t.Errorf("transient('1.5') 应解析为 1.5：%v / %v", err, m.transientMaxHours)
	}
}

func TestTransient_VacuumDeletesOnlyExpired(t *testing.T) {
	o := setupTransient(t)
	for _, n := range []string{"a", "b", "c"} {
		if _, err := o.Model("tr.wizard").Create(map[string]any{"name": n}); err != nil {
			t.Fatal(err)
		}
	}

	// 以"现在"为基准：三行都是刚建的，保留 2 小时，一行都不该删
	n, err := o.Model("tr.wizard").VacuumTransient(time.Now())
	if err != nil {
		t.Fatalf("vacuum(now): %v", err)
	}
	if n != 0 {
		t.Fatalf("刚建的行不该被清，删了 %d", n)
	}
	if cnt, _ := o.Model("tr.wizard").Count(); cnt != 3 {
		t.Fatalf("应仍有 3 行，得到 %d", cnt)
	}

	// 把基准时刻拨到 3 小时后：cutoff = now+1h，三行全部过期
	n, err = o.Model("tr.wizard").VacuumTransient(time.Now().Add(3 * time.Hour))
	if err != nil {
		t.Fatalf("vacuum(now+3h): %v", err)
	}
	if n != 3 {
		t.Fatalf("三行都应过期被清，删了 %d", n)
	}
	if cnt, _ := o.Model("tr.wizard").Count(); cnt != 0 {
		t.Fatalf("应清空，剩 %d", cnt)
	}
}

func TestTransient_VacuumErrors(t *testing.T) {
	o := setupTransient(t)
	if _, err := o.Model("tr.plain").VacuumTransient(); err == nil || !stdErrors.Is(err, ormerr.ErrNotTransient) {
		t.Errorf("非临时模型应报 ErrNotTransient，得到 %v", err)
	}
	if _, err := o.Model("tr.nocreated").VacuumTransient(); err == nil || !stdErrors.Is(err, ormerr.ErrNoCreatedField) {
		t.Errorf("没有 created 字段应报 ErrNoCreatedField（绝不能清空整表），得到 %v", err)
	}
	// 反证：没 created 字段的表一行没少
	if _, err := o.Model("tr.nocreated").Create(map[string]any{"name": "keep"}); err != nil {
		t.Fatal(err)
	}
	_, _ = o.Model("tr.nocreated").VacuumTransient(time.Now().Add(100 * time.Hour))
	if cnt, _ := o.Model("tr.nocreated").Count(); cnt != 1 {
		t.Fatalf("没有 created 字段的表不该被动到，剩 %d", cnt)
	}
}

func TestTransient_VacuumAll(t *testing.T) {
	o := setupTransient(t)
	if _, err := o.Model("tr.wizard").Create(map[string]any{"name": "w"}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Model("tr.plain").Create(map[string]any{"name": "p"}); err != nil {
		t.Fatal(err)
	}
	result, err := o.VacuumTransients(context.Background())
	// tr.nocreated 是临时模型却没有 created 字段 → 汇总错误里应有 ErrNoCreatedField，其余模型照常处理
	if err == nil || !stdErrors.Is(err, ormerr.ErrNoCreatedField) {
		t.Fatalf("汇总错误应含 ErrNoCreatedField，得到 %v", err)
	}
	if _, ok := result["tr.wizard"]; !ok {
		t.Errorf("tr.wizard 应被处理：%v", result)
	}
	if _, ok := result["tr.by.builder"]; !ok {
		t.Errorf("tr.by.builder 应被处理：%v", result)
	}
	if _, ok := result["tr.plain"]; ok {
		t.Errorf("非临时模型不该出现在结果里：%v", result)
	}
	if cnt, _ := o.Model("tr.plain").Count(); cnt != 1 {
		t.Fatalf("非临时模型的行不该被动：%d", cnt)
	}
}
