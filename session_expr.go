package orm

func (self *TSession) Model(model string, region ...string) *TSession {
	// #如果Session已经预先指定Model
	if self.Statement.model == nil || (self.Statement.model != nil && self.Statement.model.String() != model) {
		var err error
		self.Statement.model, err = self.orm.GetModel(model, region...)
		if err != nil {
			log.Panicf(err.Error())
			self.IsDeprecated = true
		}
	}

	// set IdKey
	self.Statement.IdKey = self.Statement.model.IdField() // # 主键

	/* TODO 删除  不可能会出现
	if md = nil {
		self.IsClassic = false
		tableName := utils.SnakeCasedName(strings.Replace(model, ".", "_", -1))
		//log.Err("Model %s is not a standard model type of this system", tableName)
		self.Statement.Table = self.orm.tables[tableName]
		if self.Statement.Table == nil {
			log.Errf("the table is not in database.")
			self.IsDeprecated = true
			return nil
		}
		self.Statement.AltTableNameClause = tableName
		self.Statement.TableNameClause = tableName

		// # 主键
		self.Statement.IdKey = "id"
		col := self.Statement.Table.GetFieldByName(self.Statement.Table.obj.AutoIncrementField)
		if col != nil && ((!col.Nullable && col.Base().isPrimaryKey && col.Base().isAutoIncrement) ||
			(!col.Base().Nullable && col.Base().isAutoIncrement)) {
			self.Statement.IdKey = self.Statement.Table.obj.AutoIncrementField
		}
	}
	*/

	// ### id key must exist
	if self.Statement.IdKey == "" {
		log.Errf("the statement of %s must have a Id key field is existed! please check the sync of model!", self.Statement.model.String())
		self.IsDeprecated = true
	}

	return self
}

// TODO 在生成的SQL语句前加sql
func (self *TSession) Prefix(sql string) *TSession {
	return self
}

// TODO 在生成的SQL语句后加sqlS
func (self *TSession) Suffix(sql string) *TSession {
	return self
}

// select filed or select all using * symbol
func (self *TSession) Select(fields ...string) *TSession {
	self.Statement.Select(fields...)
	return self
}

// Omit Only not use the paramters as select or update columns
func (self *TSession) Omit(fields ...string) *TSession {
	self.Statement.Omit(fields...)
	return self
}

// Id provides converting id as a query condition
func (self *TSession) Ids(ids ...interface{}) *TSession {
	self.Statement.Ids(ids...)
	return self
}

// Where condition
// Example: Where("id==?",1)
// 支持Domain 返回解析为Domain
func (self *TSession) Where(clause string, args ...interface{}) *TSession {
	self.Statement.Where(clause, args...)
	return self
}

// Join join_operator should be one of INNER, LEFT OUTER, CROSS etc - this will be prepended to JOIN
func (self *TSession) Join(joinOperator string, tablename interface{}, condition string, args ...interface{}) *TSession {
	self.Statement.Join(joinOperator, tablename, condition, args...)
	return self
}

// And provides custom query condition.
func (self *TSession) And(clause string, args ...interface{}) *TSession {
	self.Statement.And(clause, args...)
	return self
}

// Or provides custom query condition.
func (self *TSession) Or(clause string, args ...interface{}) *TSession {
	self.Statement.Or(clause, args...)
	return self
}

func (self *TSession) In(clause string, args ...interface{}) *TSession {
	self.Statement.In(clause, args...)
	return self
}

func (self *TSession) NotIn(clause string, args ...interface{}) *TSession {
	self.Statement.NotIn(clause, args...)
	return self
}

// set the pointed field value for create/write operations
func (self *TSession) Set(fieldName string, value interface{}) *TSession {
	self.Statement.Set(fieldName, value)
	return self
}

/*
	support domain string and list objec

[('foo', '=', 'bar')]
foo = 'bar'

[('id', 'in', [1,2,3])]
id in (1, 2, 3)

[('field', '=', 'value'), ('field', '<>', 42)]
( field = 'value' AND field <> 42 )

[('&', ('field', '<', 'value'), ('field', '>', 'value'))]
( field < 'value' AND field > 'value' )

[('|', ('field', '=', 'value'), ('field', '=', 'value'))]
( field = 'value' OR field = 'value' )

[('&', ('field1', '=', 'value'), ('field2', '=', 'value'), ('|', ('field3', '<>', 'value'), ('field4', '=', 'value')))]
( field1 = 'value' AND field2 = 'value' AND ( field3 <> 'value' OR field4 = 'value' ) )

[('&', ('|', ('a', '=', 1), ('b', '=', 2)), ('|', ('c', '=', 3), ('d', '=', 4)))]
( ( a = 1 OR b = 2 ) AND ( c = 3 OR d = 4 ) )

[('|', (('a', '=', 1), ('b', '=', 2)), (('c', '=', 3), ('d', '=', 4)))]
( ( a = 1 AND b = 2 ) OR ( c = 3 AND d = 4 ) )
*/
func (self *TSession) Domain(domain interface{}, args ...interface{}) *TSession {
	self.Statement.Domain(domain, args...)
	return self
}

// GroupBy Generate Group By statement
func (self *TSession) GroupBy(fields ...string) *TSession {
	self.Statement.GroupBy(fields...)
	return self
}

func (self *TSession) OrderBy(order string) *TSession {
	self.Statement.OrderBy(order)
	return self
}

func (self *TSession) Funcs(clauses ...string) *TSession {
	self.Statement.Funcs(clauses...)
	return self
}

// the value could be like "list_price ASC, name ASC, default_code ASC"
func (self *TSession) Sort(clauses ...string) *TSession {
	self.Statement.Sort(clauses...)
	return self
}

// Method Desc provide desc order by query condition, the input parameters are columns.
func (self *TSession) Desc(fileds ...string) *TSession {
	self.Statement.Desc(fileds...)
	return self
}

// Method Asc provide asc order by query condition, the input parameters are columns.
func (session *TSession) Asc(colNames ...string) *TSession {
	session.Statement.Asc(colNames...)
	return session
}

// set reutrn count 0 = default  -1 = unlimit
func (self *TSession) Limit(limit int64, offset ...int64) *TSession {
	self.Statement.Limit(limit, offset...)
	return self
}

func (self *TSession) Count() (int, error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	self.Statement.IsCount = true

	_, count, err := self._search("", nil)
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

// TODO sum
// Sum sum the records by some column. bean's non-empty fields are conditions.
func (self *TSession) Sum(colName string) (float64, error) {
	/*	defer self.Statement.Init()
		if self.IsAutoClose {
			defer self.Close()
		}

		v := reflect.ValueOf(res)
		if v.Kind() != reflect.Ptr {
			return errors.New("need a pointer to a variable")
		}

		var isSlice = v.Elem().Kind() == reflect.Slice
		var sql string
		var args []interface{}
		var err error
		if len(self.statement.RawSQL) == 0 {
			sql, args, err = self.statement.generate_sum(columnNames...)
			if err != nil {
				return err
			}
		} else {
			sql = self.statement.RawSQL
			args = self.statement.RawParams
		}

		session.queryPreprocess(&sql, args...)

		if isSlice {
			if session.isAutoCommit {
				err = session.DB().QueryRow(sql, args...).ScanSlice(res)
			} else {
				err = session.tx.QueryRow(sql, args...).ScanSlice(res)
			}
		} else {
			if session.isAutoCommit {
				err = session.DB().QueryRow(sql, args...).Scan(res)
			} else {
				err = session.tx.QueryRow(sql, args...).Scan(res)
			}
		}

		if err == sql.ErrNoRows || err == nil {
			return nil
		}
	*/
	return 0, nil
}

// NoCascade indicate that no cascade load child object
func (self *TSession) NoCascade() *TSession {
	self.Statement.UseCascade = false
	return self
}

// ForUpdate Set Read/Write locking for UPDATE
func (self *TSession) ForUpdate() *TSession {
	self.Statement.IsForUpdate = true
	return self
}
