package orm

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/errors"
	"github.com/volts-dev/utils"
)

// Create 在指定 model 上插入一条或多条记录，支持传入多个 src（变参/数组）。
// 返回值：单条（或不传 src 走 Sets）时返回该记录的 id；传入多条时返回 []any 形式的 id 列表。
// 如需 classic 模式，先链式调 session.Classic()；
// 如需自定义 ctx，先链式调 session.WithContext(ctx)。
func (self *TSession) Create(src ...any) (uid []any, err error) {
	model := self.Statement.Model
	self.Op = OpCreate
	if _, err := model.BeforeSession(self); err != nil {
		return nil, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, ErrInvalidSession
	}

	return self._create(src...)
}

// Read 在 Statement 配置的条件下读取记录集。
// 如需 classic 模式，先链式调 session.Classic()；
// 如需自定义 ctx，先链式调 session.WithContext(ctx)。
func (self *TSession) Read() (*TDataset, error) {
	model := self.Statement.Model
	self.Op = OpRead
	if _, err := model.BeforeSession(self); err != nil {
		return nil, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, ErrInvalidSession
	}

	return self._read()
}

// TODO 接受多值 dataset
// TODO 当只有M2M被更新时不更新主数据倒数据库
// Write 在 Statement 配置的条件下更新记录。
// 如需 classic 模式，先链式调 session.Classic()；
// 如需自定义 ctx，先链式调 session.WithContext(ctx)。
func (self *TSession) Write(data any) (effect int64, err error) {
	model := self.Statement.Model
	self.Op = OpWrite
	if _, err := model.BeforeSession(self); err != nil {
		return 0, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	return self._write(data)
}

// TODO 根据条件删除
// delete records
func (self *TSession) Delete(ids ...any) (res_effect int64, err error) {
	model := self.Statement.Model
	self.Op = OpDelete
	if _, err := model.BeforeSession(self); err != nil {
		return 0, err
	}
	defer func() {
		model.AfterSession(self)

		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	// TODO 为什么用len
	if len(self.Statement.Model.String()) < 1 {
		return 0, ErrTableNotFound
	}

	// get id list — merge explicit args with any ids already set via Ids()
	// flattenIds 使 Create 返回的 []any 可直接回喂给 Delete()
	if len(ids) > 0 {
		self.Statement.IdParam = append(self.Statement.IdParam, flattenIds(ids)...)
	}
	ids = self.Statement.IdParam

	// Phase 2: safety guard — block no-condition deletes unless explicitly opted-in
	if !self.allowUnsafe && !self.hasCondition() {
		return 0, errors.ErrUnsafe
	}

	if len(ids) == 0 {
		var err error
		ids, _, err = self._search("", nil)
		if err != nil {
			return 0, err
		}
	}
	expectRowCount := int64(len(ids))

	if len(ids) == 0 {
		// Nothing to delete, prevent SQL syntax error on empty IN clause
		return 0, nil
	}

	// get the model id field name
	id_field := self.Statement.Model.IdField()
	quoter := self.orm.dialect.Quoter()

	//#1 删除目标Model记录
	sql := fmt.Sprintf(`DELETE FROM %s WHERE %s in (%s); `,
		quoter.QuoteIdentMust(self.Statement.Model.Table()),
		quoter.QuoteIdentMust(id_field),
		idsToSqlHolder(ids...))
	res, err := self._exec(sql, ids...)
	if err != nil {
		return 0, err
	}

	cnt, err := res.RowsAffected()
	if err != nil {
		// autocommit 模式下 DELETE 已提交，RowsAffected 出错不应 Rollback（会造成"已回滚"的错觉）；
		// 仅在确处于事务时才回滚。
		if self.tx != nil {
			return 0, self.Rollback(err)
		}
		return 0, err
	}

	/* check the row count */
	if cnt != expectRowCount {
		log.Warnf("expect delete %d rows, but %d rows affected", expectRowCount, cnt)
		return expectRowCount, nil
	}
	/*
		table_name := self.Statement.Model.Table()
		//lCacher := self.orm.Cacher.RecCacher(self.Statement.Model.GetName()) // for del
		//if lCacher != nil {
		for _, id := range ids {
			//lCacher.Remove(id)
			self.orm.Cacher.RemoveById(table_name, id)
		}
		//}
		// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
		//lCacher = self.orm.Cacher.SqlCacher(self.Statement.Model.GetName()) // for del
		//lCacher.Clear()
		self.orm.Cacher.ClearByTable(self.Statement.Model.Table())
	*/
	return res.RowsAffected()
}

// _create 支持传入多个 src（变参/数组），一次插入多条记录。
// 优化点：整批共享的不变量（表名校验、主键字段解析）在循环外只处理一次，
// 循环内仅保留与每条记录数据相关的处理；其余逻辑与 _create 完全一致。
// 返回每条记录的 id（顺序与传入一致）；中途出错则返回已成功的 id 列表 + error。
func (self *TSession) _create(src ...any) ([]any, error) {
	// —— 整批不变量：循环外只处理一次 ——
	if len(self.Statement.Model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	// 不传 src 时回退为单条（Sets）创建
	if len(src) == 0 {
		src = []any{nil}
	}

	idField := self.Statement.Model.IdField()
	// 主键字段对象与 TIdField 断言仅解析一次；OnCreate 仍按记录调用
	var idCreator *TIdField
	if field := self.Statement.Model.GetFieldByName(idField); field != nil {
		idCreator, _ = field.(*TIdField)
	}

	ids := make([]any, 0, len(src))

	// —— 每条记录处理 ——
	for _, one := range src {
		// If src is nil but Sets are present, use Sets as the data source.
		// If src is provided, _validateValues converts it; Sets are applied afterward.
		if one == nil && len(self.Statement.Sets) == 0 {
			return ids, fmt.Errorf("must submit the values for create")
		}
		srcWasSets := false
		if one == nil {
			one = self.Statement.Sets
			srcWasSets = true
		}

		/* 解析数据 */
		data, res_err := self._validateValues(one)
		if res_err != nil {
			return ids, res_err
		}

		/* 应用 Sets（覆盖已有值） */
		if !srcWasSets && len(self.Statement.Sets) > 0 {
			rec := data.Record()
			for k, v := range self.Statement.Sets {
				rec.SetByField(k, v)
			}
		}

		/* 拆分数据 */
		newValues, refValues, newTodo, err := self._separateValues(data, self.Statement.Fields, self.Statement.NullableFields, true, nil)
		if err != nil {
			return ids, err
		}

		if idCreator != nil {
			newValues[idField] = idCreator.OnCreate(&TFieldContext{
				Session: self,
				Model:   self.Statement.Model,
				Dataset: data,
				Field:   idCreator,
			})
		}

		// 根据字段计算数据值
		datas, multiSql, err := self._todoCompute(data, nil, newTodo)
		if err != nil {
			return ids, err
		}

		/* 创建关联数据 */
		var relModel IModel
		for tbl, rel_vals := range refValues {
			fieldName := self.Statement.Model.Obj().GetRelationByName(tbl)
			hasExplicitValue := false
			if v := newValues[fieldName]; v != nil && !utils.IsBlank(v) {
				hasExplicitValue = true
			}
			if v := datas[fieldName]; len(v) > 0 && !utils.IsBlank(v[0]) {
				hasExplicitValue = true
			}

			// 跳过条件：(a) 关系表没有任何待写字段；或 (b) 用户已经显式提供了关联外键值。
			// 任一成立都说明无需自动创建/更新关联记录。
			if len(rel_vals) == 0 || hasExplicitValue {
				continue
			}

			/* 使用原事物会话进行创建或者更新关联表记录 */
			relModel, err = self._getModel(tbl) // NOTE 这里沿用了self的Tx
			if err != nil {
				return ids, err
			}

			// 获取管理表UID
			record_id := rel_vals[relModel.IdField()]
			if record_id == nil || utils.IsBlank(record_id) {
				/* 复制 OnConflict */
				tx := relModel.Tx()
				if oc := self.Statement.OnConflict; oc != nil {
					tx.OnConflict(oc)
				}

				rids, err := tx.Create(rel_vals)
				if err != nil {
					return ids, err
				}
				record_id = rids[0]
			} else {
				relModel.Tx().Ids(record_id).Write(rel_vals)
			}

			newValues[self.Statement.Model.Obj().GetRelationByName(tbl)] = record_id
		}

		// 被设置默认值的字段赋值给Val
		self.Statement.Model.Obj().GetDefault().Range(func(key, value any) bool {
			k := key.(string)
			if newValues[k] == nil {
				newValues[k] = value //fmt. lFld._symbol_c
			}
			return true
		})

		// #验证数据类型
		//TODO 需要更准确安全
		self.Statement.Model.GetBase()._validate(newValues)

		var field IField
		var id any
		recIds := make([]any, 0, multiSql)
		for idx := 0; idx < multiSql; idx++ {
			fields := make([]string, 0)
			params := make([]any, 0)
			uniqueFields := make([]string, 0)

			// 字段,值
			for k, v := range newValues {
				if v == nil {
					continue
				}

				// 避免计算字段重复
				if _, has := datas[k]; has {
					continue
				}

				if field = self.Statement.Model.GetFieldByName(k); field != nil && field.IsUnique() && !field.IsPrimaryKey() {
					uniqueFields = append(uniqueFields, field.Name())
					if multiSql > 1 {
						return ids, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
					}
				}

				if k == idField {
					id = v
				}

				fields = append(fields, k)
				params = append(params, v)
			}

			for k, vs := range datas {
				if vs == nil {
					continue
				}

				if field = self.Statement.Model.GetFieldByName(k); field != nil && field.IsUnique() && !field.IsPrimaryKey() {
					uniqueFields = append(uniqueFields, field.Name())
					if multiSql > 1 {
						return ids, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
					}
				}

				fields = append(fields, k)
				if len(vs) > 1 {
					params = append(params, vs[idx])
				} else {
					params = append(params, vs[0])
				}
			}

			OnConflictValues := make([]any, 0)
			if self.Statement.OnConflict != nil {
				if self.Statement.OnConflict.UpdateAll {
					self.Statement.OnConflict.DoUpdates = make([]string, 0)
					for field_name, v := range newValues {
						/* id 字段不参与更新 */
						if field_name == self.Statement.Model.IdField() {
							continue
						}

						if utils.IndexOf(field_name, self.Statement.OnConflict.Fields...) == -1 {
							self.Statement.OnConflict.DoUpdates = append(self.Statement.OnConflict.DoUpdates, field_name)
							OnConflictValues = append(OnConflictValues, v)
						}
					}
				} else if len(self.Statement.OnConflict.DoUpdates) > 0 {
					for _, field_name := range self.Statement.OnConflict.DoUpdates {
						if v, ok := newValues[field_name]; ok {
							OnConflictValues = append(OnConflictValues, v)
						}
					}
				} else {
					self.Statement.OnConflict.DoNothing = true
				}
			}

			params = append(params, OnConflictValues...)
			sqlExpr, isQuery := self.Statement.generate_insert(fields, uniqueFields)
			if isQuery {
				ds, err := self._query(sqlExpr, params...)
				if err != nil {
					return ids, err
				}

				id = ds.Record().GetByIndex(0)
			} else {
				var res sql.Result
				res, err = self.Exec(sqlExpr, params...)
				if err != nil {
					return ids, err
				}

				// 支持递增字段返回ID
				if len(self.Statement.Model.IdField()) > 0 {
					id, err = res.LastInsertId()
					if err != nil {
						return ids, err
					}
				}
			}

			recIds = append(recIds, id)
		}

		/*  根据 Ids 创建 M2M 关联记录 */
		if _, _, err = self._todoCompute(data, recIds, newTodo); err != nil {
			return ids, err
		}

		if id != nil {
			//更新缓存
			table_name := self.Statement.Model.Table()
			lRec := dataset.NewRecordSet(nil, newValues)
			self.orm.Cacher.PutById(table_name, utils.ToString(id), lRec) //for create

			// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
			self.orm.Cacher.ClearByTable(table_name) //for create
		}

		ids = append(ids, id)
	}

	return ids, nil
}

// Low-level implementation of write()
// TODO 只更新变更字段值
// #fix:由于更新可能只对少数字段更新,更新不适合使用缓存
func (self *TSession) _write(src any) (int64, error) {
	model := self.Statement.Model
	if len(model.String()) < 1 {
		return 0, ErrTableNotFound
	}

	// If src is nil but Sets are present, use Sets as the data source.
	if src == nil && len(self.Statement.Sets) == 0 {
		return 0, fmt.Errorf("must submit the values for update")
	}
	srcWasSets := false
	if src == nil {
		src = self.Statement.Sets
		srcWasSets = true
	}

	data, err := self._validateValues(src)
	if err != nil {
		return 0, err
	}

	/* 应用 Sets（覆盖已有值） */
	if !srcWasSets && len(self.Statement.Sets) > 0 {
		rec := data.Record()
		for k, v := range self.Statement.Sets {
			rec.SetByField(k, v)
		}
	}

	// #获取Ids
	var ids []any
	if len(self.Statement.IdParam) > 0 {
		ids = self.Statement.IdParam
	} else {
		idField := model.IdField()
		if id := data.Record().GetByField(idField); id != nil {
			//  必须不是 Set 语句值
			if _, has := self.Statement.Sets[idField]; !has {
				// flattenIds 处理 data 中 id 值本身为 []any 的情况（如直接回喂 Create 返回值）
				ids = flattenIds([]any{id})
			}
		}
	}
	includePkey := len(ids) > 0

	// Phase 2: safety guard — block no-condition writes unless explicitly opted-in.
	// Guard runs here (after ID extraction from data) so Write(map_with_id) is allowed.
	if !self.allowUnsafe && !includePkey && self.Statement.domain.Count() == 0 {
		return 0, errors.ErrUnsafe
	}

	// 组合查询语句
	var (
		from_clause, where_clause string
		where_clause_params       []any
	)

	if includePkey {
		from_clause = model.Table()

	} else if self.Statement.domain.Count() > 0 {
		query, err := self.Statement.where_calc(self.Statement.domain, false, nil)
		if err != nil {
			return 0, err
		}

		// # determine the actual query to execute
		from_clause, where_clause, where_clause_params = query.getSql()
		// the PK condition status
		if where_clause == "" {
			return 0, fmt.Errorf("must have ids or qury clause")
		}

		sql := JoinClause(
			"SELECT",
			self.Statement.IdKey,
			"FROM",
			from_clause,
			"WHERE",
			where_clause,
		)
		// 获得Id占位符索引
		ds, err := self._query(sql, where_clause_params...) // use internal _query to avoid premature AutoClose
		if err != nil {
			return 0, err
		}

		len := ds.Count()
		if len == 0 {
			return 0, fmt.Errorf("Not records found from database matching for writing update!")
		}

		ids = make([]any, len)
		ds.Range(func(pos int, record *dataset.TRecordSet) error {
			ids[pos] = record.GetByField(self.Statement.IdKey)
			return nil
		})
	} else {
		return 0, fmt.Errorf("At least have one of Where()|Domain()|Ids() condition to locate for writing update")
	}

	newVals, refVals, newTodo, err := self._separateValues(data, self.Statement.Fields, self.Statement.NullableFields, false, ids)
	if err != nil {
		return 0, err
	}

	// 根据字段计算数据值
	datas, multiSql, err := self._todoCompute(data, ids, newTodo)
	if err != nil {
		return 0, err
	}

	var field IField
	var effectedRows int64 = 0
	if len(newVals) > 0 {
		quoter := self.orm.dialect.Quoter().Quote
		for idx, id := range ids {
			//self.check_access_rule(cr, user, ids, 'write', context=context)

			params := make([]any, 0, len(newVals)+len(datas)+1)
			//set_clause := ""

			// TODO 验证数据类型
			//self._validate(lNewVals)

			// 拼SQL
			var sql strings.Builder
			sql.WriteString("UPDATE ")
			sql.WriteString(from_clause)
			sql.WriteString(" SET ")

			sqlLen := sql.Len()
			comma := false
			for k, v := range newVals {
				if comma {
					sql.WriteString(",")
				}

				// 避免计算字段重复
				if _, has := datas[k]; has {
					continue
				}

				// 更新里不予许多条唯一记录
				if field = self.Statement.Model.GetFieldByName(k); field != nil && field.IsUnique() && multiSql > 1 {
					return 0, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
				}

				sql.WriteString(quoter(k))
				sql.WriteString("=?")
				params = append(params, v)

				comma = true
			}

			comma = len(datas) != 0
			for k, vs := range datas {
				if comma {
					sql.WriteString(",")
				}

				if field = self.Statement.Model.GetFieldByName(k); field != nil && field.IsUnique() && multiSql > 1 {
					return 0, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
				}

				sql.WriteString(quoter(k))
				sql.WriteString("=?")
				if len(vs) > 1 {
					params = append(params, vs[idx])
				} else {
					params = append(params, vs[0])
				}

				comma = true
			}

			if sql.Len() == sqlLen {
				return 0, fmt.Errorf("must have values")
			}

			sql.WriteString(" WHERE ")
			if multiSql > 1 {
				sql.WriteString(fmt.Sprintf(`%s = ?`, quoter(self.Statement.IdKey)))
				params = append(params, id) // add in ids data

			} else {
				sql.WriteString(fmt.Sprintf(`%s IN (%s)`,
					quoter(self.Statement.IdKey),
					strings.Repeat("?,", len(ids)-1)+"?"),
				)
				params = append(params, ids...) // add in ids data
			}

			res, err := self._exec(sql.String(), params...)
			if err != nil {
				return 0, err
			}

			res_effect, err := res.RowsAffected()
			if err != nil {
				return 0, err
			}

			/*table_name := self.Statement.Model.GetName()
			//lCacher := self.orm.Cacher.RecCacher(self.Statement.Model.GetName()) // for write
			//if lCacher != nil {
			for _, id := range ids {
				if id != "" {
					//更新缓存
					//lKey := self.generate_caches_key(self.Statement.Model.GetName(), id)
					lRec := NewRecordSet(nil, lNewVals)
					self.orm.Cacher.PutById(table_name, id, lRec)
				}
			}*/
			//}
			/* 统计 */
			effectedRows += res_effect

			// 退出多SQL更新
			if multiSql == 1 {
				break
			}
		}
	}

	// 更新关联表
	var refIds []any
	var refModel IModel
	var ds *TDataset
	var in_vals, fieldName, sql string
	for tbl, ref_vals := range refVals {
		if len(ref_vals) == 0 {
			continue
		}

		fieldName = model.Obj().GetRelationByName(tbl)

		quoter := self.orm.dialect.Quoter().Quote
		// add in ids data
		in_vals = strings.Repeat("?,", len(ids)-1) + "?"
		sql = fmt.Sprintf("SELECT distinct %s FROM %s WHERE %s IN(%s)",
			quoter(fieldName), quoter(model.Table()), quoter(self.Statement.IdKey), in_vals)
		ds, err = self._query(sql, ids...)
		if err != nil {
			return 0, err
		}

		if ds.Count() != 0 {
			refIds = make([]any, ds.Count())
			ds.Range(func(pos int, record *dataset.TRecordSet) error {
				// 用 Range 回调的 record 取每行的值；旧实现误用 ds.Record()（始终是游标位置 0），
				// 导致多记录时所有 refIds 都取到第一行，写错关联记录。
				refIds[pos] = record.GetByField(fieldName)
				return nil
			})

			//# 重新写入关联数据
			refModel, err = self._getModel(tbl) // NOTE 这里沿用了self的Tx
			if err != nil {
				return 0, err
			}
			refModel.Tx().Ids(refIds...).Write(ref_vals) //TODO 检查是否真确使用
		}
	}

	return effectedRows, nil
}

func (self *TSession) _read() (*dataset.TDataSet, error) {
	model := self.Statement.Model

	if len(model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	// TODO: check access rights 检查权限
	//	self.check_access_rights("read")
	//	fields = self._check_field_access_rights("read", fields, nil)

	//# split fields into stored and computed fields
	storeFields := make([]string, 0, 16) // 可存于数据的字段
	relateFields := make([]string, 0, 8)
	computedFields := make([]string, 0, 8) // 数据库没有的字段

	// 字段分类
	// 验证Select * From
	if len(self.Statement.Fields) > 0 {
		for _, name := range self.Statement.Fields {
			// 排除被 Omit 标记的字段
			if self.Statement.IsOmit(name) {
				continue
			}

			field := model.Obj().GetFieldByName(name)
			if field == nil {
				log.Warnf(`%s.read() with unknown field '%s'`, model.String(), name)
				continue
			}
			if !field.IsRelated() { //如果是本Model的字段
				storeFields = append(storeFields, name)
			} else {
				computedFields = append(computedFields, name)

				if field.IsRelated() { // and field.base_field.column:
					relateFields = append(relateFields, name)
				}
			}
		}
	} else {
		for _, field := range model.GetFields() {
			name := field.Name()
			// 排除被 Omit 标记的字段
			if self.Statement.IsOmit(name) {
				continue
			}
			if !field.IsRelated() { //如果是本Model的字段
				storeFields = append(storeFields, name)
			} else {
				computedFields = append(computedFields, name)

				if field.IsRelated() { // and field.base_field.column:
					relateFields = append(relateFields, name)
				}
			}
		}
	}

	// 获取数据库数据
	//# fetch stored fields from the database to the cache
	dataset, _, err := self._readFromDatabase(storeFields, relateFields)
	if err != nil {
		return nil, err
	}

	// TODO 优化循环代码
	// 处理经典字段数据
	if (self.UseNameGet || self.IsClassic) && dataset.Count() > 0 {
		// 处理那些数据库不存在的字段：company_ids...
		//# retrieve results from records; this takes values from the cache and
		// # computes remaining fields
		nameFields := make([]IField, 0)
		/*
			for _, name := range storeFields {
				fld := self.Statement.Model.Obj().GetFieldByName(name)
				if fld != nil {
					nameFields = append(nameFields, fld)
				}
			}
		*/
		for _, name := range computedFields {
			fld := model.Obj().GetFieldByName(name)
			if fld != nil {
				nameFields = append(nameFields, fld)
			}
		}

		//FIXME　执行太多SQL
		for _, field := range nameFields {
			err := field.OnRead(&TFieldContext{
				Session: self,
				Model:   model,
				Field:   field,
				//Id:      rec_id,
				//Value:   val,
				Dataset:     dataset,
				UseNameGet:  self.UseNameGet,
				ClassicRead: self.IsClassic, // FIXME 如果为True会无限循环查询
			})
			if err != nil {
				log.Errf("%s@%s.OnRead:%s", field.ModelName(), field.Name(), err.Error())
			}
		}
	}

	dataset.First()
	dataset.Classic(self.IsClassic)
	return dataset, nil
}

/*
   """ Read the given fields of the records in ``self`` from the database,
       and store them in cache. Access errors are also stored in cache.

       :param field_names: list of column names of model ``self``; all those
           fields are guaranteed to be read
       :param inherited_field_names: list of column names from parent
           models; some of those fields may not be read
   """
*/
// 从数据库读取记录并保存到缓存中
// :param field_names: Model的所有字段
// :param inherited_field_names:关联父表的所有字段
func (self *TSession) _readFromDatabase(storeFields, relateFields []string) (res_ds *dataset.TDataSet, res_sql string, err error) {
	var (
		query *TQuery
		select_clause, from_clause, where_clause,
		order_clause, limit_clause, offset_clause, groupby_clause string
		where_clause_params []any
	)
	// 生成查询条件
	// 当指定了主键其他查询条件将失效
	if len(self.Statement.IdParam) != 0 {
		self.Statement.domain.Clear() // 清楚其他查询条件
		self.Statement.domain.IN(self.Statement.Model.IdField(), self.Statement.IdParam...)
	}

	query, err = self.Statement.where_calc(self.Statement.domain, false, nil)
	if err != nil {
		return nil, "", err
	}

	/* Join fields and function clause */
	qual_names := make([]string, 0)
	//if self.IsClassic {
	//对可迭代函数'iterable'中的每一个元素应用‘function’方法，将结果作为list返回
	//# determine the fields that are stored as columns in tables;
	fields := make([]IField, 0)
	fields_pre := make([]IField, 0)
	for _, name := range storeFields {
		if f := self.Statement.Model.Obj().GetFieldByName(name); f != nil {
			fields = append(fields, f)
		}
	}

	for _, name := range relateFields {
		if f := self.Statement.Model.Obj().GetFieldByName(name); f != nil {
			fields = append(fields, f)
		}
	}

	//	当字段为field.base_field.column.translate可调用即是translate为回调函数而非Bool值时不加入Join
	hasInherited := false
	for _, fld := range fields {
		//if fld.IsClassicRead() && !(fld.IsRelatedField() && false) { //用false代替callable(field.base_field.column.translate)
		if fld.Store() && fld.SQLType().Name != "" { //用false代替callable(field.base_field.column.translate) — IsRelated check pending
			fields_pre = append(fields_pre, fld)
		} else if fld.IsInherited() && fld.SQLType().Name != "" && !fld.HasGetter() &&
			fld.TypeName() != TYPE_O2M && fld.TypeName() != TYPE_M2M {
			// _inherits 委托字段：本表无列(store=false)，靠 qualify→inherits_join_calc
			// JOIN 父表(o2o FK)取值，故仍须进入 SELECT 并带模型限定。
			// 仅纳入父表上有真实列者：标量字段与 m2o(FK 列)。
			// o2m/m2m 是虚拟关系字段、永远没有物理列(注意它们仍可能被赋了 SQLType，
			// 故必须按 TypeName 排除，不能只看 SQLType)；getter 函数字段同样无列。
			// 这些由各自的 OnRead 单独取值，不进 JOIN-SELECT。
			fields_pre = append(fields_pre, fld)
			hasInherited = true
		}
	}

	// 多表(域条件引入 JOIN)或存在继承字段时，所有字段都带表限定，
	// 以触发 qualify→inherits_join_calc 为继承字段补上父表 JOIN。
	if len(query.tables) > 1 || hasInherited {
		for _, f := range fields_pre {
			qual_names = append(qual_names, query.qualify(f, self.Statement.Model))
		}
	} else {
		for _, f := range fields_pre {
			qual_names = append(qual_names, query.qualify(f, nil))
		}
	}

	//} else {
	//	qual_names = self.Statement.generate_fields()
	//}
	select_clause = strings.Join(append(qual_names, self.Statement.FuncsClause...), ",")

	// # determine the actual query to execute
	from_clause, where_clause, where_clause_params = query.getSql()

	// Phase 2: soft-delete auto-filter
	if deletedField := self.Statement.Model.Obj().DeletedField; deletedField != "" {
		quoter := self.orm.dialect.Quoter()
		quoted := quoter.QuoteIdentMust(deletedField)
		var sdFilter string
		switch self.softDeleteMode {
		case softDeleteFilterActive:
			sdFilter = quoted + " IS NULL"
		case softDeleteOnlyDeleted:
			sdFilter = quoted + " IS NOT NULL"
		}
		if sdFilter != "" {
			if where_clause == "" {
				where_clause = sdFilter
			} else {
				where_clause = where_clause + " AND " + sdFilter
			}
		}
	}

	if where_clause != "" {
		where_clause = "WHERE " + where_clause
	}

	// orderby clause
	order_clause = self.Statement.generate_order_by(query, nil) // TODO 未完成

	// GroupBy clause — 每个字段必须命中模型字段并经标识符校验/引用，防止注入
	if len(self.Statement.GroupByClause) > 0 {
		quoter := self.orm.dialect.Quoter()
		groupCols := make([]string, 0, len(self.Statement.GroupByClause))
		for _, name := range self.Statement.GroupByClause {
			if field := self.Statement.Model.GetFieldByName(name); field == nil {
				log.Warnf("GroupBy field %s not found on model %s, ignored", name, self.Statement.Model.String())
				continue
			}
			q, err := quoter.QuoteIdent(name)
			if err != nil {
				log.Warnf("GroupBy field %s is not a valid identifier, ignored: %v", name, err)
				continue
			}
			groupCols = append(groupCols, q)
		}
		if len(groupCols) > 0 {
			groupby_clause = "GROUP BY " + strings.Join(groupCols, ",")
		}
	}

	// limit clause
	limit := self.Statement.LimitClause
	if limit != -1 {
		if limit == 0 {
			limit = DefaultLimit

		}
		limit_clause = "LIMIT " + utils.ToString(limit)
	}

	// offset clause
	if self.Statement.OffsetClause > 0 {
		offset_clause = "OFFSET " + utils.ToString(self.Statement.OffsetClause)
	}

	res_sql = JoinClause(
		"SELECT",
		select_clause,
		"FROM",
		from_clause,
		where_clause,
		order_clause,
		groupby_clause,
		limit_clause,
		offset_clause,
	)

	// 从缓存里获得数据
	res_ds = self.orm.Cacher.GetBySql(self.Statement.Model.Table(), res_sql, where_clause_params)
	if res_ds != nil {
		res_ds.First()
		return res_ds, res_sql, nil
	}

	// 获得Id占位符索引
	res_ds, err = self.Query(res_sql, where_clause_params...) //cr.execute(res_sql, params)
	if err != nil {
		return nil, "", err
	}

	//# 添加进入缓存
	self.orm.Cacher.PutBySql(self.Statement.Model.Table(), res_sql, where_clause_params, res_ds)

	//# 必须是合法位置上
	res_ds.First()
	return res_ds, res_sql, nil
}

// TODO
// _validateValues converts any supported value into a *dataset.TDataSet.
// It does NOT apply Statement.Sets — callers handle Sets inline after this call.
// Supported inputs: *dataset.TDataSet (returned as-is), map[string]any,
// map[string]string, or a struct pointer/value.
func (self *TSession) _validateValues(values any) (*dataset.TDataSet, error) {
	// Session-specific concern: auto-detect Model from struct type name
	// when no model is set yet on the Statement. Must happen BEFORE
	// NormalizeValues so the struct path has a non-nil model.
	if values != nil && self.Statement.Model == nil {
		rv := reflect.ValueOf(values)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if rv.Kind() == reflect.Struct {
			if name := fmtModelName(utils.Obj2Name(values)); name != "" {
				self.Model(name)
			}
		}
	}
	return NormalizeValues(values, self.Statement.Model)
}

func (self *TSession) _todoCompute(data *dataset.TDataSet, ids []any, newTodo []IField) (map[string][]any, int, error) {
	// 根据字段计算数据值
	var multiSql = 1
	var name string
	var value any
	var ctx *TFieldContext
	datas := make(map[string][]any)
	for _, field := range newTodo {
		name = field.Name()
		value = data.Record().GetByField(name)
		ctx = &TFieldContext{
			Session: self,
			Model:   self.Statement.Model,
			Dataset: data,
			Field:   field,
			Value:   value,
			Ids:     ids,
		}

		if utils.IsBlank(value) {
			if defaultFunc := field.DefaultFunc(); defaultFunc != nil {
				//if utils.IsBlank(value) && !field.IsDefaultEmpty() {
				if err := defaultFunc(ctx); err != nil {
					return nil, multiSql, err
				}

				datas[name] = []any{ctx.values}
				continue
			}
		}

		if field.Store() {
			switch field.TypeName() {
			case TYPE_M2O:
				/* 字符串为Name */
				if v, ok := value.(string); ok {
					ctx.Value = v
					if err := field.OnWrite(ctx); err != nil {
						return nil, multiSql, err
					}
					datas[name] = []any{ctx.values}
					break
				}
				fallthrough
			default:
				if field.HasGetter() || field.HasSetter() {
					//if err := field.ComputeFunc(ctx); err != nil {
					//	return nil, multiSql, err
					//}
					if err := field.OnWrite(ctx); err != nil {
						return nil, multiSql, err
					}

					switch v := ctx.values.(type) {
					case map[string]any:
						datas[name] = []any{v}
					case []any:
						count := len(v)
						if count > 1 {
							//if count == len(ids) {
							datas[name] = v // 记录计算数据值
							multiSql = count
						} else if count == 1 {
							datas[name] = v
						} else {
							return nil, multiSql, fmt.Errorf("the %s ComputeFunc return values is not matching records count!", field.Name())
						}
					default:
						//if len(ids) == 1 {
						datas[name] = []any{ctx.values}
						//}
						//return nil, multiSql, fmt.Errorf("the %s ComputeFunc return values is not matching records count!", field.Name())
					}

				} else {
					datas[name] = []any{field.onConvertToWrite(self, value)} // field.SymbolFunc()(utils.Itf2Str(val))
				}
			}
		} else {
			/* for M2M 字段*/
			/* ids 可能由于 onConflict 导致无法获取到值，此时需要重新获取 */
			if ids != nil {
				if err := field.OnWrite(ctx); err != nil {
					return nil, multiSql, err
				}
			}
		}
	}

	return datas, multiSql, nil
}

// TODO FN
// 分配值并补全ID,Update,Create字段值
// separate data for difference type of update
// , includeVersion bool, includeUpdated bool, includeNil bool,
//
//	includeAutoIncr bool, allUseBool bool, useAllCols bool,
//	mustColumnMap map[string]bool, nullableMap map[string]bool,
//	columnMap map[string]bool, update, unscoped bool
//
// needID is the values inclduing key
func (self *TSession) _separateValues(data *dataset.TDataSet, mustFields []string, nullableFields map[string]bool, includeNil bool, ids []any) (map[string]any, map[string]map[string]any, []IField, error) {
	/* 用于更新本Model的实际数据 */
	new_vals := make(map[string]any)
	rel_vals := make(map[string]map[string]any)
	ext_todo := make([]IField, 0) // 最后处理的字段 Created Updated
	upd_todo := make([]IField, 0) // function 字段组 采用其他存储方式

	/* 初始化保存关联表用于更新创建关联表数据 */
	record := data.Record()
	self.Statement.Model.Obj().GetRelations().Range(func(key, value any) bool {
		tbl := utils.ToString(key)
		field_name := utils.ToString(value)
		rel_vals[tbl] = make(map[string]any) //NOTE 新建空Map以防Nil导致内存出错

		/* 添加非空值到关系表数据集里*/
		if val := record.GetByField(field_name); !utils.IsBlank(val) {
			//if val, has := data[field_name]; has && utils.IsBlank(val) {
			//if val, has := vals[self.Statement.Model.Obj().GetRelationByName(tbl)]; has && val != nil {
			rel_id := val                                          //新建新的并存入已经知道的ID
			rel_vals[tbl][self.Statement.Model.IdField()] = rel_id //utils.Itf2Str(vals[self.model._relations[tbl]])
		}
		return true
	})

	// 格式化IdField数据生成唯一ID
	idKeyName := self.Statement.IdKey

	/* 处理常规字段 */
	var errs []string
	var name string
	var field IField
	var fieldValue any
	var isBlank, setted bool
	isIncludedIds := len(ids) != 0
	for _, field = range self.Statement.Model.GetFields() {
		// ignore AutoIncrement field
		if field == nil || field.IsAutoIncrement() {
			// do no use any AutoIncrement field's value
			continue
		}

		if !field.IsInherited() {
			if field.Base().isCreatedAt && isIncludedIds {
				// 包含主键的数据,说明已经是被创建过了,则不补全该字段
				continue
			}

			if field.Base().isCreatedAt || field.Base().isUpdatedAt {
				ext_todo = append(ext_todo, field)
				continue
			}
		}

		name = field.Name()
		if name == idKeyName {
			continue
		}

		// 排除被 Omit 标记的字段，使其不参与写入
		if self.Statement.IsOmit(name) {
			continue
		}

		fieldValue = record.GetByField(name)
		setted = fieldValue != nil
		isBlank = !setted || utils.IsBlank(fieldValue)

		// int64 有时候传进来的数字是string类型 需要转换成数字类型
		if field.SQLType().IsNumeric() {
			if v, ok := fieldValue.(string); ok {
				if vv, err := utils.IsNumeric(v); err == nil {
					fieldValue = vv
				} else {
					// 如果解析成数字成功则判定为数字成功 M2O 值可能是id或者Name值
					fieldValue = field.onConvertToWrite(self, fieldValue)
				}
				record.SetByField(name, fieldValue)
			}
		}

		/* #相同名称的字段分配给对应表 */
		if comm_models := self.Statement.Model.Obj().GetCommonFieldByName(name); setted && comm_models != nil { // 获得拥有该字段的所有表
			// 为各表预存值
			/*
				modelName := self.Statement.Model.String()
				fieldValue = field.onConvertToWrite(self, fieldValue) // 为当前表添加共同字段值
				for tbl := range comm_models {
					if tbl == modelName {
						new_vals[name] = fieldValue
						data.Record().SetByField(name, fieldValue)
					} else {
						rel_vals[tbl][name] = fieldValue
					}
				}*/
			modelName := self.Statement.Model.String()
			for tbl := range comm_models {
				if tbl != modelName {
					rel_vals[tbl][name] = fieldValue
				}
			}
		}

		// 关系字段不自动转换类型！将由字段独自处理
		if field.IsRelated() {
			if setted {
				upd_todo = append(upd_todo, field)
			}

			continue
		}

		// 字段有值处理函数无论如何都要调用
		if field.HasSetter() {
			// 创建时（无 ids）总是运行 Setter（无值时补算、有值时转换）；
			// 更新时（有 ids）仅当显式提供了值时才运行。仅 (update && 无值) 不运行。
			if !isIncludedIds || setted {
				ctx := &TFieldContext{
					Session: self,
					Model:   self.Statement.Model,
					Dataset: data,
					Field:   field,
					Value:   fieldValue,
					Ids:     ids,
				}
				if err := field.OnWrite(ctx); err != nil {
					return nil, nil, nil, err
				}

				if ctx.values != nil {
					fieldValue = ctx.values
					isBlank = false
				}
			}
		}

		/* 过滤可以为空的字段空字段 */
		if isBlank && !isIncludedIds {
			/* 填补默认值 */
			if !field.IsDefaultEmpty() {
				if field.DefaultFunc() != nil {
					ctx := &TFieldContext{
						Session: self,
						Model:   self.Statement.Model,
						Dataset: data,
						Field:   field,
						Value:   fieldValue,
						Ids:     ids,
					}
					if err := field.DefaultFunc()(ctx); err != nil {
						return nil, nil, nil, err
					}

					if ctx.values != nil {
						fieldValue = ctx.values
					}
					// isBlank is unconditionally reset to false after this if/else chain
				} else if fieldValue = field.Default(); fieldValue != nil {
					/* 关系字段不自动转换类型！将由字段独自处理 */
					fieldValue = value2FieldTypeValue(field, fieldValue)
				} else {
					/* 计算默认值 */
					// For inherited fields with setter/getter, default values must still land in rel_vals.
					// Let the inherited-field handler below run (it can invoke OnWrite/DefaultFunc).
					if !field.IsInherited() {
						upd_todo = append(upd_todo, field)
						continue
					}
				}
				isBlank = false
			}

			/* 再次确认空值 */
			if isBlank && !setted {
				/* 处理空值 */
				//if setted && (includeNil || isIncludedIds) {
				//if  (includeNil || isIncludedIds) {
				/* 分离关系表字段 */
				/*if field.IsInherited() {
					// 如果是继承字段移动到 rel_vals 里创建记录，因本Model对应的数据没有该字段
					tableName := field.ModelName() // rel_fld.RelateTableName
					rel_vals[tableName][name] = field.onConvertToWrite(self, fieldValue)
				} else {
					new_vals[name] = field.onConvertToWrite(self, fieldValue)
					record.SetByField(name, fieldValue)
				}*/

				/* 更新不需要检测字段 */
				if isIncludedIds {
					// If field is explicitly marked nullable, write nil as SQL NULL
					if nullableFields != nil {
						if isNullable, ok := nullableFields[name]; ok && isNullable {
							new_vals[name] = nil
						}
					}
					continue
				} else {
					// 未包含主键的数据,需要检测是否为必须字段
					isMustField := utils.IndexOf(name, mustFields...) != -1

					// nullableFields: treat "absent/nil map" as "no constraint" (i.e. nullable).
					// If present, the value means "is nullable".
					notNullable := false
					if nullableFields != nil {
						if isNullable, ok := nullableFields[name]; ok {
							notNullable = !isNullable
						}
					}

					if isMustField || field.Required() || notNullable {
						// Fields with setter/getter/default-func may compute their values from other inputs;
						// don't force the caller to provide an explicit value in that case.
						if field.HasSetter() || field.HasGetter() || field.DefaultFunc() != nil {
							continue
						}
						errs = append(errs, fmt.Sprintf("Field %s is required", field.Name()))
					}
				}
				//}
			}
		}

		/* 接下来fieldValue为什么值都要赋值包含空值也不例外！ */
		/*
			if field.SQLType().IsNumeric() {
				if v, ok := fieldValue.(string); ok {
					// 过滤0值字符串
					if v == "0" {
						fieldValue = 0
					} else {
						// 如果解析成数字成功则判定为数字成功 M2O 值可能是id或者Name值
						fieldValue=field.onConvertToWrite(self, fieldValue)
						if v := utils.ToInt(v); v != 0 {
							fieldValue = v
						}
					}
				}
			}
		*/
		// TODO 优化确认代码位置  !NOTE! 转换值为数据库类型
		//val = field.onConvertToWrite(self, val)

		//#*** 非Model固有字段归为关联表字段 2个判断缺一不可
		//#1 判断是否是关联表可能性
		//#2 判断是否Model和关联Model都有该字段
		if field.IsInherited() {
			tableName := field.ModelName()
			if (setted && includeNil) || !utils.IsBlank(fieldValue) {
				rel_vals[tableName][name] = fieldValue // 其他表的值无需格式化  field.onConvertToWrite(self, fieldValue)
			}

			continue
		}

		if field.Store() && field.SQLType().Name != "" {
			isExplicitlyNullable := nullableFields != nil && nullableFields[name]
			if includeNil || !isBlank || isExplicitlyNullable {
				if isExplicitlyNullable && isBlank {
					new_vals[name] = nil // write SQL NULL for explicitly nullable blank field
				} else {
					fieldValue = field.onConvertToWrite(self, fieldValue)
					new_vals[name] = fieldValue
				}
				record.SetByField(name, fieldValue)
			}
		} else {
			//# 大型复杂计算字段
			upd_todo = append(upd_todo, field)
		}

		/*
			if field.IsClassicWrite() && field.Base().Fnct_inv() == nil {
				if !field.Translatable() { //TODO totranslate &&

					new_vals[name] = field.SymbolFunc()(utils.Itf2Str(val))

					//direct = append(direct, name)
				} else {
					upd_todo = append(upd_todo, name)
				}
			}
		*/
		/* check selection */
		if !field.IsInherited() && field.TypeName() == "selection" && fieldValue != nil {
			self._check_selection_field_value(field, fieldValue) //context
		}
	}

	for _, field = range ext_todo {
		name = field.Name()
		fieldValue, _ = self.orm._nowTime(field.TypeName()) //TODO 优化预先生成日期

		if len(new_vals) != 0 {
			new_vals[name] = fieldValue // 为当前表添加共同字段值
			record.SetByField(name, fieldValue)
		}

		for tbl := range self.Statement.Model.Obj().GetCommonFieldByName(name) {
			if data := rel_vals[tbl]; len(data) != 0 {
				rel_vals[tbl][name] = fieldValue // 为关联表添加共同字段值
			}
		}
	}

	// 如果出现错误
	if len(errs) != 0 {
		return nil, nil, nil, errors.New(errors.ErrValidation, fmt.Errorf("%s", strings.Join(errs, "\n")))
	}

	return new_vals, rel_vals, upd_todo, nil
}

// _structToMap is a thin session-bound wrapper around StructToMap that wires
// in Statement.Model and the Statement.OmitFields filter. Kept for backward
// compatibility with callers (including tests) that use the session method.
func (self *TSession) _structToMap(src any) map[string]any {
	return StructToMap(src, self.Statement.Model, self.Statement.OmitFields)
}

// Check whether value is among the valid values for the given
//
//	selection/reference field, and raise an exception if not.
func (self *TSession) _check_selection_field_value(field IField, value any) {
	//   field = self._fields[field]
	// field.convert_to_cache(value, self)
}
