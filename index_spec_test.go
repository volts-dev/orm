package orm

import (
	stdErrors "errors"
	"strings"
	"testing"

	"github.com/volts-dev/orm/core"
	ormerr "github.com/volts-dev/orm/errors"
)

/*
部分索引 / 表达式索引：纯函数层的合同。

  - 定义（列、表达式、谓词）哈希进名字：定义一变名字就变，SyncModel 按名字对账时旧索引
    自然过期；同名即同定义，绕开 PG 对表达式/谓词文本的规整（`lower((name)::text)`）。
  - 片段只做最基本的拒绝：`;`、注释、不配对的括号/引号。
  - parseIndexDef 按括号深度解析 CREATE INDEX 语句，PG 的 indexdef 与 SQLite 的
    sqlite_master.sql 共用。
*/

func TestIndexSpec_NamingIsStableAndDefinitionSensitive(t *testing.T) {
	a, err := newIndexSpec("pi_job", IndexSpec{Unique: true, Cols: []string{"tenant_id", "job_key"}, Where: "state = 'pending'"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Cols: []string{"tenant_id", "job_key"}, Where: "state  =  'pending'"})
	if a.Name != b.Name {
		t.Fatalf("空白差异不该改名字：%s vs %s", a.Name, b.Name)
	}
	c, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Cols: []string{"tenant_id", "job_key"}, Where: "state = 'queued'"})
	if a.Name == c.Name {
		t.Fatalf("谓词变了名字必须变：%s", a.Name)
	}
	if !strings.HasPrefix(a.Name, DefaultUniquePrefix) || !hasDefinitionHash(a.Name) {
		t.Fatalf("唯一部分索引应为 UQE_ 前缀 + _p<hash> 后缀，得到 %s", a.Name)
	}
	if !a.IsPartial() || a.HasExprs() || a.Type != UniqueType {
		t.Fatalf("形状不对：%+v", a)
	}

	e, _ := newIndexSpec("pi_job", IndexSpec{Exprs: []string{"lower(name)"}})
	if !strings.HasPrefix(e.Name, DefaultIndexPrefix) || !hasDefinitionHash(e.Name) || !e.HasExprs() {
		t.Fatalf("表达式索引形状不对：%+v", e)
	}

	// 自定义名也追加哈希：否则改了谓词名字不变，库里的旧定义永远不会被重建。
	n, _ := newIndexSpec("pi_job", IndexSpec{Name: "my_idx", Cols: []string{"name"}, Where: "state = 'x'"})
	if !hasDefinitionHash(n.Name) || !strings.Contains(n.Name, "my_idx") {
		t.Fatalf("自定义名应保留并带哈希：%s", n.Name)
	}

	// 普通索引不带哈希，与老命名完全一致。
	p, _ := newIndexSpec("pi_job", IndexSpec{Cols: []string{"name"}})
	if p.Name != generate_index_name(IndexType, "pi_job", []string{"name"}) {
		t.Fatalf("无表达式无谓词的索引命名应与 newIndex 一致：%s", p.Name)
	}

	// 超长名字截断到 63 且保留哈希后缀。
	long, _ := newIndexSpec("a_very_long_table_name_for_testing_purposes", IndexSpec{
		Cols: []string{"column_number_one", "column_number_two", "column_number_three"}, Where: "x > 0"})
	if len(long.Name) > maxIndexNameLen || !hasDefinitionHash(long.Name) {
		t.Fatalf("超长名字应截断并保留哈希：%q (%d)", long.Name, len(long.Name))
	}
}

func TestIndexSpec_Validation(t *testing.T) {
	bad := []IndexSpec{
		{},
		{Cols: []string{""}},
		{Cols: []string{"a"}, Where: "state = 'x'; DROP TABLE t"},
		{Cols: []string{"a"}, Where: "state = 'x' -- comment"},
		{Exprs: []string{"lower(name"}},
		{Exprs: []string{"lower(name))"}},
		{Cols: []string{"a"}, Where: "name = 'O'Brien'"},
		{Exprs: []string{"   "}},
	}
	for i, spec := range bad {
		if _, err := newIndexSpec("t", spec); err == nil {
			t.Errorf("case %d 应拒绝：%+v", i, spec)
		}
	}
	// 转义的单引号（''）是配对的
	if _, err := newIndexSpec("t", IndexSpec{Cols: []string{"a"}, Where: "name = 'O''Brien'"}); err != nil {
		t.Errorf("'' 转义应通过：%v", err)
	}
}

func TestIndex_EqualDefinitionalByName(t *testing.T) {
	model, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Cols: []string{"tenant_id"}, Where: "state = 'pending'"})

	// 库里反查回来：同名、类型同，但没解析出谓词（MySQL 退化/内省不全）——仍视为同定义
	db := &TIndex{Name: model.Name, Type: UniqueType, Cols: []string{"tenant_id"}}
	if !model.Equal(db) || !db.Equal(model) {
		t.Fatalf("同名的定义性索引应判等")
	}
	// 名字不同（定义变了）→ 不等
	other, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Cols: []string{"tenant_id"}, Where: "state = 'queued'"})
	if model.Equal(other) {
		t.Fatalf("谓词不同的索引不该判等")
	}
	// 普通索引仍按列集合比
	p1 := newIndex("", "t", IndexType, "a", "b")
	p2 := newIndex("", "t", IndexType, "b", "a")
	if !p1.Equal(p2) {
		t.Fatalf("普通索引列集合相同应判等")
	}
}

func TestParseIndexDef(t *testing.T) {
	cases := []struct {
		def   string
		cols  []string
		exprs []string
		where string
	}{
		{`CREATE UNIQUE INDEX "UQE_pjob_tenant_id_key_p1a2b3c4d" ON public.pi_job USING btree (tenant_id, job_key) WHERE (state = 'pending'::text)`,
			[]string{"tenant_id", "job_key"}, nil, `(state = 'pending'::text)`},
		{`CREATE INDEX idx ON public.t USING btree (lower((name)::text))`,
			nil, []string{"lower((name)::text)"}, ""},
		{`CREATE INDEX idx ON public.t USING gin (spec)`, []string{"spec"}, nil, ""},
		{`CREATE INDEX idx ON public.t USING btree (name DESC, id)`, []string{"name", "id"}, nil, ""},
		{`CREATE UNIQUE INDEX "UQE_x" ON "pi_job" ("tenant_id","job_key") WHERE (state = 'pending')`,
			[]string{"tenant_id", "job_key"}, nil, `(state = 'pending')`},
		{`CREATE INDEX "i" ON "t" ((lower(name)), "id")`, []string{"id"}, []string{"(lower(name))"}, ""},
		{`CREATE INDEX i ON t (a) WHERE (b IN ('x, y', 'z'))`, []string{"a"}, nil, `(b IN ('x, y', 'z'))`},
	}
	for _, c := range cases {
		cols, exprs, where, ok := parseIndexDef(c.def)
		if !ok {
			t.Errorf("解析失败：%s", c.def)
			continue
		}
		if strings.Join(cols, "|") != strings.Join(c.cols, "|") {
			t.Errorf("%s\n cols 得到 %v 想要 %v", c.def, cols, c.cols)
		}
		if strings.Join(exprs, "|") != strings.Join(c.exprs, "|") {
			t.Errorf("%s\n exprs 得到 %v 想要 %v", c.def, exprs, c.exprs)
		}
		if where != c.where {
			t.Errorf("%s\n where 得到 %q 想要 %q", c.def, where, c.where)
		}
	}
	if _, _, _, ok := parseIndexDef("garbage"); ok {
		t.Errorf("垃圾输入应 ok=false")
	}
}

func TestMySQL_FunctionalIndexSupportMatrix(t *testing.T) {
	cases := []struct {
		v    *core.Version
		want bool
	}{
		{&core.Version{Number: "8.0.13"}, true},
		{&core.Version{Number: "8.0.36"}, true},
		{&core.Version{Number: "8.4.0"}, true},
		{&core.Version{Number: "9.1.0"}, true},
		{&core.Version{Number: "8.0.12"}, false},
		{&core.Version{Number: "5.7.44"}, false},
		{&core.Version{Number: "10.11.6", Edition: "MariaDB"}, false},
		{&core.Version{Number: "11.4.2", Edition: "MariaDB-ubu2404"}, false},
		{&core.Version{Number: "v7.5.0", Level: "8.0.11", Edition: "TiDB"}, false},
		{&core.Version{Number: "garbage"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := mysqlSupportsFunctionalIndex(c.v); got != c.want {
			t.Errorf("%+v: 得到 %v 想要 %v", c.v, got, c.want)
		}
	}
}

func TestPG_CreateIndexSql_PartialAndExpression(t *testing.T) {
	d := newPgDialectForTest(t)
	idx, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Cols: []string{"tenant_id"}, Exprs: []string{"lower(name)"}, Where: "state = 'pending'"})
	got := d.CreateIndexUniqueSql("system", "pi_job", idx)
	for _, want := range []string{"CREATE UNIQUE INDEX IF NOT EXISTS", `ON "system"."pi_job"`, `("tenant_id",(lower(name)))`, `WHERE (state = 'pending')`} {
		if !strings.Contains(got, want) {
			t.Errorf("PG DDL 缺 %q：%s", want, got)
		}
	}
	if err := d.ValidateIndex(idx); err != nil {
		t.Errorf("PG 原生支持，不该拒绝：%v", err)
	}
}

func TestSQLite_CreateIndexSql_PartialAndExpression(t *testing.T) {
	d := QueryDialect("sqlite")
	if err := d.Init(nil, &TDataSource{DbType: "sqlite", DbName: ":memory:"}); err != nil {
		t.Fatal(err)
	}
	idx, _ := newIndexSpec("pi_job", IndexSpec{Exprs: []string{"lower(name)"}, Where: "state = 'done'"})
	got := d.CreateIndexUniqueSql("", "pi_job", idx)
	for _, want := range []string{"CREATE INDEX IF NOT EXISTS", `((lower(name)))`, `WHERE (state = 'done')`} {
		if !strings.Contains(got, want) {
			t.Errorf("SQLite DDL 缺 %q：%s", want, got)
		}
	}
}

func newMySQLDialectForTest(t *testing.T, functional bool) *mysql {
	t.Helper()
	d := QueryDialect("mysql")
	if d == nil {
		t.Fatal("mysql dialect not registered")
	}
	if err := d.Init(nil, &TDataSource{DbType: "mysql", DbName: "testdb"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	m, ok := d.(*mysql)
	if !ok {
		t.Fatalf("unexpected dialect type %T", d)
	}
	m.funcIdxForce = &functional
	return m
}

func TestMySQL_CreateIndexSql_Emulation(t *testing.T) {
	d := newMySQLDialectForTest(t, true)

	// 唯一部分索引 → 每个键包 CASE WHEN，谓词不出现在 WHERE
	pu, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Cols: []string{"tenant_id", "job_key"}, Where: "state = 'pending'"})
	got := d.CreateIndexUniqueSql("", "pi_job", pu)
	if !strings.Contains(got, "CREATE UNIQUE INDEX") ||
		!strings.Contains(got, "(CASE WHEN (state = 'pending') THEN `tenant_id` ELSE NULL END)") ||
		!strings.Contains(got, "(CASE WHEN (state = 'pending') THEN `job_key` ELSE NULL END)") ||
		strings.Contains(got, " WHERE ") {
		t.Errorf("MySQL 唯一部分索引应用 CASE WHEN 模拟：%s", got)
	}
	if err := d.ValidateIndex(pu); err != nil {
		t.Errorf("8.0.13+ 应放行：%v", err)
	}

	// 非唯一部分索引 → 去谓词的全表索引
	pi, _ := newIndexSpec("pi_job", IndexSpec{Cols: []string{"name"}, Where: "state = 'done'"})
	got = d.CreateIndexUniqueSql("", "pi_job", pi)
	if strings.Contains(got, "CASE") || strings.Contains(got, "WHERE") || !strings.Contains(got, "(`name`)") {
		t.Errorf("MySQL 非唯一部分索引应退化为全表索引：%s", got)
	}

	// 表达式 → functional key part
	ex, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Exprs: []string{"lower(name)"}})
	got = d.CreateIndexUniqueSql("", "pi_job", ex)
	if !strings.Contains(got, "((lower(name)))") {
		t.Errorf("MySQL 表达式索引应为 functional key part：%s", got)
	}

	// 表达式 + 唯一部分 → CASE 包表达式
	both, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Exprs: []string{"lower(name)"}, Where: "active = 1"})
	got = d.CreateIndexUniqueSql("", "pi_job", both)
	if !strings.Contains(got, "(CASE WHEN (active = 1) THEN lower(name) ELSE NULL END)") {
		t.Errorf("MySQL 表达式 + 唯一部分应包 CASE：%s", got)
	}
}

func TestMySQL_ValidateIndex_RefusesWithoutFunctionalParts(t *testing.T) {
	d := newMySQLDialectForTest(t, false)

	pu, _ := newIndexSpec("pi_job", IndexSpec{Unique: true, Cols: []string{"tenant_id"}, Where: "state = 'pending'"})
	if err := d.ValidateIndex(pu); err == nil || !stdErrors.Is(err, ormerr.ErrIndexUnsupported) {
		t.Errorf("老 MySQL 上的唯一部分索引必须拒绝（退化会改变唯一性语义），得到 %v", err)
	}
	ex, _ := newIndexSpec("pi_job", IndexSpec{Exprs: []string{"lower(name)"}})
	if err := d.ValidateIndex(ex); err == nil || !stdErrors.Is(err, ormerr.ErrIndexUnsupported) {
		t.Errorf("老 MySQL 上的表达式索引必须拒绝，得到 %v", err)
	}
	// 非唯一部分索引与普通索引不拒绝：能无损退化
	pi, _ := newIndexSpec("pi_job", IndexSpec{Cols: []string{"name"}, Where: "state = 'done'"})
	if err := d.ValidateIndex(pi); err != nil {
		t.Errorf("非唯一部分索引可退化为全表索引，不该拒绝：%v", err)
	}
	if err := d.ValidateIndex(newIndex("", "pi_job", UniqueType, "name")); err != nil {
		t.Errorf("普通唯一索引不该拒绝：%v", err)
	}
}
