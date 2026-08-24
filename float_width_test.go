package orm

import (
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

/*
浮点列的宽度。

`field:"float()"` 从前无条件建 REAL——单精度，约 7 位有效数字。金额过万就开始掉
小数：写 12345.67 读回 12345.7，不报错、不告警，只有错的数。int/bigint 那两个
Init 一直是看 Go 类型的，只有浮点这里没跟上。modules 全仓因此改用 double()
（287 处 double 对 12 处 float），绕过而非修复。
*/

type fwProbe struct {
	TModel  `table:"name('fw_probe')"`
	Id      int64   `field:"pk autoincr"`
	Amount  float64 `field:"float() title('Amount')"` // ← 病灶：float64 + float()
	Ratio   float32 `field:"float() title('Ratio')"`  // 两侧都说单精度，保持 real
	Wide    float32 `field:"double() title('Wide')"`  // 标签更宽，以标签为准
	Precise float64 `field:"double() title('Precise')"`
}

// ---------- 类型推导 ----------

func TestFloatWidth_TagAndGoTypeTakeTheWider(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(fwProbe)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	model := o.Model("fw.probe").Statement.Model

	cases := []struct {
		field string
		want  string
		why   string
	}{
		{"amount", Double, "float64 + float()：Go 类型更宽，必须以它为准——单精度装不下金额"},
		{"ratio", Float, "float32 + float()：两侧都说了单精度，维持 real"},
		{"wide", Double, "float32 + double()：标签更宽，以标签为准"},
		{"precise", Double, "float64 + double()"},
	}
	for _, c := range cases {
		f := model.GetFieldByName(c.field)
		if f == nil {
			t.Fatalf("字段 %s 不存在", c.field)
		}
		if got := f.SQLType().Name; got != c.want {
			t.Errorf("%s 应为 %s，得 %s —— %s", c.field, c.want, got, c.why)
		}
		// typeName 也跟着走：它就是 Attributes()["type"]，ORM 发给外部的字段描述符。
		// 所以 float64 + float() 的字段对外从 "FLOAT" 变成了 "DOUBLE"。
		// 这不是新增的变数——int/bigint 一直按 Go 类型在 "INT"/"BIGINT" 之间变
		// （TIntField.Init 从来就是看 Go 类型的），消费方本就得按族处理。
		if got := f.TypeName(); got != c.want {
			t.Errorf("%s 的 TypeName() 应为 %s，得 %s", c.field, c.want, got)
		}
		if got := f.Attributes(nil)["type"]; got != c.want {
			t.Errorf("%s 的 Attributes()[\"type\"] 应为 %s，得 %v", c.field, c.want, got)
		}
	}
}

// postgres 上的落地形态：REAL 是单精度，DOUBLE PRECISION 才是双精度。
func TestFloatWidth_PostgresDDL(t *testing.T) {
	d := &postgres{}
	d.dialect = d

	mk := func(name string, st SQLType) IField {
		f := new(TFloatField)
		b := f.Base()
		b.name = name
		b.SqlType = st
		return f
	}
	if got := d.GetSqlType(mk("amount", SQLType{Double, 0, 0})); got != "DOUBLE PRECISION" {
		t.Fatalf("Double 应落成 DOUBLE PRECISION，得 %q", got)
	}
	if got := d.GetSqlType(mk("ratio", SQLType{Float, 0, 0})); got != Real {
		t.Fatalf("Float 应落成 REAL，得 %q", got)
	}
}

// ---------- 列宽对账 ----------

func TestNumericNarrowing(t *testing.T) {
	cases := []struct {
		cur, want string
		narrowing bool
	}{
		// 真正要报的：库里是单精度，模型要双精度
		{"REAL", "DOUBLE PRECISION", true},
		{"FLOAT", "DOUBLE", true},
		{"INTEGER", "BIGINT", true},
		{"SMALLINT", "INTEGER", true},
		// 反过来不丢数据，不惊动任何人
		{"DOUBLE PRECISION", "REAL", false},
		{"BIGINT", "INTEGER", false},
		// 同宽
		{"BIGINT", "BIGINT", false},
		// 跨族：那是模型写错了类型，不是宽窄问题，交给原来那条泛化告警
		{"INTEGER", "DOUBLE PRECISION", false},
		{"REAL", "BIGINT", false},
		// 非数值一律不管
		{"VARCHAR(64)", "TEXT", false},
		{"TEXT", "VARCHAR(64)", false},
		// 带长度修饰、大小写混杂
		{"numeric(16,2)", "BIGINT", false},
		{"real", "double precision", true},
	}
	for _, c := range cases {
		if got := isNumericNarrowing(c.cur, c.want); got != c.narrowing {
			t.Errorf("isNumericNarrowing(%q, %q) = %v，期望 %v", c.cur, c.want, got, c.narrowing)
		}
	}
}

func TestNormalizeSqlTypeName(t *testing.T) {
	for in, want := range map[string]string{
		"numeric(16,2)":    "NUMERIC",
		" varchar (64) ":   "VARCHAR",
		"DOUBLE PRECISION": "DOUBLE PRECISION",
		"bigint":           "BIGINT",
	} {
		if got := normalizeSqlTypeName(in); got != want {
			t.Errorf("normalizeSqlTypeName(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// ---------- 真库：精度是否真的保住了 ----------

// 单精度约 7 位有效数字。12345.67 存进 REAL 再读出来是 12345.7——这条用例在修复前
// 会失败，正是它要证明的那个"只有错的数"。
func TestFloatWidth_PostgresKeepsPrecision(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := o.Exec(`DROP TABLE IF EXISTS fw_probe`); err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { o.Exec(`DROP TABLE IF EXISTS fw_probe`) })
	if _, err := o.SyncModel("", new(fwProbe)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	// 列类型落地形态
	ds2, err := o.Query(`SELECT column_name, data_type FROM information_schema.columns
	                     WHERE table_name='fw_probe' ORDER BY column_name`)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	ds2.First()
	for !ds2.Eof() {
		rec := ds2.Record()
		types[rec.FieldByName("column_name").AsString()] = rec.FieldByName("data_type").AsString()
		ds2.Next()
	}
	if got := types["amount"]; !strings.Contains(strings.ToLower(got), "double") {
		t.Fatalf("amount(float64 + float()) 应建成 double precision，得 %q —— 金额列是单精度就是数据损坏", got)
	}
	if got := types["ratio"]; strings.ToLower(got) != "real" {
		t.Fatalf("ratio(float32 + float()) 应维持 real，得 %q", got)
	}

	// 精度往返
	const amount = 12345.67
	ids, err := o.Model("fw.probe").Create(map[string]any{"amount": amount, "ratio": float32(1.5)})
	if err != nil {
		t.Fatal(err)
	}
	rs, err := o.Model("fw.probe").Ids(ids[0]).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := rs.Record().FieldByName("amount").AsFloat(); got != amount {
		t.Fatalf("金额往返应当分毫不差，写 %v 读回 %v —— 单精度只有约 7 位有效数字", amount, got)
	}
}
