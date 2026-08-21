package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

/*
把**直接**写在 domain 里的 x2many 叶子翻译成主键条件。

在此之前 `[('tag_ids', 'in', [11,12])]` 这种叶子是**被静默丢弃**的：o2m 与 m2m 的
`field.store` 都是 false（见 TOne2ManyField.Init / TMany2ManyField.Init），于是
parse() 里 `len(path) > 1 && field.Store() && (M2M||O2M)` 那条分支根本不可达，叶子
一路落到 `!field.Store()` 分支，生成一个空节点、什么都不 push——条件消失，SQL 里没有
它，**整表返回**。

后果不是报错而是错数据，且界面上完全看不出来：联系人按标签筛、发票明细按税筛、产品
按标签筛，点了等于没点。2026-08-21 在真栈上量过，按一个不存在的关联 id 去筛：

	res.partner.category_id      不筛 54 → 筛完 54
	account.move.line.tax_ids    不筛 56 → 筛完 56
	pro.tmpl.product_tag_ids     不筛  9 → 筛完  9

全仓一次扫描（1654 个视图 → 136 个带 search 的模型 → 337 条查询）命中 17 个这样的
现场，全是搜索面板上的常规过滤器。

### 为什么是"物化 id"而不是 JOIN

expr 这套有 JOIN 机制（add_join_context），但 expr_leaf.go:generate_alias 里明写着
JOIN 路径**不带 schema 前缀**。vectors 用 schema 隔离租户，走 JOIN 会让子查询落到
public——那就是把"筛了等于没筛"换个地方重演，只是更难查。所以这里取 Odoo
expression.py 的老做法：先查出对端 id，再把叶子换成 (id in [...])。

代价是中间结果要进内存、也进 SQL 参数表。搜索面板的场景（用户挑一个标签/科目）量级
很小；真遇到极大结果集时 x2manyIdWarnThreshold 会打一条 Warn 指出是哪个字段，而不是
悄悄截断——截断就又回到"错数据不报错"了。

### 三个必须守住的点

  - **子查询必须 Limit(-1)。** 不给 limit 是 DefaultLimit=500（session_crwd.go），
    500 条以后的关联记录会被悄悄丢掉，筛出来的父记录就少了一截。
  - **子会话必须 Clone() 而不是 NewSession()。** NewSession 用的是 orm.Schema，
    不是当前会话的 schema；Clone 同时带上 tx 和 Schema。也正因为带着 tx，
    **绝不能对 clone 调 Close()**——Close 会把外层事务一起 Rollback 掉。
  - **id 列表至少两个元素。** 单元素列表会退化成 VALUE_NODE，而 leaf_to_sql 的
    in/not in 分支只认 LIST_NODE，退化后走的是"右值是布尔"的错误分支。补哨兵 0
    顶成列表；主键是雪花 id，0 永远不存在，进 in / not in 都不改变结果。
*/

// x2manyIdSentinel 用来把单元素 id 列表顶成 LIST_NODE。主键永远不会是 0。
const x2manyIdSentinel = 0

// x2manyIdWarnThreshold 超过这个规模就提醒一声：不截断，只是让慢查询能被追溯到字段。
const x2manyIdWarnThreshold = 10000

// isX2many 判断字段是否是需要本文件处理的直接 x2many 叶子。
func isX2many(field IField) bool {
	return field != nil && utils.IndexOf(field.TypeName(), TYPE_M2M, TYPE_O2M) != -1
}

// subSession 派生一个用于中间查询的会话。
//
// 见文件头：必须 Clone（带 schema 与事务），且调用方不得 Close。
func (self *TExpression) subSession() *TSession {
	if self.session != nil {
		return self.session.Clone()
	}
	return NewSession(self.orm)
}

// qualify 给表名补上会话 schema。中间表/关联表这条路是手工拼 SQL，不经过
// where_calc→qualifiedTable 那套限定，必须自己补，否则落到 search_path 默认
// schema（通常 public），查空。
func (self *TExpression) qualify(table string) string {
	if self.session != nil && self.session.Schema != "" {
		return self.session.Schema + "." + table
	}
	if self.orm != nil && self.orm.Schema != "" {
		return self.orm.Schema + "." + table
	}
	return table
}

// resolveX2manyLeaf 把一条 x2many 叶子换成 (id in [...]) / (id not in [...])。
//
// 支持 Odoo expression.py 的全部右值形态：
//
//	('tag_ids', 'in',    [11,12])   —— 直接给对端 id（搜索面板最常见的一种）
//	('tag_ids', '=',     11)        —— 单个对端 id
//	('tag_ids', 'ilike', 'abc')     —— 按对端记录名匹配
//	('tag_ids.name', 'ilike', 'abc')—— 点号路径，按对端某字段匹配
//	('tag_ids', '=',     False)     —— "一条关联记录都没有"
func (self *TExpression) resolveX2manyLeaf(model *TModel, field IField,
	path []string, operator, right *domain.TDomainNode) (*domain.TDomainNode, error) {
	op := operator.String()
	// child_of / parent_of 在 x2many 上没有实现。老代码在这里是一个**空函数体**，
	// 条件被静默丢掉；这里改成报错——全仓 child_of/parent_of 全部用在 m2o 上
	// （partner_id / categ_id / company_id / department_id），x2many 一处也没有，
	// 真出现时宁可让调用方看见，也不要再发一份"看着正常"的错数据。
	if _, isHierarchy := HIERARCHY_FUNCS[op]; isHierarchy {
		return nil, fmt.Errorf("operator %q is not supported on the %s field %s@%s",
			op, field.TypeName(), field.Name(), field.ModelName())
	}
	negative := utils.IndexOf(op, domain.NEGATIVE_TERM_OPERATORS...) != -1

	// ★ 不要用 parse() 里那个叫 comodel 的变量——它是 GetModel(field.ModelName())，
	//   而 ModelName() 返回的是**字段所属**的模型（注释写着 "the model of the field
	//   owner"，名字起反了）。拿它当对端会去父表里找标签名，查得到才怪。
	comodel, err := self.orm.GetModel(field.RelatedModelName())
	if err != nil {
		return nil, err
	}

	if hasPlaceholder(right) {
		return nil, errPlaceholderInSubQuery(field, right)
	}

	targetIds, matchEmpty, err := self.x2manyTargetIds(comodel, path, op, right)
	if err != nil {
		return nil, err
	}

	// ★ 用独立的布尔量表达"取所有有关联的 owner"，不要拿 targetIds == nil 当哨兵：
	//   对端一条都没查到时 searchComodel 返回的也是 nil，两种情形会被混成一种，
	//   于是"筛一个不存在的标签"被当成"列出所有有标签的记录"——54 条里回 5 条，
	//   看着像筛过了，其实答案完全相反。
	ownerIds, err := self.x2manyOwnerIds(comodel, field, targetIds, matchEmpty)
	if err != nil {
		return nil, err
	}

	if len(ownerIds) > x2manyIdWarnThreshold {
		log.Warnf("domain leaf %s@%s resolved to %d ids; the filter is correct but the "+
			"generated IN(...) is large — consider narrowing the search.",
			field.Name(), field.ModelName(), len(ownerIds))
	}

	// right 不是 False：命中这些 owner。right 是 False：**排除**这些 owner。
	// 负向操作符再把结论翻一次。两者同时成立时互相抵消，故用异或。
	resultOp := "in"
	if negative != matchEmpty {
		resultOp = "not in"
	}

	leaf := domain.NewDomainNode()
	leaf.Push(model.idField)
	leaf.Push(resultOp)
	leaf.Push(idListNode(ownerIds))
	return leaf, nil
}

// idListNode 把 id 切片包成 leaf 的第三个孩子，并保证它是 LIST_NODE。见文件头第三点。
//
// ★ 非空时补位用**重复第一个 id**，不用哨兵 0。
//
//	`col in (X, X)` 与 `col in (X)` 等价，且对任何列都安全；而 0 只对**主键**安全——
//	外键列上 0 是合法值（空 m2o 落库就是 0 / -1），补个 0 进去等于顺带把"没有上级的
//	记录"也捞回来。真栈 2026-08-21 实测：`parent_id ilike 'Azure'` 回 4 条，其中 3 条
//	是真的子联系人、第 4 条是 parent_id=0 的那条；而 `parent_id ilike 'zzqx'` 回 1 条
//	——本该是 0。
//
// ★ 空列表仍然落成 [0,0]，所以**空列表只能用在主键列上**。外键列上要表达"一条都不
//
//	匹配"，请把整条叶子改挂到主键上（见 idInLeaf）。
func idListNode(ids []any) *domain.TDomainNode {
	node := domain.NewDomainNode()
	for _, id := range ids {
		node.Push(id)
	}
	if len(ids) > 0 {
		for node.Count() < 2 {
			node.Push(ids[0])
		}
		return node
	}
	for node.Count() < 2 {
		node.Push(x2manyIdSentinel)
	}
	return node
}

// idInLeaf 生成 `(column in [ids])`；ids 为空时改挂到主键上生成一条恒假条件。
//
// column 通常是外键列，而外键列上 0 是合法值，所以"一条都不匹配"不能写成
// `column in (0,0)`——那会把空外键的记录全捞回来。主键是雪花 id，0 永远不存在。
func idInLeaf(idField, column string, ids []any) *domain.TDomainNode {
	leaf := domain.NewDomainNode()
	if len(ids) == 0 {
		leaf.Push(idField)
	} else {
		leaf.Push(column)
	}
	leaf.Push("in")
	leaf.Push(idListNode(ids))
	return leaf
}

// x2manyTargetIds 解析右值，返回对端（comodel）的 id 集合。
//
// 第二个返回值为 true 表示右值是 False —— 语义是"没有任何关联记录"，此时 id 集合无意义。
func (self *TExpression) x2manyTargetIds(comodel IModel, path []string, op string,
	right *domain.TDomainNode) ([]any, bool, error) {
	// 点号路径：按对端的某个字段搜。这里不做 False 判断——('tag_ids.name','=',False)
	// 问的是"名字为空的标签"，是一次正常的对端查询。
	if len(path) > 1 {
		if err := assertSearchable(comodel, path[1]); err != nil {
			return nil, false, err
		}
		ids, err := self.searchComodel(comodel, domain.New(path[1], op, right.Value))
		return ids, false, err
	}

	if isDomainFalse(right) {
		return nil, true, nil
	}

	// 数字（或数字列表）就是对端 id，不必再查一次库。
	//
	// ★ 雪花 id 到了这里常常是**字符串**：BigNumberToString 打开后前端拿到的 id 就是
	//   字符串，原样发回来。所以 in/not in/=/!= 这几个"集合/等值"操作符下，纯数字串
	//   一律按 id 认。（真有个标签就叫 "12345" 时会被误认成 id——用 ilike 搜名字。）
	//   like/ilike 一族则永远走名称匹配，它们本来就是文本操作符。
	if idsAllowed(op) {
		if ids, ok := numericIds(right); ok {
			return ids, false, nil
		}
	}

	// 剩下的是字符串：按对端记录名匹配。负向操作符要先取反再查对端——Odoo 的
	// "not any" 语义：先找出**匹配**的标签，再排除拥有它们的父记录。若直接拿
	// 'not ilike' 去查对端，得到的是"不匹配的标签"，排除掉拥有它们的父记录，
	// 结论正好是错的。
	searchOp := op
	if neg, has := domain.TERM_OPERATORS_NEGATION[op]; has &&
		utils.IndexOf(op, domain.NEGATIVE_TERM_OPERATORS...) != -1 {
		searchOp = neg
	}
	recName := comodel.GetRecordName()
	if err := assertSearchable(comodel, recName); err != nil {
		return nil, false, err
	}
	var node *domain.TDomainNode
	if right.IsListNode() {
		node = domain.New(recName, "in", right.Flatten()...)
	} else {
		node = domain.New(recName, searchOp, right.Value)
	}
	ids, err := self.searchComodel(comodel, node)
	return ids, false, err
}

// assertSearchable 在**下钻到对端之前**拦住"这个字段查了等于没查"的情形。
//
// 对端的名称字段本身是非存储计算列时（pro.tmpl.attr.value.name、
// res.partner.category.display_name 就是），拿它去查对端会掉进同一个坑：叶子被丢掉、
// 对端整表返回、于是本模型"有任何关联的记录"全部命中。外部表现是"按属性值名字筛变体，
// 筛出来的比不筛还多"——把一个筛选器变成了它的反面。
//
// 宁可报错也不放行：与本文件 child_of 那处同一个取舍，也与 pro.variant 投影列
// "宁可 500 也不静默给错数据" 的先例一致。消除办法是让那个名称字段可搜（落成存储列，
// 或给它一个 search 实现），不是把这里放开。
func assertSearchable(comodel IModel, fieldName string) error {
	f := comodel.GetFieldByName(fieldName)
	if f == nil {
		return fmt.Errorf("field %s does not exist on %s", fieldName, comodel.String())
	}
	if f.Store() || f.SearchOnSelf() {
		return nil
	}
	return fmt.Errorf("cannot search %s@%s: it is a non-stored field with no search "+
		"implementation (see fieldStatment.Searcher), so the lookup would match every "+
		"record instead of filtering", fieldName, comodel.String())
}

// searchComodel 在对端模型上跑一次查询并取回 id。
func (self *TExpression) searchComodel(comodel IModel, node *domain.TDomainNode) ([]any, error) {
	sess := self.subSession() // 见文件头：不能 Close
	ds, err := sess.Model(comodel.String()).Domain(node).Limit(-1).Read()
	if err != nil {
		return nil, err
	}
	if ds == nil {
		return nil, nil
	}
	return ds.Keys(), nil
}

// x2manyOwnerIds 把对端 id 映射回本模型 id。
//
// anyRelated 为 true 时忽略 targetIds，取"所有**有**关联记录的本模型 id"（供 right=False
// 取反用）。它必须是独立入参：targetIds 为空既可能是"右值是 False"，也可能是"对端一条都
// 没匹配上"，两者的正确答案正好相反。
//
// 这一步走手拼 SQL 而不是 ORM：m2m 的中间表是 UpdateDb 里用一个空 TModel 注册出来的，
// 没有任何字段元数据，走 where_calc 会直接报 `Invalid field <xxx_id>`。o2m 这边为对称
// 起见也用同一条路。
func (self *TExpression) x2manyOwnerIds(comodel IModel, field IField, targetIds []any, anyRelated bool) ([]any, error) {
	var table, ownerCol, keyCol string

	switch field.TypeName() {
	case TYPE_O2M:
		// 子表上那一列外键就是父记录 id。注意用 RelatedKeyName()：OneToManyFK()
		// 对应的 oneToManyFK 字段在全仓从未被赋值，恒为空串。
		table = comodel.Table()
		ownerCol = field.RelatedKeyName()
		keyCol = comodel.IdField()
	case TYPE_M2M:
		table = fmtTableName(field.JoinModelName())
		ownerCol = field.JoinSourceKey() // 中间表里指向本模型的列
		keyCol = field.RelatedKeyName()  // 中间表里指向对端的列
	default:
		return nil, fmt.Errorf("x2manyOwnerIds called on a %s field %s@%s",
			field.TypeName(), field.Name(), field.ModelName())
	}
	if table == "" || ownerCol == "" || keyCol == "" {
		return nil, fmt.Errorf("incomplete relation metadata for %s@%s (table=%q owner=%q key=%q)",
			field.Name(), field.ModelName(), table, ownerCol, keyCol)
	}

	var where string
	var params []any
	if anyRelated {
		where = fmt.Sprintf(`"%s" IS NOT NULL AND "%s" <> 0`, ownerCol, ownerCol)
	} else if len(targetIds) == 0 {
		return nil, nil // 对端一条都没匹配上，父记录自然也没有
	} else {
		where = fmt.Sprintf(`"%s" IN (%s)`, keyCol, JoinPlaceholder("?", ",", len(targetIds)))
		params = targetIds
	}

	query := fmt.Sprintf(`SELECT DISTINCT "%s" FROM %s WHERE %s`,
		ownerCol, self.qualify(table), where)

	sess := self.subSession() // 见文件头：不能 Close
	ds, err := sess.Query(query, params...)
	if err != nil {
		return nil, err
	}
	if ds == nil {
		return nil, nil
	}
	return ds.Keys(ownerCol), nil
}

// isDomainFalse 判断右值是否是 Odoo 意义上的 False。
//
// 走过 parser 之后 `False` 是**字符串** "False"（见 domain/parser.go），不是 bool，
// 所以 utils.IsBoolItf 那套判不出来。
func isDomainFalse(node *domain.TDomainNode) bool {
	if node == nil {
		return true
	}
	if node.IsListNode() {
		return false // 空/非空列表都不是 False，是"这些 id"
	}
	switch v := node.Value.(type) {
	case nil:
		return true
	case bool:
		return !v
	case string:
		return strings.EqualFold(v, "false") || v == ""
	}
	return false
}

// isNullLiteral 判断右值是不是字面量 NULL（不分大小写）。列表节点不算——
// `('x','in',['NULL'])` 是"这些值里有个叫 NULL 的"，另一回事。
func isNullLiteral(node *domain.TDomainNode) bool {
	if node == nil || node.IsListNode() {
		return false
	}
	s, ok := node.Value.(string)
	return ok && strings.EqualFold(s, "null")
}

// idsAllowed 判断这个操作符下"纯数字串"该不该被当成 id。
func idsAllowed(op string) bool {
	return utils.IndexOf(op, "in", "not in", "=", "!=", "IN", "NOT IN") != -1
}

// idValueOf 判断单个值是不是 id，是则一并返回规范化后的值。
//
// ★ 不能用 `node.Value.(string)` 断言：JSON 解码出来的数字在这条链路上是
//
//	**encoding/json.Number**——它是个 `type Number string` 的具名类型，类型断言到
//	string 会失败，IsNumeric() 的 case 列表里也没有它。两边都不认，于是一个普普通通的
//	`('tag_ids','in',[123])` 被当成"按标签名搜 123"，掉进对端的名称查询里。
//
//	这个误判的后果取决于对端的名称字段是不是存储列，所以它伪装得很好：
//	pro.tag 的 name 是真列 → 查不到 → 返回 0 条，看着完全正常；
//	pro.tmpl.attr.value 的 name 是非存储计算列 → 那条件被丢掉 → **整表 7 条全回来**，
//	于是"按一个不存在的属性值筛"筛出了 8 个变体。同一段代码，两种表象。
//	2026-08-21 真栈上就是靠这一对差异把它揪出来的。
//
//	一律走 utils.ToString，任何具名字符串类型都能落地。
func idValueOf(node *domain.TDomainNode) (any, bool) {
	if node.IsListNode() {
		return nil, false
	}
	if node.IsNumeric() {
		return node.Value, true
	}
	s := utils.ToString(node.Value)
	if s == "" {
		return nil, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return nil, false
		}
	}
	return s, true
}

// numericIds 在右值是数字或数字列表时取出 id 集合。
func numericIds(node *domain.TDomainNode) ([]any, bool) {
	if node == nil {
		return nil, false
	}
	if node.IsListNode() {
		if node.Count() == 0 {
			return nil, false
		}
		ids := make([]any, 0, node.Count())
		for _, child := range node.Nodes() {
			v, ok := idValueOf(child)
			if !ok {
				return nil, false
			}
			ids = append(ids, v)
		}
		return ids, true
	}
	if v, ok := idValueOf(node); ok {
		return []any{v}, true
	}
	return nil, false
}

// textOperators 是"按文本匹配"的一族。落在 many2one 上时右值一定是名字，
// 哪怕它长得像个数字（标签叫 "12345" 也是名字）。
var textOperators = []string{
	"like", "not like", "ilike", "not ilike", "=like", "=ilike",
	"LIKE", "NOT LIKE", "ILIKE", "NOT ILIKE", "=LIKE", "=ILIKE",
}

// isNameOperand 判断一条 many2one 叶子的右值该按**名字**解析还是按 id。
//
//	文本操作符          → 永远按名字
//	其余（= / in / …）  → 值不是 id 才按名字
//	False / 空 / NULL   → 都不是，交给原来的 NULL 判断
func isNameOperand(op string, right *domain.TDomainNode) bool {
	if right == nil || isDomainFalse(right) {
		return false
	}
	// 字面量 NULL 配等值操作符是**空值判断**，不是对端的名字。
	//
	// 本仓 domain 里 "NULL" 一直就是"空"的写法：TStatement.Where 收的是 SQL 串，
	// 交给 String2Domain 解析后 `site_id IS NULL` 出来的正是 ('site_id','=','NULL')
	// （`IS` / `IS NOT` 在 domain/parser.go 里映射成 `=` / `!=`），而 leaf_to_sql
	// 对 ('x','=','NULL') / ('x','!=','NULL') 直接落 IS NULL / IS NOT NULL。
	//
	// 不在这里拦下来，many2one 会拿 "NULL" 去对端 rec_name 里搜名字，搜不到 →
	// idInLeaf → `(id in (0,0))` 恒假，于是 `Where("site_id=? OR site_id IS NULL")`
	// 的第二支静默失效：外键为空的行一条都回不来，查询照常成功、照常返回一批
	// "看着正常"的行。错法是**少给**，不报错、不留日志。
	// 2026-08-22 真栈：website.menu 里 website_id 为 NULL 的 Shop / Contact us /
	// Courses 就是这么从前台导航整批消失的，只剩数据里写了 website_id 的 Home。
	if isNullLiteral(right) && utils.IndexOf(op, "=", "!=", "<>") != -1 {
		return false
	}
	// 占位符留到 leaf_to_sql 才有值，这里查不了名字——退回按 id 文本比较（旧行为），
	// 那条路会正确消费 params。
	if hasPlaceholder(right) {
		return false
	}
	if utils.IndexOf(op, textOperators...) != -1 {
		return true
	}
	if right.IsListNode() {
		if right.Count() == 0 {
			return false
		}
		for _, child := range right.Nodes() {
			if _, ok := idValueOf(child); !ok {
				return true
			}
		}
		return false
	}
	_, ok := idValueOf(right)
	return !ok
}

// hasPlaceholder 判断右值里是否含 SQL 占位符（`?` / `%s`）。
//
// `Where("order_id=?", id)` 这种写法把值留在 Statement.Params 里，直到 leaf_to_sql
// 生成 SQL 时才按顺序消费；domain 树上留下的只是一个 "?" 字面量。所以凡是要**提前
// 跑一次子查询**的路径（关系路径、按名字查 m2o、x2many 下钻）都拿不到真值，必须先认出它。
func hasPlaceholder(node *domain.TDomainNode) bool {
	if node == nil {
		return false
	}
	if node.IsListNode() {
		for _, child := range node.Nodes() {
			if hasPlaceholder(child) {
				return true
			}
		}
		return false
	}
	return utils.IndexOf(node.String(), "?", "%s") != -1
}

// errPlaceholderInSubQuery 是那几条"必须先跑子查询"的路径遇到占位符时的统一说法。
func errPlaceholderInSubQuery(field IField, leaf *domain.TDomainNode) error {
	return fmt.Errorf("domain term %s on %s@%s needs a sub-query to resolve, so it cannot "+
		"take a `?` placeholder (the value only exists at SQL-generation time). "+
		"Pass the value inline via Domain(...) instead of Where(...,args)",
		leaf.String(), field.Name(), field.ModelName())
}
