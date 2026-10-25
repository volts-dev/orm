package orm

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// 按目录读列、按类型取文本、快照事务：PG 与 MySQL 给出同一套结果（跨库搬数据，2026-10-25，见 catalog.go）。

type CatRec struct {
	TModel  `table:"name('cat_rec')"`
	Id      int64     `field:"pk autoincr"`
	Active  bool      `field:"bool()"`
	Name    string    `field:"varchar(32)"`
	Note    string    `field:"text()"`
	Weight  float64   `field:"float()"`
	Seen    time.Time `field:"datetime()"`
	Birth   time.Time `field:"date()"`
	Raw     []byte    `field:"binary()"`
	Payload string    `field:"json()"`
}

func TestCatalogColumns_SameTypesOnBothDatabases(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "cat.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.SyncModel("", new(CatRec)); err != nil {
		t.Fatal(err)
	}
	s := o.NewSession()
	defer s.Close()
	schema := "public"
	if o.Dialect().DBType() == "mysql" {
		schema = o.config.DataSource.DbName
	}
	cols, err := s.CatalogColumns(schema)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range cols {
		if c.Table == "cat_rec" {
			got[c.Column] = c.Type
		}
	}
	for col, want := range map[string]string{
		"id": "int8", "active": "bool", "name": "varchar", "note": "text", "weight": "float8",
		"seen": "timestamp", "birth": "date", "raw": "bytea", "payload": "jsonb",
	} {
		if got[col] != want {
			t.Errorf("%s 的类型 = %q，want %q（全部：%v）", col, got[col], want, got)
		}
	}

	if _, err := o.Exec(`INSERT INTO cat_rec (id, active, name, note, weight, seen, birth, raw, payload)
		VALUES (1, ?, 'n', 'x', 0.1, '2026-10-23 01:02:03.456789', '2001-02-03', ?, '{"k": [1, 2]}')`, true, []byte{0, 0xff, 0x10}); err != nil {
		t.Fatal(err)
	}
	for col, want := range map[string]string{
		"active": "true", "raw": "00ff10", "weight": "0.1", "seen": "2026-10-23 01:02:03.456789", "birth": "2001-02-03", "id": "1",
	} {
		ds, err := s.Query(`SELECT ` + o.ColumnTextSql(o.dialect.Quoter().Quote(col), got[col]) + ` AS v FROM cat_rec WHERE id = 1`)
		if err != nil {
			t.Fatalf("%s: %v", col, err)
		}
		if v := ds.FieldByName("v").AsString(); v != want {
			t.Errorf("%s 的文本 = %q，want %q", col, v, want)
		}
	}
}

func TestBeginTx_RepeatableReadIsASnapshot(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "snap.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	if _, err := o.Exec(`CREATE TABLE snap_rec (id bigint PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Exec(`INSERT INTO snap_rec VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	s := o.NewSession()
	defer s.Close()
	if err := s.BeginTx(&sql.TxOptions{Isolation: sql.LevelRepeatableRead}); err != nil {
		t.Fatal(err)
	}
	defer s.Rollback(nil)
	count := func() int64 {
		ds, err := s.Query(`SELECT count(*) AS n FROM snap_rec`)
		if err != nil {
			t.Fatal(err)
		}
		return ds.FieldByName("n").AsInteger()
	}
	if n := count(); n != 1 {
		t.Fatalf("开始时 %d 行", n)
	}
	if _, err := o.Exec(`INSERT INTO snap_rec VALUES (2)`); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("快照事务里看见了之后别的连接插的行：%d 行", n)
	}
}
