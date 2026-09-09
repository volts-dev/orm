package orm

import (
	"testing"

	_ "github.com/lib/pq"
)

// fracDefaultProbe 覆盖"小数默认值被截成整数"这条：
// 0.01 是最小的会出事的值——截整之后是 0，而 0 在"现金舍入精度"这种语义上等于
// 把功能关掉（point_of_sale 的 account_cash_rounding.Rounding 就是它）。
type fracDefaultProbe struct {
	TModel  `table:"name('frac_default_probe')"`
	Id      int64   `field:"pk autoincr"`
	Rate    float64 `field:"double() default(0.01)"`
	Decay   float64 `field:"double() default(1.8)"`
	Whole   float64 `field:"double() default(1)"`
	Counter int64   `field:"int default(3)"`
}

// TestFractionalDefaultsSurviveTagParsing 是这条 bug 的直接判据。
//
// 曾经的写法是 `if field.SqlType.IsNumeric() { defaultValue = utils.ToInt64(...) }`，
// 而 NUMERIC_TYPE 把 Double/Float/Decimal 与整数类型混在一起 —— 于是
// `double() default(0.01)` 的默认值变成 0，**不报错、不告警**：DDL 写 0、落库是 0、
// 前端 fields 元数据里也是 0。全仓 139 个带默认值的浮点字段里有 136 个写的是
// 0.0/1.0，截整后碰巧是对的，所以这条一直没露头。
func TestFractionalDefaultsSurviveTagParsing(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := o.Exec(`DROP TABLE IF EXISTS frac_default_probe`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	defer o.Exec(`DROP TABLE IF EXISTS frac_default_probe`)

	model, err := o.SyncModel("test", new(fracDefaultProbe))
	if err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	_ = model

	m, err := o.GetModel("frac_default_probe")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	for _, c := range []struct {
		field string
		want  float64
	}{
		{"rate", 0.01},
		{"decay", 1.8},
		{"whole", 1},
	} {
		f := m.GetFieldByName(c.field)
		if f == nil {
			t.Fatalf("字段 %s 不见了", c.field)
		}
		got := toFloatForTest(f.Base().Default())
		if got != c.want {
			t.Errorf("%s 的默认值是 %v（%T），要的是 %v —— 小数默认值又被截成整数了",
				c.field, f.Base().Default(), f.Base().Default(), c.want)
		}
	}

	// 整数字段不受影响：它仍然该是整数，不该顺手变成浮点。
	if f := m.GetFieldByName("counter"); f != nil {
		if _, ok := f.Base().Default().(int64); !ok {
			t.Errorf("整数字段的默认值类型变了：%T", f.Base().Default())
		}
	}
}

// TestFractionalDefaultsNoChurn：带小数默认值的表第二次同步不得再发 DDL。
// 这条是上面那条修法的代价检查 —— 结构体侧变成 0.01 之后，库里内省回来的必须也是
// 0.01，否则每次启动都会重发 ALTER ... SET DEFAULT（见 session.go 的默认值比对）。
func TestFractionalDefaultsNoChurn(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := o.Exec(`DROP TABLE IF EXISTS frac_default_probe`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	defer o.Exec(`DROP TABLE IF EXISTS frac_default_probe`)

	if _, err := o.SyncModel("test", new(fracDefaultProbe)); err != nil {
		t.Fatalf("SyncModel #1: %v", err)
	}
	epoch := o.metaEpoch.Load()
	if _, err := o.SyncModel("test", new(fracDefaultProbe)); err != nil {
		t.Fatalf("SyncModel #2: %v", err)
	}
	if churn := o.metaEpoch.Load() - epoch; churn != 0 {
		t.Fatalf("第二次同步发了 %d 条 DDL，应当是 0 —— 小数默认值往返不一致，"+
			"每次启动都会重发 ALTER ... SET DEFAULT", churn)
	}
}

// TestSQLTypeIsFractional 纯判定：哪些类型能存小数。
func TestSQLTypeIsFractional(t *testing.T) {
	frac := []string{Real, Float, Double, Decimal, Numeric, Money, SmallMoney}
	whole := []string{TinyInt, SmallInt, MediumInt, Int, Integer, BigInt, Serial, BigSerial}
	for _, n := range frac {
		if !(&SQLType{Name: n}).IsFractional() {
			t.Errorf("%s 能存小数，却被判成不能", n)
		}
	}
	for _, n := range whole {
		if (&SQLType{Name: n}).IsFractional() {
			t.Errorf("%s 存不了小数，却被判成能 —— 它的默认值会从 int64 变成 float64", n)
		}
	}
}

func toFloatForTest(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return -1
}
