package orm

import "testing"

// DropNotNull 只看库里现在的列（模型里没有这个字段也行）；MySQL 上 MODIFY 要把类型、默认值原样带上。
func TestDropNotNull_KeepsTypeAndDefault(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, ":memory:")))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	for _, q := range []string{
		`CREATE TABLE dnn_probe (id bigint PRIMARY KEY, c varchar(20) NOT NULL DEFAULT 'it''s', n int NOT NULL)`,
		`INSERT INTO dnn_probe (id, c, n) VALUES (1, 'a', 1)`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s := o.NewSession()
	defer s.Close()
	for _, col := range []string{"c", "c", "nope"} { // 第二次：已经可空；第三次：列不存在——都什么都不做
		if err := s.DropNotNull("", "dnn_probe", col); err != nil {
			t.Fatalf("DropNotNull(%s): %v", col, err)
		}
	}
	if _, err := s.Exec(`INSERT INTO dnn_probe (id, c, n) VALUES (2, NULL, 2)`); err != nil {
		t.Fatalf("放开之后 c 还是插不进 NULL: %v", err)
	}
	if _, err := s.Exec(`INSERT INTO dnn_probe (id, n) VALUES (3, 3)`); err != nil {
		t.Fatal(err)
	}
	ds, err := s.Query(`SELECT c FROM dnn_probe WHERE id = 3`)
	if err != nil || ds.FieldByName("c").AsString() != "it's" {
		t.Fatalf("默认值没带上：%v %v", err, ds.Record())
	}
	if _, err := s.Exec(`INSERT INTO dnn_probe (id, c) VALUES (4, 'x')`); err == nil {
		t.Fatal("没放开的列 n 不该变")
	}
}
