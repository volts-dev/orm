package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

type (
	TExtendedLeaf struct {
		/*
			            :attr [string, tuple] leaf: operator or tuple-formatted domain expression
			            :attr obj model: current working model
						:attr list models: list of chained models, updated when adding joins
						:attr list context: list of join contexts. This is a list of
				                tuples like ``(lhs, table, lhs_col, col, link)``

				                where

				                lhs
				                    source (left hand) model
				                model
				                    destination (right hand) model
				                lhs_col
				                    source model column for join condition
				                col
				                    destination model column for join condition
				                link
				                    link column between source and destination model
				                    that is not necessarily (but generally) a real column used
				                    in the condition (i.e. in many2one); this link is used to
				                    compute aliases
		*/
		join_context []TJoinContext //join_context
		leaf         *domain.TDomainNode
		model        *TModel
		models       []*TModel
	}
)

/*
Initialize the ExtendedLeaf

	:attr [string, tuple] leaf: operator or tuple-formatted domain
	    expression
	:attr obj model: current working model
	:attr list models: list of chained models, updated when
	    adding joins
	:attr list join_context: list of join contexts. This is a list of
	    tuples like ``(lhs, table, lhs_col, col, link)``

	    where

	    lhs
	        source (left hand) model
	    model
	        destination (right hand) model
	    lhs_col
	        source model column for join condition
	    col
	        destination model column for join condition
	    link
	        link column between source and destination model
	        that is not necessarily (but generally) a real column used
	        in the condition (i.e. in many2one); this link is used to
	        compute aliases
*/
func NewExtendedLeaf(leaf *domain.TDomainNode, model *TModel, context []TJoinContext, internal bool) *TExtendedLeaf {
	ex_leaf := &TExtendedLeaf{
		leaf:         leaf,
		model:        model,
		join_context: context,
	}

	ex_leaf.normalize_leaf()

	ex_leaf.models = append(ex_leaf.models, model)
	//# check validity
	ex_leaf.check_leaf(internal)
	return ex_leaf
}

/*
" Leaf validity rules:
  - a valid leaf is an operator or a leaf
  - a valid leaf has a field objects unless
  - it is not a tuple
  - it is an inherited field
  - left is id, operator is 'child_of'
  - left is in MAGIC_COLUMNS

"
*/
func (self *TExtendedLeaf) check_leaf(internal bool) {
	//if !isOperator(self.leaf) && !isLeaf(self.leaf, internal) {
	if !self.leaf.IsDomainOperator() && !self.leaf.IsLeafNode() {

		//raise ValueError("Invalid leaf %s" % str(self.leaf))
		//提示错误
	}
}

func (self *TExtendedLeaf) generate_alias() (string, string) {
	//links = [(context[1]._table, context[4]) for context in self.join_context]
	var links [][]string
	for _, context := range self.join_context {
		links = append(links, []string{context.DestModel.table, context.Link})
	}

	return generate_table_alias(self.models[0].table, links)
}

func (self *TExtendedLeaf) is_true_leaf() bool {
	if self.leaf.IsLeafNode() {
		return domain.Domain2String(self.leaf) == domain.TRUE_LEAF
	}

	return false
}

func (self *TExtendedLeaf) is_false_leaf() bool {
	if self.leaf.IsLeafNode() {
		return domain.Domain2String(self.leaf) == domain.FALSE_LEAF
	}

	return false
}

// 格式化 操作符 统一使用 字母in,not in 或者字符 "=", "!="
// 确保 操作符为小写
// """ Change a term's operator to some canonical form, simplifying later processing. """
func (self *TExtendedLeaf) normalize_leaf() bool {
	if !self.leaf.IsLeafNode() {
		return true
	}

	original := strings.ToLower(self.leaf.String(1))
	operator := original
	if operator == "<>" {
		operator = "!="
	}
	if utils.IsBoolItf(self.leaf.Item(2)) && utils.IndexOf(operator, "in", "not in") != -1 {
		//   _log.warning("The domain term '%s' should use the '=' or '!=' operator." % ((left, original, right),))
		if operator == "in" {
			operator = "="
		} else {
			operator = "!="
		}
	}
	if self.leaf.Item(2).IsListNode() && utils.IndexOf(operator, "=", "!=") != -1 {
		//  _log.warning("The domain term '%s' should use the 'in' or 'not in' operator." % ((left, original, right),))
		if operator == "=" {
			operator = "in"
		} else {
			operator = "not in"
		}
	}

	self.leaf.Item(1).Value = operator

	return true
}

// See above comments for more details. A join context is a tuple like:
//
//	``(lhs, model, lhs_col, col, link)``
//
// After adding the join, the model of the current leaf is updated.
func (self *TExtendedLeaf) add_join_context(model *TModel, lhs_col, table_col, link string) {
	self.join_context = append(
		self.join_context,
		TJoinContext{
			SourceModel: self.model,
			DestModel:   model,
			SourceFiled: lhs_col,
			DestFiled:   table_col,
			Link:        link})
	self.models = append(self.models, model)
	self.model = model
}

func (self *TExtendedLeaf) get_tables() *utils.TStringList {
	tables := utils.NewStringList()
	links := make([][]string, 0)

	for _, context := range self.join_context {
		links = append(links, []string{context.DestModel.table, context.Link})
		_, alias_statement := generate_table_alias(self.models[0].table, links)

		tables.PushString(alias_statement)
	}

	return tables
}

func (self *TExtendedLeaf) get_join_conditions() (conditions []string) {
	conditions = make([]string, 0) //utils.NewStringList()
	alias := self.models[0].table
	for _, context := range self.join_context {
		previous_alias := alias
		alias += "__" + context.Link
		condition := fmt.Sprintf(`"%s"."%s"="%s"."%s"`, previous_alias, context.SourceFiled, alias, context.DestFiled)
		conditions = append(conditions, condition)
	}

	return conditions
}
