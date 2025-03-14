package orm

import (
	"github.com/volts-dev/utils"
)

type (
	/*
		Builder 所有操作会直接写入对应 ModelObject 里
	*/
	ModelBuilder struct {
		model *TModel
		Orm   *TOrm
	}

	fieldStatment struct {
		builder *ModelBuilder
		field   IField
	}
)

// 数据库构建器
func newModelBuilder(orm *TOrm, model *TModel) *ModelBuilder {
	return &ModelBuilder{
		Orm:   orm,
		model: model,
	}
}

func (self *ModelBuilder) SetIndiex(fieldNames ...string) *ModelBuilder {
	/* 识别索引类型 */
	idxType := IndexType
	for _, fieldName := range fieldNames {
		field := self.model.GetFieldByName(fieldName)

		if field.IsUnique() {
			idxType = UniqueType
			break
		}
	}

	self.model.Obj().AddIndex(newIndex("", self.model.Table(), idxType, fieldNames...))
	return self
}

func (self *ModelBuilder) Field(name, fieldType string) *fieldStatment {
	field := self.model.GetFieldByName(name)
	if field == nil {
		var err error
		field, err = NewField(name, WithFieldType(fieldType))
		if err != nil {
			log.Fatal(err)
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

		self.model._addField(field)
	}

	return &fieldStatment{
		builder: self,
		field:   field,
	}
}

func (self *ModelBuilder) SetName(name string) *ModelBuilder {
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

func (self *ModelBuilder) DateTimeField(name string) *fieldStatment {
	return self.Field(name, "datetime")
}

func (self *ModelBuilder) NameField(name ...string) *fieldStatment {
	fieldName := "name"
	if len(name) > 0 {
		fieldName = name[0]
	}

	return self.Field(fieldName, "recname")
}

func (self *ModelBuilder) SelectionField(name string, fn func() [][]string) *fieldStatment {
	fieldStmt := self.Field(name, "selection")
	fieldStmt.field.Base()._attr_selection = fn()
	fieldStmt.field.Init(&TTagContext{
		Orm:        self.Orm,
		Model:      self.model,
		Field:      fieldStmt.field,
		ModelValue: self.model.modelValue,
	})

	return fieldStmt
}

func (self *ModelBuilder) OneToOneField(name, relateModel string) *fieldStatment {
	fieldStmt := self.Field(name, "one2one")
	fieldStmt.field.Init(&TTagContext{
		Orm:        self.Orm,
		Model:      self.model,
		Field:      fieldStmt.field,
		ModelValue: self.model.modelValue,
		Params:     []string{relateModel},
	})

	return fieldStmt
}

func (self *ModelBuilder) OneToManyField(name, relateModel, relateField string) *fieldStatment {
	fieldStmt := self.Field(name, "one2many")
	fieldStmt.field.Init(&TTagContext{
		Orm:        self.Orm,
		Model:      self.model,
		Field:      fieldStmt.field,
		ModelValue: self.model.modelValue,
		Params:     []string{relateModel, relateField},
	})

	return fieldStmt
}

func (self *ModelBuilder) ManyToOneField(name, relateModel string) *fieldStatment {
	fieldStmt := self.Field(name, "many2one")
	fieldStmt.field.Init(&TTagContext{
		Orm:        self.Orm,
		Model:      self.model,
		Field:      fieldStmt.field,
		ModelValue: self.model.modelValue,
		Params:     []string{relateModel},
	})

	return fieldStmt
}

/*
严格按照以下顺序
many2many(关联表)
many2many(关联表,中间表)
many2many(关联表,源表字段,关联表字段)
many2many(关联表,中间表,源表字段,关联表字段)
*/
func (self *ModelBuilder) ManyToManyField(name, relateModel string, midModel ...string) *fieldStatment {
	fieldStmt := self.Field(name, "many2many")
	fieldStmt.field.Init(&TTagContext{
		Orm:        self.Orm,
		Model:      self.model,
		Field:      fieldStmt.field,
		ModelValue: self.model.modelValue,
		Params:     append([]string{relateModel}, midModel...),
	})

	return fieldStmt
}

func (self *fieldStatment) Getter(fn func(ctx *TFieldContext) error) *fieldStatment {
	field := self.field
	field.Base()._getterFunc = fn
	builder := self.builder

	if err := tag_getter(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}

	return self
}

// 写入值需要计算的字段
// 最终结果由Ctx。SetValue 返回
// 返回值包含 any,[]any,map[string]any
func (self *fieldStatment) Setter(fn func(ctx *TFieldContext) error) *fieldStatment {
	field := self.field
	field.Base()._setterFunc = fn
	builder := self.builder

	if err := tag_setter(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}

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

func (self *fieldStatment) Required(v ...bool) *fieldStatment {
	if len(v) == 0 {
		self.field.Base()._attr_required = true
		return self
	}
	self.field.Base()._attr_required = v[0]
	return self
}

func (self *fieldStatment) Default(value any) *fieldStatment {
	field := self.field
	builder := self.builder
	field.Base()._attr_default = utils.ToString(value)

	if err := tag_default(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}

	return self
}

func (self *fieldStatment) ComputeDefault(fn func(ctx *TFieldContext) error) *fieldStatment {
	field := self.field.Base()
	field._computeDefault = fn
	//field._attr_store = false
	//field._attr_readonly = field._attr_readonly || false

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

func (self *fieldStatment) IsUnique() *fieldStatment {
	field := self.field
	builder := self.builder

	if err := tag_unique(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
	}); err != nil {
		log.Warn(err.Error())
	}
	return self
}

func (self *fieldStatment) IsIndex(name ...string) *fieldStatment {
	field := self.field
	builder := self.builder

	if err := tag_index(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
		Params:     name,
	}); err != nil {
		log.Warn(err.Error())
	}

	return self
}

func (self *fieldStatment) IsCreated(name ...string) *fieldStatment {
	field := self.field
	builder := self.builder

	if err := tag_created(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
		Params:     name,
	}); err != nil {
		log.Warn(err.Error())
	}

	return self
}

func (self *fieldStatment) IsUpdated(name ...string) *fieldStatment {
	field := self.field
	builder := self.builder

	if err := tag_updated(&TTagContext{
		Orm:        builder.Orm,
		Model:      builder.model,
		Field:      field,
		ModelValue: builder.model.modelValue,
		Params:     name,
	}); err != nil {
		log.Warn(err.Error())
	}

	return self
}

func (self *fieldStatment) Ondelete(value string) *fieldStatment {
	self.field.Base().ondelete = value
	return self
}
