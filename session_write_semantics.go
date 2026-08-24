package orm

import (
	"fmt"
	"sort"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
)

// 写入语义的三块拼图：未知键、写入范围、显式空值。
//
// 这三件事从前的共同毛病是**写入请求发出去、返回成功、库里没变**——调用方拿不到
// 任何信号。分别是：
//
//  1. 未知键静默丢弃。_separateValues 是**按模型字段遍历**的，输入里没被任何字段
//     认领的键根本不会被访问到。真栈：开户向导里把 contact_address 拼成
//     contact_ddress，用户填的地址从此消失，设置面板恒为空，无错误无日志。
//  2. struct 源的零值无法表达。结构体的 "" / 0 / false 与"没赋值"在反射层面
//     不可区分，写入侧只能一律当"没提供"。于是清空一个字段做不到。
//  3. 显式 nil 被当成"没提供"。setted 的判据是"值不是 nil"，把「键不在」与
//     「键在、值是 nil」压成了同一种；于是 Write(map{"expire": nil}) 影响 0 行。

// AllowUnknownFields 允许本次会话的写入携带模型上不存在的键（默认拒绝并报错）。
//
// 用在**不可信输入的边界**上：外部请求带的键不受本进程控制（老版本前端、别的
// 客户端、透传的只读字段），把它们当致命错误会让整次保存失败。而本进程的 Go
// 业务代码是受信的，那里写错键名就该第一次跑就炸出来——所以默认严格，由边界层
// 显式放宽。
//
// 效果对整个会话生命周期有效。
func (self *TSession) AllowUnknownFields() *TSession {
	self.allowUnknownFields = true
	return self
}

// checkUnknownFields 报出输入里模型无法认领的键。
//
// 只对 map 源生效（explicitKeys 非 nil）：那是调用方**逐个手打**的键，拼错就是 bug。
// struct 源经 StructToMap 产出，天然只含模型字段；dataset 源常是读回来的结果集
// （可能带 JOIN 出来的列、计算列），回喂写入是合法用法，不在此列。
func (self *TSession) checkUnknownFields(explicitKeys map[string]bool) error {
	if self.allowUnknownFields || len(explicitKeys) == 0 {
		return nil
	}

	model := self.Statement.Model
	if model == nil {
		return nil
	}

	var unknown []string
	for name := range explicitKeys {
		if name == "" {
			continue
		}
		if model.GetFieldByName(name) != nil {
			continue
		}
		// Omit 掉的键调用方已经明说"不要写"，不再计较它是不是模型字段。
		if self.Statement.IsOmit(name) {
			continue
		}
		unknown = append(unknown, name)
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	return ormerr.New(ormerr.ErrValidation, fmt.Errorf(
		"unknown field(s) %v on model %s; they would be silently dropped — fix the key name, Omit() them, or call AllowUnknownFields() at an untrusted-input boundary",
		unknown, model.String()))
}

// writeScopeSet 把 Select()/Fields() 点名的字段解析成写入范围。
// 返回 nil 表示没点名——写入全部字段，行为与从前一致。
//
// 点名之后语义是"就写这几列"：范围内的字段一律按**调用方明确给了值**处理，
// 零值照落、不填默认值。这是 struct 源表达"清空"的唯一办法——结构体的
// "" / 0 / false 与"没赋值"在反射层面不可区分，只能由调用方点名。
//
// 只作用于**更新**。新建时不点名：那条路径的语义是"给什么写什么 + 默认值补齐"，
// 限定范围只会把该补的默认值挡在外面。Select() 在新建时维持旧含义（充当
// mustFields 的必填校验），与从前一致。
func (self *TSession) writeScopeSet() map[string]bool {
	if len(self.Statement.Fields) == 0 {
		return nil
	}
	scope := make(map[string]bool, len(self.Statement.Fields))
	for _, name := range self.Statement.Fields {
		scope[fmtFieldName(name)] = true
	}
	return scope
}

// isNullishWrite 判断一个**显式给出**的值是否表达"把这一列清空"。
//
// nil 是显然的那种。零时刻是另一种：时间列上的 time.Time{} 会被原样写成
// 0001-01-01，既不是 NULL 也不报错——列看着像清空了，`expire < now()` 之类的
// 查询却照样命中它，是比"改不动"更难查的一种坏数据。年份 1 在业务上没有任何
// 合法含义，一律按清空处理。
//
// 只在**更新**路径上调用：新建时"没值"与"NULL"是同一个意思，该由默认值来填，
// 不该越过默认值直接写 NULL。
func isNullishWrite(field IField, v any) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case time.Time:
		return t.IsZero()
	case *time.Time:
		return t == nil || t.IsZero()
	}
	return false
}
