package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/utils"
)

type (

	/*   """
	     Dumb implementation of a Query object, using 3 string lists so far
	     for backwards compatibility with the (table, where_clause, where_params) previously used.

	     TODO: To be improved after v6.0 to rewrite part of the ORM and add support for:
	      - auto-generated multiple table aliases
	      - multiple joins to the same table with different conditions
	      - dynamic right-hand-side values in domains  (e.g. a.name = a.description)
	      - etc.
	    """*/
	TQuery struct {
		session *TSession
		// holds the list of tables joined using default JOIN.
		// the table names are stored double-quoted (backwards compatibility)
		tables              []string
		where_clause        []string
		where_clause_params []any
		joins               map[string][]*utils.TStringList
		extras              map[string]*utils.TStringList
		alias_mapping       map[string]string
	}
)

func NewQuery(session *TSession, tables []string, where_clause []string, params []any, joins map[string][]*utils.TStringList, extras map[string]*utils.TStringList) (q *TQuery) {
	q = &TQuery{session: session}

	//# holds the list of tables joined using default JOIN.
	//# the table names are stored double-quoted (backwards compatibility)
	q.tables = tables

	//# holds the list of WHERE clause elements, to be joined with
	//# 'AND' when generating the final query
	q.where_clause = where_clause

	//# holds the parameters for the formatting of `where_clause`, to be
	//# passed to psycopg's execute method.
	q.where_clause_params = params

	//# holds table joins done explicitly, supporting outer joins. The JOIN
	//# condition should not be in `where_clause`. The dict is used as follows:
	//#   self.joins = {
	//#                    'table_a': [
	//#                                  ('table_b', 'table_a_col1', 'table_b_col', 'LEFT JOIN'),
	//#                                  ('table_c', 'table_a_col2', 'table_c_col', 'LEFT JOIN'),
	//#                                  ('table_d', 'table_a_col3', 'table_d_col', 'JOIN'),
	//#                               ]
	//#                 }
	//#   which should lead to the following SQL:
	//#       SELECT ... FROM "table_a" LEFT JOIN "table_b" ON ("table_a"."table_a_col1" = "table_b"."table_b_col")
	//#                                 LEFT JOIN "table_c" ON ("table_a"."table_a_col2" = "table_c"."table_c_col")
	// 必须兜底建 map：addJoin 的显式分支要往里写，而调用方(where_calc)一律传 nil。
	// 此前显式分支是死代码，nil 从未被写过，改用 LEFT JOIN 后立刻会 panic。
	if joins == nil {
		joins = make(map[string][]*utils.TStringList)
	}
	q.joins = joins

	//# holds extra conditions for table joins that should not be in the where
	//# clause but in the join condition itself. The dict is used as follows:
	//#
	//#   self.extras = {
	//#       ('table_a', ('table_b', 'table_a_col1', 'table_b_col', 'LEFT JOIN')):
	//#           ('"table_b"."table_b_col3" = %s', [42])
	//#   }
	//#
	//# which should lead to the following SQL:
	//#
	//#   SELECT ... FROM "table_a"
	//#   LEFT JOIN "table_b" ON ("table_a"."table_a_col1" = "table_b"."table_b_col" AND "table_b"."table_b_col3" = 42)
	//#   ...
	q.extras = extras

	q.alias_mapping = make(map[string]string)
	return
}

// Returns (query_from, query_where, query_params).
func (self *TQuery) getSql() (fromClause, whereClause string, whereClauseParams []any) {
	self.alias_mapping = self.getAliasMapping()

	// 显式 JOIN 的右表也被 addJoin 记进了 self.tables。它们由 JOIN 子句带进 FROM，
	// **不能**再出现在逗号分隔的表列表里——那份列表没有连接条件，同一张表两处并存
	// 会让整条查询退化成笛卡尔积。先扫一遍 joins 把这些表挑出来。
	joined := make(map[string]bool, len(self.joins))
	for _, joins := range self.joins {
		for _, join := range joins {
			joined[self.alias_mapping[join.String(0)]] = true
		}
	}

	from_clause := make([]string, 0, len(self.tables))
	from_params := make([]any, 0)
	for _, table := range self.tables {
		if joined[table] {
			continue
		}

		if len(from_clause) > 0 {
			from_clause = append(from_clause, ",")
		}
		from_clause = append(from_clause, table)

		_, table_alias := get_alias_from_query(table)
		// emitted 防环：joins 理论上是棵树，但 addJoin 的去重只看 alias_statement，
		// 环一旦出现就是无限递归+爆栈，代价远大于一个 map。
		self.addJoinsForTable(unqualifyAlias(table_alias), &from_clause, &from_params, make(map[string]bool))
	}

	fromClause = strings.Join(from_clause, "")             // 上面已经添加","
	whereClause = strings.Join(self.where_clause, " AND ") // to string
	whereClauseParams = append(from_params, self.where_clause_params...)
	return fromClause, whereClause, whereClauseParams
}

/* """ Join a destination table to the current table.

    :param implicit: False if the join is an explicit join. This allows
        to fall back on the previous implementation of ``join`` before
        OpenERP 7.0. It therefore adds the JOIN specified in ``connection``
        If True, the join is done implicitely, by adding the table alias
        in the from clause and the join condition in the where clause
        of the query. Implicit joins do not handle outer, extra, extra_params parameters.
    :param connection: a tuple ``(lhs, table, lhs_col, col, link)``.
        The join corresponds to the SQL equivalent of::

        (lhs.lhs_col = table.col)

        Note that all connection elements are strings. Please refer to expression.py for more details about joins.

    :param outer: True if a LEFT OUTER JOIN should be used, if possible
              (no promotion to OUTER JOIN is supported in case the JOIN
              was already present in the query, as for the moment
              implicit INNER JOINs are only connected from NON-NULL
              columns so it would not be correct (e.g. for
              ``_inherits`` or when a domain criterion explicitly
              adds filtering)

    :param extra: A string with the extra join condition (SQL), or None.
        This is used to provide an additional condition to the join
        clause that cannot be added in the where clause (e.g., for LEFT
        JOIN concerns). The condition string should refer to the table
        aliases as "{lhs}" and "{rhs}".

    :param extra_params: a list of parameters for the `extra` condition.
"""*/
// hasAlias 判断 FROM 列表里是否已经有这个表别名。
//
// **必须按别名比，不能比整条 from 语句**。同一个别名会被两条路径加进来：
// statement.go 的 where_calc 用 qualifiedTable 拼（`system."res_partner" as "x"`），
// 这里用 generate_table_alias 拼（`"system"."res_partner" as "x"`）——两串不相等
// 但别名相同，按整串去重就漏掉，FROM 里于是出现两个同名别名，PostgreSQL 直接报
//
//	pq: table name "res_company__partner_id" specified more than once (42712)
//
// 只在**非默认 schema** 的租户上炸：schema 为空时两串恰好相等，去重才碰巧生效。
// 真机 2026-08-08 由超级租户（schema=system）的公司菜单撞出。
func (self *TQuery) hasAlias(alias string) bool {
	// 两侧都去掉 schema 前缀再比：没有 " as " 的条目，别名就是表名本身，
	// 而它可能带着 `system.` 前缀（qualifiedTable 的产物）。
	want := unqualifyAlias(alias)
	for _, t := range self.tables {
		if _, a := get_alias_from_query(t); unqualifyAlias(a) == want {
			return true
		}
	}
	return false
}

// 添加目标表到当前表
func (self *TQuery) addJoin(connection []string, implicit bool, outer bool, extra, extra_params map[string]any) (string, string) {
	// (lhs.lhs_col = table.col)
	lhs := connection[0]     // mdoel name
	lhs_col := connection[1] // field
	table := connection[2]   // relate model name
	col := connection[3]     // realte field

	link := connection[4]
	alias, alias_statement := generate_table_alias(lhs, [][]string{{table, link}}, self.session.Schema)

	if implicit {
		if !self.hasAlias(alias) {
			self.tables = append(self.tables, alias_statement)
			condition := fmt.Sprintf(`("%s"."%s" = "%s"."%s")`, lhs, lhs_col, alias, col)
			self.where_clause = append(self.where_clause, condition)
		}
		// else: already joined, no-op

		return alias, alias_statement
	} else {
		//aliases := self._get_table_aliases()
		// assert lhs in aliases, "Left-hand-side table %s must already be part of the query tables %s!" % (lhs, str(self.tables))
		if self.hasAlias(alias) {
			// already joined, must ignore (promotion to outer and multiple joins not supported yet)
		} else {
			// add JOIN
			join_tuple := utils.NewStringList()
			self.tables = append(self.tables, alias_statement)
			if outer {
				join_tuple.PushString(alias, lhs_col, col, "LEFT JOIN")
			} else {
				join_tuple.PushString(alias, lhs_col, col, "JOIN")
			}

			// 添加到Joins
			//self.joins.setdefault(lhs, []).append(join_tuple)
			// 注意：append 可能 realloc，必须把结果存回 self.joins[lhs]
			self.joins[lhs] = append(self.joins[lhs], join_tuple)

			// TODO: if extra != nil { self.extras[(lhs, join_tuple)] = extra.format(lhs, alias), extra_params }
		}
		return alias, alias_statement
	}
}

// unqualifyAlias 去掉 schema 前缀，返回该表在 SQL 里**真正暴露的别名**。
//
// 非默认 schema 的会话(如 VectorsSystem 租户的 "system")下，self.tables 里的条目是
// where_calc 限定过的 `system.res_company`，而 joins 是按**裸表名**建的键
// (inherits_join_calc 的 lhs 用 model.Table())。拿限定名去查 joins 一条都对不上：
// 委托继承(one2one/_inherits)的父表 JOIN 完全不渲染，而 SELECT 里
// `"res_company__partner_id"."city"` 这种别名限定列照常输出 —— postgres 直接
// `missing FROM-clause entry for table "res_company__partner_id" (42P01)`。
// 该错误在 m2o 内嵌子读取里被 OnRead 吞成一行日志，请求照常 200，表现为「同一条记录
// 里没有委托继承的 comodel 内嵌成功、res.company/res.user 只剩裸 id」。
//
// `FROM system.res_company` 在 SQL 里暴露的别名本来就是裸 `res_company`，所以查 joins
// 与渲染 ON 条件都必须用裸名。回归：test/inherits_join_schema_pg_test.go。
func unqualifyAlias(alias string) string {
	if i := strings.LastIndex(alias, "."); i >= 0 {
		return alias[i+1:]
	}
	return alias
}

// addJoinsForTable 把挂在 lhs 上的显式 JOIN 子句追加进 from_clause，并递归处理右表
// 自己的 JOIN。
//
// from_clause/from_params 必须传**指针**：此前是按值传切片，函数内 append 只改到局部
// 的切片头，调用方拿不到任何追加结果——显式 JOIN 因此从来没被渲染进 FROM，而右表又
// 已被 addJoin 塞进 self.tables 照常输出，等于 `FROM a, b` 不带连接条件的笛卡尔积。
//
// :lhs table alias
func (self *TQuery) addJoinsForTable(lhs string, from_clause *[]string, from_params *[]any, emitted map[string]bool) {
	if emitted[lhs] {
		return
	}
	emitted[lhs] = true

	for _, table := range self.joins[lhs] {
		rhs, lhs_col, rhs_col, join := table.String(0), table.String(1), table.String(2), table.String(3)
		*from_clause = append(*from_clause, fmt.Sprintf(` %s %s ON ("%s"."%s" = "%s"."%s"`,
			join, self.alias_mapping[rhs], lhs, lhs_col, rhs, rhs_col))
		extra := self.extras[lhs] //.get((lhs, (table.String(0), lhs_col, rhs_col, join)))
		if extra != nil {
			*from_clause = append(*from_clause, " AND ")
			*from_clause = append(*from_clause, extra.String(0))
			*from_params = append(*from_params, extra.String(1))
		}
		*from_clause = append(*from_clause, ")")
		self.addJoinsForTable(rhs, from_clause, from_params, emitted)
	}
}

// 验证字段并添加关系表到from
// # the query may involve several tables: we need fully-qualified names
func (self *TQuery) qualify(field IField, model IModel) string {
	dialect := self.session.orm.dialect
	fieldName := dialect.Quoter().Quote(field.Name())
	if model != nil {
		res := self.inherits_join_calc(field.Name(), model)
		/*
			if field.Type == "binary" { // && (context.get('bin_size') or context.get('bin_size_' + col)):
				//# PG 9.2 introduces conflicting pg_size_pretty(numeric) -> need ::cast
				res = fmt.Sprintf(`pg_size_pretty(length(%s)::bigint)`, res)
			}*/

		return fmt.Sprintf(`%s as %s`, res, fieldName)
	}

	return fieldName
}

/*
"""

	Adds missing table select and join clause(s) to ``query`` for reaching
	the field coming from an '_inherits' parent table (no duplicates).

	:param alias: name of the initial SQL alias
	:param field: name of inherited field to reach
	:param query: query object on which the JOIN should be added
	:return: qualified name of field, to be used in SELECT clause
	"""
*/
func (self *TQuery) inherits_join_calc(fieldName string, model IModel) (result string) {
	/*
	   # INVARIANT: alias is the SQL alias of model._table in query
	   model = self
	   while field in model._inherit_fields and field not in model._columns:
	       # retrieve the parent model where field is inherited from
	       parent_model_name = model._inherit_fields[field][0]
	       parent_model = self.env[parent_model_name]
	       parent_field = model._inherits[parent_model_name]
	       # JOIN parent_model._table AS parent_alias ON alias.parent_field = parent_alias.id
	       parent_alias, _ = query.add_join(
	           (alias, parent_model._table, parent_field, 'id', parent_field),
	           implicit=True,
	       )
	       model, alias = parent_model, parent_alias
	   # handle the case where the field is translated
	   translate = model._columns[field].translate
	   if translate and not callable(translate):
	       return model.generate_translated_field(alias, field, query)
	   else:
	       return '"%s"."%s"' % (alias, field)
	*/
	alias := model.Table()
	// _inherits 委托字段：本表无此列，需 JOIN 父表(o2o 的 FK)取值。
	// 父表来源优先用 relatedFields，缺失时回退到字段自身 base.modelName，
	// 与写入路径 _separateValues 保持一致（不强依赖 relatedFields 是否填充）。
	if fld := model.GetFieldByName(fieldName); fld != nil && fld.IsInherited() {
		// # retrieve the parent model where field is inherited from
		parent_model_name := fld.ModelName()
		if rel := model.Obj().GetRelatedFieldByName(fieldName); rel != nil && rel.RelatedTableName != "" {
			parent_model_name = rel.RelatedTableName
		}

		//NOTE JOIN parent_model._table AS parent_alias ON alias.parent_field = parent_alias.id
		parent_field := model.Obj().GetRelationByName(parent_model_name)
		parent_model, err := model.Osv().GetModel(parent_model_name) // #i
		if err != nil || parent_field == "" {
			// 解析不到父表/外键则不 JOIN，退回本表限定（由调用方保证字段可读），并记录。
			log.Errf("@inherits_join_calc: cannot resolve parent %q (fk=%q) for inherited field %q: %v",
				parent_model_name, parent_field, fieldName, err)
		} else {
			// LEFT JOIN 而非隐式 INNER JOIN：委托继承的外键**允许为空**（父记录被删、
			// 外部导入的历史数据、或建记录时没给任何继承字段——写入侧只在继承字段非空
			// 时才自动建父记录）。用 INNER JOIN 的话这些行会被连接直接过滤掉，记录明明
			// 在表里、Read 却一条都不返回，而且不报任何错。继承字段读成空值才是对的。
			parent_alias, _ := self.addJoin(
				[]string{
					alias, parent_field,
					parent_model.Table(), parent_model.IdField(),
					parent_field},
				false, // 显式 JOIN：由 addJoinsForTable 渲染成 JOIN ... ON (...)
				true,  // outer → LEFT JOIN
				nil,
				nil)
			model, alias = parent_model, parent_alias
		}
	}
	//# handle the case where the field is translated
	field := model.GetFieldByName(fieldName)
	dialect := self.session.orm.dialect
	fieldName = dialect.Quoter().Quote(fieldName)
	if field != nil && field.Translate() { //  if translate and not callable(translate):
		// return model.generate_translated_field(alias, field, query)
		return fmt.Sprintf(`"%s".%s`, alias, fieldName)
	}

	return fmt.Sprintf(`"%s".%s`, alias, fieldName)
}

// 获得表别名枚举
func (self *TQuery) getAliasMapping() map[string]string {
	mapping := make(map[string]string)
	var statement string
	for _, table := range self.tables {
		_, statement = get_alias_from_query(table)
		mapping[statement] = table
	}
	return mapping
}
