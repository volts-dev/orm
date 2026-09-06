package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/orm/domain"
	ormerr "github.com/volts-dev/orm/errors"
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
		Table string
		orm   *TOrm
		// session 是发起本次解析的会话。x2many 叶子改写要在**同一个 schema 和事务**
		// 里查中间表/对端表——NewSession 用的是 orm.Schema，schema 隔离租户下会落到
		// public 查空。见 expr_x2many.go 文件头。
		session    *TSession
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

	// HIERARCHY_FUNCS 层级操作符 → 展开函数。实现见 expr_hierarchy.go。
	// 必须在 init() 里填而不是字面量初始化：展开函数会回头调 session.Read()，
	// 而那条链最终又指回本表，字面量形式构成初始化环、编译不过。
	HIERARCHY_FUNCS = map[string]hierarchyFunc{}
)

func init() {
	HIERARCHY_FUNCS["child_of"] = child_of_domain
	HIERARCHY_FUNCS["parent_of"] = parent_of_domain
}

func NewExpression(orm *TOrm, model *TModel, dom *domain.TDomainNode, context map[string]any, session ...*TSession) (*TExpression, error) {
	exp := &TExpression{
		orm:        orm,
		root_model: model,
		joins:      make([]string, 0),
	}
	if len(session) > 0 {
		exp.session = session[0]
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
		// ★ 原来只 log 一句就把记账不平的树交下去：`['|', leaf]` 这种少一项的 domain
		//   照样生成 SQL（多出来的 '|' 被吞掉，剩下的叶子按 AND 生效），结果集"看着
		//   正常"却不是调用方写的那个条件。Odoo 在这里抛 ValueError，本仓也一样拒绝。
		return nil, ormerr.New(ormerr.ErrInvalidDomain,
			fmt.Errorf("domain is syntactically not correct (operator/term arity mismatch): %s", domain.Domain2String(node)))
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
			op := n.String()
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
				// 非操作符的裸值(包括空项)原样传下去，由 parse() 的叶子闸门点名报错。
				// 原来这里外面套着 `if op != ""`，空项被**直接吞掉**：'&' 于是少了一个
				// 操作数，到 toSql 时静默退化成只剩一边的条件——又是一次不报错的放宽。
				result.Push(n)
			}
		} else {

			// (...)
			// # negate tells whether the subdomain starting with token must be negated

			if n.IsLeafNode() && is_negate {
				left, operator, right := n.String(0), n.String(1), n.Item(2)
				if negOp, has := domain.TERM_OPERATORS_NEGATION[operator]; has {
					// ★ 取反后的三元组必须落成**一个叶子节点**再 Push。
					//
					// Push 是变参追加，不是"用这三样造一个叶子"：
					// `result.Push(left, negOp, right)` 会把 left/negOp/right 拍成
					// 三个平级孩子，于是
					//   ['!','&',(user_id,=,4),(partner_id,in,[1,2])]
					// 产出 ["|","user_id","!=",4,"partner_id","not in",[1,2]]
					// —— 7 个平级孩子。回到 parse() 时第二个孩子 "user_id" 是
					// Count()==0 的值节点，撞上畸形叶子的闸门，**整条 WHERE 静默
					// 消失**（真栈实测：3 行数据全回，应回 2；同一个 domain 走
					// Delete() 会把整表删光，因为 hasCondition() 只看 domain 结构
					// 非空就放行了 AllowUnsafe 守卫）。
					// 回归：expr_not_test.go。
					result.Push(domain.New(left, negOp, right))
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
func (self *TExpression) to_ids(value *domain.TDomainNode, comodel *TModel, context map[string]any, limit int64) (*domain.TDomainNode, error) {
	if value == nil {
		return nil, fmt.Errorf("to_ids: the right operand is missing")
	}

	/* 分类：数字就是 id，直接用；字符才需要按名字查回 id */
	// ★ 数字判断必须排在前面。原来第一条是 `!value.IsListNode() && value.String() != ""`，
	//   标量 id(如 ('id','child_of',5))的 String() 是 "5" 非空，于是被当成**名字**
	//   丢去 NameSearch —— 走到下面那次查询，而 IsIntLeaf 那条分支永远轮不到。
	if value.IsNumeric() {
		return value, nil
	}
	if value.IsListNode() && value.IsIntLeaf() {
		//# given this nonsensical domain, it is generally cheaper to
		// # interpret False as [], so that "X child_of False" will
		//# match nothing
		return value, nil
	}

	var names []string
	if !value.IsListNode() {
		if s := value.String(); s != "" {
			names = append(names, s)
		}
	} else if value.IsStringList() {
		// 如果传入的是字符则可能是名称
		names = append(names, value.Strings()...)
	}

	if len(names) == 0 {
		return value, nil
	}

	/* 将分类出来名称查询并传回ID */
	// ★ NameSearch 的 error 必须接住：原来是 `lRecords, _ :=` 然后直接
	//   `lRecords.Data`，对端查询一旦失败(或本就没有 rec_name)就是**空指针崩溃**。
	//   真栈实测 `[('id','child_of',1)]` 直接 SIGSEGV —— 也就是外部传进来的
	//   domain 能把进程打崩。
	_domain := domain.New(comodel.recName, "in", value.Flatten()...)
	lRecords, err := comodel.NameSearch("", _domain, "ilike", limit, "", context)
	if err != nil {
		return nil, err
	}

	result := domain.NewDomainNode()
	if lRecords == nil {
		return result, nil
	}
	for _, rec := range lRecords.Data {
		result.Push(rec.FieldByName(comodel.idField).AsString()) //ODO: id 可能是Rec_id
	}
	return result, nil
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
			// 校验叶子完整性，避免对 1~2 元素的畸形叶子越界访问 Item(1)/Item(2) 触发 panic。
			//
			// ★ 任何元素个数不对的项一律**报错**，绝不静默跳过。
			//   这里原来对 cnt==0 写的是 `return nil`——注释说"空项静默跳过"，实际是
			//   从整个 parse() 返回：栈里**尚未处理的所有条件**跟着一起蒸发，而且返回
			//   的是 nil(成功)。distribute_not 产出平铺节点时正是从这里漏出去的，外部
			//   特征是"一加 ! 就整表返回"且全程不报错。
			//   即便只 continue 也不行：从 AND 列表里摘掉一项 = 放宽筛选，而 toSql 的
			//   栈上还少一个操作数，结果同样是静默放宽。宁可报错。
			if cnt := ex_leaf.leaf.Count(); cnt != 3 {
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

			// 相对日期字面量 '-365d' / 'today -365d' → 绝对日期。必须在这里(生成
			// SQL 之前)换掉：再往下值就直接进 params 发给数据库了。见 expr_relative_date.go
			resolveRelativeDate(field, right)
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
			ids2, ierr := self.to_ids(right, model, context, 0)
			if ierr != nil {
				return ierr
			}
			// 展开成一条普通的 (id in [...])，见 expr_hierarchy.go。
			//
			// 此前这两个函数是空实现(只剩注释掉的 Python 和 `return nil`)，而这里直接
			// `dom.Reversed()` 再遍历：Reversed() 对 nil 回空节点、循环一次不走，于是
			// 整条叶子**凭空消失**——child_of 筛选等于没筛、整表返回，且不报错。
			// 展开结果是**一条**叶子，直接入 result；不能再走 Nodes() 遍历，那会把
			// 叶子的三个孩子当成三条平级叶子拆开(本仓踩过多次的形状)。
			dom, derr := fn(self, left.String(), ids2, model, "")
			if derr != nil {
				return derr
			}
			ex_leaf.leaf = dom
			self.push_result(ex_leaf)

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
			// `partner_id.name` 这类穿过 m2o 的路径：先在对端查出 id，再把叶子换成
			// (partner_id in [...])。
			//
			// ★ 这里的 comodel 变量其实是**字段所属**模型（parse() 里那句
			//   GetModel(field.ModelName()) 的注释写着 "the model of the field owner"，
			//   名字起反了），必须自己解析真正的对端。
			// ★ 子查询走 subSession：Records() 是 NewSession，用的是 orm.Schema 而不是
			//   当前会话的 schema——schema 隔离租户（VectorsSystem 的 system）下会去查
			//   public 的同名表，查空即"这条关系过滤器筛不出任何东西"。
			// ★ 必须 Limit(-1)：不给就是 DefaultLimit=500，对端第 501 条之后的记录
			//   悄悄不算数。
			// ★ 叶子必须是 3 个孩子且第三个是 LIST_NODE。原来是
			//   Push(path[0], "in") 再 Push(ids...)——ids 有两个以上时那是个 N 孩子的
			//   畸形节点，IsLeafNode() 直接为 false。
			if hasPlaceholder(right) {
				return errPlaceholderInSubQuery(field, ex_leaf.leaf)
			}
			target, terr := self.orm.GetModel(field.RelatedModelName())
			if terr != nil {
				return terr
			}
			// ★ 右值必须整个节点传下去，不能用 right.Value：右值是列表时
			//   （`post_id.name in [a,b]`）Value 是 nil，拼出来的子条件恒不匹配，
			//   于是"多选几个"反而一条都筛不出来。
			subNode := domain.NewDomainNode()
			subNode.Push(path[1])
			subNode.Push(operator.String())
			subNode.Push(right.Clone())
			lDs, rerr := self.subSession().Model(target.String()).Domain(subNode).Limit(-1).Read()
			if rerr != nil {
				return rerr
			}
			var right_ids []any
			if lDs != nil {
				right_ids = lDs.Keys()
			}
			ex_leaf.leaf = idInLeaf(model.idField, path[0], right_ids)
			self.push(ex_leaf)

		} else if len(path) > 1 && field.Store() && utils.IndexOf(field.TypeName(), TYPE_M2M, TYPE_O2M) != -1 {
			// Making search easier when there is a left operand as column.o2m or column.m2m
			//
			// ★ 目前被上面的 isX2many() 抢先接管（o2m/m2m 的 Store() 恒 false），是死
			//   代码；但原来的三处写法都是"坏了不报"的形状，一并修掉免得分支顺序一动
			//   就复活：
			//   1) 用 fmt.Sprintf 把右值拼进 domain **字符串**——右值带引号就能改写域
			//      结构，且右值是列表时 `right.String()` 拼出来的根本不是合法域；
			//   2) `lDs, _ :=` 丢掉错误后直接 lDs.Keys()，对端查询一失败就空指针；
			//   3) `Push(idField,"in")` 再 `Push(ids...)` 造出 2+N 个孩子的畸形叶子，
			//      ids 多于一个时 IsLeafNode() 直接为 false。
			//   一律改成传节点 + propagate error + idInLeaf（与 m2o 路径一致）。
			subNode := domain.NewDomainNode()
			subNode.Push(path[1])
			subNode.Push(operator.String())
			subNode.Push(right.Clone())
			target, terr := self.orm.GetModel(field.RelatedModelName())
			if terr != nil {
				return terr
			}
			lDs, rerr := self.subSession().Model(target.String()).Domain(subNode).Limit(-1).Read()
			if rerr != nil {
				return rerr
			}
			var right_ids []any
			if lDs != nil {
				right_ids = lDs.Keys()
			}

			lDs, rerr = self.subSession().Model(model.String()).
				Domain(idInLeaf(model.idField, path[0], right_ids)).Limit(-1).Read()
			if rerr != nil {
				return rerr
			}
			var table_ids []any
			if lDs != nil {
				table_ids = lDs.Keys()
			}
			ex_leaf.leaf = idInLeaf(model.idField, model.idField, table_ids)
			self.push(ex_leaf)

		} else if isX2many(field) {
			// 直接（或点号路径的）x2many 叶子：翻译成主键条件。
			//
			// ★ 这条分支必须排在 `!field.Store()` **之前**：o2m/m2m 的 store 都是
			//   false，落到那条分支就是"生成空节点、条件消失、整表返回"。参见
			//   expr_x2many.go 文件头（含真栈实测的 54→54 / 56→56 / 9→9）。
			//
			// ★ 出错一律往上抛，不吞。这里吞掉错误的后果不是少一条日志，而是回到
			//   丢条件的老路——用户看到的是一份"看着正常"的错数据。
			newLeaf, err := self.resolveX2manyLeaf(model, field, path, operator, right)
			if err != nil {
				return err
			}
			ex_leaf.leaf = newLeaf
			self.push_result(ex_leaf)

		} else if !field.Store() && field.Searcher() != nil {
			// 非存储字段自带 search 钩子：让它把自己翻译成由存储列构成的 domain。
			// 见 field_searcher.go。
			node, err := field.Searcher()(&TFieldSearchContext{
				Model:    model,
				Field:    field,
				Session:  self.session,
				Operator: operator.String(),
				Value:    right.Value,
				Right:    right,
				Leaf:     ex_leaf.leaf,
			})
			if err != nil {
				return err
			}
			// ★ nil 表示"一条都不匹配"，落成恒假。绝不能落成恒真或干脆不 push——
			//   那就退回本次修复之前的"筛了等于没筛"。
			//
			// 恒假写成 `(id in [0,0])` 而不是 domain.FALSE_LEAF：那个常量是
			// `"(0, '=', 1)"`（带空格、单引号），而重新入栈后 is_false_leaf 拿
			// Domain2String 的输出 `(0,"=",1)` 去比，永远对不上，于是 "0" 被当成
			// 一个字段名报 `Invalid field <0>`。主键是雪花 id，0 永远不存在。
			if node == nil || node.Count() == 0 {
				falseLeaf := domain.NewDomainNode()
				falseLeaf.Push(model.idField)
				falseLeaf.Push("in")
				falseLeaf.Push(idListNode(nil))
				node = falseLeaf
			}
			// 钩子可以只还一个叶子，也可以还一整棵带 |/! 的树；normalize_domain
			// 负责补上隐式的 `&`。少了它，两个叶子会在 toSql 的栈上各留一个，
			// 生成结构性错误的 SQL（只会打一句 warn，查询照跑）。
			node, err = normalize_domain(node)
			if err != nil {
				return err
			}
			node = node.Reversed()
			for _, elem := range node.Nodes() {
				self.push(create_substitution_leaf(ex_leaf, elem, model, true))
			}

		} else if !field.Store() {
			//# Non-stored field should provide an implementation of search.
			var node *domain.TDomainNode
			if !field.SearchOnSelf() {
				//# field does not support search!
				// ★ field.Name 是方法，必须**调用**。少这对括号打出来的是
				//   `Non-stored field %!s(func() string=0x...)`——而这条日志是
				//   "非存储字段进了 domain"（后果是筛选条件被静默丢掉、返回全表）
				//   的唯一线索，不点名等于没有。
				log.Errf("Non-stored field %s@%s cannot be searched, the leaf is DROPPED "+
					"(the filter silently matches everything).", field.Name(), field.ModelName())
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
			// ★ 本条及下面两条 x2many 分支目前都被上面的 isX2many() 抢先接管(o2m/m2m 的
			//   Store() 恒 false，isX2many 排在更前面)，属于死代码。但它们原来的写法
			//   —— 空函数体、或只 log.Errf 不 return —— 全是"条件被静默丢掉、整表
			//   返回"的形状。一律改成往上抛，免得哪天分支顺序一动就复活。
			return fmt.Errorf("domain operator %q on one2many %s@%s is not implemented yet (leaf %s)",
				operator.String(), field.Name(), field.ModelName(), domain.Domain2String(ex_leaf.leaf))

		} else if field.TypeName() == TYPE_O2M {
			// TODO one2many
			return log.Errf("the one2many %s@%s is no implemented!", field.Name(), field.ModelName())
		} else if field.TypeName() == TYPE_M2M {
			// TODO many2many
			return log.Errf("the many2many %s@%s is no implemented!", field.Name(), field.ModelName())
		} else if field.TypeName() == TYPE_M2O {
			if fn, has := HIERARCHY_FUNCS[operator.String()]; has {
				// m2o 上的层级查询分两种，对标 Odoo expression.py：
				//
				//   对端是别的模型 —— ('partner_id','child_of',[7])：
				//     在**对端**里展开 7 的后代，落成 (partner_id in [后代...])。
				//   对端就是本模型 —— ('parent_id','child_of',[7])：
				//     这个字段本身就是父链接，在**本模型**里沿它展开，落成 (id in [...])。
				//
				// 分不清这两种会得到一棵错的树：前者用本模型的 parent_id 去走对端的层级，
				// 后者把父链接当成普通外键匹配，都是"筛出来了、但筛错了"。
				comodel, cerr := self.orm.GetModel(field.RelatedModelName())
				if cerr != nil {
					return cerr
				}
				comodelBase := comodel.GetBase()
				ids2, ierr := self.to_ids(right, comodelBase, context, 0)
				if ierr != nil {
					return ierr
				}

				var dom *domain.TDomainNode
				var derr error
				if comodelBase.String() != model.String() {
					dom, derr = fn(self, left.String(), ids2, comodelBase, "")
				} else {
					dom, derr = fn(self, model.idField, ids2, model, left.String())
				}
				if derr != nil {
					return derr
				}
				ex_leaf.leaf = dom
				self.push_result(ex_leaf)

			} else if isNameOperand(operator.String(), right) {
				// 右值是**名字**而不是 id：先在对端按 rec_name 查出 id，再把叶子换成
				// (本字段 in [...])。对标 Odoo expression.py 的 m2o 分支。
				//
				// 不这么做的话，`('journal_id','ilike','杂项')` 生成的是
				// `journal_id::text ilike '%杂项%'` —— 拿**主键的文本形式**去比名字，
				// 恒回 0 条。真栈 2026-08-21 实测：parent_id ilike 'Azure' 回 0
				// （真值是 3），journal_id ilike '销项' 回 0（真值是 8）。
				// 这一类的错法是"少给"，不像丢条件那样"全给"，所以更难被发现——
				// 用户只会觉得"搜不到"。
				//
				// 操作符原样交给对端（含 not like 一族）：对 m2o 这个标量而言，
				// "名字不含 X 的凭证" == "journal_id 落在（名字不含 X 的日记账）里"，
				// 不需要再翻一次符号（x2many 那边要翻，因为那是"存在一条关联"的语义）。
				target, terr := self.orm.GetModel(field.RelatedModelName())
				if terr != nil {
					return terr
				}
				recName := target.GetRecordName()
				// GetRecordName() 在模型既无 recName 声明又无 name 列时**回落到主键**。
				// 那种模型压根没有名字可搜，拿 id 去 ilike 会直接撞
				// `pq: operator does not exist: bigint ~~* unknown`（主键这一列不会被
				// 加 ::text 转换）。走下面的退路即可。
				noName := recName == "" || recName == target.IdField()
				if serr := assertSearchable(target, recName); noName || serr != nil {
					if noName {
						serr = fmt.Errorf("%s 没有名字列（rec_name 回落到主键）", target.String())
					}
					// 对端 rec_name 不可搜时**保持旧行为**并点名，不报错：
					// 旧行为的错法是回 0 条（可见、安全），而下钻到不可搜的对端会让它
					// 整表返回，于是 (field in [对端全部 id]) —— 那是"筛了等于没筛",
					// 正是本轮在消灭的东西。两害相权。
					log.Warnf("m2o %s@%s 按名字过滤退回按 id 文本比较（几乎必然回 0 条）：%s",
						field.Name(), field.ModelName(), serr.Error())
					self.push_result(ex_leaf)
				} else {
					subNode := domain.NewDomainNode()
					subNode.Push(recName)
					subNode.Push(operator.String())
					subNode.Push(right.Clone())
					lDs, rerr := self.subSession().Model(target.String()).Domain(subNode).Limit(-1).Read()
					if rerr != nil {
						return rerr
					}
					var right_ids []any
					if lDs != nil {
						right_ids = lDs.Keys()
					}
					ex_leaf.leaf = idInLeaf(model.idField, path[0], right_ids)
					self.push_result(ex_leaf)
				}

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
// isEmptyRelation 返回"这个 many2one 没有指向任何记录"的 SQL 片段。
//
// Odoo 里 `('x','=',False)` 是表达"关系为空"的标准写法，移植过来的 XML（视图
// domain、动作 domain、**记录规则的 domain_force**）到处都是。本仓一个关键差异是：
// 未设置的 m2o 在库里有**三种**形态，同一张表里就能同时见到 ——
//
//	res_partner.company_id   NULL=0   0=0   -1=41   真值=13
//	res_partner.user_id      NULL=51  0=0   -1=0    真值=3
//	res_partner.parent_id    NULL=27  0=1   -1=0    真值=26
//
// 三种来源各不相同：create 时省略该字段落 NULL；界面表单把空 m2o 发成 false 或 0，
// 落 0；`SetDefaultByName("company_id", -1)` 落 -1（记录规则那边也是按
// `company_id < 1` 判"无归属"的）。所以只写 `IS NULL` 会漏掉后两种。
//
// 主键是雪花 id，恒为正，`<= 0` 不会误伤真实记录。
func isEmptyRelation(aliasTable, column string) string {
	return fmt.Sprintf(`(%s."%s" IS NULL OR %s."%s" <= 0)`, aliasTable, column, aliasTable, column)
}

func isNotEmptyRelation(aliasTable, column string) string {
	return fmt.Sprintf(`(%s."%s" IS NOT NULL AND %s."%s" > 0)`, aliasTable, column, aliasTable, column)
}

// isFalsyValue / isTruthyValue 把 `('x','=',False)` / `('x','!=',False)` 翻成 SQL。
//
// # 为什么不只是 many2one
//
// isEmptyRelation 那一版只认 many2one/one2one，别的类型一路把 false 当普通右值绑进
// `x = ?`，后果按列类型分成**两种**，其中一种不报错：
//
//	timestamp  → pq: invalid input syntax for type timestamp: "false" (22007)   ← 500
//	bigint     → pq: invalid input syntax for type bigint: "false"    (22P02)   ← 500
//	varchar    → PG 把参数当文本，跑成 `x = 'false'` —— **不报错，静默筛错**
//
// 第三种最坏。2026-08-23 真栈实测：calendar_event 里 privacy 为空串的有 2 行，
// `('privacy','=',False)` 应当返回这 2 行，实际返回 **0 行**，日志里只有一条
// 正常的 INFO SQL。界面上表现为"这个筛选器点了没反应"，没有任何东西指向这里。
//
// # 各类型的"空"是什么
//
// 照 Odoo 的 falsy 语义，不是一律 IS NULL：
//
//	字符/选择   NULL 或 ''    ——  '' 是界面清空文本框的落库形态，只写 IS NULL 会漏
//	数值        NULL 或 0     ——  Odoo 里 0 是 falsy
//	时间/二进制 NULL          ——  没有"零值"落库形态；空字符串塞进 timestamp 会报错
//	json/jsonb  NULL          ——  `jsonb = ''` 本身就是 22P02，不能套字符那条
//	关系        见 isEmptyRelation（NULL/0/-1 三态）
//
// bool 刻意**不**在这里处理：下方原有的 Bool 分支已经是对的，让它继续负责，
// 免得同一语义有两个出口。
//
// 返回的第二个值是"认不认得这个类型"。认不得就返回 false，调用方回落到原来的
// 通用路径 —— 宁可维持现状，也不要对着未知类型瞎猜一条谓词。
func isFalsyValue(typeName, aliasTable, column string) (string, bool) {
	q := fmt.Sprintf(`%s."%s"`, aliasTable, column)
	switch typeName {
	case TYPE_M2O, TYPE_O2O:
		return isEmptyRelation(aliasTable, column), true
	case TYPE_SELECTION:
		return fmt.Sprintf(`(%s IS NULL OR %s = '')`, q, q), true
	case TYPE_JSONB, TYPE_PROPERTIES, TYPE_PROPERTIES_DEFINITION:
		return fmt.Sprintf(`(%s IS NULL)`, q), true
	case Bool, Boolean:
		return "", false // 交给下方原有的 Bool 分支
	}
	switch strings.ToUpper(typeName) {
	case Json, Jsonb:
		return fmt.Sprintf(`(%s IS NULL)`, q), true
	}
	switch SqlTypes[strings.ToUpper(typeName)] {
	case TEXT_TYPE:
		return fmt.Sprintf(`(%s IS NULL OR %s = '')`, q, q), true
	case NUMERIC_TYPE:
		return fmt.Sprintf(`(%s IS NULL OR %s = 0)`, q, q), true
	case TIME_TYPE, BLOB_TYPE:
		return fmt.Sprintf(`(%s IS NULL)`, q), true
	}
	return "", false
}

// falsyOk 只回答"isFalsyValue/isTruthyValue 认不认得这个类型"，
// 供上面的 else-if 链在进入分支前判断 —— Go 的 else-if 没法先算出值再决定进不进。
func falsyOk(typeName, op string) bool {
	var ok bool
	if op == "=" {
		_, ok = isFalsyValue(typeName, "t", "c")
	} else {
		_, ok = isTruthyValue(typeName, "t", "c")
	}
	return ok
}

// isEmptyRight 判右值是不是"空"的两种形态：nil 与空字符串。
//
// ★ 不能用 utils.ToString(v) == "" 一把梭：`ToString(false)` 是 "false"、
// `ToString(0)` 是 "0"，都不该落进来（false 由上面的 falsy 分支管，0 是真值）。
func isEmptyRight(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

// emptyRightIsNullish 判"右值为空"在这个列类型上该不该按 IS NULL 语义处理。
//
// 只对**非文本**列成立 —— 文本列的 `= ”` 是合法比较，通用分支已经处理对了。
// 认不得的类型返回 false 回落原路：宁可维持现状，也不要把一个没验过的类型
// 改道到新语义上。
func emptyRightIsNullish(typeName string) bool {
	switch typeName {
	case TYPE_SELECTION, Bool, Boolean:
		return false
	}
	if SqlTypes[strings.ToUpper(typeName)] == TEXT_TYPE {
		return false
	}
	_, ok := isFalsyValue(typeName, "t", "c")
	return ok
}

func isTruthyValue(typeName, aliasTable, column string) (string, bool) {
	q := fmt.Sprintf(`%s."%s"`, aliasTable, column)
	switch typeName {
	case TYPE_M2O, TYPE_O2O:
		return isNotEmptyRelation(aliasTable, column), true
	case TYPE_SELECTION:
		return fmt.Sprintf(`(%s IS NOT NULL AND %s <> '')`, q, q), true
	case TYPE_JSONB, TYPE_PROPERTIES, TYPE_PROPERTIES_DEFINITION:
		return fmt.Sprintf(`(%s IS NOT NULL)`, q), true
	case Bool, Boolean:
		return "", false
	}
	switch strings.ToUpper(typeName) {
	case Json, Jsonb:
		return fmt.Sprintf(`(%s IS NOT NULL)`, q), true
	}
	switch SqlTypes[strings.ToUpper(typeName)] {
	case TEXT_TYPE:
		return fmt.Sprintf(`(%s IS NOT NULL AND %s <> '')`, q, q), true
	case NUMERIC_TYPE:
		return fmt.Sprintf(`(%s IS NOT NULL AND %s <> 0)`, q, q), true
	case TIME_TYPE, BLOB_TYPE:
		return fmt.Sprintf(`(%s IS NOT NULL)`, q), true
	}
	return "", false
}

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

	// `properties.<项名>` 这种带点的左值不是字段名，会被下面那道闸判成非法字段。
	// 先在这里认出来：认得出就在取完值之后走 jsonb 分支，认不出就照旧当非法处理。
	propField, propName := resolvePropertyPath(model, left.String())

	// ★ 两处判据原来都是失效的：
	//   - `left.ValueIn(TRUE_LEAF, FALSE_LEAF)` 拿**左值**("1"/"0")去和整条叶子的
	//     字符串常量比，恒 false；
	//   - `left.ValueIn(MAGIC_COLUMNS)` 把 []string 当一个 any 传进 `...any`，
	//     ValueIn 的 switch 只认 string / *TDomainNode，也恒 false。
	//   合法字段能命中 GetFieldByName 所以没炸，但这道闸门实际只剩一个条件。
	if !leaf.IsTrueLeaf() && !leaf.IsFalseLeaf() &&
		model.GetFieldByName(left.String()) == nil &&
		utils.IndexOf(left.String(), MAGIC_COLUMNS...) == -1 && propField == nil { //
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

	// ★ 判"是不是一组值"用 Count() 而不是 IsListNode()。
	//   IsLeafNode() 是个**带副作用的谓词**：认出三元 LIST_NODE 是叶子后会就地把
	//   nodeType 改写成 LEAF_NODE 作记忆化。于是一个恰好三元、且中间那个值恰好是
	//   term 操作符的**值列表**（`('op','in',['=','<','>'])`），只要这棵树被渲染过
	//   一次（打一遍日志就够），IsListNode() 就变成 false，整组值被当成标量、
	//   Value 又是 nil —— 条件恒不匹配。Count() 不受记忆化影响。
	if right.Count() > 0 {
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

	if propField != nil {
		q, p, err := propertyLeafToSql(self.orm.dialect, aliasTable, propField.Name(), propName, operator.String(), vals)
		if err != nil {
			log.Errf("%s in domain term %s", err.Error(), leaf.String())
			return "0 = 1", res_params, res_arg
		}
		return q, p, res_arg

	} else if leaf.IsTrueLeaf() {
		res_query = "TRUE"
		res_params = nil

	} else if leaf.IsFalseLeaf() {
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
		if right.Count() > 0 { // 一组值（同上，不能用 IsListNode）
			// ★ 布尔 false 要从绑定值里**剔除**并转成 IS NULL 语义
			//   （Odoo 惯例：('x','in',[1,2,False]) == x in (1,2) OR x IS NULL）。
			//
			//   原来写的是 `res_params = utils.SliceDelete(res_params, any(idx))`：
			//   SliceDelete 是**按值**删除，删的是"等于 idx 这个数"的元素，而不是第
			//   idx 个元素（何况 idx 是 int、参数多是 int64，类型都对不上，基本恒不
			//   命中）。后果一是 false 原样留在绑定参数里——PG 上整型列会直接
			//   `operator does not exist: bigint = boolean`；后果二是万一命中就删掉
			//   一个真 id，并让占位符个数与参数个数对不上。
			//   占位符个数一律以 res_params 为准，不能再用 len(vals)。
			check_nulls := false
			for _, item := range vals {
				if utils.IsBoolItf(item) && !utils.ToBool(item) {
					check_nulls = true
					continue
				}
				res_params = append(res_params, item)
			}

			// In 值操作
			if len(res_params) > 0 {
				holders := strings.Repeat("?,", len(res_params)-1) + "?"
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

			// 关系字段的"空"不止 NULL（见 isEmptyRelation），`('x','in',[1,False])`
			// 若只补 IS NULL，会漏掉落成 0 / -1 的那些行。非关系字段维持 IS NULL。
			isRel := is_field && (field.TypeName() == TYPE_M2O || field.TypeName() == TYPE_O2O)
			nullSql := fmt.Sprintf(`%s."%s" IS NULL`, aliasTable, left.String())
			notNullSql := fmt.Sprintf(`%s."%s" IS NOT NULL`, aliasTable, left.String())
			if isRel {
				nullSql = isEmptyRelation(aliasTable, left.String())
				notNullSql = isNotEmptyRelation(aliasTable, left.String())
			}

			if check_nulls && operator.String() == "in" {
				res_query = fmt.Sprintf(`(%s OR %s)`, res_query, nullSql)

			} else if !check_nulls && operator.String() == "not in" {
				res_query = fmt.Sprintf(`(%s OR %s)`, res_query, nullSql)

			} else if check_nulls && operator.String() == "not in" {
				res_query = fmt.Sprintf(`(%s AND %s)`, res_query, notNullSql) // needed only for TRUE.
			}

		} else if right.Value == nil { // 空集合
			// (left,'in',[]) / (left,'not in',[])：解析器把空列表拆成了 Value 为 nil
			// 的标量节点(见 parser.go 的单元素拆包)，走不到上面的列表分支。
			// 原来落到最后那条 "单值" 分支，生成 `x = NULL`——'in' 恰好回 0 条(蒙对)，
			// 'not in' 也回 0 条(**应回全部**)。
			if operator.String() == "in" {
				res_query = "FALSE"
			} else {
				res_query = "TRUE"
			}
			res_params = nil

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
			// 单值 in/not in：必须**按操作符**生成，不能一律写 '='。
			//
			// 解析器会把单元素列表拆包成标量(parser.go 的 `if list.Count()==1
			// { return list.Item(0) }`)，所以 `('name','not in',['x'])` 和
			// `.NotIn("name","x")` 到这里 right 都不是 LIST_NODE。原来这里硬写
			// `= ?`，于是 **not in 变成了 in**——实测 `.NotIn("name","probe_a")`
			// 生成 `WHERE name = 'probe_a'`，恰好只回被排除的那一条。两个及以上
			// 值走上面的列表分支才是对的，所以单参数用法长期没被测出来。
			//
			// not in 补 `OR IS NULL`：与上面列表分支的 not in 语义保持一致
			// （SQL 里 NULL 不满足 `!=`，但"不在集合里"应当包含 NULL 行）。
			if operator.String() == "in" {
				res_query = fmt.Sprintf(`(%s."%s" = ?)`, aliasTable, left.String()) //TODO quote
			} else {
				res_query = fmt.Sprintf(`((%s."%s" != ?) OR %s."%s" IS NULL)`,
					aliasTable, left.String(), aliasTable, left.String())
			}
			res_params = append(res_params, vals[0])

		}
	} else if is_field && len(vals) > 0 && utils.IsBoolItf(vals[0]) && !utils.ToBool(vals[0]) &&
		(operator.String() == "=" || operator.String() == "!=") && falsyOk(field.TypeName(), operator.String()) {
		// 任意字段上的 `= False` / `!= False`：Odoo 语义是"这个字段为空 / 非空"。
		//
		// 原来这里没有分支，false 一路当成普通右值绑进 `x = ?`：many2one 上是
		// `pq: invalid input syntax for type bigint: "false" (22P02)`，datetime 上是
		// 22007，varchar 上**不报错但筛的是 `x = 'false'`**。三种后果、一个病灶。
		// 本仓移植过来的 XML 里这条写法有 220 处（2026-08-23 全仓分拣），其中挂在
		// 记录规则 domain_force 上的一崩就是整模型读不出来。
		//
		// 各类型"空"的准确形态见 isFalsyValue 的注释。
		if operator.String() == "=" {
			res_query, _ = isFalsyValue(field.TypeName(), aliasTable, left.String())
		} else {
			res_query, _ = isTruthyValue(field.TypeName(), aliasTable, left.String())
		}
		res_params = nil

	} else if is_field && len(vals) > 0 && isEmptyRight(vals[0]) &&
		(operator.String() == "=" || operator.String() == "!=") && emptyRightIsNullish(field.TypeName()) {
		// **非文本**列上的 `= ''` / `= nil`：与上面那条 `= False` 是同一个语义
		// （"这个字段为空"），只是右值形态不同。
		//
		// 不修的表现是 500，而这条 domain **不是人写的，是框架自己生成的**：
		// read_group 的下钻域（core/model/model_controller_read_group.go 的
		// formatGroups）对"分组键为空"的那一组，m2o 走 `AsString()` 得到空串、
		// 日期走显式 nil，两者都拼成 `(字段,'=',空)` 发回前端；看板/透视点进
		// 那一列时原样打回来：
		//
		//	SELECT ... WHERE ((folder_id = $3) OR folder_id IS NULL)  [args] [... ""]
		//	pq: invalid input syntax for type bigint: "" (22P02)
		//
		// 2026-08-31 documents 的看板按工作区分组时真栈撞到：根工作区没有父级，
		// 于是必然有一个"无工作区"的组，点它就是 500。任何"按可为空的 m2o／日期
		// 分组"的看板都在这条路上，只是别处的分组字段大多必填才没暴露。
		//
		// 走到这里之前的行为：落进最后那个通用分支，`add_null := right.String() == ""`
		// 已经补了 `OR x IS NULL`，但 `x = ?` 那半边照样把空串绑给 bigint/timestamp。
		// PG 上是 22P02/22007，**sqlite 上被静默折算成 `x = 0` 回错行**。
		//
		// ★ 文本类（varchar/text/selection）**有意不进这个分支**：它们的
		// `= ''` 本来就是合法比较，通用分支产出的 `(x = '' OR x IS NULL)` 与
		// isFalsyValue 的输出等价，改道没有收益，只会多一处行为变更。
		if operator.String() == "=" {
			res_query, _ = isFalsyValue(field.TypeName(), aliasTable, left.String())
		} else {
			res_query, _ = isTruthyValue(field.TypeName(), aliasTable, left.String())
		}
		res_params = nil

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

		// like 家族的 SQL 由方言生成：`::text` 转型和 ILIKE 都是 postgres 专有的，
		// 这里原来对所有方言硬拼 `cast = "::text"`，sqlite/mysql 上任何 like/ilike
		// 的 domain 查询都直接 `unrecognized token: ":"` —— 也就是整个模糊搜索在
		// 非 postgres 后端上根本不能用。见 IDialect.LikeClause。
		isLike := strings.HasSuffix(sql_operator, "like")

		// #组合Sql
		if is_field || utils.IndexOf(left.String(), MAGIC_COLUMNS...) != -1 {
			// ★ 这里原来第二条走的是 `left.ValueIn(MAGIC_COLUMNS)`——ValueIn 的形参是
			//   `...any`，把 []string 整个当**一个** any 传进去，它的 switch 只认
			//   string 和 *TDomainNode，两个 case 都不匹配，恒 false（同 1052 行那道
			//   字段合法性闸门）。改用与 parse() 一致的 IndexOf(..., MAGIC_COLUMNS...)。
			//unaccent = self._unaccent if sql_operator.endswith('like') else lambda x: x
			column := fmt.Sprintf("%s.%s", aliasTable, quoter.Quote(left.String()))
			if isLike {
				res_query = self.orm.dialect.LikeClause(column, sql_operator)
			} else {
				res_query = fmt.Sprintf("(%s %s ?)", column, sql_operator)
			}

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
			// ★ 两处曾经的错：
			//   1) `stack.Push("(NOT (%s))", stack.Pop().String())` —— Push 是**变参
			//      追加**不是 Printf，字面量 "(NOT (%s))" 会原样拼进 SQL，而被 Pop
			//      出来的子句被当成另一个平级元素压回去。
			//   2) 栈里只剩一个元素时它是 VALUE_NODE，旧版 Pop() 只认 LIST_NODE 返回
			//      nil，`.String()` 当场空指针崩溃(Pop 已在 domain.go 里对称化)。
			// 走到这条分支的前提是该 '!' 没被 distribute_not 下推——即操作符不在
			// TERM_OPERATORS_NEGATION 里(=like/=ilike/=?/child_of)。
			sub := stack.Pop()
			if sub == nil {
				log.Errf("domain to sql: '!' has no operand, the leaf is DROPPED: %v", self.result)
				continue
			}
			stack.Push(fmt.Sprintf("(NOT (%s))", sub.String()))

		} else {
			// domain 操作符
			q1 = stack.Pop()
			q2 = stack.Pop()
			if q1 != nil && q2 != nil {
				lStr := fmt.Sprintf("(%s %s %s)", q1.String(), domain.DOMAIN_OPERATORS_KEYWORDS[eleaf.leaf.String()], q2.String())
				stack.Push(lStr)
			} else {
				// 操作数不够 = domain 结构本身坏了。压回已取出的那个，避免把
				// 一整条子句静默丢掉(丢条件就是放宽筛选)。
				log.Errf("domain to sql: operator %q lacks operands, the domain is malformed: %v",
					eleaf.leaf.String(), self.result)
				if q1 != nil {
					stack.Push(q1.String())
				}
				if q2 != nil {
					stack.Push(q2.String())
				}
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
