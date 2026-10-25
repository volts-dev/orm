package orm

import (
	"path/filepath"
	"strings"
	"testing"
)

// dialect_time_sql.go 的几样在 PG 与 MySQL 上取出同样的值（MySQL 兼容，2026-10-26）。
func TestTimeSql_SameValuesOnPGAndMySQL(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "tsql.db"))))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	d := o.Dialect()
	ts := "timestamp"
	if d.DBType() == "mysql" {
		ts = "datetime(6)"
	}
	for _, q := range []string{
		`CREATE TABLE tsql (id bigint PRIMARY KEY, a ` + ts + `, b ` + ts + `, g int)`,
		`INSERT INTO tsql VALUES (1, '2026-10-25 08:05:09.250000', '2026-10-25 09:35:10.750000', 1),
			(2, '2026-10-26 23:59:59', NULL, 1), (3, '2026-10-25 00:00:00', '2026-10-25 00:00:00', 2)`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	hour, _ := GranularitySql(d, "a", "hour")
	minute, _ := GranularitySql(d, "a", "minute")
	dow, _ := GranularitySql(d, "a", "day_of_week")
	hn, _ := GranularitySql(d, "a", "hour_number")
	q := `SELECT id, ` + TimestampTextSql(d, hour) + ` AS h, ` + TimestampTextSql(d, minute) + ` AS m, ` +
		d.TextOf(dow, nil) + ` AS dw, LPAD(` + d.TextOf(hn, nil) + `, 2, '0') AS hh, ` +
		`ROUND(` + SecondsBetweenSql(d, "a", "COALESCE(b, "+TimestampParamSql(d)+")") + `, 3) AS secs FROM tsql ORDER BY id`
	ds, err := o.Query(q, "2026-10-27 00:00:00.5")
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	var got []string
	for ds.First(); !ds.Eof(); ds.Next() {
		f := ds.FieldByName
		got = append(got, strings.Join([]string{f("id").AsString(), f("h").AsString(), f("m").AsString(), f("dw").AsString(), f("hh").AsString(),
			trimNum(f("secs").AsString())}, "|"))
	}
	want := []string{
		"1|2026-10-25 08:00:00|2026-10-25 08:05:00|0|08|5401.5",
		"2|2026-10-26 23:00:00|2026-10-26 23:59:00|1|23|1.5", // 23:59:59 → 次日 00:00:00.5
		"3|2026-10-25 00:00:00|2026-10-25 00:00:00|0|00|0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("得\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	ds, err = o.Query(`SELECT g, ` + StringAggOrderedSql(d, "id", " - ", "id DESC") + ` AS s FROM tsql GROUP BY g ORDER BY g`)
	if err != nil {
		t.Fatal(err)
	}
	var aggs []string
	for ds.First(); !ds.Eof(); ds.Next() {
		aggs = append(aggs, ds.FieldByName("s").AsString())
	}
	if strings.Join(aggs, ";") != "2 - 1;3" {
		t.Errorf("StringAggOrderedSql = %v", aggs)
	}
}

func trimNum(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}
