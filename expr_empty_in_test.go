package orm

import (
	"strings"
	"testing"

	"github.com/volts-dev/orm/domain"
)

/*
`in` / `not in` 的值列表里出现**空串**的回归（`=` / `!=` 的空右值见 expr_empty_right_test.go）。

前端拿一个空的多对一去读（`account_move_id` 是 ""），发出来的是 ids [""]，到 ORM 就是
`('id','in',[''])`。解析器把单元素列表拆成标量，于是落进 `in` 的单值分支，生成：

	SELECT ... FROM account_move WHERE (... AND (account_move."id" = $3))  [args] [... ""]
	pq: invalid input syntax for type bigint: "" (22P02)

（2026-10-11 真栈 account.move/read，整次读取 500。）列表里的 `false` 早就会被摘出来换成
IS NULL 语义，空串是同一个意思的另一种写法，却原样绑进了 bigint 列。

sqlite 不做类型检查，`num = ''` 在这里只是静默回 0 行，所以数值列的判据是**集合**（NULL 行
要能回来），id 列的判据是**绑定参数里不再有空串**（id 没有 NULL 行，集合本身分不出新旧）。
*/

func TestEmptyIn_IdSingleEmptyStringBindsNothing(t *testing.T) {
	o := setupFalsyTypes(t)
	model, err := o.GetModel("falsy.row")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []*domain.TDomainNode{
		domain.New("id", "in", ""),
		domain.New("id", "in", "", ""),
		domain.New("id", "not in", ""),
	} {
		exp, err := NewExpression(o, model.GetBase(), d, nil)
		if err != nil {
			t.Fatal(err)
		}
		clause, params := exp.ToSql()
		for _, p := range params {
			if s, ok := p.(string); ok && s == "" {
				t.Fatalf("%s 把空串绑进了 bigint 列（PG 上 22P02）: %s %q",
					d.String(), strings.Join(clause, " "), params)
			}
		}
	}
	// 集合：in [''] 一条不回，not in [''] 全回。
	checkFalsy(t, o, []any{"id", "in", []any{""}}, nil, "id 不可能为空")
	checkFalsy(t, o, []any{"id", "not in", []any{""}}, []string{"empty_row", "full", "null_row"}, "not in 空 = 有值")
}

func TestEmptyIn_NumberEmptyStringIsNull(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"num", "in", []any{""}}, []string{"null_row"},
		"回空是还在跑 `num = ''`（PG 上 22P02）")
	checkFalsy(t, o, []any{"num", "in", []any{7, ""}}, []string{"full", "null_row"},
		"列表里的空串同 false：摘出来补 IS NULL")
	checkFalsy(t, o, []any{"num", "not in", []any{""}}, []string{"empty_row", "full"},
		"not in 空串 = 不为 NULL")
}

// 文本列的空串是一个真值，不能被当成「没值」摘掉。
func TestEmptyIn_TextEmptyStringIsAValue(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "in", []any{"", "hello"}}, []string{"empty_row", "full"},
		"文本列的空串被当成了 NULL")
}
