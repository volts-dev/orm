package orm

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"errors"

	"github.com/volts-dev/dataset"
)

// FieldAccessMode 字段读写模式
type FieldAccessMode int

const (
	ReadWrite FieldAccessMode = iota + 1 // 双向同步
	WriteOnly                            // 仅写入数据库
	ReadOnly                             // 仅从数据库读取
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
		isPrimaryKey    bool // 是否为主键字段
		isCompositeKey  bool // 是否为复合主键的组成字段
		isAutoIncrement bool // 数据库是否自动递增此字段
		isUnique        bool // 是否带 UNIQUE 约束
		isDBColumn      bool // 是否对应一个真实的数据库列
		isCreatedAt     bool // 是否为记录创建时间戳字段
		isUpdatedAt     bool // 是否为记录最后更新时间戳字段
		isDeletedAt     bool // 是否为软删除时间戳字段
		isCascade       bool // 删除时是否级联到关联记录
		isVersion       bool // 是否参与乐观锁版本控制
		isNameField     bool // 是否为 model 的展示名字段
		hasGetter       bool // 是否注册了自定义读取函数
		hasSetter       bool // 是否注册了自定义写入函数

		SqlType         SQLType
		MapType         FieldAccessMode
		IsJSON          bool
		EnumOptions     map[string]int
		SetOptions      map[string]int
		DisableTimeZone bool
		TimeZone        *time.Location // column specified time zone

		formatChar  string              // printf 风格的格式占位符（如 "%s"）
		formatFunc  func(string) string // 对格式化后字符串再加工的函数
		autoJoin    bool                // 查询时是否自动 JOIN 关联表
		isInherited bool                // 该字段是否来自 inherits 继承
		isRelated   bool                // 该字段是否指向其他 model 的外键
		modelName   string              // 当前字段所属 model 的名字
		relatedModelName string              // 关联到的 model 名
		relatedKeyName   string              // 关联表的主键字段名
		joinModelName    string              // M2M 连接表对应的 model 名
		joinSourceKey    string              // 在 M2M 连接表中指向源 model 主键的字段名
		isIndexed        bool                // 数据库是否对此字段建索引
		searchOnSelf     bool                // 关联字段是否允许在 self 上直接搜索
		translatable     bool                // 字段值是否可翻译
		outputAs         string              // 读取时将值伪装为哪种类型（char/int/bool 等）

		name             string                 // 字段在数据库中的列名
		store            bool                   // 是否将字段值持久化到数据库
		manual           bool                   // 是否手动管理（非框架自动）
		depends          []string               // 依赖的其他字段名列表
		readonly         bool                   // 是否只读
		writeonly        bool                   // 是否只写
		required         bool                   // 是否非空
		description      string                 // 字段的长描述（help text）
		label            string                 // 字段在 UI 表单中显示的名字
		size             int                    // 长度或精度约束
		sortable         bool                   // 是否可排序
		searchable       bool                   // 是否可搜索
		typeName         string                 // ORM 层字段类型标识（最终存入 dataset）
		defaultValue     any                    // 默认值字符串
		relatedPath      string                 // 关联路径表达式（用途未确认，保留兼容）
		relationModel    string                 // 关联到的 model 名（与 relatedModelName 用途略有差异）
		uiStates         map[string]interface{} // 传递给 UI 的 state 属性 map
		selection        [][]string             // selection 字段的可选值列表
		domain           string                 // 字段级的搜索域表达式
		permissionGroups string                 // 限制访问该字段的权限组名（逗号分隔）
		onDelete         string                 // 关联记录被删除时的处理策略（cascade/set null/restrict/...）

		inverseHandler interface{} // 计算字段的反向写入处理器
		computeGroup   string      // 同一 compute group 的字段在单次调用中一起计算
		defaultFunc    FieldFunc   // 默认值生成函数
		setterFunc     FieldFunc   // 写入计算/格式化函数
		getterFunc     FieldFunc   // 读取计算/格式化函数
		boundModel     any         // 绑定的 model 引用（供 compute 使用）
		setterMethod   string      // 写入方法名
		getterMethod   string      // 读取方法名
		computeAsAdmin bool        // 计算字段是否以 admin 权限重新计算

		oneToManyFK        string // one-to-many 关系中对端的外键字段名
		m2mLimit           int64  // many-to-many 关系单次取回的上限
		useAttachmentStore bool   // 值是否存入 attachment 表（适合大字段）
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
		formatChar: "%s",
		formatFunc: _FieldFormat,
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

// _FieldFormat is the identity field formatter.
func _FieldFormat(str string) string {
	return str
}

// _CharFormat is the identity char formatter (previously wrapped values in single quotes).
func _CharFormat(str string) string {
	return str //`'` + str + `'`
}

// Description returns the long-form help text for the field.
func (self *TField) Description() string {
	return self.description
}

// ModelName returns the name of the model this field belongs to.
func (self *TField) ModelName() string {
	return self.modelName
}

// RelatedKeyName returns the related table's primary-key field name.
func (self *TField) RelatedKeyName() string {
	return self.relatedKeyName
}

// JoinSourceKey returns the source-side foreign key in an M2M join table.
func (self *TField) JoinSourceKey() string {
	return self.joinSourceKey
}

// RelatedModelName returns the name of the related model (for relational fields).
func (self *TField) RelatedModelName() string {
	return self.relatedModelName
}

// JoinModelName returns the M2M join table's model name.
func (self *TField) JoinModelName() string {
	return self.joinModelName
}

// Groups returns the comma-separated permission groups that may access the field.
func (self *TField) Groups() string {
	return self.permissionGroups
}

// Readonly returns the readonly flag; when val is supplied, sets it first.
func (self *TField) Readonly(val ...bool) bool {
	if len(val) > 0 {
		self.readonly = val[0]
	}
	return self.readonly
}

// Required returns the required-not-null flag; when val is supplied, sets it first.
func (self *TField) Required(val ...bool) bool {
	if len(val) > 0 {
		self.required = val[0]
	}
	return self.required
}

// Searchable returns whether the field can be filtered on; when val is supplied, sets it first.
func (self *TField) Searchable(val ...bool) bool {
	if len(val) > 0 {
		self.searchable = val[0]
	}
	return self.searchable
}

// TypeName returns the ORM-level type identifier (e.g. "char", "int", "many2one").
func (self *TField) TypeName() string { return self.typeName }

// OutputAs returns the type identifier the value is coerced to on read (char/int/bool/...).
func (self *TField) OutputAs() string { return self.outputAs }

// SetOutputAs sets the output coercion type identifier.
func (self *TField) SetOutputAs(dataType string) { self.outputAs = dataType }

// SQLType returns the field's SQL column type.
func (self *TField) SQLType() *SQLType { return &self.SqlType }

// OneToManyFK returns the inverse foreign-key field name for one-to-many relations.
func (self *TField) OneToManyFK() string { return self.oneToManyFK }

// FormatChar returns the printf-style format placeholder used to render the value (e.g. "%s").
func (self *TField) FormatChar() string { return self.formatChar }

// FormatFunc returns the optional formatter that post-processes the placeholder output.
func (self *TField) FormatFunc() func(string) string { return self.formatFunc }

// Label returns the human-readable label shown in UI forms.
func (self *TField) Label() string { return self.label }

// Translate reports whether the field's value is translatable.
func (self *TField) Translate() bool { return self.translatable }

// Getter returns the name of the registered getter method, or "" if none.
func (self *TField) Getter() string { return self.getterMethod }

// Setter returns the name of the registered setter method, or "" if none.
func (self *TField) Setter() string { return self.setterMethod }

// GetterFunc invokes the registered getter function in the given context.
func (self *TField) GetterFunc(ctx *TFieldContext) error {
	return self.getterFunc(ctx)
}

// SetterFunc invokes the registered setter function in the given context.
func (self *TField) SetterFunc(ctx *TFieldContext) error {
	return self.setterFunc(ctx)
}

// Store returns whether the field is persisted to the database; when val is supplied, sets it first.
func (self *TField) Store(val ...bool) bool {
	if len(val) > 0 {
		self.store = val[0]
	}

	return self.store
}

// Default returns the default value; when val is supplied, sets it first.
func (self *TField) Default(val ...any) any {
	if len(val) > 0 {
		self.defaultValue = val[0]
	}

	return self.defaultValue
}

// DefaultFunc returns the default-value function, or nil if none.
func (self *TField) DefaultFunc() FieldFunc {
	return self.defaultFunc
}

// Size returns the size constraint (length/precision); when val is supplied, sets it first.
func (self *TField) Size(val ...int) int {
	if len(val) > 0 {
		self.size = val[0]
	}
	return self.size
}

// States returns the per-state UI attribute map; when val is supplied, replaces it first.
func (self *TField) States(val ...map[string]interface{}) map[string]interface{} {
	if len(val) > 0 {
		self.uiStates = val[0]
	}
	return self.uiStates
}

// SearchOnSelf reports whether searches on the related field may be performed on self.
func (self *TField) SearchOnSelf() bool {
	return self.searchOnSelf
}

// IsIndexed reports whether the database has an index on this field.
func (self *TField) IsIndexed() bool {
	return self.isIndexed
}

// FuncMultiName returns the compute-group name. Currently unused, kept for compatibility.
func (self *TField) FuncMultiName() string {
	return self.computeGroup
}

// InverseHandler returns the inverse compute handler, if registered.
func (self *TField) InverseHandler() interface{} {
	return self.inverseHandler
}

// IsRelated returns the related-field flag; when arg is supplied, sets it first.
func (self *TField) IsRelated(arg ...bool) bool {
	if len(arg) > 0 {
		self.isRelated = arg[0]
	}
	return self.isRelated
}

// IsPrimaryKey reports whether this field is the model's primary key.
func (self *TField) IsPrimaryKey() bool {
	return self.isPrimaryKey
}

// IsCompositeKey reports whether this field is part of a composite primary key.
func (self *TField) IsCompositeKey() bool {
	return self.isCompositeKey
}

// IsAutoIncrement reports whether the database assigns this field's value automatically.
func (self *TField) IsAutoIncrement() bool {
	return self.isAutoIncrement
}

// IsDefaultEmpty reports whether the field has no default value (literal or function).
func (self *TField) IsDefaultEmpty() bool {
	return self.defaultValue == nil && self.defaultFunc == nil
}

// IsUnique reports whether the field has a UNIQUE constraint.
func (self *TField) IsUnique() bool {
	return self.isUnique
}

// IsCreatedAt reports whether this field stores the record's creation timestamp.
func (self *TField) IsCreatedAt() bool {
	return self.isCreatedAt
}

// IsDeletedAt reports whether this field stores the soft-delete timestamp.
func (self *TField) IsDeletedAt() bool {
	return self.isDeletedAt
}

// IsUpdatedAt reports whether this field stores the record's last-update timestamp.
func (self *TField) IsUpdatedAt() bool {
	return self.isUpdatedAt
}

// IsCascade reports whether deletes propagate through this relation.
func (self *TField) IsCascade() bool {
	return self.isCascade
}

// IsVersion reports whether this field implements optimistic-locking version control.
func (self *TField) IsVersion() bool {
	return self.isVersion
}

// HasGetter reports whether a custom getter is registered.
func (self *TField) HasGetter() bool {
	return self.hasGetter
}

// HasSetter reports whether a custom setter is registered.
func (self *TField) HasSetter() bool {
	return self.hasSetter
}

// IsNameField reports whether this field is the model's display-name column.
func (self *TField) IsNameField() bool {
	return self.isNameField
}

// IsInherited returns the inherits-field flag; when arg is supplied, sets it first.
func (self *TField) IsInherited(arg ...bool) bool {
	if len(arg) > 0 {
		self.isInherited = arg[0]
	}
	return self.isInherited
}

// UseAttachment reports whether the field's value is stored in the attachment table rather than inline.
func (self *TField) UseAttachment() bool {
	return self.useAttachmentStore
}

// IsAutoJoin reports whether the ORM auto-joins this relation in queries.
func (self *TField) IsAutoJoin() bool {
	return self.autoJoin
}

// New returns a shallow copy of the field.
func (self *TField) New() (res *TField) {
	res = &TField{}
	*res = *self
	return
}

// Init initializes the field from a tag context. Called once when the tag is parsed.
func (self *TField) Init(ctx *TTagContext) {

}

// Base returns the underlying TField, useful for accessing private state.
func (self *TField) Base() *TField {
	return self
}

// SetBase replaces the underlying TField in-place (used by subclasses during init).
func (self *TField) SetBase(f *TField) {
	*self = *f
}

// Name returns the field's column name in the database.
func (self *TField) Name() string {
	return self.name
}

// Domain returns the search-domain expression scoped to this field.
func (self *TField) Domain() string {
	return self.domain
}

// RelationModel returns the relation model name.
func (self *TField) RelationModel() string {
	return self.relationModel
}

// SetName overrides the field's database column name.
func (self *TField) SetName(name string) {
	self.name = name
}

// SetModelName overrides the model name this field belongs to.
func (self *TField) SetModelName(name string) {
	self.modelName = name
}

// SetModel binds the field to its owner model.
func (self *TField) SetModel(model IModel) {
	self.boundModel = model
}

// UpdateDb writes any schema changes implied by the field to the database.
func (self *TField) UpdateDb(ctx *TTagContext) {
}

// Attributes returns a map describing the field's published attributes.
func (self *TField) Attributes(ctx *TTagContext) map[string]interface{} {
	return map[string]interface{}{
		"name":              self.name,
		"store":             self.store,
		"manual":            self.manual,
		"depends":           self.depends,
		"readonly":          self.readonly,
		"required":          self.required,
		"help":              self.description,
		"string":            self.label,
		"size":              self.size,
		"sortable":          self.sortable,
		"searchable":        self.searchable,
		"type":              self.typeName,
		"default":           self.defaultValue,
		"related":           self.relatedPath,
		"states":            self.uiStates,
		"selection":         self.selection,
		"groups":            self.permissionGroups,
		"domain":            self.domain,
		"index":             self.isIndexed,
		"isInherited":       self.isInherited,
		"relationModelName": self.relatedModelName,
		"relationKeyName":   self.relatedKeyName,
	}
}

// SetAttributes is a placeholder for setting field attributes by name.
func (self *TField) SetAttributes(name string) {

}

// 把数据库返回的原始值转换为字段对外暴露的值
func (self *TField) onConvertToRead(session *TSession, cols []string, record []interface{}, colIndex int) interface{} {
	value := *record[colIndex].(*interface{})
	return value2FieldTypeValue(self, value)

}

// 把内存中的值转换为数据库格式
func (self *TField) onConvertToWrite(session *TSession, value interface{}) interface{} {
	return value2SqlTypeValue(self, value)
}

// OnRead is fired when the field's raw value is read from the database.
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

// OnWrite is fired when the field's raw value is about to be written to the database.
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
