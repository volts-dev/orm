package orm

import (
	"reflect"
	"testing"
)

// 一个模型同时注册在两个 region 下时，挑哪个**不能**由 map 迭代序决定：
// Go 的 map 迭代序每次都不一样，随机挑的表现是同一条请求时好时坏。
func TestPickModelRegion_PrefersNamedOverPrototype(t *testing.T) {
	typ := reflect.TypeOf(TModel{})
	types := map[string]map[string]reflect.Type{
		"":              {"admin.service": typ}, // _reverse() 按表反查出来的裸原型
		"admin_service": {"admin.service": typ}, // 具名模块注册的真模型
	}
	// 跑多轮：一次撞对不算数，map 迭代序是随机的。
	for i := 0; i < 50; i++ {
		got, m := pickModelRegion(types)
		if got != "admin_service" {
			t.Fatalf("第 %d 轮挑到 %q，应恒为具名 region admin_service", i, got)
		}
		if m == nil {
			t.Fatalf("第 %d 轮返回了 nil map", i)
		}
	}
}

// 多个具名 region 时按名字排序取第一个——值本身不重要，**确定**才重要。
func TestPickModelRegion_DeterministicAmongNamed(t *testing.T) {
	typ := reflect.TypeOf(TModel{})
	types := map[string]map[string]reflect.Type{
		"zeta": {"x.y": typ}, "alpha": {"x.y": typ}, "mid": {"x.y": typ},
	}
	for i := 0; i < 50; i++ {
		if got, _ := pickModelRegion(types); got != "alpha" {
			t.Fatalf("第 %d 轮挑到 %q，应恒为 alpha", i, got)
		}
	}
}

// 只有原型区时仍要能拿到它，否则连"库里有表、代码里没模型"这条路都断了。
func TestPickModelRegion_FallsBackToPrototype(t *testing.T) {
	typ := reflect.TypeOf(TModel{})
	types := map[string]map[string]reflect.Type{"": {"x.y": typ}}
	got, m := pickModelRegion(types)
	if got != "" || m == nil {
		t.Fatalf("只有原型区时应回落到原型区，得到 region=%q map=%v", got, m)
	}
}
