package orm

import (
	"fmt"

	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

// 层级查询：child_of / parent_of。
//
// # 背景
//
// 这两个操作符此前是**空实现**——child_of_domain / parent_of_domain 的函数体只剩
// 注释掉的 Python 和一句 `return nil`。调用点拿到 nil 后 `Reversed()` 回空节点、
// 循环一次不走，于是整条叶子**凭空消失**：child_of 筛选等于没筛、整表返回，不报错。
//
// 行级权限规则正是靠它表达"我这条记录的公司是我某个公司的祖先/后代"，一条筛不出来
// 的规则就是一次越权。vectors 侧的代价：core/recordrule/legacy.go 把存量的 27 条
// parent_of 规则整体改写成 in（语义收紧），compile.go 干脆把 parent_of 拉黑；
// 而 child_of 留在白名单里，配上后来加的"拒绝静默丢条件"的保护，等于一用就整页报错。
//
// # 为什么不用 parent_path
//
// Odoo 有两条路：_parent_store 模型上用物化路径列 parent_path 做 `=like '1/4/%'`，
// 否则逐层展开。物化路径要加列、要在每次改父节点时重写整棵子树，是一次 schema 迁移
// 加一套写入期维护；而本仓的树都是业务层级（公司、科目、品类、菜单），深度个位数。
// 这里走逐层展开：不动表结构、对存量数据立即生效、三种方言一致。
//
// 递归 CTE 能一次查完，但 MySQL 5.7 没有，且收益要在很深的树上才显现——留作后续。
//
// # parent_of 在 orm 这边可用，在 vectors 的记录规则那边仍然关着
//
// 别把这两件事读成一件。orm 实现了它 ≠ 行级权限可以开始用它。
//
// vectors 的 core/recordrule/compile.go 有一张操作符白名单，parent_of **刻意**不在
// 表里：存量库 27 条 parent_of 规则（横跨 31 个模型）当前经 legacy.go 降级成 in，
// 而 `in ⊆ parent_of`——把它放进白名单等于一次性给这 27 条规则放宽可见范围。收紧
// 可以顺手做，放宽不行，必须逐条复核。复核完由那边把它加进白名单，orm 这边不需要
// 任何改动。
//
// child_of 不同：它从前是 fail-open（静默整表返回，规则形同虚设），实现之后开始
// 真的筛，方向是收紧，所以一直在白名单里。

// maxHierarchyDepth 逐层展开的层数上限。
//
// 已访问集合本身就保证了终止（id 空间有限，环也走不回头），这个上限防的是另一件事：
// 一棵异常深的树会发出同样多次查询。业务层级到不了这个深度，撞上基本等于数据坏了，
// 明确报错好过闷头发几百条 SQL。
const maxHierarchyDepth = 100

// hierarchyFunc 把一条层级叶子展开成一条普通的 (left in [...])。
//
// left       落在结果叶子上的列名（'id' 或某个 m2o 字段名）
// ids        种子记录的 id（来自 to_ids，可能是标量、也可能是列表）
// model      在**哪个模型**里走这棵树
// parentName 用哪个字段当父链接；空串表示按约定推断（见 hierarchyParentField）
type hierarchyFunc func(ex *TExpression, left string, ids *domain.TDomainNode, model *TModel, parentName string) (*domain.TDomainNode, error)

// child_of_domain 展开 [(left, 'child_of', ids)]：ids 的全部后代，**含自身**。
func child_of_domain(ex *TExpression, left string, ids *domain.TDomainNode, model *TModel, parentName string) (*domain.TDomainNode, error) {
	return ex.expandHierarchy(left, ids, model, parentName, false)
}

// parent_of_domain 展开 [(left, 'parent_of', ids)]：ids 的全部祖先，**含自身**。
func parent_of_domain(ex *TExpression, left string, ids *domain.TDomainNode, model *TModel, parentName string) (*domain.TDomainNode, error) {
	return ex.expandHierarchy(left, ids, model, parentName, true)
}

// expandHierarchy 沿父链逐层展开。up=false 往下找后代，up=true 往上找祖先。
func (self *TExpression) expandHierarchy(left string, ids *domain.TDomainNode, model *TModel, parentName string, up bool) (*domain.TDomainNode, error) {
	seeds := hierarchySeedIds(ids)
	if len(seeds) == 0 {
		// 对齐 Odoo：`X child_of False` 匹配不到任何东西，落成恒假而不是恒真。
		// 恒假由 idInLeaf 的空列表分支产出（一个永远不存在的哨兵 id）。
		return idInLeaf(model.IdField(), left, nil), nil
	}

	parentField, err := hierarchyParentField(model, parentName)
	if err != nil {
		return nil, err
	}

	// 已访问集合按 id 的字符串形态去重：种子可能是 to_ids 经 NameSearch 回来的
	// **字符串** id，而库里查出来的是原生整型，不归一会把同一条记录当成两条，
	// 环形数据(a→b→a)就再也退不出来。
	seen := make(map[string]bool, len(seeds))
	out := make([]any, 0, len(seeds))
	frontier := make([]any, 0, len(seeds))
	for _, id := range seeds {
		v := normalizeHierarchyId(id)
		k := utils.ToString(v)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, v)
		frontier = append(frontier, v)
	}

	for depth := 0; len(frontier) > 0; depth++ {
		if depth >= maxHierarchyDepth {
			return nil, fmt.Errorf(
				"%s on %s exceeded the max hierarchy depth (%d) via %q — the tree is probably malformed",
				hierarchyOpName(up), model.String(), maxHierarchyDepth, parentField)
		}

		var step []any
		if up {
			step, err = self.hierarchyParents(model, parentField, frontier)
		} else {
			step, err = self.hierarchyChildren(model, parentField, frontier)
		}
		if err != nil {
			return nil, err
		}

		next := step[:0:0]
		for _, id := range step {
			v := normalizeHierarchyId(id)
			k := utils.ToString(v)
			if k == "" || k == "0" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, v)
			next = append(next, v)
		}
		frontier = next
	}

	return idInLeaf(model.IdField(), left, out), nil
}

// hierarchyChildren 下钻一层：父字段落在 frontier 里的那些记录。
func (self *TExpression) hierarchyChildren(model *TModel, parentField string, frontier []any) ([]any, error) {
	idField := model.IdField()
	// Limit(-1)：这是内部展开，边界由 frontier 决定；吃默认上限会让深层子树凭空消失。
	ds, err := self.subSession().Model(model.String()).
		Select(idField).In(parentField, frontier...).Limit(-1).Read()
	if err != nil {
		return nil, err
	}
	if ds == nil {
		return nil, nil
	}
	return ds.Keys(idField), nil
}

// hierarchyParents 上溯一层：frontier 各记录父字段的值。
func (self *TExpression) hierarchyParents(model *TModel, parentField string, frontier []any) ([]any, error) {
	idField := model.IdField()
	ds, err := self.subSession().Model(model.String()).
		Select(idField, parentField).Ids(frontier...).Limit(-1).Read()
	if err != nil {
		return nil, err
	}
	if ds == nil {
		return nil, nil
	}

	out := make([]any, 0, ds.Count())
	ds.First()
	for !ds.Eof() {
		// 不用 Classic 读，所以 m2o 回来的是外键列的原始标量；空父节点(根)会是
		// nil / 0 / 空串，由调用方按 seen 的 "" 与 "0" 过滤掉。
		if v := ds.Record().GetByField(parentField); !utils.IsBlank(v) {
			out = append(out, v)
		}
		ds.Next()
	}
	return out, nil
}

// hierarchyParentField 决定用哪个字段当父链接。
//
// 本 ORM 没有 Odoo 的 _parent_name 声明，按三级约定推断：
//  1. 调用方点名的（`('parent_id','child_of',…)` 这种形态，字段就是父链接本身）
//  2. 名为 parent_id 的自引用 many2one（与 Odoo 的默认一致）
//  3. 模型上**唯一**的自引用 many2one
//
// 推断不出来一律报错。猜错父字段产出的是一棵错的树——那正是"看着正常的错数据"，
// 比查不出来贵得多。
func hierarchyParentField(model *TModel, declared string) (string, error) {
	modelName := model.String()
	isSelfRef := func(f IField) bool {
		return f != nil && f.Store() && f.TypeName() == TYPE_M2O && f.RelatedModelName() == modelName
	}

	if declared != "" {
		if f := model.GetFieldByName(declared); !isSelfRef(f) {
			return "", fmt.Errorf(
				"hierarchy on %s: field %q is not a self-referencing many2one, cannot walk the tree with it",
				modelName, declared)
		}
		return declared, nil
	}

	if f := model.GetFieldByName(DefaultParentField); isSelfRef(f) {
		return DefaultParentField, nil
	}

	var candidates []string
	for _, f := range model.GetFields() {
		if isSelfRef(f) {
			candidates = append(candidates, f.Name())
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return "", fmt.Errorf(
			"hierarchy on %s: no self-referencing many2one field (expected %q or exactly one) — child_of/parent_of need a parent link",
			modelName, DefaultParentField)
	default:
		return "", fmt.Errorf(
			"hierarchy on %s: ambiguous parent link, several self-referencing many2one fields %v — name the one to use, e.g. ('%s','child_of',…)",
			modelName, candidates, candidates[0])
	}
}

// hierarchySeedIds 把 to_ids 的返回摊平成种子 id 列表。
// to_ids 可能回标量(('id','child_of',5))、整型列表，或 NameSearch 查出的字符串 id。
func hierarchySeedIds(ids *domain.TDomainNode) []any {
	if ids == nil {
		return nil
	}
	out := make([]any, 0, 4)
	for _, v := range ids.Flatten() {
		if v == nil || utils.IsBlank(v) {
			continue
		}
		// false 是空值的通用写法（`X child_of False`），当作没给。
		if b, ok := v.(bool); ok && !b {
			continue
		}
		out = append(out, v)
	}
	return out
}

// normalizeHierarchyId 把数字形态的字符串 id 收敛成数值。
// to_ids 走 NameSearch 那条分支回的是字符串，直接拿去和整型主键比会在 PostgreSQL
// 上撞类型；而混进结果列表也会让同一条记录被当成两条。非数字 id 原样返回。
func normalizeHierarchyId(v any) any {
	if s, ok := v.(string); ok {
		if n, err := utils.IsNumeric(s); err == nil {
			return n
		}
	}
	return v
}

func hierarchyOpName(up bool) string {
	if up {
		return "parent_of"
	}
	return "child_of"
}
