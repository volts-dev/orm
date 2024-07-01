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

func (self *ModelBuilder) Field(name, fieldType string) *fieldStatment {
	field := self.model.GetFieldByName(name)
	if field == nil {
		var err error
		field, err = NewField(name, WithFieldType(fieldType))
		if err != nil {
			log.Err(err)
		}

		if utils.IndexOf(field.Type(), TYPE_O2O, TYPE_O2M, TYPE_M2O, TYPE_M2M, TYPE_SELECTION) == -1 {
			fieldContext := &TTagContext{
				Orm:        self.Orm,
				Model:      self.model,
				Field:      field,
				ModelValue: self.model.modelValue,
			}
			field.Init(fieldContext)
		}

		self.model.AddField(field)
	}

	return &fieldStatment{
		field: field,
	}
}

func (self *ModelBuilder) SetName(name string) *ModelBuilder {
	return self
}

func (self *ModelBuilder) ___AddFields(fields ...*fieldStatment) *ModelBuilder {
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

	return self.Field(fieldName, "id")
}

func (self *ModelBuilder) BoolField(name string) *fieldStatment {
	return self.Field(name, "bool")
}

func (self *ModelBuilder) IntField(name string) *fieldStatment {
	return self.Field(name, "int")
}

func (self *ModelBuilder) BigIntField(name string) *fieldStatment {
	return self.Field(name, "bigint")
}

func (self *ModelBuilder) CharField(name string) *fieldStatment {
	return self.Field(name, "char")
}

func (self *ModelBuilder) VarcharField(name string) *fieldStatment {
	return self.Field(name, "varchar")
}

func (self *ModelBuilder) TextField(name string) *fieldStatment {
	return self.Field(name, "text")
}

func (self *ModelBuilder) NameField(name ...string) *fieldStatment {
	fieldName := "name"
	if len(name) > 0 {
		fieldName = name[0]
	}

	return self.Field(fieldName, "recname")
}

func (self *ModelBuilder) SelectionField(name string, fn func() [][]string) *fieldStatment {
	fieldState := self.Field(name, "selection")
	field := fieldState.field.Base()
	field._attr_selection = fn()
	return fieldState
}

func (self *ModelBuilder) OneToOneField(name, relateModel string) *fieldStatment {
	fieldState := self.Field(name, "one2one")
	field := fieldState.field.Base()
	field.isRelatedField = true
	field._attr_store = true
	field.comodel_name = fmtModelName(relateModel)
	field.cokey_field_name = ""
	field._attr_relation = field.comodel_name
	return fieldState
}

func (self *ModelBuilder) OneToManyField(name, relateModel, relateField string) *fieldStatment {
	fieldState := self.Field(name, "one2many")
	field := fieldState.field.Base()
	field.isRelatedField = true
	field._attr_store = false
	field.comodel_name = fmtModelName(relateModel)     //目标表
	field.cokey_field_name = fmtFieldName(relateField) //目标表关键字段
	field._attr_relation = field.comodel_name
	return fieldState
}

func (self *ModelBuilder) ManyToOneField(name, relateModel string) *fieldStatment {
	fieldState := self.Field(name, "many2one")
	field := fieldState.field.Base()
	field.isRelatedField = true
	field._attr_store = false
	field.comodel_name = fmtModelName(relateModel)
	field.cokey_field_name = ""
	field._attr_relation = field.comodel_name
	return fieldState
}

func (self *ModelBuilder) ManyToManyField(name, relateModel string, midModel ...string) *fieldStatment {
	fieldState := self.Field(name, "many2many")
	field := fieldState.field.Base()

	model1 := fmtModelName(utils.TitleCasedName(self.model.GetName())) // 字段归属的Model
	model2 := fmtModelName(utils.TitleCasedName(relateModel))          // 字段链接的Model
	rel_model := ""
	if len(midModel) > 0 {
		rel_model = fmtModelName(utils.TitleCasedName(midModel[0])) // 表字段关系的Model
	} else {
		rel_model = fmt.Sprintf("%s.%s.rel", model1, model2) // 表字段关系的Model
	}
	field.comodel_name = model2                                          //目标表
	field.relmodel_name = rel_model                                      //提供目标表格关系的表
	field.cokey_field_name = fmtFieldName(fmt.Sprintf("%s_id", model1))  //目标表关键字段
	field.relkey_field_name = fmtFieldName(fmt.Sprintf("%s_id", model2)) // 关系表关键字段
	field.isRelatedField = true
	field._attr_store = false
	field._attr_relation = field.comodel_name
	field._attr_type = TYPE_M2M

	return &fieldStatment{
		field: field,
	}
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

func (self *fieldStatment) Size(v int) *fieldStatment {
	self.field.Base()._attr_size = v
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

func (self *fieldStatment) Default(value string) *fieldStatment {
	self.field.Base()._attr_default = value
	return self
}

func (self *fieldStatment) Domain(value string) *fieldStatment {
	self.field.Base()._attr_domain = value
	return self
}

// 字段显示抬头
func (self *fieldStatment) Title(value string) *fieldStatment {
	self.field.Base()._attr_title = value
	return self
}

func (self *fieldStatment) Store(value bool) *fieldStatment {
	self.field.Base()._attr_store = value
	return self
}

func (self *fieldStatment) IsPrimaryKey() *fieldStatment {
	self.field.Base().isPrimaryKey = true
	return self
}
