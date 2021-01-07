package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/orm/logger"

	"github.com/volts-dev/utils"
)

type (
	TRelational struct {
		TField
	}

	TRelationalMultiField struct {
		TRelational
	}

	// 主表[字段所在的表]字段值是关联表其中之一条记录,关联表字段相当于主表或其他表的补充扩展或共同字段
	// 特性：存储,外键在主表,主表类似于继承了关联表的多有字段
	// 例子：合作伙伴里有个人和公司,他们都有名称,联系方式,地址等共同信息 这些信息可以又关联表存储
	TOne2OneField struct {
		TRelational
	}

	// 主表[字段所在的表]字段值是关联表其中多条记录的集,关联表记录可以赋值给主表多条记录
	// 特性：不存储,外键在关联表,关联表有XX_id以表示记录归主表那条记录绑定
	// 例子：订单系统的主从表
	TOne2ManyField struct {
		TRelational
	}

	// 主表[字段所在的表]字段值是关联表其中之一条记录,关联表记录可以赋值给主表多条记录
	// 特性：存储,外键在主表,值只有一个
	// 例子：下拉选择菜单,性别
	TMany2OneField struct {
		TRelational
	}

	// 字段值是中间表中绑定的多条关联表记录集(多条记录)
	TMany2ManyField struct {
		TRelationalMultiField
	}

	TMany2ManyFieldCtrl struct {
	}
)

func init() {
	RegisterField("one2one", newOne2OneField)
	RegisterField("many2one", newMany2OneField)
	RegisterField("many2many", newMany2ManyField)
	RegisterField("one2many", newOne2ManyField)
}

func newOne2OneField() IField {
	return new(TOne2OneField)
}

func newMany2OneField() IField {
	return new(TMany2OneField)
}

// difine many2many(relate.model,ref.model,base_id,relate_id)
func newMany2ManyField() IField {
	return new(TMany2ManyField)
}

func newOne2ManyField() IField {
	return new(TOne2ManyField)
}

func (self *TRelational) GetAttributes(ctx *TFieldContext) map[string]interface{} {
	attrs := self.Base().GetAttributes(ctx)
	attrs["relation"] = self._attr_relation
	return attrs
}

func (self *TOne2OneField) Init(ctx *TFieldContext) { //comodel_name string, inverse_name string
	field_Value := ctx.FieldTypeValue
	field := ctx.Field
	field.Base().SqlType = Type2SQLType(field_Value.Type())
	field.Base()._attr_store = true
	field.Base()._attr_type = TYPE_O2O
	params := ctx.Params
	if len(params) > 0 {
		field.Base().comodel_name = params[0]
		field.Base()._attr_relation = params[0]
	}
}

func (self *TOne2OneField) OnRead(ctx *TFieldEventContext) error {
	model, err := ctx.Session.Orm().osv.GetModel(self.RelateModelName())
	if err != nil {
		// # Should not happen, unless the foreign key is missing.
		return err
	}

	ds := ctx.Dataset
	if ds != nil {
		ds.First()
		for !ds.Eof() {
			// 获取关联表主键
			rel_id := ds.FieldByName(self.Name()).AsInterface()

			// igonre blank value
			if utils.IsBlank(rel_id) {
				if self._attr_required {
					return fmt.Errorf("the Many2One field %s:%s is required!", self.model_name, self.Name())
				}

				ds.Next()
				continue
			}

			//logger.Dbg("CTR:", ctx.Field.Name(), ctx.Value != BlankNumItf, ctx.Value != interface{}('0'), model, ctx.Value, lId)
			rel_ds, err := model.NameGet([]interface{}{rel_id})
			if err != nil {
				return err
			}

			ds.FieldByName(self.Name()).AsInterface([]interface{}{rel_ds.FieldByName(model.IdField()).AsInterface(), rel_ds.FieldByName(model.GetRecordName()).AsInterface()})
			ds.Next()
		}
	}

	return nil
}

func (self *TMany2ManyField) Init(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	//	fld.Base()._column_type = "" //* not a store field
	fld.Base()._attr_store = false
	cnt := len(params)
	if cnt == 3 {
		model1 := fmtModelName(utils.TitleCasedName(fld.ModelName())) // 字段归属的Model
		model2 := fmtModelName(utils.TitleCasedName(params[0]))       // 字段链接的Model
		rel_model := fmt.Sprintf("%s_%s_rel", model1, model2)         // 表字段关系的Model
		fld.Base().comodel_name = model2                              //目标表
		fld.Base().relmodel_name = rel_model                          //提供目标表格关系的表
		fld.Base().cokey_field_name = fmtFieldName(params[1])         //目标表关键字段
		fld.Base().relkey_field_name = fmtFieldName(params[2])        // 关系表关键字段
		fld.Base()._attr_relation = fld.Base().comodel_name
		fld.Base()._attr_type = TYPE_M2M

	} else if cnt == 1 {
		model1 := fmtModelName(utils.TitleCasedName(fld.ModelName())) // 字段归属的Model
		model2 := fmtModelName(utils.TitleCasedName(params[0]))       // 字段链接的Model
		rel_model := fmt.Sprintf("%s_%s_rel", model1, model2)         // 表字段关系的Model
		fld.Base().comodel_name = model2                              //目标表
		fld.Base().relmodel_name = rel_model                          //提供目标表格关系的表
		fld.Base().cokey_field_name = fmt.Sprintf("%s_id", model1)    //目标表关键字段
		fld.Base().relkey_field_name = fmt.Sprintf("%s_id", model2)   // 关系表关键字段
		fld.Base()._attr_relation = fld.Base().comodel_name
		fld.Base()._attr_type = "many2many"

	} else {
		logger.Panicf("field %s of model %s must format like 'Many2Many(relate_model)' or 'Many2Many(relate_model,model_id,relate_model_id)'!", fld.Name(), self.model_name)
	}
}

// 创建关联表
//model, columns
func (self *TMany2ManyField) UpdateDb(ctx *TFieldContext) {
	orm := ctx.Orm
	fld := ctx.Field
	model := ctx.Model
	rel := strings.Replace(fld.MiddleModelName(), ".", "_", -1)

	has, err := orm.IsTableExist(rel)
	if err != nil {
		logger.Errf("m2m check table %s failed:%s", ctx.Field.RelateModelName(), err.Error())
	}

	if !has {
		field := model.GetFieldByName(model.IdField())
		sqlType := field.SQLType().Name
		id1 := fld.RelateFieldName()
		id2 := fld.MiddleFieldName()
		query := fmt.Sprintf(`
	           CREATE TABLE "%s" (
				"%s" %s NOT NULL,
				"%s" %s NOT NULL,UNIQUE("%s","%s"));
	           COMMENT ON TABLE "%s" IS '%s';
	           CREATE INDEX ON "%s" ("%s");
	           CREATE INDEX ON "%s" ("%s")`,
			rel,
			id1, sqlType,
			id2, sqlType, id1, id2,
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
func (self *TMany2ManyField) OnRead(ctx *TFieldEventContext) error {
	session := ctx.Session
	field := ctx.Field.Base()
	ds := ctx.Dataset
	model := ctx.Model
	id_field := model.IdField()

	// TODO 检测字段应该在注册MODEL时完成
	// 检测关联Model合法性
	if !ctx.Session.Orm().HasModel(field.comodel_name) || !ctx.Session.Orm().HasModel(field.comodel_name) {
		return nil
	}

	// TODO　model 规范命名方式
	cotable_name := field.comodel_name   //# 字段关联表名
	reltable_name := field.relmodel_name //# 字段M2m关系表名

	node, err := domain.String2Domain(field.Domain())
	if err != nil {
		return err
	}
	sess := session.Orm().NewSession()
	defer sess.Close()

	//table_name := field.comodel_name//sess.Statement.TableName()
	sess.Model(field.Base().comodel_name)
	wquery, err := sess.Statement.where_calc(node, false, nil)
	if err != nil {
		return err
	}
	order_by := sess.Statement.generate_order_by(wquery, nil)
	from_c, where_c, where_params := wquery.get_sql()
	if where_c == "" {
		where_c = "1=1"
	}

	limit := ""
	if field.limit > 0 {
		limit = fmt.Sprintf("LIMIT %v", field.limit)
	}

	offset := ""

	// the table name in cacher
	cacher_table_name := field.Relation() + "_" + from_c

	//Many2many('res.lang', 'website_lang_rel', 'website_id', 'lang_id')
	//SELECT {rel}.{id1}, {rel}.{id2} FROM {rel}, {from_c} WHERE {where_c} AND {rel}.{id1} IN %s AND {rel}.{id2} = {tbl}.id {order_by} {limit} OFFSET {offset}
	query := fmt.Sprintf(
		`SELECT %s.%s, %s.%s FROM %s, %s WHERE %s AND %s.%s IN (?) AND %s.%s = %s.id %s %s %s`,
		reltable_name, field.cokey_field_name, reltable_name, field.relkey_field_name, reltable_name, from_c,
		where_c, reltable_name, field.cokey_field_name, reltable_name, field.relkey_field_name, cotable_name,
		order_by, limit, offset,
	)

	var res_ds *dataset.TDataSet

	ds.First()
	for !ds.Eof() {
		id := ds.FieldByName(id_field).AsInterface()

		// # 添加 IDs 作为参数
		params := append(where_params, id)

		// # 获取字段关联表的字符
		res_ds = ctx.Session.Orm().Cacher.GetBySql(cacher_table_name, query, params)
		if res_ds == nil {
			// TODO 只查询缺的记录不查询所有
			// # 如果缺省缓存记录重新查询

			ds, err := sess.Query(query, params...)
			if err != nil {
				logger.Err(err)
				ds.Next()
				continue
			}

			// # store result in cache
			session.Orm().Cacher.PutBySql(cacher_table_name, query, where_params, ds) // # 添加Sql查询结果

			res_ds = ds
		}

		//group := make(map[interface{}][]int64)
		list := make([]interface{}, res_ds.Count())
		res_ds.First()
		for !res_ds.Eof() {
			//list := group[key]
			//list = append(list, res_ds.FieldByName(field.relkey_field_name).AsInteger())
			//group[key] = list
			list[res_ds.Position] = res_ds.FieldByName(field.relkey_field_name).AsInterface()
			res_ds.Next()
		}

		ds.FieldByName(field.Name()).AsInterface(list) // 修改数据集
		ds.Next()
	}

	return nil
}

// # beware of duplicates when inserting
func (self *TMany2ManyField) link(ids []interface{}, ctx *TFieldEventContext) error {
	orm := ctx.Session.Orm()
	dialect := ctx.Session.Orm().dialect
	field := ctx.Field
	rec_id := ctx.Id // 字段所在记录的ID
	session := orm.NewSession()
	{
		session.Begin()

		middle_table_name := dialect.Quote(strings.Replace(field.MiddleModelName(), ".", "_", -1))
		for _, relate_id := range ids {
			query := fmt.Sprintf(
				`INSERT INTO %v (%s, %s) VALUES (?,?) ON CONFLICT DO NOTHING`,
				middle_table_name, dialect.Quote(field.RelateFieldName()), dialect.Quote(field.MiddleFieldName()),
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
			_, err := session.Exec(query, relate_id, rec_id)
			if err != nil {
				session.Rollback()
				return err
			}
		}

		err := session.Commit()
		if err != nil {
			session.Rollback()
			return err
		}
	}
	session.Close()
	return nil
}

// TODO 错误将IDS删除基数
//# remove all records for which user has access rights
func (self *TMany2ManyField) unlink_all(ids []interface{}, ctx *TFieldEventContext) error {
	orm := ctx.Session.Orm()
	dialect := ctx.Session.Orm().dialect
	field := ctx.Field
	//	model := ctx.Model
	//	model_name := field.ModelName()
	middle_table_name := dialect.Quote(strings.Replace(field.MiddleModelName(), ".", "_", -1))
	in_sql := strings.Repeat("?,", len(ids)-1) + "?"
	query := fmt.Sprintf(`DELETE FROM %s WHERE %s.%s IN (%s) AND %s.%s= ?`,
		middle_table_name,
		middle_table_name, dialect.Quote(field.RelateFieldName()), // Where
		in_sql,                                                    // In
		middle_table_name, dialect.Quote(field.MiddleFieldName()), // And
		//model_name, model.IdField(),
	)

	arg := append(ids, ctx.Id)

	// 提交修改
	session := orm.NewSession()
	{
		session.Begin()
		_, err := session.Exec(query, arg...)
		if err != nil {
			session.Rollback()
			return err
		}

		session.Commit()
		if err != nil {
			session.Rollback()
			return err
		}

	}
	session.Close()
	return nil
}

// write relate data to the reference table
func (self *TMany2ManyField) OnWrite(ctx *TFieldEventContext) error {
	ids := make([]interface{}, 0)

	// TODO　更多类型
	// 支持一下几种M2M数据类型
	switch ctx.Value.(type) {
	case []int:
		for _, v := range ctx.Value.([]int) {
			ids = append(ids, v)
		}
	case []int64:
		for _, v := range ctx.Value.([]int64) {
			ids = append(ids, v)
		}
	case []interface{}:
		ids = ctx.Value.([]interface{})
	default:
		logger.Errf("M2M field name <%s> could not support this type of value %v", ctx.Field.Name(), ctx.Value)
	}

	if len(ids) > 0 {
		/*		query := fmt.Sprintf(
						`SELECT {rel}.{id1}, {rel}.{id2} FROM {tables} WHERE {rel}.{id1} IN %s AND {rel}.{id2}={table}.id AND {cond}`,
						middle_table_name,field.RelateFieldName(),// select
						middle_table_name, field.MiddleFieldName(),
						middle_table_name,
								middle_table_name,field.RelateFieldName(),

						in_sql,                                     // In
						middle_table_name, field.MiddleFieldName(), // And
						model_name, model.IdField(),
					)
						        query = `
				            SELECT {rel}.{id1}, {rel}.{id2} FROM {tables}
				            WHERE {rel}.{id1} IN %s AND {rel}.{id2}={table}.id AND {cond}
				        `.format(
				            rel=self.relation, id1=self.column1, id2=self.column2,
				            table=comodel._table, tables=",".join(tables),
				            cond=" AND ".join(clauses) if clauses else "1=1",
				        )
		*/
		// TODO 读比写快 不删除原有数据 直接读取并对比再添加
		// unlink all the relate record on the ref. table
		err := self.unlink_all(ids, ctx)
		if err != nil {
			return err
		}

		// relink record from new data
		err = self.link(ids, ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

func (self *TMany2OneField) Init(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	// 不直接指定 采用以下tag写法
	// field:"many2one() int()"
	//lField.initMany2One(lTag[1:]...)	fld._classic_read = true // 预先设计是false
	//fld.Base()._classic_write = true
	logger.Assert(len(params) > 0, "Many2One(%s) of model %s must including at least 1 args!", fld.Name(), self.model_name)
	fld.Base().comodel_name = fmtModelName(utils.TitleCasedName(params[0])) //目标表
	fld.Base()._attr_relation = fld.Base().comodel_name
	fld.Base()._attr_type = TYPE_M2O
}

func (self *TMany2OneField) OnRead(ctx *TFieldEventContext) error {
	model, err := ctx.Session.Orm().osv.GetModel(self.RelateModelName())
	if err != nil {
		// # Should not happen, unless the foreign key is missing.
		return err
	}

	ds := ctx.Dataset
	if ctx.Session.IsClassic && ds != nil {
		//# evaluate name_get() as superuser, because the visibility of a
		//# many2one field value (id and name) depends on the current record's
		//# access rights, and not the value's access rights.
		//   value_sudo = value.sudo()
		//# performance trick: make sure that all records of the same
		//# model as value in value.env will be prefetched in value_sudo.env
		// value_sudo.env.prefetch[value._name].update(value.env.prefetch[value._name])
		ds.First()
		for !ds.Eof() {
			// 获取关联表主键
			rel_id := ds.FieldByName(self.Name()).AsInterface()

			// igonre blank value
			if utils.IsBlank(rel_id) {
				if self._attr_required {
					return fmt.Errorf("the Many2One field %s:%s is required!", self.model_name, self.Name())
				}

				ds.Next()
				continue
			}

			//logger.Dbg("CTR:", ctx.Field.Name(), ctx.Value != BlankNumItf, ctx.Value != interface{}('0'), model, ctx.Value, lId)
			rel_ds, err := model.NameGet([]interface{}{rel_id})
			if err != nil {
				return err
			}

			id_field := model.IdField() // get the id field name
			ds.FieldByName(self.Name()).AsInterface([]interface{}{rel_ds.FieldByName(id_field).AsInterface(), rel_ds.FieldByName("name").AsInterface()})

			ds.Next()
		}
	}

	return nil
}

func (self *TMany2OneField) OnWrite(ctx *TFieldEventContext) error {
	field := ctx.Field

	switch ctx.Value.(type) {
	case []interface{}:
		if lst, ok := ctx.Value.([]interface{}); ok && len(lst) > 0 {
			ctx.Value = lst[0]
		}

	default:
		logger.Errf("%s OnWrite many2one fail", field.Name())

	}

	return nil
}

func (self *TOne2ManyField) Init(ctx *TFieldContext) { //comodel_name string, inverse_name string
	field := ctx.Field
	params := ctx.Params
	//	self.Base()._column_type = ""
	field.Base()._attr_store = false

	//Field.Base()._classic_read = false
	//Field.Base()._classic_write = false
	logger.Assert(len(params) > 1, "One2Many(%s) of model %s must including at least 2 args!", field.Name(), self.model_name)

	field.Base().comodel_name = fmtModelName(utils.TitleCasedName(params[0])) //目标表
	field.Base().cokey_field_name = fmtFieldName(params[1])                   //目标表关键字段
	field.Base()._attr_relation = field.Base().comodel_name
	field.Base()._attr_type = TYPE_O2M
}

func (self *TOne2ManyField) OnRead(ctx *TFieldEventContext) error {
	//	orm := ctx.Session.orm
	ds := ctx.Dataset
	field := ctx.Field
	//idFieldName := ctx.Model.IdField()
	model := ctx.Model
	/*
		// # retrieve the lines in the comodel
		relmodel_name := self.relmodel_name
		relkey_field_name := self.relkey_field_name
		rel_model, err := orm.GetModel(relmodel_name)
		if err != nil {
			return err
		}

		rel_filed := rel_model.GetFieldByName(relkey_field_name)
		if rel_filed.SQLType().Name != TYPE_O2M {
			return logger.Errf("the relate model %s field % is not many2one type.", relmodel_name, relkey_field_name)
		}
	*/
	ids := ds.Keys()
	sds, err := model.One2many(ids, field.Name()) // rel_model.Records().In(field.Name(), ids).Read()
	if err != nil {
		logger.Errf("One2Many field %s search relate model %s faild", field.Name(), field.RelateModelName())
		return err
	}

	tmap := make(map[interface{}][]*dataset.TRecordSet)

	// 根据id 分配记录
	sds.First()
	for !sds.Eof() {
		id := sds.FieldByName(field.RelateFieldName()).AsInterface()
		recs := tmap[id] // 记录数组
		recs = append(recs, sds.Record())
		sds.Next()
	}

	// 把分配的记录存回记录集
	for _, n := range ids {
		recs := tmap[n] // 记录数组
		rec := ds.RecordByKey(n)
		rec.FieldByName(field.Name()).AsInterface(recs)
	}

	return nil
	/*   comodel = records.env[self.comodel_name].with_context(**self.context)
	     inverse = self.inverse_name
	     get_id = (lambda rec: rec.id) if comodel._fields[inverse].type == 'many2one' else int
	     domain = self.domain(records) if callable(self.domain) else self.domain
	     domain = domain + [(inverse, 'in', records.ids)]
	     lines = comodel.search(domain, limit=self.limit)

	     # group lines by inverse field (without prefetching other fields)
	     group = defaultdict(list)
	     for line in lines.with_context(prefetch_fields=False):
	         # line[inverse] may be a record or an integer
	         group[get_id(line[inverse])].append(line.id)

	     # store result in cache
	     cache = records.env.cache
	     for record in records:
	         cache.set(record, self, tuple(group[record.id]))
	*/
}

func (self *TOne2ManyField) _OnWrite(ctx *TFieldEventContext) error {
	/* comodel = records.env[self.comodel_name].with_context(**self.context)
	   inverse = self.inverse_name
	   vals_list = []                  # vals for lines to create in batch

	   def flush():
	       if vals_list:
	           comodel.create(vals_list)
	           vals_list.clear()

	   def drop(lines):
	       if getattr(comodel._fields[inverse], 'ondelete', False) == 'cascade':
	           lines.unlink()
	       else:
	           lines.write({inverse: False})

	   with records.env.norecompute():
	       for act in (value or []):
	           if act[0] == 0:
	               for record in records:
	                   vals_list.append(dict(act[2], **{inverse: record.id}))
	           elif act[0] == 1:
	               comodel.browse(act[1]).write(act[2])
	           elif act[0] == 2:
	               comodel.browse(act[1]).unlink()
	           elif act[0] == 3:
	               drop(comodel.browse(act[1]))
	           elif act[0] == 4:
	               record = records[-1]
	               line = comodel.browse(act[1])
	               line_sudo = line.sudo().with_context(prefetch_fields=False)
	               if int(line_sudo[inverse]) != record.id:
	                   line.write({inverse: record.id})
	           elif act[0] == 5:
	               flush()
	               domain = self.domain(records) if callable(self.domain) else self.domain
	               domain = domain + [(inverse, 'in', records.ids)]
	               drop(comodel.search(domain))
	           elif act[0] == 6:
	               flush()
	               record = records[-1]
	               comodel.browse(act[2]).write({inverse: record.id})
	               domain = self.domain(records) if callable(self.domain) else self.domain
	               domain = domain + [(inverse, 'in', records.ids), ('id', 'not in', act[2] or [0])]
	               drop(comodel.search(domain))

	       flush()
	*/
	return nil
}
