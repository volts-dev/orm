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
		leaf         *TDomainNode
		model        *TModel

		models []*TModel
	}

	/*""" Parse a domain expression
	    Use a real polish notation
	    Leafs are still in a ('foo', '=', 'bar') format
	    For more info: http://christophe-simonis-at-tiny.blogspot.com/2008/08/new-new-domain-notation.html
	"""*/
	TExpression struct {
		orm        *TOrm
		root_model *TModel // 本次解析的主要
		Table      string
		Expression *TDomainNode
		stack      []*TExtendedLeaf
		result     []*TExtendedLeaf
		joins      []string //*utils.TStringList
	}
)

func NewExpression(orm *TOrm, model *TModel, domain *TDomainNode, context map[string]interface{}) (*TExpression, error) {
	exp := &TExpression{
		//Table: table,
		orm:        orm,
		root_model: model,
		joins:      make([]string, 0),
	}

	node, err := normalize_domain(domain)
	if err != nil {
		return nil, err
	}

	exp.Expression = distribute_not(node)

	//PrintDomain(exp.Expression) // print domain

	err = exp.parse(context)
	if err != nil {
		return nil, err
	}

	return exp, nil
}

/* Initialize the ExtendedLeaf

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
func NewExtendedLeaf(leaf *TDomainNode, model *TModel, context []TJoinContext, internal bool) *TExtendedLeaf {
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

/*" Leaf validity rules:
    - a valid leaf is an operator or a leaf
    - a valid leaf has a field objects unless
        - it is not a tuple
        - it is an inherited field
        - left is id, operator is 'child_of'
        - left is in MAGIC_COLUMNS
"*/
func (self *TExtendedLeaf) check_leaf(internal bool) {
	if !isOperator(self.leaf) && !isLeaf(self.leaf, internal) {
		//raise ValueError("Invalid leaf %s" % str(self.leaf))
		//提示错误
	}
}

func (self *TExtendedLeaf) generate_alias() string {
	//links = [(context[1]._table, context[4]) for context in self.join_context]
	var links [][]string
	for _, context := range self.join_context {
		links = append(links, []string{context.DestModel.GetName(), context.Link})
	}

	alias, _ /* alias_statement*/ := generate_table_alias(self.models[0].GetName(), links)
	//logger.Dbg("generate_alias", alias)
	return alias
}

func (self *TExtendedLeaf) is_true_leaf() bool {
	if isLeaf(self.leaf) {
		return parseDomain(self.leaf) == TRUE_LEAF
	}

	return false
}

func (self *TExtendedLeaf) is_false_leaf() bool {
	if isLeaf(self.leaf) {
		return parseDomain(self.leaf) == FALSE_LEAF
	}

	return false
}

/*""" Test whether an object is a valid domain operator. """*/
func isOperator(op *TDomainNode) bool {
	if op.IsList() {
		return false
	}

	return utils.InStrings(op.String(), DOMAIN_OPERATORS...) != -1
}

//
func idsToSqlHolder(ids ...interface{}) string {
	return strings.Repeat("?,", len(ids)-1) + "?"
}
func (self *TExtendedLeaf) normalize_leaf() bool {
	self.leaf = normalize_leaf(self.leaf)
	return true
}

// See above comments for more details. A join context is a tuple like:
//        ``(lhs, model, lhs_col, col, link)``
// After adding the join, the model of the current leaf is updated.
func (self *TExtendedLeaf) add_join_context(model *TModel, lhs_col, table_col, link string) {
	self.join_context = append(self.join_context,
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
		links = append(links, []string{context.DestModel.GetName(), context.Link})
		_, alias_statement := generate_table_alias(self.models[0].GetName(), links)
		tables.PushString(alias_statement)
	}

	return tables
}

func (self *TExtendedLeaf) get_join_conditions() (conditions []string) {
	conditions = make([]string, 0) //utils.NewStringList()
	alias := self.models[0].GetName()
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
func normalize_leaf(leaf *TDomainNode) *TDomainNode {
	if !isLeaf(leaf) {
		return leaf
	}

	original := strings.ToLower(leaf.String(1))
	operator := original
	if operator == "<>" {
		operator = "!="
	}
	if utils.IsBoolItf(leaf.Item(2)) && utils.InStrings(operator, "in", "not in") != -1 {
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

	leaf.Item(1).Value = operator
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
func isLeaf(element *utils.TStringList, internal ...bool) bool {
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
	   def isLeaf(element, internal=False):

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
func normalize_domain(domain *TDomainNode) (*TDomainNode, error) {
	if domain == nil {
		logger.Err("Invaild domain")
		return String2Domain(TRUE_DOMAIN)
	}

	// must be including Terms
	if !domain.IsList() {
		return nil, fmt.Errorf("Domains to normalize must have a 'domain' form: a list or tuple of domain components")
	}

	// 将LEAF封装成完整Domain
	if isLeaf(domain) {
		shell := NewDomainNode()
		shell.Push(domain)
		domain = shell
	}

	op_arity := map[string]int{
		NOT_OPERATOR: 1,
		AND_OPERATOR: 2,
		OR_OPERATOR:  2,
	}

	result := NewDomainNode()
	var expected int = 1
	for _, node := range domain.Nodes() {
		if expected == 0 { // more than expected, like in [A, B]
			result.Insert(0, AND_OPERATOR) //put an extra '&' in front
			expected = 1
		}

		result.Push(node) //添加
		if isLeaf(node) { //domain term
			expected -= 1
		} else {
			//logger.Dbg("op_arity", op_arity[item.Text])
			// 如果不是Term而是操作符
			expected += op_arity[node.String()] - 1
		}
	}

	// 错误提示
	if expected != 0 {
		logger.Errf("This domain is syntactically not correct: %s", Domain2String(domain))
	}

	return result, nil
}

// From a leaf, create a new leaf (based on the new_elements tuple
// and new_model), that will have the same join context. Used to
// insert equivalent leafs in the processing stack. """
func create_substitution_leaf(leaf *TExtendedLeaf, new_elements *TDomainNode, new_model *TModel, internal bool) *TExtendedLeaf {
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
func distribute_not(domain *TDomainNode) *TDomainNode {
	if domain == nil {
		return NewDomainNode() //返回空白确保循环不会出现==nil
	}

	stack := NewDomainNode()
	stack.Push("false")
	result := NewDomainNode()

	for _, node := range domain.Nodes() {
		is_negate := false
		negate := stack.Pop()
		if negate != nil {
			is_negate = utils.StrToBool(negate.String())
		}

		//logger.Dbg("opop", isLeaf(node), node.Count(), parseDomain(node.Item(1)), node.String(0))
		// # negate tells whether the subdomain starting with token must be negated
		if isLeaf(node) {
			// (...)
			if is_negate {
				left, operator, right := node.String(0), node.String(1), node.Item(2)
				if _, has := TERM_OPERATORS_NEGATION[operator]; has {
					result.Push(left, TERM_OPERATORS_NEGATION[operator], right)
				} else {
					result.Push(NOT_OPERATOR)
					result.Push(node)
				}
			} else {
				result.Push(node)
			}

		} else if node.Count() > 1 && node.Item(0).IsDomainOperator() {
			// [&,(...),(...)]
			result.Push(node)

		} else if op := node.String(); op != "" {
			if op == NOT_OPERATOR {
				stack.Push(utils.BoolToStr(!is_negate))
			} else if _, has := DOMAIN_OPERATORS_NEGATION[op]; has {
				if is_negate {
					result.Push(DOMAIN_OPERATORS_NEGATION[op])
				} else {
					result.Push(op)
				}

				stack.Push(utils.BoolToStr(is_negate))
				stack.Push(utils.BoolToStr(is_negate))

			} else {
				result.Push(op)
			}
		}
	}

	return result
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

func (self *TExpression) recursive_children(ids []interface{}, model *TModel, parent_field string, context map[string]interface{}) (*TDomainNode, error) {
	result := NewDomainNode()
	if ids == nil {
		return result, nil
	}

	//lRec := model.Search(fmt.Sprintf("[('%s', 'in', '%s')]", parent_field, ids), 0, 0, "", false, context)
	//lRec := model.SearchRead(fmt.Sprintf("[('%s', 'in', '%s')]", parent_field, ids), nil, 0, 0, "", context)
	//lRec, _ := model.Records().Domain(fmt.Sprintf("[('%s', 'in', '%s')]", parent_field, ids.Flatten())).Read()
	rc, err := model.Records().In(parent_field, ids...).Read()
	if err != nil {
		return nil, err
	}

	result.Push(ids...)
	lst, err := self.recursive_children(rc.Keys(), model, parent_field, context)
	if err != nil {
		return nil, err
	}
	for _, node := range lst.Nodes() {
		result.Push(node)

	}

	return result, nil // ids + recursive_children(ids2, model, parent_field)
}

// Return a domain implementing the child_of operator for [(left,child_of,ids)],
// either as a range using the parent_left/right tree lookup fields
// (when available), or as an expanded [(left,in,child_ids)]
func (self *TExpression) child_of_domain(left *TDomainNode, ids *TDomainNode, left_model *TModel, parent string, prefix string, context map[string]interface{}) *TDomainNode {
	result := NewDomainNode()
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
		result.Push("in")
		result.Push(self.recursive_children(ids.Flatten(), left_model, parent, context))
		return result
	}

	return result
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

// TODO 为完成
// Normalize a single id or name, or a list of those, into a list of ids
// :param {int,long,basestring,list,tuple} value:
//  if int, long -> return [value]
// if basestring, convert it into a list of basestrings, then
//  if list of basestring ->
//   perform a name_search on comodel for each name
//       return the list of related ids
// 获得Ids
func (self *TExpression) to_ids(value *TDomainNode, comodel *TModel, context map[string]interface{}, limit int64) *TDomainNode {
	var names []string

	// 分类 id 直接返回 Name 则需要查询获得其Id
	if value != nil {
		// 如果是字符
		if !value.IsList() && value.String() != "" {
			names = append(names, value.String())

		} else if value.IsList() && value.IsStringLeaf() {
			// 如果传入的是字符则可能是名称
			names = append(names, value.Strings()...)

		} else if value.IsIntLeaf() { // 如果是数字
			//# given this nonsensical domain, it is generally cheaper to
			// # interpret False as [], so that "X child_of False" will
			//# match nothing
			//logger.Warmf("Unexpected domain [%s], interpreted as False", leaf)
			return value //strings.Join(value.Strings(), ",")

		}
	} else {
		logger.Warnf("Unexpected domain [%s], interpreted as False", Domain2String(value))

	}

	// 将分类出来名称查询并传回ID
	if names != nil {
		var name_get_list []string // 存放IDs
		//  name_get_list = [name_get[0] for name in names for name_get in comodel.name_search(cr, uid, name, [], 'ilike', context=context, limit=limit)]
		//for _, name := range names.Items() {
		lRecords := comodel.SearchName(strings.Join(value.Strings(), ","), "", "ilike", limit, "", context)
		for _, rec := range lRecords.Data {
			name_get_list = append(name_get_list, rec.FieldByName(comodel.idField).AsString()) //ODO: id 可能是Rec_id
		}
		//}

		result := NewDomainNode()
		for _, name := range name_get_list {
			result.Push(name)

		}
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
func (self *TExpression) parse(context map[string]interface{}) error {
	var (
		ex_leaf               *TExtendedLeaf
		left, operator, right *TDomainNode
		path                  []string
		comodel               IModel
		err                   error
	)

	for _, leaf := range self.Expression.Nodes() {
		self.stack = append(self.stack, NewExtendedLeaf(leaf, self.root_model, nil, false))
	}

	// process from right to left; expression is from left to right
	self.reverse(self.stack)
	for len(self.stack) > 0 {
		ex_leaf = self.pop() // Get the next leaf to process

		// 获取各参数 # Get working variables
		if isOperator(ex_leaf.leaf) {
			left = ex_leaf.leaf
			operator = nil
			right = nil
		} else if ex_leaf.is_true_leaf() || ex_leaf.is_false_leaf() {
			left = ex_leaf.leaf.Item(0)     // 1      TRUE_LEAF  = "(1, '=', 1)"
			operator = ex_leaf.leaf.Item(1) // =
			right = ex_leaf.leaf.Item(2)    // 1
		} else {
			left = ex_leaf.leaf.Item(0)
			operator = ex_leaf.leaf.Item(1)
			right = ex_leaf.leaf.Item(2)
		}

		// :var list path: left operand seen as a sequence of field names
		path = strings.SplitN(left.String(), ".", 2) // "foo.bar" -> ["foo", "bar"]
		model := ex_leaf.model                       // get the model instance
		lFieldName := path[0]                        // get the   first part
		//IsInheritField := model.obj.GetRelatedFieldByName(lFieldName) != nil
		//_, IsInheritField := model._relate_fields[lFieldName] // 是否是继承字段
		//column := model._Columns[path[0]]
		//   comodel = model.pool.get(getattr(field, 'comodel_name', None))

		// get the model
		field := model.GetFieldByName(lFieldName) // get the field instance which has full details
		if field != nil {
			comodel, err = model.Orm().GetModel(field.ModelName()) // get the model of the field owner
			if err != nil {
				return err
			}
		}

		//logger.Dbg("Leaf>", path, model, field, IsInheritField)
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
		if isOperator(ex_leaf.leaf) || ex_leaf.is_true_leaf() || ex_leaf.is_false_leaf() {
			self.push_result(ex_leaf)

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
		} else if field == nil {
			// FIELD NOT FOUND
			return fmt.Errorf("Invalid field %s in leaf %s", left.String(), Domain2String(ex_leaf.leaf))

		} else if field.IsInheritedField() {
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
			//if field != nil && field.IsRelatedField() && IsInheritField {

			//# comments about inherits'd fields
			//#  { 'field_name': ('parent_model', 'm2o_field_to_reach_parent',
			//#                    field_column_obj, origina_parent_model), ... }

			// next_model = model.pool[model._inherit_fields[path[0]][0]]
			//ex_leaf.add_join_context(next_model, model._inherits[next_model._name], 'id', model._inherits[next_model._name])
			logger.Dbg("ttt", model.obj.GetRelatedFieldByName(lFieldName))

			related_field := model.obj.GetRelatedFieldByName(lFieldName)
			next_model, err := model.orm.GetModel(related_field.RelateTableName)
			if err != nil {
				return err
			}
			logger.Dbg("ttt", next_model, related_field)
			//logger.Dbg("IsRelatedField>>", lFieldName, next_model.GetModelName())
			ex_leaf.add_join_context(next_model.GetBase(), model.obj.GetRelationByName(next_model.GetName()), "id", model.obj.GetRelationByName(next_model.GetName()))
			self.push(ex_leaf)

		} else if left.String() == self.root_model.idField && utils.InStrings(operator.String(), "child_of", "parent_of") != -1 {
			// TODO check id 必须改为动态
			ids2 := self.to_ids(right, model, context, 0)
			var dom *TDomainNode

			if operator.String() == "child_of" {
				dom = self.child_of_domain(left, ids2, model, "", "", nil)
			} else if operator.String() == "parent_of" {

			}

			dom = dom.Reversed()
			for _, dom_leaf := range dom.Nodes() {
				new_leaf := create_substitution_leaf(ex_leaf, dom_leaf, model, false)
				self.push(new_leaf)
			}

		} else if field != nil && utils.InStrings(path[0], MAGIC_COLUMNS...) != -1 {
			self.push_result(ex_leaf)

		} else if len(path) > 1 && field.Type() == "many2one" && field.IsAutoJoin() {
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

			//logger.Dbg(`if len(path) > 1 &&field.Type="many2one" && field.IsAutoJoin()`)
			// # res_partner.state_id = res_partner__state_id.id
			ex_leaf.add_join_context(comodel.GetBase(), lFieldName, "id", lFieldName)
			self.push(create_substitution_leaf(ex_leaf, NewDomainNode(path[1], operator.String(), right.String()), comodel.GetBase(), false))

		} else if len(path) > 1 && field.Store() && field.Type() == "one2many" && field.IsAutoJoin() {
			//logger.Dbg(`if len(path) > 1 &&field.Type="many2one" && field.IsAutoJoin()`)
			//  # res_partner.id = res_partner__bank_ids.partner_id
			ex_leaf.add_join_context(comodel.GetBase(), "id", field.FieldsId(), lFieldName)
			domain, err := Query2Domain(field.Domain()) //column._domain(model) if callable(column._domain) else column._domain
			if err != nil {
				logger.Err(err)
			}
			self.push(create_substitution_leaf(ex_leaf, NewDomainNode(path[1], operator.String(), right.String()), comodel.GetBase(), false))
			if domain != nil {
				domain, err = normalize_domain(domain)
				if err != nil {
					logger.Err(err)
				}
				domain = domain.Reversed()
				for _, elem := range domain.Nodes() {
					self.push(create_substitution_leaf(ex_leaf, elem, comodel.GetBase(), false))
				}

				op, err := Query2Domain(AND_OPERATOR)
				if err != nil {
					logger.Err(err)
				}
				self.push(create_substitution_leaf(ex_leaf, op, comodel.GetBase(), false))
			}

		} else if len(path) > 1 && field.Store() && field.IsAutoJoin() {
			return fmt.Errorf("_auto_join attribute not supported on many2many column %s", left.String())

		} else if len(path) > 1 && field.Store() && field.Type() == "many2one" {
			//logger.Dbg(`if len(path) > 1 && field.Type == 'many2one'`)
			domain := fmt.Sprintf(`[('%s', '%s', '%s')]`, path[1], operator.String(), right.String())
			lDs, _ := comodel.Records().Domain(domain).Read() //search(cr, uid, [(path[1], operator, right)], context=dict(context, active_test=False))
			right_ids := lDs.Keys()
			ex_leaf.leaf = NewDomainNode()
			ex_leaf.leaf.Push(path[0], "in")
			ex_leaf.leaf.Push(right_ids...) //    leaf.leaf = (path[0], 'in', right_ids)
			self.push(ex_leaf)

		} else if len(path) > 1 && field.Store() && utils.InStrings(field.Type(), "many2many", "one2many") != -1 {
			// Making search easier when there is a left operand as column.o2m or column.m2m
			domain := fmt.Sprintf(`[('%s', '%s', '%s')]`, path[1], operator.String(), right.String())
			lDs, _ := comodel.Records().Domain(domain).Read()
			right_ids := lDs.Keys()

			domain = fmt.Sprintf(`[('%s', 'in', [%s])]`, path[0], idsToSqlHolder(right_ids))
			lDs, _ = model.Records().Domain(domain, right_ids...).Read()
			table_ids := lDs.Keys()
			ex_leaf.leaf = NewDomainNode()
			ex_leaf.leaf.Push("id", "in")
			ex_leaf.leaf.Push(table_ids...) //    leaf.leaf = (path[0], 'in', right_ids)
			self.push(ex_leaf)

		} else if !field.Store() {
			//# Non-stored field should provide an implementation of search.
			var domain *TDomainNode
			if !field.Search() {
				//# field does not support search!
				logger.Errf("Non-stored field %s cannot be searched.", field.Name)
				// if _logger.isEnabledFor(logging.DEBUG):
				//     _logger.debug(''.join(traceback.format_stack()))
				//# Ignore it: generate a dummy leaf.
				domain = NewDomainNode()
			} else {
				//# Let the field generate a domain.
				if len(path) > 1 {
					operator.Value = "in"
					lDomain := fmt.Sprintf(`[('%s', '%s', '%s')]`, path[1], operator.String(), right.String())
					lDs, _ := comodel.Records().Domain(lDomain).Read()
					right.Clear()
					right.Push(lDs.Keys()...)
				}

				//	TODO 以下代码为翻译
				//recs = model.browse(cr, uid, [], context=context)
				//domain = field.determine_domain(recs, operator, right)
			}

			if domain == nil {
				ex_leaf.leaf, err = Query2Domain(TRUE_LEAF)
				if err != nil {
					logger.Err(err)
				}
				self.push(ex_leaf)
			} else {
				domain = domain.Reversed()
				for _, elem := range domain.Nodes() {
					self.push(create_substitution_leaf(ex_leaf, elem, model, true))
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
				ltemp := NewDomainNode()
				ltemp.Push(left, operator, right)
				self.push(create_substitution_leaf(ex_leaf, ltemp, model, false))

			} else if field.Translatable() && right != nil { //column.translate and not callable(column.translate) and right:

			} else {
				//logger.Dbg(`_push_result`)
				self.push_result(ex_leaf)
			}
		}

	} // end of for
	// ----------------------------------------
	// END OF PARSING FULL DOMAIN
	// -> generate joins
	// ----------------------------------------
	var joins, conditions []string
	joins = make([]string, 0) // NewDomainNode()
	for _, eleaf := range self.result {
		conditions = eleaf.get_join_conditions()
		joins = append(joins, conditions...)
	}
	self.joins = joins

	return nil
}

// 用leaf生成SQL
// 将值用占位符? 区分出来
// res_query：查询语法
// res_params：新占位符？参数值
// res_arg：params 分配给占用符后剩下的值
func (self *TExpression) leaf_to_sql(eleaf *TExtendedLeaf, params []interface{}) (res_query string, res_params []interface{}, res_arg []interface{}) {
	var (
	//first_right_value interface{}   // 提供最终值以供条件判断
	)
	var vals []interface{}              // 该Term 的值 每个Or,And等条件都有自己相当数量的值
	res_params = make([]interface{}, 0) //NewDomainNode()
	res_arg = params                    // 初始化剩余参数

	model := eleaf.model

	leaf := eleaf.leaf
	left := leaf.Item(0)
	operator := leaf.Item(1)
	right := leaf.Item(2)

	field := model.GetFieldByName(left.String())
	is_field := field != nil // 是否是model字段
	//	is_holder := false

	//logger.Dbg("_leaf_to_sql", is_field, left.String(), operator.String(), right.String(), right.IsList())

	// 重新检查合法性 不行final sanity checks - should never fail
	if utils.InStrings(operator.String(), append(TERM_OPERATORS, "inselect", "not inselect")...) == -1 {
		logger.Warnf(`Invalid operator %s in domain term %s`, operator.Strings(), leaf.String())
	}

	if !(left.In(TRUE_LEAF, FALSE_LEAF) || model.GetFieldByName(left.String()) != nil || left.In(MAGIC_COLUMNS)) { //
		logger.Warnf(`Invalid field %s in domain term %s`, left.Strings(), leaf.String())
	}
	//        assert not isinstance(right, BaseModel), \
	//            "Invalid value %r in domain term %r" % (right, leaf)

	table_alias := eleaf.generate_alias()
	holder_count := 0
	if right.IsList() {
		for _, node := range right.children {
			// 识别SQL占位符并切取值
			if utils.InStrings(node.String(), "?", "%s") != -1 {
				vals = append(vals, params[holder_count:1]...)
				holder_count++
				res_arg = params[holder_count:] // 修改params值留到下个Term 返回

			} else {
				vals = append(vals, node.Value)

			}
		}
	} else {
		if utils.InStrings(right.String(), "?", "%s") != -1 {
			vals = append(vals, params[holder_count:1]...)
			holder_count++
			res_arg = params[holder_count:] // 修改params值留到下个Term 返回
		} else {
			vals = append(vals, right.Value)
		}
		//logger.Dbg("holder2", right.String(), right.Value, vals, params, params[holder_count:1], holder_count, res_arg)

	}
	//logger.Dbg("holder", vals, holder_count, res_arg)

	/*	// 检测查询是否占位符?并获取值
		if utils.InStrings(right.String(), "?", "%s") != -1 {
			is_holder = true
			if len(params) > 0 {
				first_right_value = params[0]
				le := utils.MaxInt(1, right.Count()-1)

				logger.Dbg("getttttttt", le, params, params[0:le], params[le:], right.Count(), right.String())
				vals = params[0:le]
				res_arg = params[le:] // 修改params值留到下个Term 返回
			}
			//res_params = append(res_params, lVal)
		} else {
			// 使用Vals作为right传值
			vals = append(vals, right.Flatten()...)
		}
	*/

	if leaf.String() == TRUE_LEAF {
		res_query = "TRUE"
		res_params = nil

	} else if leaf.String() == FALSE_LEAF {
		res_query = "FALSE"
		res_params = nil

	} else if operator.String() == "inselect" { // in(val,val)
		holders := strings.Repeat("?,", len(vals)-1) + "?"
		res_query = fmt.Sprintf(`(%s."%s" in (%s))`, table_alias, left.String(), holders)
		res_params = append(res_params, vals...)

	} else if operator.String() == "not inselect" {
		holders := strings.Repeat("?,", len(vals)-1) + "?"
		res_query = fmt.Sprintf(`%s."%s" not in (%s))`, table_alias, left.String(), holders)
		res_params = append(res_params, vals...)

	} else if operator.In("in", "not in") { //# 数组值
		if right.IsList() {
			res_params = append(res_params, vals...)

			check_nulls := false
			//logger.Dbg("right.IsList ", is_holder, res_params)
			for idx, item := range res_params {
				if utils.IsBoolItf(item) && utils.Itf2Bool(item) == false {
					check_nulls = true
					res_params = utils.SlicRemove(res_params, idx)
				}
			}

			//logger.Dbg("res_params", res_params)
			// In 值操作
			if len(vals) > 0 {
				holders := ""
				if left.String() == self.root_model.idField {
					//instr = strings.Join(utils.Repeat("%s", len(res_params)), ",") // 数字不需要冒号[1,2,3]
					holders = strings.Repeat("?,", len(vals)-1) + "?"
					//logger.Dbg("instr", instr, res_params)
				} else {
					// 获得字段Fortmat格式符 %s,%d 等
					//ss := model.FieldByName(left.String()).SymbolChar()
					//ss := "?"
					// 等同于参数量重复打印格式符
					holders = strings.Repeat("?,", len(vals)-1) + "?" // 字符串需要冒号 ['1','2','3']
					// res_params = map(ss[1], res_params) // map(function, iterable, ...)
					//logger.Dbg("instr else", instr, res_params)
				}
				res_query = fmt.Sprintf(`(%s."%s" %s (%s))`, table_alias, left.String(), operator.String(), holders)
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

		} else if utils.IsBoolItf(vals[0]) { // Must not happen
			r := ""
			logger.Warnf(`The domain term "%s" should use the '=' or '!=' operator.`, leaf.String())
			if operator.String() == "in" {
				if utils.Itf2Bool(vals[0]) {
					r = "NOT NULL"
				} else {
					r = "NULL"
				}
			} else {
				if utils.Itf2Bool(vals[0]) {
					r = "NULL"
				} else {
					r = "NOT NULL"
				}
			}
			res_query = fmt.Sprintf(`(%s."%s" IS %s)`, table_alias, left.String(), r)
			res_params = nil

			//  raise ValueError("Invalid domain term %r" % (leaf,))
		} else {
			// single value use "=" term
			res_query = fmt.Sprintf(`(%s."%s" = ?)`, table_alias, left.String()) //TODO quote
			res_params = append(res_params, vals[0])

		}
	} else if is_field && (field.Type() == FIELD_TYPE_BOOL) &&
		((operator.String() == "=" && utils.Itf2Bool(vals[0]) == false) || (operator.String() == "!=" && utils.Itf2Bool(vals[0]) == true)) {
		// 字段是否Bool类型
		res_query = fmt.Sprintf(`(%s."%s" IS NULL or %s."%s" = false )`, table_alias, left.String(), table_alias, left.String())
		res_params = nil

	} else if (vals == nil || utils.IsBlank(vals[0])) && operator.String() == "=" {
		res_query = fmt.Sprintf(`%s."%s" IS NULL `, table_alias, left.String())
		res_params = nil

	} else if is_field && field.Type() == FIELD_TYPE_BOOL &&
		((operator.String() == "!=" && utils.Itf2Bool(vals[0]) == false) || (operator.String() == "==" && utils.Itf2Bool(vals[0]) == true)) {
		res_query = fmt.Sprintf(`(%s."%s" IS NOT NULL and %s."%s" != false)`, table_alias, left.String(), table_alias, left.String())
		res_params = nil

	} else if (vals == nil || utils.IsBlank(vals[0])) && (operator.String() == "!=") {
		res_query = fmt.Sprintf(`%s."%s" IS NOT NULL`, table_alias, left.String())
		res_params = nil

	} else if operator.String() == "=?" { //TODO  未完成 # Boolen 判断
		if vals == nil || utils.IsBlank(vals[0]) {
			// '=?' is a short-circuit that makes the term TRUE if right is None or False
			res_query = "TRUE"
			res_params = nil
		} else {
			// '=?' behaves like '=' in other cases
			lDomain, err := Query2Domain(fmt.Sprintf(`[('%s','=','%s')]`, left.String(), right.String()))
			if err != nil {
				logger.Err(err)
			}
			res_query, res_params, res_arg = self.leaf_to_sql(create_substitution_leaf(eleaf, lDomain, model, false), nil)
		}

	} else if left.String() == self.root_model.idField {
		res_query = fmt.Sprintf("%s.%s %s ?", table_alias, self.root_model.idField, operator.String())
		res_params = append(res_params, vals...)

	} else {
		// TODO 字段值格式化
		// 是否需要添加“%%”
		need_wildcard := operator.In("like", "ilike", "not like", "not ilike")
		add_null := right.String() == ""

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
		if is_field {
			//format = need_wildcard and '%s' or model._columns[left]._symbol_set[0]
			format := ""
			// 范查询
			if need_wildcard {
				//format = "'%s'" // %XX%号在参数里添加
				format = "?" // fmt.Sprintf(field._symbol_c, "?") //field.SymbolFunc("?") //
			} else {
				//format = field.SymbolChar()
				format = "?" // fmt.Sprintf(field._symbol_c, "?") //field.SymbolFunc("?") //
			}

			//unaccent = self._unaccent if sql_operator.endswith('like') else lambda x: x
			column := fmt.Sprintf("%s.%s", table_alias, self.orm.dialect.Quote(left.String()))
			res_query = fmt.Sprintf("(%s %s %s)", column+cast, sql_operator, format)

		} else if left.In(MAGIC_COLUMNS) {
			res_query = fmt.Sprintf("(%s.\"%s\"%s %s ?)", table_alias, left.String(), cast, sql_operator)

		} else {
			//# Must not happen
			logger.Errf(`Invalid field %s in domain term %s`, left.String(), leaf.String())
		}

		if add_null {
			res_query = fmt.Sprintf(`(%s OR %s."%s" IS NULL)`, res_query, table_alias, left.String())
		}

		res_params = append(res_params, vals...)
	}
	//logger.Dbg("toDQL:", res_query, res_params)
	return res_query, res_params, res_arg
}

// to generate the SQL expression and params
func (self *TExpression) ToSql(params ...interface{}) ([]string, []interface{}) {
	return self.to_sql(params...)
}

// params sql value
func (self *TExpression) to_sql(params ...interface{}) ([]string, []interface{}) {
	var (
		stack  = NewDomainNode()
		ops    map[string]string
		q1, q2 *TDomainNode
		query  string
	)
	res_params := make([]interface{}, 0)

	// 翻转顺序以便递归生成
	// Process the domain from right to left, using a stack, to generate a SQL expression.
	self.reverse(self.result)
	params = utils.ReverseItfs(params...)
	// 遍历并生成
	for _, eleaf := range self.result {
		if isLeaf(eleaf.leaf, true) {
			query, query_params, other_params := self.leaf_to_sql(eleaf, params) //internal: allow or not the 'inselect' internal operator in the term. This should be always left to False.
			params = other_params                                                // 剩余的params参数
			res_params = utils.SlicInsert(res_params, 0, query_params...)
			stack.Push(query)

		} else if eleaf.leaf.String() == NOT_OPERATOR {
			stack.Push("(NOT (%s))", stack.Pop().String())

		} else {
			// domain 操作符
			ops = map[string]string{AND_OPERATOR: " AND ", OR_OPERATOR: " OR "} //TODO 优化
			q1 = stack.Pop()
			q2 = stack.Pop()
			if q1 != nil && q2 != nil {
				lStr := fmt.Sprintf("(%s %s %s)", q1.String(), ops[eleaf.leaf.String()], q2.String())
				stack.Push(lStr)
			}
		}
	}

	// TODO 移除
	//if len(params) > 0 {
	//	logger.Dbg("ttt2", params)
	//	res_params = append(res_params, params...)
	//}

	// #上面Pop取出合并后应该为1
	if stack.Count() != 1 {
		res_params = nil
		logger.Errf("domain to sql error: stack.Len() %d %v", stack.Count(), self.result)
	}

	query = stack.String(0)
	joins := strings.Join(self.joins, " AND ")
	if joins != "" {
		query = fmt.Sprintf("(%s) AND %s", joins, query)
	}

	return []string{query}, res_params //lParams.Flatten()
}

//""" Returns the list of tables for SQL queries, like select from ... """
func (self *TExpression) get_tables() *utils.TStringList {
	tables := utils.NewStringList()
	for _, leaf := range self.result {
		for _, table := range leaf.get_tables().Items() {
			if !tables.Has(table.String()) {
				tables.PushString(table.String())
			}
		}
	}

	table_name := _quote(self.root_model.GetName())
	if !tables.Has(table_name) {
		tables.PushString(table_name)
	}

	return tables
}
