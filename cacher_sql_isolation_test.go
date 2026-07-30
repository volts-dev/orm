package orm

import (
	"testing"

	_ "modernc.org/sqlite"
)

// CacheModel 用于 SQL 结果缓存的隔离性验证。
type CacheModel struct {
	TModel `table:"name('cache_model')"`
	Id     int64  `field:"pk autoincr title('ID')"`
	Name   string `field:"varchar() size(64)"`
	Age    int    `field:"int()"`
}

func newCacheOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: ":memory:"}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(CacheModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if _, err := o.Model("cache.model").Create(map[string]any{"name": "alice", "age": 30}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// SQL 结果缓存默认关闭(status map 为空时 Get/PutBySql 直接短路)，必须显式打开
	// 才能测到它。这也是这个 bug 长期休眠的原因。
	o.Cacher.SetStatus(true, "cache_model")
	return o
}

// TestSqlCacheNotPollutedByReader 锁死「缓存交出去的是副本」。
//
// 读路径拿到结果集后会对它做 First()/Classic()，关系字段的 OnRead 还会 SetByField
// 写回记录。若 GetBySql 返回的是缓存里的同一个对象，第一次命中就把缓存内容改掉了，
// 后续请求读到的是被上一个请求改过的数据——而且不报任何错。
func TestSqlCacheNotPollutedByReader(t *testing.T) {
	o := newCacheOrm(t)

	// 第一次读：填充缓存。
	first, err := o.Model("cache.model").Read()
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if first.Count() != 1 {
		t.Fatalf("first read count = %d, want 1", first.Count())
	}

	// 模拟调用方对结果集的正常改写(经典读回填、业务层加工都会这么干)。
	first.First()
	if !first.Record().SetByField("name", "MUTATED") {
		t.Fatal("SetByField 失败")
	}
	first.Record().SetByField("injected", "x") // 顺带改字段表

	// 第二次读：应命中缓存，且必须拿到未被污染的原始数据。
	second, err := o.Model("cache.model").Read()
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if second.Count() != 1 {
		t.Fatalf("second read count = %d, want 1", second.Count())
	}
	second.First()
	if got := second.Record().GetByField("name"); got != "alice" {
		t.Fatalf("缓存被上一次读污染: name = %v, want alice", got)
	}
	if second.HasField("injected") {
		t.Error("缓存的字段表被上一次读污染: 出现了 injected 字段")
	}
}

// 生产侧同理：PutBySql 存的必须是副本，否则调用方存完后继续改同一个对象，
// 改动会直接写进缓存。_readFromDatabase 正是「先 Put 再把它返回给 _read 去改」。
func TestSqlCacheNotPollutedByProducer(t *testing.T) {
	o := newCacheOrm(t)

	first, err := o.Model("cache.model").Read()
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	// _read() 在 Put 之后还会对同一个结果集调 Classic()，这里直接模拟。
	first.Classic(true)
	first.First()
	first.Record().SetByField("age", 999)

	second, err := o.Model("cache.model").Read()
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	second.First()
	if got := second.Record().FieldByName("age").AsInteger(); got != 30 {
		t.Fatalf("生产侧改动泄漏进缓存: age = %d, want 30", got)
	}
}
