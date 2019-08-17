package orm

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-xorm/core"
	"github.com/volts-dev/utils"
)

type (
	ITagController func(hd *TFieldContext)
)

const (
	//******* common ********
	TAG_NAME     = "name"    // #字段名称
	TAG_OLD_NANE = "oldname" // #将被更换的名称

	//******* table ********
	TAG_TABLE_NAME        = "table_name"
	TAG_TABLE_DESCRIPTION = "table_description"
	TAG_TABLE_ORDER       = "table_order"
	//******* field ********
	// #attr
	TAG_IGNORE = "-" // 忽略某些继承者成员
	//TAG_READ_ONLY     = "<-"
	TAG_WRITE_ONLY = "->"

	TAG_ID            = "id"
	TAG_PK            = "pk"
	TAG_AUTO          = "autoincr"
	TAG_TYPE          = "type"
	TAG_SIZE          = "size"
	TAG_TITLE         = "title" // #字段显示名称
	TAG_HELP          = "help"  // #字段描述
	TAG_CREATED       = "created"
	TAG_UPDATED       = "updated"
	TAG_REQUIRED      = "required"
	TAG_DEFAULT       = "default"
	TAG_IDX           = "index"  // #索引字段
	TAG_UNIQUE        = "unique" // #保持唯一
	TAG_READ_ONLY     = "readonly"
	TAG_STATES        = "states"
	TAG_PRIORITY      = "priority"   // TODO
	TAG_ON_DELETE     = "ondelete"   // TODO
	TAGTRANSLATE_     = "translate"  // TODO
	TAG_SELECT        = "select"     // #select=True （在外键字段上创建了一个索引）
	TAG_CLASSIC_READ  = "read"       // #经典模式
	TAG_CLASSIC_WRITE = "write"      // #经典模式
	TAG_STORE         = "store"      //
	TAG_DOMAIN        = "domain"     //
	TAG_ATTACHMENT    = "attachment" // #使用集中存储二进制模式 可以是表/目录/云上
	TAG_SELECTABLE    = "selectable" //
	TAG_DELETED       = "deleted"    // TODO
	TAG_VER           = "version"    // TODO
	TAG_COMPUTE       = "compute"    // # 函数赋值
	TAG_INVERSE       = "inverse"    // # 函数赋值相反

	// # type
	TAG_INT       = "int"
	TAG_BIGINT    = "bigint"
	TAG_FLOAT     = "float"
	TAG_DATE      = "date"     // #日期
	TAG_TIME      = "datetime" // #完整时间 包含时区
	TAG_BOOL      = "bool"
	TAG_CHAR      = "char"
	TAG_VAR_CHAR  = "varchar"
	TAG_TEXT      = "text"
	TAG_SELECTION = "selection"
	TAG_JSON      = "json"
	TAG_BIN       = "binary"
	TAG_ON2MANY   = "one2many"
	TAG_MANY2ONE  = "many2one"
	TAG_MANY2MANY = "many2many"
	TAG_RELATION  = "relation"

	// # rel
	TAG_RELATED   = "related" //废弃
	TAG_EXTENDS   = "extends"
	TAG_RELATE    = "relate"
	TAG_INHERITS  = "inherits"  // #postgres 的继承功能
	TAG_INHERITED = "inherited" // #该字段继承来自X表X字段名称 //name = openerp.fields.Char(related='partner_id.name', inherited=True)
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

		// #attr
		//tag_ctrl[TAG_IGNORE] = "-" // 忽略某些继承者成员
		//tag_ctrl[TAG_READ_ONLY] = "<-"
		//tag_ctrl[TAG_WRITE_ONLY] = "->"
		TAG_NAME:     tag_name,
		TAG_OLD_NANE: tag_old_name,
		TAG_ID:       tag_id,
		TAG_PK:       tag_pk,
		TAG_AUTO:     tag_auto,
		TAG_TYPE:     tag_type,
		TAG_SIZE:     tag_size,
		TAG_TITLE:    tag_title,
		TAG_HELP:     tag_help,
		TAG_CREATED:  tag_created,
		TAG_UPDATED:  tag_updated,
		TAG_REQUIRED: tag_required,
		TAG_DEFAULT:  tag_default,
		//TAG_IDX] =
		//TAG_UNIQUE] =
		TAG_READ_ONLY: tag_read_only,
		//TAG_STATES:tag_s
		//TAG_PRIORITY] = "priority"     // TODO
		TAG_ON_DELETE: tag_ondelete,
		//TAGTRANSLATE_] = "translate"   // TODO
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
		TAG_COMPUTE: tag_compute,
		TAG_INVERSE: tag_inverse,
		//TAG_RELATION:tag_

		// # rel
		//TAG_RELATED] = "related" //废弃
		TAG_EXTENDS: tag_extends,
		TAG_RELATE:  tag_relate,
		//TAG_INHERITS: tag_extends_relate, //tag_inherits
		//T
	}
}

// Register makes a log provide available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func RegisterTagController(name string, ctrl ITagController) {
	name = strings.ToLower(name)
	if tag_ctrl == nil {
		panic("logs: Register provide is nil")
	}
	if _, dup := tag_ctrl[name]; dup {
		panic("logs: Register called twice for provider " + name)
	}
	tag_ctrl[name] = ctrl
}

func GetTagControllerByName(name string) ITagController {
	ctrl, ok := tag_ctrl[name]
	if !ok {
		fmt.Errorf("cache: unknown adapter name %q (forgot to import?)", name)
		return nil
	}

	return ctrl
}

// 字段值计算函数 必须是Model的方法
func tag_compute(ctx *TFieldContext) {
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

	fld := ctx.Field
	params := ctx.Params

	//# by default, computed fields are not stored, not copied and readonly
	fld.Base()._attr_store = false
	fld.Base()._attr_readonly = false

	if len(params) > 0 {
		fld.Base()._compute = "" // 初始化
		lStr := strings.Trim(params[0], "'")
		lStr = strings.Replace(lStr, "''", "'", -1)
		if m := ctx.Model.GetBase()._cls_value.MethodByName(lStr); m.IsValid() {
			fld.Base()._compute = lStr
		}
	} else {
		logger.Err("Compute tag ", fld.Name(), "'s Args can no be blank!")
	}
}

func tag_inverse(ctx *TFieldContext) {
}

// 废弃
func tag_type(ctx *TFieldContext) {

}

//
func tag_id(ctx *TFieldContext) {
	// do nothing here
	// already implement on field_id.go

	// set the id field to model
	ctx.Model.IdField(ctx.Field.Name())
}

func tag_pk(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.IsPrimaryKey = true
	col.Nullable = false
	fld.Base().primary_key = true
}

func tag_auto(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.IsAutoIncrement = true
	fld.Base().auto_increment = true
}

// TODO test
func tag_default(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field
	params := ctx.Params
	model := ctx.Model

	if len(params) > 0 {
		col.Default = params[0]
		model.Obj().SetDefaultByName(fld.Name(), params[0]) // save to model object
	}
}

//TODO tag_extends 未完成
func tag_extends(ctx *TFieldContext) {
	fld_val := ctx.FieldTypeValue
	model := ctx.Model
	params := ctx.Params

	switch fld_val.Kind() {
	case reflect.Ptr:
		logger.Errf("field:%s as pointer is not supported!", fld_val.Type().Name())
		break
	case reflect.Struct:
		// #当该值为空时表示不限制字段
		lRelFields := params
		lRelFieldsCnt := len(lRelFields)

		object_name := utils.Obj2Name(fld_val.Interface())
		model_name := fmtModelName(object_name)
		// 现在成员名是关联的Model名,Tag 为关联的字段
		model.Obj().SetRelationByName(model_name, params[0])

		parentModel := ctx.Orm.mapping("", fld_val.Interface())
		for _, fld := range parentModel._fields {
			// #限制某些字段
			// @ 当参数多余1个时判断为限制字段　例如：`field:"extends(PartnerId,Name)"`
			if lRelFieldsCnt > 1 && utils.InStrings(fld.Name(), lRelFields...) == -1 {
				continue
			}

			lNewFld := utils.Clone(fld).(IField) // 复制关联字段
			lNewFld.SetBase(fld.Base())
			if f := model.FieldByName(fld.Name()); f == nil {
				// # 当Tag为Extends,Inherits时,该结构体所有合法字段将被用于创建数据库表字段

				// db:读写锁
				model.GetBase().table.AddColumn(lNewFld.Column())

				if lNewFld.Base().auto_increment {
					model.GetBase().table.AutoIncrement = lNewFld.Name()
				}

				//# 以下因为使用postgres 的继承方法时Model部分字段是由Parent继承来的
				//# 映射时是没有Parent的字段如Id 所以在此获取Id主键.
				if lNewFld.Base().primary_key && lNewFld.Base().auto_increment {
					model.GetBase().idField = lNewFld.Name()
					//logger.Dbg("RecordField", fld.Name())
				}

				model.GetBase()._fields[fld.Name()] = lNewFld
			}
		}
	}
}

func tag_relate(ctx *TFieldContext) {
	fld_val := ctx.FieldTypeValue
	model := ctx.Model
	params := ctx.Params

	switch fld_val.Kind() {
	case reflect.Ptr:
		logger.Errf("field:%s as pointer is not supported!", fld_val.Type().Name())
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
			parentModel = ctx.Orm.mapping("", fld_val.Interface())
			logger.Dbg("parentModel ", model_name)
		}

		for _, fld := range parentModel.GetFields() {
			// #限制某些字段
			// @ 当参数多余1个时判断为限制字段　例如：`field:"relate(PartnerId,Name)"`
			if lRelFieldsCnt > 1 && utils.InStrings(fld.Name(), lRelFields...) == -1 {
				continue
			}

			new_field := utils.Clone(fld).(IField) // 复制关联字段
			new_field.SetBase(fld.Base())
			if f := model.FieldByName(fld.Name()); f == nil {
				//logger.Dbg("FFF", parentModel.GetModelName(), fld.Name(), fld, new_field)

				// # 当Tag为Extends,Inherits时,该结构体所有合法字段将被用于创建数据库表字段
				new_field.Base().isInheritedField = true

				if new_field.Base().auto_increment {
					model.GetBase().table.AutoIncrement = new_field.Name()
				}

				//# 映射时是没有Parent的字段如Id 所以在此获取Id主键.
				if new_field.Base().primary_key && new_field.Base().auto_increment {
					model.GetBase().idField = new_field.Name()
				}
				model.GetBase()._fields[fld.Name()] = new_field

			} else {
				model.GetBase().SetCommonFieldByName(fld.Name(), parentModel.GetTableName(), new_field)
				model.GetBase().SetCommonFieldByName(fld.Name(), f.Base().model_name, f)
				//logger.Dbg("_common_fields2", fld.Name, parentModel.name, f.model_name, &lNewFld)
			}
		}
	}
}

func tag_created(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.IsCreated = true
	fld.Base().isCreated = true
}

func tag_updated(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field

	col.IsUpdated = true
	fld.Base().isUpdated = true
}

func tag_deleted(ctx *TFieldContext) {
	col := ctx.Column
	col.IsDeleted = true
}

func tag_ver(ctx *TFieldContext) {
	col := ctx.Column
	col.IsVersion = true
	col.Default = "1"
}

func tag_name(ctx *TFieldContext) {
	model := ctx.Model
	col := ctx.Column
	fld := ctx.Field
	params := ctx.Params
	cnt := len(params)
	if cnt == 1 {
		name := params[0]
		name = strings.Trim(name, "'")

		//  更新关联字段名称
		for tbl, fieldName := range model.GetBase().obj.GetRelations() {
			if fld.Name() == fieldName {
				model.GetBase().obj.SetRelationByName(tbl, name)
				break
			}
		}

		// 完成修改
		col.Name = name
		fld.Base()._attr_name = name
	}

	if cnt == 2 {
		//old_name := params[1]
		//new_ame := params[2]
		//TODO
	}
}

func tag_old_name(ctx *TFieldContext) {
}

func tag_title(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		title := strings.Trim(params[0], "'")
		title = strings.Replace(title, "''", "'", -1)
		fld.Base()._attr_title = title
	}
}

func tag_help(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		help := strings.Trim(params[0], "'")
		help = strings.Replace(help, "''", "'", -1)
		col.Comment = help
		fld.Base()._attr_help = help
	}
}

func tag_required(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field
	params := ctx.Params

	fld.Base()._attr_required = true
	if len(params) > 0 {
		fld.Base()._attr_required = utils.StrToBool(params[0])
	}

	col.Nullable = !fld.Base()._attr_required
}

func tag_read_only(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field
	params := ctx.Params

	fld.Base()._attr_readonly = true
	col.MapType = core.ONLYFROMDB
	if len(params) > 0 {
		fld.Base()._attr_readonly = utils.StrToBool(params[0])
	}
}

func tag_size(ctx *TFieldContext) {
	col := ctx.Column
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		col.Length = int(utils.StrToInt64(params[0]))
		fld.Base()._attr_size = utils.StrToInt64(params[0])
	}
}

func tag_ondelete(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		fld.Base().ondelete = strings.Trim(params[0], "'")
	}
}

func tag_store(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		fld.Base()._attr_store = utils.StrToBool(params[0])
	} else {
		fld.Base()._attr_store = true
	}
}

func tag_domain(ctx *TFieldContext) {
	fld := ctx.Field
	params := ctx.Params

	if len(params) > 0 {
		domain := strings.Trim(params[0], "'")
		fld.Base()._attr_domain = domain
	}
}

func tag_attachment(ctx *TFieldContext) {
	if fld, ok := ctx.Field.(*TBinField); ok {
		fld.attachment = true
	}
}

// Only for table
// sample: `table:"name('orm.user')"`
func tag_table_name(ctx *TFieldContext) {
	model := ctx.Model
	params := ctx.Params

	// TODO 未安全测试
	if len(params) > 0 {
		name := params[0]
		name = strings.Trim(name, "'")

		if name != "" { // 检测合法不为空
			model.GetBase().name = fmtModelName(name)
			model.GetBase().table.Name = fmtModelName(name) //strings.Replace(name, ".", "_", -1)
			//logger.Dbg("tag_table_name", params, name, model.GetBase().name, model.GetBase().table.Name)
		}
	}
}

// Only for table
func tag_table_description(ctx *TFieldContext) {
	model := ctx.Model
	params := ctx.Params

	if len(params) > 0 {
		description := strings.Trim(params[0], "'")
		description = strings.Replace(description, "''", "'", -1)
		model.GetBase()._description = description
		model.GetBase().table.Comment = description
	}
}

// Only for table
// TODO 支持多字段排序
func tag_table_order(ctx *TFieldContext) {
	model := ctx.Model
	params := ctx.Params

	if len(params) > 0 {
		model.GetBase()._order = strings.Trim(params[0], "'")
	}
}
