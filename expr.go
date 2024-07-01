package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

type (
	/*""" Class wrapping a domain leaf, and giving some services and management
	    features on it. In particular it managed join contexts to be able to
	    construct queries through multiple models.
	"""*/
	TJoinContext struct {
		SourceModel *TModel // source (left hand) model
		DestModel   *TModel // destination (right hand) model
		SourceFiled string  // source model column for join condition
		DestFiled   string  // destination model column for join condition
		Link        string
	}

	/*""" Parse a domain expression
	    Use a real polish notation
	    Leafs are still in a ('foo', '=', 'bar') format
	    For more info: http://christophe-simonis-at-tiny.blogspot.com/2008/08/new-new-domain-notation.html
	"""*/
	TExpression struct {
		Table      string
		orm        *TOrm
		root_model *TModel // 本次解析的主要
		Expression *domain.TDomainNode
		stack      []*TExtendedLeaf
		result     []*TExtendedLeaf
		joins      []string //*utils.TStringList
	}
)

var (
	op_arity = map[string]int{
		domain.NOT_OPERATOR: 1,
		domain.AND_OPERATOR: 2,
		domain.OR_OPERATOR:  2,
	}

	HIERARCHY_FUNCS = map[string]func(*domain.TDomainNode, *domain.TDomainNode, *TModel, string, string, map[string]interface{}) *domain.TDomainNode{
		"child_of":  child_of_domain,
		"parent_of": parent_of_domain}
)

func NewExpression(orm *TOrm, model *TModel, dom *domain.TDomainNode, context map[string]interface{}) (*TExpression, error) {
	exp := &TExpression{
		orm:        orm,
		root_model: model,
		joins:      make([]string, 0),
	}

	node, err := normalize_domain(dom)
	if err != nil {
		return nil, err
	}
	//domain.PrintDomain(dom) // print domain

	exp.Expression = distribute_not(node)

	//domain.PrintDomain(exp.Expression) // print domain

	err = exp.parse(context)
	if err != nil {
		return nil, err
	}

	return exp, nil
}

/*
# --------------------------------------------------
# Generic leaf manipulation
# --------------------------------------------------
*/
func quoteStr(str string) string {
	if !strings.HasPrefix(str, `"`) {
		return `"` + str + `"`
	}
	return str
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

/*
Returns a new domain expression where all domain components from “domains“

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

/*
# --------------------------------------------------
# Generic domain manipulation
# --------------------------------------------------

	"""Returns a normalized version of ``domain_expr``, where all implicit '&' operators
	   have been made explicit. One property of normalized domain expressions is that they
	   can be easily combined together as if they were single domain components.
	"""
*/
func normalize_domain(node *domain.TDomainNode) (*domain.TDomainNode, error) {
	if node == nil {
		log.Warnf("The domain is Invaild!")
		return domain.String2Domain(domain.TRUE_DOMAIN, nil)
	}

	// must be including Terms
	if node.IsValueNode() {
		return nil, fmt.Errorf("Domains to normalize must have a 'domain' form: a list or tuple of domain components")
	}

	// 将LEAF封装成完整Domain
	if node.IsLeafNode() {
		shell := domain.NewDomainNode()
		shell.Push(node)
		node = shell
	}

	result := domain.NewDomainNode()
	var expected int = 1
	for _, n := range node.Nodes() {
		if expected == 0 { // more than expected, like in [A, B]
			result.Insert(0, domain.AND_OPERATOR) //put an extra '&' in front
			expected = 1
		}

		result.Push(n) //添加

		if !n.IsValueNode() { // domain term
			expected -= 1
		} else {
			// 如果不是Term而是操作符
			expected += op_arity[n.String()] - 1
		}
	}

	if expected != 0 {
		log.Errf("This domain is syntactically not correct: %s", domain.Domain2String(node))
	}

	return result, nil
}

// From a leaf, create a new leaf (based on the new_elements tuple
// and new_model), that will have the same join context. Used to
// insert equivalent leafs in the processing stack. """
func create_substitution_leaf(leaf *TExtendedLeaf, new_elements *domain.TDomainNode, new_model *TModel, internal bool) *TExtendedLeaf {
	if new_model == nil {
		new_model = leaf.model
	}
	var new_join_context []TJoinContext
	new_join_context = leaf.join_context //复制
	return NewExtendedLeaf(new_elements, new_model, new_join_context, internal)
}

func child_of_domain(left *domain.TDomainNode, ids *domain.TDomainNode, left_model *TModel, parent string, prefix string, context map[string]interface{}) *domain.TDomainNode {
	//""" Return a domain implementing the child_of operator for [(left,child_of,ids)],
	//    either as a range using the parent_path tree lookup field
	//    (when available), or as an expanded [(left,in,child_ids)] """

	/* if not ids:
	       return [FALSE_LEAF]
	   if left_model._parent_store:
	       doms = OR([
	           [('parent_path', '=like', rec.parent_path + '%')]
	           for rec in left_model.browse(ids)
	       ])
	       if prefix:
	           return [(left, 'in', left_model.search(doms).ids)]
	       return doms
	   else:
	       parent_name = parent or left_model._parent_name
	       child_ids = set(ids)
	       while ids:
	           ids = left_model.search([(parent_name, 'in', ids)]).ids
	           child_ids.update(ids)
	       return [(left, 'in', list(child_ids))]

	*/
	return nil
}

func parent_of_domain(left *domain.TDomainNode, ids *domain.TDomainNode, left_model *TModel, parent string, prefix string, context map[string]interface{}) *domain.TDomainNode {
	/*
	   // Return a domain implementing the parent_of operator for [(left,parent_of,ids)],
	   // either as a range using the parent_path tree lookup field
	   // (when available), or as an expanded [(left,in,parent_ids)]

	              if left_model._parent_store:
	                  parent_ids = [
	                      int(label)
	                      for rec in left_model.browse(ids)
	                      for label in rec.parent_path.split('/')[:-1]
	                  ]
	                  if prefix:
	                      return [(left, 'in', parent_ids)]
	                  return [('id', 'in', parent_ids)]
	              else:
	                  parent_name = parent or left_model._parent_name
	                  parent_ids = set()
	                  for record in left_model.browse(ids):
	                      while record:
	                          parent_ids.add(record.id)
	                          record = record[parent_name]
	                  return [(left, 'in', list(parent_ids))]
	*/
	return nil

}

/*
" Distribute any '!' domain operators found inside a normalized domain.

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

	"
*/
func distribute_not(node *domain.TDomainNode) *domain.TDomainNode {
	if node == nil {
		return domain.NewDomainNode() //返回空白确保循环不会出现==nil
	}

	stack := domain.NewDomainNode()
	stack.Push("false")
	result := domain.NewDomainNode()

	for _, n := range node.Nodes() {
		is_negate := false
		negate := stack.Pop()
		if negate != nil {
			is_negate = utils.StrToBool(negate.String())
		}

		if n.IsValueNode() {
			if op := n.String(); op != "" {
				if op == domain.NOT_OPERATOR {
					stack.Push(utils.ToString(!is_negate))
				} else if _, has := domain.DOMAIN_OPERATORS_NEGATION[op]; has {
					if is_negate {
						result.Push(domain.DOMAIN_OPERATORS_NEGATION[op])
					} else {
						result.Push(op)
					}

					stack.Push(utils.ToString(is_negate))
					stack.Push(utils.ToString(is_negate))

				} else {
					result.Push(op)
				}
			}
		} else {

			// (...)
			// # negate tells whether the subdomain starting with token must be negated

			if n.IsLeafNode() && is_negate {
				left, operator, right := n.String(0), n.String(1), n.Item(2)
				if _, has := domain.TERM_OPERATORS_NEGATION[operator]; has {
					result.Push(left, domain.TERM_OPERATORS_NEGATION[operator], right)
				} else {
					result.Push(domain.NOT_OPERATOR)
					result.Push(n)
				}
			} else {
				// [&,(...),(...)]
				result.Push(n)
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
	srcTableName := src_table_alias
	if joined_tables == nil {
		return srcTableName, quoteStr(srcTableName)
	}

	for _, link := range joined_tables {
		srcTableName = srcTableName + "__" + link[1]
	}

	if len(srcTableName) > 64 {
		log.Errf("Table alias name %s is longer than the 64 characters size accepted by default in postgresql.", srcTableName)
	}

	return srcTableName, fmt.Sprintf("%s as %s", quoteStr(joined_tables[0][0]), quoteStr(srcTableName))
}

func idsToSqlHolder(ids ...interface{}) string {
	return strings.Repeat("?,", len(ids)-1) + "?"
}

// :param string from_query: is something like :
//   - '"res_partner"' OR
//   - '"res_partner" as "res_users__partner_id"”
//
// from_query: 表名有关的字符串
func get_alias_from_query(from_query string) (string, string) {
	from_splitted := strings.Split(from_query, " as ")
	if len(from_splitted) > 1 {
		return strings.Replace(from_splitted[0], `"`, "", -1), strings.Replace(from_splitted[1], `"`, "", -1)
	} else {
		return strings.Replace(from_splitted[0], `"`, "", -1), strings.Replace(from_splitted[0], `"`, "", -1)
	}
}

func (self *TExpression) recursive_children(ids []interface{}, model *TModel, parent_field string, context map[string]interface{}) (*domain.TDomainNode, error) {
	result := domain.NewDomainNode()
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

// Push a leaf to the results. This leaf has been fully processed and validated.
func (self *TExpression) push_result(leaf *TExtendedLeaf) {
	self.result = append(self.result, leaf)
}

// 反转
func (self *TExpression) reverse(lst []*TExtendedLeaf) {
	var tmp []*TExtendedLeaf
	lCnt := len(lst)
	for i := lCnt - 1; i >= 0; i-- {
		tmp = append(tmp, lst[i])
	}
	copy(lst, tmp)
}

// TODO 为完成
// Normalize a single id or name, or a list of those, into a list of ids
// :param {int,long,basestring,list,tuple} value:
//
//	if int, long -> return [value]
//
// if basestring, convert it into a list of basestrings, then
//
//	if list of basestring ->
//	 perform a name_search on comodel for each name
//	     return the list of related ids
//
// 获得Ids
func (self *TExpression) to_ids(value *domain.TDomainNode, comodel *TModel, context map[string]interface{}, limit int64) *domain.TDomainNode {
	var names []string

	/* 分类 id 直接返回 Name 则需要查询获得其Id */
	if value != nil {
		// 如果是字符
		if !value.IsListNode() && value.String() != "" {
			names = append(names, value.String())

		} else if value.IsListNode() && value.IsStringList() {
			// 如果传入的是字符则可能是名称
			names = append(names, value.Strings()...)

		} else if value.IsIntLeaf() { // 如果是数字
			//# given this nonsensical domain, it is generally cheaper to
			// # interpret False as [], so that "X child_of False" will
			//# match nothing
			//log.Warmf("Unexpected domain [%s], interpreted as False", leaf)
			return value //strings.Join(value.Strings(), ",")

		}
	} else {
		log.Errf("Unexpected domain [%s], interpreted as False", domain.Domain2String(value))

	}

	/* 将分类出来名称查询并传回ID */
	if names != nil {
		var name_get_list []string // 存放IDs
		//  name_get_list = [name_get[0] for name in names for name_get in comodel.name_search(cr, uid, name, [], 'ilike', context=context, limit=limit)]
		//for _, name := range names.Items() {
		// 这里使用精准名称“in”查询
		_domain := domain.New(comodel.nameField, "in", value.Flatten()...)
		lRecords, _ := comodel.NameSearch("", _domain, "ilike", limit, "", context)
		for _, rec := range lRecords.Data {
			name_get_list = append(name_get_list, rec.FieldByName(comodel.idField).AsString()) //ODO: id 可能是Rec_id
		}
		//}

		result := domain.NewDomainNode()
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
		left, operator, right *domain.TDomainNode
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
		if ex_leaf.leaf.IsDomainOperator() {
			left = ex_leaf.leaf
			operator = nil
			right = nil
			/*	}else if ex_leaf.leaf.Item(0).IsDomainOperator() {
				left = ex_leaf.leaf.Item(0)
				operator = nil
				right = nil
				ex_leaf = NewExtendedLeaf(ex_leaf.leaf, self.root_model, nil, false)
			*/
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
		fieldName := path[0]                         // get the   first part
		//IsInheritField := model.obj.GetRelatedFieldByName(fieldName) != nil
		//_, IsInheritField := model._relate_fields[fieldName] // 是否是继承字段
		//column := model._Columns[path[0]]
		//   comodel = model.pool.get(getattr(field, 'comodel_name', None))

		// get the model
		field := model.GetFieldByName(fieldName) // get the field instance which has full details
		if field != nil {
			comodel, err = model.Orm().GetModel(field.ModelName()) // get the model of the field owner
			if err != nil {
				return err
			}
		}

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
		if ex_leaf.leaf.IsDomainOperator() || ex_leaf.is_true_leaf() || ex_leaf.is_false_leaf() {
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
			return log.Errf("Invalid field <%s>@<%s> in leaf <%s>", left.String(), model.String(), domain.Domain2String(ex_leaf.leaf))

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

			related_field := model.obj.GetRelatedFieldByName(fieldName)
			next_model, err := model.orm.GetModel(related_field.RelateTableName)
			if err != nil {
				return err
			}
			ex_leaf.add_join_context(next_model.GetBase(), model.obj.GetRelationByName(next_model.String()), "id", model.obj.GetRelationByName(next_model.String()))
			self.push(ex_leaf)

		} else if fn, has := HIERARCHY_FUNCS[operator.String()]; has && left.String() == self.root_model.idField {
			// 父子关系
			// TODO check id 必须改为动态
			ids2 := self.to_ids(right, model, context, 0)
			dom := fn(left, ids2, model, "", "", nil)
			dom = dom.Reversed()
			for _, dom_leaf := range dom.Nodes() {
				new_leaf := create_substitution_leaf(ex_leaf, dom_leaf, model, false)
				self.push(new_leaf)
			}

		} else if utils.InStrings(path[0], MAGIC_COLUMNS...) != -1 {
			self.push_result(ex_leaf)

		} else if len(path) > 1 && field.Type() == TYPE_M2O && field.IsAutoJoin() {
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

			// # res_partner.state_id = res_partner__state_id.id
			ex_leaf.add_join_context(comodel.GetBase(), fieldName, "id", fieldName)
			self.push(create_substitution_leaf(ex_leaf, domain.NewDomainNode(path[1], operator.String(), right.String()), comodel.GetBase(), false))

		} else if len(path) > 1 && field.Store() && field.Type() == TYPE_O2M && field.IsAutoJoin() {
			//  # res_partner.id = res_partner__bank_ids.partner_id
			ex_leaf.add_join_context(comodel.GetBase(), "id", field.FieldsId(), fieldName)
			node, err := domain.String2Domain(field.Domain(), nil) //column._domain(model) if callable(column._domain) else column._domain
			if err != nil {
				log.Err(err)
			}
			self.push(create_substitution_leaf(ex_leaf, domain.NewDomainNode(path[1], operator.String(), right.String()), comodel.GetBase(), false))
			if node != nil {
				node, err = normalize_domain(node)
				if err != nil {
					log.Err(err)
				}
				node = node.Reversed()
				for _, elem := range node.Nodes() {
					self.push(create_substitution_leaf(ex_leaf, elem, comodel.GetBase(), false))
				}

				op, err := domain.String2Domain(domain.AND_OPERATOR, nil)
				if err != nil {
					log.Err(err)
				}
				self.push(create_substitution_leaf(ex_leaf, op, comodel.GetBase(), false))
			}

		} else if len(path) > 1 && field.Store() && field.IsAutoJoin() {
			return fmt.Errorf("_auto_join attribute not supported on many2many column %s", left.String())

		} else if len(path) > 1 && field.Store() && field.Type() == TYPE_M2O {
			domain_str := fmt.Sprintf(`[('%s', '%s', '%s')]`, path[1], operator.String(), right.String())
			lDs, _ := comodel.Records().Domain(domain_str).Read() //search(cr, uid, [(path[1], operator, right)], context=dict(context, active_test=False))
			right_ids := lDs.Keys()
			ex_leaf.leaf = domain.NewDomainNode()
			ex_leaf.leaf.Push(path[0], "in")
			ex_leaf.leaf.Push(right_ids...) //    leaf.leaf = (path[0], 'in', right_ids)
			self.push(ex_leaf)

		} else if len(path) > 1 && field.Store() && utils.InStrings(field.Type(), TYPE_M2M, TYPE_O2M) != -1 {
			// Making search easier when there is a left operand as column.o2m or column.m2m
			domain_str := fmt.Sprintf(`[('%s', '%s', '%s')]`, path[1], operator.String(), right.String())
			lDs, _ := comodel.Records().Domain(domain_str).Read()
			right_ids := lDs.Keys()

			domain_str = fmt.Sprintf(`[('%s', 'in', [%s])]`, path[0], idsToSqlHolder(right_ids))
			lDs, _ = model.Records().Domain(domain_str, right_ids...).Read()
			table_ids := lDs.Keys()
			ex_leaf.leaf = domain.NewDomainNode()
			ex_leaf.leaf.Push("id", "in")
			ex_leaf.leaf.Push(table_ids...) //    leaf.leaf = (path[0], 'in', right_ids)
			self.push(ex_leaf)

		} else if !field.Store() {
			//# Non-stored field should provide an implementation of search.
			var node *domain.TDomainNode
			if !field.Search() {
				//# field does not support search!
				log.Errf("Non-stored field %s cannot be searched.", field.Name)
				// if _log.isEnabledFor(logging.DEBUG):
				//     _log.debug(''.join(traceback.format_stack()))
				//# Ignore it: generate a dummy leaf.
				node = domain.NewDomainNode()
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

			if node == nil {
				ex_leaf.leaf, err = domain.String2Domain(domain.TRUE_LEAF, nil)
				if err != nil {
					log.Err(err)
				}
				self.push(ex_leaf)
			} else {
				node = node.Reversed()
				for _, elem := range node.Nodes() {
					self.push(create_substitution_leaf(ex_leaf, elem, model, true))
				}
			}
			//} else if field.IsFuncField() && !field.Store { // isinstance(column, fields.function) and not column.store

		} else if field.Type() == TYPE_O2M && (operator.String() == "child_of" || operator.String() == "parent_of") {
			// -------------------------------------------------
			// RELATIONAL FIELDS
			// -------------------------------------------------

		} else if field.Type() == TYPE_O2M {
			// TODO one2many
			log.Errf("the one2many %s@%s is no implemented!", field.Name(), field.ModelName())
		} else if field.Type() == TYPE_M2M {
			// TODO many2many
			log.Errf("the many2many %s@%s is no implemented!", field.Name(), field.ModelName())
		} else if field.Type() == TYPE_M2O {
			if _, has := HIERARCHY_FUNCS[operator.String()]; has {
				/*
				   ids2 = to_ids(right, comodel, leaf)
				                       if field.comodel_name != model._name:
				                           dom = HIERARCHY_FUNCS[operator](left, ids2, comodel, prefix=field.comodel_name)
				                       else:
				                           dom = HIERARCHY_FUNCS[operator]('id', ids2, model, parent=left)
				                       for dom_leaf in dom:
				                           push(dom_leaf, model, alias)
				*/
			} else {
				// 对多值修改为In操作
				if _, ok := right.Value.([]any); ok {
					ex_leaf.leaf.Item(1).Value = "in"
					self.push_result(ex_leaf)

				} else {
					self.push_result(ex_leaf)

				}
				//expr, params = self.leaf_to_sql(ex_leaf, model, alias)
				/*
				   def _get_expression(comodel, left, right, operator):
				                          #Special treatment to ill-formed domains
				                          operator = (operator in ['<', '>', '<=', '>=']) and 'in' or operator

				                          dict_op = {'not in': '!=', 'in': '=', '=': 'in', '!=': 'not in'}
				                          if isinstance(right, tuple):
				                              right = list(right)
				                          if (not isinstance(right, list)) and operator in ['not in', 'in']:
				                              operator = dict_op[operator]
				                          elif isinstance(right, list) and operator in ['!=', '=']:  # for domain (FIELD,'=',['value1','value2'])
				                              operator = dict_op[operator]
				                          res_ids = comodel.with_context(active_test=False)._name_search(right, [], operator, limit=None)
				                          if operator in NEGATIVE_TERM_OPERATORS:
				                              res_ids = list(res_ids) + [False]  # TODO this should not be appended if False was in 'right'
				                          return left, 'in', res_ids
				                      # resolve string-based m2o criterion into IDs
				                      if isinstance(right, str) or \
				                              isinstance(right, (tuple, list)) and right and all(isinstance(item, str) for item in right):
				                          push(_get_expression(comodel, left, right, operator), model, alias)
				                      else:
				                          # right == [] or right == False and all other cases are handled by __leaf_to_sql()
				                          expr, params = self.__leaf_to_sql(leaf, model, alias)
				                          push_result(expr, params)
				*/
			}

		} else if field.Type() == "binary" && field.(*TBinField).attachment {

		} else {
			// -------------------------------------------------
			// OTHER FIELDS
			// -> datetime fields: manage time part of the datetime
			//    column when it is not there
			// -> manage translatable fields
			// -------------------------------------------------

			if field.Type() == "datetime" && right != nil && right.Count() == 10 {
				if operator.ValueIn(">", "<=") {
					//  right += ' 23:59:59'
				} else {
					//  right += ' 00:00:00'
				}
				ltemp := domain.NewDomainNode()
				ltemp.Push(left, operator, right)
				self.push(create_substitution_leaf(ex_leaf, ltemp, model, false))

			} else if field.Translatable() && right != nil && !utils.IsBlank(right.Value) { //column.translate and not callable(column.translate) and right:
				// 翻译
				need_wildcard := operator.ValueIn("like", "ilike", "not like", "not ilike")
				if need_wildcard {
					right.Value = "%" + utils.ToString(right.Value) + "%"
				}
				self.push_result(ex_leaf)
			} else {
				self.push_result(ex_leaf)
			}
		}

	} // end of for

	// ----------------------------------------
	// END OF PARSING FULL DOMAIN
	// -> generate joins
	// ----------------------------------------
	var joins, conditions []string
	joins = make([]string, 0) // domain.NewDomainNode()
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
	quoter := self.orm.dialect.Quoter()
	var vals []interface{}              // 该Term 的值 每个Or,And等条件都有自己相当数量的值
	res_params = make([]interface{}, 0) //domain.NewDomainNode()
	res_arg = params                    // 初始化剩余参数

	model := eleaf.model

	leaf := eleaf.leaf
	left := leaf.Item(0)
	operator := leaf.Item(1)
	right := leaf.Item(2)

	field := model.GetFieldByName(left.String())
	is_field := field != nil // 是否是model字段
	//	is_holder := false

	// 重新检查合法性 不行final sanity checks - should never fail
	if utils.InStrings(operator.String(), append(domain.TERM_OPERATORS, "inselect", "not inselect")...) == -1 {
		log.Errf(`Invalid operator %s in domain term %s`, operator.Strings(), leaf.String())
	}

	if !(left.ValueIn(domain.TRUE_LEAF, domain.FALSE_LEAF) || model.GetFieldByName(left.String()) != nil || left.ValueIn(MAGIC_COLUMNS)) { //
		log.Errf(`Invalid field %s in domain term %s`, left.Strings(), leaf.String())
	}
	//        assert not isinstance(right, BaseModel), \
	//            "Invalid value %r in domain term %r" % (right, leaf)

	_, table_alias := eleaf.generate_alias()
	holder_count := 0
	if right.IsListNode() {
		for _, node := range right.Nodes() {
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
	}

	/*	// 检测查询是否占位符?并获取值
				if utils.InStrings(right.String(), "?", "%s") != -1 {
					is_holder = true
					if len(params) > 0 {
						first_right_value = params[0]
						le := utils.MaxInt(1, right.Count()-1)

		 				vals = params[0:le]
						res_arg = params[le:] // 修改params值留到下个Term 返回
					}
					//res_params = append(res_params, lVal)
				} else {
					// 使用Vals作为right传值
					vals = append(vals, right.Flatten()...)
				}
	*/

	if leaf.String() == domain.TRUE_LEAF {
		res_query = "TRUE"
		res_params = nil

	} else if leaf.String() == domain.FALSE_LEAF {
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

	} else if operator.ValueIn("in", "not in") { //# 数组值
		if right.IsListNode() {
			res_params = append(res_params, vals...)

			check_nulls := false
			for idx, item := range res_params {
				if utils.IsBoolItf(item) && utils.Itf2Bool(item) == false {
					check_nulls = true
					res_params = utils.SlicRemove(res_params, idx)
				}
			}

			// In 值操作
			if len(vals) > 0 {
				holders := ""
				if left.String() == self.root_model.idField {
					//instr = strings.Join(utils.Repeat("%s", len(res_params)), ",") // 数字不需要冒号[1,2,3]
					holders = strings.Repeat("?,", len(vals)-1) + "?"
				} else {
					// 获得字段Fortmat格式符 %s,%d 等
					//ss := model.FieldByName(left.String()).SymbolChar()
					//ss := "?"
					// 等同于参数量重复打印格式符
					holders = strings.Repeat("?,", len(vals)-1) + "?" // 字符串需要冒号 ['1','2','3']
					// res_params = map(ss[1], res_params) // map(function, iterable, ...)
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
			log.Errf(`The domain term "%s" should use the '=' or '!=' operator.`, leaf.String())
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
	} else if is_field && (field.Type() == Bool) &&
		((operator.String() == "=" && utils.Itf2Bool(vals[0]) == false) || (operator.String() == "!=" && utils.Itf2Bool(vals[0]) == true)) {
		// 字段是否Bool类型
		res_query = fmt.Sprintf(`(%s."%s" IS NULL or %s."%s" = false )`, table_alias, left.String(), table_alias, left.String())
		res_params = nil

	} else if (vals == nil || utils.Itf2Str(vals[0]) == "NULL" /*utils.IsBlank(vals[0])*/) && operator.String() == "=" {
		res_query = fmt.Sprintf(`%s."%s" IS NULL `, table_alias, left.String())
		res_params = nil

	} else if is_field && field.Type() == Bool &&
		((operator.String() == "!=" && utils.Itf2Bool(vals[0]) == false) || (operator.String() == "==" && utils.Itf2Bool(vals[0]) == true)) {
		res_query = fmt.Sprintf(`(%s."%s" IS NOT NULL and %s."%s" != false)`, table_alias, left.String(), table_alias, left.String())
		res_params = nil

	} else if (vals == nil || utils.Itf2Str(vals[0]) == "NULL" /*utils.IsBlank(vals[0])*/) && (operator.String() == "!=") {
		res_query = fmt.Sprintf(`%s."%s" IS NOT NULL`, table_alias, left.String())
		res_params = nil

	} else if operator.String() == "=?" { //TODO  未完成 # Boolen 判断
		if vals == nil || utils.IsBlank(vals[0]) {
			// '=?' is a short-circuit that makes the term TRUE if right is None or False
			res_query = "TRUE"
			res_params = nil
		} else {
			// '=?' behaves like '=' in other cases
			lDomain, err := domain.String2Domain(fmt.Sprintf(`[('%s','=','%s')]`, left.String(), right.String()), nil)
			if err != nil {
				log.Err(err)
			}
			res_query, res_params, res_arg = self.leaf_to_sql(create_substitution_leaf(eleaf, lDomain, model, false), nil)
		}

	} else if left.String() == self.root_model.idField {
		res_query = fmt.Sprintf("%s.%s %s ?", table_alias, self.root_model.idField, operator.String())
		res_params = append(res_params, vals...)

	} else {
		// TODO 字段值格式化
		// 是否需要添加“%%”
		need_wildcard := operator.ValueIn("like", "ilike", "not like", "not ilike")
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
			column := fmt.Sprintf("%s.%s", table_alias, quoter.Quote(left.String()))
			res_query = fmt.Sprintf("(%s %s %s)", column+cast, sql_operator, format)

		} else if left.ValueIn(MAGIC_COLUMNS) {
			res_query = fmt.Sprintf("(%s.\"%s\"%s %s ?)", table_alias, left.String(), cast, sql_operator)

		} else {
			//# Must not happen
			log.Errf(`Invalid field %s in domain term %s`, left.String(), leaf.String())
		}

		if add_null {
			res_query = fmt.Sprintf(`(%s OR %s."%s" IS NULL)`, res_query, table_alias, left.String())
		}

		res_params = append(res_params, vals...)
	}
	return res_query, res_params, res_arg
}

// to generate the SQL expression and params
func (self *TExpression) ToSql(params ...interface{}) ([]string, []interface{}) {
	return self.toSql(params...)
}

// 传递domain值并重新生成
// params sql value
func (self *TExpression) toSql(params ...interface{}) ([]string, []interface{}) {
	var (
		stack  = domain.NewDomainNode()
		q1, q2 *domain.TDomainNode
		query  string
	)

	// 翻转顺序以便递归生成
	// Process the domain from right to left, using a stack, to generate a SQL expression.
	self.reverse(self.result)
	params = utils.Reversed(params...)

	// 遍历并生成
	res_params := make([]interface{}, 0)
	for _, eleaf := range self.result {
		if eleaf.leaf.IsLeafNode() {
			query, query_params, other_params := self.leaf_to_sql(eleaf, params) //internal: allow or not the 'inselect' internal operator in the term. This should be always left to False.
			params = other_params                                                // 剩余的params参数
			res_params = utils.SlicInsert(res_params, 0, query_params...)
			stack.Push(query)

		} else if eleaf.leaf.String() == domain.NOT_OPERATOR {
			stack.Push("(NOT (%s))", stack.Pop().String())

		} else {
			// domain 操作符
			q1 = stack.Pop()
			q2 = stack.Pop()
			if q1 != nil && q2 != nil {
				lStr := fmt.Sprintf("(%s %s %s)", q1.String(), domain.DOMAIN_OPERATORS_KEYWORDS[eleaf.leaf.String()], q2.String())
				stack.Push(lStr)
			}
		}
	}

	// #上面Pop取出合并后应该为单节点query值
	if !stack.IsValueNode() {
		log.Warnf("domain to sql error: stack.Len() %d %v", stack.Count(), self.result)
	}

	query = stack.String()
	joins := strings.Join(self.joins, " AND ")
	if joins != "" {
		query = fmt.Sprintf("(%s) AND %s", joins, query)
	}

	return []string{query}, res_params //lParams.Flatten()
}

// """ Returns the list of tables for SQL queries, like select from ... """
func (self *TExpression) get_tables() *utils.TStringList {
	tables := utils.NewStringList()
	for _, leaf := range self.result {
		for _, table := range leaf.get_tables().Items() {
			name := table.String()
			if !tables.Has(name) {
				tables.PushString(name)
			}
		}
	}

	//table_name := quoteStr(self.root_model.table)
	table_name := self.root_model.table
	if !tables.Has(table_name) {
		tables.PushString(table_name)
	}

	return tables
}
