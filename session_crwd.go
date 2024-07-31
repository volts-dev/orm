package orm

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
	"github.com/volts-dev/volts/logger"
)

// TODO 支持数据组/
func (self *TSession) Create(src interface{}, classic_create ...bool) (uid interface{}, err error) {
	defer self._resetStatement()
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, fmt.Errorf("the session of query is invalid!")
	}

	var classic bool
	if len(classic_create) > 0 {
		self.IsClassic = classic
	}

	return self._create(src)
}

// start to read data from the database
func (self *TSession) Read(classic_read ...bool) (*TDataset, error) {
	// reset after complete
	defer self._resetStatement()
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
	defer self._resetStatement()
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
	defer self._resetStatement()
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	// TODO 为什么用len
	if len(self.Statement.model.String()) < 1 {
		return 0, ErrTableNotFound
	}

	// get id list
	if len(ids) > 0 {
		self.Statement.IdParam = append(self.Statement.IdParam, ids...)
	} else {
		var err error
		ids, _, err = self._search("", nil)
		if err != nil {
			return 0, err
		}
	}

	// get the model id field name
	id_field := self.Statement.model.IdField()

	//#1 删除目标Model记录
	sql := fmt.Sprintf(`DELETE FROM %s WHERE %s in (%s); `, self.Statement.model.Table(), id_field, idsToSqlHolder(ids...))
	res, err := self._exec(sql, ids...)
	if err != nil {
		return 0, err
	}

	if cnt, err := res.RowsAffected(); err != nil || (int(cnt) != len(ids)) {
		return 0, self.Rollback(err)
	}

	table_name := self.Statement.model.Table()
	//lCacher := self.orm.Cacher.RecCacher(self.Statement.model.GetName()) // for del
	//if lCacher != nil {
	for _, id := range ids {
		//lCacher.Remove(id)
		self.orm.Cacher.RemoveById(table_name, id)
	}
	//}
	// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
	//lCacher = self.orm.Cacher.SqlCacher(self.Statement.model.GetName()) // for del
	//lCacher.Clear()
	self.orm.Cacher.ClearByTable(self.Statement.model.Table())

	return res.RowsAffected()
}

func (self *TSession) _create(src any) (any, error) {
	if len(self.Statement.model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	// 解析数据
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
	idField := self.Statement.model.IdField()
	if field := self.Statement.model.GetFieldByName(idField); field != nil {
		idValue = data.Record().GetByField(idField)
		if utils.IsBlank(idValue) {
			if f, ok := field.(*TIdField); ok {
				idValue = f.OnCreate(&TFieldContext{
					Session: self,
					Model:   self.Statement.model,
					Dataset: data,
					Field:   field,
				})
				newValues[idField] = idValue
			}
		}
	}

	// 根据字段计算数据值
	datas, multiSql, err := self._todoCompute(data, []any{idValue}, newTodo)
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
			effect, err := relModel.Tx().Create(rel_vals)
			if err != nil {
				return nil, err
			}
			record_id = effect
		} else {
			relModel.Tx().Ids(record_id).Write(rel_vals)
		}

		newValues[self.Statement.model.Obj().GetRelationByName(tbl)] = record_id
	}

	// 被设置默认值的字段赋值给Val
	for k, v := range self.Statement.model.Obj().GetDefault() {
		if newValues[k] == nil {
			newValues[k] = v //fmt. lFld._symbol_c
		}
	}

	// #验证数据类型
	//TODO 需要更准确安全
	self.Statement.model.GetBase()._validate(newValues)

	var field IField
	fields := make([]string, 0)
	params := make([]interface{}, 0)

	var id any
	for idx := 0; idx < multiSql; idx++ {
		// 字段,值
		for k, v := range newValues {
			if v == nil {
				continue
			}

			if field = self.Statement.model.GetFieldByName(k); field != nil && field.IsUnique() && multiSql > 1 {
				return nil, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
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

			if field = self.Statement.model.GetFieldByName(k); field != nil && field.IsUnique() && multiSql > 1 {
				return nil, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
			}

			fields = append(fields, k)
			if len(vs) > 1 {
				params = append(params, vs[idx])
			} else {
				params = append(params, vs[0])
			}
		}

		var res sql.Result
		sql, isQuery := self.Statement.generate_insert(fields)
		if isQuery {
			ds, err := self._query(sql, params...)
			if err != nil {
				return nil, err
			}

			id = ds.Record().GetByIndex(0)
		} else {
			res, err = self.Exec(sql, params...)
			if err != nil {
				return nil, err
			}

			// 支持递增字段返回ID
			if len(self.Statement.model.IdField()) > 0 {
				id, err = res.LastInsertId()
				if err != nil {
					return nil, err
				}
			}
		}
	}

	if id != nil {
		//更新缓存
		table_name := self.Statement.model.Table()
		lRec := dataset.NewRecordSet(nil, newValues)
		self.orm.Cacher.PutById(table_name, utils.ToString(id), lRec) //for create

		// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
		self.orm.Cacher.ClearByTable(table_name) //for create
	}

	return id, nil
}

func (self *TSession) _todoCompute(data *dataset.TDataSet, ids []any, newTodo []IField) (map[string][]any, int, error) {
	// 根据字段计算数据值
	var multiSql int = 1
	datas := make(map[string][]any)
	for _, field := range newTodo {
		name := field.Name()
		value := data.Record().GetByField(field.Name())
		ctx := &TFieldContext{
			Session: self,
			Model:   self.Statement.model,
			Dataset: data,
			Field:   field,
			Value:   value,
			Ids:     ids,
		}

		if field.Store() {
			if field.IsCompute() {
				values, err := field.ComputeFunc(ctx)
				if err != nil {
					return nil, multiSql, err
				}

				// 返回值应该是和IDS同等或者只有一个共同值
				count := len(values)
				if count == len(ids) {
					datas[name] = values //记录计算数据值
					multiSql = count
				} else if count == 1 {
					datas[name] = values
				} else {
					return nil, multiSql, fmt.Errorf("the %s ComputeFunc return values is not matching records count!", field.Name())
				}

				// 结束Compute
				continue
			}

			switch field.Type() {
			case TYPE_M2O:
				/* 字符串为Name */
				if v, ok := value.(string); ok {
					ctx.Value = v
					if err := field.OnWrite(ctx); err != nil {
						return nil, multiSql, err
					}
					datas[name] = []any{ctx.Value}
					break
				}
				fallthrough
			default:
				datas[name] = []any{field.onConvertToWrite(self, value)} // field.SymbolFunc()(utils.Itf2Str(val))
			}
		} else {
			err := field.OnWrite(ctx)
			if err != nil {
				return nil, multiSql, err
			}
		}
	}

	return datas, multiSql, nil
}

// Low-level implementation of write()
// TODO 只更新变更字段值
// #fix:由于更新可能只对少数字段更新,更新不适合使用缓存
func (self *TSession) _write(src any) (int64, error) {
	model := self.Statement.model
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
			//#更新
			//self.check_access_rule(cr, user, ids, 'write', context=context)

			params := make([]interface{}, 0)
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

				// 更新里不予许多条唯一记录
				if field = self.Statement.model.GetFieldByName(k); field != nil && field.IsUnique() && multiSql > 1 {
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

				if field = self.Statement.model.GetFieldByName(k); field != nil && field.IsUnique() && multiSql > 1 {
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

			// format sql
			//sql := fmt.Sprintf(`UPDATE %s SET %s %s `,
			//	from_clause,
			//	set_clause,
			//	where_clause,
			//)

			res, err := self._exec(sql.String(), params...)
			if err != nil {
				return 0, err
			}

			res_effect, err := res.RowsAffected()
			if err != nil {
				return 0, err
			}

			/*table_name := self.Statement.model.GetName()
			//lCacher := self.orm.Cacher.RecCacher(self.Statement.model.GetName()) // for write
			//if lCacher != nil {
			for _, id := range ids {
				if id != "" {
					//更新缓存
					//lKey := self.generate_caches_key(self.Statement.model.GetName(), id)
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

		// for sub_ids in cr.split_for_in_conditions(ids):
		//     cr.execute('select distinct "'+col+'" from "'+self._table+'" ' \
		//               'where id IN %s', (sub_ids,))
		//    nids.extend([x[0] for x in cr.fetchall()])

		// add in ids data
		in_vals = strings.Repeat("?,", len(ids)-1) + "?"
		sql = fmt.Sprintf(`SELECT distinct "%s" FROM "%s" WHERE %s IN(%s)`, fieldName, model.Table(), self.Statement.IdKey, in_vals)
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
	model := self.Statement.model

	if len(model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	// TODO: check access rights 检查权限
	//	self.check_access_rights("read")
	//	fields = self._check_field_access_rights("read", fields, nil)

	//# split fields into stored and computed fields
	storeFields := make([]string, 0) // 可存于数据的字段
	relateFields := make([]string, 0)
	computedFields := make([]string, 0) // 数据库没有的字段

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

	// 处理经典字段数据
	if (self.UseNameGet || self.IsClassic) && dataset.Count() > 0 {
		// 处理那些数据库不存在的字段：company_ids...
		//# retrieve results from records; this takes values from the cache and
		// # computes remaining fields
		nameFields := make([]IField, 0)
		/*
			for _, name := range storeFields {
				fld := self.Statement.model.Obj().GetFieldByName(name)
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
	res, ids_less = self.orm.Cacher.GetByIds(self.Statement.model.Table(), ids...)
	return
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
		self.Statement.domain.IN(self.Statement.model.IdField(), self.Statement.IdParam...)
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
		if f := self.Statement.model.Obj().GetFieldByName(name); f != nil {
			fields = append(fields, f)
		}
	}

	for _, name := range relateFields {
		if f := self.Statement.model.Obj().GetFieldByName(name); f != nil {
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
			qual_names = append(qual_names, query.qualify(f, self.Statement.model))
		}
	} else {
		for _, f := range fields_pre {
			qual_names = append(qual_names, f.Name())
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
	res_ds = self.orm.Cacher.GetBySql(self.Statement.model.Table(), res_sql, where_clause_params)
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
	self.orm.Cacher.PutBySql(self.Statement.model.Table(), res_sql, where_clause_params, res_ds)

	//# 必须是合法位置上
	res_ds.First()
	return res_ds, res_sql, nil
}

// TODO
// 验证输入的数据
func (self *TSession) _validateValues(values interface{}) (*dataset.TDataSet, error) {
	var result *dataset.TDataSet
	if values != nil {
		result = dataset.NewDataSet(dataset.WithData(self.Statement.Sets))
		for k, v := range self._convertItf2ItfMap(values) {
			result.Record().SetByField(k, v)
		}
	} else {
		if len(self.Statement.Sets) == 0 {
			return nil, fmt.Errorf("must submit the values for update")
		}

		result = dataset.NewDataSet(dataset.WithData(self.Statement.Sets))
	}

	return result, nil
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
	upd_todo := make([]IField, 0) // function 字段组 采用其他存储方式

	// 初始化保存关联表用于更新创建关联表数据
	for tbl, field_name := range self.Statement.model.Obj().GetRelations() {
		rel_vals[tbl] = make(map[string]interface{}) //NOTE 新建空Map以防Nil导致内存出错

		if val := data.Record().GetByField(field_name); val != nil {
			//if val, has := vals[self.Statement.model.Obj().GetRelationByName(tbl)]; has && val != nil {
			rel_id := val                                          //新建新的并存入已经知道的ID
			rel_vals[tbl][self.Statement.model.IdField()] = rel_id //utils.Itf2Str(vals[self.model._relations[tbl]])
		}
	}

	// 格式化IdField数据生成唯一ID
	idKey := self.Statement.IdKey
	needID := ids == nil || len(ids) == 0
	/*
		if needID { //
			var idValue any
			if field := self.Statement.model.GetFieldByName(idKey); field != nil {
				idValue = data.Record().GetByField(idKey)
				if utils.IsBlank(idValue) {
					if f, ok := field.(*TIdField); ok {
						idValue = f.OnCreate(&TFieldContext{
							Session: self,
							Model:   self.Statement.model,
							Dataset: data,
							Field:   field,
						})

						ids = append(ids, idValue)
					}
				}

				// check again
				if utils.IsBlank(idValue) {
					//if id, has := new_vals[idKey]; has {
					if field.Type() == TYPE_M2O {
						// TODO name 字段
						if nameVal := data.Record().GetByField(self.Statement.model.NameField()); nameVal != nil {
							//if nameVal, has := vals[self.Statement.model.NameField()]; has {
							if self.CacheNameIds == nil {
								self.CacheNameIds = make(map[string]any)
							}
							self.CacheNameIds[nameVal.(string)] = idValue
						}
					}
				}

				new_vals[idKey] = idValue
			}
		}*/

	/* 处理常规字段 */
	var field IField
	var name string
	var value any
	var isBlank bool
	for _, field = range self.Statement.model.GetFields() {
		// --强字段--
		// ignore AutoIncrement field
		if field == nil || field.IsAutoIncrement() {
			// do no use any AutoIncrement field's value
			continue
		}

		name = field.Name()
		if name == idKey {
			continue
		}

		value = data.Record().GetByField(name)
		isBlank = utils.IsBlank(value)

		/* 填补默认值 */
		if isBlank && !field.IsDefaultEmpty() {
			value = value2FieldTypeValue(field, field.Default())
		}

		/* update time zone to create and update tags' fields */
		if isBlank && field.Base().isCreated {
			if !needID {
				// 包含主键的数据,说明已经是被创建过了,则不补全该字段
				continue
			}

			value, _ = self.orm.nowTime(field.Type()) //TODO 优化预先生成日期
		} else if field.Base().isUpdated {
			value, _ = self.orm.nowTime(field.Type()) //TODO 优化预先生成日期
		}

		/* */
		if field.SQLType().IsNumeric() {
			if v, ok := value.(string); ok {
				// 过滤0值字符串
				if v == "0" {
					value = 0
				} else {
					// 如果解析成数字成功则判定为数字成功 M2O 值可能是id或者Name值
					if v := utils.ToInt(v); v != 0 {
						value = v
					}
				}
			}
		}

		// --非强字段--
		is_must_field := mustFields[name]
		nullableField := nullableFields[name]
		if isBlank && is_must_field {
			/* Set(k,v) 指定字段*/
			// TODO
			logger.Dbgf("this field for debug %s", field.Name())
		} else {
			/* 过滤可以为空的字段空字段 */
			if !is_must_field && !field.IsCompute() && !field.Required() {
				if isBlank {
					if !nullableField || !includeNil {
						continue
					}

					/* 处理空值且Required */
					new_vals[name] = field.onConvertToWrite(self, value)
					continue
				}
			}

			// TODO 优化确认代码位置  !NOTE! 转换值为数据库类型
			//val = field.onConvertToWrite(self, val)

			/* #相同名称的字段分配给对应表 */
			if comm_models := self.Statement.model.Obj().GetCommonFieldByName(name); comm_models != nil { // 获得拥有该字段的所有表
				// 为各表预存值
				for tbl := range comm_models {
					if tbl == self.Statement.model.String() {
						new_vals[name] = field.onConvertToWrite(self, value) // 为当前表添加共同字段值

					} else if rel_vals[tbl] != nil {
						rel_vals[tbl][name] = field.onConvertToWrite(self, value) // 为关联表添加共同字段值
					}
				}

				continue //* 字段分配完毕
			}

			//#*** 非Model固有字段归为关联表字段 2个判断缺一不可
			//#1 判断是否是关联表可能性
			//#2 判断是否Model和关联Model都有该字段
			///rel_fld := self.model.RelateFieldByName(name)
			///if rel_fld != nil && field.IsRelatedField() {
			//comm_field := self.model.obj.GetCommonFieldByName(name)
			if field.IsInheritedField() {
				// 如果是继承字段移动到tocreate里创建记录，因本Model对应的数据没有该字段
				tableName := field.ModelName() // rel_fld.RelateTableName
				rel_vals[tableName][name] = field.onConvertToWrite(self, value)

			} else {
				if field.Store() && !field.IsCompute() && !field.IsRelatedField() && field.SQLType().Name != "" {
					new_vals[name] = field.onConvertToWrite(self, value)
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
				if !field.IsInheritedField() && field.Type() == "selection" && value != nil {
					self._check_selection_field_value(field, value) //context
				}
			}
		}
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
		panic("not struct")
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
		if fld := self.Statement.model.Obj().GetFieldByName(lName); fld == nil {
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
		}
		lValue = lFieldValue.Interface()
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
	value_type := reflect.TypeOf(value)
	if value_type.Kind() == reflect.Ptr || value_type.Kind() == reflect.Struct {
		// # change model of the session
		if self.Statement.model == nil {
			model_name := fmtModelName(utils.Obj2Name(value))
			if model_name != "" {
				self.Model(model_name)
			}
		}

		return self._convertStruct2Itfmap(value)
	} else if value_type.Kind() == reflect.Map {
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
