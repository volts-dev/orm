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
	return self._write(data, nil)
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
		ids, err = self._search("", nil)
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

func (self *TSession) _create(src interface{}) (res_id interface{}, res_err error) {
	if len(self.Statement.model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	// 解析数据
	var vals map[string]interface{}
	vals, res_err = self._validateValues(src)
	if res_err != nil {
		return nil, res_err
	}

	// 拆分数据
	newValues, refValues, lNewTodo, res_err := self._separateValues(vals, self.Statement.Fields, self.Statement.NullableFields, true, true)
	if res_err != nil {
		return nil, res_err
	}

	// 创建关联数据
	for tbl, rel_vals := range refValues {
		if len(rel_vals) == 0 {
			continue // # 关系表无数据更新则忽略
		}

		// ???删除关联外键
		//if _, has := vals[self.model._relations[tbl]]; has {
		//	delete(vals, self.model._relations[tbl])
		//}

		/* 使用原事物会话进行创建或者更新关联表记录 */
		lMdlObj, err := self._getModel(tbl) // NOTE 这里沿用了self的Tx
		if err != nil {
			return nil, err
		}

		// 获取管理表UID
		record_id := rel_vals[lMdlObj.IdField()]
		if record_id == nil || utils.IsBlank(record_id) {
			effect, err := lMdlObj.Tx().Create(rel_vals)
			if err != nil {
				return nil, err
			}
			record_id = effect
		} else {
			lMdlObj.Tx().Ids(record_id).Write(rel_vals)
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

	id_field := self.Statement.model.IdField()
	fields := make([]string, 0)
	params := make([]interface{}, 0)
	// 字段,值
	for k, v := range newValues {
		if v == nil { // 过滤nil 的值
			continue
		}

		if k == id_field {
			res_id = v
		}

		fields = append(fields, k)
		params = append(params, v)
	}

	var res sql.Result
	var err error
	sql, isQuery := self.Statement.generate_insert(fields)
	if isQuery {
		ds, err := self._query(sql, params...)
		if err != nil {
			return nil, err
		}

		res_id = ds.Record().GetByIndex(0)
	} else {
		res, err = self.Exec(sql, params...)
		if err != nil {
			return nil, err
		}

		// 支持递增字段返回ID
		if len(self.Statement.model.IdField()) > 0 {
			res_id, res_err = res.LastInsertId()
			if res_err != nil {
				return nil, res_err
			}
		}
	}

	/* 更新关联字段 */
	for _, name := range lNewTodo {
		lField := self.Statement.model.GetFieldByName(name)
		if lField != nil {
			if err = lField.OnWrite(&TFieldContext{
				Session: self,
				Model:   self.Statement.model,
				Id:      res_id,
				Field:   lField,
				Value:   vals[name]}); err != nil {
				return nil, err
			}
		}
	}

	if res_id != nil {
		//更新缓存
		table_name := self.Statement.model.Table()
		lRec := dataset.NewRecordSet(nil, newValues)
		self.orm.Cacher.PutById(table_name, utils.IntToStr(res_id), lRec) //for create

		// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
		self.orm.Cacher.ClearByTable(table_name) //for create
	}

	return res_id, nil
}

// TODO 只更新变更字段值
// #fix:由于更新可能只对少数字段更新,更新不适合使用缓存
func (self *TSession) _write(src interface{}, context map[string]interface{}) (res_effect int64, res_err error) {
	if len(self.Statement.model.String()) < 1 {
		return -1, ErrTableNotFound
	}

	var (
		ids              []interface{}
		values, lNewVals map[string]interface{}
		lRefVals         map[string]map[string]interface{}
		lNewTodo         []string

		query                     *TQuery
		from_clause, where_clause string
		where_clause_params       []interface{}
	)

	values, res_err = self._validateValues(src)
	if res_err != nil {
		return 0, res_err
	}

	// #获取Ids
	if len(self.Statement.IdParam) > 0 {
		ids = self.Statement.IdParam
	} else {
		idField := self.Statement.model.IdField()
		if id, has := values[idField]; has {
			//  必须不是 Set 语句值
			if _, has := self.Statement.Sets[idField]; !has {
				ids = []interface{}{id}
			}
		}
	}

	// 组合查询语句
	if len(ids) > 0 {
		from_clause = self.Statement.model.Table()
		where_clause = fmt.Sprintf(`%s IN (%s)`,
			self.Statement.IdKey,
			strings.Repeat("?,", len(ids)-1)+"?")
		where_clause_params = ids

	} else if self.Statement.domain.Count() > 0 {
		query, res_err = self.Statement.where_calc(self.Statement.domain, false, nil)
		if res_err != nil {
			return 0, res_err
		}

		// # determine the actual query to execute
		from_clause, where_clause, where_clause_params = query.getSql()
	} else {
		return 0, fmt.Errorf("At least have one of Where()|Domain()|Ids() condition to locate for writing update")
	}

	if where_clause != "" {
		where_clause = "WHERE " + where_clause
	}

	// the PK condition status
	includePkey := len(ids) > 0
	if !includePkey && where_clause == "" {
		return 0, fmt.Errorf("must have ids or qury clause")
	}

	if self.IsClassic {
		//???
		for field := range values {
			var fobj IField
			fobj = self.Statement.model.GetFieldByName(field)
			if fobj == nil {
				lF := self.Statement.model.Obj().GetRelatedFieldByName(field)
				if lF != nil {
					fobj = lF.RelateField
				}
			}

			if fobj == nil {
				continue
			}
		}

		lNewVals, lRefVals, lNewTodo, res_err = self._separateValues(values, self.Statement.Fields, self.Statement.NullableFields, false, !includePkey)
		if res_err != nil {
			return 0, res_err
		}
	}

	if len(lNewVals) > 0 {
		//#更新
		//self.check_access_rule(cr, user, ids, 'write', context=context)

		params := make([]interface{}, 0)
		set_clause := ""

		// TODO 验证数据类型
		//self._validate(lNewVals)

		// 拼SQL
		quoter := self.orm.dialect.Quoter()
		for k, v := range lNewVals {
			if set_clause != "" {
				set_clause += ","
			}

			set_clause += quoter.Quote(k) + "=?"
			params = append(params, v)
		}

		if set_clause == "" {
			return 0, fmt.Errorf("must have values")
		}

		// add in ids data
		params = append(params, where_clause_params...)

		// format sql
		sql := fmt.Sprintf(`UPDATE %s SET %s %s `,
			from_clause,
			set_clause,
			where_clause,
		)

		res, err := self._exec(sql, params...)
		if err != nil {
			return 0, err
		}

		res_effect, res_err = res.RowsAffected()
		if res_err != nil {
			return 0, res_err
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

	}

	// 更新关联表
	for tbl, ref_vals := range lRefVals {
		if len(ref_vals) == 0 {
			continue
		}

		lFldName := self.Statement.model.Obj().GetRelationByName(tbl)
		nids := make([]interface{}, 0)
		// for sub_ids in cr.split_for_in_conditions(ids):
		//     cr.execute('select distinct "'+col+'" from "'+self._table+'" ' \
		//               'where id IN %s', (sub_ids,))
		//    nids.extend([x[0] for x in cr.fetchall()])

		// add in ids data
		in_vals := strings.Repeat("?,", len(ids)-1) + "?"
		lSql := fmt.Sprintf(`SELECT distinct "%s" FROM "%s" WHERE %s IN(%s)`, lFldName, self.Statement.model.Table(), self.Statement.IdKey, in_vals)
		lDs, err := self.orm.Query(lSql, ids...)
		if err != nil {
			return 0, err
		}

		lDs.First()
		for !lDs.Eof() {
			nids = append(nids, lDs.FieldByName(lFldName).AsInterface())
			lDs.Next()
		}

		if len(ref_vals) > 0 { //# 重新写入关联数据
			lMdlObj, err := self._getModel(tbl) // NOTE 这里沿用了self的Tx
			if err != nil {
				return 0, err
			}
			lMdlObj.Tx().Ids(nids...).Write(ref_vals) //TODO 检查是否真确使用
		}
	}

	// TODO 计算字段预先计算好值更新到记录里而不单一更新
	// 更新计算字段
	for _, name := range lNewTodo {
		lField := self.Statement.model.GetFieldByName(name)
		if lField != nil {
			err := lField.OnWrite(&TFieldContext{
				Session: self,
				Model:   self.Statement.model,
				Id:      ids[0], // TODO 修改获得更合理
				Field:   lField,
				Value:   values[name],
			})
			if err != nil {
				return 0, err
			}

			res_effect++
		}
	}

	return
}

func (self *TSession) _read() (*dataset.TDataSet, error) {
	if len(self.Statement.model.String()) < 1 {
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

			field := self.Statement.model.Obj().GetFieldByName(name)
			if field == nil {
				log.Warnf(`%s.read() with unknown field '%s'`, self.Statement.model.String(), name)
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
		for _, field := range self.Statement.model.GetFields() {
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
	if self.IsClassic && dataset.Count() > 0 {
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
			fld := self.Statement.model.Obj().GetFieldByName(name)
			if fld != nil {
				nameFields = append(nameFields, fld)
			}
		}

		//FIXME　执行太多SQL
		for _, field := range nameFields {
			err := field.OnRead(&TFieldContext{
				Session: self,
				Model:   self.Statement.model,
				Field:   field,
				//Id:      rec_id,
				//Value:   val,
				Dataset:    dataset,
				UseNameGet: self.IsClassic,
				//ClassicRead: self.IsClassic, // FIXME 如果为True会无限循环查询
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
	{ // 生成查询条件
		// 当指定了主键其他查询条件将失效
		if len(self.Statement.IdParam) != 0 {
			self.Statement.domain.Clear() // 清楚其他查询条件
			self.Statement.domain.IN(self.Statement.model.IdField(), self.Statement.IdParam...)
		}

		query, err = self.Statement.where_calc(self.Statement.domain, false, nil)
		if err != nil {
			return nil, "", err
		}

		// orderby clause
		order_clause = self.Statement.generate_order_by(query, nil) // TODO 未完成

		// limit clause
		if self.Statement.LimitClause > 0 {
			limit_clause = "LIMIT " + utils.ToString(self.Statement.LimitClause)
		}

		// offset clause
		if self.Statement.OffsetClause > 0 {
			offset_clause = "OFFSET " + utils.ToString(self.Statement.OffsetClause)
		}

		// 生成字段名列表
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

		/* Join fields and function clause */
		select_clause = strings.Join(append(qual_names, self.Statement.FuncsClause...), ",")

		// # determine the actual query to execute
		from_clause, where_clause, where_clause_params = query.getSql()
		if where_clause != "" {
			where_clause = "WHERE " + where_clause
		}

		if len(self.Statement.GroupBySClause) > 0 {
			groupby_clause = "GROUP BY " + strings.Join(self.Statement.GroupBySClause, ",")
		}
	}

	res_sql = JoinClause(
		"SELECT",
		select_clause,
		"FROM",
		from_clause,
		where_clause,
		limit_clause,
		offset_clause,
		order_clause,
		groupby_clause,
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
func (self *TSession) _validateValues(values interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	if values != nil {
		result = self._convertItf2ItfMap(values)
		if len(result) == 0 {
			return nil, fmt.Errorf("can't support this type of values: %v", values)
		}

		result = utils.MergeMaps(self.Statement.Sets, result)

	} else {
		if len(self.Statement.Sets) == 0 {
			return nil, fmt.Errorf("must submit the values for update")
		}

		result = self.Statement.Sets
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
// includePkey is the values inclduing key
func (self *TSession) _separateValues(vals map[string]interface{}, mustFields map[string]bool, nullableFields map[string]bool, includeNil bool, mustPkey bool) (map[string]interface{}, map[string]map[string]interface{}, []string, error) {
	//!!! create record not need to including pk

	// 用于更新本Model的实际数据
	/*    # list of column assignments defined as tuples like:
	      #   (column_name, format_string, column_value)
	      #   (column_name, sql_formula)
	      # Those tuples will be used by the string formatting for the INSERT
	      # statement below.
	      ('id', "nextval('%s')" % self._sequence),*/
	new_vals := make(map[string]interface{})
	rel_vals := make(map[string]map[string]interface{})
	upd_todo := make([]string, 0) // function 字段组 采用其他存储方式

	// 保存关联表用于更新创建关联表数据
	for tbl, field_name := range self.Statement.model.Obj().GetRelations() {
		rel_vals[tbl] = make(map[string]interface{}) //NOTE 新建空Map以防Nil导致内存出错
		if val, has := vals[field_name]; has && val != nil {
			//if val, has := vals[self.Statement.model.Obj().GetRelationByName(tbl)]; has && val != nil {
			rel_id := val //新建新的并存入已经知道的ID
			if rel_id != nil {
				rel_vals[tbl][self.Statement.model.IdField()] = rel_id //utils.Itf2Str(vals[self.model._relations[tbl]])
			}
		}
	}

	// 处理常规字段
	for _, field := range self.Statement.model.GetFields() {
		if field == nil {
			continue
		}

		name := field.Name()

		// --强字段--
		// TODO 保留审视 // ignore AutoIncrement field
		//	if col != nil && !mustPkey && (col.IsAutoIncrement || col.IsPrimaryKey) {
		if field.IsAutoIncrement() {
			continue //!!! do no use any AutoIncrement field's value
		}

		value, has := vals[name]
		/* 填补默认值 */
		if (!has || value == nil) && !field.IsDefaultEmpty() {
			value = value2FieldTypeValue(field, field.Default())
		}

		// update time zone to create and update tags' fields
		if mustPkey && field.Base().isCreated {
			if !has {
				lTimeItfVal, _ := self.orm.nowTime(field.Type()) //TODO 优化预先生成日期
				value = lTimeItfVal
			}

		} else if field.Base().isCreated {
			// 包含主键的数据,说明已经是被创建过了,则不补全该字段
			continue

		} else if field.Base().isUpdated {
			lTimeItfVal, _ := self.orm.nowTime(field.Type()) //TODO 优化预先生成日期
			value = lTimeItfVal
		}

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

		isBlank := utils.IsBlank(value)

		// ** 格式化IdField数据 生成UID
		if name == self.Statement.IdKey {
			if isBlank {
				if f, ok := field.(*TIdField); mustPkey && ok {
					new_vals[name] = f.OnCreate(&TFieldContext{
						Session: self,
						Model:   self.Statement.model,
						Field:   field,
						Id:      utils.ToString(0),
						Value:   value},
					)

				}
			} else {
				new_vals[name] = value
			}

			if id, has := new_vals[self.Statement.IdKey]; has {
				if field.Type() == TYPE_M2O {
					// TODO name 字段
					if nameVal, has := vals[self.Statement.model.NameField()]; has {
						if self.CacheNameIds == nil {
							self.CacheNameIds = make(map[string]any)
						}
						self.CacheNameIds[nameVal.(string)] = id
					}
				}
			}

			continue
		}

		// --非强字段--
		is_must_field := mustFields[name]
		nullableField := nullableFields[name]
		if !has && value == nil {
			/* Set(k,v) 指定字段*/
			if is_must_field {
				// TODO
			}

		} else {
			// 过滤可以为空的字段空字段
			if isBlank && !field.Required() && (!is_must_field || !nullableField || !includeNil) {
				continue
			}

			/* 处理空值且Required */
			if isBlank {
				new_vals[name] = field.onConvertToWrite(self, value)
				continue
			}

			// TODO 优化确认代码位置  !NOTE! 转换值为数据库类型
			//val = field.onConvertToWrite(self, val)

			// #相同名称的字段分配给对应表
			comm_models := self.Statement.model.Obj().GetCommonFieldByName(name) // 获得拥有该字段的所有表
			if comm_models != nil {
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
				if field.Store() && field.SQLType().Name != "" {
					switch field.Type() {
					case TYPE_M2O:
						/* 字符串为Name */
						if v, ok := value.(string); ok {
							ctx := &TFieldContext{
								Session: self,
								Model:   self.Statement.model,
								//Id:      res_id, // TODO
								Field: field,
								Value: v,
							}
							if err := field.OnWrite(ctx); err != nil {
								return nil, nil, nil, err
							}
							new_vals[name] = ctx.Value
							break
						}
						fallthrough
					default:
						new_vals[name] = field.onConvertToWrite(self, value) // field.SymbolFunc()(utils.Itf2Str(val))
					}

				} else {
					//# 大型复杂计算字段
					upd_todo = append(upd_todo, name)
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
