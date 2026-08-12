package orm

import (
	"context"
	"testing"
)

// SyncModel 尾部那段"把反查到的其他表也登记成模型"**必须在冻结后跳过**。
//
// 症状：schema 隔离租户（vectors 的 VectorsSystem）升级任何模块都失败 ——
//
//	Upgrade "rating" failed: materialize schema system:
//	orm: osv is frozen, cannot register new models
//
// 而它要做的加列 DDL 在主循环里其实早已完成，失败发生在收尾。
//
// 为什么这在微服务拓扑下是**必然**而不是偶发：本进程的 schema 里躺着别的进程所属
// 模块的表。真机实测 kylin 的 `system` schema 共 347 张表，其中 70 张属于 kylin
// 根本没装的模块（sale / delivery / website_sale …）。它们的模型不在本进程的 osv
// 里，也不该在 —— 跨进程访问走属主服务。于是那个循环撞上第一张这样的表就返回
// ErrOsvFrozen。
//
// 这条测试不碰数据库：直接验 RegisterModel 在两种状态下的契约，
// 那正是 SyncModel 尾部依赖的东西。
func TestRegisterModel_FrozenRejectsNewButAcceptsKnown(t *testing.T) {
	orm := &TOrm{}
	osv := newOsv(orm)
	orm.osv = osv

	// 直接放进注册表，绕开 RegisterModel 冻结前那条要完整元数据的长路径 ——
	// 这条测试要证的是**冻结之后**的契约，启动期的注册另有测试覆盖。
	osv.models.Store("rating.rating", &TModelObject{name: "rating.rating"})

	if osv.IsFrozen() {
		t.Fatal("还没 Freeze，IsFrozen 就为真")
	}
	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze 失败: %v", err)
	}
	if !osv.IsFrozen() {
		t.Fatal("Freeze 之后 IsFrozen 仍为假 —— SyncModel 会据此决定跳不跳收尾那段")
	}

	// 已注册的同名模型：幂等 no-op。运行期把既有模型物化到另一个 schema 走这条。
	if err := osv.RegisterModel("rating", newMinimalTestModel("rating.rating")); err != nil {
		t.Fatalf("冻结后重复注册同名模型应当是 no-op，却报错: %v", err)
	}

	// 真正的新模型：拒绝。**这正是 SyncModel 收尾那段会撞上的东西** ——
	// 反查出来的 sale.order / website.sale.product.review 在本进程都是"新模型"。
	err := osv.RegisterModel("sale", newMinimalTestModel("sale.order"))
	if err != ErrOsvFrozen {
		t.Fatalf("冻结后注册新模型应当回 ErrOsvFrozen，实得: %v", err)
	}
}

// IsFrozen 必须能被包外读到 —— 它是 SyncModel 之外的调用方判断
// "现在还能不能登记新模型"的唯一入口；没有它，调用方只能靠捕获
// ErrOsvFrozen 事后补救，而那时整个操作已经失败了。
func TestOsv_IsFrozen_IsExported(t *testing.T) {
	orm := &TOrm{}
	osv := newOsv(orm)
	orm.osv = osv

	if osv.IsFrozen() {
		t.Fatal("新建的 osv 不该是冻结的")
	}
	if err := osv.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze 失败: %v", err)
	}
	if !osv.IsFrozen() {
		t.Fatal("Freeze 之后 IsFrozen 应为真")
	}
}
