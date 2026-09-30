package orm

import (
	stdErrors "errors"
	"strings"
	"testing"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
	_ "modernc.org/sqlite"
)

// 连接串：专门的字段都要落到串里，Host 不带端口时补上 Port。
func TestDataSource_PostgresConnString(t *testing.T) {
	cases := []struct {
		name string
		ds   TDataSource
		want string
	}{
		{
			// 本仓一直这样写：Host 自带端口。拼出来的串必须与改动前一字不差
			// （除了查询部分只有 sslmode 一项）。
			name: "host 自带端口，什么都不加",
			ds:   TDataSource{DbType: "postgres", DbName: "vs", Host: "localhost:5432", SSLMode: "disable", UserName: "postgres", Password: "p@ss"},
			want: "postgres://postgres:p%40ss@localhost:5432/vs?sslmode=disable",
		},
		{
			name: "host 不带端口时用 Port（此前被忽略，恒连 5432）",
			ds:   TDataSource{DbType: "postgres", DbName: "vs", Host: "db.internal", Port: "6432", SSLMode: "disable", UserName: "u"},
			want: "postgres://u:@db.internal:6432/vs?sslmode=disable",
		},
		{
			name: "host 不带端口、Port 也没给：补默认的 5432",
			ds:   TDataSource{DbType: "postgres", DbName: "vs", Host: "db.internal", SSLMode: "disable", UserName: "u"},
			want: "postgres://u:@db.internal:5432/vs?sslmode=disable",
		},
		{
			name: "应用名、建连超时、二进制参数、额外参数",
			ds: TDataSource{DbType: "postgres", DbName: "vs", Host: "localhost:6432", SSLMode: "disable", UserName: "u",
				ApplicationName: "hr svc/1", Timeout: 1500 * time.Millisecond, BinaryParameters: true,
				Params: map[string]string{"target_session_attrs": "read-write", "sslmode": "require"}},
			// 键按字典序；connect_timeout 按秒向上取整；专门字段盖过 Params 里的同名键。
			want: "postgres://u:@localhost:6432/vs?application_name=hr+svc%2F1&binary_parameters=yes&connect_timeout=2&sslmode=disable&target_session_attrs=read-write",
		},
	}
	for _, c := range cases {
		got, err := c.ds.toString()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func newPooledOrm(t *testing.T, opts ...Option) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(append([]Option{WithDataSource(ds)}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { o.Close() })
	return o
}

// New() 把池参数套到连接池上，且在第一次查库之前——反查已经在用这个池了。
func TestNew_AppliesPoolConfig(t *testing.T) {
	o := newPooledOrm(t,
		WithMaxOpenConns(4), WithMaxIdleConns(3),
		WithConnMaxLifetime(time.Hour), WithConnMaxIdleTime(time.Minute),
		WithAcquireTimeout(7*time.Second),
	)
	st := o.PoolStats()
	if st.MaxOpenConnections != 4 {
		t.Errorf("MaxOpenConnections = %d, want 4", st.MaxOpenConnections)
	}
	if st.AcquireTimeout != 7*time.Second {
		t.Errorf("AcquireTimeout = %s, want 7s", st.AcquireTimeout)
	}
	if st.GatePeakInUse < 1 {
		t.Errorf("启动期的查库没有经过闸（峰值为 %d）——池参数套晚了", st.GatePeakInUse)
	}

	// 没设的保持标准库默认：不设上限、没有闸。
	plain := newPooledOrm(t)
	if st := plain.PoolStats(); st.MaxOpenConnections != 0 || st.GatePeakInUse != 0 {
		t.Errorf("没设池参数却有了上限: %+v", st)
	}

	// 运行期改：Set* 同时更新 Config 与池。
	plain.SetMaxOpenConns(2)
	plain.SetAcquireTimeout(time.Second)
	if st := plain.PoolStats(); st.MaxOpenConnections != 2 || st.AcquireTimeout != time.Second {
		t.Errorf("Set* 没生效: %+v", st)
	}
	if plain.Config().MaxOpenConns != 2 || plain.Config().AcquireTimeout != time.Second {
		t.Errorf("Set* 没回写 Config: %+v", plain.Config())
	}
}

// 池满时，会话层拿到的是认得出的 ErrPoolTimeout，而且是在上限之内拿到的——
// 这是"一个请求握着事务又要第二条连接"那种互等的出口。
func TestSession_PoolExhaustedSurfacesAsErrPoolTimeout(t *testing.T) {
	o := newPooledOrm(t, WithMaxOpenConns(1), WithAcquireTimeout(100*time.Millisecond))
	if _, err := o.SyncModel("", new(BenchModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	holder := o.NewSession()
	defer holder.Close()
	if err := holder.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	type result struct {
		what string
		err  error
	}
	done := make(chan result, 3)
	go func() {
		_, err := o.Model("bench.model").Limit(-1).Read()
		done <- result{"自动提交的读", err}
		_, err = o.Model("bench.model").Create(map[string]any{"name": "x", "age": 1})
		done <- result{"自动提交的写", err}
		s := o.NewSession()
		err = s.Begin()
		s.Close()
		done <- result{"第二个事务的 Begin", err}
	}()
	for i := 0; i < 3; i++ {
		select {
		case r := <-done:
			if !stdErrors.Is(r.err, ormerr.ErrPoolTimeout) {
				t.Errorf("%s: want ErrPoolTimeout, got %v", r.what, r.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("池满时会话层挂住了，没有在上限内返回")
		}
	}

	// 占着连接的事务结束后，同样的操作恢复正常。
	if err := holder.Rollback(nil); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if _, err := o.Model("bench.model").Limit(-1).Read(); err != nil {
		t.Fatalf("连接还回后仍读不了: %v", err)
	}
}

// SyncModel 开不了事务时要报错，而不是悄悄退成"每条 DDL 各自提交"。
func TestSyncModel_BeginFailureIsReported(t *testing.T) {
	o := newPooledOrm(t, WithMaxOpenConns(1), WithAcquireTimeout(80*time.Millisecond))

	holder := o.NewSession()
	defer holder.Close()
	if err := holder.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	errc := make(chan error, 1)
	go func() {
		_, err := o.SyncModel("", new(BenchModel))
		errc <- err
	}()
	select {
	case err := <-errc:
		if !stdErrors.Is(err, ormerr.ErrPoolTimeout) {
			t.Fatalf("want ErrPoolTimeout, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SyncModel 挂住了")
	}
}

// 放开超时只对事务有意义；没有这类超时的方言（sqlite）上是空操作，不报错。
func TestRelaxTimeouts_DialectAndTransactionRules(t *testing.T) {
	pg := QueryDialect("postgres")
	if pg == nil {
		t.Fatal("postgres dialect not registered")
	}
	relax, restore := pg.StatementTimeoutSql(true), pg.StatementTimeoutSql(false)
	for _, want := range []string{"SET LOCAL statement_timeout = 0", "SET LOCAL lock_timeout = 0"} {
		if !strings.Contains(relax, want) {
			t.Errorf("relax = %q, 缺 %q", relax, want)
		}
	}
	// 还原必须是 TO DEFAULT：默认值配在库/角色上，这里不该写死一个数。
	if strings.Count(restore, "TO DEFAULT") != 2 || strings.Contains(relax, "SET statement") {
		t.Errorf("restore = %q，应当把两项都还原成 DEFAULT", restore)
	}
	// 绝不能是会话级 SET：那会留在池化的连接上，下一个借到它的请求就没有超时了。
	for _, stmt := range strings.Split(relax+";"+restore, ";") {
		if s := strings.TrimSpace(stmt); s != "" && !strings.HasPrefix(s, "SET LOCAL ") {
			t.Errorf("出现了非 SET LOCAL 的语句: %q", s)
		}
	}

	o := newPooledOrm(t)
	s := o.NewSession()
	defer s.Close()
	if err := s.RelaxTimeouts(); err != nil {
		t.Fatalf("sqlite 上应当是空操作: %v", err)
	}
	if err := s.RestoreTimeouts(); err != nil {
		t.Fatalf("sqlite 上应当是空操作: %v", err)
	}
}
