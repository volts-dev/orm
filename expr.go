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

	HIERARCHY_FUNCS = map[string]func(*domain.TDomainNode, *domain.TDomainNode, *TModel, string, string, map[string]any) *domain.TDomainNode{
		"child_of":  child_of_domain,
		"parent_of": parent_of_domain}
)

func NewExpression(orm *TOrm, model *TModel, dom *domain.TDomainNode, context map[string]any) (*TExpression, error) {
	exp := &TExpression{
		orm:        orm,
		root_model: model,
		joins:      make([]string, 0),
	}

	node, err := normalize_domain(dom)
	if err != nil {
		return nil, err
	}

	exp.Expression = distribute_not(node)
	if err = exp.parse(context); err != nil {
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

	node = node.FlattenNode()

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
	var expected = 1
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
	new_join_context := leaf.join_context //复制
	return NewExtendedLeaf(new_elements, new_model, new_join_context, internal)
}

func child_of_domain(left *domain.TDomainNode, ids *domain.TDomainNode, left_model *TModel, parent string, prefix string, context map[string]any) *domain.TDomainNode {
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

func parent_of_domain(left *domain.TDomainNode, ids *domain.TDomainNode, left_model *TModel, parent string, prefix string, context map[string]any) *domain.TDomainNode {
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
			is_negate = utils.ToBool(negate.String())
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
// quoteTableWithSchema 与 quoteStr 同样的硬编码双引号风格，但支持 schema 前缀：
// 生成 "schema"."table"，而不是把整个 "schema.table" 当一个字面标识符塞进一对引号
// 里——后者在 Postgres 中会被解析成名字里带字面点号的单个标识符，永远匹配不到真实的
// schema.table，等价于查询了一张不存在的表（对该 schema 下的所有行都是"查无此表"，
// 但 Go 侧只是 JOIN 结果为空，不报错）。
func quoteTableWithSchema(schema, table string) string {
	if schema == "" {
		return quoteStr(table)
	}
	return quoteStr(schema) + "." + quoteStr(table)
}

// 生成Joint用的表别名
//
// schema 是当前会话的活动 schema（如 VectorsSystem 租户的 "system"）。被 JOIN 的
// comodel 表名必须带上它，否则在非默认 schema 的租户下，生成的 FROM 子句里这张表
// 会落到 search_path 的默认 schema（通常是 public），JOIN 找不到匹配行——表现为
// 一整条主记录都读不到（如 res.user 读 company_id 触发的 res_user__partner_id
// delegate join）。
func generate_table_alias(src_table_alias string, joined_tables [][]string, schema string) (string, string) {
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

	return srcTableName, fmt.Sprintf("%s as %s", quoteTableWithSchema(schema, joined_tables[0][0]), quoteStr(srcTableName))
}

func idsToSqlHolder(ids ...any) string {
	ln := len(ids)
	if ln == 0 {
		return ""
	}
	return strings.Repeat("?,", ln-1) + "?"
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
func (self *TExpression) to_ids(value *domain.TDomainNode, comodel *TModel, context map[string]any, limit int64) *domain.TDomainNode {
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
		_domain := domain.New(comodel.recName, "in", value.Flatten()...)
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
func (self *TExpression) parse(context map[string]any) error {
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
			// 校验叶子完整性，避免对 1~2 元素的畸形叶子越界访问 Item(1)/Item(2) 触发 panic
			if cnt := ex_leaf.leaf.Count(); cnt != 3 {
				if cnt == 0 {
					return nil // 空项静默跳过（保持原行为）
				}
				return fmt.Errorf("invalid domain leaf: expected 3 elements, got %d: %s", cnt, ex_leaf.leaf.String())
			}
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

		} else if field.IsInherited() {
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
			next_model, err := model.orm.GetModel(related_field.RelatedTableName)
			if err != nil {
				return err
			}
			ex_leaf.add_join_context(next_model.GetBase(), model.obj.GetRelationByName(next_model.String()), next_model.IdField(), model.obj.GetRelationByName(next_model.String()))
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

		} else if utils.IndexOf(path[0], MAGIC_COLUMNS...) != -1 {
			self.push_result(ex_leaf)

		} else if len(path) > 1 && field.TypeName() == TYPE_M2O && field.IsAutoJoin() {
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
			ex_leaf.add_join_context(comodel.GetBase(), fieldName, comodel.IdField(), fieldName)
			self.push(create_substitution_leaf(ex_leaf, domain.NewDomainNode(path[1], operator.String(), right.String()), comodel.GetBase(), false))

		} else if len(path) > 1 && field.Store() && field.TypeName() == TYPE_O2M && field.IsAutoJoin() {
			//  # res_partner.id = res_partner__bank_ids.partner_id
			ex_leaf.add_join_context(comodel.GetBase(), comodel.IdField(), field.OneToManyFK(), fieldName)
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

		} else if len(path) > 1 && field.Store() && field.TypeName() == TYPE_M2O {
			domain_str := fmt.Sprintf(`[('%s', '%s', '%s')]`, path[1], operator.String(), right.String())
			lDs, _ := comodel.Records().Domain(domain_str).Read() //search(cr, uid, [(path[1], operator, right)], context=dict(context, active_test=False))
			right_ids := lDs.Keys()
			ex_leaf.leaf = domain.NewDomainNode()
			ex_leaf.leaf.Push(path[0], "in")
			ex_leaf.leaf.Push(right_ids...) //    leaf.leaf = (path[0], 'in', right_ids)
			self.push(ex_leaf)

		} else if len(path) > 1 && field.Store() && utils.IndexOf(field.TypeName(), TYPE_M2M, TYPE_O2M) != -1 {
			// Making search easier when there is a left operand as column.o2m or column.m2m
			domain_str := fmt.Sprintf(`[('%s', '%s', '%s')]`, path[1], operator.String(), right.String())
			lDs, _ := comodel.Records().Domain(domain_str).Read()
			right_ids := lDs.Keys()

			domain_str = fmt.Sprintf(`[('%s', 'in', [%s])]`, path[0], idsToSqlHolder(right_ids))
			lDs, _ = model.Records().Domain(domain_str, right_ids...).Read()
			table_ids := lDs.Keys()
			ex_leaf.leaf = domain.NewDomainNode()
			ex_leaf.leaf.Push(model.idField, "in")
			ex_leaf.leaf.Push(table_ids...) //    leaf.leaf = (path[0], 'in', right_ids)
			self.push(ex_leaf)

		} else if !field.Store() {
			//# Non-stored field should provide an implementation of search.
			var node *domain.TDomainNode
			if !field.SearchOnSelf() {
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

		} else if field.TypeName() == TYPE_O2M && (operator.String() == "child_of" || operator.String() == "parent_of") {
			// -------------------------------------------------
			// RELATIONAL FIELDS
			// -------------------------------------------------

		} else if field.TypeName() == TYPE_O2M {
			// TODO one2many
			log.Errf("the one2many %s@%s is no implemented!", field.Name(), field.ModelName())
		} else if field.TypeName() == TYPE_M2M {
			// TODO many2many
			log.Errf("the many2many %s@%s is no implemented!", field.Name(), field.ModelName())
		} else if field.TypeName() == TYPE_M2O {
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

		} else if field.TypeName() == "binary" && field.(*TBinField).attachment {

		} else {
			// -------------------------------------------------
			// OTHER FIELDS
			// -> datetime fields: manage time part of the datetime
			//    column when it is not there
			// -> manage translatable fields
			// -------------------------------------------------

			if field.TypeName() == "datetime" && right != nil && right.Count() == 10 {
				// TODO: append time part to right (' 23:59:59' for >/<=, ' 00:00:00' otherwise)
				ltemp := domain.NewDomainNode()
				ltemp.Push(left, operator, right)
				self.push(create_substitution_leaf(ex_leaf, ltemp, model, false))

			} else if field.Translate() && right != nil && !utils.IsBlank(right.Value) { //column.translate and not callable(column.translate) and right:
				// 翻译字段：与普通字段一样 push_result，通配符 %...% 的包裹统一在
				// leaf_to_sql 里按 need_wildcard 处理，避免在此再包一次导致 %%...%%。
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
func (self *TExpression) leaf_to_sql(eleaf *TExtendedLeaf, params []any) (res_query string, res_params []any, res_arg []any) {
	var (
	//first_right_value interface{}   // 提供最终值以供条件判断
	)
	quoter := self.orm.dialect.Quoter()
	var vals []any              // 该Term 的值 每个Or,And等条件都有自己相当数量的值
	res_params = make([]any, 0) //domain.NewDomainNode()
	res_arg = params            // 初始化剩余参数

	model := eleaf.model

	leaf := eleaf.leaf
	left := leaf.Item(0)
	operator := leaf.Item(1)
	right := leaf.Item(2)

	field := model.GetFieldByName(left.String())
	is_field := field != nil // 是否是model字段
	//	is_holder := false

	// 合法性闸门：非法操作符或非法字段一律中止该叶子的 SQL 生成，返回恒假谓词，
	// 绝不把未经校验的字段名/操作符拼进 SQL（防注入）。合法查询的字段必能命中
	// GetFieldByName / MAGIC_COLUMNS / TRUE_LEAF|FALSE_LEAF，因此不会触发此分支。
	if utils.IndexOf(operator.String(), append(domain.TERM_OPERATORS, "inselect", "not inselect")...) == -1 {
		log.Errf(`Invalid operator %s in domain term %s`, operator.Strings(), leaf.String())
		return "0 = 1", res_params, res_arg
	}

	if !left.ValueIn(domain.TRUE_LEAF, domain.FALSE_LEAF) && model.GetFieldByName(left.String()) == nil && !left.ValueIn(MAGIC_COLUMNS) { //
		log.Errf(`Invalid field %s in domain term %s`, left.Strings(), leaf.String())
		return "0 = 1", res_params, res_arg
	}
	//        assert not isinstance(right, BaseModel), \
	//            "Invalid value %r in domain term %r" % (right, leaf)

	aliasTable, _ := eleaf.generate_alias()
	// 识别 SQL 占位符 (?, %s) 并逐个从 params 队列消费实际值。
	// res_arg 是消费完后剩下的 params，留给下一个 Term 使用。
	//
	// 早期实现写成 params[holder_count:1] —— 只对第一个 ? 凑效
	// (holder_count=0 时 params[0:1]，但 holder_count++ 之后 params[1:1] 是
	// 空切片，多占位符场景会丢值)。改用 params[holder_count] 直接索引。
	holder_count := 0
	consumeHolder := func(token string) (any, bool) {
		if utils.IndexOf(token, "?", "%s") == -1 {
			return nil, false
		}
		if holder_count >= len(params) {
			log.Errf("placeholder %q in domain term %s has no matching param (consumed %d of %d)",
				token, leaf.String(), holder_count, len(params))
			return nil, true
		}
		v := params[holder_count]
		holder_count++
		return v, true
	}

	if right.IsListNode() {
		for _, node := range right.Nodes() {
			if v, isHolder := consumeHolder(node.String()); isHolder {
				vals = append(vals, v)
			} else {
				vals = append(vals, node.Value)
			}
		}
	} else {
		if v, isHolder := consumeHolder(right.String()); isHolder {
			vals = append(vals, v)
		} else {
			vals = append(vals, right.Value)
		}
	}
	res_arg = params[holder_count:] // 剩余参数留给下个 Term

	/*	// 检测查询是否占位符?并获取值
				if utils.IndexOf(right.String(), "?", "%s") != -1 {
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
		res_query = fmt.Sprintf(`(%s."%s" in (%s))`, aliasTable, left.String(), holders)
		res_params = append(res_params, vals...)

	} else if operator.String() == "not inselect" {
		holders := strings.Repeat("?,", len(vals)-1) + "?"
		res_query = fmt.Sprintf(`%s."%s" not in (%s))`, aliasTable, left.String(), holders)
		res_params = append(res_params, vals...)

	} else if operator.ValueIn("in", "not in") { //# 数组值
		if right.IsListNode() {
			res_params = append(res_params, vals...)

			check_nulls := false
			for idx, item := range res_params {
				if utils.IsBoolItf(item) && !utils.ToBool(item) {
					check_nulls = true
					res_params = utils.SliceDelete(res_params, any(idx))
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
				res_query = fmt.Sprintf(`(%s."%s" %s (%s))`, aliasTable, left.String(), operator.String(), holders)
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
				res_query = fmt.Sprintf(`(%s OR %s."%s" IS NULL)`, res_query, aliasTable, left.String())

			} else if !check_nulls && operator.String() == "not in" {
				res_query = fmt.Sprintf(`(%s OR %s."%s" IS NULL)`, res_query, aliasTable, left.String())

			} else if check_nulls && operator.String() == "not in" {
				res_query = fmt.Sprintf(`(%s AND %s."%s" IS NOT NULL)`, res_query, aliasTable, left.String()) // needed only for TRUE.
			}

		} else if utils.IsBoolItf(vals[0]) { // Must not happen
			r := ""
			log.Errf(`The domain term "%s" should use the '=' or '!=' operator.`, leaf.String())
			if operator.String() == "in" {
				if utils.ToBool(vals[0]) {
					r = "NOT NULL"
				} else {
					r = "NULL"
				}
			} else {
				if utils.ToBool(vals[0]) {
					r = "NULL"
				} else {
					r = "NOT NULL"
				}
			}
			res_query = fmt.Sprintf(`(%s."%s" IS %s)`, aliasTable, left.String(), r)
			res_params = nil

			//  raise ValueError("Invalid domain term %r" % (leaf,))
		} else {
			// single value use "=" term
			res_query = fmt.Sprintf(`(%s."%s" = ?)`, aliasTable, left.String()) //TODO quote
			res_params = append(res_params, vals[0])

		}
	} else if is_field && (field.TypeName() == Bool) &&
		((operator.String() == "=" && !utils.ToBool(vals[0])) || (operator.String() == "!=" && utils.ToBool(vals[0]))) {
		// 字段是否Bool类型
		res_query = fmt.Sprintf(`(%s."%s" IS NULL or %s."%s" = false )`, aliasTable, left.String(), aliasTable, left.String())
		res_params = nil

	} else if (vals == nil || utils.ToString(vals[0]) == "NULL" /*utils.IsBlank(vals[0])*/) && operator.String() == "=" {
		res_query = fmt.Sprintf(`%s."%s" IS NULL `, aliasTable, left.String())
		res_params = nil

	} else if is_field && field.TypeName() == Bool &&
		((operator.String() == "!=" && !utils.ToBool(vals[0])) || (operator.String() == "==" && utils.ToBool(vals[0]))) {
		res_query = fmt.Sprintf(`(%s."%s" IS NOT NULL and %s."%s" != false)`, aliasTable, left.String(), aliasTable, left.String())
		res_params = nil

	} else if (vals == nil || utils.ToString(vals[0]) == "NULL" /*utils.IsBlank(vals[0])*/) && (operator.String() == "!=") {
		res_query = fmt.Sprintf(`%s."%s" IS NOT NULL`, aliasTable, left.String())
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
		res_query = fmt.Sprintf("%s.%s %s ?", aliasTable, self.root_model.idField, operator.String())
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
			column := fmt.Sprintf("%s.%s", aliasTable, quoter.Quote(left.String()))
			res_query = fmt.Sprintf("(%s %s %s)", column+cast, sql_operator, format)

		} else if left.ValueIn(MAGIC_COLUMNS) {
			res_query = fmt.Sprintf("(%s.\"%s\"%s %s ?)", aliasTable, left.String(), cast, sql_operator)

		} else {
			//# Must not happen
			log.Errf(`Invalid field %s in domain term %s`, left.String(), leaf.String())
		}

		if add_null {
			res_query = fmt.Sprintf(`(%s OR %s."%s" IS NULL)`, res_query, aliasTable, left.String())
		}

		// like/ilike 模糊匹配：把查询值包上通配符 %...%。不包的话 ilike 退化成整串
		// 大小写不敏感精确匹配——前端 many2one 的 name_search 传入片段(operator=ilike)
		// 就搜不到任何数据。need_wildcard 只对 like/ilike/not like/not ilike 为真；
		// =like/=ilike 是"原样"变体(上面已映射成 like/ilike 但 need_wildcard=false)，
		// 由调用方自带通配符，不在此包裹。
		if need_wildcard {
			for i, v := range vals {
				vals[i] = "%" + utils.ToString(v) + "%"
			}
		}

		res_params = append(res_params, vals...)
	}
	return res_query, res_params, res_arg
}

// to generate the SQL expression and params
func (self *TExpression) ToSql(params ...any) ([]string, []any) {
	return self.toSql(params...)
}

// 传递domain值并重新生成
// params sql value
func (self *TExpression) toSql(params ...any) ([]string, []any) {
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
	res_params := make([]any, 0)
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
