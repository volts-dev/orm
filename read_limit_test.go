package orm

import (
	stdErrors "errors"
	"fmt"
	"path/filepath"
	"testing"

	ormerr "github.com/volts-dev/orm/errors"
)

/*
读取上限的三件事。

DefaultLimit=500 当初是为了"别让人无意中扫全表"，但实测下来它拦错了对象：

  - Search() 压根没有默认上限，枚举整张表的 id 一直放行——想扫表的人一直扫得到；
  - 被拦的全是**已经被 WHERE 圈住**的查询，270 处 Limit(-1) 无一例外是这种形状；
  - 最糟的是关系字段的子读取：调用方**没有任何 API** 能把 limit 传进去，
    而那是一次整批共享的查询，截断的结果不是"少几行"，是整条记录的关系字段空掉。
*/

type rlOrder struct {
	TModel  `table:"name('rl_order')"`
	Id      int64  `field:"pk autoincr"`
	Name    string `field:"varchar(64)"`
	LineIds []any  `field:"one2many(rl_line,order_id)"`
}

type rlLine struct {
	TModel  `table:"name('rl_line')"`
	Id      int64  `field:"pk autoincr"`
	OrderId int64  `field:"many2one(rl_order)"`
	Label   string `field:"varchar(64)"`
}

// ---------- 第 1 层：内部子读取不设上限 ----------

// o2m 子读取是**一次**查询取回整批父记录的明细
// （`WHERE order_id IN (全部父 id) LIMIT ?`），所以上限是整批共享的。
//
// 修复前真库实测：20 张单 × 40 行 = 800 行，读回来只有 500 行，
// **其中 7 张单一行明细都没有**——不是末尾被截断，是整条记录的关系字段整个空掉，
// 谁空取决于数据库返回顺序。无错误、无告警，调用方也没有任何办法传 limit 进去。
func TestRead_O2MSubReadIsNotCapped(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "rl.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(rlOrder), new(rlLine)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}

	// 明细总数必须**超过** DefaultLimit，否则这条用例什么都证明不了
	const orders, linesPer = 20, 40
	if orders*linesPer <= DefaultLimit {
		t.Fatalf("用例失效：%d 行没有超过 DefaultLimit=%d", orders*linesPer, DefaultLimit)
	}

	orderIds := make([]any, 0, orders)
	for i := 0; i < orders; i++ {
		ids, err := o.Model("rl.order").Create(map[string]any{"name": fmt.Sprintf("SO%03d", i)})
		if err != nil {
			t.Fatal(err)
		}
		orderIds = append(orderIds, ids[0])
		for j := 0; j < linesPer; j++ {
			if _, err := o.Model("rl.line").Create(map[string]any{
				"order_id": ids[0], "label": fmt.Sprintf("L%03d-%02d", i, j)}); err != nil {
				t.Fatal(err)
			}
		}
	}

	rs, err := o.Model("rl.order").Classic().Ids(orderIds...).Read()
	if err != nil {
		t.Fatal(err)
	}
	total, empty := 0, 0
	rs.First()
	for !rs.Eof() {
		n := 0
		if v, ok := rs.Record().GetByField("line_ids").([]any); ok {
			n = len(v)
		}
		total += n
		if n == 0 {
			empty++
		}
		rs.Next()
	}
	if empty != 0 {
		t.Errorf("%d 张单的明细整个空掉了 —— 整批共享的上限把记录清空了", empty)
	}
	if total != orders*linesPer {
		t.Errorf("o2m 应回填 %d 行，实得 %d 行 —— 子读取被默认上限截断",
			orders*linesPer, total)
	}
}

// ---------- 第 2 层：隐式截断要出声 ----------

func TestIsImplicitlyTruncated(t *testing.T) {
	cases := []struct {
		count, limit int64
		implicit     bool
		want         bool
		why          string
	}{
		{500, 500, true, true, "顶到隐式上限：很可能丢了数据"},
		{499, 500, true, false, "没顶到上限，结果是完整的"},
		{20, 20, false, false, "调用方自己写的 Limit(20)，拿到 20 条正是他要的"},
		{0, 500, true, false, "空结果集"},
		{100, -1, true, false, "Limit(-1) 不产生上限"},
		{100, 0, true, false, "没有上限"},
	}
	for _, c := range cases {
		if got := isImplicitlyTruncated(c.count, c.limit, c.implicit); got != c.want {
			t.Errorf("isImplicitlyTruncated(%d,%d,%v)=%v，期望 %v —— %s",
				c.count, c.limit, c.implicit, got, c.want, c.why)
		}
	}
}

// ---------- 第 4 层：无条件读取的守卫 ----------

type rlRec struct {
	TModel `table:"name('rl_rec')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
}

func newRlOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "rlrec.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(rlRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if _, err := o.Model("rl.rec").Create(map[string]any{"name": "a"}); err != nil {
		t.Fatal(err)
	}
	return o
}

// 一条 Ids/Where/Domain 都没有、又没给上限的读取就是扫全表。
// 与写侧那道 ErrUnsafe 守卫同源、同判据（hasCondition），只是挪到读侧。
func TestRead_UnscopedIsRejected(t *testing.T) {
	o := newRlOrm(t)

	if _, err := o.Model("rl.rec").Read(); !stdErrors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("无条件无上限的 Read() 应报 ErrUnsafe，得 %v", err)
	}
	// Search 此前**完全没有**默认上限，那道"防扫全表"从来没覆盖到它。
	if _, _, err := o.Model("rl.rec").Search(); !stdErrors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("无条件无上限的 Search() 应报 ErrUnsafe，得 %v", err)
	}
}

// 四种表态都放行：任意条件、显式上限、Limit(-1)、AllowUnsafe。
func TestRead_ScopedOrExplicitIsAllowed(t *testing.T) {
	o := newRlOrm(t)

	t.Run("Where 条件", func(t *testing.T) {
		if _, err := o.Model("rl.rec").Where("name=?", "a").Read(); err != nil {
			t.Fatalf("带条件的读取不该被拦：%v", err)
		}
	})
	t.Run("Ids 条件", func(t *testing.T) {
		if _, err := o.Model("rl.rec").Ids(int64(1)).Read(); err != nil {
			t.Fatalf("按 id 的读取不该被拦：%v", err)
		}
	})
	t.Run("Domain 条件", func(t *testing.T) {
		if _, err := o.Model("rl.rec").Domain(`[('name','=','a')]`).Read(); err != nil {
			t.Fatalf("带 domain 的读取不该被拦：%v", err)
		}
	})
	t.Run("显式分页", func(t *testing.T) {
		if _, err := o.Model("rl.rec").Limit(10).Read(); err != nil {
			t.Fatalf("显式给了上限不该被拦：%v", err)
		}
	})
	t.Run("Limit(-1) 明确要全部", func(t *testing.T) {
		if _, err := o.Model("rl.rec").Limit(-1).Read(); err != nil {
			t.Fatalf("Limit(-1) 是明确表态，不该被拦：%v", err)
		}
		if _, _, err := o.Model("rl.rec").Limit(-1).Search(); err != nil {
			t.Fatalf("Limit(-1) 的 Search 不该被拦：%v", err)
		}
	})
	t.Run("AllowUnsafe", func(t *testing.T) {
		if _, err := o.Model("rl.rec").AllowUnsafe().Read(); err != nil {
			t.Fatalf("AllowUnsafe() 是明确表态，不该被拦：%v", err)
		}
	})
}

// 钩子注入的条件必须算数 —— 守卫排在 BeforeSession **之后**。
//
// 多租户与行级权限就是这么实现的：vectors 的 BeforeSession 对 OpRead 调
// `session.Where("tenant_id=?", tid)`（core/model/model.go）。守卫要是排在钩子
// 之前，那些条件在它眼里等于不存在，每一次正常的带租户读取都会被拦下。
type rlHooked struct {
	TModel `table:"name('rl_hooked')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
}

// BeforeSession 模拟租户隔离：读取时往会话上追加一个条件。
func (self *rlHooked) BeforeSession(session *TSession) (*TSession, error) {
	if session.Op == OpRead {
		session.Where("name=?", "a")
	}
	return session, nil
}

func TestRead_HookInjectedConditionCounts(t *testing.T) {
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "rlhook.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(rlHooked)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	if _, err := o.Model("rl.hooked").Create(map[string]any{"name": "a"}); err != nil {
		t.Fatal(err)
	}

	// 调用方一个条件都没写，条件完全来自钩子——必须放行。
	rs, err := o.Model("rl.hooked").Read()
	if err != nil {
		t.Fatalf("钩子注入的条件应当算数，守卫必须排在 BeforeSession 之后：%v", err)
	}
	if rs.Count() != 1 {
		t.Fatalf("期望读到 1 条，得 %d", rs.Count())
	}
}
