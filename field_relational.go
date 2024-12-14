package orm

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

type (
	TRelational struct {
		TField
	}

	TRelationalMultiField struct {
		TRelational
	}

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
	// 特性：存储,外键在主表,值只有一个,many child -> one parent 用于指定ParentID 表示本表的多条记录是关联表的某条记录的Child
	// 例子：订单系统的主从表 从表下拉选择菜单,性别
	TMany2OneField struct {
		TRelational
	}

	TMany2ManyField struct {
		TRelationalMultiField
	}
)

func init() {
	RegisterField("one2one", newOne2OneField)
	RegisterField("one2many", newOne2ManyField)
	RegisterField("many2one", newMany2OneField)
	RegisterField("many2many", newMany2ManyField)
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

func (self *TRelational) GetAttributes(ctx *TTagContext) map[string]interface{} {
	attrs := self.Base().GetAttributes(ctx)
	attrs["relation"] = self.related_model_name
	return attrs
}

func (self *TOne2OneField) Init(ctx *TTagContext) { //related_model_name string, inverse_name string
	field_Value := ctx.FieldTypeValue
	field := ctx.Field.Base()
	field.isRelatedField = true

	field._attr_store = true
	field._attr_type = TYPE_O2O
	params := ctx.Params

	var modelName string
	if len(params) > 0 {
		modelName = fmtModelName(utils.TitleCasedName(params[0]))
		field.related_model_name = params[0]
		field._attr_relation = params[0]
	}

	// 现在成员名是关联的Model名,Tag 为关联的字段
	model := ctx.Model
	model.Obj().SetRelationByName(modelName, field.Name())

	parentModel, err := ctx.Orm.GetModel(modelName)
	if err != nil || parentModel == nil {
		log.Fatalf("field One2One %s@%s must including model name %s registered in orm!", field.Name(), model.String(), modelName)
	}

	if field_Value.IsValid() {
		field.SqlType = GoType2SQLType(field_Value.Type())
	} else {
		/* 保持和ID一致的类型 */
		idfield := parentModel.GetFieldByName(parentModel.IdField())
		if idfield == nil {
			panic("id field not found")
		}

		field.SqlType = *idfield.SQLType()
	}

	var (
		parentField, newField IField
		fieldName             string
	)
	for _, parentField = range parentModel.GetFields() {
		// #限制某些字段
		// @ 当参数多余1个时判断为限制字段　例如：`field:"relate(PartnerId,Name)"`
		//if lRelFieldsCnt > 1 && utils.InStrings(parentField.Name(), lRelFields...) == -1 {
		//	continue
		//}
		fieldName = parentField.Name()
		newField = utils.Clone(parentField).(IField) // 复制关联字段
		newField.SetBase(parentField.Base())

		if f := model.GetFieldByName(fieldName); f != nil {
			// 相同字段处理
			model.GetBase().obj.SetCommonFieldByName(fieldName, parentModel.String(), newField)
			model.GetBase().obj.SetCommonFieldByName(fieldName, f.Base().model_name, f)

		} else {
			// # 当Tag为Extends,Inherits时,该结构体所有合法字段将被用于创建数据库表字段
			newField.Base().isInheritedField = true
			newField.Base()._attr_store = false // 关系字段不存储

			if newField.IsAutoIncrement() {
				//model.GetBase().table.AutoIncrement = fieldName
				model.Obj().AutoIncrementField = fieldName
			}

			//# 映射时是没有Parent的字段如Id 所以在此获取Id主键.
			if newField.Base().isPrimaryKey && newField.Base().isAutoIncrement {
				model.GetBase().idField = fieldName
			}
			model.GetBase().obj.SetFieldByName(fieldName, newField)
		}
	}
}

func (self *TOne2OneField) OnRead(ctx *TFieldContext) error {
	field := ctx.Field
	if !field.IsRelatedField() {
		return fmt.Errorf("the field %s must related field, but not %s!", field.Name(), field.Type())
	}

	ds, err := ctx.Model.OneToOne(ctx)
	if err != nil {
		return err
	}

	if ds.Count() > 0 {
		field := ctx.Field

		relateModel, err := ctx.Model.Orm().GetModel(field.RelatedModelName())
		if err != nil {
			// # Should not happen, unless the foreign key is missing.
			return err
		}

		//group := ds.GroupBy(field.RelatedFieldName())
		group := ds.GroupBy(relateModel.IdField())
		ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
			// 获取关联表主键
			//fieldValue := record.GetByField(field.Name())
			fieldValue := record.GetByField(field.RelatedFieldName())
			grp := group[fieldValue]

			if grp.Count() > 1 {
				return fmt.Errorf(
					"model %s's has more than 1 record for %s@%s OneToOne Id %v",
					field.RelatedModelName(), field.Name(), field.ModelName(), grp.Keys())
			}

			//record.SetByField(field.Name(), grp.Record().AsItfMap())
			for _, f := range grp.Record().Fields() {
				if !record.FieldByName(f).IsValid {
					record.SetByField(f, grp.Record().GetByField(f))
				}
			}

			return nil
		})
	}

	return nil
}

func (self *TOne2ManyField) Init(ctx *TTagContext) { //related_model_name string, inverse_name string
	field := ctx.Field
	params := ctx.Params

	log.Assert(len(params) < 2, "One2Many(%s) of model %s must including at least 2 args!", field.Name(), self.model_name)
	// self.Base()._column_type = ""
	// Field.Base()._classic_read = false
	// Field.Base()._classic_write = false
	field.Base().isRelatedField = true
	field.Base()._attr_store = false
	field.Base().related_model_name = fmtModelName(utils.TitleCasedName(params[0])) //目标表
	field.Base().related_keyfield_name = fmtFieldName(params[1])                    //目标表关键字段
	field.Base()._attr_relation = field.Base().related_model_name
	field.Base()._attr_type = TYPE_O2M
}

func (self *TOne2ManyField) OnRead(ctx *TFieldContext) error {
	// 字段计算步获取任何关系值
	ctx.UseNameGet = false
	ctx.ClassicRead = false

	if self.hasGetter {
		self._getterFunc(ctx)
	} else {
		field := ctx.Field
		if !field.IsRelatedField() {
			return fmt.Errorf("the field %s must related field, but not %s!", field.Name(), field.Type())
		}

		ds, err := ctx.Model.OneToMany(ctx)
		if err != nil {
			return err
		}

		if ds.Count() > 0 {
			// 获得关系Model 以提供idfield
			relateModel, err := ctx.Model.Orm().GetModel(field.RelatedModelName())
			if err != nil {
				return err
			}

			group := ds.GroupBy(field.RelatedFieldName())
			ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
				fieldValue := record.GetByField(ctx.Model.IdField())
				grp := group[fieldValue]
				if grp.Count() > 0 {
					//var records []map[string]any
					var records []any // 只保存ID
					grp.Range(func(pos int, record *dataset.TRecordSet) error {
						records = append(records, record.GetByField(relateModel.IdField()))
						return nil
					})
					record.SetByField(field.Name(), records)
				}

				return nil
			})
		}
	}

	return nil
}

func (self *TOne2ManyField) _OnWrite(ctx *TFieldContext) error {
	/* comodel = records.env[self.related_model_name].with_context(**self.context)
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

func (self *TMany2OneField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	params := ctx.Params
	fieldValue := ctx.FieldTypeValue

	if fieldValue.IsValid() {
		field.SqlType = GoType2SQLType(fieldValue.Type())
	} else {
		modelName := fmtModelName(utils.TitleCasedName(params[0]))
		relateModel, err := ctx.Orm.GetModel(modelName)
		if err != nil || relateModel == nil {
			log.Fatalf("field One2One %s@%s must including model name %s registered in orm!", field.Name(), relateModel.String(), modelName)
		}
		/* 保持和ID一致的类型 */
		idfield := relateModel.GetFieldByName(relateModel.IdField())
		if idfield == nil {
			panic("id field not found")
		}

		field.SqlType = *idfield.SQLType()
	}

	// 不直接指定 采用以下tag写法
	// field:"many2one() int()"
	//lField.initMany2One(lTag[1:]...)	fld._classic_read = true // 预先设计是false
	//fld.Base()._classic_write = true
	log.Assert(len(params) < 1, "Many2One(%s) of model %s must including at least 1 args!", field.Name(), self.model_name)
	field.isRelatedField = true
	field.related_model_name = fmtModelName(utils.TitleCasedName(params[0])) //目标表
	field._attr_relation = field.Base().related_model_name
	field._attr_type = TYPE_M2O
	field._attr_store = true

}

// TODO 未完成
func (self *TMany2OneField) OnRead(ctx *TFieldContext) error {
	field := ctx.Field
	if !field.IsRelatedField() {
		return fmt.Errorf("the field %s must related field, but not %s!", field.Name(), field.Type())
	}

	ds, err := ctx.Model.ManyToOne(ctx)
	if err != nil {
		return err
	}

	if ds.Count() > 0 {
		relateModel, err := ctx.Model.Orm().GetModel(field.RelatedModelName())
		if err != nil {
			return err
		}
		group := ds.GroupBy(relateModel.IdField())
		ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
			fieldValue := record.GetByField(field.Name())
			grp := group[fieldValue]

			if grp.Count() != 1 {
				return fmt.Errorf(
					"model %s's has more than 1 record for %s@%s ManyToOne Id %v",
					field.RelatedModelName(), field.Name(), field.ModelName(), grp.Keys())
			}

			record.SetByField(field.Name(), grp.Record().AsMap())
			return nil
		})
	}
	/*
		model, err := ctx.Session.Orm().osv.GetModel(self.RelatedModelName())
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
						return fmt.Errorf("the Many2One field(%s@%s) value is required!", self.model_name, self.Name())
					}

					ds.Next()
					continue
				}

				rel_ds, err := model.NameGet([]interface{}{rel_id})
				if err != nil {
					return err
				}

				id_field := model.IdField() // get the id field name
				ds.FieldByName(self.Name()).AsInterface([]interface{}{rel_ds.FieldByName(id_field).AsInterface(), rel_ds.FieldByName("name").AsInterface()})

				ds.Next()
			}
		}*/

	return nil
}

func (self *TMany2OneField) OnWrite(ctx *TFieldContext) error {
	value := ctx.Value

	/* 读取默认 */
	if utils.IsBlank(value) {
		value = ctx.Field.Default()
	}

	switch v := value.(type) {
	case string:
		/* 空字符串不进行Name搜索 */
		if v == "" {
			return nil
		}

		// 处理值为名称转为ID
		model, err := ctx.Model.Orm().GetModel(self.RelatedModelName(), WithContext(ctx.Model.Options().Context))
		if err != nil {
			return err
		}

		ds, err := model.NameSearch(v, nil, "", 1, "", nil)
		if err != nil {
			return err
		}

		if ds.Count() > 0 {
			ctx.SetValue(ds.Record().GetByField(model.IdField()))
		} else {
			if id, has := ctx.Session.CacheNameIds[v]; has {
				ctx.SetValue(id)
				break
			}

			/* 如果是命名者 有权根据名称创建记录 且关联模型支持RecordName */
			if ctx.Field.IsNamed() {
				if recName := model.GetRecordName(); recName != "" {
					model.Tx(ctx.Session.Clone())
					ids, err := model.Create(&CreateRequest{
						Context: ctx.Context,
						Data: []any{map[string]interface{}{
							recName: v,
						}},
					})
					if err != nil {
						return err
					}

					ctx.SetValue(ids[0])

					if ctx.Session.CacheNameIds == nil {
						ctx.Session.CacheNameIds = make(map[string]any)
					}
					ctx.Session.CacheNameIds[v] = ids[0]
				}
			}

		}
	case []interface{}:
		if len(v) > 0 {
			ctx.SetValue(v[0])
		}
	default:
		// 不修改
		ctx.SetValue(ctx.Value)
		//return fmt.Errorf("%s@%s OnWrite many2one failed with value:%v", field.Name(), ctx.Model.String(), v)
	}

	return nil
}

func (self *TMany2ManyField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	params := ctx.Params

	//	field._column_type = "" //* not a store field
	field.isRelatedField = true
	field._attr_store = false
	model_name := fmtModelName(utils.TitleCasedName(field.ModelName())) // 字段归属的Model

	cnt := len(params)
	if cnt == 0 {
		log.Panicf("Many2Many(%s) of model %s must including at least 1 args!", field.Name(), field.ModelName())
	}

	related_model := fmtModelName(utils.TitleCasedName(params[0])) // 字段链接的Model
	field.related_model_name = related_model                       //目标表
	field._attr_relation = related_model
	field._attr_type = TYPE_M2M

	switch cnt {
	case 1:
		// many2many(关联表)
		middle_model := fmt.Sprintf("%s.%s.rel", model_name, related_model)             // 表字段关系的Model
		field.middle_model_name = middle_model                                          //提供目标表格关系的表
		field.middle_keyfield_name = fmtFieldName(fmt.Sprintf("%s_id", model_name))     // 关系表关键字段
		field.related_keyfield_name = fmtFieldName(fmt.Sprintf("%s_id", related_model)) //目标表关键字段

	case 2:
		// many2many(关联表,关系表)
		middle_model := fmtModelName(utils.TitleCasedName(params[1]))                   // 表字段关系的Model
		field.middle_model_name = middle_model                                          //提供目标表格关系的表
		field.middle_keyfield_name = fmtFieldName(fmt.Sprintf("%s_id", model_name))     // 关系表关键字段
		field.related_keyfield_name = fmtFieldName(fmt.Sprintf("%s_id", related_model)) //目标表关键字段

	case 3:
		// many2many(关联表,字段1,字段2)
		middle_model := fmt.Sprintf("%s.%s.rel", model_name, related_model) // 表字段关系的Model
		field.middle_model_name = middle_model                              //提供目标表格关系的表
		field.middle_keyfield_name = fmtFieldName(params[1])                // 关系表关键字段
		field.related_keyfield_name = fmtFieldName(params[2])               //目标表关键字段

	case 4:
		// many2many(关联表,关系表,字段1,字段2)
		middle_model := fmtModelName(utils.TitleCasedName(params[1])) // 表字段关系的Model
		field.middle_model_name = middle_model                        //提供目标表格关系的表
		field.middle_keyfield_name = fmtFieldName(params[2])          // 关系表关键字段
		field.related_keyfield_name = fmtFieldName(params[3])         //目标表关键字段

	default:
		log.Panicf("field %s of model %s must format like 'Many2Many(relate_model)' or 'Many2Many(relate_model,model_id,relate_model_id)'!", field.Name(), self.model_name)
	}

	ctx.Orm.osv.middleModel.Store(field.middle_model_name, true)
}

// 创建关联表
// model, columns
func (self *TMany2ManyField) UpdateDb(ctx *TTagContext) {
	orm := ctx.Orm
	field := ctx.Field

	if _, has := orm.osv.models.Load(field.MiddleModelName()); has {
		return
	}

	idField := ctx.Model.GetFieldByName(ctx.Model.IdField())
	sqlType := orm.dialect.GetSqlType(idField)
	middle_model := strings.Replace(field.MiddleModelName(), ".", "_", -1)
	id1 := field.RelatedFieldName()
	id2 := field.MiddleFieldName()
	query := fmt.Sprintf(`
	           CREATE TABLE IF NOT EXISTS "%s" (
				"%s" %s NOT NULL,
				"%s" %s NOT NULL,UNIQUE("%s","%s"));
	           COMMENT ON TABLE "%s" IS '%s';
	           CREATE INDEX ON "%s" ("%s");
	           CREATE INDEX ON "%s" ("%s")`,
		middle_model,
		id1, sqlType,
		id2, sqlType, id1, id2,
		middle_model, fmt.Sprintf("RELATION BETWEEN %s AND %s", self.model_name, middle_model),
		middle_model, id1,
		middle_model, id2)
	if _, err := orm.Exec(query); err != nil {
		log.Errf("m2m create table '%s' failure : SQL:%s,\nError:%s", ctx.Field.RelatedModelName(), query, err.Error())
	}

	self.update_db_foreign_keys(ctx)

	// 新建模型
	relModel := new(TModel)
	model_val := reflect.Indirect(reflect.ValueOf(relModel))
	model_type := model_val.Type()
	model, err := orm._modelMetas(newModel(field.MiddleModelName(), middle_model, model_val, model_type, nil))
	if err != nil {
		log.Err(err)
	}
	// 注册model
	if err = orm.osv.RegisterModel("", model.GetBase()); err != nil {
		log.Err(err)
	}
}

// 设置字段获得的值
// TODO :未完成
func (self *TMany2ManyField) OnRead(ctx *TFieldContext) error {
	field := ctx.Field
	if !field.IsRelatedField() {
		return fmt.Errorf("the field %s must related field, but not %s!", field.Name(), field.Type())
	}

	ds, err := ctx.Model.ManyToMany(ctx)
	if err != nil {
		return err
	}

	if ds.Count() > 0 {
		group := ds.GroupBy(field.RelatedFieldName())
		ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
			//fieldValue := record.GetByField(field.Name()) // 货得many2many字段值
			fieldValue := record.GetByField(ctx.Model.IdField()) // 货得many2many字段值
			fieldRecord := group[fieldValue]
			if fieldRecord.Count() > 0 {
				var records []map[string]any
				fieldRecord.Range(func(pos int, record *dataset.TRecordSet) error {
					records = append(records, record.AsMap())
					return nil
				})
				record.SetByField(field.Name(), records)
			}

			return nil
		})
	}

	return nil
}

// write relate data to the reference table
func (self *TMany2ManyField) OnWrite(ctx *TFieldContext) error {
	/* 空值不处理 */
	if ctx.Value == nil {
		return nil
	}

	// TODO　更多类型
	// 支持一下几种M2M数据类型
	ids := make([]any, 0)
	switch v := ctx.Value.(type) {
	case []int:
		ids = utils.ToAnySlice(v...)
	case []int64:
		ids = utils.ToAnySlice(v...)
	case []any:
		ids = v
	default:
		return log.Errf("M2M field name <%s> could not support this type of value %v", ctx.Field.Name(), ctx.Value)
	}

	if len(ids) > 0 {
		// TODO 读比写快 不删除原有数据 直接读取并对比再添加
		// unlink all the relate record on the ref. table
		err := self.unlink_all(ctx, ids)
		if err != nil {
			return err
		}

		// relink record from new data
		err = self.link(ctx, ids)
		if err != nil {
			return err
		}
	}

	return nil
}

// # beware of duplicates when inserting
func (self *TMany2ManyField) link(ctx *TFieldContext, ids []interface{}) error {
	quoter := ctx.Session.Orm().dialect.Quoter()
	field := ctx.Field

	middle_table_name := quoter.Quote(strings.Replace(field.MiddleModelName(), ".", "_", -1))
	for _, rec_id := range ctx.Ids {
		for _, relate_id := range ids {
			query := fmt.Sprintf(
				`INSERT INTO %v (%s, %s) VALUES (?,?) ON CONFLICT DO NOTHING`,
				middle_table_name, quoter.Quote(field.MiddleFieldName()), quoter.Quote(field.RelatedFieldName()),
			)

			/*
			   	query := fmt.Sprintf(`INSERT INTO %s (%s, %s)
			                           (SELECT a, b FROM unnest(array[%s]) AS a, unnest(array[%s]) AS b)
			                           EXCEPT (SELECT %s, %s FROM %s WHERE %s IN (%s))`,
			   		middle_table_name, field.RelatedFieldName(), field.MiddleFieldName(),
			   		rec_id, strings.Join(ids, ","),
			   		field.RelatedFieldName(), field.MiddleFieldName(), middle_table_name, field.RelatedFieldName(), rec_id,
			   	)
			*/

			_, err := ctx.Session.Exec(query, rec_id, relate_id)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// TODO 错误将IDS删除基数
// # remove all records for which user has access rights
func (self *TMany2ManyField) unlink_all(ctx *TFieldContext, ids []interface{}) error {
	quote := ctx.Session.Orm().dialect.Quoter().Quote
	field := ctx.Field
	//	model := ctx.Model
	//	model_name := field.ModelName()
	middle_table_name := quote(strings.Replace(field.MiddleModelName(), ".", "_", -1))
	insql := strings.Repeat("?,", len(ids)-1) + "?"
	and_insql := strings.Repeat("?,", len(ctx.Ids)-1) + "?"

	var b strings.Builder
	fmt.Fprintf(&b, "DELETE FROM %s WHERE %s.%s IN (%s) ",
		middle_table_name, middle_table_name, quote(field.MiddleFieldName()), insql)

	if len(ids) > 0 {
		fmt.Fprintf(&b, " AND %s.%s IN (%s)", middle_table_name, quote(field.RelatedFieldName()), and_insql)
	}

	// 提交修改
	session := ctx.Session // orm.NewSession()
	_, err := session.Exec(b.String(), append(ctx.Ids, ids...)...)
	if err != nil {
		return err
	}

	return nil
}

// Add the foreign keys corresponding to the field's relation table.
func (self *TMany2ManyField) update_db_foreign_keys(ctx *TTagContext) {
	/*        cr = model._cr
	          comodel = model.env[self.related_model_name]
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
