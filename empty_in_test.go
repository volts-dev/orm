package orm

import (
	stdErrors "errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
	ormerr "github.com/volts-dev/orm/errors"
	"github.com/volts-dev/utils"
)

/*
`x IN ()` 恒假，不是"没有这个条件"。

vectors 侧的两处工作坊（module/registry/internal/model/api_user_admin.go idListDomain、
manager/platform_user_ops.go TenantIdsOfUser）记着：本仓 ORM 会把 `in []` 当成
「没有这个条件」整条丢掉，于是权限列表把**全表/全平台**甩给用户。为此他们造了一条
`["id","in",["0"]]` 的假条件。

漏在三个入口：domain.IN()/NotIn() 零参直接 return；TStatement.In()/NotIn() 零参 log
一句后 return；TStatement.Ids() 零 id 什么都不加。现在 IN 空集落成合法叶子
(field, 'IN', <空>) 由 expr 渲染为 FALSE；NOT IN 空集是恒真、AND 的单位元，保持无操作
（有意不落 TRUE 叶子——那会让 hasCondition() 误判"有条件"，绕开 Write/Delete 的
ErrUnsafe 守卫）。
*/

type einItem struct {
	TModel `table:"name('ein_item')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
	Age    int    `field:"int()"`
}

func setupEin(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "ein.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(einItem)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := o.Model("ein.item").Create(map[string]any{"name": fmt.Sprintf("n%d", i), "age": i}); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

func wantRows(t *testing.T, label string, sess *TSession, want int) {
	t.Helper()
	rs, err := sess.Read()
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	if rs.Count() != want {
		t.Fatalf("%s: 应为 %d 行，得到 %d", label, want, rs.Count())
	}
}

func TestEmptyIn_StatementIn(t *testing.T) {
	o := setupEin(t)
	var none []any
	wantRows(t, `.In("id") 零参`, o.Model("ein.item").In("id"), 0)
	wantRows(t, `.In("id", none...)`, o.Model("ein.item").In("id", none...), 0)
	// 有其他条件时同样恒假，而不是"退化成只剩其他条件"
	wantRows(t, `.Where(age>=0).In("id")`, o.Model("ein.item").Where("age>=?", 0).In("id"), 0)
}

func TestEmptyIn_Ids(t *testing.T) {
	o := setupEin(t)
	var none []any
	wantRows(t, `.Ids()`, o.Model("ein.item").Ids(), 0)
	wantRows(t, `.Ids(none...)`, o.Model("ein.item").Ids(none...), 0)
	// vectors export_data 的形状：Ids(ids...).Limit(-1)——原来 ids 为空时读回**全表**
	wantRows(t, `.Ids(none...).Limit(-1)`, o.Model("ein.item").Ids(none...).Limit(-1), 0)
	// 传一个空切片（未展开）同样是零 id
	wantRows(t, `.Ids([]any{})`, o.Model("ein.item").Ids([]any{}), 0)
	// 全 nil 也是零 id
	wantRows(t, `.Ids(nil, nil)`, o.Model("ein.item").Ids(nil, nil), 0)
	// 有条件叠加也恒假
	wantRows(t, `.Where(age>=0).Ids()`, o.Model("ein.item").Where("age>=?", 0).Ids(), 0)
	// 正常点名不受影响
	wantRows(t, `.Ids(1,2)`, o.Model("ein.item").Ids(int64(1), int64(2)), 2)
}

func TestEmptyIn_IdsWriteAndDelete(t *testing.T) {
	o := setupEin(t)
	eff, err := o.Model("ein.item").Ids().Write(map[string]any{"age": 99})
	if eff != 0 {
		t.Fatalf("零 id 的 Write 不该改到任何行，effect=%d", eff)
	}
	if err == nil || !stdErrors.Is(err, ormerr.ErrNotFound) {
		t.Fatalf("零 id 的 Write 应报 ErrNotFound（一行都没匹配），得到 %v", err)
	}
	if stdErrors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf("零 id 是明确的「这零条」，不该被当成无条件写而报 ErrUnsafe")
	}

	eff, err = o.Model("ein.item").Ids().Delete()
	if err != nil || eff != 0 {
		t.Fatalf("零 id 的 Delete 应为 (0, nil)，得到 (%d, %v)", eff, err)
	}
	rs, _ := o.Model("ein.item").Limit(-1).Read()
	if rs.Count() != 5 {
		t.Fatalf("零 id 的 Delete 删掉了 %d 行", 5-rs.Count())
	}
	rs.Range(func(_ int, rec *dataset.TRecordSet) error {
		if utils.ToInt(rec.GetByField("age")) == 99 {
			t.Fatalf("零 id 的 Write 改到了某一行")
		}
		return nil
	})
}

func TestEmptyIn_DomainForms(t *testing.T) {
	o := setupEin(t)
	wantRows(t, `Domain("[('id','in',[])]")`, o.Model("ein.item").Domain(`[('id','in',[])]`), 0)
	wantRows(t, `Domain([]any{["id","in",[]]})`, o.Model("ein.item").Domain([]any{[]any{"id", "in", []any{}}}), 0)
	// domain.New 的零值形态：原来只有两个孩子，IsLeafNode() 恒 false，整棵树结构性坏掉
	var none []any
	wantRows(t, `domain.New("id","in", none...)`, o.Model("ein.item").Domain(domain.New("id", "in", none...)), 0)
	wantRows(t, `domain.NewDomainNode().IN("id")`, o.Model("ein.item").Domain(domain.NewDomainNode().IN("id")), 0)
	// not in 空集：恒真 → 等价于无条件（这里给 Limit 绕开无界守卫）
	wantRows(t, `Domain("[('id','not in',[])]")`, o.Model("ein.item").Domain(`[('id','not in',[])]`).Limit(-1), 5)
}

// NOT IN 空集是无操作：不能让一条"什么都没限制"的语句看起来有条件。
func TestEmptyIn_NotInIsNoopAndUnsafeGuardHolds(t *testing.T) {
	o := setupEin(t)
	_, err := o.Model("ein.item").NotIn("id").Read()
	if err == nil || !stdErrors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf(".NotIn(\"id\") 零参应视同无条件、被无界读守卫拦下，得到 %v", err)
	}
	_, err = o.Model("ein.item").NotIn("id").Delete()
	if err == nil || !stdErrors.Is(err, ormerr.ErrUnsafe) {
		t.Fatalf(".NotIn(\"id\") 零参的 Delete 必须被 ErrUnsafe 拦下，得到 %v", err)
	}
	if n := domain.NewDomainNode().NotIn("id").Count(); n != 0 {
		t.Fatalf("NotIn 零参不该往树里加东西，Count=%d", n)
	}
}

// domain.New 零值：三个孩子、是合法叶子。
func TestDomainNew_ZeroValuesIsWellFormedLeaf(t *testing.T) {
	var none []any
	n := domain.New("id", "in", none...)
	if n.Count() != 3 {
		t.Fatalf("New 零值应有 3 个孩子，得到 %d", n.Count())
	}
	if !n.IsLeafNode() {
		t.Fatalf("New 零值应是合法叶子：%s", n.String())
	}
}

// Where().Write() 一行没匹配到：带类型的 ErrNotFound，且原文保留（vectors 有按原文匹配的存量代码）。
func TestWrite_NoMatchIsTypedNotFound(t *testing.T) {
	o := setupEin(t)
	_, err := o.Model("ein.item").Where("id=?", 999999).Write(map[string]any{"age": 1})
	if err == nil {
		t.Fatal("一行没匹配的 Write 应报错")
	}
	if !stdErrors.Is(err, ormerr.ErrNotFound) {
		t.Fatalf("应可用 errors.Is(err, ErrNotFound) 判别，得到 %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "Not records found") {
		t.Fatalf("原文须保留以兼容存量字符串匹配，得到 %q", err.Error())
	}
}
