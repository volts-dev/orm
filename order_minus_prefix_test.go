package orm

import (
	"testing"

	"github.com/volts-dev/dataset"
)

/*
`-字段` 排序写法的回归（`-date` = `date desc`）。

这是 Django / Odoo 前端 orderBy 一系的写法，移植代码里反复漏进来：mass_mailing 的「从收藏
载入」写的是 OrderBy("-favorite_date")，stock 估值写过 OrderBy("-date", "-id")。ORM 原来把
`-favorite_date` 整个当字段名，查不到就跳过并只打一条

	Sorting field -favorite_date not found on model mailing.mailing

然后兜底按主键**升序**——不是「没排序」，结果稳定、看着合理，只是方向反了：存货 FIFO 算成
LIFO、「最后一条」取到最早那条（见 memory i_orm_orderby_minus_prefix_silently_dropped）。

前缀后面再写方向时以写明的方向为准（`-date asc` 是自相矛盾的写法，按字面的 asc 走并告警）。
*/

func orderedNames(t *testing.T, o *TOrm, order ...string) []string {
	t.Helper()
	ds, err := o.Model("falsy.row").OrderBy(order...).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read order %v: %v", order, err)
	}
	var out []string
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		out = append(out, rec.FieldByName("name").AsString())
		return nil
	})
	return out
}

func TestOrderBy_MinusPrefixMeansDesc(t *testing.T) {
	o := setupFalsyTypes(t) // num: full=7, empty_row=0, null_row=NULL；id 顺序 full, null_row, empty_row

	want := orderedNames(t, o, "num desc")
	if got := orderedNames(t, o, "-num"); !sameStrings(got, want) {
		t.Fatalf(`OrderBy("-num") 得到 %v，应与 "num desc" 一致 %v（按 id 升序就是前缀被当成字段名丢掉了）`, got, want)
	}
	// 多项 + 逗号串里的前缀。
	if got, want := orderedNames(t, o, "-num", "-id"), orderedNames(t, o, "num desc", "id desc"); !sameStrings(got, want) {
		t.Fatalf(`OrderBy("-num", "-id") 得到 %v，应为 %v`, got, want)
	}
	if got, want := orderedNames(t, o, "name, -num"), orderedNames(t, o, "name, num desc"); !sameStrings(got, want) {
		t.Fatalf(`OrderBy("name, -num") 得到 %v，应为 %v`, got, want)
	}
	// 主键也认前缀。
	if got, want := orderedNames(t, o, "-id"), orderedNames(t, o, "id desc"); !sameStrings(got, want) {
		t.Fatalf(`OrderBy("-id") 得到 %v，应为 %v`, got, want)
	}
}
