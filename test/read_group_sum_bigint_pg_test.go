package test

import (
	"testing"

	_ "github.com/lib/pq"
	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm"
)

// read_group 的整数 measure 在 **Postgres** 上必须是真的合计，不能恒为 0。
//
// # 失败的样子
//
// graph / pivot / kanban 三种视图都靠 read_group 取数。回归时它们**不报错**：
// 每组的记录数（__count）完全正确，只有各 measure 的合计全是 0 —— 图表画出一
// 排贴地的柱子、透视表满屏 0，看起来像"这个库还没有数据"，而不像有 bug。
//
// # 为什么只在 Postgres 上
//
// PG 的聚合会**抬升类型**：`SUM(bigint)` 出来是 numeric（`AVG` 更是一律
// numeric）。而 numeric 在 Go 里没有对应类型，lib/pq 只解 int8 / float8 / bool /
// 时间 / bytea，其余一概以 []byte 交回。orm 的读出口按字段声明的类型解码，
// utils.ToInt64 不认 []byte，**静默返回 0**。
// `COUNT(*)` 是 bigint、照常解成 int64，所以"分组数对、合计全零"。
//
// sqlite 上 `SUM(int)` 仍然是整数，read_group_measure_test.go 跑的正是 sqlite，
// 所以那一组用例全绿也证明不了这条。**这个用例必须连真库**。
type (
	RGSumRow struct {
		orm.TModel `table:"name('rg_sum_row')"`
		Id         int64  `field:"pk autoincr title('ID')"`
		Kind       string `field:"varchar() size(16) index"`
		// int64 → BIGINT，SUM 之后是 numeric —— 这就是回归点。
		Qty int64 `field:"int() default(0)"`
		// 对照组：float64 → DOUBLE PRECISION，SUM 之后仍是 double，
		// 驱动解得出 float64，所以浮点 measure 从来没坏过。回归时这一列是对的、
		// 整数列是 0，两者并排才看得出病灶在类型抬升而不在分组本身。
		Amount float64 `field:"double() default(0)"`
	}
)

func TestReadGroup_SumOfBigIntMeasure_PG(t *testing.T) {
	ensurePG(t)

	o, err := orm.New(orm.WithDataSource(defaultPostgresSource()))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.Exec(`DROP TABLE IF EXISTS public.rg_sum_row`); err != nil {
		t.Fatalf("清场: %v", err)
	}
	if _, err := o.SyncModel("test", new(RGSumRow)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	for _, r := range []struct {
		kind string
		qty  int64
		amt  float64
	}{
		{"x", 1, 1.5}, {"x", 2, 2.5}, {"y", 4, 4.0},
	} {
		if _, err := o.Model("rg.sum.row").Create(map[string]any{
			"kind": r.kind, "qty": r.qty, "amount": r.amt,
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	mdl, err := o.GetModel("rg.sum.row")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	grouper, ok := mdl.(interface {
		ReadGroup(*orm.ReadGroupRequest) (*dataset.TDataSet, error)
	})
	if !ok {
		t.Fatalf("模型不支持 ReadGroup")
	}

	ds, err := grouper.ReadGroup(&orm.ReadGroupRequest{
		Model:   "rg.sum.row",
		Fields:  []string{"qty", "amount"},
		GroupBy: []string{"kind"},
	})
	if err != nil {
		t.Fatalf("ReadGroup: %v", err)
	}
	if ds == nil || ds.Count() != 2 {
		t.Fatalf("应当分出 2 组，实得 %v", ds.Count())
	}

	got := map[string][3]float64{} // kind → [__count, qty 合计, amount 合计]
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		got[rec.FieldByName("kind").AsString()] = [3]float64{
			float64(rec.FieldByName(orm.GroupCountField).AsInteger()),
			rec.FieldByName("qty").AsFloat(),
			rec.FieldByName("amount").AsFloat(),
		}
		return nil
	})

	for _, want := range []struct {
		kind              string
		count, qty, amunt float64
	}{
		{"x", 2, 3, 4.0},
		{"y", 1, 4, 4.0},
	} {
		g, has := got[want.kind]
		if !has {
			t.Fatalf("分组 %q 不见了，实得 %v", want.kind, got)
		}
		if g[0] != want.count {
			t.Errorf("分组 %q 的 __count = %v，应为 %v", want.kind, g[0], want.count)
		}
		if g[1] != want.qty {
			t.Errorf("分组 %q 的 qty 合计 = %v，应为 %v —— PG 的 SUM(bigint) 是 numeric，"+
				"lib/pq 以 []byte 交回，按整数解码时静默变成了 0。"+
				"真机上的样子：图/透视/看板的分组数全对而合计全是零。",
				want.kind, g[1], want.qty)
		}
		if g[2] != want.amunt {
			t.Errorf("分组 %q 的 amount 合计 = %v，应为 %v（浮点列是对照组，"+
				"它坏了说明问题不在类型抬升上）", want.kind, g[2], want.amunt)
		}
	}
}
