package orm

import (
	stdErrors "errors"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	ormerr "github.com/volts-dev/orm/errors"
	"github.com/volts-dev/utils"
)

// 连接池与库侧超时在**真的 PostgreSQL** 上的行为。sqlite 测不了这些：它没有
// application_name、没有 statement_timeout，lib/pq 的连接串参数也轮不到它解析。
// 连不上本机的测试库就跳过（与本包其它 *_pg_test.go 同一约定）。

func pgPoolOrm(t testing.TB, tune func(*TDataSource), opts ...Option) *TOrm {
	t.Helper()
	ds := pgDataSource(t, "test_orm_addfield")
	if tune != nil {
		tune(ds)
	}
	o, err := New(append([]Option{WithDataSource(ds)}, opts...)...)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	return o
}

func pgScalar(t *testing.T, s *TSession, query string, args ...any) string {
	t.Helper()
	ds, err := s.Query(query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if ds.Count() == 0 {
		t.Fatalf("%s: 没有返回行", query)
	}
	for _, v := range ds.Data[0].AsMap() {
		return utils.ToString(v)
	}
	return ""
}

// 连接串参数真的到了库上：application_name 在 pg_stat_activity 里看得见，
// connect_timeout 不妨碍正常连接。
func TestPG_ApplicationNameReachesServer(t *testing.T) {
	o := pgPoolOrm(t, func(ds *TDataSource) {
		ds.ApplicationName = "orm-pool-test"
		ds.Timeout = 5 * time.Second
	})
	s := o.NewSession()
	defer s.Close()
	if got := pgScalar(t, s, "SELECT application_name FROM pg_catalog.pg_stat_activity WHERE pid = pg_backend_pid()"); got != "orm-pool-test" {
		t.Fatalf("application_name = %q, want orm-pool-test", got)
	}
}

// 池满时第三个事务在上限内拿到 ErrPoolTimeout，库上这个应用的连接数没有超过上限。
func TestPG_PoolLimitHoldsAgainstRealServer(t *testing.T) {
	o := pgPoolOrm(t, func(ds *TDataSource) { ds.ApplicationName = "orm-pool-limit" },
		WithMaxOpenConns(2), WithAcquireTimeout(200*time.Millisecond))

	a, b := o.NewSession(), o.NewSession()
	defer a.Close()
	defer b.Close()
	if err := a.Begin(); err != nil {
		t.Fatalf("begin a: %v", err)
	}
	if err := b.Begin(); err != nil {
		t.Fatalf("begin b: %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		c := o.NewSession()
		defer c.Close()
		errc <- c.Begin()
	}()
	select {
	case err := <-errc:
		if !stdErrors.Is(err, ormerr.ErrPoolTimeout) {
			t.Fatalf("第三个事务: want ErrPoolTimeout, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("池满时挂住了")
	}

	// 从事务里数（不再占新连接）：这个应用名下的连接不超过 2 条。
	n := pgScalar(t, a, "SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE application_name = 'orm-pool-limit'")
	if utils.ToInt(n) > 2 {
		t.Fatalf("库上有 %s 条连接，超过了上限 2", n)
	}
}

// 库的 statement_timeout 收掉语句时，ORM 给的是 ErrTimeout（而不是一句裸的 pq 错误）。
func TestPG_StatementTimeoutMapsToErrTimeout(t *testing.T) {
	o := pgPoolOrm(t, nil)
	s := o.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := s.Exec("SET LOCAL statement_timeout = '200ms'"); err != nil {
		t.Fatalf("set: %v", err)
	}
	start := time.Now()
	_, err := s.Query("SELECT pg_sleep(5)")
	if !stdErrors.Is(err, ormerr.ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("语句没有被库按时收掉: %s", d)
	}
	s.Rollback(nil)
}

// RelaxTimeouts 放开、RestoreTimeouts 还原成**库上的默认值**（不是 0，也不是放开前
// 事务里临时设的那个数）；自动提交的会话上直接报错。
func TestPG_RelaxAndRestoreTimeouts(t *testing.T) {
	o := pgPoolOrm(t, nil)

	auto := o.NewSession()
	defer auto.Close()
	if err := auto.RelaxTimeouts(); err == nil || !strings.Contains(err.Error(), "transaction") {
		t.Fatalf("自动提交的会话上应当报错，got %v", err)
	}
	sessionDefault := pgScalar(t, auto, "SHOW statement_timeout")

	s := o.NewSession()
	defer s.Close()
	if err := s.Begin(); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := s.Exec("SET LOCAL statement_timeout = '200ms'; SET LOCAL lock_timeout = '150ms'"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err := s.RelaxTimeouts(); err != nil {
		t.Fatalf("RelaxTimeouts: %v", err)
	}
	if got := pgScalar(t, s, "SHOW statement_timeout"); got != "0" {
		t.Fatalf("放开后 statement_timeout = %q, want 0", got)
	}
	if got := pgScalar(t, s, "SHOW lock_timeout"); got != "0" {
		t.Fatalf("放开后 lock_timeout = %q, want 0", got)
	}
	// 放开是真的：一条比 200ms 久的语句跑得完。
	if _, err := s.Query("SELECT pg_sleep(0.5)"); err != nil {
		t.Fatalf("放开后长语句仍被收掉: %v", err)
	}

	if err := s.RestoreTimeouts(); err != nil {
		t.Fatalf("RestoreTimeouts: %v", err)
	}
	if got := pgScalar(t, s, "SHOW statement_timeout"); got != sessionDefault {
		t.Fatalf("还原后 statement_timeout = %q, want 会话默认值 %q", got, sessionDefault)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// SET LOCAL 不留在连接上：事务结束后，同一个池里再取连接看到的仍是默认值。
	after := o.NewSession()
	defer after.Close()
	if got := pgScalar(t, after, "SHOW statement_timeout"); got != sessionDefault {
		t.Fatalf("事务结束后 statement_timeout = %q，放开的设置泄漏到了池化的连接上", got)
	}
}

// binary_parameters=yes 下带参查询照常工作（阶段 2 接连接池代理的前提；更细的类型
// 回归在那一阶段做，这里只锁住"开了不至于连不上、查不了"）。
func TestPG_BinaryParametersSmoke(t *testing.T) {
	o := pgPoolOrm(t, func(ds *TDataSource) { ds.BinaryParameters = true })
	s := o.NewSession()
	defer s.Close()
	if got := pgScalar(t, s, "SELECT (?::int + 1)::text || '-' || ?::text", 41, "中文"); got != "42-中文" {
		t.Fatalf("got %q, want 42-中文", got)
	}
}
