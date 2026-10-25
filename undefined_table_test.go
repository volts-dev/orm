package orm

import (
	stdErrors "errors"
	"path/filepath"
	"testing"

	ormerr "github.com/volts-dev/orm/errors"
)

// 表不存在在三个库上都是 ErrUndefinedTable（MySQL 兼容 P4，2026-10-25）：上层「表还没建就当没有」的判断
// 此前各自匹配驱动原文，PG 是 does not exist、MySQL 是 doesn't exist，MySQL 上一律没认出来。
func TestMapError_UndefinedTable(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "undef.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	s := o.NewSession()
	defer s.Close()
	if _, err := s.Query("SELECT 1 FROM no_such_table_here"); !stdErrors.Is(err, ormerr.ErrUndefinedTable) {
		t.Errorf("%s 上查不存在的表：%v，想要 ErrUndefinedTable", o.Dialect().DBType(), err)
	}
	if _, err := s.Exec("DELETE FROM no_such_table_here"); !stdErrors.Is(err, ormerr.ErrUndefinedTable) {
		t.Errorf("%s 上写不存在的表：%v，想要 ErrUndefinedTable", o.Dialect().DBType(), err)
	}
}
