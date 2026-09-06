package orm

func (self *TSession) Model(model string, options ...ModelOption) *TSession {
	// #如果Session已经预先指定Model
	if self.Statement.Model == nil || (self.Statement.Model != nil && self.Statement.Model.String() != model) {
		var err error
		self.Statement.Model, err = self.orm.GetModel(model, options...)
		if err != nil {
			// 错误文本是数据不是格式串：带 % 的错误（如 SQL 里的 LIKE '%x%'）
			// 会被当成格式符，打出 %!x(MISSING) 之类的乱码。
			log.Panicf("%s", err.Error())
			self.IsDeprecated = true
		}
	}

	// set IdKey
	self.Statement.IdKey = self.Statement.Model.IdField() // # 主键

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

	// 除中间表外，主键必须存在
	if self.Statement.IdKey == "" {
		if _, ok := self.orm.osv.middleModel.Load(self.Statement.Model.String()); !ok {
			log.Errf("the statement of %s must have a Id key field is existed! please check the sync of model!", self.Statement.Model.String())
			self.IsDeprecated = true
		}
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
func (self *TSession) Ids(ids ...any) *TSession {
	self.Statement.Ids(ids...)
	return self
}

// Where condition
// Example: Where("id=?", 1)   （注意是单个 =：`==` 不是本 DSL 的算子，会以 invalid domain leaf 报错）
// 支持Domain 返回解析为Domain
func (self *TSession) Where(clause string, args ...any) *TSession {
	self.Statement.Where(clause, args...)
	return self
}

// Join join_operator should be one of INNER, LEFT OUTER, CROSS etc - this will be prepended to JOIN
func (self *TSession) Join(joinOperator string, tablename any, condition string, args ...any) *TSession {
	self.Statement.Join(joinOperator, tablename, condition, args...)
	return self
}

// And provides custom query condition.
func (self *TSession) And(clause string, args ...any) *TSession {
	self.Statement.And(clause, args...)
	return self
}

// Or provides custom query condition.
func (self *TSession) Or(clause string, args ...any) *TSession {
	self.Statement.Or(clause, args...)
	return self
}

func (self *TSession) In(clause string, args ...any) *TSession {
	self.Statement.In(clause, args...)
	return self
}

func (self *TSession) NotIn(clause string, args ...any) *TSession {
	self.Statement.NotIn(clause, args...)
	return self
}

// set the pointed field value for create/write operations
func (self *TSession) Set(fieldName string, value any) *TSession {
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
func (self *TSession) Domain(domain any, args ...any) *TSession {
	self.Statement.Domain(domain, args...)
	return self
}

// GroupBy Generate Group By statement
func (self *TSession) GroupBy(fields ...string) *TSession {
	self.Statement.GroupBy(fields...)
	return self
}

// OrderBy 追加排序字段。可一次传多个，也可链式多次调用：
// OrderBy("sequence", "id") 与 OrderBy("sequence,id") 与 OrderBy("sequence").OrderBy("id")
// 三者等价。
func (self *TSession) OrderBy(order ...string) *TSession {
	self.Statement.OrderBy(order...)
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
func (self *TSession) Asc(colNames ...string) *TSession {
	self.Statement.Asc(colNames...)
	return self
}

/*
// Limit 设置查询的限制条件，常用于分页查询
// 该方法接收两个可变参数：limit（限制返回的记录数）和offset（从哪条记录开始）
// offset是可选参数，如果未提供，则默认从第一条记录开始
// 返回值是当前的TSession对象，支持链式调用
0 = default
-1 = unlimit
*/
func (self *TSession) Limit(limit int64, offset ...int64) *TSession {
	self.Statement.Limit(limit, offset...)
	return self
}

// NoCascade indicate that no cascade load child object
func (self *TSession) NoCascade() *TSession {
	self.Statement.UseCascade = false
	return self
}

// ForUpdate 给本次读取加排他行锁（SELECT ... FOR UPDATE）。
//
// 必须在事务里用，否则返回 errors.ErrLockOutsideTransaction：单语句事务会在语句
// 结束的一瞬间释放锁，等于没加。典型的读-改-写：
//
//	sess.Begin()
//	ds, err := sess.Model("sale.order").Ids(id).ForUpdate().Read()
//	... // 此刻该行已被本事务独占
//	sess.Model("sale.order").Ids(id).Write(map[string]any{"state": "done"})
//	sess.Commit()
//
// 也可加在 Where(...).Write(...) 上，这样"定位 id 的 SELECT"与随后的 UPDATE
// 之间不再有窗口，条件更新成为真正的原子 CAS：
//
//	sess.Model("sale.order").Where("state=?", "draft").ForUpdate().Write(vals)
//
// 可选 NoWait() / SkipLocked() 调整抢不到锁时的行为。
// SQLite 无行锁，只打一条警告并照常执行（写事务本身即全库互斥）。
func (self *TSession) ForUpdate(opts ...LockOption) *TSession {
	self.Statement.Lock = newLock(LockUpdate, opts...)
	return self
}

// ForShare 给本次读取加共享行锁（SELECT ... FOR SHARE，MySQL 5.7 为
// LOCK IN SHARE MODE）：允许别人同样共享读，但阻止修改。
// 约束同 ForUpdate。
func (self *TSession) ForShare(opts ...LockOption) *TSession {
	self.Statement.Lock = newLock(LockShare, opts...)
	return self
}

func (self *TSession) OnConflict(conflict *OnConflict) *TSession {
	self.Statement.OnConflict = conflict
	return self
}

// Nullable marks the given field names as explicitly nullable in the next Write
// call, allowing nil values to be written as SQL NULL rather than skipped.
// Use when restoring soft-deleted records: .Nullable("deleted_at").Write({deleted_at: nil}).
func (self *TSession) Nullable(fields ...string) *TSession {
	if self.Statement.NullableFields == nil {
		self.Statement.NullableFields = make(map[string]bool)
	}
	for _, f := range fields {
		self.Statement.NullableFields[f] = true
	}
	return self
}
