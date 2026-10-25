package orm

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// 规格（properties）筛选在 PG 与 MySQL 上筛出同一批记录（MySQL 兼容，2026-10-26）。
func TestPropertyFilter_SameRowsOnPGAndMySQL(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "props.db"))))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	d := o.Dialect()
	jt := "jsonb"
	if d.DBType() == "mysql" {
		jt = "json"
	}
	for _, q := range []string{
		`CREATE TABLE prop_rec (id bigint PRIMARY KEY, props ` + jt + `)`,
		`INSERT INTO prop_rec VALUES
			(1, '{"mat": "丝", "w": 5}'),
			(2, '{"mat": "棉", "w": 10}'),
			(3, '{"mat": false, "w": 0}'),
			(4, '{"mat": null}'),
			(5, '{}'),
			(6, NULL),
			(7, '{"mat": "真丝绸", "w": 7.5, "d": "2026-10-01"}')`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, c := range []struct {
		key, op string
		vals    []any
		want    string
	}{
		{"mat", "=", []any{"丝"}, "1"},
		{"mat", "!=", []any{"丝"}, "2 3 4 5 6 7"},
		{"mat", "=", []any{false}, "3 4 5 6"},
		{"mat", "!=", []any{false}, "1 2 7"},
		{"mat", "in", []any{"丝", "棉"}, "1 2"},
		{"mat", "not in", []any{"丝", "棉"}, "3 4 5 6 7"},
		{"mat", "ilike", []any{"丝"}, "1 7"},
		{"mat", "not like", []any{"丝"}, "2 3"}, // false 的文本是 'false'；null / 缺键是 NULL，NOT LIKE 也不成立
		{"w", "=", []any{0}, "3"},
		{"w", ">", []any{5}, "2 7"},
		{"w", "<=", []any{5}, "1 3"},
		{"d", ">=", []any{"2026-09-01"}, "7"},
	} {
		t.Run(fmt.Sprintf("%s %s %v", c.key, c.op, c.vals), func(t *testing.T) {
			cond, args, err := propertyLeafToSql(d, "r", "props", c.key, c.op, c.vals)
			if err != nil {
				t.Fatal(err)
			}
			ds, err := o.Query(`SELECT r.id AS id FROM prop_rec r WHERE `+cond+` ORDER BY r.id`, args...)
			if err != nil {
				t.Fatalf("%s: %v", cond, err)
			}
			var got []string
			for ds.First(); !ds.Eof(); ds.Next() {
				got = append(got, ds.FieldByName("id").AsString())
			}
			if strings.Join(got, " ") != c.want {
				t.Errorf("得 %s，want %s\n    %s %v", strings.Join(got, " "), c.want, cond, args)
			}
		})
	}
}
