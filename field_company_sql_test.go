package orm

import (
	"fmt"
	"testing"
)

// 按公司区分字段的 SQL 片段收进方言之后（2026-10-24），PG 上生成的 SQL 必须与之前逐字节一致：
// 查询缓存按 SQL 原文做键，语义也不能有一丝变化。下面的期望值就是收口之前 field_company.go 的格式串。
func TestCompanyValueExpr_PostgresUnchanged(t *testing.T) {
	pg := &postgres{}
	f := &TField{}
	f.SqlType = SQLType{BigInt, 0, 0}
	f.typeName = BigInt
	f.name = "level"
	ref := `"t"."level"`
	typ := pg.valueSqlType(f)
	cases := []struct {
		cid  int64
		dyn  string
		want string
	}{
		{7, "", fmt.Sprintf(`(COALESCE(%s->>'%d', %s->>'0'%s))::%s`, ref, 7, ref, "", typ)},
		{0, "", fmt.Sprintf(`(%s->>'0')::%s`, ref, typ)},
		{7, "'5'", fmt.Sprintf(`(CASE WHEN %[1]s->'%[2]d' IS NOT NULL THEN %[1]s->>'%[2]d' WHEN %[1]s->'0' IS NOT NULL THEN %[1]s->>'0' ELSE %[3]s END)::%[4]s`, ref, 7, "'5'", typ)},
		{0, "'5'", fmt.Sprintf(`(CASE WHEN %[1]s->'0' IS NOT NULL THEN %[1]s->>'0' ELSE %[2]s END)::%[3]s`, ref, "'5'", typ)},
	}
	for _, c := range cases {
		got, err := companyValueExpr(pg, ref, f, c.cid, c.dyn)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("cid=%d dyn=%q:\n got %s\nwant %s", c.cid, c.dyn, got, c.want)
		}
	}
	f.SetDefault(int64(3))
	got, _ := companyValueExpr(pg, ref, f, 7, "")
	if want := fmt.Sprintf(`(COALESCE(%s->>'%d', %s->>'0'%s))::%s`, ref, 7, ref, ", '3'", typ); got != want {
		t.Errorf("带静态默认值:\n got %s\nwant %s", got, want)
	}

	set, _, _ := companySetClause(pg, `"level"`, f, 7, int64(9), false)
	if want := `"level" = jsonb_set(COALESCE("level", '{}'::jsonb), '{7}', ?::jsonb)`; set != want {
		t.Errorf("set:\n got %s\nwant %s", set, want)
	}
	set, _, _ = companySetClause(pg, `"level"`, f, 7, nil, true)
	if want := `"level" = jsonb_set(COALESCE("level", '{}'::jsonb), '{7}', 'null'::jsonb)`; set != want {
		t.Errorf("set null:\n got %s\nwant %s", set, want)
	}
	set, _, _ = companySetClause(pg, `"level"`, f, 7, nil, false)
	if want := `"level" = COALESCE("level", '{}'::jsonb) - '7'`; set != want {
		t.Errorf("unset:\n got %s\nwant %s", set, want)
	}
}
