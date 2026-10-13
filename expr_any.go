package orm

import (
	"strings"

	"github.com/volts-dev/orm/domain"
)

// any / not any（Odoo 17+ 的 Domain 操作符）：`(field, 'any', 子 domain)`，右值是**对端模型**上的一段 domain。
//
//	x2many：本记录至少有一条关联记录满足子 domain（同一条关联记录满足子 domain 的全部条件）；
//	many2one：关联的那条记录满足子 domain；
//	not any：取反。
//
// 点号路径穿过 many2one（`project_id.collaborator_ids any [...]`）走既有的 m2o 路径分支：先在对端按
// (collaborator_ids, any, 子 domain) 查出 id，再换成 (project_id in ids)。
//
// 上游门户的记录规则（project_task_rule_portal：项目的协作者里有我、且不是受限访问）就是这个形状；此前 ORM
// 不认 any，整条规则编译失败 = 门户用户读任务一律被拒。

func isAnyOperator(op string) bool {
	switch strings.ToLower(op) {
	case "any", "not any":
		return true
	}
	return false
}

// resolveM2oAny (m2o, any|not any, 子 domain) → (m2o in ids) / (m2o not in ids)。
func (self *TExpression) resolveM2oAny(model *TModel, field IField, column, op string, right *domain.TDomainNode) (*domain.TDomainNode, error) {
	if hasPlaceholder(right) {
		return nil, errPlaceholderInSubQuery(field, right)
	}
	target, err := self.orm.GetModel(field.RelatedModelName())
	if err != nil {
		return nil, err
	}
	ids, err := self.searchComodel(target, right)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(op) == "any" {
		return idInLeaf(model.idField, column, ids), nil
	}
	// not any：关联记录不满足子 domain（含没有关联记录的，Odoo 同）。表达式这一层要的是**一条**叶子，
	// 所以落成 (id not in 「m2o 落在匹配集里的那些本模型记录」)，空 m2o 的行自然留下。
	var owners []any
	if len(ids) > 0 {
		ds, err := self.subSession().Model(model.String()).Select(model.idField).Domain(idInLeaf(model.idField, column, ids)).Limit(-1).Read()
		if err != nil {
			return nil, err
		}
		if ds != nil {
			owners = ds.Keys()
		}
	}
	leaf := domain.NewDomainNode()
	leaf.Push(model.idField)
	leaf.Push("not in")
	leaf.Push(idListNode(owners))
	return leaf, nil
}
