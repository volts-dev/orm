package test

import (
	"path/filepath"
	"testing"

	"github.com/volts-dev/orm"

	_ "modernc.org/sqlite"
)

// ★ 防回退：按 id 写/删必须同样受会话条件约束。
//
// UPDATE/DELETE 走"点名 id"时拼出的 SQL 是 `... WHERE id IN (...)`，Statement 上
// 累积的 Where()/Domain() 一条都不进去。凡是靠"往会话追加条件"实现的范围限制
// （vectors 的多租户 tenant_id 过滤、行级权限的记录规则）在按 id 写时因此**整段失效**：
// 读侧看不见的行，按 id 改得到、删得掉。
//
// 2026-08-08 真栈实测（webtest 租户 + 多商家隔离规则）：商家 A 对商家 B 的
// stock.quant 发一次按 id 的更新，quantity 真实落库，无错误、无日志——读隔离一切正常，
// 写隔离完全不存在。修在 orm 侧（scopeIdsByDomain），因为业务层再怎么校验，
// 只要有人拿到 session 直接按 id 写就绕过去了。

type scopedRowModel struct {
	orm.TModel `table:"name('scoped_row')"`
	Id         int64  `field:"pk autoincr title('ID') index"`
	Name       string `field:"varchar()"`
	Tenant     int    `field:"int() default(1)"`
}

// scopedHookOn 关掉时 BeforeSession 不加条件，供测试以"上帝视角"核对真实落库结果。
var scopedHookOn = true

// BeforeSession 模拟 vectors 的 withSession：读/写/删一律限定在租户 1。
func (self *scopedRowModel) BeforeSession(s *orm.TSession) (*orm.TSession, error) {
	if !scopedHookOn {
		return s, nil
	}
	switch s.Op {
	case orm.OpRead, orm.OpCount, orm.OpWrite, orm.OpDelete:
		s.Where("tenant=?", 1)
	}
	return s, nil
}

func newScopedOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "scoped.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("test", new(scopedRowModel)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	return o
}

// scopedRawName 绕过会话条件读回真实的库值。
func scopedRawName(t *testing.T, o *orm.TOrm, id any) (string, bool) {
	t.Helper()
	prev := scopedHookOn
	scopedHookOn = false
	defer func() { scopedHookOn = prev }()

	m, err := o.GetModel("scoped_row")
	if err != nil {
		t.Fatal(err)
	}
	ds, err := m.Records().Ids(id).Read()
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if ds == nil || ds.Count() == 0 {
		return "", false
	}
	return ds.Record().FieldByName("name").AsString(), true
}

func newScopedRows(t *testing.T, o *orm.TOrm) (mine, others any) {
	t.Helper()
	m, err := o.GetModel("scoped_row")
	if err != nil {
		t.Fatal(err)
	}
	// 建记录不受钩子影响（Create 分支不加条件），两条分属两个租户。
	a, err := m.Records().Create(map[string]any{"name": "mine", "tenant": 1})
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	b, err := m.Records().Create(map[string]any{"name": "theirs", "tenant": 2})
	if err != nil {
		t.Fatalf("create theirs: %v", err)
	}
	return a[0], b[0]
}

func TestWriteByIdRespectsSessionScope(t *testing.T) {
	o := newScopedOrm(t)
	mine, others := newScopedRows(t, o)
	m, err := o.GetModel("scoped_row")
	if err != nil {
		t.Fatal(err)
	}

	// 范围内的行：照常写得动，别把正常路径一起挡了。
	if _, err := m.Records().Ids(mine).Write(map[string]any{"name": "mine-v2"}); err != nil {
		t.Fatalf("写自己的行不该失败: %v", err)
	}
	if got, _ := scopedRawName(t, o, mine); got != "mine-v2" {
		t.Fatalf("自己的行应被改成 mine-v2，实际 %q", got)
	}

	// 范围外的行：影响 0 行，且库里的值一个字节都不许变。
	effect, err := m.Records().Ids(others).Write(map[string]any{"name": "hacked"})
	if err != nil {
		t.Fatalf("越权写不该报错（语义是够不着=什么都没改）: %v", err)
	}
	if effect != 0 {
		t.Fatalf("越权写应影响 0 行，实际 %d", effect)
	}
	if got, _ := scopedRawName(t, o, others); got != "theirs" {
		t.Fatalf("**越权写真实落库了**：别人的行应保持 theirs，实际 %q", got)
	}
}

func TestDeleteByIdRespectsSessionScope(t *testing.T) {
	o := newScopedOrm(t)
	mine, others := newScopedRows(t, o)
	m, err := o.GetModel("scoped_row")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Records().Delete(others); err != nil {
		t.Fatalf("越权删不该报错（够不着=什么都没删）: %v", err)
	}
	if _, ok := scopedRawName(t, o, others); !ok {
		t.Fatal("**越权删真实生效了**：别人的行不该被删掉")
	}

	if _, err := m.Records().Delete(mine); err != nil {
		t.Fatalf("删自己的行不该失败: %v", err)
	}
	if _, ok := scopedRawName(t, o, mine); ok {
		t.Fatal("自己的行应被删除")
	}
}

// ★ 防回退：按 id 读同样必须受会话条件约束。
//
// _readFromDatabase 此前在有 IdParam 时先 domain.Clear() 再放 id 条件，注释写着
// "当指定了主键其他查询条件将失效"——于是按 id 读绕过一切范围限制。真栈实测：
// 同一会话按 Domain 读只看得见自己的行（正确），按 Ids 读别人的行照样返回。
func TestReadByIdRespectsSessionScope(t *testing.T) {
	o := newScopedOrm(t)
	mine, others := newScopedRows(t, o)
	m, err := o.GetModel("scoped_row")
	if err != nil {
		t.Fatal(err)
	}

	// 自己的行：按 id 读得到，别把正常路径一起挡了。
	ds, err := m.Records().Ids(mine).Read()
	if err != nil {
		t.Fatalf("读自己的行不该失败: %v", err)
	}
	if ds == nil || ds.Count() != 1 {
		t.Fatalf("自己的行应读到 1 条，实际 %v", ds.Count())
	}

	// 范围外的行：按 id 也读不到。
	ds, err = m.Records().Ids(others).Read()
	if err != nil {
		t.Fatalf("越权读不该报错（够不着=查不到）: %v", err)
	}
	if ds != nil && ds.Count() != 0 {
		t.Fatalf("**按 id 读绕过了会话条件**：不该读到别人的行，实际 %d 条", ds.Count())
	}

	// 混读：只回自己的那条。
	ds, err = m.Records().Ids(mine, others).Read()
	if err != nil {
		t.Fatalf("混合 id 读失败: %v", err)
	}
	if ds == nil || ds.Count() != 1 {
		t.Fatalf("混合 id 读应只回 1 条（自己的），实际 %v", ds.Count())
	}
}
