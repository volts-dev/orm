package orm

import (
	"path/filepath"
	"testing"
)

/*
复用的会话（NewSession / 事务）上，上一条语句的子句不能漏进下一条。

Init() 原来只复位 domain/Ids/Fields/Limit/Sets 这些，FuncsClause、GroupByClause、
SortClauses、OnConflict、UseCascade 从不复位：

  - Count() 经 Funcs("count") 留下 FuncsClause，紧接着的 Read 去 SELECT 一个不存在的列
    （sqlite: `no such column: count`）——多语句事务里 Count 之后的任何读都 500；
  - 带 OnConflict 的 Create 之后，下一条普通 Create 静默继承 ON CONFLICT 语义。
*/

type srItem struct {
	TModel `table:"name('sr_item')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
	Age    int    `field:"int()"`
}

func setupSr(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "sr.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SyncModel("", new(srItem)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := o.Model("sr.item").Create(map[string]any{"name": "n", "age": i}); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

func TestStatementReset_CountThenRead(t *testing.T) {
	o := setupSr(t)
	sess := o.NewSession()
	defer sess.Close()

	n, err := sess.Model("sr.item").Count()
	if err != nil || n != 3 {
		t.Fatalf("Count: %d / %v", n, err)
	}
	if len(sess.Statement.FuncsClause) != 0 {
		t.Fatalf("Count 之后 FuncsClause 应已复位，得到 %v", sess.Statement.FuncsClause)
	}
	rs, err := sess.Model("sr.item").Where("age=?", 1).Read()
	if err != nil {
		t.Fatalf("Count 之后同会话的 Read 失败（上一条的 count 漏进了 SELECT）：%v", err)
	}
	if rs.Count() != 1 {
		t.Fatalf("age=1 应为 1 行，得到 %d", rs.Count())
	}
}

func TestStatementReset_GroupByAndSortDoNotLeak(t *testing.T) {
	o := setupSr(t)
	sess := o.NewSession()
	defer sess.Close()

	if _, err := sess.Model("sr.item").GroupBy("name").Sort("age desc").Limit(-1).Read(); err != nil {
		t.Fatal(err)
	}
	if len(sess.Statement.GroupByClause) != 0 || len(sess.Statement.SortClauses) != 0 {
		t.Fatalf("GroupBy/Sort 应随语句复位，得到 %v / %v", sess.Statement.GroupByClause, sess.Statement.SortClauses)
	}
	rs, err := sess.Model("sr.item").Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	if rs.Count() != 3 {
		t.Fatalf("上一条的 GROUP BY 漏进来了：应 3 行，得到 %d", rs.Count())
	}
}

func TestStatementReset_OnConflictDoesNotLeak(t *testing.T) {
	o := setupSr(t)
	sess := o.NewSession()
	defer sess.Close()

	if _, err := sess.Model("sr.item").OnConflict(&OnConflict{DoNothing: true}).Create(map[string]any{"name": "x", "age": 9}); err != nil {
		t.Fatal(err)
	}
	if sess.Statement.OnConflict != nil {
		t.Fatalf("OnConflict 应随语句复位，下一条 Create 不该静默继承 ON CONFLICT 语义")
	}
	if sess.Statement.UseCascade {
		t.Fatalf("UseCascade 应随语句复位")
	}
}
