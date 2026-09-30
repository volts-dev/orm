package orm

import (
	"context"
	stdErrors "errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
	"github.com/volts-dev/utils"
)

// 行范围、特权连接池与借用事务在**真的 PostgreSQL** 上的行为（见 row_scope.go、
// privileged.go、session.go 的 Clone）。连不上本机测试库就跳过。

const rowScopeSetting = "app.tenant_id"

// currentScope 读当前语句看到的行范围；没设过是空串。
func currentScope(t *testing.T, s *TSession) string {
	t.Helper()
	return pgScalar(t, s, "SELECT coalesce(current_setting('app.tenant_id', true), '') AS v")
}

// Enforce：范围随语句到库上——自动提交的包隐式事务，显式事务里在下一条语句前补设、
// 换了就重设；都不会留在池化的连接上。
func TestPG_RowScope_EnforceReachesServer(t *testing.T) {
	o := pgPoolOrm(t, nil, WithRowScope(rowScopeSetting, RowScopeEnforce), WithMaxOpenConns(1))

	s := o.NewSession().SetRowScope(42)
	defer s.Close()
	if got := currentScope(t, s); got != "42" {
		t.Fatalf("自动提交：current_setting = %q, want 42", got)
	}

	// 池只有一条连接：下面这个没设范围的会话拿到的正是刚才那条。
	bare := o.NewSession()
	defer bare.Close()
	if got := currentScope(t, bare); got != "" {
		t.Fatalf("范围泄漏到了池化的连接上：%q", got)
	}

	tx := o.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	if got := currentScope(t, tx); got != "" {
		t.Fatalf("还没设范围的事务：%q", got)
	}
	tx.SetRowScope(7) // Begin 之后才定下来的范围
	if got := currentScope(t, tx); got != "7" {
		t.Fatalf("Begin 之后设的范围没补上：%q", got)
	}
	tx.SetRowScope(8)
	if got := currentScope(t, tx); got != "8" {
		t.Fatalf("事务中途换范围没重设：%q", got)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := currentScope(t, bare); got != "" {
		t.Fatalf("事务结束后范围留在了连接上：%q", got)
	}
}

// 范围可以跟着 context 走：会话自己没设时用 context 里的，显式设的优先；派生会话一样。
func TestPG_RowScope_FromContext(t *testing.T) {
	o := pgPoolOrm(t, nil, WithRowScope(rowScopeSetting, RowScopeEnforce))
	ctx := ContextWithRowScope(context.Background(), 11)

	s := o.NewSession().WithContext(ctx)
	defer s.Close()
	if got := currentScope(t, s); got != "11" {
		t.Fatalf("context 里的范围没发出去：%q", got)
	}
	if got := currentScope(t, s.Clone()); got != "11" {
		t.Fatalf("Clone 丢了 context 里的范围：%q", got)
	}
	s.SetRowScope(12)
	if got := currentScope(t, s); got != "12" {
		t.Fatalf("显式设的范围应当优先：%q", got)
	}
	s.ClearRowScope()
	if got := currentScope(t, s); got != "11" {
		t.Fatalf("撤掉显式范围后应当回落到 context：%q", got)
	}
}

type eventLog struct {
	mu  sync.Mutex
	evs []RowScopeEvent
}

func (l *eventLog) add(e RowScopeEvent) {
	l.mu.Lock()
	l.evs = append(l.evs, e)
	l.mu.Unlock()
}

func (l *eventLog) take() []RowScopeEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.evs
	l.evs = nil
	return out
}

// Audit：范围不发给库；没设范围的语句、普通会话上的结构变更各报一条；特权会话不报。
func TestPG_RowScope_AuditReportsWithoutSending(t *testing.T) {
	var log eventLog
	o := pgPoolOrm(t, nil, WithRowScope(rowScopeSetting, RowScopeAudit), WithRowScopeHook(log.add))

	s := o.NewSession().SetRowScope(42)
	defer s.Close()
	if got := currentScope(t, s); got != "" {
		t.Fatalf("Audit 下范围不该发给库：%q", got)
	}
	if evs := log.take(); len(evs) != 0 {
		t.Fatalf("设了范围的会话不该报：%+v", evs)
	}

	bare := o.NewSession()
	defer bare.Close()
	pgScalar(t, bare, "SELECT 1 AS v")
	evs := log.take()
	if len(evs) != 1 || evs[0].Kind != RowScopeUnscoped || evs[0].SQL != "SELECT 1 AS v" || evs[0].InTx {
		t.Fatalf("没设范围的语句：%+v", evs)
	}

	if _, err := s.Exec("CREATE TEMP TABLE row_scope_audit_probe (id int)"); err != nil {
		t.Fatal(err)
	}
	if evs := log.take(); len(evs) != 1 || evs[0].Kind != RowScopeDDL {
		t.Fatalf("普通会话上的结构变更（设了范围也要报）：%+v", evs)
	}

	p := o.NewPrivilegedSession()
	defer p.Close()
	pgScalar(t, p, "SELECT 1 AS v")
	if _, err := p.Exec("CREATE TEMP TABLE row_scope_audit_probe2 (id int)"); err != nil {
		t.Fatal(err)
	}
	if evs := log.take(); len(evs) != 0 {
		t.Fatalf("特权会话不该报：%+v", evs)
	}

	// 派生会话继承范围：Clone 出来的不报。
	c := s.Clone()
	pgScalar(t, c, "SELECT 2 AS v")
	if evs := log.take(); len(evs) != 0 {
		t.Fatalf("Clone 丢了范围：%+v", evs)
	}
}

// Clone 借用来源的事务：看得见同一事务里还没提交的写；它的 Commit / Close 不结束
// 事务；来源回滚时借用者写的也一起回滚；来源结束后借用者退回自动提交。
func TestPG_Clone_BorrowsTransaction(t *testing.T) {
	o := pgPoolOrm(t, nil)
	admin := o.NewSession()
	defer admin.Close()
	mustExec(t, admin, "DROP TABLE IF EXISTS clone_tx_probe")
	mustExec(t, admin, "CREATE TABLE clone_tx_probe (id int PRIMARY KEY)")
	t.Cleanup(func() { o.Exec("DROP TABLE IF EXISTS clone_tx_probe") })

	count := func(s *TSession) int {
		return utils.ToInt(pgScalar(t, s, "SELECT count(*) AS n FROM clone_tx_probe"))
	}

	owner := o.NewSession()
	defer owner.Close()
	if err := owner.Begin(); err != nil {
		t.Fatal(err)
	}
	mustExec(t, owner, "INSERT INTO clone_tx_probe VALUES (1)")

	c := owner.Clone()
	if n := count(c); n != 1 {
		t.Fatalf("借用者看不见同一事务里没提交的写：%d 行", n)
	}
	mustExec(t, c, "INSERT INTO clone_tx_probe VALUES (2)")
	if err := c.Begin(); err != nil { // 已在事务里：空操作
		t.Fatal(err)
	}
	if err := c.Commit(); err != nil { // 借用者的 Commit：空操作
		t.Fatal(err)
	}
	c.Close() // 借用者的 Close：不回滚

	if n := count(admin); n != 0 {
		t.Fatalf("借用者的 Commit 提交了别人的事务：外面看得见 %d 行", n)
	}
	if n := count(owner); n != 2 {
		t.Fatalf("借用者的 Close 回滚了别人的事务：事务里剩 %d 行", n)
	}

	c2 := owner.Clone()
	owner.Rollback(nil)
	if n := count(admin); n != 0 {
		t.Fatalf("来源回滚后还剩 %d 行", n)
	}
	// 来源结束了，借用者退回自动提交而不是往结束了的事务上发语句。
	mustExec(t, c2, "INSERT INTO clone_tx_probe VALUES (3)")
	if n := count(admin); n != 1 {
		t.Fatalf("来源结束后借用者的写没有自动提交：%d 行", n)
	}

	// 来源不在事务里时，Clone 就是普通的自动提交会话，Close 什么都不回滚。
	free := admin.Clone()
	mustExec(t, free, "INSERT INTO clone_tx_probe VALUES (4)")
	free.Close()
	if n := count(admin); n != 2 {
		t.Fatalf("自动提交的克隆：%d 行", n)
	}
}

func mustExec(t *testing.T, s *TSession, q string, args ...any) {
	t.Helper()
	if _, err := s.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// 真的行级安全：普通身份只看得见、只写得进自己范围的行，没设范围就报错；特权身份
// 什么都看得见；结构同步走特权池。这一条要建两个库角色（集群级对象），用完即删。
func TestPG_RowScope_RowLevelSecurityWithTwoRoles(t *testing.T) {
	root := pgPoolOrm(t, nil)
	rs := root.NewSession()
	defer rs.Close()

	const app, owner, pass = "orm_rls_test_app", "orm_rls_test_owner", "orm_rls_test"
	cleanup := func() {
		for _, q := range []string{
			"DROP TABLE IF EXISTS rls_probe",
			"DROP TABLE IF EXISTS rls_synced_probe",
			fmt.Sprintf("DROP OWNED BY %s", app),
			fmt.Sprintf("DROP OWNED BY %s", owner),
			fmt.Sprintf("DROP ROLE IF EXISTS %s", app),
			fmt.Sprintf("DROP ROLE IF EXISTS %s", owner),
		} {
			root.Exec(q) // 角色不存在时 DROP OWNED 报错，忽略
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, q := range []string{
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", app, pass),
		fmt.Sprintf("CREATE ROLE %s LOGIN BYPASSRLS PASSWORD '%s'", owner, pass),
		fmt.Sprintf("GRANT CREATE, USAGE ON SCHEMA public TO %s", owner),
		fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", app),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s", owner, app),
	} {
		mustExec(t, rs, q)
	}

	var log eventLog
	o := pgPoolOrm(t, func(ds *TDataSource) { ds.UserName, ds.Password = app, pass },
		WithRowScope(rowScopeSetting, RowScopeEnforce),
		WithRowScopeHook(log.add),
		// 一条连接：下面"设过又清空的连接"那一支拿到的必然是用过的那条。
		WithMaxOpenConns(1), WithAcquireTimeout(5*time.Second),
		WithPrivilegedDataSource(&TDataSource{DbType: "postgres", Host: "localhost", Port: "5432",
			UserName: owner, Password: pass, DbName: "test_orm_addfield", SSLMode: "disable"}, 2))
	if !o.HasPrivilegedPool() {
		t.Fatal("没开出特权池")
	}

	p := o.NewPrivilegedSession()
	defer p.Close()
	if who := pgScalar(t, p, "SELECT current_user AS u"); who != owner {
		t.Fatalf("特权会话的身份是 %q", who)
	}
	mustExec(t, p, "CREATE TABLE rls_probe (id int PRIMARY KEY, tenant_id bigint NOT NULL)")
	mustExec(t, p, "ALTER TABLE rls_probe ENABLE ROW LEVEL SECURITY")
	mustExec(t, p, "ALTER TABLE rls_probe FORCE ROW LEVEL SECURITY")
	mustExec(t, p, "CREATE POLICY tenant_isolation ON rls_probe USING (tenant_id = current_setting('app.tenant_id')::bigint) WITH CHECK (tenant_id = current_setting('app.tenant_id')::bigint)")
	mustExec(t, p, "INSERT INTO rls_probe VALUES (1, 1), (2, 1), (3, 2)")

	t1 := o.NewSession().SetRowScope(1)
	defer t1.Close()
	if who := pgScalar(t, t1, "SELECT current_user AS u"); who != app {
		t.Fatalf("普通会话的身份是 %q", who)
	}
	// 故意不带租户条件的裸查询：只看得见自己的。
	if n := pgScalar(t, t1, "SELECT count(*) AS n FROM rls_probe"); n != "2" {
		t.Fatalf("范围 1 看见了 %s 行，want 2", n)
	}
	// 写进别人的范围：库拒。
	if _, err := t1.Exec("INSERT INTO rls_probe VALUES (4, 2)"); !stdErrors.Is(err, ormerr.ErrRowScopeViolation) {
		t.Fatalf("写进别的范围应当被拒（ErrRowScopeViolation），got %v", err)
	}
	// 改别人的行：看不见，影响 0 行。
	res, err := t1.Exec("UPDATE rls_probe SET id = id WHERE tenant_id = 2")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 0 {
		t.Fatalf("改到了别的范围的 %d 行", n)
	}

	// 没设范围：大声报错，而不是静默返回空集。两种库原文都要认成 ErrRowScopeMissing：
	// 连接上从没设过（新连接），和设过之后留下空值（刚才 t1 用过的那条）。
	log.take()
	fresh := pgPoolOrm(t, func(ds *TDataSource) { ds.UserName, ds.Password = app, pass },
		WithRowScope(rowScopeSetting, RowScopeEnforce))
	for name, s := range map[string]*TSession{"从没设过的连接": fresh.NewSession(), "设过又清空的连接": o.NewSession()} {
		_, err := s.Query("SELECT count(*) FROM rls_probe")
		s.Close()
		if !stdErrors.Is(err, ormerr.ErrRowScopeMissing) || !strings.Contains(err.Error(), "app.tenant_id") {
			t.Fatalf("%s：没设范围应当报 ErrRowScopeMissing，got %v", name, err)
		}
	}
	if evs := log.take(); len(evs) != 1 || evs[0].Kind != RowScopeUnscoped {
		t.Fatalf("没设范围的语句应当报一条事件：%+v", evs)
	}

	// 事务 + 借用者：借用者跟着来源的范围走。
	tx := o.NewSession().SetRowScope(2)
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		t.Fatal(err)
	}
	mustExec(t, tx, "INSERT INTO rls_probe VALUES (5, 2)")
	if n := pgScalar(t, tx.Clone(), "SELECT count(*) AS n FROM rls_probe"); n != "2" {
		t.Fatalf("借用者在范围 2 里看见 %s 行，want 2（含同事务里没提交的那行）", n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// 特权身份：什么都看得见。
	if n := pgScalar(t, p, "SELECT count(*) AS n FROM rls_probe"); n != "4" {
		t.Fatalf("特权会话看见 %s 行，want 4", n)
	}

	// 普通会话上调 SyncModel：换到特权池去建表，表的属主是特权身份。
	type RlsSyncedProbe struct {
		TModel `table:"name('rls_synced_probe')"`
		Id     int64  `field:"pk autoincr"`
		Name   string `field:"char() size(32)"`
	}
	if _, err := o.NewSession().SyncModel("test", new(RlsSyncedProbe)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if got := pgScalar(t, rs, "SELECT tableowner AS o FROM pg_tables WHERE tablename = 'rls_synced_probe'"); got != owner {
		t.Fatalf("SyncModel 建的表属主是 %q，want %q（没走特权池）", got, owner)
	}
}

type rsTag struct {
	TModel `table:"name('rs_tag')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
}

type rsMain struct {
	TModel `table:"name('rs_main')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32) recname()"`
	TagIds []any  `field:"many2many(rs_tag,rs_main_tag_rel,main_id,tag_id)"`
}

// 关系字段的读取在 ORM 内部另起会话查关联表：那个会话要带上调用方的范围，否则开了
// 行级安全就被库拒（Audit 下表现为一条"没设范围"的事件）。
func TestPG_RowScope_ManyToManyInheritsScope(t *testing.T) {
	var log eventLog
	o := pgPoolOrm(t, nil, WithRowScope(rowScopeSetting, RowScopeAudit), WithRowScopeHook(log.add))
	for _, tb := range []string{"rs_main_tag_rel", "rs_main", "rs_tag"} {
		o.Exec("DROP TABLE IF EXISTS " + tb + " CASCADE")
	}
	t.Cleanup(func() {
		for _, tb := range []string{"rs_main_tag_rel", "rs_main", "rs_tag"} {
			o.Exec("DROP TABLE IF EXISTS " + tb + " CASCADE")
		}
	})
	if _, err := o.SyncModel("", new(rsTag), new(rsMain)); err != nil {
		t.Fatal(err)
	}
	s := o.NewSession().SetRowScope(1)
	defer s.Close()
	t1, err := s.Model("rs.tag").Create(map[string]any{"name": "T1"})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := s.Model("rs.main").Create(map[string]any{"name": "M1", "tag_ids": []any{[]any{6, 0, []any{t1[0]}}}})
	if err != nil {
		t.Fatal(err)
	}
	log.take()

	m, err := o.GetModel("rs.main")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := m.ManyToMany(&TFieldContext{Model: m, Ids: ids, Field: m.GetFieldByName("tag_ids"), Session: s})
	if err != nil {
		t.Fatal(err)
	}
	if ds == nil || ds.Count() != 1 {
		t.Fatalf("关联行没读出来：%v", ds)
	}
	if evs := log.take(); len(evs) != 0 {
		t.Fatalf("关系读取另起的会话丢了调用方的范围：%+v", evs)
	}
}

type RsDlgParent struct {
	TModel `table:"name('rs_dlg_parent')"`
	Id     int64  `field:"pk autoincr title('ID') index"`
	Name   string `field:"varchar() required"`
	City   string `field:"varchar()"`
}

type rsDlgChild struct {
	TModel      `table:"name('rs_dlg_child')"`
	RsDlgParent `field:"relate(ParentId)"`
	ParentId    int64  `field:"one2one(rs_dlg_parent)"`
	Id          int64  `field:"pk autoincr title('ID') index"`
	Name        string `field:"varchar() required"`
}

// 委托继承建子记录时自动建的父记录走 _getModel 另起的会话：它要带上调用方的范围与事务。
func TestPG_RowScope_DelegatedParentInheritsScope(t *testing.T) {
	var log eventLog
	o := pgPoolOrm(t, nil, WithRowScope(rowScopeSetting, RowScopeAudit), WithRowScopeHook(log.add))
	drop := func() {
		for _, tb := range []string{"rs_dlg_child", "rs_dlg_parent"} {
			o.Exec("DROP TABLE IF EXISTS " + tb + " CASCADE")
		}
	}
	drop()
	t.Cleanup(drop)
	if _, err := o.SyncModel("", new(RsDlgParent), new(rsDlgChild)); err != nil {
		t.Fatal(err)
	}
	if err := o.Freeze(context.Background()); err != nil { // 委托继承的字段在 Freeze 时才接上
		t.Fatal(err)
	}
	log.take()

	s := o.NewSession().SetRowScope(3)
	defer s.Close()
	if err := s.Begin(); err != nil {
		t.Fatal(err)
	}
	child, err := o.GetModel("rs.dlg.child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := child.Tx(s).Create(map[string]any{"name": "C1", "city": "Hangzhou"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := pgScalar(t, s, "SELECT count(*) AS n FROM rs_dlg_parent WHERE city = 'Hangzhou'"); n != "1" {
		t.Fatalf("父记录没建出来（用例没走到委托继承那条路）：%s", n)
	}
	for _, e := range log.take() {
		if e.Kind == RowScopeUnscoped {
			t.Fatalf("委托继承的父记录顶着没设范围的会话写：%s", e.SQL)
		}
	}
}
