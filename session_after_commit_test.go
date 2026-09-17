package orm

import (
	"testing"

	_ "github.com/lib/pq"
)

// afterCommitProbe 是 AfterCommit 测试用的表。
type afterCommitProbe struct {
	TModel `table:"name('after_commit_probe')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar()"`
}

func afterCommitOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if _, err := o.Exec(`DROP TABLE IF EXISTS after_commit_probe`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	t.Cleanup(func() { o.Exec(`DROP TABLE IF EXISTS after_commit_probe`) })
	if _, err := o.SyncModel("test", new(afterCommitProbe)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

// visibleFromOtherConnection 用一条**全新的**自动提交会话数行——那正是跨进程
// 通知的对端看到的样子（它不在我们的事务里）。
func visibleFromOtherConnection(t *testing.T, o *TOrm, name string) int {
	t.Helper()
	ds, err := o.NewSession().Model("after_commit_probe").Where("name=?", name).Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ds == nil {
		return 0
	}
	return ds.Count()
}

// TestAfterCommit_RunsAfterTheRowIsVisible 是这个设施存在的理由：回调里另一条
// 连接必须**读得到**事务里刚写下的行。在事务里直接做那件事（发通知让对端回读），
// 对端读到的是一条还不存在的记录。
func TestAfterCommit_RunsAfterTheRowIsVisible(t *testing.T) {
	o := afterCommitOrm(t)

	tx := o.NewSession()
	if err := tx.Begin(); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Model("after_commit_probe").Create(map[string]any{"name": "in-tx"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	ran, seen := false, -1
	tx.AfterCommit(func() {
		ran = true
		seen = visibleFromOtherConnection(t, o, "in-tx")
	})
	if ran {
		t.Fatal("事务还没提交，回调就执行了")
	}
	if n := visibleFromOtherConnection(t, o, "in-tx"); n != 0 {
		t.Fatalf("前提不成立：提交前别的连接已经读得到 %d 行", n)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !ran {
		t.Fatal("提交成功后回调没有执行")
	}
	if seen != 1 {
		t.Fatalf("回调里另一条连接读到 %d 行，应当是 1——回调跑在提交之前了", seen)
	}
}

// TestAfterCommit_AutoCommitRunsImmediately：不在事务里（前台控制器的常态）时，
// 每条语句已经各自提交，回调立即执行。调用方因此不必知道自己是被谁调的。
func TestAfterCommit_AutoCommitRunsImmediately(t *testing.T) {
	o := afterCommitOrm(t)

	s := o.NewSession()
	if _, err := s.Model("after_commit_probe").Create(map[string]any{"name": "auto"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	seen := -1
	s.AfterCommit(func() { seen = visibleFromOtherConnection(t, o, "auto") })
	if seen != 1 {
		t.Fatalf("自动提交会话：回调应立即执行且读得到那一行，得到 %d", seen)
	}
}

// TestAfterCommit_RollbackDiscards：回滚了就不该发生"提交之后"的事——否则那封
// 通知已经发出去了，而它说的那条记录并不存在。
func TestAfterCommit_RollbackDiscards(t *testing.T) {
	o := afterCommitOrm(t)

	tx := o.NewSession()
	if err := tx.Begin(); err != nil {
		t.Fatalf("begin: %v", err)
	}
	ran := false
	tx.AfterCommit(func() { ran = true })
	tx.Rollback(nil)
	if ran {
		t.Fatal("回滚后回调仍然执行了")
	}

	// 同一个会话之后再开事务并提交，不能把上一轮丢弃的回调捡回来执行。
	if err := tx.Begin(); err != nil {
		t.Fatalf("begin#2: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit#2: %v", err)
	}
	if ran {
		t.Fatal("上一轮回滚丢弃的回调在下一轮提交时被执行了")
	}
}

// TestAfterCommit_DerivedSessionSharesTheTx：从派生会话（_getModel 起的那种，
// 业务覆写里"顺带取的另一个模型"就是它）登记的回调，发起 Commit 的原会话必须
// 执行到。挂在 TSession 上而不是 *core.Tx 上的话，这条会静默失效。
func TestAfterCommit_DerivedSessionSharesTheTx(t *testing.T) {
	o := afterCommitOrm(t)

	tx := o.NewSession()
	if err := tx.Begin(); err != nil {
		t.Fatalf("begin: %v", err)
	}
	m, err := tx._getModel("after_commit_probe")
	if err != nil {
		t.Fatalf("_getModel: %v", err)
	}
	derived := m.Records()
	derived.IsAutoCommit = tx.IsAutoCommit
	derived.tx = tx.tx

	ran := false
	derived.AfterCommit(func() { ran = true })
	if ran {
		t.Fatal("派生会话登记的回调在提交前就执行了")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if !ran {
		t.Fatal("派生会话登记的回调，原会话提交后没有执行")
	}

	// 事务已经结束，派生会话手里还是那个 *Tx（标志位是派生时抄的）。此时登记的
	// 回调按终态处理：已提交 → 立即执行，而不是挂在一个再也不会提交的事务上。
	late := false
	derived.AfterCommit(func() { late = true })
	if !late {
		t.Fatal("事务提交之后才登记的回调没有立即执行——它会永远挂着")
	}
}

// TestAfterCommit_PanickingHookDoesNotBreakCommit：事务已经提交了，回调出错不能
// 让 Commit 报失败（调用方会以为没提交而重试，造出第二条记录），也不能拦住后面的回调。
func TestAfterCommit_PanickingHookDoesNotBreakCommit(t *testing.T) {
	o := afterCommitOrm(t)

	tx := o.NewSession()
	if err := tx.Begin(); err != nil {
		t.Fatalf("begin: %v", err)
	}
	second := false
	tx.AfterCommit(func() { panic("boom") })
	tx.AfterCommit(func() { second = true })
	if err := tx.Commit(); err != nil {
		t.Fatalf("回调 panic 让 Commit 报错了: %v", err)
	}
	if !second {
		t.Fatal("前一个回调 panic 拦住了后一个")
	}
}
