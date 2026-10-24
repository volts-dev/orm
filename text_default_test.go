package orm

import (
	"path/filepath"
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
