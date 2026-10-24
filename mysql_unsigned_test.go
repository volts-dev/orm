package orm

import (
	"testing"
)

// 库里有别人建的、带无符号整数列的表（MySQL 自带的 mysql 库就有 int / smallint / bigint unsigned）：
// New 反查时曾报 unknown colType UNSIGNED INT，整个 ORM 起不来。只有 UNSIGNED TINYINT 在类型表里。
func TestMySQL_IntrospectsUnsignedColumns(t *testing.T) {
	skipUnlessDialect(t, "mysql")
	ds := testDataSource(t, "")
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := o.Exec("CREATE TABLE foreign_unsigned (id int unsigned NOT NULL PRIMARY KEY, a smallint unsigned, b mediumint unsigned, c bigint unsigned, d tinyint unsigned)"); err != nil {
		t.Fatal(err)
	}
	o.Close()

	o, err = New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("库里有无符号列时 New 失败: %v", err)
	}
	defer o.Close()
	s := o.NewSession()
	defer s.Close()
	m, err := o.GetModel("foreign_unsigned")
	if err != nil || m == nil {
		t.Fatalf("反查出的模型: %v %v", m, err)
	}
	for col, want := range map[string]string{"id": UnsignedInt, "a": UnsignedSmallInt, "b": UnsignedMediumInt, "c": UnsignedBigInt, "d": UnsignedTinyInt} {
		if f := m.GetFieldByName(col); f == nil || f.SQLType().Name != want {
			t.Errorf("%s 反查成 %v，想要 %s", col, f, want)
		}
	}
}
