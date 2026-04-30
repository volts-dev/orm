package orm

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/errors"
	"github.com/volts-dev/utils"
)

// TODO 支持数据组/
func (self *TSession) Create(src interface{}, classic_create ...bool) (uid interface{}, err error) {
	model := self.Statement.Model
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

	if len(classic_create) > 0 {
		self.IsClassic = classic_create[0]
	}

	return self._create(src)
}

// start to read data from the database
func (self *TSession) Read(classic_read ...bool) (*TDataset, error) {
	model := self.Statement.Model
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

	if len(classic_read) > 0 {
		self.IsClassic = classic_read[0]
	}

	return self._read()
}

// TODO 接受多值 dataset
// TODO 当只有M2M被更新时不更新主数据倒数据库
// start to write data from the database
func (self *TSession) Write(data interface{}, classic_write ...bool) (effect int64, err error) {
	model := self.Statement.Model
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

	if len(classic_write) > 0 {
		self.IsClassic = classic_write[0]
	}

	return self._write(data)
}

// TODO 根据条件删除
// delete records
func (self *TSession) Delete(ids ...interface{}) (res_effect int64, err error) {
	model := self.Statement.Model
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
	if len(ids) > 0 {
		self.Statement.IdParam = append(self.Statement.IdParam, ids...)
	}
	ids = self.Statement.IdParam
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

	//#1 删除目标Model记录
	sql := fmt.Sprintf(`DELETE FROM %s WHERE %s in (%s); `, self.Statement.Model.Table(), id_field, idsToSqlHolder(ids...))
	res, err := self._exec(sql, ids...)
	if err != nil {
		return 0, err
	}

	cnt, err := res.RowsAffected()
	if err != nil {
		return 0, self.Rollback(err)
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

func (self *TSession) _create(src any) (any, error) {
	if len(self.Statement.Model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	/* 解析数据 */
	data, res_err := self._validateValues(src)
	if res_err != nil {
		return nil, res_err
	}

	/* 拆分数据 */
	newValues, refValues, newTodo, err := self._separateValues(data, self.Statement.Fields, self.Statement.NullableFields, true, nil)
	if err != nil {
		return nil, err
	}

	//
	var idValue any
	idField := self.Statement.Model.IdField()
	if field := self.Statement.Model.GetFieldByName(idField); field != nil {
		idValue = data.Record().GetByField(idField)
		//if utils.IsBlank(idValue) {
		if f, ok := field.(*TIdField); ok {
			idValue = f.OnCreate(&TFieldContext{
				Session: self,
				Model:   self.Statement.Model,
				Dataset: data,
				Field:   field,
			})
			newValues[idField] = idValue
		}
		//}
	}

	// 根据字段计算数据值
	datas, multiSql, err := self._todoCompute(data, nil, newTodo)
	if err != nil {
		return 0, err
	}

	/* 创建关联数据 */
	var relModel IModel
	for tbl, rel_vals := range refValues {
		if len(rel_vals) == 0 {
			continue // # 关系表无数据更新则忽略
		}

		// ???删除关联外键
		//if _, has := vals[self.model._relations[tbl]]; has {
		//	delete(vals, self.model._relations[tbl])
		//}

		/* 使用原事物会话进行创建或者更新关联表记录 */
		relModel, err = self._getModel(tbl) // NOTE 这里沿用了self的Tx
		if err != nil {
			return nil, err
		}

		// 获取管理表UID
		record_id := rel_vals[relModel.IdField()]
		if record_id == nil || utils.IsBlank(record_id) {
			/* 复制 OnConflict */
			tx := relModel.Tx()
			if oc := self.Statement.OnConflict; oc != nil {
				tx.OnConflict(oc)
			}

			id, err := tx.Create(rel_vals)
			if err != nil {
				return nil, err
			}
			record_id = id
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
	ids := make([]any, 0, multiSql)
	for idx := 0; idx < multiSql; idx++ {
		fields := make([]string, 0)
		params := make([]interface{}, 0)
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
					return nil, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
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
					return nil, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
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
				return nil, err
			}

			id = ds.Record().GetByIndex(0)
		} else {
			var res sql.Result
			res, err = self.Exec(sqlExpr, params...)
			if err != nil {
				return nil, err
			}

			// 支持递增字段返回ID
			if len(self.Statement.Model.IdField()) > 0 {
				id, err = res.LastInsertId()
				if err != nil {
					return nil, err
				}
			}
		}

		ids = append(ids, id)
	}

	/*  根据 Ids 创建 M2M 关联记录 */
	if _, _, err = self._todoCompute(data, ids, newTodo); err != nil {
		return 0, err
	}

	if id != nil {
		//更新缓存
		table_name := self.Statement.Model.Table()
		lRec := dataset.NewRecordSet(nil, newValues)
		self.orm.Cacher.PutById(table_name, utils.ToString(id), lRec) //for create

		// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
		self.orm.Cacher.ClearByTable(table_name) //for create
	}

	return id, nil
}

// Low-level implementation of write()
// TODO 只更新变更字段值
// #fix:由于更新可能只对少数字段更新,更新不适合使用缓存
func (self *TSession) _write(src any) (int64, error) {
	model := self.Statement.Model
	if len(model.String()) < 1 {
		return 0, ErrTableNotFound
	}

	data, err := self._validateValues(src)
	if err != nil {
		return 0, err
	}

	// #获取Ids
	var ids []interface{}
	if len(self.Statement.IdParam) > 0 {
		ids = self.Statement.IdParam
	} else {
		idField := model.IdField()
		if id := data.Record().GetByField(idField); id != nil {
			//  必须不是 Set 语句值
			if _, has := self.Statement.Sets[idField]; !has {
				ids = []interface{}{id}
			}
		}
	}
	includePkey := len(ids) > 0

	// 组合查询语句
	var (
		from_clause, where_clause string
		where_clause_params       []interface{}
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
		ds, err := self.Query(sql, where_clause_params...) //cr.execute(res_sql, params)
		if err != nil {
			return 0, err
		}

		len := ds.Count()
		if len == 0 {
			return 0, fmt.Errorf("Not records found from database matching for writing update!")
		}

		ids = make([]interface{}, len)
		ds.Range(func(pos int, record *dataset.TRecordSet) error {
			ids[pos] = record.GetByField(self.Statement.IdKey)
			return nil
		})
		includePkey = true
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

			params := make([]interface{}, 0, len(newVals)+len(datas)+1)
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
				sql.WriteString(fmt.Sprintf(`%s = ?`, self.Statement.IdKey))
				params = append(params, id) // add in ids data

			} else {
				sql.WriteString(fmt.Sprintf(`%s IN (%s)`,
					self.Statement.IdKey,
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
		ds, err = self.orm.Query(sql, ids...)
		if err != nil {
			return 0, err
		}

		if ds.Count() != 0 {
			refIds = make([]any, ds.Count())
			ds.Range(func(pos int, record *dataset.TRecordSet) error {
				refIds[pos] = ds.Record().GetByField(fieldName)
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
		for name, allowed := range self.Statement.Fields {
			if !allowed {
				continue
			}

			field := model.Obj().GetFieldByName(name)
			if field == nil {
				log.Warnf(`%s.read() with unknown field '%s'`, model.String(), name)
				continue
			}
			if !field.IsRelatedField() { //如果是本Model的字段
				storeFields = append(storeFields, name)
			} else {
				computedFields = append(computedFields, name)

				if field.IsRelatedField() { // and field.base_field.column:
					relateFields = append(relateFields, name)
				}
			}
		}
	} else {
		for _, field := range model.GetFields() {
			name := field.Name()
			if !field.IsRelatedField() { //如果是本Model的字段
				storeFields = append(storeFields, name)
			} else {
				computedFields = append(computedFields, name)

				if field.IsRelatedField() { // and field.base_field.column:
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

// # ids_less 缺少的ID
func (self *TSession) _readFromCache(ids []interface{}) (res []*dataset.TRecordSet, ids_less []interface{}) {
	return self.orm.Cacher.GetByIds(self.Statement.Model.Table(), ids...)
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
		where_clause_params []interface{}
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
	for _, fld := range fields {
		//if fld.IsClassicRead() && !(fld.IsRelatedField() && false) { //用false代替callable(field.base_field.column.translate)
		if fld.Store() && fld.SQLType().Name != "" && !(fld.IsRelatedField() && false) { //用false代替callable(field.base_field.column.translate)
			fields_pre = append(fields_pre, fld)
		}
	}

	if len(query.tables) > 1 {
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
	if where_clause != "" {
		where_clause = "WHERE " + where_clause
	}

	// orderby clause
	order_clause = self.Statement.generate_order_by(query, nil) // TODO 未完成

	// GroupBy clause
	if len(self.Statement.GroupByClause) > 0 {
		groupby_clause = "GROUP BY " + strings.Join(self.Statement.GroupByClause, ",")
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
// 验证输入的数据
func (self *TSession) _validateValues(values interface{}) (*dataset.TDataSet, error) {
	var result *dataset.TDataSet
	if values != nil {
		valuesMap := self._convertItf2ItfMap(values)
		if len(self.Statement.Sets) > 0 {
			result = dataset.NewDataSet(dataset.WithData(self.Statement.Sets))
			rec := result.Record()
			for k, v := range valuesMap {
				rec.SetByField(k, v)
			}
		} else {
			result = dataset.NewDataSet()
			if err := result.NewRecord(valuesMap); err != nil {
				return nil, err
			}
		}
	} else {
		if len(self.Statement.Sets) == 0 {
			return nil, fmt.Errorf("must submit the values for update")
		}

		result = dataset.NewDataSet(dataset.WithData(self.Statement.Sets))
	}

	return result, nil
}

func (self *TSession) _todoCompute(data *dataset.TDataSet, ids []any, newTodo []IField) (map[string][]any, int, error) {
	// 根据字段计算数据值
	var multiSql int = 1
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
			switch field.Type() {
			case TYPE_O2O:
				continue
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
func (self *TSession) _separateValues(data *dataset.TDataSet, mustFields map[string]bool, nullableFields map[string]bool, includeNil bool, ids []any) (map[string]interface{}, map[string]map[string]interface{}, []IField, error) {
	/* 用于更新本Model的实际数据 */
	new_vals := make(map[string]interface{})
	rel_vals := make(map[string]map[string]interface{})
	ext_todo := make([]IField, 0) // 最后处理的字段 Created Updated
	upd_todo := make([]IField, 0) // function 字段组 采用其他存储方式

	/* 初始化保存关联表用于更新创建关联表数据 */
	record := data.Record()
	self.Statement.Model.Obj().GetRelations().Range(func(key, value any) bool {
		tbl := utils.ToString(key)
		field_name := utils.ToString(value)
		rel_vals[tbl] = make(map[string]interface{}) //NOTE 新建空Map以防Nil导致内存出错

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

		if !field.IsInheritedField() {
			if field.Base().isCreated && isIncludedIds {
				// 包含主键的数据,说明已经是被创建过了,则不补全该字段
				continue
			}

			if field.Base().isCreated || field.Base().isUpdated {
				ext_todo = append(ext_todo, field)
				continue
			}
		}

		name = field.Name()
		if name == idKeyName {
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
		if field.IsRelatedField() {
			if setted {
				upd_todo = append(upd_todo, field)
			}

			continue
		}

		// 字段有值处理函数无论如何都要调用
		if field.HasSetter() {
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

		/* 过滤可以为空的字段空字段 */
		if isBlank {
			/* 填补默认值 */
			if !field.IsDefaultEmpty() {
				/* 关系字段不自动转换类型！将由字段独自处理 */
				if fieldValue = field.Default(); fieldValue != nil {
					fieldValue = value2FieldTypeValue(field, fieldValue)
				} else {
					/* 计算默认值 */
					// For inherited fields with setter/getter, default values must still land in rel_vals.
					// Let the inherited-field handler below run (it can invoke OnWrite/DefaultFunc).
					if !field.IsInheritedField() {
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
				/*if field.IsInheritedField() {
					// 如果是继承字段移动到 rel_vals 里创建记录，因本Model对应的数据没有该字段
					tableName := field.ModelName() // rel_fld.RelateTableName
					rel_vals[tableName][name] = field.onConvertToWrite(self, fieldValue)
				} else {
					new_vals[name] = field.onConvertToWrite(self, fieldValue)
					record.SetByField(name, fieldValue)
				}*/

				/* 更新不需要检测字段 */
				if isIncludedIds {
					// 包含主键的数据,说明已经是被创建过了,则不补全该字段
					continue
				} else {
					// 未包含主键的数据,需要检测是否为必须字段
					isMustField := false
					if mustFields != nil {
						isMustField = mustFields[name]
					}

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
		if field.IsInheritedField() {
			tableName := field.ModelName()
			if (setted && includeNil) || !utils.IsBlank(fieldValue) {
				rel_vals[tableName][name] = fieldValue // 其他表的值无需格式化  field.onConvertToWrite(self, fieldValue)
			}

			continue
		}

		if field.Store() && field.SQLType().Name != "" {
			if includeNil || !isBlank {
				fieldValue = field.onConvertToWrite(self, fieldValue)
				new_vals[name] = fieldValue
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
		if !field.IsInheritedField() && field.Type() == "selection" && fieldValue != nil {
			self._check_selection_field_value(field, fieldValue) //context
		}
	}

	for _, field = range ext_todo {
		name = field.Name()
		fieldValue, _ = self.orm._nowTime(field.Type()) //TODO 优化预先生成日期

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
		return nil, nil, nil, errors.NewORMError(errors.ErrorTypeValidation, strings.Join(errs, "\n"))
	}

	return new_vals, rel_vals, upd_todo, nil
}

func (self *TSession) _convertStruct2Itfmap(src interface{}) (res_map map[string]interface{}) {
	var (
		lField           reflect.StructField
		lFieldType       reflect.Type
		lFieldValue      reflect.Value
		lIsRequiredField bool
		lCol             *TField

		lName  string
		lValue interface{} //
	)

	res_map = make(map[string]interface{})
	v := reflect.ValueOf(src)

	// if pointer get the underlying element≤
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		log.Warn("Convert to struct map failed: not a struct pointer or struct value")
		return nil
	}

	lType := v.Type()

	lToOmitFields := len(self.Statement.Fields) > 0
	//	lOmitFields := make([]string, 0) // 有效字段
	for i := 0; i < lType.NumField(); i++ {
		lField = lType.Field(i)
		lName = fmtFieldName(lField.Name)

		lIsRequiredField = true
		if lToOmitFields {
			// 强制过滤已经设定的字段是否作为Query使用
			if b, ok := self.Statement.Fields[lName]; ok {
				if !b {
					continue
				}
			}
		}

		lFieldType = lField.Type
		lFieldValue = v.FieldByName(lField.Name)

		var (

		//			IsStruct bool
		// lFinalVal interface{}
		)

		// we can't access the value of unexported fields
		if lField.PkgPath != "" {
			continue
		}

		// don't check if it's omitted
		var tag string
		if tag = lField.Tag.Get(self.orm.config.FieldIdentifier); tag == "-" {
			continue
		}

		lTags := splitTag(tag)
		for _, tag := range lTags {
			lTag := parseTag(tag)
			switch strings.ToLower(lTag[0]) {
			case "name":
				if len(lTag) > 1 {
					lName = fmtFieldName(lTag[1])
				}
			case "extends", "relate":
				//				IsStruct = true
				if (lFieldValue.Kind() == reflect.Ptr && lFieldValue.Elem().Kind() == reflect.Struct) ||
					lFieldValue.Kind() == reflect.Struct {
					m := self._convertStruct2Itfmap(lFieldValue.Interface())

					for col, val := range m {
						res_map[col] = val
					}

					//
					goto CONTINUE
				}
			}
		}

		// 字段必须在数据库里
		if fld := self.Statement.Model.Obj().GetFieldByName(lName); fld == nil {
			continue
		} else {
			lCol = fld.Base()
			//废弃
			//if lCol == nil {
			//	continue
			//}
		}

		switch lFieldType.Kind() {
		case reflect.Struct:
			if lFieldType.ConvertibleTo(TimeType) {
				t := lFieldValue.Convert(TimeType).Interface().(time.Time)
				if !lIsRequiredField && (t.IsZero() || !lFieldValue.IsValid()) {
					continue
				}
				lValue = self.orm.FormatTime(lCol.SQLType().Name, t)
			} else {
				if lCol.SQLType().IsJson() {
					if lCol.SQLType().IsText() {
						bytes, err := json.Marshal(lFieldValue.Interface())
						if err != nil {
							log.Errf("IsJson", err)
							continue
						}
						lValue = string(bytes)
					} else if lCol.SQLType().IsBlob() {
						var bytes []byte
						var err error
						bytes, err = json.Marshal(lFieldValue.Interface())
						if err != nil {
							log.Errf("IsBlob", err)
							continue
						}
						lValue = bytes
					}
				} else {
					// any other
					log.Err("other field type ", lName)
				}
			}
		default:
			lValue = lFieldValue.Interface()
		}

		if lValue == nil && lFieldValue.IsValid() {
			lValue = lFieldValue.Interface()
		}

		res_map[lName] = lValue

	CONTINUE:
	}

	return
}

// # transfer struct to Itf map and record model name if could
// #1 限制字段使用 2.添加Model
func (self *TSession) _convertItf2ItfMap(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}

	// 创建 Map
	valueType := reflect.TypeOf(value)
	if valueType.Kind() == reflect.Ptr {
		valueType = valueType.Elem()
	}

	if valueType.Kind() == reflect.Struct {
		// # change model of the session
		if self.Statement.Model == nil {
			model_name := fmtModelName(utils.Obj2Name(value))
			if model_name != "" {
				self.Model(model_name)
			}
		}

		return self._convertStruct2Itfmap(value)
	} else if valueType.Kind() == reflect.Map {
		if m, ok := value.(map[string]interface{}); ok {
			return m
		} else if m, ok := value.(map[string]string); ok {
			res_map := make(map[string]interface{})

			for key, val := range m {
				res_map[key] = val // 格式化为字段类型
			}

			return res_map
		}
	}

	return nil
}

// Check whether value is among the valid values for the given
//
//	selection/reference field, and raise an exception if not.
func (self *TSession) _check_selection_field_value(field IField, value interface{}) {
	//   field = self._fields[field]
	// field.convert_to_cache(value, self)
}
