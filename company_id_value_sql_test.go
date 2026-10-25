package orm

import (
	"path/filepath"
	"strings"
	"testing"
)

// CompanyIdValueSql：按公司区分的 many2one 在「每行不同的公司」里的 id，PG 与 MySQL 取出同样的值。
func TestCompanyIdValueSql_SameValuesOnPGAndMySQL(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "cid.db"))))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	jsonType := "jsonb"
	if o.Dialect().DBType() == "mysql" {
		jsonType = "json"
	}
	for _, q := range []string{
		`CREATE TABLE cid_rec (id bigint PRIMARY KEY, company_id bigint, v ` + jsonType + `)`,
		`INSERT INTO cid_rec VALUES
			(1, 1, '{"1": 5, "0": 3}'),
			(2, 2, '{"1": 5, "0": 3}'),
			(3, 1, '{"1": null, "0": 3}'),
			(4, 1, '{"0": 3}'),
			(5, 1, NULL),
			(6, 1, '{"1": "x", "0": 3}'),
			(7, 7, '{"7": 2114394690179059712}'),
			(8, 9, '{}')`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	expr, err := CompanyIdValueSql(o.Dialect(), "r.v", "r.company_id")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := o.Query(`SELECT r.id AS id, ` + expr + ` AS cid FROM cid_rec r ORDER BY r.id`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for ds.First(); !ds.Eof(); ds.Next() {
		got = append(got, ds.FieldByName("id").AsString()+":"+ds.FieldByName("cid").AsString())
	}
	// 1 本公司有值；2 回落到 "0"；3 本公司格子是 JSON null：0，不回落；4 回落；5 整列空；6 不是数字：0；7 雪花 id 不丢精度；8 空对象。
	if want := "1:5 2:3 3:0 4:3 5:0 6:0 7:2114394690179059712 8:0"; strings.Join(got, " ") != want {
		t.Errorf("得 %s\nwant %s", strings.Join(got, " "), want)
	}
}
