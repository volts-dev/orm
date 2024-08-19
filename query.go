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
		// holds the list of tables joined using default JOIN.
		// the table names are stored double-quoted (backwards compatibility)
		tables              []string
		where_clause        []string
		where_clause_params []interface{}
		joins               map[string][]*utils.TStringList
		extras              map[string]*utils.TStringList
		alias_mapping       map[string]string
	}
)

func NewQuery(tables []string, where_clause []string, params []interface{}, joins map[string][]*utils.TStringList, extras map[string]*utils.TStringList) (q *TQuery) {
	q = &TQuery{}

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
func (self *TQuery) getSql() (fromClause, whereClause string, whereClauseParams []interface{}) {
	self.alias_mapping = self.getAliasMapping()

	var table_alias string
	var has bool
	tables_to_process := self.tables
	from_clause := make([]string, 0)
	from_params := make([]interface{}, 0)
	for pos, table := range tables_to_process {
		if pos > 0 {
			from_clause = append(from_clause, ",")
		}

		from_clause = append(from_clause, table)
		_, table_alias = get_alias_from_query(table)
		if _, has = self.joins[table_alias]; has {
			self.addJoinsForTable(table_alias, tables_to_process, from_clause, from_params)
		}
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
// 添加目标表到当前表
func (self *TQuery) addJoin(connection []string, implicit bool, outer bool, extra, extra_params map[string]interface{}) (string, string) {
	// (lhs.lhs_col = table.col)
	lhs := connection[0]     // mdoel name
	lhs_col := connection[1] // field
	table := connection[2]   // relate model name
	col := connection[3]     // realte field

	link := connection[4]
	alias, alias_statement := generate_table_alias(lhs, [][]string{{table, link}})

	if implicit {
		if utils.IndexOf(alias_statement, self.tables...) == -1 {
			self.tables = append(self.tables, alias_statement)
			condition := fmt.Sprintf(`("%s"."%s" = "%s"."%s")`, lhs, lhs_col, alias, col)
			self.where_clause = append(self.where_clause, condition)
		} else {
			//# already joined
			// pass
		}

		return alias, alias_statement
	} else {
		//aliases := self._get_table_aliases()
		// assert lhs in aliases, "Left-hand-side table %s must already be part of the query tables %s!" % (lhs, str(self.tables))
		if utils.IndexOf(alias_statement, self.tables...) != -1 {
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
			join := self.joins[lhs]
			if join == nil {
				join = make([]*utils.TStringList, 0) // NewStringList()
				self.joins[lhs] = join
			}
			join = append(join, join_tuple)

			if extra != nil {
				//extra = extra.format(lhs=lhs, rhs=alias)
				//self.extras[(lhs, join_tuple)] = (extra, extra_params)
			}
		}
		return alias, alias_statement
	}
}

// :lhs table name
func (self *TQuery) addJoinsForTable(lhs string, tables_to_process, from_clause []string, from_params []interface{}) {
	if tablelst, has := self.joins[lhs]; has {
		for _, table := range tablelst {
			rhs, lhs_col, rhs_col, join := table.String(0), table.String(1), table.String(2), table.String(3)
			utils.SliceDelete(tables_to_process, self.alias_mapping[table.String(0)]) //     tables_to_process.remove()
			from_clause = append(from_clause, fmt.Sprintf(` %s %s ON ("%s"."%s" = "%s"."%s"`,
				join, self.alias_mapping[rhs], lhs, lhs_col, rhs, rhs_col))
			extra := self.extras[lhs] //.get((lhs, (table.String(0), lhs_col, rhs_col, join)))
			if extra != nil {
				from_clause = append(from_clause, " AND ")
				from_clause = append(from_clause, extra.String(0))
				from_params = append(from_params, extra.String(1))
			}
			from_clause = append(from_clause, ")")
			self.addJoinsForTable(rhs, tables_to_process, from_clause, from_params)
		}
	}
}

// 验证字段并添加关系表到from
// # the query may involve several tables: we need fully-qualified names
func (self *TQuery) qualify(field IField, model IModel) string {
	res := self.inherits_join_calc(field.Name(), model)
	/*
		if field.Type == "binary" { // && (context.get('bin_size') or context.get('bin_size_' + col)):
			//# PG 9.2 introduces conflicting pg_size_pretty(numeric) -> need ::cast
			res = fmt.Sprintf(`pg_size_pretty(length(%s)::bigint)`, res)
		}*/
	return fmt.Sprintf(`%s as "%s"`, res, field.Name())
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
	if rel := model.Obj().GetRelatedFieldByName(fieldName); rel != nil {
		//for name, _ := range self._relate_fields {
		if fld := model.GetFieldByName(fieldName); fld != nil && fld.IsInheritedField() {
			// # retrieve the parent model where field is inherited from
			parent_model_name := rel.RelateTableName
			parent_model, err := model.Osv().GetModel(parent_model_name) // #i
			if err != nil {
				log.Err(err, "@inherits_join_calc")
			}

			//NOTE JOIN parent_model._table AS parent_alias ON alias.parent_field = parent_alias.id
			parent_field := model.Obj().GetRelationByName(parent_model_name)
			parent_alias, _ := self.addJoin(
				[]string{
					alias, parent_field,
					parent_model.Table(), parent_model.IdField(),
					parent_field},
				true,
				false,
				nil,
				nil)
			model, alias = parent_model, parent_alias

		} else {
			//log.Dbg("inherits_join_calc:", field, alias, fld)
		}
	}
	//# handle the case where the field is translated
	field := model.GetFieldByName(fieldName)
	if field != nil && field.Translatable() { //  if translate and not callable(translate):
		// return model.generate_translated_field(alias, field, query)
		return fmt.Sprintf(`"%s"."%s"`, alias, fieldName)
	}

	return fmt.Sprintf(`"%s"."%s"`, alias, fieldName)
}

func (self *TQuery) _get_table_aliases() (aliases []string) {
	//from openerp.osv.expression import get_alias_from_query
	aliases = make([]string, 0)
	for _, from_statement := range self.tables {
		_, alias := get_alias_from_query(from_statement)
		aliases = append(aliases, alias)
	}
	return aliases // [get_alias_from_query(from_statement)[1] for from_statement in self.tables]

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
