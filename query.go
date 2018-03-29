package orm

import (
	"fmt"
	"strings"
	//"vectors/utils"
	"vectors/utils"
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
		tables []string

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
func (self *TQuery) get_sql() (from_clause, where_clause string, where_clause_params []interface{}) {
	tables_to_process := self.tables
	self.alias_mapping = self._get_alias_mapping()
	lFromClause := make([]string, 0)
	lFromParams := make([]interface{}, 0)

	for pos, table := range tables_to_process {
		if pos > 0 {
			lFromClause = append(lFromClause, ",")
		}
		lFromClause = append(lFromClause, table)
		_, table_alias := get_alias_from_query(table)
		if _, has := self.joins[table_alias]; has {
			self.add_joins_for_table(table_alias, tables_to_process, lFromClause, lFromParams)
		}
	}

	from_clause = strings.Join(lFromClause, "")             // 上面已经添加","
	where_clause = strings.Join(self.where_clause, " AND ") // to string
	where_clause_params = append(lFromParams, self.where_clause_params...)
	return //"".join(from_clause), " AND ".join(self.where_clause), from_params + self.where_clause_params
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
func (self *TQuery) add_join(connection []string, implicit bool, outer bool, extra, extra_params map[string]interface{}) (string, string) {
	lhs := connection[0]
	table := connection[1]
	lhs_col := connection[2]
	col := connection[3]
	link := connection[4]
	alias, alias_statement := generate_table_alias(lhs, [][]string{[]string{table, link}})

	if implicit {
		/*	       if alias_statement not in self.tables:
			           self.tables.append(alias_statement)
			           condition = '("%s"."%s" = "%s"."%s")' % (lhs, lhs_col, alias, col)
			           self.where_clause.append(condition)
			       else:
			           # already joined
			           pass
			       return alias, alias_statement
		*/
		if utils.InStrings(alias_statement, self.tables...) == -1 {
			self.tables = append(self.tables, alias_statement)
			condition := fmt.Sprintf(`("%s"."%s" = "%s"."%s")`, lhs, lhs_col, alias, col)
			self.where_clause = append(self.where_clause, condition)
		} else {
			//# already joined
			// pass
		}
		return alias, alias_statement
	} else {
		/*
		   aliases = self._get_table_aliases()
		   	       assert lhs in aliases, "Left-hand-side table %s must already be part of the query tables %s!" % (lhs, str(self.tables))
		   	       if alias_statement in self.tables:
		   	           # already joined, must ignore (promotion to outer and multiple joins not supported yet)
		   	           pass
		   	       else:
		   	           # add JOIN
		   	           self.tables.append(alias_statement)
		   	           join_tuple = (alias, lhs_col, col, outer and 'LEFT JOIN' or 'JOIN')
		   	           self.joins.setdefault(lhs, []).append(join_tuple)
		   	           if extra:
		   	               extra = extra.format(lhs=lhs, rhs=alias)
		   	               self.extras[(lhs, join_tuple)] = (extra, extra_params)
		   	       return alias, alias_statement
		*/
		//aliases := self._get_table_aliases()
		// assert lhs in aliases, "Left-hand-side table %s must already be part of the query tables %s!" % (lhs, str(self.tables))
		if utils.InStrings(alias_statement, self.tables...) != -1 {
			//# already joined, must ignore (promotion to outer and multiple joins not supported yet)
		} else {
			//# add JOIN
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
//
func (self *TQuery) add_joins_for_table(lhs string, tables_to_process, from_clause []string, from_params []interface{}) {
	if tablelst, has := self.joins[lhs]; has {
		for _, table := range tablelst {
			rhs, lhs_col, rhs_col, join := table.String(0), table.String(1), table.String(2), table.String(3)
			utils.StringsDel(tables_to_process, self.alias_mapping[table.String(0)]) //     tables_to_process.remove()
			from_clause = append(from_clause, fmt.Sprintf(` %s %s ON ("%s"."%s" = "%s"."%s"`,
				join, self.alias_mapping[rhs], lhs, lhs_col, rhs, rhs_col))
			extra := self.extras[lhs] //.get((lhs, (table.String(0), lhs_col, rhs_col, join)))
			if extra != nil {
				from_clause = append(from_clause, " AND ")
				from_clause = append(from_clause, extra.String(0))
				from_params = append(from_params, extra.String(1))
				//logger.Dbg("add_joins_for_table", extra.String(0), extra.String(1))
			}
			from_clause = append(from_clause, ")")
			self.add_joins_for_table(rhs, tables_to_process, from_clause, from_params)
		}
	}
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
func (self *TQuery) _get_alias_mapping() (mapping map[string]string) {
	mapping = make(map[string]string)
	for _, table := range self.tables {
		_, statement := get_alias_from_query(table)
		mapping[statement] = table
	}
	return
}
