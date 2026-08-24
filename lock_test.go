package orm

import (
	stdErrors "errors"
	"path/filepath"
	"strings"
	"testing"

	ormerr "github.com/volts-dev/orm/errors"
)

/*
ForUpdate() 的回归。它此前是**纯装饰**：只把 Statement.IsForUpdate 置 true，
全仓没有一处读它，dialect.ForUpdateSql() 也没有调用点——发出去的是一条普通
SELECT，调用方却以为拿到了行锁。读-改-写之间没有互斥，并发下静默丢更新。

这一组用例锁定四件事：
  - 锁子句真的进了 SQL，且位置合法（LIMIT/OFFSET 之后）；
  - 不在事务里加锁一律报错，而不是发一条没用的 SQL；
  - 聚合查询（Count/Sum/GroupBy）加锁报错，而不是把锁悄悄丢掉；
  - 方言不支持（sqlite）时留下警告并降级，同样不静默。
*/

type LockRec struct {
	TModel `table:"name('lock_rec')"`
	Id     int64  `field:"pk autoincr title('ID')"`
	Name   string `field:"varchar() size(64)"`
	Qty    int    `field:"int()"`
}

func newLockOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "lock.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(LockRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if _, err := o.Model("lock.rec").Create(map[string]any{"name": "a", "qty": 1}); err != nil {
		t.Fatal(err)
	}
	return o
}

// ---------- 方言层：锁子句的文本 ----------

// 通用实现（postgres 走这条）：FOR UPDATE OF <别名> [NOWAIT|SKIP LOCKED]。
// `OF <别名>` 不是可有可无的修饰：读取路径会为 _inherits 字段接 LEFT JOIN 父表，
// 不限定表时 PG 直接以 "FOR UPDATE cannot be applied to the nullable side of an
// outer join" 拒绝整条查询。
func TestLockClause_Generic(t *testing.T) {
	d := &TDialect{}
	cases := []struct {
		lock  *TLock
		alias string
		want  string
	}{
		{nil, `"t"`, ""},
		{&TLock{Mode: LockNone}, `"t"`, ""},
		{&TLock{Mode: LockUpdate}, "", "FOR UPDATE"},
		{&TLock{Mode: LockUpdate}, `"t"`, `FOR UPDATE OF "t"`},
		{&TLock{Mode: LockShare}, `"t"`, `FOR SHARE OF "t"`},
		{&TLock{Mode: LockUpdate, Wait: LockWaitNoWait}, `"t"`, `FOR UPDATE OF "t" NOWAIT`},
		{&TLock{Mode: LockUpdate, Wait: LockWaitSkip}, `"t"`, `FOR UPDATE OF "t" SKIP LOCKED`},
	}
	for _, c := range cases {
		got, err := d.LockClause(c.lock, c.alias)
		if err != nil {
			t.Fatalf("LockClause(%v): %v", c.lock, err)
		}
		if got != c.want {
			t.Fatalf("LockClause(%v, %q) = %q, want %q", c.lock, c.alias, got, c.want)
		}
	}
}

// MySQL 的差异必须收在方言里：没有 PG 那种 `OF <别名>`（8.0.1 前根本不存在），
// 共享锁在 5.7 只有 LOCK IN SHARE MODE。
func TestLockClause_MySQL(t *testing.T) {
	d := &mysql{}
	cases := []struct {
		lock *TLock
		want string
	}{
		{&TLock{Mode: LockUpdate}, "FOR UPDATE"},
		{&TLock{Mode: LockUpdate, Wait: LockWaitNoWait}, "FOR UPDATE NOWAIT"},
		{&TLock{Mode: LockUpdate, Wait: LockWaitSkip}, "FOR UPDATE SKIP LOCKED"},
		{&TLock{Mode: LockShare}, "LOCK IN SHARE MODE"},
		{&TLock{Mode: LockShare, Wait: LockWaitNoWait}, "FOR SHARE NOWAIT"},
	}
	for _, c := range cases {
		// 别名照样传进去：mysql 必须忽略它，拼上就是语法错误。
		got, err := d.LockClause(c.lock, "`t`")
		if err != nil {
			t.Fatalf("LockClause(%v): %v", c.lock, err)
		}
		if got != c.want {
			t.Fatalf("LockClause(%v) = %q, want %q", c.lock, got, c.want)
		}
	}
}

// SQLite 没有行锁也没有 FOR UPDATE 语法，必须报 ErrLockNotSupported 让上层
// 打警告——返回空串了事就又变回"以为锁住了其实没有"。
func TestLockClause_SQLite_ReportsUnsupported(t *testing.T) {
	d := &sqlite{}

	got, err := d.LockClause(nil, `"t"`)
	if err != nil || got != "" {
		t.Fatalf("无锁请求应静默返回空串，得 (%q, %v)", got, err)
	}

	got, err = d.LockClause(&TLock{Mode: LockUpdate}, `"t"`)
	if got != "" {
		t.Fatalf("sqlite 不该产出锁子句，得 %q", got)
	}
	if !stdErrors.Is(err, ormerr.ErrLockNotSupported) {
		t.Fatalf("期望 ErrLockNotSupported，得 %v", err)
	}
}

// ---------- 会话层：三道校验 ----------

// 不在事务里加锁 = 单语句事务，语句一结束锁就释放，等于没锁。
// 必须报错，因为这恰恰是调用方最容易误以为已经安全的形态。
func TestForUpdate_OutsideTransaction_IsRejected(t *testing.T) {
	o := newLockOrm(t)

	_, err := o.Model("lock.rec").Ids(int64(1)).ForUpdate().Read()
	if !stdErrors.Is(err, ormerr.ErrLockOutsideTransaction) {
		t.Fatalf("事务外 ForUpdate().Read() 应报 ErrLockOutsideTransaction，得 %v", err)
	}

	_, err = o.Model("lock.rec").Where("qty=?", 1).ForUpdate().Write(map[string]any{"qty": 2})
	if !stdErrors.Is(err, ormerr.ErrLockOutsideTransaction) {
		t.Fatalf("事务外 ForUpdate().Write() 应报 ErrLockOutsideTransaction，得 %v", err)
	}

	_, _, err = o.Model("lock.rec").Limit(-1).ForUpdate().Search()
	if !stdErrors.Is(err, ormerr.ErrLockOutsideTransaction) {
		t.Fatalf("事务外 ForUpdate().Search() 应报 ErrLockOutsideTransaction，得 %v", err)
	}
}

// 聚合没有可锁的行。宁可报错，也不把调用方要的锁悄悄丢掉。
func TestForUpdate_OnAggregate_IsRejected(t *testing.T) {
	o := newLockOrm(t)

	sess := o.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer sess.Rollback(nil)

	if _, err := sess.Model("lock.rec").ForUpdate().Count(); !stdErrors.Is(err, ormerr.ErrLockNotApplicable) {
		t.Fatalf("ForUpdate().Count() 应报 ErrLockNotApplicable，得 %v", err)
	}
	if _, err := sess.Model("lock.rec").ForUpdate().Sum("qty"); !stdErrors.Is(err, ormerr.ErrLockNotApplicable) {
		t.Fatalf("ForUpdate().Sum() 应报 ErrLockNotApplicable，得 %v", err)
	}
	if _, err := sess.Model("lock.rec").Limit(-1).GroupBy("name").ForUpdate().Read(); !stdErrors.Is(err, ormerr.ErrLockNotApplicable) {
		t.Fatalf("GroupBy().ForUpdate().Read() 应报 ErrLockNotApplicable，得 %v", err)
	}
}

// sqlite 上加锁读要能正常跑完（降级为警告），且 SQL 里不能出现 FOR UPDATE
// ——sqlite 不认这个语法，拼进去整条查询就废了。
func TestForUpdate_SQLite_DegradesWithoutBreakingSQL(t *testing.T) {
	o := newLockOrm(t)

	sess := o.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer sess.Rollback(nil)

	ds, err := sess.Model("lock.rec").Ids(int64(1)).ForUpdate().Read()
	if err != nil {
		t.Fatalf("sqlite 上的 ForUpdate().Read() 应降级执行，得 %v", err)
	}
	if ds == nil || ds.Count() != 1 {
		t.Fatalf("期望读到 1 条，得 %v", ds)
	}
	if sql, _ := sess.LastSQL(); strings.Contains(strings.ToUpper(sql), "FOR UPDATE") {
		t.Fatalf("sqlite 的 SQL 不该含 FOR UPDATE：%s", sql)
	}
}

// 事务内的加锁写：条件更新的"先 SELECT id 再 UPDATE"两步之间加了锁，
// 语义上仍必须与不加锁时一致（改到同样的行、返回同样的影响行数）。
func TestForUpdate_Write_KeepsSemantics(t *testing.T) {
	o := newLockOrm(t)

	sess := o.NewSession()
	defer sess.Close()
	if err := sess.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	n, err := sess.Model("lock.rec").Where("qty=?", 1).ForUpdate().Write(map[string]any{"qty": 9})
	if err != nil {
		t.Fatalf("ForUpdate().Write(): %v", err)
	}
	if n != 1 {
		t.Fatalf("期望影响 1 行，得 %d", n)
	}
	if err := sess.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	ds, err := o.Model("lock.rec").Ids(int64(1)).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.FieldByName("qty").AsInteger(); got != 9 {
		t.Fatalf("qty 应为 9，得 %d", got)
	}
}

// 锁选项只改锁行为，不改结果集。
func TestLockOptions(t *testing.T) {
	if l := newLock(LockUpdate); l.Wait != LockWaitBlock || l.String() != "for update" {
		t.Fatalf("默认应为阻塞等待，得 %s", l)
	}
	if l := newLock(LockUpdate, NoWait()); l.Wait != LockWaitNoWait || l.String() != "for update nowait" {
		t.Fatalf("NoWait() 未生效：%s", l)
	}
	if l := newLock(LockShare, SkipLocked()); l.Wait != LockWaitSkip || l.String() != "for share skip locked" {
		t.Fatalf("SkipLocked() 未生效：%s", l)
	}
	var nilLock *TLock
	if nilLock.IsLocking() {
		t.Fatal("nil 锁不该被当成要加锁")
	}
	if nilLock.String() != "none" {
		t.Fatalf("nil 锁的 String() 应为 none，得 %s", nilLock)
	}
}
