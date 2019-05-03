package orm

import (
	"fmt"
	"strings"

	"volts-dev/dataset"

	"github.com/go-xorm/core"
	"github.com/volts-dev/utils"
)

type (
	TRelational struct {
		TField
	}

	TRelationalMultiField struct {
		TRelational
	}

	TMany2OneField struct {
		TRelational
	}

	TMany2ManyField struct {
		TRelationalMultiField
	}

	TOne2ManyField struct {
		TRelationalMultiField
	}

	TMany2ManyFieldCtrl struct {
	}
)

func init() {
	RegisterField("many2one", NewMany2OneField)
	RegisterField("many2many", NewMany2ManyField)
	RegisterField("one2many", NewOne2ManyField)
}

func NewMany2OneField() IField {
	return new(TMany2OneField)
}

// difine many2many(relate.model,ref.model,base_id,relate_id)
func NewMany2ManyField() IField {
	return new(TMany2ManyField)
}

func NewOne2ManyField() IField {
	return new(TOne2ManyField)
}

func (self *TRelational) GetAttributes(ctx *TFieldContext) map[string]interface{} {
	attrs := self.Base().GetAttributes(ctx)
	attrs["relation"] = self._attr_relation
	return attrs
}

func (self *TMany2ManyField) Init(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	fld.Base()._column_type = ""
	cnt := len(params)
	if cnt > 4 {
		fld.Base().comodel_name = utils.DotCasedName(utils.TitleCasedName(params[1]))  //目标表
		fld.Base().relmodel_name = utils.DotCasedName(utils.TitleCasedName(params[2])) //提供目标表格关系的表
		fld.Base().cokey_field_name = utils.SnakeCasedName(params[3])                  //目标表关键字段
		fld.Base().relkey_field_name = utils.SnakeCasedName(params[4])                 // 关系表关键字段
		fld.Base()._attr_relation = fld.Base().comodel_name
		fld.Base()._attr_type = "many2many"
	} else if cnt == 2 {
		model1 := utils.DotCasedName(utils.TitleCasedName(fld.ModelName())) // 字段归属的Model
		model2 := utils.DotCasedName(utils.TitleCasedName(params[1]))       // 字段链接的Model
		rel_model := fmt.Sprintf("%s_%s_rel", model1, model2)               // 表字段关系的Model
		fld.Base().comodel_name = model2                                    //目标表
		fld.Base().relmodel_name = rel_model                                //提供目标表格关系的表
		fld.Base().cokey_field_name = fmt.Sprintf("%s_id", model1)          //目标表关键字段
		fld.Base().relkey_field_name = fmt.Sprintf("%s_id", model2)         // 关系表关键字段
		fld.Base()._attr_relation = fld.Base().comodel_name
		fld.Base()._attr_type = "many2many"
	} else {
		logger.Panicf("Many2Many(%s) of model %s must including at least 4 args!", fld.Name(), self.model_name)
	}
	//logger.Dbg("M2M", fld.Base())
}

//model, columns
func (self *TMany2ManyField) UpdateDb(ctx *TFieldContext) {
	orm := ctx.Orm
	fld := ctx.Field
	rel := strings.Replace(fld.MiddleModelName(), ".", "_", -1)

	has, err := orm.IsTableExist(rel)
	if err != nil {
		logger.Errf("m2m check table %s failed:%s", ctx.Field.RelateModelName(), err.Error())
	}

	if !has {
		id1 := fld.RelateFieldName()
		id2 := fld.MiddleFieldName()
		query := fmt.Sprintf(`
	           CREATE TABLE "%s" ("%s" INTEGER NOT NULL,
	                                 "%s" INTEGER NOT NULL,
	                                 UNIQUE("%s","%s"));
	           COMMENT ON TABLE "%s" IS '%s';
	           CREATE INDEX ON "%s" ("%s");
	           CREATE INDEX ON "%s" ("%s")`,
			rel, id1,
			id2,
			id1, id2,
			rel, fmt.Sprintf("RELATION BETWEEN %s AND %s", self.ModelName(), rel),
			rel, id1,
			rel, id2)
		_, err := orm.Exec(query)
		if err != nil {
			logger.Errf("m2m create table '%s' failure : SQL:%s,Error:%s", ctx.Field.RelateModelName(), query, err.Error())
		}

		self.update_db_foreign_keys(ctx)
	}

	/*
	   cr = model._cr
	   # Do not reflect relations for custom fields, as they do not belong to a
	   # module. They are automatically removed when dropping the corresponding
	   # 'ir.model.field'.
	   if not self.manual:
	       model.pool.post_init(model.env['ir.model.relation']._reflect_relation,
	                            model, self.relation, self._module)
	   if not sql.table_exists(cr, self.relation):
	       comodel = model.env[self.comodel_name]
	       query = """
	           CREATE TABLE "{rel}" ("{id1}" INTEGER NOT NULL,
	                                 "{id2}" INTEGER NOT NULL,
	                                 UNIQUE("{id1}","{id2}"));
	           COMMENT ON TABLE "{rel}" IS %s;
	           CREATE INDEX ON "{rel}" ("{id1}");
	           CREATE INDEX ON "{rel}" ("{id2}")
	       """.format(rel=self.relation, id1=self.column1, id2=self.column2)
	       cr.execute(query, ['RELATION BETWEEN %s AND %s' % (model._table, comodel._table)])
	       _schema.debug("Create table %r: m2m relation between %r and %r", self.relation, model._table, comodel._table)
	       model.pool.post_init(self.update_db_foreign_keys, model)
	       return True
	*/
}

//Add the foreign keys corresponding to the field's relation table.
func (self *TMany2ManyField) update_db_foreign_keys(ctx *TFieldContext) {
	/*        cr = model._cr
	          comodel = model.env[self.comodel_name]
	          reflect = model.env['ir.model.constraint']._reflect_constraint
	          # create foreign key references with ondelete=cascade, unless the targets are SQL views
	          if sql.table_kind(cr, model._table) != 'v':
	              sql.add_foreign_key(cr, self.relation, self.column1, model._table, 'id', 'cascade')
	              reflect(model, '%s_%s_fkey' % (self.relation, self.column1), 'f', None, self._module)
	          if sql.table_kind(cr, comodel._table) != 'v':
	              sql.add_foreign_key(cr, self.relation, self.column2, comodel._table, 'id', 'cascade')
	              reflect(model, '%s_%s_fkey' % (self.relation, self.column2), 'f', None, self._module)
	*/
}

// 设置字段获得的值
// TODO :未完成
func (self *TMany2ManyField) OnConvertToRead(ctx *TFieldEventContext) interface{} {
	session := ctx.Session
	field := ctx.Field.Base()
	id := ctx.Id

	// TODO 检测字段应该在注册MODEL时完成
	// 检测关联Model合法性
	if !ctx.Session.Orm().HasModel(field.comodel_name) || !ctx.Session.Orm().HasModel(field.comodel_name) {
		return ctx.Value
	}

	// TODO　model 规范命名方式
	cotable_name := strings.Replace(field.comodel_name, ".", "_", -1)   //# 字段关联表名
	reltable_name := strings.Replace(field.relmodel_name, ".", "_", -1) //# 字段M2m关系表名

	domain := Query2StringList(field.Domain())
	sess := session.Orm().NewSession()
	defer sess.Close()

	//table_name := field.comodel_name//sess.Statement.TableName()
	sess.Model(field.Base().comodel_name)
	wquery := sess.Statement._where_calc(domain, false, nil)
	order_by := sess.Statement._generate_order_by(wquery, nil)
	from_c, where_c, where_params := wquery.get_sql()
	if where_c == "" {
		where_c = "1=1"
	}

	limit := ""
	if field.limit > 0 {
		limit = fmt.Sprintf("LIMIT %v", field.limit)
	}

	//Many2many('res.lang', 'website_lang_rel', 'website_id', 'lang_id')
	//SELECT {rel}.{id1}, {rel}.{id2} FROM {rel}, {from_c} WHERE {where_c} AND {rel}.{id1} IN %s AND {rel}.{id2} = {tbl}.id {order_by} {limit} OFFSET {offset}
	query := fmt.Sprintf(`SELECT %s.%s, %s.%s FROM %s, %s
                    WHERE %s AND %s.%s IN (?) AND %s.%s = %s.id
                    %s %s OFFSET %d`,
		reltable_name, field.cokey_field_name, reltable_name, field.relkey_field_name, reltable_name, from_c,
		where_c, reltable_name, field.cokey_field_name, reltable_name, field.relkey_field_name, cotable_name,
		order_by, limit, 0,
	)

	// # 添加 IDs 作为参数
	where_params = append(where_params, id)

	var res_ds *dataset.TDataSet
	var less_ids []string
	cacher_table_name := field.Relation() + "_" + from_c

	// # 获取字段关联表的字符
	ids := ctx.Session.Orm().cacher.GetBySql(cacher_table_name, query, where_params)
	if len(ids) > 0 {
		// # 查询 field.Relation 表数据
		records, less := session.Orm().cacher.GetByIds(cacher_table_name, ids...)
		if len(less) == 0 {
			res_ds = dataset.NewDataSet()
			res_ds.AppendRecord(records...)
		} else {
			less_ids = less
		}
	}

	// # 如果缺省缓存记录重新查询  TODO 只查询缺的记录不查询所有
	if ids == nil || len(less_ids) > 0 {
		ds, err := sess.Query(query, where_params...)
		if err != nil {
			logger.Errf(err.Error())
			return nil
		}

		// # store result in cache
		session.Orm().cacher.PutBySql(cacher_table_name, query, where_params, ds.Keys()...) // # 添加Sql查询结果
		for _, id := range ds.Keys() {
			session.Orm().cacher.PutById(cacher_table_name, id, ds.RecordByKey(id)) // # 添加记录缓存
		}

		res_ds = ds
	}

	//group := make(map[interface{}][]int64)
	list := make([]int64, res_ds.Count())
	res_ds.First()
	for !res_ds.Eof() {
		//key := res_ds.FieldByName(field.cokey_field_name).AsInteger()

		//list := group[key]
		//list = append(list, res_ds.FieldByName(field.relkey_field_name).AsInteger())
		//group[key] = list
		list[res_ds.Position] = res_ds.FieldByName(field.relkey_field_name).AsInteger()
		res_ds.Next()
	}
	/*
		DataSet := ctx.Dataset
		// # 保存到值
		for _, id := range DataSet.Keys() {
			logger.Dbg("ttttttttt", field.Name(), DataSet.RecordByKey(id).GetByName(field.Name()), DataSet.Fields)
			DataSet.RecordByKey(id).GetByName(field.Name()).AsInterface(group[id])
		}
	*/
	return list // group //DataSet.FieldByName(field.Name()).AsInterface()
}

// # beware of duplicates when inserting
func (self *TMany2ManyField) link(ids []string, ctx *TFieldEventContext) {
	orm := ctx.Session.Orm()
	field := ctx.Field
	rec_id := ctx.Id // 字段所在记录的ID
	session := orm.NewSession()
	session.Begin()

	middle_table_name := strings.Replace(field.MiddleModelName(), ".", "_", -1)
	for _, relate_id := range ids {
		query := fmt.Sprintf(`INSERT INTO %s (%s, %s)
                VALUES (%s,%s)
                ON CONFLICT DO NOTHING`,
			middle_table_name, field.RelateFieldName(), field.MiddleFieldName(),
			relate_id, rec_id,
		)

		/*
		   	query := fmt.Sprintf(`INSERT INTO %s (%s, %s)
		                           (SELECT a, b FROM unnest(array[%s]) AS a, unnest(array[%s]) AS b)
		                           EXCEPT (SELECT %s, %s FROM %s WHERE %s IN (%s))`,
		   		middle_table_name, field.RelateFieldName(), field.MiddleFieldName(),
		   		rec_id, strings.Join(ids, ","),
		   		field.RelateFieldName(), field.MiddleFieldName(), middle_table_name, field.RelateFieldName(), rec_id,
		   	)
		*/
		_, err := session.Exec(query)
		if err != nil {
			session.Rollback()
			logger.Err(err)
		}
	}

	err := session.Commit()
	if err != nil {
		session.Rollback()
		logger.Err(err)
	}
	session.Close()
}

//# remove all records for which user has access rights
func (self *TMany2ManyField) unlink_all(ids []string, ctx *TFieldEventContext) {
	orm := ctx.Session.Orm()
	field := ctx.Field
	model_name := strings.Replace(field.ModelName(), ".", "_", -1)
	middle_table_name := strings.Replace(field.MiddleModelName(), ".", "_", -1)
	query := fmt.Sprintf(`DELETE FROM %s
                        WHERE %s.%s IN (?) AND %s.%s=%s.id`,
		middle_table_name,
		middle_table_name, field.RelateFieldName(),
		middle_table_name, field.MiddleFieldName(),
		model_name,
	)

	orm.Exec(query, strings.Join(ids, ","))
}

// write relate data to the reference table
func (self *TMany2ManyField) OnConvertToWrite(ctx *TFieldEventContext) interface{} {
	if values, ok := ctx.Value.([]int64); ok {
		if len(values) > 0 {
			ids := utils.IntsToStrs(values)

			// TODO 读比写快 不删除原有数据 直接读取并对比再添加
			// unlink all the relate record on the ref. table
			self.unlink_all(ids, ctx)

			// relink record from new data
			self.link(ids, ctx)
		}
	}
	return nil
}
func (self *TMany2OneField) Init(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field
	params := ctx.Params

	col.SQLType = core.SQLType{core.Int, 0, 0}
	fld.Base()._column_type = core.Int
	// 不直接指定 采用以下tag写法
	// field:"many2one() int()"
	//col.SQLType = core.Type2SQLType(lFieldType)
	//lField.initMany2One(lTag[1:]...)	fld._classic_read = true // 预先设计是false
	//fld.Base()._classic_write = true
	logger.Assert(len(params) > 1, "Many2One(%s) of model %s must including at least 1 args!", fld.Name(), self.model_name)
	fld.Base().comodel_name = utils.DotCasedName(utils.TitleCasedName(params[1])) //目标表
	fld.Base()._attr_relation = fld.Base().comodel_name
	fld.Base()._attr_type = "many2one" //core.Int
}

func (self *TMany2OneField) OnConvertToRead(ctx *TFieldEventContext) interface{} {
	lId := utils.Itf2Int(ctx.Value)
	if ctx.Session.IsClassic && lId > 0 {
		//# evaluate name_get() as superuser, because the visibility of a
		//# many2one field value (id and name) depends on the current record's
		//# access rights, and not the value's access rights.
		//   value_sudo = value.sudo()
		//# performance trick: make sure that all records of the same
		//# model as value in value.env will be prefetched in value_sudo.env
		// value_sudo.env.prefetch[value._name].update(value.env.prefetch[value._name])
		lModel, err := ctx.Session.Orm().osv.GetModel(self.RelateModelName())
		if err != nil {
			// # Should not happen, unless the foreign key is missing.
			logger.Err(err)
		} else {
			//logger.Dbg("CTR:", ctx.Field.Name(), ctx.Value != BlankNumItf, ctx.Value != interface{}('0'), lModel, ctx.Value, lId)
			ds := lModel.NameGet([]string{utils.IntToStr(lId)})
			return []string{ds.FieldByName("id").AsString(), ds.FieldByName("name").AsString()}
		}
	}

	return ctx.Value
}
func (self *TMany2OneField) OnConvertToWrite(ctx *TFieldEventContext) interface{} {
	switch ctx.Value.(type) {
	case []string:
		if lst, ok := ctx.Value.([]string); ok && len(lst) > 0 {
			return lst[0]
		}
	default:
		logger.Warnf("%s convert_to_write many2one fail", ctx.Field.Name())
	}

	return ctx.Value
}

func (self *TOne2ManyField) Init(ctx *TFieldContext) { //comodel_name string, inverse_name string
	Field := ctx.Field
	Params := ctx.Params
	self.Base()._column_type = ""
	//Field.Base()._classic_read = false
	//Field.Base()._classic_write = false
	logger.Assert(len(Params) > 2, "One2Many(%s) of model %s must including at least 1 args!", Field.Name(), self.model_name)

	Field.Base().comodel_name = utils.DotCasedName(utils.TitleCasedName(Params[1])) //目标表
	Field.Base().cokey_field_name = utils.SnakeCasedName(Params[2])                 //目标表关键字段
	Field.Base()._attr_relation = Field.Base().comodel_name
	Field.Base()._attr_type = "one2many"
}
