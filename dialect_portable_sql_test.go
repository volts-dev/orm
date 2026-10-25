package orm

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/utils"
)

// dialect_portable_sql.go 的几种语句在三个库上改的、删的、比出来的是同一批行（ORM_TEST_DB=sqlite|postgres|mysql）。
func TestPortableSql_SameRowsOnEveryDialect(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "portable.db"))))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	d := o.Dialect()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := o.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	rows := func(q string, args ...any) string {
		t.Helper()
		ds, err := o.Query(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		var out []string
		cols := strings.Split(q[len("SELECT "):strings.Index(q, " FROM ")], ",")
		for ds.First(); !ds.Eof(); ds.Next() {
			var cells []string
			for _, f := range cols {
				f = strings.TrimSpace(f)
				if i := strings.LastIndex(f, " "); i >= 0 {
					f = f[i+1:]
				}
				if i := strings.LastIndex(f, "."); i >= 0 {
					f = f[i+1:]
				}
				cells = append(cells, ds.FieldByName(f).AsString())
			}
			out = append(out, strings.Join(cells, "/"))
		}
		return strings.Join(out, " ")
	}
	reset := func() {
		exec(`DROP TABLE IF EXISTS pt_tgt`)
		exec(`DROP TABLE IF EXISTS pt_src`)
		// 两张表都有 v 与 tenant_id：MySQL 多表语句里不带别名就是 1052。
		exec(`CREATE TABLE pt_tgt (id bigint PRIMARY KEY, tenant_id bigint, v int, w varchar(20))`)
		exec(`CREATE TABLE pt_src (id bigint PRIMARY KEY, tenant_id bigint, v int)`)
		exec(`INSERT INTO pt_tgt VALUES (1, 7, 10, 'a'), (2, 7, 20, 'b'), (3, 8, 30, 'c'), (4, 7, NULL, 'd')`)
		exec(`INSERT INTO pt_src VALUES (1, 7, 11), (2, 7, 20), (3, 7, 33), (4, 7, 44)`)
	}

	t.Run("UpdateFrom", func(t *testing.T) {
		reset()
		res, err := o.Exec(d.UpdateFromSql(UpdateFrom{
			Table: "pt_tgt", Alias: "t", From: "pt_src s",
			Set:   []SetExpr{{"v", "s.v"}, {"w", "'x'"}},
			Where: "s.id = t.id AND s.tenant_id = t.tenant_id AND t.tenant_id = ? AND " + d.IsDistinctSql("t.v", "s.v"),
		}), 7)
		if err != nil {
			t.Fatal(err)
		}
		// 1：10≠11 改；2：20=20 不改；3：租户不同不改；4：NULL≠44 改。
		if n, _ := res.RowsAffected(); n != 2 {
			t.Errorf("改了 %d 行，want 2", n)
		}
		if got, want := rows(`SELECT id, v, w FROM pt_tgt ORDER BY id`), "1/11/x 2/20/b 3/30/c 4/44/x"; got != want {
			t.Errorf("得 %s\nwant %s", got, want)
		}
		// 来源是子查询。
		exec(d.UpdateFromSql(UpdateFrom{
			Table: "pt_tgt", Alias: "t", From: "(SELECT tenant_id, max(v) AS mv FROM pt_src GROUP BY tenant_id) m",
			Set: []SetExpr{{"v", "m.mv"}}, Where: "m.tenant_id = t.tenant_id AND t.id = ?",
		}), 2)
		if got, want := rows(`SELECT id, v FROM pt_tgt WHERE id = 2`), "2/44"; got != want {
			t.Errorf("子查询来源：得 %s，want %s", got, want)
		}
	})

	t.Run("DeleteUsing", func(t *testing.T) {
		reset()
		exec(d.DeleteUsingSql(DeleteUsing{Table: "pt_tgt", Alias: "t", Using: "pt_src s",
			Where: "s.id = t.id AND s.tenant_id = t.tenant_id AND s.v > ?"}), 15)
		// 2（20>15，同租户）、4（44>15）删；1（11）留；3 租户不同留。
		if got, want := rows(`SELECT id FROM pt_tgt ORDER BY id`), "1 3"; got != want {
			t.Errorf("得 %s，want %s", got, want)
		}
	})

	t.Run("IsDistinct", func(t *testing.T) {
		reset()
		exec(`INSERT INTO pt_src VALUES (5, NULL, NULL)`)
		// 对 (a, b)：(10,11) (20,20) (NULL,44) (NULL,NULL)
		q := func(cond string) string {
			return rows(`SELECT t.id AS id FROM pt_tgt t JOIN pt_src s ON s.id = (CASE WHEN t.id = 3 THEN 5 ELSE t.id END)
				WHERE ` + cond + ` ORDER BY t.id`)
		}
		exec(`UPDATE pt_tgt SET v = NULL WHERE id = 3`)
		if got, want := q(d.IsDistinctSql("t.v", "s.v")), "1 4"; got != want {
			t.Errorf("IS DISTINCT FROM：得 %s，want %s（NULL 与 NULL 不算不同）", got, want)
		}
		if got, want := q(d.IsNotDistinctSql("t.v", "s.v")), "2 3"; got != want {
			t.Errorf("IS NOT DISTINCT FROM：得 %s，want %s", got, want)
		}
	})

	t.Run("OnConflictDoNothing", func(t *testing.T) {
		reset()
		// INSERT … SELECT，来源里也有 v：MySQL 上 ON DUPLICATE KEY UPDATE 的列不带表名就是 1052。
		// SQLite 的 INSERT … SELECT … ON CONFLICT 要有 WHERE 才不和 JOIN 的 ON 混淆。
		q := `INSERT INTO pt_tgt (id, tenant_id, v, w) SELECT s.id, s.tenant_id, s.v, 'new' FROM pt_src s WHERE s.id > ?` +
			d.OnConflictDoNothingSql("pt_tgt", "v")
		exec(q, 0)
		exec(`INSERT INTO pt_src VALUES (9, 7, 99)`)
		exec(q, 0)
		if got, want := rows(`SELECT id, v, w FROM pt_tgt ORDER BY id`), "1/10/a 2/20/b 3/30/c 4//d 9/99/new"; got != want {
			t.Errorf("得 %s\nwant %s（已有的行一个字段都不该变）", got, want)
		}
	})

	t.Run("StringAgg", func(t *testing.T) {
		exec(`DROP TABLE IF EXISTS pt_ids`)
		exec(`CREATE TABLE pt_ids (id bigint PRIMARY KEY, g int)`)
		// 200 个 19 位的 id：拼起来约 4KB，超过 MySQL group_concat_max_len 的默认 1024 字节。
		var want []string
		var vals []string
		for i := 0; i < 200; i++ {
			id := int64(2114394690179059712) + int64(i)*7919
			want = append(want, fmt.Sprint(id))
			vals = append(vals, fmt.Sprintf("(%d, %d)", id, i%2))
		}
		exec(`INSERT INTO pt_ids VALUES ` + strings.Join(vals, ", "))
		ds, err := o.Query(`SELECT ` + d.StringAggSql("id", ",", false) + ` AS ids, ` + d.StringAggSql("g", ";", true) + ` AS gs FROM pt_ids`)
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Split(utils.ToString(ds.FieldByName("ids").AsInterface()), ",")
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("拼回来 %d 个 id（want %d）——被截断了？前几个 %v", len(got), len(want), got[:min(3, len(got))])
		}
		gs := strings.Split(utils.ToString(ds.FieldByName("gs").AsInterface()), ";")
		sort.Strings(gs)
		if strings.Join(gs, ";") != "0;1" {
			t.Errorf("DISTINCT 拼 %q，want 0;1", gs)
		}
	})
}
