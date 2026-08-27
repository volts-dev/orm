package test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/volts-dev/orm"
	"github.com/volts-dev/orm/domain"

	_ "modernc.org/sqlite"
)

// ★ 防回退：会话条件把关联表 JOIN 进 FROM 时，按 id 写/删不许因 SQL 歧义整条失败。
//
// scopeIdsByDomain 先发一条 `SELECT id FROM ... WHERE 条件 AND id IN (...)`，把点名的
// ids 收窄到条件同样成立的那批。条件里只要出现一个**本表没有的列**——委托继承
// (one2one/_inherits)的父表字段，或写成点号的关联字段——where_calc 就把父表 JOIN 进
// FROM。两张表都有 `id` 列，于是裸 `id` 无法解析：
//
//	pq: column reference "id" is ambiguous at column 8 (42702)   // PG
//	SQL logic error: ambiguous column name: id (1)               // sqlite
//
// 2026-08-27 真栈：改系统用户偏好设置必报「服务器内部错误（错误编号 ERR-…）」。
// res.user 委托继承 res.partner，行级权限规则建在 partner 的 company_id 上，于是
// 每一次保存都命中。报出来的只有一句 SQL 错误，不指向任何模型、任何字段，光看它
// 找不回这里；搜索路径(session_query.go)一直写的是 `SELECT "表"."id" FROM`，
// 唯独这条收窄查询漏了限定。
type DottedPartner struct {
	orm.TModel `table:"name('dotted_partner')"`
	Id         int64 `field:"pk autoincr title('ID') index"`
	CompanyId  int   `field:"int() default(1)"`
}

// 委托继承 DottedPartner —— 对标 res.user 继承 res.partner：company_id 这一列
// 不在本表上，条件引用它就必须 JOIN 父表，FROM 于是变成两张表。
type DottedUser struct {
	orm.TModel         `table:"name('dotted_user')"`
	DottedPartner `field:"relate(PartnerId)"`
	PartnerId          int64  `field:"one2one(dotted_partner)"`
	Id                 int64  `field:"pk autoincr title('ID') index"`
	Name               string `field:"varchar()"`
}

// dottedRuleOn 关掉时 BeforeSession 不加条件，供测试以"上帝视角"核对真实落库结果。
var dottedRuleOn = true

// BeforeSession 模拟记录规则：条件建在继承来的父表字段上，写/删同样受限。
func (self *DottedUser) BeforeSession(s *orm.TSession) (*orm.TSession, error) {
	if !dottedRuleOn {
		return s, nil
	}
	switch s.Op {
	case orm.OpRead, orm.OpCount, orm.OpWrite, orm.OpDelete:
		s.Domain(domain.New("company_id", "=", 1))
	}
	return s, nil
}

func newDottedOrm(t *testing.T) *orm.TOrm {
	t.Helper()
	ds := &orm.TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "dotted.db")}
	o, err := orm.New(orm.WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("test", new(DottedPartner), new(DottedUser)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	// one2one 委托继承在 Freeze 的 Phase 2(inheritO2OFields)才执行，SyncModel 不会
	// 自动 Freeze——不调它，company_id 就不会出现在 dotted_user 上。
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return o
}

// dottedRawName 绕过记录规则读回真实的库值。
func dottedRawName(t *testing.T, o *orm.TOrm, id any) (string, bool) {
	t.Helper()
	prev := dottedRuleOn
	dottedRuleOn = false
	defer func() { dottedRuleOn = prev }()

	m, err := o.GetModel("dotted_user")
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

// newDottedRows 建两个用户：mine 的 partner 属于公司 1(规则内)，others 属于公司 2。
func newDottedRows(t *testing.T, o *orm.TOrm) (mine, others any) {
	t.Helper()
	prev := dottedRuleOn
	dottedRuleOn = false
	defer func() { dottedRuleOn = prev }()

	partner, err := o.GetModel("dotted_partner")
	if err != nil {
		t.Fatal(err)
	}
	p1, err := partner.Records().Create(map[string]any{"company_id": 1})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := partner.Records().Create(map[string]any{"company_id": 2})
	if err != nil {
		t.Fatal(err)
	}

	user, err := o.GetModel("dotted_user")
	if err != nil {
		t.Fatal(err)
	}
	mine, err = user.Records().Create(map[string]any{"name": "mine", "partner_id": p1[0]})
	if err != nil {
		t.Fatal(err)
	}
	others, err = user.Records().Create(map[string]any{"name": "others", "partner_id": p2[0]})
	if err != nil {
		t.Fatal(err)
	}
	return mine.([]any)[0], others.([]any)[0]
}

// 规则内的行：按 id 写必须成功落库，而不是撞 SQL 歧义 500。
func TestWriteByIdWithJoinedDomainSucceeds(t *testing.T) {
	o := newDottedOrm(t)
	mine, _ := newDottedRows(t, o)

	user, err := o.GetModel("dotted_user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := user.Records().Ids(mine).Write(map[string]any{"name": "changed"}); err != nil {
		t.Fatalf("按 id 写在会引发父表 JOIN 的会话条件下失败：%v", err)
	}

	got, ok := dottedRawName(t, o, mine)
	if !ok {
		t.Fatal("目标行不见了")
	}
	if got != "changed" {
		t.Fatalf("落库值 = %q，想要 %q", got, "changed")
	}
}

// 收窄语义不许被这次限定改坏：规则外的行按 id 写依旧改不到。
func TestWriteByIdWithJoinedDomainStaysScoped(t *testing.T) {
	o := newDottedOrm(t)
	_, others := newDottedRows(t, o)

	user, err := o.GetModel("dotted_user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := user.Records().Ids(others).Write(map[string]any{"name": "hacked"}); err != nil {
		t.Logf("写规则外的行返回错误(可接受)：%v", err)
	}

	got, ok := dottedRawName(t, o, others)
	if !ok {
		t.Fatal("目标行不见了")
	}
	if got != "others" {
		t.Fatalf("规则外的行被改成了 %q——按 id 写绕过了记录规则", got)
	}
}

// 删除走同一条收窄路径，同样不许因歧义而失败。
func TestDeleteByIdWithJoinedDomainSucceeds(t *testing.T) {
	o := newDottedOrm(t)
	mine, others := newDottedRows(t, o)

	user, err := o.GetModel("dotted_user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := user.Records().Ids(mine).Delete(); err != nil {
		t.Fatalf("按 id 删在会引发父表 JOIN 的会话条件下失败：%v", err)
	}
	if _, ok := dottedRawName(t, o, mine); ok {
		t.Fatal("规则内的行没被删掉")
	}
	if _, ok := dottedRawName(t, o, others); !ok {
		t.Fatal("规则外的行被连带删了")
	}
}

// 按**条件**写（不点名 id）走的是另一条路：_write 先 `SELECT id FROM 条件` 拿到 ids
// 再逐条 UPDATE。那条 SELECT 的 FROM 同样来自 where_calc，同样会 JOIN 进父表，
// 所以裸 `id` 在这里也一样炸——两处得一起限定，只修一处症状只减半。
func TestWriteByDomainWithJoinedDomainSucceeds(t *testing.T) {
	o := newDottedOrm(t)
	mine, others := newDottedRows(t, o)

	user, err := o.GetModel("dotted_user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := user.Records().Where("name=?", "mine").Write(map[string]any{"name": "changed"}); err != nil {
		t.Fatalf("按条件写在会引发父表 JOIN 的会话条件下失败：%v", err)
	}

	got, ok := dottedRawName(t, o, mine)
	if !ok {
		t.Fatal("目标行不见了")
	}
	if got != "changed" {
		t.Fatalf("落库值 = %q，想要 %q", got, "changed")
	}
	// 规则外的行不许被波及。
	if got, _ := dottedRawName(t, o, others); got != "others" {
		t.Fatalf("规则外的行被改成了 %q", got)
	}
}
