package orm

import "testing"

func TestDropIndexIfExists(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, ":memory:")))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	for _, q := range []string{
		`CREATE TABLE dix_probe (id bigint PRIMARY KEY, name varchar(20))`,
		`CREATE UNIQUE INDEX "UQE_dix_name" ON dix_probe (name)`,
		`INSERT INTO dix_probe (id, name) VALUES (1, 'a')`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s := o.NewSession()
	defer s.Close()
	if _, err := s.Exec(`INSERT INTO dix_probe (id, name) VALUES (2, 'a')`); err == nil {
		t.Fatal("唯一索引还在时重名应当插不进去")
	}
	for i := 0; i < 2; i++ { // 第二次：已经没了，什么都不做
		if err := s.DropIndexIfExists("", "dix_probe", "UQE_dix_name"); err != nil {
			t.Fatalf("第 %d 次: %v", i+1, err)
		}
	}
	if _, err := s.Exec(`INSERT INTO dix_probe (id, name) VALUES (2, 'a')`); err != nil {
		t.Fatalf("删掉唯一索引之后重名应当插得进去: %v", err)
	}
}
