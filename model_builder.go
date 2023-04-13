package orm

import (
	"fmt"

	"github.com/volts-dev/utils"
)

type (
	ModelBuilder struct {
		model *TModel
		Orm   *TOrm
	}

	fieldStatment struct {
		field IField
	}
)

// 数据库构建器
func newModelBuilder(orm *TOrm, model *TModel) *ModelBuilder {
	return &ModelBuilder{
		Orm:   orm,
		model: model,
	}
}

func (self *ModelBuilder) SetName(name string) *ModelBuilder {
	return self
}

func (self *ModelBuilder) AddFields(fields ...*fieldStatment) *ModelBuilder {
	for _, statment := range fields {
		self.model.AddField(statment.field)
	}
	return self
}

func (self *ModelBuilder) IdField(name ...string) *fieldStatment {
	fieldName := "id"
	if len(name) > 0 {
		fieldName = name[0]
	}
	fieldState := self.NewField(fieldName, "id")
	field := fieldState.field.Base()
	field.isPrimaryKey = true
	field._attr_store = true

	self.model.IdField(field.Name())
	return fieldState
}

func (self *ModelBuilder) NameField(name ...string) *fieldStatment {
	fieldName := "name"
	if len(name) > 0 {
		fieldName = name[0]
	}
	fieldState := self.NewField(fieldName, "recname")
	field := fieldState.field.Base()
	field._attr_store = true

	self.model.SetRecordName(field.Name())
	return fieldState
}

func (self *ModelBuilder) OneToOneField(name, relateModel string) *fieldStatment {
	fieldState := self.NewField(name, "one2one")
	field := fieldState.field.Base()
	field.isRelatedField = true
	field._attr_store = true
	field.comodel_name = fmtModelName(relateModel)
	field.cokey_field_name = ""
	field._attr_relation = field.comodel_name
	return fieldState
}

func (self *ModelBuilder) OneToManyField(name, relateModel, relateField string) *fieldStatment {
	fieldState := self.NewField(name, "one2many")
	field := fieldState.field.Base()
	field.isRelatedField = true
	field._attr_store = false
	field.comodel_name = fmtModelName(relateModel)     //目标表
	field.cokey_field_name = fmtFieldName(relateField) //目标表关键字段
	field._attr_relation = field.comodel_name
	return fieldState
}

func (self *ModelBuilder) ManyToOneField(name, relateModel string) *fieldStatment {
	fieldState := self.NewField(name, "many2one")
	field := fieldState.field.Base()
	field.isRelatedField = true
	field._attr_store = false
	field.comodel_name = fmtModelName(relateModel)
	field.cokey_field_name = ""
	field._attr_relation = field.comodel_name
	return fieldState
}

func (self *ModelBuilder) ManyToManyField(name, relateModel, midModel string) *fieldStatment {
	fieldState := self.NewField(name, "many2many")
	field := fieldState.field.Base()

	model1 := fmtModelName(utils.TitleCasedName(self.model.GetName()))   // 字段归属的Model
	model2 := fmtModelName(utils.TitleCasedName(relateModel))            // 字段链接的Model
	rel_model := fmtModelName(utils.TitleCasedName(midModel))            // 表字段关系的Model
	field.comodel_name = model2                                          //目标表
	field.relmodel_name = rel_model                                      //提供目标表格关系的表
	field.cokey_field_name = fmtFieldName(fmt.Sprintf("%s_id", model1))  //目标表关键字段
	field.relkey_field_name = fmtFieldName(fmt.Sprintf("%s_id", model2)) // 关系表关键字段
	field.isRelatedField = true
	field._attr_store = false
	field._attr_relation = field.comodel_name
	field._attr_type = TYPE_M2M
	return fieldState
}

func (self *ModelBuilder) NewField(name, fieldType string) *fieldStatment {
	fs := &fieldStatment{
		field: NewField(name, fieldType),
	}

	return fs
}

func (self *fieldStatment) Compute(fn func(ctx *TFieldContext) error) *fieldStatment {
	field := self.field.Base()
	field.isCompute = true
	field._compute = ""
	field._computeFunc = fn
	field._attr_store = false
	field._attr_readonly = field._attr_readonly || false

	return self
}

// 字段显示抬头
func (self *fieldStatment) String(v string) *fieldStatment {
	self.field.Base()._attr_title = v
	return self
}

// 字段的帮助描述
func (self *fieldStatment) Help(v string) *fieldStatment {
	self.field.Base()._attr_help = v
	return self
}

func (self *fieldStatment) Readonly(v bool) *fieldStatment {
	self.field.Base()._attr_readonly = v
	return self
}

func (self *fieldStatment) Required(v bool) *fieldStatment {
	self.field.Base()._attr_required = v
	return self
}

func (self *fieldStatment) Default(value any) *fieldStatment {
	self.field.Base()._attr_default = value
	return self
}

func (self *fieldStatment) IsPrimaryKey() *fieldStatment {
	self.field.Base().isPrimaryKey = true
	return self
}
