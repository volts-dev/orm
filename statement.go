package orm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/orm/logger"
	"github.com/volts-dev/utils"
)

type (
	// TODO 添加错误信息使整个statement 无法执行错误不合法查询
	TStatement struct {
		session        *TSession
		model          *TModel
		domain         *domain.TDomainNode // 查询条件
		Params         []interface{}       // 储存有序值
		IdKey          string              // 开发者决定的数据表主键
		IdParam        []interface{}
		Sets           map[string]interface{} // 预存数据值 供更新
		Fields         map[string]bool        // show which fields will be list out
		NullableFields map[string]bool
		//TableNameClause    string
		//AltTableNameClause string // 当无Objet实例时使用字符串表名称
		AliasTableName string
		JoinClause     string
		FromClause     string
		OmitClause     string
		GroupBySClause string
		OrderByClause  string
		LimitClause    int64
		OffsetClause   int64
		IsCount        bool
		IsForUpdate    bool
		UseCascade     bool
		Charset        string //???
		StoreEngine    string //???
	}
)

// Init reset all the statment's fields
func (self *TStatement) Init() {
	self.domain = domain.NewDomainNode()
	self.Sets = nil // 不预先创建添加GC负担
	self.IdParam = make([]interface{}, 0)
	self.Fields = make(map[string]bool)         // TODO 优化
	self.NullableFields = make(map[string]bool) // TODO 优化
	self.FromClause = ""
	self.OrderByClause = ""
	self.LimitClause = 0
	self.OffsetClause = 0
	self.IsCount = false
	self.Params = make([]interface{}, 0)
}

// Id generate "where id = ? " statment or for composite key "where key1 = ? and key2 = ?"
func (self *TStatement) Ids(ids ...interface{}) *TStatement {
	self.IdParam = append(self.IdParam, ids...)
	return self
}

func (self *TStatement) Select(fields ...string) *TStatement {
	for idx, name := range fields {
		name = fmtModelName(name) //# 支持输入结构字段名称
		if idx == 0 && (name == "*" || name == "'*'" || name == `"*"`) {
			self.Fields = nil
			return self
		}

		// 安全代码应该由开发者自己检查
		if field := self.session.Statement.model.obj.GetFieldByName(name); field != nil {
			self.Fields[name] = true
		}
	}

	return self
}

// Where add Where statment
func (self *TStatement) Where(query string, args ...interface{}) *TStatement {
	if !strings.Contains(query, self.session.orm.dialect.EqStr()) {
		query = strings.Replace(query, "=", self.session.orm.dialect.EqStr(), -1)
	}

	self.Op(domain.AND_OPERATOR, query, args...)

	return self
}

func (self *TStatement) Domain(dom interface{}, args ...interface{}) *TStatement {
	self.Op(domain.AND_OPERATOR, dom, args...)

	return self
}

func (self *TStatement) Op(op string, cond interface{}, args ...interface{}) {
	var new_cond *domain.TDomainNode
	var err error
	switch cond.(type) {
	case string:
		// 添加信的条件
		new_cond, err = domain.String2Domain(cond.(string))
		if err != nil {
			logger.Err(err)
		}
	case *domain.TDomainNode:
		new_cond = cond.(*domain.TDomainNode)
	default:
		logger.Errf("op not support this query %v", cond)
	}

	self.domain.OP(op, new_cond)
	if args != nil {
		self.Params = append(self.Params, args...)
	}
}

// And add Where & and statment
func (self *TStatement) And(query string, args ...interface{}) *TStatement {
	self.Op(domain.AND_OPERATOR, query, args...)
	return self
}

// Or add Where & Or statment
func (self *TStatement) Or(query string, args ...interface{}) *TStatement {
	self.Op(domain.OR_OPERATOR, query, args...)
	return self
}

// In generate "Where column IN (?) " statement
func (self *TStatement) In(field string, args ...interface{}) *TStatement {
	if len(args) == 0 {
		// TODO report err stack
		return self
	}

	keys := domain.NewDomainNode(args...)
	new_cond := domain.NewDomainNode()
	new_cond.Push(field)
	new_cond.Push("IN")
	new_cond.Push(keys)
	self.Op(domain.AND_OPERATOR, new_cond)
	return self
}

func (self *TStatement) NotIn(field string, args ...interface{}) *TStatement {
	keys := domain.NewDomainNode(args...)
	new_cond := domain.NewDomainNode()
	new_cond.Push(field)
	new_cond.Push("NOT IN")
	new_cond.Push(keys)
	self.Op(domain.AND_OPERATOR, new_cond)
	return self
}

func (self *TStatement) Set(fieldName string, value interface{}) *TStatement {
	if self.Sets == nil {
		self.Sets = make(map[string]interface{})
	}
	self.Sets[fieldName] = value
	return self
}

func (self *TStatement) Join(joinOperator string, tablename interface{}, condition string, args ...interface{}) {

}

// GroupBy
func (self *TStatement) GroupBy(keys string) *TStatement {
	self.GroupBySClause = keys
	return self
}

// OrderBy generate "Order By order" statement
func (self *TStatement) OrderBy(order string) *TStatement {
	if len(self.OrderByClause) > 0 {
		self.OrderByClause += ", "
	}
	self.OrderByClause += order
	return self
}

// Desc generate `ORDER BY xx DESC`
func (self *TStatement) Desc(fileds ...string) *TStatement {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, self.OrderByClause)
	if len(self.OrderByClause) > 0 {
		fmt.Fprint(&buf, ", ")
	}
	//newColNames := statement.col2NewColsWithQuote(colNames...)
	fmt.Fprintf(&buf, "%v DESC", strings.Join(fileds, " DESC, "))
	self.OrderByClause = buf.String()
	return self
}

// Omit do not use the columns
func (self *TStatement) Omit(fields ...string) {
	for _, field := range fields {
		self.Fields[strings.ToLower(field)] = false
	}
	self.OmitClause = self.session.orm.dialect.Quote(strings.Join(fields, self.session.orm.dialect.Quote(", ")))
}

// Limit generate LIMIT start, limit statement
func (self *TStatement) Limit(limit int64, offset ...int64) *TStatement {
	self.LimitClause = limit
	if len(offset) > 0 {
		self.OffsetClause = offset[0]
	}
	return self
}

func (self *TStatement) generate_create_table() string {
	return self.session.orm.dialect.CreateTableSql(self.session.Statement.model, self.StoreEngine, self.Charset)
}

func (self *TStatement) generate_sum(columns ...string) (string, []interface{}, error) {
	/*	var sumStrs = make([]string, 0, len(columns))
		for _, colName := range columns {
			if !strings.Contains(colName, " ") && !strings.Contains(colName, "(") {
				colName = self.session.Orm().Quote(colName)
			}
			sumStrs = append(sumStrs, fmt.Sprintf("COALESCE(sum(%s),0)", colName))
		}
		sumSelect := strings.Join(sumStrs, ", ")

		condSQL, condArgs, err := statement.genConds(bean)
		if err != nil {
			return "", nil, err
		}

		sqlStr, err := statement.genSelectSQL(sumSelect, condSQL)
		if err != nil {
			return "", nil, err
		}

		return sqlStr, append(statement.joinArgs, condArgs...), nil
	*/
	return "", nil, nil
}

func (self *TStatement) generate_unique() []string {
	var sqls []string = make([]string, 0)
	for _, index := range self.session.Statement.model.obj.indexes {
		if index.Type == UniqueType {
			sql := self.session.orm.dialect.CreateIndexSql(self.session.Statement.model.GetName(), index)
			sqls = append(sqls, sql)
		}
	}
	return sqls
}

func (self *TStatement) generate_add_column(col IField) (string, []interface{}) {
	quote := self.session.orm.dialect.Quote
	sql := fmt.Sprintf("ALTER TABLE %v ADD %v;", quote(self.session.Statement.model.GetName()), col.String(self.session.orm.dialect))
	return sql, []interface{}{}
}

func (self *TStatement) generate_index() []string {
	var sqls []string = make([]string, 0)
	quote := self.session.orm.dialect.Quote

	for idxName, index := range self.session.Statement.model.obj.indexes {
		lIdxName := fmt.Sprintf("IDX_%v_%v", self.session.Statement.model.GetName(), idxName)
		if index.Type == IndexType {
			sql := fmt.Sprintf("CREATE INDEX %v ON %v (%v);", quote(lIdxName),
				quote(self.session.Statement.model.GetName()), quote(strings.Join(index.Cols, quote(","))))
			sqls = append(sqls, sql)
		}
	}
	return sqls
}

func (self *TStatement) generate_insert(fields []string) (query string, isQuery bool) {
	id_field := self.model.idField
	sqlStr, isQuery := self.session.orm.dialect.GenInsertSql(self.model.GetName(), fields, id_field)
	return sqlStr, isQuery
}

// Auto generating conditions according a struct
func (self *TStatement) generate_query(vals map[string]interface{}, includeVersion bool, includeUpdated bool, includeNil bool,
	includeAutoIncr bool, allUseBool bool, useAllCols bool, unscoped bool, mustColumnMap map[string]bool) (res_clause string, res_params []interface{}) {
	//res_domain = utils.NewStringList()
	lClauses := make([]string, 0)
	res_params = make([]interface{}, 0)

	var (
		//		field                *TField
		col IField
		//left, oprator, right string

		lIsRequiredField bool
		lFieldType       reflect.Type
		lFieldVal        reflect.Value
	)

	for name, val := range vals {

		//field = self.session.model.FieldByName(name)
		col = self.model.GetFieldByName(name) // field.column
		if col == nil {
			continue
		}

		if !includeVersion && col.IsVersion() {
			continue
		}

		if !includeUpdated && col.IsUpdated() {
			continue
		}

		if !includeAutoIncr && col.IsAutoIncrement() {
			continue
		}

		if self.session.orm.dialect.DBType() == MSSQL && col.SQLType().Name == Text {
			continue
		}
		if col.SQLType().IsJson() {
			continue
		}

		if val == nil {
			continue
		}

		lFieldType = reflect.TypeOf(val)
		lFieldVal = reflect.ValueOf(val)
		lIsRequiredField = useAllCols
		// 强制过滤已经设定的字段是否作为Query使用
		if b, ok := mustColumnMap[strings.ToLower(col.Name())]; ok {
			if b {
				lIsRequiredField = true
			} else {
				continue
			}
		}

		// 处理指针结构
		if lFieldType.Kind() == reflect.Ptr {
			if val == nil {
				if includeNil {
					//args = append(args, nil)
					//colNames = append(colNames, fmt.Sprintf("%v %s ?", colName, engine.dialect.EqStr()))
					lClauses = append(lClauses, fmt.Sprintf("%v %s ?", name, self.session.orm.dialect.EqStr()))
					//res_domain.AddSubList(name, self.session.orm.dialect.EqStr(), "?")
					res_params = append(res_params, nil)
				}
				continue

			} else {
				// dereference ptr type to instance type
				lFieldVal = lFieldVal.Elem()
				lFieldType = reflect.TypeOf(lFieldVal.Interface())
				lIsRequiredField = true
			}
		}

		switch lFieldType.Kind() {
		case reflect.Bool:
			if !allUseBool || !lIsRequiredField {
				// if a bool in a struct, it will not be as a condition because it default is false,
				// please use Where() instead
				continue
			}
		case reflect.String:
			/*if !requiredField && fieldValue.String() == "" {
				continue
			}
			// for MyString, should convert to string or panic
			if fieldType.String() != reflect.String.String() {
				val = fieldValue.String()
			} else {
				val = fieldValue.Interface()
			}*/
		case reflect.Int8, reflect.Int16, reflect.Int, reflect.Int32, reflect.Int64:
			/*if !requiredField && fieldValue.Int() == 0 {
				continue
			}
			val = fieldValue.Interface()*/
		case reflect.Float32, reflect.Float64:
			/*if !requiredField && fieldValue.Float() == 0.0 {
				continue
			}
			val = fieldValue.Interface()*/
		case reflect.Uint8, reflect.Uint16, reflect.Uint, reflect.Uint32, reflect.Uint64:
			/*if !requiredField && fieldValue.Uint() == 0 {
				continue
			}
			t := int64(fieldValue.Uint())
			val = reflect.ValueOf(&t).Interface()*/
		case reflect.Struct:
			if lFieldType.ConvertibleTo(TimeType) {
				t := lFieldVal.Convert(TimeType).Interface().(time.Time)
				if !lIsRequiredField && (t.IsZero() || !lFieldVal.IsValid()) {
					continue
				}
				val = self.session.orm.FormatTime(col.SQLType().Name, t)
			} else if _, ok := reflect.New(lFieldType).Interface().(Conversion); ok {
				continue

				/*} else if valNul, ok := fieldValue.Interface().(driver.Valuer); ok {
				val, _ = valNul.Value()
				if val == nil {
					continue
				}*/
			} else {
				if col.SQLType().IsJson() {
					if col.SQLType().IsText() {
						bytes, err := json.Marshal(val)
						if err != nil {
							logger.Err("adas", err)
							continue
						}
						val = string(bytes)
					} else if col.SQLType().IsBlob() {
						var bytes []byte
						var err error
						bytes, err = json.Marshal(val)
						if err != nil {
							logger.Errf("asdf", err)
							continue
						}
						val = bytes
					}
				} else {
					// any other
				}
			}
		case reflect.Array, reflect.Slice, reflect.Map:
			if lFieldVal == reflect.Zero(lFieldType) {
				continue
			}
			if lFieldVal.IsNil() || !lFieldVal.IsValid() || lFieldVal.Len() == 0 {
				continue
			}

			if col.SQLType().IsText() {
				bytes, err := json.Marshal(lFieldVal.Interface())
				if err != nil {
					logger.Errf("generate_query:", err)
					continue
				}
				val = string(bytes)
			} else if col.SQLType().IsBlob() {
				var bytes []byte
				var err error
				if (lFieldType.Kind() == reflect.Array || lFieldType.Kind() == reflect.Slice) &&
					lFieldType.Elem().Kind() == reflect.Uint8 {
					if lFieldVal.Len() > 0 {
						val = lFieldVal.Bytes()
					} else {
						continue
					}
				} else {
					bytes, err = json.Marshal(lFieldVal.Interface())
					if err != nil {
						logger.Err("1", err)
						continue
					}
					val = bytes
				}
			} else {
				continue
			}
		default:
			//val = lFieldVal.Interface()
		}

		var Clause string
		if col.IsPrimaryKey() && self.session.orm.dialect.DBType() == "ql" {
			//condi = "id() == ?"
			Clause = "id() == ?"
			//left = "id()"
			//oprator = "="
			//right = "?"

		} else {
			//condi = fmt.Sprintf("%v %s ?", colName, self.session.orm.dialect.EqStr())
			Clause = fmt.Sprintf("%v %s ?", name, self.session.orm.dialect.EqStr())
			//left = name
			//oprator = "="
			//right = "?"
		}
		lClauses = append(lClauses, Clause)
		//res_domain.AddSubList(right, oprator, left)
		res_params = append(res_params, val)
	}

	res_clause = strings.Join(lClauses, " "+self.session.orm.dialect.AndStr()+" ")
	return
}

/*Computes the WHERE clause needed to implement an OpenERP domain.
  :param domain: the domain to compute
  :type domain: list
  :param active_test: whether the default filtering of records with ``active``
                      field set to ``False`` should be applied.
  :return: the query expressing the given domain as provided in domain
  :rtype: osv.query.Query
*/
func (self *TStatement) where_calc(node *domain.TDomainNode, active_test bool, context map[string]interface{}) (*TQuery, error) {
	if context == nil {
		context = make(map[string]interface{})
	}

	// domain = domain[:]
	// if the object has a field named 'active', filter out all inactive
	// records unless they were explicitely asked for
	if has := self.session.Statement.model.obj.GetFieldByName("active"); has != nil && active_test {
		if node != nil {
			// the item[0] trick below works for domain items and '&'/'|'/'!'
			// operators too
			var hasfield bool
			for _, node := range node.Nodes() {
				if node.String(0) == "active" {
					hasfield = true
				}
			}
			if !hasfield {
				//domain.Insert(0, Query2StringList(`('active', '=', 1)`))
				node, err := domain.String2Domain(`[('active', '=', 1)]`)
				if err != nil {
					logger.Err(err)
				}
				node.Insert(0, node)
			}
		} else {
			//domain = Query2StringList(`[('active', '=', 1)]`)
			var err error
			node, err = domain.String2Domain(`[('active', '=', 1)]`)
			if err != nil {
				logger.Err(err)
			}

		}
	}

	tables := make([]string, 0)
	var where_clause []string
	var where_params []interface{}
	if node != nil && node.Count() > 0 {
		exp, err := NewExpression(self.session.orm, self.session.Statement.model, node, context)
		if err != nil {
			return nil, err
		}

		tables = exp.get_tables().Strings()
		where_clause, where_params = exp.to_sql(self.Params...)

	} else {
		where_clause, where_params, tables = nil, nil, append(tables, self.session.Statement.model.GetName())

	}

	return NewQuery(tables, where_clause, where_params, nil, nil), nil //self.Registry.r Query(tables, where_clause, where_params)
}

func (self *TStatement) _check_qorder(word string) (result bool) {
	re, err := regexp.Compile(`^(\s*([a-z0-9:_]+|"[a-z0-9:_]+")(\s+(desc|asc))?\s*(,|$))+$`) //`^(\s*([a-z0-9:_]+|"[a-z0-9:_]+")(\s+(desc|asc))?\s*(,|$))+(?<!,)$`
	if err != nil {
		logger.Err(err)
	}

	//matches := re.FindAllStringSubmatch(word, -1)
	if re.Match([]byte(word)) {
		//  raise UserError(_('Invalid "order" specified. A valid "order" specification is a comma-separated list of valid field names (optionally followed by asc/desc for the direction)'))
		logger.Err(`Invalid "order" specified. A valid "order" specification is a comma-separated list of valid field names (optionally followed by asc/desc for the direction)`)
	}
	return true
}

func (self *TStatement) generate_order_by_inner(alias, order_spec string, query *TQuery, reverse_direction bool, seen []string) []string {
	if seen == nil {
		//初始化
	}
	order_by_elements := make([]string, 0)
	self._check_qorder(order_spec)
	var (
		order_direction string

		//inner_clauses = make([]string, 0)
	)

	for _, order_part := range strings.Split(order_spec, ",") {
		order_split := strings.Split(utils.Trim(order_part), " ")
		order_field := order_split[0]
		if len(order_split) == 2 {
			order_direction = strings.ToUpper(utils.Trim(order_split[1]))
		} else {
			order_direction = ""
		}

		if reverse_direction {
			if order_direction == "DESC" {
				order_direction = "ASC"
			} else {
				order_direction = "DESC"
			}
		}

		//do_reverse := order_direction == "DESC"
		//var inner_clauses []string
		//add_dir := false
		if order_field == self.IdKey {
			lStr := fmt.Sprintf(`"%s"."%s" %s`, alias, order_field, order_direction)
			order_by_elements = append(order_by_elements, lStr)

		} else {
			field := self.session.Statement.model.obj.GetFieldByName(order_field)
			if field == nil {
				//raise ValueError(_("Sorting field %s not found on model %s") % (order_field, self._name))
				logger.Warnf("Sorting field %s not found on model %s", order_field, self.model.GetName())
				continue
			}

			if field.IsRelatedField() {

			}

			if field.Store() && field.Type() == "many2one" {
				// key = (self._name, order_column._obj, order_field)
				// if key not in seen{
				//     seen.add(key)
				//     inner_clauses = self.generate_m2o_order_by(alias, order_field, query, do_reverse, seen)
				//	}
			} else if field.Store() && field.SQLType().Name != "" {
				qualifield_name := query.inherits_join_calc(order_field, self.session.Statement.model)
				if field.Type() == "boolean" {
					qualifield_name = fmt.Sprintf(`COALESCE(%s, false)`, qualifield_name)
				}

				lStr := fmt.Sprintf(`"%s %s"`, qualifield_name, order_direction)
				order_by_elements = append(order_by_elements, lStr)
			} else {
				continue //# ignore non-readable or "non-joinable" fields
			}
		}

		/*
				if order_fld := self.session.model.FieldByName(order_field); order_fld != nil {

					if order_fld.IsClassicRead() { //_classic_read:
						if order_fld.Translatable() { // && not callable(order_column.translate):
							// inner_clauses = []string{self.generate_translated_field(alias, order_field, query)}
						} else {
							inner_clauses = []string{fmt.Sprintf(`"%s"."%s"`, alias, order_field)}
						}
						add_dir = true
					} else if order_fld.Store() && order_fld.Type() == "many2one" {
						// key = (self._name, order_column._obj, order_field)
						// if key not in seen{
						//     seen.add(key)
						//     inner_clauses = self.generate_m2o_order_by(alias, order_field, query, do_reverse, seen)
						//	}
					} else {
						continue //# ignore non-readable or "non-joinable" fields
					}
				} else if rel_fld := self.session.model.RelateFieldByName(order_field); rel_fld != nil {
					parent_obj := self.session.orm.osv.GetModel(rel_fld.RelateTableName) // #i
					order_fld := parent_obj.FieldByName(order_field)
					// parent_obj = self.pool[self._inherit_fields[order_field][3]]
					// order_column = parent_obj._columns[order_field]
					if order_fld.IsClassicRead() { //_classic_read:
						inner_clauses = []string{self.inherits_join_calc(alias, order_field, query)}
						add_dir = true
					} else if order_fld.Type() == "many2one" {
						// key = (parent_obj._name, order_column._obj, order_field)
						// if key not in seen{
						//    seen.add(key)
						//     inner_clauses = self.generate_m2o_order_by(alias, order_field, query, do_reverse, seen)
						//}
					} else {
						continue //# ignore non-readable or "non-joinable" fields
					}
				}
			}

			if order_fld != nil && order_fld.Type() == "boolean" {
				inner_clauses = []string{fmt.Sprintf(`COALESCE(%s, false)`, inner_clauses[0])}
			}
			for _, clause := range inner_clauses {
				if add_dir {
					order_by_elements = append(order_by_elements, fmt.Sprintf(`%s %s`, clause, order_direction))
				} else {
					order_by_elements = append(order_by_elements, clause)
				}
			}*/

	}

	/*       if seen is None:
	             seen = set()
	         order_by_elements = []
	         self._check_qorder(order_spec)
	         for order_part in order_spec.split(','):
	             order_split = order_part.strip().split(' ')
	             order_field = order_split[0].strip()
	             order_direction = order_split[1].strip().upper() if len(order_split) == 2 else ''
	             if reverse_direction:
	                 order_direction = 'ASC' if order_direction == 'DESC' else 'DESC'
	             do_reverse = order_direction == 'DESC'
	             order_column = None
	             inner_clauses = []
	             add_dir = False
	             if order_field == 'id':
	                 order_by_elements.append('"%s"."%s" %s' % (alias, order_field, order_direction))
	             elif order_field in self._columns:
	                 order_column = self._columns[order_field]
	                 if order_column._classic_read:
	                     if order_column.translate and not callable(order_column.translate):
	                         inner_clauses = [self.generate_translated_field(alias, order_field, query)]
	                     else:
	                         inner_clauses = ['"%s"."%s"' % (alias, order_field)]
	                     add_dir = True
	                 elif order_column._type == 'many2one':
	                     key = (self._name, order_column._obj, order_field)
	                     if key not in seen:
	                         seen.add(key)
	                         inner_clauses = self.generate_m2o_order_by(alias, order_field, query, do_reverse, seen)
	                 else:
	                     continue  # ignore non-readable or "non-joinable" fields
	             elif order_field in self._inherit_fields:
	                 parent_obj = self.pool[self._inherit_fields[order_field][3]]
	                 order_column = parent_obj._columns[order_field]
	                 if order_column._classic_read:
	                     inner_clauses = [self.inherits_join_calc(alias, order_field, query)]
	                     add_dir = True
	                 elif order_column._type == 'many2one':
	                     key = (parent_obj._name, order_column._obj, order_field)
	                     if key not in seen:
	                         seen.add(key)
	                         inner_clauses = self.generate_m2o_order_by(alias, order_field, query, do_reverse, seen)
	                 else:
	                     continue  # ignore non-readable or "non-joinable" fields
	             else:
	                 raise ValueError(_("Sorting field %s not found on model %s") % (order_field, self._name))
	             if order_column and order_column._type == 'boolean':
	                 inner_clauses = ["COALESCE(%s, false)" % inner_clauses[0]]

	             for clause in inner_clauses:
	                 if add_dir:
	                     order_by_elements.append("%s %s" % (clause, order_direction))
	                 else:
	                     order_by_elements.append(clause)
	         return order_by_elements
	*/

	return order_by_elements
}

/** TODO 未完成    Attempt to construct an appropriate ORDER BY clause based on order_spec, which must be
*      a comma-separated list of valid field names, optionally followed by an ASC or DESC direction.
*
*        :raise ValueError in case order_spec is malformed
 */
func (self *TStatement) generate_order_by(query *TQuery, context map[string]interface{}) (result string) {
	order_by_clause := ""

	if self.OrderByClause != "" {
		order_by_elements := self.generate_order_by_inner(self.session.Statement.model.GetName(), self.OrderByClause, query, false, nil)
		if len(order_by_elements) > 0 {
			order_by_clause = strings.Join(order_by_elements, ",")
		}
	}

	if order_by_clause != "" {
		return fmt.Sprintf(` ORDER BY %s `, order_by_clause)
	}

	return
}

func (self *TStatement) generate_fields() []string {
	table := self.model
	var fields []string
	for _, col := range table.GetFields() {
		if self.OmitClause != "" {
			if _, ok := self.Fields[strings.ToLower(col.Name())]; ok {
				continue
			}
		}

		if col.Base().MapType == ONLYTODB {
			continue
		}

		if self.JoinClause != "" {
			var name string
			if self.AliasTableName != "" {
				name = self.session.orm.dialect.Quote(self.AliasTableName)
			} else {
				name = self.session.orm.dialect.Quote(self.model.GetName())
			}
			name += "." + self.session.orm.dialect.Quote(col.Name())
			if col.IsPrimaryKey() && self.session.orm.dialect.DBType() == "ql" {
				fields = append(fields, "id() AS "+name)
			} else {
				fields = append(fields, name)
			}
		} else {
			name := self.session.orm.dialect.Quote(col.Name())
			if col.IsPrimaryKey() && self.session.orm.dialect.DBType() == "ql" {
				fields = append(fields, "id() AS "+name)
			} else {
				fields = append(fields, name)
			}
		}
	}
	return fields
}
