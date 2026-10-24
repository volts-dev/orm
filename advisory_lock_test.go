package orm

import (
	stdErrors "errors"
	"path/filepath"
	"testing"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
)

// 事务级咨询锁（advisory_lock.go）：持锁的事务结束前，别人「试着拿」拿不到、「等着拿」一直等；
// 事务一结束（提交或回滚）就放开。MySQL 上是锁表行锁模拟的，语义要与 PG 一样。
func TestAdvisoryXactLock(t *testing.T) {
	if testDBKind() == "sqlite" {
		t.Skip("SQLite 写事务全库串行，咨询锁恒成功（无从验证互斥）")
	}
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "adv.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { o.Close() }) // 先于下面收事务的 Cleanup 登记，后执行（LIFO）
	const key = int64(0x0a11ce)

	// 用例中途失败时也要把还开着的事务收掉：否则测试库的清理（DROP DATABASE）等它的锁，一直挂到超时。
	var open []*TSession
	t.Cleanup(func() {
		for _, s := range open {
			s.Rollback(nil)
			s.Close()
		}
	})
	begin := func() *TSession {
		s := o.NewSession()
		if err := s.Begin(); err != nil {
			t.Fatal(err)
		}
		open = append(open, s)
		return s
	}
	holder := begin()
	if err := holder.AdvisoryXactLock(key); err != nil {
		t.Fatalf("拿锁: %v", err)
	}
	// 同一事务里再拿一次：PG 可重入，MySQL 的行锁同样可重入。
	if ok, err := holder.TryAdvisoryXactLock(key); err != nil || !ok {
		t.Fatalf("持锁的事务自己再试：%v %v", ok, err)
	}

	other := begin()
	if ok, err := other.TryAdvisoryXactLock(key); err != nil || ok {
		t.Fatalf("别人持着时试着拿应当拿不到：ok=%v err=%v", ok, err)
	}
	// 不同的键互不影响。
	if ok, err := other.TryAdvisoryXactLock(key + 1); err != nil || !ok {
		t.Fatalf("另一个键应当拿得到：ok=%v err=%v", ok, err)
	}
	other.Rollback(nil)
	other.Close()

	// 等着拿：持锁的事务提交之后才拿到。
	got := make(chan time.Time, 1)
	errc := make(chan error, 1)
	go func() {
		w := o.NewSession()
		defer w.Close()
		if err := w.Begin(); err != nil {
			errc <- err
			return
		}
		if err := w.AdvisoryXactLock(key); err != nil {
			errc <- err
			return
		}
		got <- time.Now()
		w.Rollback(nil)
	}()
	time.Sleep(300 * time.Millisecond)
	select {
	case <-got:
		t.Fatal("持锁的事务还没结束，等锁的那个就拿到了")
	case err := <-errc:
		t.Fatalf("等锁: %v", err)
	default:
	}
	released := time.Now()
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	holder.Close()
	select {
	case at := <-got:
		if at.Before(released) {
			t.Fatal("拿到锁的时刻早于持锁事务提交")
		}
	case err := <-errc:
		t.Fatalf("等锁: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("持锁的事务提交了，等锁的那个 10 秒内仍没拿到")
	}

	// 回滚同样放锁。
	r := begin()
	if err := r.AdvisoryXactLock(key); err != nil {
		t.Fatal(err)
	}
	r.Rollback(nil)
	r.Close()
	again := begin()
	if ok, err := again.TryAdvisoryXactLock(key); err != nil || !ok {
		t.Fatalf("回滚之后应当拿得到：ok=%v err=%v", ok, err)
	}
}

// 自动提交的会话上拿事务级锁等于没锁：报错，而不是悄悄成功。
func TestAdvisoryXactLock_OutsideTransactionIsRejected(t *testing.T) {
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "adv2.db"))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer o.Close()
	s := o.NewSession()
	defer s.Close()
	if err := s.AdvisoryXactLock(1); !stdErrors.Is(err, ormerr.ErrLockOutsideTransaction) {
		t.Fatalf("事务外拿锁应报 ErrLockOutsideTransaction，得 %v", err)
	}
}
