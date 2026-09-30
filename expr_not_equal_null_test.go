package orm

import "testing"

/*
否定操作符与 NULL（问题 47）。

Odoo 的 `('f','!=',v)` 是「f 不等于 v，**或者 f 没有值**」；SQL 的 `f != v` 对 NULL 行
是 NULL（不匹配）。本仓此前原样译成 SQL，于是移植过来的域和记录规则凡写 `!=` 的都
把空值行排除了。2026-09-29 真栈：calendar 的 Private events 规则
`('privacy','!=','private')` 把没设隐私的会议全挡在写范围之外。

`not in` 的两个分支早就补了 `OR x IS NULL`，漏的是 `!=`、`not like`、`not ilike`。

反证（把 add_null 那条 if 删掉）：

	('txt','!=','hello')        得到 [empty_row]  应为 [empty_row null_row]
	('num','!=',7)              得到 [empty_row]  应为 [empty_row null_row]
	('txt','not ilike','hel')   得到 [empty_row]  应为 [empty_row null_row]
*/

func TestNotEqual_IncludesNullRows(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "!=", "hello"}, []string{"empty_row", "null_row"},
		"漏掉 null_row 说明 `!=` 还是 SQL 语义")
	checkFalsy(t, o, []any{"num", "!=", 7}, []string{"empty_row", "null_row"},
		"数值列同理")
	checkFalsy(t, o, []any{"amount", "!=", 1.5}, []string{"empty_row", "null_row"},
		"浮点列同理")
}

func TestNotLike_IncludesNullRows(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "not ilike", "hel"}, []string{"empty_row", "null_row"},
		"漏掉 null_row 说明 not ilike 没补 IS NULL")
	checkFalsy(t, o, []any{"txt", "not like", "hel"}, []string{"empty_row", "null_row"},
		"not like 同理")
}

// 不等于空串 / 不等于 0 问的是"有值"，NULL 行不该进来 —— 右值为假时不补。
func TestNotEqual_FalsyRightStillExcludesNull(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "!=", ""}, []string{"full"},
		"混进 null_row 说明 `!= ''` 被补了 IS NULL")
	checkFalsy(t, o, []any{"num", "!=", 0}, []string{"full"},
		"混进 null_row 说明 `!= 0` 被补了 IS NULL")
}

// 肯定操作符不受影响。
func TestEqual_StillExcludesNull(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "=", "hello"}, []string{"full"}, "")
	checkFalsy(t, o, []any{"txt", "ilike", "hel"}, []string{"full"}, "")
}
