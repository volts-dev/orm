package orm

import "testing"

/*
右值为**空**（`''` 或 nil）时的 `=` / `!=` 回归。

expr_falsy_types_test.go 管的是 `('x','=',False)`。这个文件管的是同一个语义的
另外两种右值形态 —— 而它们**不是人写在 XML 里的，是框架自己生成的**：

	core/model/model_controller_read_group.go 的 formatGroups 为每个分组拼一条
	下钻域（`__domain`）。分组键为空时：many2one 那支走 AsString() 得到**空串**，
	日期那支显式给 **nil**。看板/透视点开"无XX"那一列，前端把这条域原样打回来。

2026-08-31 documents 真栈实测：文档按工作区（folder_id，可为空——根工作区没有
父级）分组，点"无工作区"那一列：

	SELECT ... WHERE ((folder_id = $3) OR folder_id IS NULL)   [args] [... ""]
	pq: invalid input syntax for type bigint: "" (22P02)

病灶：这两种形态都落进 leaf_to_sql 最后那个通用分支。那里的
`add_null := right.String() == ""` 已经补了 `OR x IS NULL`，但 `x = ?` 那半边
照样把空串绑给 bigint/timestamp。

# 这些用例跑在 sqlite 上，为什么仍然抓得住

与 falsy 那份同一条注意事项：sqlite 不做类型检查，PG 上 500 的在这里变成
**静默回错行**（空串折算成 0 / 日期比较不成立），所以判据一律是**集合本身**，
不是 err != nil。数值样本必须带 NULL 行，否则坏代码 `num = ''`→`num = 0` 恰好
命中 empty_row，反证就是绿的。

反证（把新分支的条件改成 `false &&`）2026-08-31 实测：

	('num','=',"")    得到 [null_row]                 应为 [empty_row null_row]
	('amount','=',"") 得到 [null_row]                 应为 [empty_row null_row]
	('num','=',nil)   得到 [null_row]                 应为 [empty_row null_row]
	('num','!=',"")   得到 [empty_row full null_row]  应为 [full]
	('dt','!=',nil)   得到 [empty_row null_row]       应为 [full]

★ **`('dt','=',nil)` / `('dt','=',"")` 这两条在 sqlite 上修改前后都是绿的**，
别照着"它没变红"去删：sqlite 拿空串跟 datetime 比不成立、恰好与 IS NULL 语义
撞对了，而 PG 上同一条是 22007 直接 500。它们是给 PG 留的护栏。

★ 文本那条同理是护栏不是反证（文本列**有意不进新分支**）：改坏了（把 varchar
也改道）表现为空串行被漏掉。
*/

// 数值：空串右值。PG 上原来是 22P02 直接 500。
func TestEmptyRight_NumberEqualsEmptyString(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"num", "=", ""}, []string{"empty_row", "null_row"},
		"只剩 empty_row 是 sqlite 把空串折成 0 的假象——PG 上这条是 22P02")
	checkFalsy(t, o, []any{"amount", "=", ""}, []string{"empty_row", "null_row"},
		"浮点列同理")
}

// 数值：nil 右值（日期分组那支给的就是它，数值列同样走得到）。
func TestEmptyRight_NumberEqualsNil(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"num", "=", nil}, []string{"empty_row", "null_row"},
		"漏掉 null_row 说明 nil 没被认成'空'")
}

// 时间：只有 NULL 一种空形态；空串塞进 timestamp 本身就是 22007，
// 所以这里不能套文本那条 `OR x = ”`。
func TestEmptyRight_DatetimeEqualsNilAndEmpty(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"dt", "=", nil}, []string{"empty_row", "null_row"},
		"read_group 的日期下钻域给的正是 nil；回空说明还在跑 `dt = ''`")
	checkFalsy(t, o, []any{"dt", "=", ""}, []string{"empty_row", "null_row"},
		"空串形态同理")
}

// `!=` 与 `=` 是一对：只修一半的表现是"筛'有值的'把空行也带出来"。
func TestEmptyRight_NotEquals(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"num", "!=", ""}, []string{"full"},
		"混进 empty_row/null_row 说明 `!=` 那半边没走 isTruthyValue")
	checkFalsy(t, o, []any{"dt", "!=", nil}, []string{"full"},
		"同上")
}

// ★ 护栏：文本列不该被改道。空串在 varchar 上是合法比较，
// 通用分支产出的 `(x = ” OR x IS NULL)` 已经是对的。
func TestEmptyRight_TextKeepsOldBehaviour(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "=", ""}, []string{"empty_row", "null_row"},
		"文本列的空串比较被改坏了")
}

// ★ 护栏：0 不是"空右值"。把它也认成空的话，`('num','=',0)` 会连 NULL 行一起返回。
func TestEmptyRight_ZeroIsNotEmpty(t *testing.T) {
	o := setupFalsyTypes(t)
	got := falsyNames(t, o, []any{"num", "=", 0})
	for _, n := range got {
		if n == "null_row" {
			t.Fatalf("('num','=',0) 把 NULL 行也带出来了：%v —— 0 被误认成了空右值", got)
		}
	}
}
