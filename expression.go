package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/utils"
)

/*
const (
	// Domain operators.
	NOT_OPERATOR = "!"
	OR_OPERATOR  = "|"
	AND_OPERATOR = "&"

	TRUE_LEAF  = "(1, '=', 1)"
	FALSE_LEAF = "(0, '=', 1)"

	TRUE_DOMAIN  = "[" + TRUE_LEAF + "]"
	FALSE_DOMAIN = "[" + FALSE_LEAF + "]"
)

var (
	DOMAIN_OPERATORS = []string{NOT_OPERATOR, OR_OPERATOR, AND_OPERATOR}

	/*# List of available term operators. It is also possible to use the '<>'
	# operator, which is strictly the same as '!='; the later should be prefered
	# for consistency. This list doesn't contain '<>' as it is simpified to '!='
	# by the normalize_operator() function (so later part of the code deals with
	# only one representation).
	# Internals (i.e. not available to the user) 'inselect' and 'not inselect'
	# operators are also used. In this case its right operand has the form (subselect, params).
*/
/*	TERM_OPERATORS = []string{"=", "!=", "<=", "<", ">", ">=", "=?", "=like", "=ilike",
		"like", "not like", "ilike", "not ilike", "in", "not in", "child_of"}
)
*/
type (
	/*""" Class wrapping a domain leaf, and giving some services and management
	    features on it. In particular it managed join contexts to be able to
	    construct queries through multiple models.
	"""*/
	TJoinContext struct {
		SourceModel *TModel //source (left hand) model
		DestModel   *TModel //destination (right hand) model
		SourceFiled string  //source model column for join condition
		DestFiled   string  //destination model column for join condition
		Link        string
	}
	TExtendedLeaf struct {
		/*
			            :attr [string, tuple] leaf: operator or tuple-formatted domain expression
			            :attr obj model: current working model
						:attr list _models: list of chained models, updated when adding joins
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
		leaf         *utils.TStringList
		model        *TModel

		_models []*TModel
	}

	/*""" Parse a domain expression
	    Use a real polish notation
	    Leafs are still in a ('foo', '=', 'bar') format
	    For more info: http://christophe-simonis-at-tiny.blogspot.com/2008/08/new-new-domain-notation.html
	"""*/
	TExpression struct {
		root_model *TModel // 本次解析的主要
		Table      string
		Expression *utils.TStringList
		stack      []*TExtendedLeaf
		result     []*TExtendedLeaf
		joins      []string //*utils.TStringList
	}
)

func NewExpression(model *TModel, domain *utils.TStringList, context map[string]interface{}) *TExpression {
	exp := &TExpression{
		//Table: table,
		root_model: model,
		joins:      make([]string, 0),
	}

	//utils.PrintStringList(domain)
	exp.Expression = distribute_not(normalize_domain(domain))
	exp.parse(context)
	return exp
}

/* Initialize the ExtendedLeaf

   :attr [string, tuple] leaf: operator or tuple-formatted domain
       expression
   :attr obj model: current working model
   :attr list _models: list of chained models, updated when
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
func NewExtendedLeaf(leaf *utils.TStringList, model *TModel, context []TJoinContext, internal bool) *TExtendedLeaf {
	ex_leaf := &TExtendedLeaf{
		leaf:         leaf,
		model:        model,
		join_context: context,
	}

	ex_leaf.normalize_leaf()

	ex_leaf._models = append(ex_leaf._models, model)
	//# check validity
	ex_leaf.check_leaf(internal)
	return ex_leaf
}

/*" Leaf validity rules:
    - a valid leaf is an operator or a leaf
    - a valid leaf has a field objects unless
        - it is not a tuple
        - it is an inherited field
        - left is id, operator is 'child_of'
        - left is in MAGIC_COLUMNS
"*/
func (self *TExtendedLeaf) check_leaf(internal bool) {
	if !is_operator(self.leaf) && !is_leaf(self.leaf, internal) {
		//raise ValueError("Invalid leaf %s" % str(self.leaf))
		//提示错误
	}
}

func (self *TExtendedLeaf) generate_alias() string {
	//links = [(context[1]._table, context[4]) for context in self.join_context]
	var links [][]string
	for _, context := range self.join_context {
		links = append(links, []string{context.DestModel.GetTableName(), context.Link})
	}

	alias, _ /* alias_statement*/ := generate_table_alias(self._models[0].GetTableName(), links)
	//logger.Dbg("generate_alias", alias)
	return alias
}
func (self *TExtendedLeaf) is_operator() bool {
	return is_operator(self.leaf)
}
func (self *TExtendedLeaf) is_true_leaf() bool {
	return self.leaf.Text == TRUE_LEAF
}
func (self *TExtendedLeaf) is_false_leaf() bool {
	return self.leaf.Text == FALSE_LEAF
}

func (self *TExtendedLeaf) is_leaf(interna bool) bool {
	return is_leaf(self.leaf, interna)
}

/*""" Test whether an object is a valid domain operator. """*/
func is_operator(op *utils.TStringList) bool {
	return utils.InStrings(StringList2Domain(op), DOMAIN_OPERATORS...) != -1
}

func (self *TExtendedLeaf) normalize_leaf() bool {
	self.leaf = normalize_leaf(self.leaf)
	return true
}

// See above comments for more details. A join context is a tuple like:
//        ``(lhs, model, lhs_col, col, link)``
// After adding the join, the model of the current leaf is updated.
func (self *TExtendedLeaf) add_join_context(model *TModel, lhs_col, table_col, link string) {

	self.join_context = append(self.join_context, TJoinContext{SourceModel: self.model,
		DestModel: model, SourceFiled: lhs_col, DestFiled: table_col, Link: link})
	self._models = append(self._models, model)
	self.model = model
}

func (self *TExtendedLeaf) get_tables() (tables *utils.TStringList) {
	tables = utils.NewStringList()
	links := make([][]string, 0)
	for _, context := range self.join_context {
		links = append(links, []string{context.DestModel.GetTableName(), context.Link})
		_, alias_statement := generate_table_alias(self._models[0].GetTableName(), links)
		tables.PushString(alias_statement)
	}

	return
}

func (self *TExtendedLeaf) get_join_conditions() (conditions []string) {
	conditions = make([]string, 0) //utils.NewStringList()
	alias := self._models[0].GetTableName()
	for _, context := range self.join_context {
		previous_alias := alias
		alias += "__" + context.Link
		condition := fmt.Sprintf(`"%s"."%s"="%s"."%s"`, previous_alias, context.SourceFiled, alias, context.DestFiled)
		//logger.Dbg("condition", condition)
		conditions = append(conditions, condition)
	}

	return conditions
}

/*
func NewList(src string) (node *TNode) {
	node = &TNode{IsList: false}
	node = node.parse(src)
	return
}

func (self *TNode) parse(src string) (node *TNode) {
	// 初始化 Text Node
	node = &TNode{
		Type:     LeafType,
		Text:     src,
		Elements: nil}

	// ('active', '=', True)
	if strings.HasPrefix(src, "(") && strings.HasSuffix(src, ")") {
		node.Type = NodeType
		lElements := strings.Split(src[1:len(src)-1], ",")
		for _, e := range lElements {
			n := &TNode{Elements: nil, Text: e, Type: LeafType}
			node.Elements = append(node.Elements, n)
		}
	}

	// ['&', ('active', '=', True), ('value', '!=', 'foo')] 只对[]处理 其他返回Text Node
	if strings.HasPrefix(src, "[") && strings.HasSuffix(src, "]") {
		node.Type = TreeType
		lElements := strings.Split(src[1:len(src)-1], ",")
		for _, e := range lElements {
			n := self.parse(e)
			node.Elements = append(node.Elements, n)
		}
	}
	return
}

func (self *TNode) doPrint(a []interface{}, addspace, addnewline bool) {

}
*/

/*
# --------------------------------------------------
# Generic leaf manipulation
# --------------------------------------------------
*/

func _quote(to_quote string) string {
	/*
	   	def _quote(to_quote):
	       if '"' not in to_quote:
	           return '"%s"' % to_quote
	       return to_quote
	*/
	if strings.Index(`"`, to_quote) == -1 {
		return `"` + to_quote + `"`
	}
	return to_quote
}

/*
def generate_table_alias(src_table_alias, joined_tables=[]):
    """ Generate a standard table alias name. An alias is generated as following:
        - the base is the source table name (that can already be an alias)
        - then, each joined table is added in the alias using a 'link field name'
          that is used to render unique aliases for a given path
        - returns a tuple composed of the alias, and the full table alias to be
          added in a from condition with quoting done
        Examples:
        - src_table_alias='res_users', join_tables=[]:
            alias = ('res_users','"res_users"')
        - src_model='res_users', join_tables=[(res.partner, 'parent_id')]
            alias = ('res_users__parent_id', '"res_partner" as "res_users__parent_id"')

        :param model src_table_alias: model source of the alias
        :param list joined_tables: list of tuples
                                   (dst_model, link_field)

        :return tuple: (table_alias, alias statement for from clause with quotes added)
    """
    alias = src_table_alias
    if not joined_tables:
        return '%s' % alias, '%s' % _quote(alias)
    for link in joined_tables:
        alias += '__' + link[1]
    assert len(alias) < 64, 'Table alias name %s is longer than the 64 characters size accepted by default in postgresql.' % alias
    return '%s' % alias, '%s as %s' % (_quote(joined_tables[-1][0]), _quote(alias))


def get_alias_from_query(from_query):
    """ :param string from_query: is something like :
        - '"res_partner"' OR
        - '"res_partner" as "res_users__partner_id"''
    """
    from_splitted = from_query.split(' as ')
    if len(from_splitted) > 1:
        return from_splitted[0].replace('"', ''), from_splitted[1].replace('"', '')
    else:
        return from_splitted[0].replace('"', ''), from_splitted[0].replace('"', '')

*/

// 格式化 操作符 统一使用 字母in,not in 或者字符 "=", "!="
// 确保 操作符为小写
//""" Change a term's operator to some canonical form, simplifying later processing. """
func normalize_leaf(leaf *utils.TStringList) *utils.TStringList {
	if !is_leaf(leaf) {
		return leaf
	}

	original := strings.ToLower(leaf.String(1))
	operator := original
	if operator == "<>" {
		operator = "!="
	}
	if utils.IsBoolStr(leaf.String(2)) && utils.InStrings(operator, "in", "not in") != -1 {
		//   _logger.warning("The domain term '%s' should use the '=' or '!=' operator." % ((left, original, right),))
		if operator == "in" {
			operator = "="
		} else {
			operator = "!="
		}
	}
	if leaf.Item(2).IsList() && utils.InStrings(operator, "=", "!=") != -1 {
		//  _logger.warning("The domain term '%s' should use the 'in' or 'not in' operator." % ((left, original, right),))
		if operator == "=" {
			operator = "in"
		} else {
			operator = "not in"
		}
	}

	leaf.Item(1).SetText(operator)
	return leaf
}

//    """AND([D1,D2,...]) returns a domain representing D1 and D2 and ... """
func AND(domains ...string) *utils.TStringList {
	return combine(AND_OPERATOR, TRUE_DOMAIN, FALSE_DOMAIN, domains)
}

//    """OR([D1,D2,...]) returns a domain representing D1 or D2 or ... """
func OR(domains ...string) *utils.TStringList {
	return combine(OR_OPERATOR, FALSE_DOMAIN, TRUE_DOMAIN, domains)
}

/*Returns a new domain expression where all domain components from ``domains``
  have been added together using the binary operator ``operator``. The given
  domains must be normalized.

  :param unit: the identity element of the domains "set" with regard to the operation
               performed by ``operator``, i.e the domain component ``i`` which, when
               combined with any domain ``x`` via ``operator``, yields ``x``.
               E.g. [(1,'=',1)] is the typical unit for AND_OPERATOR: adding it
               to any domain component gives the same domain.
  :param zero: the absorbing element of the domains "set" with regard to the operation
               performed by ``operator``, i.e the domain component ``z`` which, when
               combined with any domain ``x`` via ``operator``, yields ``z``.
               E.g. [(1,'=',1)] is the typical zero for OR_OPERATOR: as soon as
               you see it in a domain component the resulting domain is the zero.
  :param domains: a list of normalized domains.
*/
func combine(operator, unit, zero string, domains []string) (result *utils.TStringList) {
	count := 0
	for _, domain := range domains {
		if domain == unit {
			continue
		}
		if domain == zero {
			return utils.NewStringList(zero)
		}
		if domain != "" {
			result.PushString(domain)
			count++
		}
	}

	//result = [operator] * (count - 1) + result
	for count == 1 { //(count - 1)
		result.Insert(0, operator)

	}

	return
}

/*""" Test whether an object is a valid domain term:
    - is a list or tuple
    - with 3 elements
    - second element if a valid op

    :param tuple element: a leaf in form (left, operator, right)
    :param boolean internal: allow or not the 'inselect' internal operator
        in the term. This should be always left to False.

    Note: OLD TODO change the share wizard to use this function.
"""*/ /*
func is_leaf(element *utils.TStringList, internal ...bool) bool {
	INTERNAL_OPS := append(TERM_OPERATORS, "<>")
	if internal != nil && internal[0] {
		INTERNAL_OPS = append(INTERNAL_OPS, "inselect")
		INTERNAL_OPS = append(INTERNAL_OPS, "not inselect")
	}

	//??? 出现过==Nil还是继续执行之下的代码
	return (element != nil && element.IsList()) &&
		(element.Len() == 3) &&
		utils.InStrings(element.String(1), INTERNAL_OPS...) != -1 ||
		utils.InStrings(element.String(), TRUE_LEAF, FALSE_LEAF) != -1

	/*
	   def is_leaf(element, internal=False):

	       INTERNAL_OPS = TERM_OPERATORS + ('<>',)
	       if internal:
	           INTERNAL_OPS += ('inselect', 'not inselect')
	       return (isinstance(element, tuple) or isinstance(element, list)) \
	           and len(element) == 3 \
	           and element[1] in INTERNAL_OPS \
	           and ((isinstance(element[0], basestring) and element[0]) or tuple(element) in (TRUE_LEAF, FALSE_LEAF))
*/
//}

/*
# --------------------------------------------------
# Generic domain manipulation
# --------------------------------------------------
 """Returns a normalized version of ``domain_expr``, where all implicit '&' operators
    have been made explicit. One property of normalized domain expressions is that they
    can be easily combined together as if they were single domain components.
 """
*/
func normalize_domain(domain *utils.TStringList) (result *utils.TStringList) {
	//lLst := utils.Domain2StringList(domain)
	if domain == nil {
		logger.Err("Invaild domain")
		return Query2StringList(TRUE_DOMAIN)
	}

	// must be including Terms
	if !domain.IsList() {
		logger.Err("Domains to normalize must have a 'domain' form: a list or tuple of domain components")
		//return // TODO 考虑是否直接返回？？？
		return Query2StringList(TRUE_DOMAIN)
	}

	op_arity := map[string]int{
		NOT_OPERATOR: 1,
		AND_OPERATOR: 2,
		OR_OPERATOR:  2,
	}

	result = utils.NewStringList()
	var expected int = 1
	for _, item := range domain.Items() {
		if expected == 0 { // more than expected, like in [A, B]
			result.Insert(0, AND_OPERATOR) //put an extra '&' in front
			expected = 1
		}

		result.Push(item) //添加

		if item.IsList() { //domain term
			expected -= 1
		} else {
			//logger.Dbg("op_arity", op_arity[item.Text])
			// 如果不是Term而是操作符
			expected += op_arity[item.Text] - 1
		}
	}

	// 错误提示
	if expected != 0 {
		logger.Errf("This domain is syntactically not correct: %s", StringList2Domain(domain))
	}

	// 格式化List 生成Text
	//result.Update()
	//logger.Err("normalize_domain", StringList2Domain(result))
	return result
}

// 操作符相反
/*    """ Distribute any '!' domain operators found inside a normalized domain.
    Because we don't use SQL semantic for processing a 'left not in right'
    query (i.e. our 'not in' is not simply translated to a SQL 'not in'),
    it means that a '! left in right' can not be simply processed
    by __leaf_to_sql by first emitting code for 'left in right' then wrapping
    the result with 'not (...)', as it would result in a 'not in' at the SQL
    level.

    This function is thus responsible for pushing any '!' domain operators
    inside the terms themselves. For example::

         ['!','&',('user_id','=',4),('partner_id','in',[1,2])]
            will be turned into:
         ['|',('user_id','!=',4),('partner_id','not in',[1,2])]

"""*/
func negate(leaf *utils.TStringList) (node *utils.TStringList) {
	mapping := map[string]string{
		"<":  ">=",
		">":  "<=",
		"<=": ">",
		">=": "<",
		"=":  "!=",
		"!=": "=",
	}

	var operator string
	if leaf.Count() == 3 && leaf.IsList() {
		operator = leaf.String(1)
	}

	// change to "not in"
	if utils.InStrings(operator, "in", "like", "ilike") != -1 {
		leaf.SetText("not "+operator, 1)
		return leaf
	}

	// change to "in"
	if utils.InStrings(operator, "not in", "not like", "not ilike") != -1 {
		leaf.SetText(operator[4:], 1)
		return leaf
	}

	// replace operator from mapping
	if op, ok := mapping[operator]; ok {
		leaf.SetText(op, 1)
		return leaf
	}

	// 注意是解析的utils.Query2StringList 不是NewStringlist
	lLeaf := Query2StringList(fmt.Sprintf(
		"[%s, ('%s', '%s', '%s')]",
		NOT_OPERATOR,
		leaf.String(0),
		leaf.String(1),
		leaf.String(2),
	))

	return lLeaf
}

/*
   """Negate the domain ``subtree`` rooted at domain[0],
   leaving the rest of the domain intact, and return
   (negated_subtree, untouched_domain_rest)

 	['!','&',('user_id','=',4),('partner_id','in',[1,2])]
            will be turned into:
    ['|',('user_id','!=',4),('partner_id','not in',[1,2])]

   """*/
// 操作符 为否
func distribute_negate(domain *utils.TStringList) (*utils.TStringList, *utils.TStringList) {
	// STEP: 当Domain 第一个是Term时-->
	if is_leaf(domain.Item(0)) {
		//logger.Dbg("distribute_negate is_leaf", domain.Clone(1, -1))
		return negate(domain.Item(0)),
			domain.Clone(1, -1)
	}

	var (
		lLst, done1, todo1, done2, todo2 *utils.TStringList
	)
	// STEP:当Domain 第一个是&时-->
	if domain.String(0) == AND_OPERATOR {
		//logger.Dbg("distribute_negate", AND_OPERATOR)
		done1, todo1 = distribute_negate(domain.Clone(1, -1))
		done2, todo2 = distribute_negate(todo1)
		//logger.Dbg("distribute_negate", done1.String(), done2.String())

		lLst := utils.NewStringList()
		lLst.Push(utils.NewStringList(OR_OPERATOR), done1, done2)
		return lLst, todo2
		//     return [OR_OPERATOR] + done1 + done2, todo2
	}

	// STEP:当Domain 第一个是|时-->
	if domain.String(0) == OR_OPERATOR {
		done1, todo1 = distribute_negate(domain.Clone(1, -1))
		done2, todo2 = distribute_negate(todo1)

		lLst = utils.NewStringList()
		lLst.Push(utils.NewStringList(AND_OPERATOR), done1, done2)
		return lLst, todo2
	}

	return nil, nil
}

// From a leaf, create a new leaf (based on the new_elements tuple
// and new_model), that will have the same join context. Used to
// insert equivalent leafs in the processing stack. """
func create_substitution_leaf(leaf *TExtendedLeaf, new_elements *utils.TStringList, new_model *TModel, internal bool) *TExtendedLeaf {
	if new_model == nil {
		new_model = leaf.model
	}
	var new_join_context []TJoinContext
	new_join_context = leaf.join_context //复制
	return NewExtendedLeaf(new_elements, new_model, new_join_context, internal)
}

/*" Distribute any '!' domain operators found inside a normalized domain.

  Because we don't use SQL semantic for processing a 'left not in right'
  query (i.e. our 'not in' is not simply translated to a SQL 'not in'),
  it means that a '! left in right' can not be simply processed
  by __leaf_to_sql by first emitting code for 'left in right' then wrapping
  the result with 'not (...)', as it would result in a 'not in' at the SQL
  level.

  This function is thus responsible for pushing any '!' domain operators
  inside the terms themselves. For example::

       ['!','&',('user_id','=',4),('partner_id','in',[1,2])]
          will be turned into:
       ['|',('user_id','!=',4),('partner_id','not in',[1,2])]

  "*/
func distribute_not(domain *utils.TStringList) (result *utils.TStringList) {
	if domain == nil {
		return utils.NewStringList() //返回空白确保循环不会出现==nil
	}

	stack := utils.NewStringList()
	stack.PushString("false")
	result = utils.NewStringList()

	for _, item := range domain.Items() {
		is_negate := false
		negate := stack.Pop()
		if negate != nil {
			is_negate = utils.StrToBool(negate.String())
		}

		// # negate tells whether the subdomain starting with token must be negated
		if is_leaf(item) {
			if is_negate {
				left, operator, right := item.String(0), item.String(1), item.String(2)
				if _, has := TERM_OPERATORS_NEGATION[operator]; has {
					result.PushString(left, TERM_OPERATORS_NEGATION[operator], right)
				} else {
					result.PushString(NOT_OPERATOR)
					result.Push(item)
				}
			} else {
				result.Push(item)
			}
		} else if item.Text == NOT_OPERATOR {
			stack.PushString(utils.BoolToStr(!is_negate))
		} else if _, has := DOMAIN_OPERATORS_NEGATION[item.String()]; has {
			if is_negate {
				result.PushString(DOMAIN_OPERATORS_NEGATION[item.String()])
			} else {
				result.Push(item)
			}
			stack.PushString(utils.BoolToStr(is_negate))
			stack.PushString(utils.BoolToStr(is_negate))
		} else {
			result.Push(item)
		}

	}

	/*
		if domain.String(0) != NOT_OPERATOR {
			lFst := domain.Item(0)
			result.Push(lFst)
			result.Push(distribute_not(domain.Clone(1, -1)).Items()...)

			return //result.Push(lFst, distribute_not(domain.Clone(1, -1)).Items()...)
		} else {
			//logger.Dbg("todo Clone next", domain.Clone(1, -1).String(0))
			done, todo := distribute_negate(domain.Clone(1, -1))
			result.Push(done.Items()...)

			// 循环遍历下一个
			if todo != nil {
				//logger.Dbg("todo ", todo.String(0))
				result.Push(distribute_not(todo).Items()...)
			}

			return //result.Push(done, distribute_not(todo))
		}

		return nil*/
	return
}

/* Generate a standard table alias name. An alias is generated as following:
   - the base is the source table name (that can already be an alias)
   - then, each joined table is added in the alias using a 'link field name'
     that is used to render unique aliases for a given path
   - returns a tuple composed of the alias, and the full table alias to be
     added in a from condition with quoting done
   Examples:
   - src_table_alias='res_users', join_tables=[]:
       alias = ('res_users','"res_users"')
   - src_model='res_users', join_tables=[(res.partner, 'parent_id')]
       alias = ('res_users__parent_id', '"res_partner" as "res_users__parent_id"')

   :param model src_table_alias: model source of the alias
   :param list joined_tables: list of tuples
                              (dst_model, link_field)

   :return tuple: (table_alias, alias statement for from clause with quotes added)
*/
// 生成Joint用的表别名
func generate_table_alias(src_table_alias string, joined_tables [][]string) (string, string) {

	alias := src_table_alias
	if joined_tables == nil {
		return alias, _quote(alias)
	}

	for _, link := range joined_tables {
		alias = alias + "__" + link[1]
	}

	if len(alias) > 64 {
		logger.Warnf("Table alias name %s is longer than the 64 characters size accepted by default in postgresql.", alias)
	}

	return alias, fmt.Sprintf("%s as %s", _quote(joined_tables[0][0]), _quote(alias))
}

// :param string from_query: is something like :
//  - '"res_partner"' OR
//  - '"res_partner" as "res_users__partner_id"''
// from_query: 表名有关的字符串
func get_alias_from_query(from_query string) (string, string) {
	from_splitted := strings.Split(from_query, " as ")
	if len(from_splitted) > 1 {
		return strings.Replace(from_splitted[0], `"`, "", -1), strings.Replace(from_splitted[1], `"`, "", -1)
	} else {
		return strings.Replace(from_splitted[0], `"`, "", -1), strings.Replace(from_splitted[0], `"`, "", -1)
	}
}

func (self *TExpression) recursive_children(ids *utils.TStringList, model *TModel, parent_field string, context map[string]interface{}) (result *utils.TStringList) {
	result = utils.NewStringList()
	if ids == nil {
		return result
	}
	//lRec := model.Search(fmt.Sprintf("[('%s', 'in', '%s')]", parent_field, ids), 0, 0, "", false, context)
	//lRec := model.SearchRead(fmt.Sprintf("[('%s', 'in', '%s')]", parent_field, ids), nil, 0, 0, "", context)
	lRec, _ := model.Records().Domain(fmt.Sprintf("[('%s', 'in', '%s')]", parent_field, ids.Flatten())).Read()
	ids2 := utils.NewStringList()
	for _, key := range lRec.Keys() {
		ids2.PushString(key)
	}
	result.Push(ids.Items()...)
	result.Push(self.recursive_children(ids2, model, parent_field, context).Items()...)
	return // ids + recursive_children(ids2, model, parent_field)
}

// Return a domain implementing the child_of operator for [(left,child_of,ids)],
// either as a range using the parent_left/right tree lookup fields
// (when available), or as an expanded [(left,in,child_ids)]
func (self *TExpression) child_of_domain(left *utils.TStringList, ids *utils.TStringList, left_model *TModel, parent string, prefix string, context map[string]interface{}) (result *utils.TStringList) {
	result = utils.NewStringList()
	if left_model._parent_store {
		/*    if left_model._parent_store and (not left_model.pool._init):
		      # TODO: Improve where joins are implemented for many with '.', replace by:
		      # doms += ['&',(prefix+'.parent_left','<',o.parent_right),(prefix+'.parent_left','>=',o.parent_left)]
		      doms = []
		      for o in left_model.browse(cr, uid, ids, context=context):
		          if doms:
		              doms.insert(0, OR_OPERATOR)
		          doms += [AND_OPERATOR, ('parent_left', '<', o.parent_right), ('parent_left', '>=', o.parent_left)]
		      if prefix:
		          return [(left, 'in', left_model.search(cr, uid, doms, context=context))]
		      return doms
		  else:*/

	} else {
		result.Push(left)
		result.PushString("in")
		result.Push(self.recursive_children(ids, left_model, parent, context))
		return
	}

	return

}

// Pop a leaf to process.
func (self *TExpression) pop() (eleaf *TExtendedLeaf) {
	cnt := len(self.stack)
	if cnt == 0 {
		return
	}
	eleaf = self.stack[cnt-1]
	self.stack = self.stack[:cnt-1]
	return
}

// Push a leaf to be processed right after.
func (self *TExpression) push(eleaf *TExtendedLeaf) {
	self.stack = append(self.stack, eleaf)
}

//反转
func (self *TExpression) reverse(lst []*TExtendedLeaf) {
	var lTmp []*TExtendedLeaf
	lCnt := len(lst)
	for i := lCnt - 1; i >= 0; i-- {
		lTmp = append(lTmp, lst[i])
	}
	copy(lst, lTmp)
}

// Push a leaf to the results. This leaf has been fully processed and validated.
func (self *TExpression) push_result(leaf *TExtendedLeaf) {
	self.result = append(self.result, leaf)
}

// Normalize a single id or name, or a list of those, into a list of ids
// :param {int,long,basestring,list,tuple} value:
//  if int, long -> return [value]
// if basestring, convert it into a list of basestrings, then
//  if list of basestring ->
//   perform a name_search on comodel for each name
//       return the list of related ids
// 获得Ids
func (self *TExpression) to_ids(value *utils.TStringList, comodel *TModel, context map[string]interface{}, limit int) *utils.TStringList {
	var names *utils.TStringList

	// 如果是数字
	if value.IsBaseInt() {
		return value //strings.Join(value.Strings(), ",")
	} else if value != nil && value.IsList() && value.IsBaseString() {
		names = value // 如果传入的是字符则可能是名称
	}

	// 查询名称并传回ID
	if names != nil {
		var name_get_list []string // 存放IDs
		//  name_get_list = [name_get[0] for name in names for name_get in comodel.name_search(cr, uid, name, [], 'ilike', context=context, limit=limit)]
		//for _, name := range names.Items() {
		lRecords := comodel.SearchName(strings.Join(value.Strings(), ","), "", "ilike", limit, "", context)
		for _, rec := range lRecords.Data {
			name_get_list = append(name_get_list, rec.FieldByName("id").AsString()) //ODO: id 可能是Rec_id
		}
		//}

		result := utils.NewStringList()

		result.PushString(name_get_list...)
		return result //strings.Join(name_get_list, ",") // 合并为  1,2,3
	}

	return value
}

/*"" Transform the leaves of the expression

    The principle is to pop elements from a leaf stack one at a time.
    Each leaf is processed. The processing is a if/elif list of various
    cases that appear in the leafs (many2one, function fields, ...).
    Two things can happen as a processing result:
    - the leaf has been modified and/or new leafs have to be introduced
      in the expression; they are pushed into the leaf stack, to be
      processed right after
    - the leaf is added to the result

    Some internal var explanation:
        :var list path: left operand seen as a sequence of field names
            ("foo.bar" -> ["foo", "bar"])
        :var obj model: model object, model containing the field
            (the name provided in the left operand)
        :var obj field: the field corresponding to `path[0]`
        :var obj column: the column corresponding to `path[0]`
        :var obj comodel: relational model of field (field.comodel)
            (res_partner.bank_ids -> res.partner.bank)
"""*/
// @转换提取Model和Filed ("foo.bar" -> ["foo", "bar"])
func (self *TExpression) parse(context map[string]interface{}) {
	var (
		lExLeaf               *TExtendedLeaf
		left, operator, right *utils.TStringList
		lPath                 []string
		comodel               IModel
		err                   error
	)

	for _, leaf := range self.Expression.Items() {
		self.stack = append(self.stack, NewExtendedLeaf(leaf, self.root_model, nil, false))
	}

	// process from right to left; expression is from left to right
	self.reverse(self.stack)
	for len(self.stack) > 0 {
		lExLeaf = self.pop() // Get the next leaf to process

		// 获取各参数 # Get working variables
		if lExLeaf.is_operator() {
			left = lExLeaf.leaf
			operator = nil
			right = nil
		} else if lExLeaf.is_true_leaf() || lExLeaf.is_false_leaf() {
			left = lExLeaf.leaf.Item(0)     // 1      TRUE_LEAF  = "(1, '=', 1)"
			operator = lExLeaf.leaf.Item(1) // =
			right = lExLeaf.leaf.Item(2)    // 1
		} else {
			left = lExLeaf.leaf.Item(0)
			operator = lExLeaf.leaf.Item(1)
			right = lExLeaf.leaf.Item(2)
		}

		// :var list path: left operand seen as a sequence of field names
		lPath = strings.SplitN(left.String(), ".", 2) // "foo.bar" -> ["foo", "bar"]
		model := lExLeaf.model                        // get the model instance
		lFieldName := lPath[0]                        // get the   first part
		IsInheritField := model.RelateFieldByName(lFieldName) != nil
		//_, IsInheritField := model._relate_fields[lFieldName] // 是否是继承字段
		//column := model._Columns[lPath[0]]
		//   comodel = model.pool.get(getattr(field, 'comodel_name', None))

		// get the model
		field := model.FieldByName(lFieldName) // get the field instance which has full details
		if field != nil {
			comodel, err = model.Orm().GetModel(field.ModelName()) // get the model of the field owner
			if err != nil {
				logger.Err(err)
			}
		}

		//logger.Dbg("Leaf>", lPath, model, field, IsInheritField)
		// ########################################
		// 			解析修改leaf 兼容字段
		// ########################################

		// ----------------------------------------
		// SIMPLE CASE
		// 1. leaf is an operator
		// 2. leaf is a true/false leaf
		// -> add directly to result
		// ----------------------------------------
		// 对操[作符/True/False]的条件直接添加，无需转换
		if lExLeaf.is_operator() || lExLeaf.is_true_leaf() || lExLeaf.is_false_leaf() {
			self.push_result(lExLeaf)

			/*
			   # ----------------------------------------
			   # FIELD NOT FOUND
			   # -> from inherits'd fields -> work on the related model, and add
			   #    a join condition
			   # -> ('id', 'child_of', '..') -> use a 'to_ids'
			   # -> but is one on the _log_access special fields, add directly to
			   #    result
			   #    TODO: make these fields explicitly available in self.columns instead!
			   # -> else: crash
			   # ----------------------------------------
			*/
		} else if field == nil && !IsInheritField {
			// FIELD NOT FOUND
			//panic(logger.Err("Invalid field %r in leaf %r", left.String(), lExLeaf.leaf.String()))
			logger.Errf("Invalid field %s in leaf %s", left.String(), lExLeaf.leaf.String())

			//} else if (field == nil || (field != nil && field.IsForeignField())) && IsInheritField {
		} else if IsInheritField {
			// ----------------------------------------
			// FIELD NOT FOUND
			// -> from inherits'd fields -> work on the related model, and add
			///    a join condition
			// -> ('id', 'child_of', '..') -> use a 'to_ids'
			// -> but is one on the _log_access special fields, add directly to
			//    result
			//    TODO: make these fields explicitly available in self.columns instead!
			// -> else: crash
			// ----------------------------------------
			//if field != nil && field.IsForeignField() && IsInheritField {

			//# comments about inherits'd fields
			//#  { 'field_name': ('parent_model', 'm2o_field_to_reach_parent',
			//#                    field_column_obj, origina_parent_model), ... }

			// next_model = model.pool[model._inherit_fields[path[0]][0]]
			//lExLeaf.add_join_context(next_model, model._inherits[next_model._name], 'id', model._inherits[next_model._name])

			lRefFld := model.RelateFieldByName(lFieldName)
			next_model, err := model.orm.GetModel(lRefFld.RelateTableName)
			if err != nil {
				logger.Err(err)
			}
			//logger.Dbg("IsForeignField>>", lFieldName, next_model.GetModelName())
			lExLeaf.add_join_context(next_model.GetBase(), model._relations[next_model.GetModelName()], "id", model._relations[next_model.GetModelName()])
			self.push(lExLeaf)

		} else if left.String() == "id" && utils.InStrings(operator.String(), "child_of", "parent_of") != -1 {
			//--------------------------
			// TODO id 必须改为动态

			ids2 := self.to_ids(right, model, context, 0)
			var dom *utils.TStringList
			if operator.String() == "child_of" {
				dom = self.child_of_domain(left, ids2, model, "", "", nil)
			} else if operator.String() == "parent_of" {

			}
			dom = dom.Reversed()
			for _, dom_leaf := range dom.Items() {
				new_leaf := create_substitution_leaf(lExLeaf, dom_leaf, model, false)
				self.push(new_leaf)
			}

		} else if field != nil && utils.InStrings(lPath[0], MAGIC_COLUMNS...) != -1 {
			self.push_result(lExLeaf)

		} else if len(lPath) > 1 && !field.IsForeignField() && field.Type() == "many2one" && field.IsAutoJoin() {
			/* # ----------------------------------------
			   # PATH SPOTTED
			   # -> many2one or one2many with IsAutoJoin():
			   #    - add a join, then jump into linked column: column.remaining on
			   #      src_table is replaced by remaining on dst_table, and set for re-evaluation
			   #    - if a domain is defined on the column, add it into evaluation
			   #      on the relational table
			   # -> many2one, many2many, one2many: replace by an equivalent computed
			   #    domain, given by recursively searching on the remaining of the path
			   # -> note: hack about columns.property should not be necessary anymore
			   #    as after transforming the column, it will go through this loop once again
			   # ----------------------------------------*/

			//logger.Dbg(`if len(lPath) > 1 &&field.Type="many2one" && field.IsAutoJoin()`)
			// # res_partner.state_id = res_partner__state_id.id
			lExLeaf.add_join_context(comodel.GetBase(), lFieldName, "id", lFieldName)
			self.push(create_substitution_leaf(lExLeaf, utils.NewStringList(lPath[1], operator.String(), right.String()), comodel.GetBase(), false))

		} else if len(lPath) > 1 && field.Store() && !field.IsForeignField() && field.Type() == "one2many" && field.IsAutoJoin() {
			//logger.Dbg(`if len(lPath) > 1 &&field.Type="many2one" && field.IsAutoJoin()`)
			//  # res_partner.id = res_partner__bank_ids.partner_id
			lExLeaf.add_join_context(comodel.GetBase(), "id", field.FieldsId(), lFieldName)
			domain := Query2StringList(field.Domain()) //column._domain(model) if callable(column._domain) else column._domain
			self.push(create_substitution_leaf(lExLeaf, utils.NewStringList(lPath[1], operator.String(), right.String()), comodel.GetBase(), false))
			if domain != nil {
				domain = normalize_domain(domain)
				domain = domain.Reversed()
				for _, elem := range domain.Items() {
					self.push(create_substitution_leaf(lExLeaf, elem, comodel.GetBase(), false))
				}
				self.push(create_substitution_leaf(lExLeaf, Query2StringList(AND_OPERATOR), comodel.GetBase(), false))
			}

		} else if len(lPath) > 1 && field.Store() && !field.IsForeignField() && field.IsAutoJoin() {
			logger.Errf("_auto_join attribute not supported on many2many column %s", left.String())

		} else if len(lPath) > 1 && field.Store() && !field.IsForeignField() && field.Type() == "many2one" {
			//logger.Dbg(`if len(path) > 1 && field.Type == 'many2one'`)
			lDomain := fmt.Sprintf(`[('%s', '%s', '%s')]`, lPath[1], operator.String(), right.String())
			lDs, _ := comodel.Records().Domain(lDomain).Read() //search(cr, uid, [(path[1], operator, right)], context=dict(context, active_test=False))
			right_ids := lDs.Keys()
			lExLeaf.leaf = utils.NewStringList()
			lExLeaf.leaf.PushString(lPath[0], "in")
			lExLeaf.leaf.PushString(right_ids...) //    leaf.leaf = (path[0], 'in', right_ids)
			self.push(lExLeaf)

		} else if len(lPath) > 1 && field.Store() && !field.IsForeignField() && utils.InStrings(field.Type(), "many2many", "one2many") != -1 {
			// Making search easier when there is a left operand as column.o2m or column.m2m
			lDomain := fmt.Sprintf(`[('%s', '%s', '%s')]`, lPath[1], operator.String(), right.String())
			lDs, _ := comodel.Records().Domain(lDomain).Read()
			right_ids := lDs.Keys()

			lDomain = fmt.Sprintf(`[('%s', 'in', [%s])]`, lPath[0], strings.Join(right_ids, ","))
			lDs, _ = model.Records().Domain(lDomain).Read()
			table_ids := lDs.Keys()
			lExLeaf.leaf = utils.NewStringList()
			lExLeaf.leaf.PushString("id", "in")
			lExLeaf.leaf.PushString(table_ids...) //    leaf.leaf = (path[0], 'in', right_ids)
			self.push(lExLeaf)

		} else if !field.Store() && field == nil || field.IsForeignField() {
			//# Non-stored field should provide an implementation of search.
			var domain *utils.TStringList
			if !field.Search() {
				//# field does not support search!
				logger.Errf("Non-stored field %s cannot be searched.", field.Name)
				// if _logger.isEnabledFor(logging.DEBUG):
				//     _logger.debug(''.join(traceback.format_stack()))
				//# Ignore it: generate a dummy leaf.
				domain = utils.NewStringList()
			} else {
				//# Let the field generate a domain.
				if len(lPath) > 1 {
					operator.SetText("in")
					lDomain := fmt.Sprintf(`[('%s', '%s', '%s')]`, lPath[1], operator.String(), right.String())
					lDs, _ := comodel.Records().Domain(lDomain).Read()
					right.Clear()
					right.PushString(lDs.Keys()...)
				}

				//	TODO 以下代码为翻译
				//recs = model.browse(cr, uid, [], context=context)
				//domain = field.determine_domain(recs, operator, right)
			}

			if domain == nil {
				lExLeaf.leaf = Query2StringList(TRUE_LEAF)
				self.push(lExLeaf)
			} else {
				domain = domain.Reversed()
				for _, elem := range domain.Items() {
					self.push(create_substitution_leaf(lExLeaf, elem, model, true))
				}
			}
			//} else if field.IsFuncField() && !field.Store { // isinstance(column, fields.function) and not column.store

		} else if field.Type() == "one2many" && (operator.String() == "child_of" || operator.String() == "parent_of") {
			// -------------------------------------------------
			// RELATIONAL FIELDS
			// -------------------------------------------------

		} else if field.Type() == "one2many" {
		} else if field.Type() == "many2many" {

		} else if field.Type() == "many2one" {

		} else if field.Type() == "binary" && field.(*TBinField).attachment {
		} else {
			// -------------------------------------------------
			// OTHER FIELDS
			// -> datetime fields: manage time part of the datetime
			//    column when it is not there
			// -> manage translatable fields
			// -------------------------------------------------

			if field.Type() == "datetime" && right != nil && right.Count() == 10 {
				if operator.In(">", "<=") {
					//  right += ' 23:59:59'
				} else {
					//  right += ' 00:00:00'
				}
				ltemp := utils.NewStringList()
				ltemp.Push(left, operator, right)
				self.push(create_substitution_leaf(lExLeaf, ltemp, model, false))

			} else if field.Translatable() && right != nil { //column.translate and not callable(column.translate) and right:

			} else {
				//logger.Dbg(`_push_result`)
				self.push_result(lExLeaf)
			}
		}

	} // end of for
	// ----------------------------------------
	// END OF PARSING FULL DOMAIN
	// -> generate joins
	// ----------------------------------------
	var joins, lLst []string
	joins = make([]string, 0) // utils.NewStringList()
	for _, eleaf := range self.result {
		lLst = eleaf.get_join_conditions()
		//for _, lst := range lLst {
		//	joins.Union(lst)
		//}
		joins = append(joins, lLst...)
	}

	self.joins = joins
}

// 用leaf生成SQL
// 将值用占位符? 区分出来
func (self *TExpression) leaf_to_sql(eleaf *TExtendedLeaf, params []interface{}) (res_query string, res_params []interface{}, res_arg []interface{}) {
	var (
		vals              []interface{} // 该Term 的值 每个Or,And等条件都有自己相当数量的值
		lIsHolder         bool          = false
		first_right_value string        // 提供最终值以供条件判断
	)
	res_params = make([]interface{}, 0) //utils.NewStringList()
	model := eleaf.model
	leaf := eleaf.leaf

	left := leaf.Item(0)
	operator := leaf.Item(1)
	right := leaf.Item(2)

	// 是否是model字段
	lField := model.FieldByName(left.String())
	lIsField := lField != nil

	//logger.Dbg("_leaf_to_sql", lIsField, left.String(), operator.String(), right.String(), right.IsList())

	// 重新检查合法性 不行final sanity checks - should never fail
	if utils.InStrings(operator.String(), append(TERM_OPERATORS, "inselect", "not inselect")...) == -1 {
		logger.Warnf(`Invalid operator %s in domain term %s`, operator.Strings(), leaf.String())
	}

	if !(left.In(TRUE_LEAF, FALSE_LEAF) || model.FieldByName(left.String()) != nil || left.In(MAGIC_COLUMNS)) { //
		logger.Warnf(`Invalid field %s in domain term %s`, left.Strings(), leaf.String())
	}
	//        assert not isinstance(right, BaseModel), \
	//            "Invalid value %r in domain term %r" % (right, leaf)

	table_alias := eleaf.generate_alias()

	// 检测查询是否占位符?并获取值
	first_right_value = right.String(0)
	if utils.HasStrings(right.String(), "?", "%s") != -1 {
		lIsHolder = true
		if len(params) > 0 {
			first_right_value = utils.Itf2Str(params[0])
			len := utils.MaxInt(1, right.Count()-1)

			//logger.Dbg("getttttttt", len, params, params[0:len], params[len:], right.Count(), right.String())
			vals = params[0:len]
			res_arg = params[len:] // 修改params值留到下个Term 返回
		}
		//res_params = append(res_params, lVal)
	}

	if leaf.String() == TRUE_LEAF {
		res_query = "TRUE"
		res_params = nil

	} else if leaf.String() == FALSE_LEAF {
		res_query = "FALSE"
		res_params = nil

	} else if operator.String() == "inselect" { // in(val,val)
		if lIsHolder {
			res_params = append(res_params, first_right_value)
		} else {
			res_params = append(res_params, right.String(1)) //right.Item(1)
		}
		res_query = fmt.Sprintf(`(%s."%s" in (?))`, table_alias, left.String())

	} else if operator.String() == "not inselect" {
		if lIsHolder {
			res_params = append(res_params, first_right_value)
		} else {
			res_params = append(res_params, right.String(1)) //right.Item(1)
		}
		res_query = fmt.Sprintf(`%s."%s" not in (?))`, table_alias, left.String())

	} else if operator.In("in", "not in") { //# 数组值
		if right.IsList() {
			if lIsHolder {
				res_params = append(res_params, vals...)
			} else {
				res_params = append(res_params, utils.Strs2Itfs(right.Flatten())...) // right
			}

			check_nulls := false
			//logger.Dbg("right.IsList ", lIsHolder, res_params)
			for idx, item := range res_params {
				//if utils.IsBool(item) && utils.Itf2Bool(item) == false {
				if utils.IsBoolItf(item) && utils.Itf2Bool(item) == false {
					check_nulls = true
					//logger.Dbg("check_nulls", true)
					//res_params.Remove(idx)
					res_params = utils.SlicRemove(res_params, idx)
				}
			}
			//logger.Dbg("res_params", res_params)
			// In 值操作
			if res_params != nil {
				instr := ""
				if left.String() == "id" {
					//instr = strings.Join(utils.Repeat("%s", len(res_params)), ",") // 数字不需要冒号[1,2,3]
					instr = strings.Join(utils.Repeat("?", len(res_params)), ",")
					//logger.Dbg("instr", instr, res_params)
				} else {
					// 获得字段Fortmat格式符 %s,%d 等
					//ss := model.FieldByName(left.String()).SymbolChar()
					ss := "?"
					// 等同于参数量重复打印格式符
					instr = strings.Join(utils.Repeat(ss, len(res_params)), ",") // 字符串需要冒号 ['1','2','3']
					// res_params = map(ss[1], res_params) // map(function, iterable, ...)
					//logger.Dbg("instr else", instr, res_params)
				}
				res_query = fmt.Sprintf(`(%s."%s" %s (%s))`, table_alias, left.String(), operator.String(), instr)
			} else {
				// The case for (left, 'in', []) or (left, 'not in', []).
				// 对于空值的语句
				if operator.String() == "in" {
					res_query = "FALSE"
				} else {
					res_query = "TRUE"
				}
			}

			if check_nulls && operator.String() == "in" {
				res_query = fmt.Sprintf(`(%s OR %s."%s" IS NULL)`, res_query, table_alias, left.String())

			} else if !check_nulls && operator.String() == "not in" {
				res_query = fmt.Sprintf(`(%s OR %s."%s" IS NULL)`, res_query, table_alias, left.String())

			} else if check_nulls && operator.String() == "not in" {
				res_query = fmt.Sprintf(`(%s AND %s."%s" IS NOT NULL)`, res_query, table_alias, left.String()) // needed only for TRUE.
			}
		} else { // Must not happen
			var is_bool_value bool
			if lIsHolder {
				is_bool_value = utils.Itf2Bool(vals[0])
			} else {
				is_bool_value = utils.IsBoolStr(right.String())
			}
			if is_bool_value {
				r := ""
				logger.Warnf(`The domain term "%s" should use the '=' or '!=' operator.`, leaf.String())
				if operator.String() == "in" {
					if utils.StrToBool(right.String()) {
						r = "NOT NULL"
					} else {
						r = "NULL"
					}
				} else {
					if utils.StrToBool(right.String()) {
						r = "NULL"
					} else {
						r = "NOT NULL"
					}
				}
				res_query = fmt.Sprintf(`(%s."%s" IS %s)`, table_alias, left.String(), r)
				res_params = nil
			}
			//  raise ValueError("Invalid domain term %r" % (leaf,))
		}
	} else if lIsField && (lField.Type() == FIELD_TYPE_BOOL) &&
		((operator.String() == "=" && strings.ToLower(first_right_value) == "false") || (operator.String() == "!=" && strings.ToLower(first_right_value) == "ture")) {
		// 字段是否Bool类型
		res_query = fmt.Sprintf(`(%s."%s" IS NULL or %s."%s" = false )`, table_alias, left.String(), table_alias, left.String())
		res_params = nil

	} else if (strings.ToLower(first_right_value) == "false" || strings.ToLower(first_right_value) == "") && (operator.String() == "=") {
		res_query = fmt.Sprintf(`%s."%s" IS NULL `, table_alias, left.String())
		res_params = nil

	} else if lIsField && lField.Type() == FIELD_TYPE_BOOL &&
		((operator.String() == "!=" && strings.ToLower(first_right_value) == "false") || (operator.String() == "==" && strings.ToLower(first_right_value) == "ture")) {
		res_query = fmt.Sprintf(`(%s."%s" IS NOT NULL and %s."%s" != false)`, table_alias, left.String(), table_alias, left.String())
		res_params = nil

	} else if (strings.ToLower(first_right_value) == "false" || strings.ToLower(first_right_value) == "") && (operator.String() == "!=") {
		res_query = fmt.Sprintf(`%s."%s" IS NOT NULL`, table_alias, left.String())
		res_params = nil

	} else if operator.String() == "=?" { // # Boolen 判断
		if strings.ToLower(first_right_value) == "false" || strings.ToLower(first_right_value) == "" {
			// '=?' is a short-circuit that makes the term TRUE if right is None or False
			res_query = "TRUE"
			res_params = nil
		} else {
			// '=?' behaves like '=' in other cases
			lDomain := Query2StringList(fmt.Sprintf(`[('%s','=','%s')]`, left.String(), right.String()))
			res_query, res_params, res_arg = self.leaf_to_sql(create_substitution_leaf(eleaf, lDomain, model, false), nil)
		}

	} else if left.String() == "id" { // TODO id字段必须和Orm 其他地方一致
		res_query = fmt.Sprintf("%s.id %s ?", table_alias, operator.String())
		if lIsHolder {
			res_params = append(res_params, vals...)
		} else {
			//logger.Dbg("ID###", right.String(), right.Flatten(), utils.Strs2Itfs(right.Flatten()))
			res_params = append(res_params, utils.Strs2Itfs(right.Flatten())...) //fright
		}

	} else {
		// 是否需要添加“%%”
		need_wildcard := operator.In("like", "ilike", "not like", "not ilike")

		// 兼容 =like 和 =ilike
		sql_operator := operator.String()
		if sql_operator == "=like" {
			sql_operator = "like"
		} else if sql_operator == "=ilike" {
			sql_operator = "ilike"
		}

		cast := ""
		if strings.HasSuffix(sql_operator, "like") { // # cast = '::text' if  sql_operator.endswith('like') else ''
			cast = "::text"
		}

		// #组合Sql
		if lIsField {
			//format = need_wildcard and '%s' or model._columns[left]._symbol_set[0]
			format := ""
			// 范查询
			if need_wildcard {
				//format = "'%s'" // %XX%号在参数里添加
				format = "?" // fmt.Sprintf(lField._symbol_c, "?") //lField.SymbolFunc("?") //
			} else {
				//format = lField.SymbolChar()
				format = "?" // fmt.Sprintf(lField._symbol_c, "?") //lField.SymbolFunc("?") //
			}

			//unaccent = self._unaccent if sql_operator.endswith('like') else lambda x: x
			column := fmt.Sprintf("%s.%s", table_alias, _quote(left.String()))
			res_query = fmt.Sprintf("(%s %s %s)", column+cast, sql_operator, format)
		} else if left.In(MAGIC_COLUMNS) {
			res_query = fmt.Sprintf("(%s.\"%s\"%s %s ?)", table_alias, left.String(), cast, sql_operator)
			if lIsHolder {
				res_params = append(res_params, vals...)
			} else {
				res_params = append(res_params, right.String()) // utils.NewStringList(right.String())
			}
		} else {
			//# Must not happen
			logger.Errf(`Invalid field %s in domain term %s`, left.String(), leaf.String())
		}

		add_null := false
		if need_wildcard {
			if lIsHolder {
				res_params = append(res_params, vals...)
			} else {
				res_params = append(res_params, fmt.Sprintf("%%%s%%", right.String())) //utils.NewStringList(lVal)
			}

			add_null = right.String() == ""
		} else if lIsField {
			if lIsHolder {
				res_params = append(res_params, vals...)
			} else {
				//res_params = utils.NewStringList(lField.SymbolFunc()(right.String())) //添加字符串参数
				res_params = append(res_params, lField.SymbolFunc()(right.String()))
			}
			//res_params = utils.NewStringList(lField.SymbolFunc()(lVal)) //添加字符串参数

		}

		if add_null {
			res_query = fmt.Sprintf(`(%s OR %s."%s" IS NULL)`, res_query, table_alias, left.String())
		}
	}
	//logger.Dbg("toDQL:", res_query, res_params)
	return
}

// to generate the SQL expression and params
func (self *TExpression) ToSql(params ...interface{}) ([]string, []interface{}) {
	return self.to_sql(params...)
}

func (self *TExpression) to_sql(params ...interface{}) ([]string, []interface{}) {
	var (
		stack = utils.NewStringList()
		//		lParams = utils.NewStringList()
		ops    map[string]string
		q1, q2 *utils.TStringList
		query  string
	)
	res_params := make([]interface{}, 0)

	// 翻转顺序以便递归生成
	// Process the domain from right to left, using a stack, to generate a SQL expression.
	self.reverse(self.result)
	params = utils.ReverseItfs(params...)

	// 遍历并生成
	for _, eleaf := range self.result {
		//logger.Dbg("self.result", eleaf.leaf.String(), eleaf.join_context, params)
		if eleaf.is_leaf(true) {
			q, p, en_p := self.leaf_to_sql(eleaf, params) //internal: allow or not the 'inselect' internal operator in the term. This should be always left to False.
			params = en_p
			res_params = utils.SlicInsert(res_params, 0, p...)
			stack.PushString(q)
			//logger.Dbg("is_leaf", q, p, res_params, stack.String())
		} else if eleaf.leaf.String() == NOT_OPERATOR {
			//logger.Dbg("NOT_OPERATOR", stack.Pop().String())
			stack.PushString("(NOT (%s))", stack.Pop().String())
		} else {
			ops = map[string]string{AND_OPERATOR: " AND ", OR_OPERATOR: " OR "}
			q1 = stack.Pop()
			q2 = stack.Pop()
			//logger.Dbg("q1,q2", q1 != nil, q2 != nil)
			if q1 != nil && q2 != nil {
				lStr := fmt.Sprintf("(%s %s %s)", q1.String(), ops[eleaf.leaf.String()], q2.String())
				//logger.Dbg("q1,q2 2", q1 != nil, q2 != nil, lStr, stack.Len(), stack.String())
				stack.PushString(lStr)
				//logger.Dbg("q1,q2 2", q1 != nil, q2 != nil, lStr, stack.Len(), stack.String())
			}
		}
	}

	//logger.Dbg("aaaa", stack.Count(), stack.String())
	// #上面Pop取出合并后应该为1
	if stack.Count() != 1 {
		res_params = nil
		logger.Errf("domain to sql error: stack.Len() %d", stack.Count())
	}

	query = stack.String(0)
	joins := strings.Join(self.joins, " AND ")
	if joins != "" {
		query = fmt.Sprintf("(%s) AND %s", joins, query)
	}
	//logger.Dbg("lParams.Flatten()", joins, res_params)
	return []string{query}, res_params //lParams.Flatten()
}

//""" Returns the list of tables for SQL queries, like select from ... """
func (self *TExpression) get_tables() (tables *utils.TStringList) {
	tables = utils.NewStringList()
	for _, leaf := range self.result {
		for _, table := range leaf.get_tables().Items() {
			if !tables.Has(table.String()) {
				tables.PushString(table.String())
			}
		}
	}

	table_name := _quote(self.root_model.GetTableName())
	if !tables.Has(table_name) {
		tables.PushString(table_name)
	}

	return tables
}
