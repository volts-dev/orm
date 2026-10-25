package orm

import (
	"path/filepath"
	"testing"
)

// 会话级锁：拿到之后别人拿不到（不等、立即回 false），解锁之后能拿；锁挂在独占的那条连接上，中间池里的连接来回换
// 也不影响（此前媒体迁移随手拿连接加锁 / 解锁，解锁多半落在别的连接上、锁一直挂着）。
func TestTrySessionLock(t *testing.T) {
	skipUnlessDialect(t, "postgres", "mysql")
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "slock.db"))), WithMaxOpenConns(3))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	unlock, ok, err := o.TrySessionLock(42)
	if err != nil || !ok {
		t.Fatalf("第一次拿锁: ok=%v err=%v", ok, err)
	}
	for i := 0; i < 5; i++ {
		if _, err := o.Exec("SELECT 1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok2, err := o.TrySessionLock(42); err != nil || ok2 {
		t.Fatalf("别人持着时应拿不到：ok=%v err=%v", ok2, err)
	}
	unlock()
	unlock2, ok3, err := o.TrySessionLock(42)
	if err != nil || !ok3 {
		t.Fatalf("解锁之后应能拿到：ok=%v err=%v", ok3, err)
	}
	unlock2()
}
