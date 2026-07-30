package orm

import (
	"testing"

	_ "modernc.org/sqlite"
)

// CycleAModel / CycleBModel 互相持有 many2one，构成关系环。
// 现实里这种环很常见：用户→所属公司、公司→负责人（也是用户）。
type CycleAModel struct {
	TModel `table:"name('cycle_a')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar() size(32)"`
	BId    int64  `field:"many2one(cycle_b)"`
}

type CycleBModel struct {
	TModel `table:"name('cycle_b')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar() size(32)"`
	AId    int64  `field:"many2one(cycle_a)"`
}

// TestClassicReadOnCyclicRelationTerminates 锁死「Classic 读遇到关系环会终止」。
//
// 读路径没有任何深度/环路计数器，靠的是一个很不显眼的性质：ManyToOne 的子读取
// （model_request.go 的 `sub.Ids(ids...).Read()`）**不**把 Classic 传下去，于是子会话
// 的 `_read()` 不满足派发条件，comodel 自己的关系字段不再展开——下钻深度恰好一层。
//
// 一旦有人"顺手"给那行补上 `.Classic()`（看起来完全合理：既然是经典读，子记录也该
// 经典读），A→B→A→B… 就会无限递归。栈溢出是 fatal error，直接打死整个测试进程，
// 不是一条能忽略的失败。本用例就是拦这个的。
//
// 实测查询数：2（顶层读 cycle_a + 一次子读 cycle_b），不随环长增加。
func TestClassicReadOnCyclicRelationTerminates(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(CycleAModel), new(CycleBModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	// 先各建一条，再互相指向对方，把环真正闭合。
	aIds, err := o.Model("cycle.a").Create(map[string]any{"name": "a1"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	bIds, err := o.Model("cycle.b").Create(map[string]any{"name": "b1", "a_id": aIds[0]})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if _, err := o.Model("cycle.a").Ids(aIds[0]).Write(map[string]any{"b_id": bIds[0]}); err != nil {
		t.Fatalf("close the cycle: %v", err)
	}

	// 必须返回，且不能爆栈。
	res, err := o.Model("cycle.a").Ids(aIds[0]).Classic().Read()
	if err != nil {
		t.Fatalf("classic read: %v", err)
	}
	if res.Count() != 1 {
		t.Fatalf("count = %d, want 1", res.Count())
	}
	res.First()
	if got := res.Record().GetByField("name"); got != "a1" {
		t.Fatalf("name = %v, want a1", got)
	}
}
