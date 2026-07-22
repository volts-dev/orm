package orm

import (
	"testing"

	_ "github.com/lib/pq"
)

// defChurnProbe 覆盖曾经每次启动误发 SET DEFAULT 的两类默认值：
// 负整数(DB 存 '-2'::integer,解析漏 Trim 引号) 与 空串(DEFAULT '',被 gate 当无默认)。
type defChurnProbe struct {
	TModel `table:"name('def_churn_probe')"`
	Id     int64  `field:"pk autoincr"`
	Neg    int    `field:"int default(-2)"`
	Empty  string `field:"varchar(16) default('')"`
	Zero   int    `field:"int default(0)"`
	Note   string `field:"varchar(32)"`
}

// TestDefaultRoundTripNoChurn 证明"建表→再同步"对带默认值(含负整数/空串)的模型是幂等的：
// 第二次 SyncModel 不得再发任何 DDL(否则就是 dialect 反查默认值与结构体侧口径不一致，
// _alterTable 每次启动重发 ALTER ... SET DEFAULT 的老毛病)。用 metaEpoch(每条 DDL +1)
// 精确计数第二次同步发出的 DDL 条数。
func TestDefaultRoundTripNoChurn(t *testing.T) {
	ds := &TDataSource{DbType: "postgres", Host: "localhost", Port: "5432", UserName: "postgres", Password: "postgres", DbName: "test_orm", SSLMode: "disable"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}

	if _, err := o.Exec(`DROP TABLE IF EXISTS def_churn_probe`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	defer o.Exec(`DROP TABLE IF EXISTS def_churn_probe`)

	// 第一次：建表(会发 DDL)。
	if _, err := o.SyncModel("test", new(defChurnProbe)); err != nil {
		t.Fatalf("SyncModel #1: %v", err)
	}
	epochAfterCreate := o.metaEpoch.Load()

	// 第二次：表已存在，走 _alterTable。默认值若忠实往返,应零 DDL。
	if _, err := o.SyncModel("test", new(defChurnProbe)); err != nil {
		t.Fatalf("SyncModel #2: %v", err)
	}
	if churn := o.metaEpoch.Load() - epochAfterCreate; churn != 0 {
		t.Fatalf("2nd SyncModel emitted %d DDL statement(s), expected 0 — default round-trip churn "+
			"(check parsePgColumn quote-trim / empty-default gate)", churn)
	}
}
