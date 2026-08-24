package orm

import (
	"testing"

	_ "github.com/lib/pq"
)

/*
裸 SQL 的 schema 归属。

SetSchema 一直只影响 ORM **自己拼**的 SQL；Exec/Query 收到的字符串里表名已经写死，
裸表名由 postgres 的 search_path 解析，永远落 public。于是同一张表 ORM 写 system、
裸 SQL 读 public，查 0 行、**不报错**。

vectors 记下的真实代价：税额算成 0、组税展不开、property 的 m2m 读恒空；最贵的一次
在 product —— 读关联表拿"当前挂着哪些值"读成空，调用方据此拼 (6,0,全集) 覆盖式写回，
把原有取值全抹掉。读错 schema 会从"读不到"升级成"丢数据"。

下面每条用例都在**两个 schema 里各放一张同名表、内容不同**，据此分辨究竟打到了哪张。
*/

type schemaProbe struct {
	TModel `table:"name('schema_probe')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(32)"`
}

const schemaProbeNS = "orm_schema_test"

func newSchemaOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}

	cleanup := func() {
		o.Exec(`DROP TABLE IF EXISTS public.schema_probe`)
		o.Exec(`DROP SCHEMA IF EXISTS ` + schemaProbeNS + ` CASCADE`)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := o.Exec(`CREATE SCHEMA ` + schemaProbeNS); err != nil {
		t.Skipf("cannot create schema: %v", err)
	}
	// 两个 schema 里各一张同名表，内容不同 —— 唯一能分辨"打到了哪张"的办法。
	for _, q := range []string{
		`CREATE TABLE public.schema_probe (id bigserial primary key, name varchar(32))`,
		`CREATE TABLE ` + schemaProbeNS + `.schema_probe (id bigserial primary key, name varchar(32))`,
		`INSERT INTO public.schema_probe (name) VALUES ('public-row')`,
		`INSERT INTO ` + schemaProbeNS + `.schema_probe (name) VALUES ('scoped-row')`,
	} {
		if _, err := o.Exec(q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	return o
}

// 无 schema 的会话必须一切照旧 —— 落 public。
func TestSchema_RawSqlWithoutSchemaIsUnchanged(t *testing.T) {
	o := newSchemaOrm(t)

	ds, err := o.NewSession().Query(`SELECT name FROM schema_probe`)
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.Record().FieldByName("name").AsString(); got != "public-row" {
		t.Fatalf("无 schema 的会话应落 public，得 %q", got)
	}
}

// 自动提交会话上的裸 Query：这是修复前**静默读错表**的那条路。
func TestSchema_RawQueryFollowsSessionSchema(t *testing.T) {
	o := newSchemaOrm(t)

	sess := o.NewSession()
	defer sess.Close()
	sess.SetSchema(schemaProbeNS)

	ds, err := sess.Query(`SELECT name FROM schema_probe`)
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.Record().FieldByName("name").AsString(); got != "scoped-row" {
		t.Fatalf("裸 Query 应落会话 schema，得 %q —— SetSchema 对 Query 没生效", got)
	}
}

// 自动提交会话上的裸 Exec：修复前它会**改错表**且不报错。
func TestSchema_RawExecFollowsSessionSchema(t *testing.T) {
	o := newSchemaOrm(t)

	sess := o.NewSession()
	defer sess.Close()
	sess.SetSchema(schemaProbeNS)

	if _, err := sess.Exec(`UPDATE schema_probe SET name='touched'`); err != nil {
		t.Fatal(err)
	}

	// 专属 schema 那张被改了
	ds, err := o.Query(`SELECT name FROM ` + schemaProbeNS + `.schema_probe`)
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.Record().FieldByName("name").AsString(); got != "touched" {
		t.Fatalf("目标 schema 的行应当被改，得 %q", got)
	}
	// public 那张必须**纹丝不动** —— 这才是"没打错表"的证明
	ds, err = o.Query(`SELECT name FROM public.schema_probe`)
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.Record().FieldByName("name").AsString(); got != "public-row" {
		t.Fatalf("public 的行被误改成 %q —— 裸 Exec 打错了 schema", got)
	}
}

// 事务里同理（走 SET LOCAL，提交后自动还原）。
func TestSchema_RawSqlInTransaction(t *testing.T) {
	o := newSchemaOrm(t)

	sess := o.NewSession()
	defer sess.Close()
	sess.SetSchema(schemaProbeNS)
	if err := sess.Begin(); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	ds, err := sess.Query(`SELECT name FROM schema_probe`)
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.Record().FieldByName("name").AsString(); got != "scoped-row" {
		t.Fatalf("事务内的裸 Query 应落会话 schema，得 %q", got)
	}
	if err := sess.Commit(); err != nil {
		t.Fatal(err)
	}
}

// ★ 最重要的一条：search_path 绝不能泄漏到连接池上。
//
// 用会话级 SET（而不是 SET LOCAL）就会留在连接上，下一个借到它的请求继承这个
// schema —— 那是**跨租户串数据**，比原来的 bug 严重得多。这里连着跑若干次无
// schema 的查询，必须每次都落 public。
func TestSchema_SearchPathDoesNotLeakToPool(t *testing.T) {
	o := newSchemaOrm(t)

	scoped := o.NewSession()
	defer scoped.Close()
	scoped.SetSchema(schemaProbeNS)
	if _, err := scoped.Query(`SELECT name FROM schema_probe`); err != nil {
		t.Fatal(err)
	}

	// 连续多次，尽量借到刚才那条连接
	for i := 0; i < 20; i++ {
		plain := o.NewSession()
		ds, err := plain.Query(`SELECT name FROM schema_probe`)
		plain.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := ds.Record().FieldByName("name").AsString(); got != "public-row" {
			t.Fatalf("第 %d 次：无 schema 的会话读到了 %q —— search_path 泄漏到了连接池，这是跨租户串数据", i, got)
		}
	}
}

// 已经显式限定过的 SQL 不受影响：schema 优先、public 兜底，两者不冲突。
func TestSchema_ExplicitlyQualifiedStillWins(t *testing.T) {
	o := newSchemaOrm(t)

	sess := o.NewSession()
	defer sess.Close()
	sess.SetSchema(schemaProbeNS)

	ds, err := sess.Query(`SELECT name FROM public.schema_probe`)
	if err != nil {
		t.Fatal(err)
	}
	if got := ds.Record().FieldByName("name").AsString(); got != "public-row" {
		t.Fatalf("显式写了 public. 的查询必须照打 public，得 %q", got)
	}
}
