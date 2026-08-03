package orm

import (
	"context"
	"testing"
)

// _getModel 是「顺带写/读另一张表」的入口：委托继承自动建父记录、o2m/m2m 的关联写回
// 都从这里取 comodel。它必须把调用方的上下文一起带过去，事务和 schema 早就带了，
// **模型 Ctx 一直没带**。
//
// 上层(vectors)把登录会话挂在模型 Ctx 的 "AuthSession" 上，并据此在创建时盖
// tenant_id / create_id / write_id。Ctx 丢了，经这条路建出来的行三个戳全是 0——
// 而这种行**按 id 读得到、按任何带租户条件的读一律读不到**，写入却是成功的、零报错。
// 真机 2026-08-04：产品模板 Prices 页加的两条价格规则落库即 tenant_id=0，刷新后空白。
func Test_getModel_inheritsCallerContext(t *testing.T) {
	o := setupIntegrationOrm(t)

	type ctxKey string
	const authKey ctxKey = "AuthSession"

	caller, err := o.GetModel("bench.model")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	caller.Ctx(context.WithValue(context.Background(), authKey, "tenant-42"))

	sess := caller.Records()
	sess.Statement.Model = caller

	comodel, err := sess._getModel("bench.model")
	if err != nil {
		t.Fatalf("_getModel: %v", err)
	}
	got := comodel.Ctx().Value(authKey)
	if got != "tenant-42" {
		t.Fatalf("comodel 没继承调用方的 Ctx: AuthSession=%v，要的是 tenant-42", got)
	}
}

// 每次 GetModel 都新建模型包装（_initObject），所以往 Ctx 上写会话不会串到别的请求。
// 这一条是上面那个修复的安全前提：如果模型是共享单例，继承 Ctx 就成了跨请求泄漏。
func Test_getModel_returnsPerCallWrapper(t *testing.T) {
	o := setupIntegrationOrm(t)

	type ctxKey string
	const authKey ctxKey = "AuthSession"

	a, err := o.GetModel("bench.model")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	a.Ctx(context.WithValue(context.Background(), authKey, "tenant-A"))

	b, err := o.GetModel("bench.model")
	if err != nil {
		t.Fatalf("GetModel: %v", err)
	}
	if v := b.Ctx().Value(authKey); v == "tenant-A" {
		t.Fatal("两次 GetModel 拿到的是同一个模型包装——往 Ctx 上挂登录会话会跨请求泄漏")
	}
}
