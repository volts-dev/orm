package orm

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

type (
	// TODO 添加错误信息使整个statement 无法执行错误不合法查询
	TStatement struct {
		session        *TSession
		Model          IModel              //*TModel
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
		GroupByClause  []string
		FuncsClause    []string // SQL函数
		SortClauses    []string
		AscFields      []string
		DescFields     []string
		OrderByClause  string
		LimitClause    int64
		OffsetClause   int64
		IsCount        bool
		IsForUpdate    bool
		UseCascade     bool
		OnConflict     *OnConflict
		Charset        string //???
		StoreEngine    string //???
	}
)

// Init reset all the statment's fields
func (self *TStatement) Init() {
	self.domain = domain.NewDomainNode()
	self.IdParam = make([]interface{}, 0)
	self.Fields = make(map[string]bool)         // TODO 优化
	self.NullableFields = make(map[string]bool) // TODO 优化
	self.FromClause = ""
	self.OrderByClause = ""
	self.AscFields = nil
	self.DescFields = nil
	self.LimitClause = 0
	self.OffsetClause = 0
	self.IsCount = false
	self.Params = make([]interface{}, 0)
	self.Sets = nil // 不预先创建添加GC负担

	/* 复制session */
	for _, f := range self.session.Sets {
		if f.Queryable {
			self.Where(f.Name+"=?", f.Value)
		}

		self.Set(f.Name, f.Value)
	}
}

// Id generate "where id = ? " statment or for composite key "where key1 = ? and key2 = ?"
func (self *TStatement) Ids(ids ...interface{}) *TStatement {
	self.IdParam = append(self.IdParam, ids...)
	return self
}

func (self *TStatement) Select(fields ...string) *TStatement {
	obj := self.Model.Obj()
	for idx, name := range fields {
		name = fmtFieldName(name) //# 支持输入结构字段名称
		if idx == 0 && (name == "*" || name == "'*'" || name == `"*"`) {
			self.Fields = nil
			return self
		}

		// 安全代码应该由开发者自己检查
		if field := obj.GetFieldByName(name); field != nil {
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

	return self.Op(domain.AND_OPERATOR, query, args...)
}

func (self *TStatement) Domain(dom interface{}, args ...interface{}) *TStatement {
	return self.Op(domain.AND_OPERATOR, dom, args...)
}

func (self *TStatement) Op(op string, cond interface{}, args ...interface{}) *TStatement {
	var new_cond *domain.TDomainNode
	var err error
	switch v := cond.(type) {
	case string:
		// 添加信的条件
		new_cond, err = domain.String2Domain(v, nil)
		if err != nil {
			log.Err(err)
		}
	case *domain.TDomainNode:
		new_cond = v
	default:
		log.Errf("op not support this query %v", v)
	}

	self.domain.OP(op, new_cond)
	if args != nil {
		self.Params = append(self.Params, args...)
	}

	return self
}

// And add Where & and statment
func (self *TStatement) And(query string, args ...interface{}) *TStatement {
	return self.Op(domain.AND_OPERATOR, query, args...)
}

// Or add Where & Or statment
func (self *TStatement) Or(query string, args ...interface{}) *TStatement {
	return self.Op(domain.OR_OPERATOR, query, args...)
}

// In generate "Where column IN (?) " statement
func (self *TStatement) In(field string, args ...interface{}) *TStatement {
	if len(args) == 0 {
		// FIXME IN Condition must pass at least one arguments
		// TODO report err stack
		log.Errf("IN Condition must pass at least one arguments")
		return self
	}

	if self.domain == nil {
		self.domain = domain.NewDomainNode()
	}

	self.domain.IN(field, args...)
	//cond := domain.New(field, "IN", args...)
	//self.Op(domain.AND_OPERATOR, cond)
	return self
}

func (self *TStatement) NotIn(field string, args ...interface{}) *TStatement {
	if len(args) == 0 {
		// FIXME IN Condition must pass at least one arguments
		// TODO report err stack
		log.Errf("NotIn Condition must pass at least one arguments")
		return self
	}

	if self.domain == nil {
		self.domain = domain.NewDomainNode()
	}

	self.domain.NotIn(field, args...)

	//cond := domain.New(field, "NOT IN", args...)
	//self.Op(domain.AND_OPERATOR, cond)
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
func (self *TStatement) GroupBy(fields ...string) *TStatement {
	self.GroupByClause = fields
	return self
}

// OrderBy generate "Order By order" statement
// order string like "id desc,address asc"
func (self *TStatement) OrderBy(clause string) *TStatement {
	if len(self.OrderByClause) > 0 {
		self.OrderByClause += ", "
	}
	self.OrderByClause += clause
	return self
}

// SQL 函数
func (self *TStatement) Funcs(clauses ...string) *TStatement {
	self.FuncsClause = clauses
	return self
}

func (self *TStatement) Sort(clauses ...string) *TStatement {
	self.SortClauses = clauses
	return self
}

// Desc generate `ORDER BY xx DESC`
func (self *TStatement) Desc(fileds ...string) *TStatement {
	self.DescFields = append(self.DescFields, fileds...)
	return self
}

func (self *TStatement) Asc(fileds ...string) *TStatement {
	self.AscFields = append(self.AscFields, fileds...)
	return self
}

// Omit do not use the columns
func (self *TStatement) Omit(fields ...string) {
	for _, field := range fields {
		self.Fields[strings.ToLower(field)] = false
	}
	quoter := self.session.orm.dialect.Quoter()
	self.OmitClause = quoter.Quote(strings.Join(fields, quoter.Quote(", ")))
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
	return self.session.orm.dialect.CreateTableSql(self.Model, self.StoreEngine, self.Charset)
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
	for _, index := range self.Model.Obj().indexes {
		if index.Type == UniqueType {
			sql := self.session.orm.dialect.CreateIndexUniqueSql(self.Model.Table(), index)
			sqls = append(sqls, sql)
		}
	}
	return sqls
}

func (self *TStatement) generate_add_column(field IField) (string, []interface{}) {
	sql := self.session.orm.dialect.GenAddColumnSQL(self.Model.Table(), field)
	return sql, []interface{}{}
}

func (self *TStatement) generate_index() ([]string, error) {
	var sqls []string = make([]string, 0)
	tableName := fmtTableName(self.Model.String())

	for idxName, index := range self.Model.Obj().indexes {
		if index.Type == IndexType {
			exist, err := self.session.IsIndexExist(tableName, idxName, false)
			if err != nil {
				return nil, err
			}

			if exist {
				continue
			}

			sql := self.session.orm.dialect.CreateIndexUniqueSql(tableName, index)
			sqls = append(sqls, sql)
		}
	}

	return sqls, nil
}

func (self *TStatement) generate_insert(fields, uniqueFields []string) (string, bool) {
	return self.session.orm.dialect.GenInsertSql(self.Model.Table(), fields, uniqueFields, self.Model.IdField(), self.OnConflict), true
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
		col = self.Model.GetFieldByName(name)
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
			if lFieldType.Kind() == reflect.Ptr {
				if includeNil {
					//args = append(args, nil)
					//colNames = append(colNames, fmt.Sprintf("%v %s ?", colName, engine.dialect.EqStr()))
					lClauses = append(lClauses, fmt.Sprintf("%v %s ?", name, self.session.orm.dialect.EqStr()))
					//res_domain.AddSubList(name, self.session.orm.dialect.EqStr(), "?")
					res_params = append(res_params, nil)
				}
			}
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
			// dereference ptr type to instance type
			lFieldVal = lFieldVal.Elem()
			lFieldType = reflect.TypeOf(lFieldVal.Interface())
			lIsRequiredField = true
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
							log.Err("adas", err)
							continue
						}
						val = string(bytes)
					} else if col.SQLType().IsBlob() {
						var bytes []byte
						var err error
						bytes, err = json.Marshal(val)
						if err != nil {
							log.Errf("asdf", err)
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
					log.Errf("generate_query:", err)
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

	return strings.Join(lClauses, " "+self.session.orm.dialect.AndStr()+" "), res_params
}

/*
Computes the WHERE clause needed to implement an OpenERP domain.

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
	if active_test {
		if field := self.Model.Obj().GetFieldByName("active"); field != nil {
			if node != nil {
				// the item[0] trick below works for domain items and '&'/'|'/'!'
				// operators too
				var hasfield bool
				for _, node := range node.Nodes() {
					if node.String(0) == "active" {
						hasfield = true
						break
					}
				}

				if !hasfield {
					node, err := domain.String2Domain(`[('active', '=', 1)]`, nil)
					if err != nil {
						log.Panic(err)
					}
					node.Insert(0, node)
				}
			} else {
				var err error
				node, err = domain.String2Domain(`[('active', '=', 1)]`, nil)
				if err != nil {
					log.Panic(err)
				}
			}
		}
	}

	tables := make([]string, 0)
	var where_clause []string
	var where_params []interface{}
	if node != nil && node.Count() > 0 {
		exp, err := NewExpression(self.session.orm, self.Model.GetBase(), node, context)
		if err != nil {
			return nil, err
		}

		tables = exp.get_tables().Strings()
		where_clause, where_params = exp.toSql(self.Params...)

	} else {
		where_clause, where_params, tables = nil, nil, append(tables, self.Model.Table())
	}

	return NewQuery(self.session, tables, where_clause, where_params, nil, nil), nil
}

func (self *TStatement) _check_qorder(word string) (result bool) {
	re, err := regexp.Compile(`^(\s*([a-z0-9:_]+|"[a-z0-9:_]+")(\s+(desc|asc))?\s*(,|$))+$`) //`^(\s*([a-z0-9:_]+|"[a-z0-9:_]+")(\s+(desc|asc))?\s*(,|$))+(?<!,)$`
	if err != nil {
		log.Err(err)
	}

	//matches := re.FindAllStringSubmatch(word, -1)
	if re.Match([]byte(word)) {
		//  raise UserError(_('Invalid "order" specified. A valid "order" specification is a comma-separated list of valid field names (optionally followed by asc/desc for the direction)'))
		log.Err(`Invalid "order" specified. A valid "order" specification is a comma-separated list of valid field names (optionally followed by asc/desc for the direction)`)
	}
	return true
}

func (self *TStatement) generate_order_by_inner(alias, order_spec string, query *TQuery, reverse_direction bool, seen []string) []string {
	if seen == nil {
		//初始化
	}
	order_by_elements := make([]string, 0)

	generate_order := func(fields []string, order_direction string) {
		for _, fieldName := range fields {
			if fieldName == self.IdKey {
				lStr := fmt.Sprintf(`"%s"."%s" %s`, alias, fieldName, order_direction)
				order_by_elements = append(order_by_elements, lStr)

			} else {
				field := self.Model.Obj().GetFieldByName(fieldName)
				if field == nil {
					//raise ValueError(_("Sorting field %s not found on model %s") % (order_field, self._name))
					log.Warnf("Sorting field %s not found on model %s", fieldName, self.Model.String())
					continue
				}

				if field.IsRelatedField() {

				}

				if field.Store() && field.Type() == TYPE_M2O {
					// key = (self._name, order_column._obj, order_field)
					// if key not in seen{
					//     seen.add(key)
					//     inner_clauses = self.generate_m2o_order_by(alias, order_field, query, do_reverse, seen)
					//	}
				} else if field.Store() && field.SQLType().Name != "" {
					qualifield_name := query.inherits_join_calc(fieldName, self.Model)
					if field.Type() == "boolean" {
						qualifield_name = fmt.Sprintf(`COALESCE(%s, false)`, qualifield_name)
					}

					lStr := fmt.Sprintf(`%s %s`, qualifield_name, order_direction)
					order_by_elements = append(order_by_elements, lStr)
				} else {
					continue //# ignore non-readable or "non-joinable" fields
				}
			}
		}
	}

	for _, order_part := range strings.Split(order_spec, ",") {
		order_split := strings.Split(order_part, " ")
		order_field := strings.TrimSpace(order_split[0])
		order_direction := ""
		if len(order_split) == 2 {
			order_direction = strings.ToUpper(strings.TrimSpace(order_split[1]))
		} else {
			order_direction = ""
		}
		generate_order([]string{order_field}, order_direction)
	}

	generate_order(self.AscFields, "ASC")
	generate_order(self.DescFields, "DESC")
	return order_by_elements
}

func (self *TStatement) ___generate_order_by_inner(alias, order_spec string, query *TQuery, reverse_direction bool, seen []string) []string {
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
			field := self.Model.Obj().GetFieldByName(order_field)
			if field == nil {
				//raise ValueError(_("Sorting field %s not found on model %s") % (order_field, self._name))
				log.Warnf("Sorting field %s not found on model %s", order_field, self.Model.String())
				continue
			}

			if field.IsRelatedField() {

			}

			if field.Store() && field.Type() == TYPE_M2O {
				// key = (self._name, order_column._obj, order_field)
				// if key not in seen{
				//     seen.add(key)
				//     inner_clauses = self.generate_m2o_order_by(alias, order_field, query, do_reverse, seen)
				//	}
			} else if field.Store() && field.SQLType().Name != "" {
				qualifield_name := query.inherits_join_calc(order_field, self.Model)
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
func (self *TStatement) generate_order_by(query *TQuery, context map[string]interface{}) string {
	order_by_clause := ""

	if self.OrderByClause != "" || len(self.AscFields) > 0 || len(self.DescFields) > 0 {
		order_by_elements := self.generate_order_by_inner(self.Model.Table(), self.OrderByClause, query, false, nil)
		if len(order_by_elements) > 0 {
			order_by_clause = strings.Join(order_by_elements, ",")
		}
	}

	if order_by_clause != "" {
		return fmt.Sprintf(` ORDER BY %s `, order_by_clause)
	}

	return ""
}

func (self *TStatement) generate_fields() []string {
	table := self.Model
	quoter := self.session.orm.dialect.Quoter()

	var fields []string
	for _, field := range table.GetFields() {
		if self.OmitClause != "" {
			if _, ok := self.Fields[strings.ToLower(field.Name())]; ok {
				continue
			}
		}

		if !field.Store() || field.Base().MapType == ONLYTODB {
			continue
		}

		quote := quoter.Quote
		var name string
		if self.JoinClause != "" {
			if self.AliasTableName != "" {
				name = quote(self.AliasTableName)
			} else {
				name = quote(self.Model.Table())
			}
			name += "." + quote(field.Name())
		} else {
			name = quote(field.Name())
		}

		if field.IsPrimaryKey() && self.session.orm.dialect.DBType() == "ql" {
			fields = append(fields, "id() AS "+name)
		} else {
			fields = append(fields, name)
		}
	}

	return fields
}
