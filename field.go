package orm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"errors"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

// FieldAccessMode 字段读写模式
type FieldAccessMode int

const (
	ReadWrite  FieldAccessMode = iota + 1 // 双向同步
	WriteOnly                              // 仅写入数据库
	ReadOnly                               // 仅从数据库读取
)

var (
	// 注册的Writer类型函数接口
	field_creators = make(map[string]func() IField)
)

type (
	FieldFunc = func(*TFieldContext) error
	// The context for Tag
	TTagContext struct {
		Orm            *TOrm
		Model          IModel        // required
		Field          IField        // required
		FieldTypeValue reflect.Value // 用途未确认，保留兼容
		ModelValue     reflect.Value
		Params         []string // 属性参数 int(<params>)
	}

	TFieldContext struct {
		Ids []any // 提供查询所有指定外键绑定的Ids
		//Id          interface{} // the current id of current record
		Value       any    // the current value of the field
		Field       IField // FieldTypeValue reflect.Value
		Fields      []string
		Domain      string // update 支持查询条件
		Model       IModel
		Session     *TSession
		Dataset     *dataset.TDataSet // 数据集将被修改
		ClassicRead bool
		UseNameGet  bool
		Context     context.Context
		values      any // the current value of the field

	}

	// IField represents a model field. It exposes:
	//
	//   - introspection (IsPrimaryKey, IsAutoIncrement, IsIndexed, ...)
	//   - configuration accessors (Readonly, Required, Store, Default, ...)
	//   - schema metadata (SQLType, TypeName, Label, Description, ...)
	//   - relational metadata (RelatedModelName, RelatedKeyName, JoinModelName, ...)
	//   - I/O hooks (OnRead, OnWrite)
	//   - classic codec hooks (onConvertToRead/Write, unexported)
	//
	// IField is implemented by TField and its type-specific subclasses
	// (TCharField, TIntField, etc.).
	IField interface {
		// IsPrimaryKey reports whether this field is the model's primary key.
		IsPrimaryKey() bool
		// IsCompositeKey reports whether this field is part of a composite primary key.
		IsCompositeKey() bool
		// IsAutoIncrement reports whether the database assigns this field's value automatically.
		IsAutoIncrement() bool
		// IsDefaultEmpty reports whether the field has no default value (literal or function).
		IsDefaultEmpty() bool
		// IsUnique reports whether the field has a UNIQUE constraint.
		IsUnique() bool
		// IsCreatedAt reports whether this field stores the record's creation timestamp.
		IsCreatedAt() bool
		// IsDeletedAt reports whether this field stores the soft-delete timestamp.
		IsDeletedAt() bool
		// IsUpdatedAt reports whether this field stores the record's last-update timestamp.
		IsUpdatedAt() bool
		// IsNameField reports whether this field is the model's display-name column.
		IsNameField() bool
		// IsCascade reports whether deletes propagate through this relation.
		IsCascade() bool
		// IsVersion reports whether this field implements optimistic-locking version control.
		IsVersion() bool
		// SQLType returns the field's SQL column type.
		SQLType() *SQLType
		// Init initializes the field from a tag context. Called once when the tag is parsed.
		Init(*TTagContext)
		// Base returns the underlying TField, useful for accessing private state.
		Base() *TField

		// Name returns the field's column name in the database.
		Name() string
		// Label returns the human-readable label shown in UI forms.
		Label() string
		// Description returns the long-form help text for the field.
		Description() string
		// TypeName returns the ORM-level type identifier (e.g. "char", "int", "many2one").
		TypeName() string
		// Groups returns the comma-separated permission groups that may access the field.
		Groups() string
		// Readonly returns the readonly flag; when val is supplied, sets it first.
		Readonly(val ...bool) bool
		// Required returns the required-not-null flag; when val is supplied, sets it first.
		Required(val ...bool) bool
		// Searchable returns whether the field can be filtered on; when val is supplied, sets it first.
		Searchable(val ...bool) bool
		// Store returns whether the field is persisted to the database; when val is supplied, sets it first.
		Store(val ...bool) bool
		// Size returns the size constraint (length/precision); when val is supplied, sets it first.
		Size(val ...int) int
		// Default returns the default value; when val is supplied, sets it first.
		Default(val ...any) any
		// DefaultFunc returns the default-value function, or nil if none.
		DefaultFunc() FieldFunc
		// States returns the per-state UI attribute map; when val is supplied, replaces it first.
		States(val ...map[string]interface{}) map[string]interface{}
		// Domain returns the search-domain expression scoped to this field.
		Domain() string
		// Translate reports whether the field's value is translatable.
		Translate() bool
		// SearchOnSelf reports whether searches on the related field may be performed on self.
		SearchOnSelf() bool
		// OutputAs returns the type identifier the value is coerced to on read (char/int/bool/...).
		OutputAs() string
		// UpdateDb writes any schema changes implied by the field to the database.
		UpdateDb(ctx *TTagContext)
		// Attributes returns a map describing the field's published attributes.
		Attributes(ctx *TTagContext) map[string]interface{}
		// SetOutputAs sets the output coercion type identifier.
		SetOutputAs(dataType string)
		// SetName overrides the field's database column name.
		SetName(name string)
		// SetModelName overrides the model name this field belongs to.
		SetModelName(name string)
		// SetModel binds the field to its owner model.
		SetModel(IModel)
		// SetBase replaces the underlying TField in-place (used by subclasses during init).
		SetBase(field *TField)
		// Getter returns the name of the registered getter method, or "" if none.
		Getter() string
		// GetterFunc invokes the registered getter function in the given context.
		GetterFunc(*TFieldContext) error
		// Setter returns the name of the registered setter method, or "" if none.
		Setter() string
		// SetterFunc invokes the registered setter function in the given context.
		SetterFunc(*TFieldContext) error
		// FormatChar returns the printf-style format placeholder used to render the value (e.g. "%s").
		FormatChar() string
		// FormatFunc returns the optional formatter that post-processes the placeholder output.
		FormatFunc() func(string) string
		// ModelName returns the name of the model this field belongs to.
		ModelName() string
		// RelatedModelName returns the name of the related model (for relational fields).
		RelatedModelName() string
		// RelatedKeyName returns the related table's primary-key field name.
		RelatedKeyName() string
		// JoinSourceKey returns the source-side foreign key in an M2M join table.
		JoinSourceKey() string
		// JoinModelName returns the M2M join table's model name.
		JoinModelName() string
		// OneToManyFK returns the inverse foreign-key field name for one-to-many relations.
		OneToManyFK() string
		// IsIndexed reports whether the database has an index on this field.
		IsIndexed() bool
		// IsRelated returns the related-field flag; when arg is supplied, sets it first.
		IsRelated(arg ...bool) bool
		// IsInherited returns the inherits-field flag; when arg is supplied, sets it first.
		IsInherited(arg ...bool) bool
		// IsAutoJoin reports whether the ORM auto-joins this relation in queries.
		IsAutoJoin() bool
		// HasGetter reports whether a custom getter is registered.
		HasGetter() bool
		// HasSetter reports whether a custom setter is registered.
		HasSetter() bool
		// UseAttachment reports whether the field's value is stored in the attachment table rather than inline.
		UseAttachment() bool

		// OnRead is fired when the field's raw value is read from the database.
		OnRead(ctx *TFieldContext) error
		// OnWrite is fired when the field's raw value is about to be written to the database.
		OnWrite(ctx *TFieldContext) error

		// onConvertToRead converts a raw database value to the field's read-format value.
		onConvertToRead(session *TSession, cols []string, record []interface{}, colIndex int) interface{}
		// onConvertToWrite converts an in-memory value to the field's database-format value.
		onConvertToWrite(session *TSession, value interface{}) interface{}
	}

	TField struct {
		// 共同属性
		isPrimaryKey    bool //
		isCompositeKey  bool
		isAutoIncrement bool
		isUnique        bool
		isDBColumn      bool
		isCreatedAt       bool //# 时间字段自动更新日期
		isUpdatedAt       bool //
		isDeletedAt       bool
		isCascade       bool
		isVersion       bool
		isNameField         bool
		hasGetter       bool
		hasSetter       bool

		// SQL属性
		SqlType SQLType
		MapType FieldAccessMode
		IsJSON bool
		EnumOptions     map[string]int
		SetOptions      map[string]int
		DisableTimeZone bool
		TimeZone        *time.Location // column specified time zone

		formatChar             string              // Format 符号 "%s,%d..."
		formatFunc             func(string) string // Format 自定义函数
		autoJoin            bool                //
		inheritAllFields              bool                // 是否继承该字段指向的Model的多有字段
		tagArgs                 map[string]string   // [Tag]val 里的参数
		setupStage           string              // 字段安装完成步骤 Base,full
		isInherited      bool                // 该字段是否关联表的字段 relate
		isRelated        bool                // 该字段是否关联表的外键字段
		isAutoCreated         bool                // 是否是自动创建的字段 ("magic" field)
		modelName            string              // 字段所在的模型名称
		relatedModelName    string              // 连接的数据模型 关联字段的模型名称 字段关联的Model # name of the model of values (if relational)
		relatedKeyName string              // 关联字段所在的表的主键
		joinModelName     string              // 关系表数据模型 字段关联的Model和字段的many2many关系表Model
		joinSourceKey  string              // M2M 表示源表(modelName)在中间表中关联字段的关联字段名，即源表主键字段
		isIndexed             bool                // whether the field is indexed in database
		searchOnSelf          bool                // allow searching on self only if the related field is searchable
		translatable          bool                //???
		outputAs              string              //值将作为[char,int,bool]被转换

		// published exportable
		name              string                 // name of the field
		store             bool                   // # 字段值是否保存到数据库
		manual            bool                   //
		depends           []string               //
		readonly          bool                   // 只读
		writeonly         bool                   // 只读
		required          bool                   // 字段不为空
		description              string                 //
		label             string                 // 字段的Title
		size              int                    // 考虑废弃 长度大小
		sortable          bool                   // 可排序
		searchable        bool                   //
		typeName              string                 // #字段类型 最终存于dataset数据类型view
		defaultValue           string                 /* 存储默认值字符串 */ // default(recs) returns the default value
		relatedPath           string                 // ???
		relationModel          string                 // 关系表
		uiStates            map[string]interface{} // 传递 UI 属性
		selection         [][]string             //
		companyDependent bool                   // ???
		changeDefault    bool                   // ???
		domain            string
		permissionGroups            string //???// private membership
		deprecatedNote          string //???
		onDelete                string // 当这个字段指向的资源删除时将发生。预定义值：cascade，set null，restrict，no action，set default。默认值：set null

		inverseHandler    interface{} // ??? 函数,handler #是一个允许设置这个字段值的函数或方法。
		computeGroup  string      //默认为空 参见Model:calendar_attendee - for function field 一个组名。所有的有相同multi参数的字段将在一个单一函数调用中计算
		searchHandler string      //允许你在这个字段上定义搜索功能
		defaultFunc FieldFunc   //
		// 字段值的计算函数，默认的，计算的字段不会存到数据库中，解决方法是使用store=True属性存储该字段函数必须是Model的 document = fields.Char(compute='_get_document', inverse='_set_document')
		setterFunc   FieldFunc // 写入计算格式化函数
		getterFunc   FieldFunc // 读取计算格式化函数
		boundModel    any       // 提供给compute使用
		setterMethod  string    // 写入计算格式化函数
		getterMethod  string    // 读取计算格式化函数
		computeDepends      []string  // 约束 compute 计算依赖哪些字段来触发
		computeAsAdmin bool      //# whether field should be recomputed as admin		_related       string      //nickname = fields.Char(related='user_id.partner_id.name', store=True)
		previousName      string    //# the previous name of this field, so that ORM can rename it automatically at migration

		// # one2many
		oneToManyFK string

		// # many2many field limit
		m2mLimit int64

		// # binary
		useAttachmentStore bool
	}

	// TRelatedField 描述 inherits 继承字段的映射：
	// 当前 model 上出现的某个字段实际来自父 model 的哪个字段。
	TRelatedField struct {
		name             string // 当前 model 中的字段名
		RelatedTableName string // 关联表（父 model）名
		RelatedFieldName string // 关联表中的目标字段名
		RelatedField     IField // 关联到的实际 IField 实例
		RelatedRootModel string // 多级 inherits 时最顶层的 model 名
	}

	TFieldValue struct {
		Name      string
		Value     any
		Queryable bool // 是否可以查询
	}
)

// Register makes a log provide available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func RegisterField(type_name string, creator func() IField) {
	type_name = strings.ToLower(type_name)
	if creator == nil {
		panic("orm: RegisterField creator is nil")
	}
	if _, dup := field_creators[type_name]; dup {
		panic("orm: RegisterField called twice for field type: " + type_name)
	}
	field_creators[type_name] = creator
}

func FieldTypes() []string {
	types := make([]string, 0)
	for k := range field_creators {
		types = append(types, k)
	}
	return types
}

func newBaseField(name string, opts ...FieldOption) *TField {
	field := &TField{
		//defaultIsEmpty: true,
		formatChar:        "%s",
		formatFunc:        _FieldFormat,
		name:       name,
		store:      false, // 默认必须是False避免于后面Tag冲突
		searchable: true,
		required:   false,
	}

	//cfg := newFieldConfig(field)
	//cfg.Init(opts...)

	for _, opt := range opts {
		opt(field)
	}

	return field
}

func IsFieldType(type_name string) (res bool) {
	_, res = field_creators[type_name]
	return
}

// sqlType:接受数据类型SQLType/string
func NewField(name string, opts ...FieldOption) (IField, error) {
	/* 创建基础Field */
	baseField := newBaseField(name, opts...)

	/* 根据orm数据类型创建Field */
	var field IField
	fieldType := baseField.TypeName()
	if fieldType != "" {
		creator, ok := field_creators[fieldType]
		if !ok {
			return nil, fmt.Errorf("Unknown adapter name %q (forgot to import?)", fieldType)
		}
		field = creator()
	} else {
		switch baseField.SQLType().Name {
		case Bool, Boolean:
			fieldType = "bool"
		case Bit, TinyInt, SmallInt, MediumInt, Int, Integer, Serial, BigInt, UnsignedBigInt, BigSerial:
			fieldType = "int"
		case Float, Real:
			fieldType = "float"
		case Double:
			fieldType = "double"
		case Char, NChar, TinyText, Enum, Set:
			fieldType = "char"
		case Decimal, Numeric:
			fieldType = "char"
		case Varchar, NVarchar, Uuid:
			fieldType = "varchar"
		case Text, MediumText, LongText, Clob:
			fieldType = "text"
		case DateTime, Date, Time, TimeStamp, TimeStampz:
			fieldType = "datetime"
		case TinyBlob, Blob, LongBlob, Bytea, Binary, MediumBlob, VarBinary:
			fieldType = "binary"
		}

		creator, ok := field_creators[fieldType]
		if !ok {
			return nil, errors.New("could not create this new field " + name)
		}
		field = creator()
	}

	field.SetBase(baseField)
	/*
		var type_name string
		var new_field *TField

		switch v := sqlType.(type) {
		case string:
			type_name = strings.ToLower(v)
			new_field = newBaseField(name, SQLType{Name: type_name})
		case SQLType:
			type_name = strings.ToLower(v.Name)
			new_field = newBaseField(name, SQLType{Name: type_name})
			new_field.SqlType = v
		}

		var orm_type_name string
		v := strings.ToUpper(type_name)
		switch v {
		case Bool:
			orm_type_name = "bool"
		case Bit, TinyInt, SmallInt, MediumInt, Int, Integer, Serial:
			orm_type_name = "int"
		case BigInt, BigSerial:
			orm_type_name = "bigint"
		case Float, Real:
			orm_type_name = "float"
		case Double:
			orm_type_name = "double"
		case Char, Varchar, NVarchar, TinyText, Enum, Set, Uuid, Clob:
			orm_type_name = "char"
		case Decimal, Numeric:
			orm_type_name = "char"
		case Varchar:
			orm_type_name = "vchar"
		case Text, MediumText, LongText:
			orm_type_name = "text"
		case DateTime, Date, Time, TimeStamp, TimeStampz:
			orm_type_name = "datetime"
		case TinyBlob, Blob, LongBlob, Bytea, Binary, MediumBlob, VarBinary:
			orm_type_name = "binary"
		default:
			orm_type_name = v
		}

		if orm_type_name == "" {
			log.Errf("the sqltype %v is not supported!", sqlType)
			return nil
		}

		creator, ok := field_creators[orm_type_name]
		if !ok {
			log.Errf("cache: unknown adapter name %q (forgot to import?)", name)
			return nil
		}

		fld := creator()
		fld.SetBase(new_field)
	*/
	return field, nil
}

func NewRelatedField(name, relatedTable, relatedField string, field IField, rootModel string) *TRelatedField {
	return &TRelatedField{
		name:             name,
		RelatedTableName: relatedTable,
		RelatedFieldName: relatedField,
		RelatedField:     field,
		RelatedRootModel: rootModel,
	}
}

func _FieldFormat(str string) string {
	return str
}

func _CharFormat(str string) string {
	return str //`'` + str + `'`
}

func (self *TField) Description() string {
	return self.description
}

func (self *TField) ModelName() string {
	return self.modelName
}

func (self *TField) RelatedKeyName() string {
	return self.relatedKeyName
}

func (self *TField) JoinSourceKey() string {
	return self.joinSourceKey
}

// 字段关联的表
func (self *TField) RelatedModelName() string {
	return self.relatedModelName
}

// 多对多关系中 记录2表记录关联关系的表
func (self *TField) JoinModelName() string {
	return self.joinModelName
}

func (self *TField) Groups() string {
	return self.permissionGroups
}
func (self *TField) Readonly(val ...bool) bool {
	if len(val) > 0 {
		self.readonly = val[0]
	}
	return self.readonly
}

func (self *TField) Required(val ...bool) bool {
	if len(val) > 0 {
		self.required = val[0]
	}
	return self.required
}

func (self *TField) Searchable(val ...bool) bool {
	if len(val) > 0 {
		self.searchable = val[0]
	}
	return self.searchable
}

// orm field type
func (self *TField) TypeName() string             { return self.typeName }
func (self *TField) OutputAs() string             { return self.outputAs }
func (self *TField) SetOutputAs(dataType string)  { self.outputAs = dataType }

// database sql field type
func (self *TField) SQLType() *SQLType               { return &self.SqlType }
func (self *TField) OneToManyFK() string             { return self.oneToManyFK }
func (self *TField) FormatChar() string              { return self.formatChar }
func (self *TField) FormatFunc() func(string) string { return self.formatFunc }
func (self *TField) Label() string                   { return self.label }
func (self *TField) Translate() bool                 { return self.translatable }
func (self *TField) Getter() string                  { return self.getterMethod }
func (self *TField) Setter() string                  { return self.setterMethod }

func (self *TField) GetterFunc(ctx *TFieldContext) error {
	return self.getterFunc(ctx)
}

func (self *TField) SetterFunc(ctx *TFieldContext) error {
	return self.setterFunc(ctx)
}

func (self *TField) Store(val ...bool) bool {
	if len(val) > 0 {
		self.store = val[0]
	}

	return self.store
}

func (self *TField) Default(val ...any) any {
	if len(val) > 0 {
		self.defaultValue = utils.ToString(val[0])
	}

	return self.defaultValue
}

func (self *TField) DefaultFunc() FieldFunc {
	return self.defaultFunc
}

func (self *TField) Size(val ...int) int {
	if len(val) > 0 {
		self.size = val[0]
	}
	return self.size
}

func (self *TField) States(val ...map[string]interface{}) map[string]interface{} {
	if len(val) > 0 {
		self.uiStates = val[0]
	}
	return self.uiStates
}

func (self *TField) SearchOnSelf() bool {
	return self.searchOnSelf
}

func (self *TField) __IsClassicRead() bool {
	return false //self._classic_read
}

func (self *TField) __IsClassicWrite() bool {
	return false //self._classic_write
}

func (self *TField) IsIndexed() bool {
	return self.isIndexed
}

// FuncMultiName 当前未被调用，保留兼容
func (self *TField) FuncMultiName() string {
	return self.computeGroup
}

func (self *TField) InverseHandler() interface{} {
	return self.inverseHandler
}

// 该字段是不是指向其他model的id
func (self *TField) IsRelated(arg ...bool) bool {
	if len(arg) > 0 {
		self.isRelated = arg[0]
	}
	return self.isRelated
}

func (self *TField) IsPrimaryKey() bool {
	return self.isPrimaryKey
}

// 是复合主键
func (self *TField) IsCompositeKey() bool {
	return self.isCompositeKey

}

func (self *TField) IsAutoIncrement() bool {
	return self.isAutoIncrement
}

func (self *TField) IsDefaultEmpty() bool {
	return self.defaultValue == "" && self.defaultFunc == nil
}

func (self *TField) IsUnique() bool {
	return self.isUnique
}

func (self *TField) IsCreatedAt() bool {
	return self.isCreatedAt
}

func (self *TField) IsDeletedAt() bool {
	return self.isDeletedAt
}

func (self *TField) IsUpdatedAt() bool {
	return self.isUpdatedAt
}

func (self *TField) IsCascade() bool {
	return self.isCascade
}

func (self *TField) IsVersion() bool {
	return self.isVersion
}

func (self *TField) HasGetter() bool {
	return self.hasGetter
}
func (self *TField) HasSetter() bool {
	return self.hasSetter
}

func (self *TField) IsNameField() bool {
	return self.isNameField
}

func (self *TField) IsInherited(arg ...bool) bool {
	if len(arg) > 0 {
		self.isInherited = arg[0]
	}
	return self.isInherited
}

func (self *TField) UseAttachment() bool {
	return self.useAttachmentStore
}

func (self *TField) IsAutoJoin() bool {
	return self.autoJoin
}

// 复制一个新的一样的
func (self *TField) New() (res *TField) {
	res = &TField{}
	*res = *self
	return
}

func (self *TField) Init(ctx *TTagContext) {

}

// 返回原型
func (self *TField) Base() *TField {
	return self
}

func (self *TField) SetBase(f *TField) {
	*self = *f
}

func (self *TField) Name() string {
	return self.name
}

func (self *TField) Domain() string {
	return self.domain
}

func (self *TField) RelationModel() string {
	return self.relationModel
}
func (self *TField) SetName(name string) {
	self.name = name
}

func (self *TField) SetModelName(name string) {
	self.modelName = name
}

func (self *TField) SetModel(model IModel) {
	self.boundModel = model
}

// 重载
func (self *TField) UpdateDb(ctx *TTagContext) {
}

// “”” Return a dictionary that describes the field “self”. “””
// 返回字段自己 补充部分属性值
// func (self *TField) GetDescription() (res *TField) {
func (self *TField) Attributes(ctx *TTagContext) map[string]interface{} {
	return map[string]interface{}{
		"name":       self.name,
		"store":      self.store,
		"manual":     self.manual,
		"depends":    self.depends,
		"readonly":   self.readonly,
		"required":   self.required,
		"help":       self.description,
		"string":     self.label,
		"size":       self.size,
		"sortable":   self.sortable,
		"searchable": self.searchable,
		"type":       self.typeName,
		"default":    self.defaultValue,
		"related":    self.relatedPath,
		"states":     self.uiStates,
		"selection":  self.selection,
		"groups":     self.permissionGroups,
		"domain":     self.domain,
		"index":      self.isIndexed,
	}
}

func (self *TField) SetAttributes(name string) {

}

// 转换值到字段输出数据类型
func (self *TField) onConvertToRead(session *TSession, cols []string, record []interface{}, colIndex int) interface{} {
	value := *record[colIndex].(*interface{})
	return value2FieldTypeValue(self, value)

}

// 转换值到字段数据库类型
func (self *TField) onConvertToWrite(session *TSession, value interface{}) interface{} {
	return value2SqlTypeValue(self, value)
}

/*
""" Convert “value“ from the record format to the format returned by
method :meth:`BaseModel.read`.

:param bool use_name_get: when True, the value's display name will be

	computed using :meth:`BaseModel.name_get`, if relevant for the field

"""
*/
func (self *TField) OnRead(ctx *TFieldContext) error {
	model := ctx.Model
	field := self
	if field.hasGetter {
		if mehodName := field.getterMethod; mehodName != "" {
			// TODO 同一记录方法到OBJECT里使用Method
			if method := model.GetBase().modelValue.MethodByName(mehodName); method.IsValid() {
				args := make([]reflect.Value, 0)
				args = append(args, reflect.ValueOf(ctx))
				results := method.Call(args) //
				if len(results) == 1 {
					//fld.Selection, _ = results[0].Interface().([][]string)
					// 返回结果nil或者错误
					if err, ok := results[0].Interface().(error); ok && err != nil {
						return err
					}
				}
			}
		}
	}

	return nil
}

/*
""" Convert “value“ from the record format to the format of method
:meth:`BaseModel.write`.
"""
*/
func (self *TField) OnWrite(ctx *TFieldContext) error {
	field := self
	if field.hasSetter {
		if err := field.SetterFunc(ctx); err != nil {
			return err
		}

		/*
			model := ctx.Model
			if mehodName := field.setterMethod; mehodName != "" {
				// TODO 同一记录方法到OBJECT里使用Method
				if method := model.GetBase().modelValue.MethodByName(mehodName); method.IsValid() {
					args := make([]reflect.Value, 0)
					args = append(args, reflect.ValueOf(ctx))
					results := method.Call(args) //
					if len(results) == 1 {
						//fld.Selection, _ = results[0].Interface().([][]string)
						// 返回结果nil或者错误
						if err, ok := results[0].Interface().(error); ok && err != nil {
							return err
						}
					}
				}
			}*/
	} else {
		/* 默认返回值不变 */
		ctx.SetValue(ctx.Value)
	}

	return nil
}

func (self *TFieldContext) SetValue(v any) error {
	self.values = v
	return nil
}
