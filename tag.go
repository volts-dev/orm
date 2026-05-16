package orm

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/volts-dev/utils"
)

/*
*

	字段Tag

*
*/
type (
	ITagController func(hd *TTagContext) error
)

const (
	//******* common tag ********
	TAG_NAME     = "name"    // #字段名称
	TAG_OLD_NANE = "oldname" // #将被更换的名称

	//******* table tags ********
	TAG_TABLE_NAME        = "table_name"
	TAG_TABLE_DESCRIPTION = "table_description"
	TAG_TABLE_ORDER       = "table_order"

	// rel
	//TAG_RELATED   = "related" //废弃
	TAG_TABLE_EXTENDS = "table_extends" // TODO
	TAG_TABLE_RELATE  = "table_relate"
	TAG_INHERITS      = "inherits"  // #postgres 的继承功能
	TAG_INHERITED     = "inherited" // #该字段继承来自X表X字段名称 //name = openerp.fields.Char(related='partner_id.name', inherited=True)
	//******* field tags********
	// attr
	TAG_IGNORE        = "-" // 忽略某些继承者成员
	TAG_READ_ONLY     = "<-"
	TAG_WRITE_ONLY    = "->"
	TAG_PK            = "pk"
	TAG_AUTO          = "autoincr"
	TAG_TYPE          = "type"
	TAG_SIZE          = "size"
	TAG_TITLE         = "title" // #字段显示名称
	TAG_HELP          = "help"  // #字段描述
	TAG_CREATED       = "created"
	TAG_UPDATED       = "updated"
	TAG_REQUIRED      = "required"
	TAG_NAMED         = "named"
	TAG_DEFAULT       = "default"
	TAG_IDX           = "index"  // #索引字段
	TAG_UNIQUE        = "unique" // #保持唯一
	TAG_AS            = "as"
	TAG_STATES        = "states"
	TAG_PRIORITY      = "priority"   // TODO
	TAG_ON_DELETE     = "ondelete"   // TODO
	TAG_TRANSLATE     = "translate"  // TODO
	TAG_SELECT        = "select"     // #select=True （在外键字段上创建了一个索引）
	TAG_CLASSIC_READ  = "read"       // #经典模式
	TAG_CLASSIC_WRITE = "write"      // #经典模式
	TAG_STORE         = "store"      //
	TAG_DOMAIN        = "domain"     //
	TAG_ATTACHMENT    = "attachment" // #使用集中存储二进制模式 可以是表/目录/云上
	TAG_SELECTABLE    = "selectable" //
	TAG_DELETED       = "deleted"    // TODO
	TAG_VER           = "version"    // TODO
	TAG_SETTER        = "setter"     // # 函数赋值
	TAG_GETTER        = "getter"     // # 函数赋值

	// type
	TAG_ID        = "id"
	TAG_INT       = "int"
	TAG_BIGINT    = "bigint"
	TAG_FLOAT     = "float"
	TAG_DATE      = "date"     // #日期
	TAG_TIME      = "datetime" // #完整时间 包含时区
	TAG_BOOL      = "bool"
	TAG_CHAR      = "char"
	TAG_RECNAME   = "recname"
	TAG_VAR_CHAR  = "varchar"
	TAG_TEXT      = "text"
	TAG_SELECTION = "selection"
	TAG_JSON      = "json"
	TAG_BIN       = "binary"
	TAG_ON2MANY   = "one2many"
	TAG_MANY2ONE  = "many2one"
	TAG_MANY2MANY = "many2many"
	TAG_RELATION  = "relation" // 关系表 用于多对多等

)

var (
	tag_ctrl map[string]ITagController
)

func init() {
	// #注册Tag处理
	tag_ctrl = map[string]ITagController{
		TAG_TABLE_NAME:        tag_table_name,
		TAG_TABLE_DESCRIPTION: tag_table_description,
		TAG_TABLE_ORDER:       tag_table_order,
		// # rel
		TAG_TABLE_EXTENDS: tag_table_extends,
		//TAG_TABLE_RELATE:  tag_table_relate,

		// #attr
		//tag_ctrl[TAG_IGNORE] = "-" // 忽略某些继承者成员
		"readonly":     tag_read_only,
		"writeonly":    tag_write_only,
		TAG_READ_ONLY:  tag_read_only,
		TAG_WRITE_ONLY: tag_write_only,
		TAG_NAME:       tag_name,
		TAG_OLD_NANE:   tag_old_name,
		TAG_ID:         tag_id,
		TAG_RECNAME:    tag_recname,
		TAG_PK:         tag_pk,
		TAG_AUTO:       tag_auto,
		TAG_TYPE:       tag_type,
		TAG_SIZE:       tag_size,
		TAG_TITLE:      tag_title,
		TAG_HELP:       tag_help,
		TAG_CREATED:    tag_created,
		TAG_UPDATED:    tag_updated,
		TAG_REQUIRED:   tag_required,
		TAG_NAMED:      tag_named,
		TAG_DEFAULT:    tag_default,
		TAG_IDX:        tag_index,
		TAG_UNIQUE:     tag_unique,
		TAG_AS:         tag_as,
		//TAG_STATES:tag_s
		//TAG_PRIORITY] = "priority"     // TODO
		TAG_ON_DELETE: tag_ondelete,
		TAG_TRANSLATE: tag_translate, // TODO
		//TAG_SELECT] = "select"         // #select=True （在外键字段上创建了一个索引）
		//TAG_CLASSIC_READ:  tag_read,
		//TAG_CLASSIC_WRITE: tag_write,
		TAG_STORE:      tag_store,
		TAG_DOMAIN:     tag_domain,
		TAG_ATTACHMENT: tag_attachment,
		//TAG_SELECTABLE] =
		//TAG_GROUPS] = "groups"         // #groups='base.group_user' CSV list of ext IDs of groups
		TAG_DELETED: tag_deleted,
		TAG_VER:     tag_ver,

		// # type
		/*TAG_INT:       tag_int,
		TAG_BIGINT:    tag_bigint,
		TAG_FLOAT:     tag_float,
		TAG_DATE:      tag_date,
		TAG_TIME:      tag_time,
		TAG_BOOL:      tag_bool,
		TAG_CHAR:      tag_char,
		TAG_VAR_CHAR:  tag_char,
		TAG_TEXT:      tag_text,
		TAG_SELECTION: tag_selection,
		TAG_BIN:       tag_binary,
		TAG_ON2MANY:   tag_one2many,
		TAG_MANY2ONE:  tag_many2one,
		TAG_MANY2MANY: tag_many2many,
		TAG_JSON:    tag_json,*/
		//TAG_RELATION: tag_relation,
		TAG_SETTER: tag_setter,
		TAG_GETTER: tag_getter,
	}
}

// Register makes a log provide available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func RegisterTagController(name string, ctrl ITagController) {
	name = strings.ToLower(name)
	if tag_ctrl == nil {
		log.Panic("Register Tag Controller provide is nil")
	}

	if _, dup := tag_ctrl[name]; dup {
		log.Panic("Register called twice for provider " + name)
	}

	tag_ctrl[name] = ctrl
}

func GetTagControllerByName(name string) ITagController {
	if ctrl, has := tag_ctrl[name]; has {
		return ctrl
	}
	return nil
}

// getter 通过ctx信息处理并修改回dataset里
func tag_getter(ctx *TTagContext) error {
	/*
		fnct 是一个计算字段值的方法或函数。必须在声明函数字段前声明它。
		fnct_inv：是一个允许设置这个字段值的函数或方法。
		type：由函数返回的字段类型名。其可以是任何字段类型名 除了函数
		fnct_search：允许你在这个字段上定义搜索功能
		method：这个字段是由一个方法计算还是由一个全局函数计算。
		store：是否将这个字段存储在数据库中。默认false
		multi：一个组名。所有的有相同multi参数的字段将在一个单一函数调用中计算
	*/
	//lField.Type = "function" function 是未定义字段

	field := ctx.Field.Base()

	//# by default, computed fields are not stored, not copied and readonly
	field.store = field.store || false
	field.readonly = field.readonly || false
	field.getterMethod = "" // 初始化
	field.computeAsAdmin = field.computeAsAdmin || false

	if field.getterFunc == nil {
		params := ctx.Params
		if len(params) == 0 {
			log.Panic("Compute getter tag ", field.Name(), "'s Args can no be blank!")
		}

		methodName := strings.Trim(params[0], "'")
		methodName = strings.Replace(methodName, "''", "'", -1)
		if m := ctx.Model.GetBase().modelValue.MethodByName(methodName); m.IsValid() {
			if fn, ok := m.Interface().(FieldFunc); ok {
				field.boundModel = ctx.Model.GetBase().modelValue.Interface()
				field.getterMethod = methodName
				field.getterFunc = fn
			}
		}
	}

	field.hasGetter = field.getterFunc != nil || false
	return nil
}

func tag_setter(ctx *TTagContext) error {
	field := ctx.Field.Base()

	//# by default, computed fields are not stored, not copied and readonly
	field.store = field.store || false
	field.readonly = field.readonly || false
	field.setterMethod = ""
	field.computeAsAdmin = field.computeAsAdmin || false

	if field.setterFunc == nil {
		params := ctx.Params
		if len(params) == 0 {
			log.Panic("Compute setter tag ", field.Name(), "'s Args can no be blank!")
		}
		funcName := strings.Trim(params[0], "'")
		funcName = strings.Replace(funcName, "''", "'", -1)
		if m := ctx.Model.GetBase().modelValue.MethodByName(funcName); m.IsValid() {
			if fn, ok := m.Interface().(FieldFunc); ok {
				field.boundModel = ctx.Model.GetBase().modelValue.Interface()
				field.setterMethod = funcName
				field.setterFunc = fn
			}
		}
	}

	field.hasSetter = field.setterFunc != nil || false
	return nil
}

// dataset 数据类型
func tag_type(ctx *TTagContext) error {
	params := ctx.Params
	field := ctx.Field.Base()

	if len(params) > 0 {
		field.typeName = params[0]
	}
	return nil
}

func tag_as(ctx *TTagContext) error {
	params := ctx.Params
	field := ctx.Field.Base()

	if len(params) > 0 {
		field.outputAs = params[0]
	}
	return nil
}

func tag_id(ctx *TTagContext) error {
	// do nothing here
	// already implement on field_id.go

	// set the id field to model
	ctx.Model.IdField(ctx.Field.Name())
	return nil
}

func tag_recname(ctx *TTagContext) error {
	// set the id field to model
	ctx.Model.SetRecordName(ctx.Field.Name())
	return nil
}

func tag_pk(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params
	val := true
	if len(params) > 0 {
		val = utils.ToBool(params[0])
	}

	field.isPrimaryKey = val
	field.isUnique = val
	field.required = true
	return nil
}
func tag_auto(ctx *TTagContext) error {
	field := ctx.Field.Base()

	field.isAutoIncrement = true
	return nil
}

// TODO test
func tag_default(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params
	model := ctx.Model

	if field.defaultValue == "" {
		if len(params) > 0 {
			value := strings.Trim(params[0], "'")
			field.defaultValue = value
		}
	}

	if field.typeName == Bool {
		field.defaultValue = strings.ToLower(field.defaultValue)
	}

	model.Obj().SetDefaultByName(field.Name(), utils.ToString(field.defaultValue)) // save to model object

	return nil
}
func tag_created(ctx *TTagContext) error {
	field := ctx.Field.Base()
	field.isCreatedAt = true
	return nil
}

func tag_updated(ctx *TTagContext) error {
	field := ctx.Field.Base()
	field.isUpdatedAt = true
	return nil
}

func tag_deleted(ctx *TTagContext) error {
	field := ctx.Field.Base()
	field.isDeletedAt = true
	return nil
}

func tag_ver(ctx *TTagContext) error {
	field := ctx.Field.Base()
	field.isVersion = true
	field.defaultValue = "1"
	return nil
}

func tag_named(ctx *TTagContext) error {
	field := ctx.Field.Base()
	field.isNameField = true
	return nil
}

func tag_name(ctx *TTagContext) error {
	model := ctx.Model
	field := ctx.Field.Base()
	params := ctx.Params
	cnt := len(params)
	if cnt == 1 {
		name := params[0]
		name = strings.Trim(name, "'")

		//  更新关联字段名称
		var modelName, fieldName string
		model.GetBase().Obj().GetRelations().Range(func(key, value any) bool {
			modelName = key.(string)
			fieldName = value.(string)
			if field.Name() == fieldName {
				model.GetBase().obj.SetRelationByName(modelName, name)
				return true
			}
			return true
		})

		// 完成修改
		//col.Name = name
		field.name = name
	}

	if cnt == 2 {
		//old_name := params[1]
		//new_ame := params[2]
		//TODO
	}
	return nil
}

func tag_old_name(ctx *TTagContext) error {
	return nil
}

func tag_title(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		title := strings.Trim(params[0], "'")
		title = strings.Replace(title, "''", "'", -1)
		field.label = title
	}
	return nil
}

func tag_help(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		help := strings.Trim(params[0], "'")
		help = strings.Replace(help, "''", "'", -1)
		field.description = help
	}
	return nil
}

// unique(uniquename)
func tag_unique(ctx *TTagContext) error {
	field := ctx.Field.Base()
	model := ctx.Model
	field_name := field.Name()

	var index *TIndex
	var ok bool

	tableName := model.Table()
	uniqueName := ""
	if len(ctx.Params) > 0 {
		uniqueName = ctx.Params[0]
	} else {
		uniqueName = generate_index_name(UniqueType, tableName, []string{field_name})
	}
	if index, ok = model.Obj().indexes[uniqueName]; ok {
		index.AddColumn(field_name)
	} else {
		index = newIndex(uniqueName, tableName, UniqueType)
		index.AddColumn(field_name)
		model.Obj().AddIndex(index)
	}

	field.isUnique = true
	return nil
}

// index(indexname)
func tag_index(ctx *TTagContext) error {
	field := ctx.Field.Base()
	model := ctx.Model
	field_name := field.Name()

	tableName := model.Table()
	indexName := ""
	if len(ctx.Params) > 0 {
		indexName = ctx.Params[0]
	} else {
		indexName = generate_index_name(IndexType, tableName, []string{field_name})
	}

	if index, ok := model.Obj().indexes[indexName]; ok {
		index.AddColumn(field_name)
	} else {
		index := newIndex(indexName, tableName, IndexType)
		index.AddColumn(field_name)
		model.Obj().AddIndex(index)
	}

	field.isIndexed = true
	return nil
}

func tag_required(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	field.required = true
	if len(params) > 0 {
		field.required = utils.ToBool(params[0])
	}
	return nil
}

func tag_read_only(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	field.readonly = true
	field.MapType = ReadOnly
	if len(params) > 0 {
		field.readonly = utils.ToBool(params[0])
	}
	return nil
}

func tag_write_only(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	field.writeonly = true
	field.MapType = WriteOnly
	if len(params) > 0 {
		field.writeonly = utils.ToBool(params[0])
	}
	return nil
}

func tag_size(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		field.size = utils.ToInt(params[0])
	}
	return nil
}

func tag_ondelete(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		field.onDelete = strings.Trim(params[0], "'")
	}
	return nil
}

func tag_translate(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		field.translatable = utils.ToBool(params[0])
	} else {
		field.translatable = true
	}
	return nil
}

func tag_store(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		field.store = utils.ToBool(params[0])
	} else {
		field.store = true
	}
	return nil
}

func tag_domain(ctx *TTagContext) error {
	field := ctx.Field.Base()
	params := ctx.Params

	if len(params) > 0 {
		domain := strings.Trim(params[0], "'")
		field.domain = domain
	}
	return nil
}

func tag_attachment(ctx *TTagContext) error {
	if fld, ok := ctx.Field.(*TBinField); ok {
		fld.useAttachmentStore = true
	}
	return nil
}

// Only for table
// sample: `table:"name('orm.user')"`
func tag_table_name(ctx *TTagContext) error {
	model := ctx.Model
	params := ctx.Params

	// TODO 未安全测试
	if len(params) > 0 {
		name := params[0]
		name = strings.Trim(name, "'")

		if name != "" { // 检测合法不为空
			model.GetBase().name = fmtModelName(name)
			// Keep table name in sync with custom table tag.
			// This ensures schema migration and other DDL paths that use model.Table() stay consistent.
			model.GetBase().table = fmtTableName(name)
		}
	}

	return nil
}

// Only for table
func tag_table_description(ctx *TTagContext) error {
	model := ctx.Model
	params := ctx.Params

	if len(params) > 0 {
		description := strings.Trim(params[0], "'")
		description = strings.Replace(description, "''", "'", -1)
		model.GetBase().description = description
	}
	return nil

}

// Only for table
// TODO 支持多字段排序
func tag_table_order(ctx *TTagContext) error {
	model := ctx.Model
	params := ctx.Params

	if len(params) > 0 {
		model.GetBase().options.Order = unique(append(model.GetBase().options.Order, strings.Trim(params[0], "'")))
	}
	return nil

}

// TODO tag_extends 未完成
func tag_table_extends(ctx *TTagContext) error {
	fld_val := ctx.FieldTypeValue
	model := ctx.Model
	params := ctx.Params

	ctx.Field.Base().store = false // 忽略某些继承者成员
	switch fld_val.Kind() {
	case reflect.Ptr:
		log.Errf("field:%s as pointer is not supported!", fld_val.Type().Name())
		break
	case reflect.Struct:
		// #当该值为空时表示不限制字段
		lRelFields := params
		lRelFieldsCnt := len(lRelFields)

		object_name := utils.Obj2Name(fld_val.Interface())
		model_name := fmtModelName(object_name)

		// 现在成员名是关联的Model名,Tag 为关联的字段
		model.Obj().SetRelationByName(model_name, params[0])

		parentModel, err := ctx.Orm._mapping(fld_val.Interface())
		if err != nil {
			return err
		}

		for _, fld := range parentModel.obj.GetFields() {
			// #限制某些字段
			// @ 当参数多余1个时判断为限制字段　例如：`field:"extends(PartnerId,Name)"`
			if lRelFieldsCnt > 1 && utils.IndexOf(fld.Name(), lRelFields...) == -1 {
				continue
			}

			lNewFld := utils.Clone(fld).(IField) // 复制关联字段
			lNewFld.SetBase(fld.Base())
			if f := model.GetFieldByName(fld.Name()); f == nil {
				// # 当Tag为Extends,Inherits时,该结构体所有合法字段将被用于创建数据库表字段

				// db:读写锁
				//model.GetBase().table.AddColumn(lNewFld.Column())
				//model.GetBase()._addField(lNewFld)

				if lNewFld.IsAutoIncrement() {
					model.Obj().AutoIncrementField = lNewFld.Name()
				}

				//# 以下因为使用postgres 的继承方法时Model部分字段是由Parent继承来的
				//# 映射时是没有Parent的字段如Id 所以在此获取Id主键.
				if lNewFld.Base().isPrimaryKey && lNewFld.Base().isAutoIncrement {
					model.GetBase().idField = lNewFld.Name()
				}

				model.GetBase().obj.SetField(lNewFld)
			}
		}
	}
	return nil

}

// relation(关系表)
func tag_relation(ctx *TTagContext) error {
	return nil

}

// 废弃O2O
// relate(modelName,relateField)
func tag_table_relate(ctx *TTagContext) error {
	model := ctx.Model
	params := ctx.Params

	// #当该值为空时表示不限制字段
	if len(params) != 2 {
		return fmt.Errorf("relate:%v must including model name and field!", params)
	}

	modelName := fmtModelName(params[0])
	relateField := fmtFieldName(params[1])

	// 现在成员名是关联的Model名,Tag 为关联的字段
	model.Obj().SetRelationByName(modelName, relateField)

	parentModel, err := ctx.Orm.GetModel(modelName)
	if err != nil || parentModel == nil {
		return fmt.Errorf("tag func relate(%v) must including model name and field!", params)

	}

	var (
		parentField, newField IField
		fieldName             string
	)
	for _, parentField = range parentModel.GetFields() {
		// #限制某些字段
		// @ 当参数多余1个时判断为限制字段　例如：`field:"relate(PartnerId,Name)"`
		//if lRelFieldsCnt > 1 && utils.IndexOf(parentField.Name(), lRelFields...) == -1 {
		//	continue
		//}
		fieldName = parentField.Name()
		newField = utils.Clone(parentField).(IField) // 复制关联字段
		newField.SetBase(parentField.Base())

		if f := model.GetFieldByName(fieldName); f != nil {
			// 相同字段处理
			model.GetBase().obj.SetCommonFieldByName(fieldName, parentModel.String(), newField)
			model.GetBase().obj.SetCommonFieldByName(fieldName, f.Base().modelName, f)

		} else {
			// # 当Tag为Extends,Inherits时,该结构体所有合法字段将被用于创建数据库表字段
			newField.Base().isInherited = true
			newField.Base().store = false // 关系字段不存储

			if newField.IsAutoIncrement() {
				//model.GetBase().table.AutoIncrement = fieldName
				model.Obj().AutoIncrementField = fieldName
			}

			//# 映射时是没有Parent的字段如Id 所以在此获取Id主键.
			if newField.Base().isPrimaryKey && newField.Base().isAutoIncrement {
				model.GetBase().idField = fieldName
			}
			model.GetBase().obj.SetField(newField)
		}
	}
	return nil

}

func ___tag_relate(ctx *TTagContext) error {
	fld_val := ctx.FieldTypeValue
	model := ctx.Model
	params := ctx.Params

	ctx.Field.Base().store = false // 忽略某些继承者成员

	switch fld_val.Kind() {
	case reflect.Ptr:
		log.Errf("field:%s as pointer is not supported!", fld_val.Type().Name())
		break
	case reflect.Struct:
		// #当该值为空时表示不限制字段
		lRelFields := params
		lRelFieldsCnt := len(lRelFields)

		//
		object_name := utils.Obj2Name(fld_val.Interface())
		model_name := fmtModelName(object_name)

		// 现在成员名是关联的Model名,Tag 为关联的字段
		model.Obj().SetRelationByName(model_name, fmtFieldName(params[0]))

		parentModel, err := ctx.Orm.GetModel(model_name)
		if err != nil || parentModel == nil {
			parentModel, err = ctx.Orm._mapping(fld_val.Interface())
			if err != nil {
				return err
			}
		}

		var (
			parent_field, new_field IField
			field_name              string
		)
		for _, parent_field = range parentModel.GetFields() {
			// #限制某些字段
			// @ 当参数多余1个时判断为限制字段　例如：`field:"relate(PartnerId,Name)"`
			if lRelFieldsCnt > 1 && utils.IndexOf(parent_field.Name(), lRelFields...) == -1 {
				continue
			}
			field_name = parent_field.Name()
			new_field = utils.Clone(parent_field).(IField) // 复制关联字段
			new_field.SetBase(parent_field.Base())

			if f := model.GetFieldByName(field_name); f != nil {
				model.GetBase().obj.SetCommonFieldByName(field_name, parentModel.String(), new_field)
				model.GetBase().obj.SetCommonFieldByName(field_name, f.Base().modelName, f)

			} else {
				// # 当Tag为Extends,Inherits时,该结构体所有合法字段将被用于创建数据库表字段
				new_field.Base().isInherited = true
				new_field.Base().store = false // 关系字段不存储

				if new_field.IsAutoIncrement() {
					//model.GetBase().table.AutoIncrement = field_name
					model.Obj().AutoIncrementField = field_name
				}

				//# 映射时是没有Parent的字段如Id 所以在此获取Id主键.
				if new_field.Base().isPrimaryKey && new_field.Base().isAutoIncrement {
					model.GetBase().idField = field_name
				}
				model.GetBase().obj.SetField(new_field)
			}
		}
	}
	return nil
}
