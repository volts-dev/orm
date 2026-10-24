package orm

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/volts-dev/utils"
)

// 字段落型清单（docs/mysql/varchar_classification）把一批 recname 字段改成 TEXT：写法是
// `recname() text()`——标签里最后一个字段类型词定类型，recname 退成普通标签照样设记录名。
// 反过来写（text() recname()）会落成 VARCHAR，这条用例把写法钉住。
type TextRecName struct {
	TModel `table:"name('text_recname')"`
	Id     int64  `field:"pk autoincr"`
	Desc   string `field:"recname() text() required"`
}

func TestRecNameText_IsTextAndStillRecordName(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "rt.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(TextRecName)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	m, err := o.GetModel("text.recname")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.GetBase().GetRecordName(); got != "desc" {
		t.Errorf("记录名 = %q，想要 desc", got)
	}
	if f := m.GetFieldByName("desc"); f == nil || f.SQLType().Name != Text {
		t.Errorf("desc 的类型 = %v，想要 TEXT", f.SQLType())
	}
}

func TestDefaultValueSql_EscapesAndWrapsMySQLLob(t *testing.T) {
	text := &TTextField{}
	text.SqlType = SQLType{Text, 0, 0}
	vc := &TField{}
	vc.SqlType = SQLType{Varchar, 255, 0}
	my := &mysql{}
	cases := []struct {
		d     IDialect
		f     IField
		s     string
		quote bool
		want  string
	}{
		{&postgres{}, text, "it's", true, `'it''s'`},
		{&postgres{}, vc, "0", false, `0`},
		{my, vc, `a\b'c`, true, `'a\\b''c'`},
		{my, text, "[]", true, `('[]')`},
		{my, text, "", true, `('')`},
	}
	for _, c := range cases {
		if got := c.d.DefaultValueSql(c.f, c.s, c.quote); got != c.want {
			t.Errorf("%s DefaultValueSql(%q) = %s，想要 %s", c.d.String(), c.s, got, c.want)
		}
	}
	for in, want := range map[string]string{
		`_utf8mb4\'[]\'`:     "[]",
		`_utf8mb4\'it\'\'s\'`: "it's",
		`_utf8mb4\'\'`:       "",
		`now()`:              "now()",
	} {
		if got := mysqlGeneratedDefault(in); got != want {
			t.Errorf("mysqlGeneratedDefault(%s) = %q，想要 %q", in, got, want)
		}
	}
}

// MySQL 反查：TEXT 列的表达式默认值（COLUMN_DEFAULT 是 _utf8mb4\'[]\'，EXTRA 带 DEFAULT_GENERATED）
// 要还原成与 VARCHAR 列同形的字面量。反查出来的列会并进模型，不还原的话库里有、结构体里没有的列
// 新建时就落那串原文。
func TestMySQLGetFields_TextExpressionDefault(t *testing.T) {
	skipUnlessDialect(t, "mysql")
	o, err := New(WithDataSource(testDataSource(t, "")))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(ResyncTag), new(ResyncPartner)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	m, err := o.GetModel("resync.partner")
	if err != nil {
		t.Fatal(err)
	}
	s := o.NewSession()
	defer s.Close()
	_, cols, err := o.dialect.GetFields(s.context, s, m)
	if err != nil {
		t.Fatalf("GetFields: %v", err)
	}
	// 反查出来的文本默认值是 SQL 字面量（带引号）——与 VARCHAR 列同形。
	f, st := cols["dom"], cols["state"]
	if f == nil || st == nil {
		t.Fatal("反查结果里没有 dom / state 列")
	}
	if got := utils.ToString(st.Base().Default()); got != "'draft'" {
		t.Fatalf("VARCHAR 列 state 的反查默认值 = %q，想要 'draft'（对照组）", got)
	}
	if got := utils.ToString(f.Base().Default()); got != "'[]'" {
		t.Errorf("TEXT 列 dom 的反查默认值 = %q，想要 '[]'（与 VARCHAR 列同形）", got)
	}
}

// unbounded：不限长的 char。字段类型不变（前端照旧是单行输入框——改成 text() 会变成多行框），
// 只有 MySQL 的存储换成 TEXT；PG / SQLite 本来就不限长。
type UnboundedRec struct {
	TModel `table:"name('unbounded_rec')"`
	Id     int64  `field:"pk autoincr"`
	Label  string `field:"recname() unbounded required"`
	Url    string `field:"varchar() unbounded"`
	Short  string `field:"varchar()"`
}

func TestUnbounded_KeepsCharTypeAndStoresLongValues(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "ub.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	rec := &ddlRecorder{}
	o.AddHook(rec)
	if _, err := o.SyncModel("", new(UnboundedRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	m, err := o.GetModel("unbounded.rec")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.GetBase().GetRecordName(); got != "label" {
		t.Errorf("记录名 = %q，想要 label", got)
	}
	for _, name := range []string{"label", "url"} {
		if f := m.GetFieldByName(name); f.TypeName() != Varchar || f.SQLType().Name != Varchar {
			t.Errorf("%s: TypeName=%s SQLType=%s，都应仍是 VARCHAR（前端据此给单行输入框）", name, f.TypeName(), f.SQLType().Name)
		}
	}
	if testDBKind() == "mysql" {
		ddl := strings.Join(rec.ddl, "\n")
		for _, want := range []string{"`label` TEXT", "`url` TEXT", "`short` VARCHAR(255)"} {
			if !strings.Contains(ddl, want) {
				t.Errorf("MySQL 建表语句里没有 %s：\n%s", want, ddl)
			}
		}
	}
	long := strings.Repeat("u", 300)
	ids, err := o.Model("unbounded.rec").Create(map[string]any{"label": long, "url": long})
	if err != nil {
		t.Fatalf("300 字符写进 unbounded 字段: %v", err)
	}
	ds, _ := o.Model("unbounded.rec").Ids(ids...).Read()
	if got := ds.FieldByName("url").AsString(); got != long {
		t.Errorf("读回 %d 字符，想要 300", len(got))
	}
}

func TestUnbounded_RejectsNonCharField(t *testing.T) {
	type BadUnbounded struct {
		TModel `table:"name('bad_unbounded')"`
		Id     int64 `field:"pk autoincr"`
		Qty    int64 `field:"int() unbounded"`
	}
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "bu.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(BadUnbounded)); err == nil || !strings.Contains(err.Error(), "unbounded") {
		t.Fatalf("int 字段写 unbounded 应报错，得 %v", err)
	}
}

// builder 写法（b.VarcharField(x).Unbounded()）与标签同义。
type UnboundedBuilt struct {
	TModel `table:"name('unbounded_built')"`
	Id     int64 `field:"pk autoincr"`
}

func (self UnboundedBuilt) OnBuildFields() error {
	if err := self.TModel.OnBuildFields(); err != nil {
		return err
	}
	self.Builder().VarcharField("hook_url").Unbounded()
	return nil
}

func TestUnbounded_Builder(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "ubb.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(UnboundedBuilt)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	m, _ := o.GetModel("unbounded.built")
	if f := m.GetFieldByName("hook_url"); f == nil || !f.Base().unbounded || f.TypeName() != Varchar {
		t.Fatalf("builder 的 Unbounded() 没生效：%v", f)
	}
	long := strings.Repeat("h", 300)
	if _, err := o.Model("unbounded.built").Create(map[string]any{"hook_url": long}); err != nil {
		t.Fatalf("300 字符: %v", err)
	}
}
