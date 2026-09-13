package orm

import "github.com/volts-dev/dataset"

// ICreateValuesHook 是模型可选实现的「逐条建前」钩子：每条记录在 Sets 施加之后、
// 拆分成写入列之前调一次，看得到、也改得了**调用方实际给的值**。
//
// # 为什么 BeforeSession 不够
//
// BeforeSession 在 Create 入口就跑完了，那时 src 还没解析 —— 它能做的只有往会话上挂
// Set，而 Set 的语义是**覆盖**（见 _create 的「应用 Sets」）。tenant_id / create_id
// 这类必须由服务端说了算的列，覆盖是对的；「有缺省值、但允许调用方在一定范围内显式
// 指定」的列用覆盖就是错的，而且错得无声无息。
//
// 实例是 vectors 的 company_id：原先在 BeforeSession 里 Set 当前公司，于是用户在表单上
// 选了 B 公司的日记账、公司格也跟着变成 B，存下去仍是 A。2026-09-13 真栈（工作集 A,B、
// 当前 A）：日记账、凭证、收付款、调拨、工时条目、工资单六类，显式给 B 的全部落成 A ——
// 凭证那条挂着 B 的日记账、记在 A 的账上，不报错。
//
// # 契约
//
//   - 只在 Create 上调，Write 不调（更新语义里"没给"就是"不动"，谈不上缺省）。
//   - values 是**本条**记录；多行 Create 逐行各调一次。钩子直接 SetByField 改它。
//   - 返回钩子写过的字段名：它们与 Sets 一样按"调用方明写"处理（并入 explicitKeys）。
//   - 返回 error 则本次 Create 就此失败；同一次调用里已插入的前几行与其他任何中途
//     失败一样，由调用方的事务回滚。
type ICreateValuesHook interface {
	BeforeCreateValues(session *TSession, values *dataset.TRecordSet) ([]string, error)
}

// beforeCreateValues 调模型的建前钩子；模型没实现就什么都不做。
func (self *TSession) beforeCreateValues(values *dataset.TRecordSet) ([]string, error) {
	hook, ok := self.Statement.Model.(ICreateValuesHook)
	if !ok || values == nil {
		return nil, nil
	}
	return hook.BeforeCreateValues(self, values)
}

// mergeHookKeys 把钩子写过的键并进显式键集合，口径同 mergeExplicitKeys：
// struct / dataset 源（keys 为 nil）保持整体语义不变。
func mergeHookKeys(keys map[string]bool, touched []string) map[string]bool {
	if len(touched) == 0 || keys == nil {
		return keys
	}
	for _, k := range touched {
		keys[k] = true
	}
	return keys
}
