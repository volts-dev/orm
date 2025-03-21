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

const (
	// 字段读写模式
	TWOSIDES = iota + 1
	ONLYTODB
	ONLYFROMDB
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
		FieldTypeValue reflect.Value // TODO 废弃
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

	// IField represents a field interface in a data model.
	// It defines the behaviors and properties of a field, including its configuration, constraints, and interactions with other fields or models.
	IField interface {
		Config() *FieldConfig
		//String(d IDialect) string
		IsPrimaryKey() bool
		IsCompositeKey() bool // 是复合主键
		IsAutoIncrement() bool
		IsDefaultEmpty() bool
		IsUnique() bool
		IsCreated() bool
		IsDeleted() bool
		IsUpdated() bool
		IsNamed() bool
		IsCascade() bool
		IsVersion() bool
		SQLType() *SQLType
		Init(*TTagContext) // call when parse the field tag
		Base() *TField     // return itself

		// attributes func
		Name() string // name of field in database
		Title() string
		Help() string
		Type() string //
		Groups() string
		Readonly(val ...bool) bool
		Required(val ...bool) bool
		Searchable(val ...bool) bool
		Store(val ...bool) bool
		Size(val ...int) int
		Default(val ...any) any
		DefaultFunc() FieldFunc
		States(val ...map[string]interface{}) map[string]interface{}
		Domain() string
		Translate() bool
		Search() bool
		As() string // return the type of the value format as
		// 获取Field所有属性值
		UpdateDb(ctx *TTagContext)
		GetAttributes(ctx *TTagContext) map[string]interface{}
		SetAs(dataType string)
		SetName(name string)
		SetModelName(name string)
		SetModel(IModel)
		SetBase(field *TField)
		//ColumnType() string // the sql type
		Getter() string
		GetterFunc(*TFieldContext) error
		Setter() string
		SetterFunc(*TFieldContext) error
		SymbolChar() string
		SymbolFunc() func(string) string
		ModelName() string
		RelatedModelName() string
		RelatedFieldName() string
		MiddleFieldName() string
		MiddleModelName() string // 多对多关系中 记录2表记录关联关系的表
		FieldsId() string
		IsIndex() bool
		//IsRelated() bool
		IsRelatedField(arg ...bool) bool
		IsInheritedField(arg ...bool) bool
		//IsCommonField(arg ...bool) bool
		IsAutoJoin() bool // 自动Join
		HasGetter() bool
		HasSetter() bool
		//IsClassicRead() bool
		//IsClassicWrite() bool

		UseAttachment() bool

		// raw I/O event of field when it be read/write.
		// [原始数据] 处理计算读取数据库的原始数据 将会调用Compute等标签里的函数
		OnRead(ctx *TFieldContext) error // (res map[string]map[string]interface{})         // 字段数据获取
		// [原始数据] 处理计算写入数据库原始数据 将会调用Compute等标签里的函数
		OnWrite(ctx *TFieldContext) error //(res map[string]map[string]interface{}) // 字段数据保存

		// classic I/O event of the field. It will be call when using classic query. READ/WRITE the relate data FROM/TO its relation table
		// the RETURN value is the value of field.
		//[经典数据] 从原始数据转换提供Classical数据读法,数据修剪,Many2One显示Names表等
		onConvertToRead(session *TSession, cols []string, record []interface{}, colIndex int) interface{} // TODO compute
		onConvertToWrite(session *TSession, value interface{}) interface{}                                // TODO 不返回或者返回错误
	}

	TField struct {
		// 共同属性
		isPrimaryKey    bool //
		isCompositeKey  bool
		isAutoIncrement bool
		isUnique        bool
		isColumn        bool
		isCreated       bool //# 时间字段自动更新日期
		isUpdated       bool //
		isDeleted       bool
		isCascade       bool
		isVersion       bool
		isNamed         bool
		hasGetter       bool
		hasSetter       bool

		//defaultIsEmpty bool
		//comment        string
		help string
		//Default        string
		//Length         int
		//Length2        int
		//Nullable       bool
		// SQL属性
		SqlType SQLType
		MapType int
		//Name            string
		//TableName       string
		//FieldName       string
		IsJSON bool
		//Indexes         map[string]int //#
		EnumOptions     map[string]int
		SetOptions      map[string]int
		DisableTimeZone bool
		TimeZone        *time.Location // column specified time zone

		_symbol_c             string              // Format 符号 "%s,%d..."
		_symbol_f             func(string) string // Format 自定义函数
		_auto_join            bool                //
		_inherit              bool                // 是否继承该字段指向的Model的多有字段
		_args                 map[string]string   // [Tag]val 里的参数
		_setup_done           string              // 字段安装完成步骤 Base,full
		isInheritedField      bool                // 该字段是否关联表的字段 relate
		isRelatedField        bool                // 该字段是否关联表的外键字段
		automatic             bool                // 是否是自动创建的字段 ("magic" field)
		model_name            string              // 字段所在的模型名称
		related_model_name    string              // 连接的数据模型 关联字段的模型名称 字段关联的Model # name of the model of values (if relational)
		related_keyfield_name string              // 关联字段所在的表的主键
		middle_model_name     string              // 关系表数据模型 字段关联的Model和字段的many2many关系表Model
		middle_keyfield_name  string              // M2M 表示源表(model_name)在中间表中关联字段的关联字段名，即源表主键字段
		index                 bool                // whether the field is indexed in database
		search                bool                // allow searching on self only if the related field is searchable
		translate             bool                //???
		as                    string              //值将作为[char,int,bool]被转换

		// published exportable
		_attr_name              string                 // name of the field
		_attr_store             bool                   // # 字段值是否保存到数据库
		_attr_manual            bool                   //
		_attr_depends           []string               //
		_attr_readonly          bool                   // 只读
		_attr_writeonly         bool                   // 只读
		_attr_required          bool                   // 字段不为空
		_attr_help              string                 //
		_attr_title             string                 // 字段的Title
		_attr_size              int                    // 考虑废弃 长度大小
		_attr_sortable          bool                   // 可排序
		_attr_searchable        bool                   //
		_attr_type              string                 // #字段类型 最终存于dataset数据类型view
		_attr_default           string                 /* 存储默认值字符串 */ // default(recs) returns the default value
		_attr_related           string                 // ???
		_attr_relation          string                 // 关系表
		_attr_states            map[string]interface{} // 传递 UI 属性
		_attr_selection         [][]string             //
		_attr_company_dependent bool                   // ???
		_attr_change_default    bool                   // ???
		_attr_domain            string
		_attr_groups            string //???// private membership
		deprecated              string //???
		ondelete                string // 当这个字段指向的资源删除时将发生。预定义值：cascade，set null，restrict，no action，set default。默认值：set null

		//# Tag标记变量
		//_column_type string // #存储 column 类型 当该字段值非空时数据将直接存入数据库,而非计算值
		//_func          string      //是一个计算字段值的方法或函数。必须在声明函数字段前声明它。
		_func_inv       interface{} // ??? 函数,handler #是一个允许设置这个字段值的函数或方法。
		_func_multi     string      //默认为空 参见Model:calendar_attendee - for function field 一个组名。所有的有相同multi参数的字段将在一个单一函数调用中计算
		_func_search    string      //允许你在这个字段上定义搜索功能
		_computeDefault FieldFunc   //
		// 字段值的计算函数，默认的，计算的字段不会存到数据库中，解决方法是使用store=True属性存储该字段函数必须是Model的 document = fields.Char(compute='_get_document', inverse='_set_document')
		_setterFunc   FieldFunc // 写入计算格式化函数
		_getterFunc   FieldFunc // 读取计算格式化函数
		_model        any       // 提供给compute使用
		_setter       string    // 写入计算格式化函数
		_getter       string    // 读取计算格式化函数
		_depends      []string  // 约束 compute 计算依赖哪些字段来触发
		_compute_sudo bool      //# whether field should be recomputed as admin		_related       string      //nickname = fields.Char(related='user_id.partner_id.name', store=True)
		_oldname      string    //# the previous name of this field, so that ORM can rename it automatically at migration

		// # one2many
		_fields_id string

		// # many2many field limit
		limit int64

		// # binary
		attachment bool
	}

	TRelatedField struct {
		// Mapping from inherits'd field name to triple (m, r, f, n) where
		// m is the model from which it is inherits'd,
		// r is the (local) field towards m,
		// f is the _column object itself,
		// n is the original (i.e. top-most) parent model.
		// Example:
		//  { 'field_name': ('parent_model', 'm2o_field_to_reach_parent',
		//                   field_column_obj, origina_parent_model), ... }
		name string
		//ParentModel         string // 继承至哪个
		//ParentM2OField      string // 外键 m2o_field_to_reach_parent
		//FieldColumn         *TField
		//OriginalParentModel *TModel // 最底层的Model
		RelateTableName   string //idx:0 TODO Table改为Model
		RelateFieldName   string //idx:1
		RelateField       IField //idx:2
		RelateTopestTable string //idx:3 //关联字段由那个表产生
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
		panic("logs: Register provide is nil")
	}
	if _, dup := field_creators[type_name]; dup {
		panic("logs: Register called twice for provider name:" + type_name)
	}
	field_creators[type_name] = creator
}

func FieldTypes() []string {
	types := make([]string, 0)
	for k, _ := range field_creators {
		types = append(types, k)
	}
	return types
}

func newBaseField(name string, opts ...FieldOption) *TField {
	field := &TField{

		//defaultIsEmpty: true,
		_symbol_c:        "%s",
		_symbol_f:        _FieldFormat,
		_attr_name:       name,
		_attr_store:      false, // 默认必须是False避免于后面Tag冲突
		_attr_searchable: true,
		_attr_required:   false,
	}

	cfg := newFieldConfig(field)
	cfg.Init(opts...)
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
	fieldType := baseField.Type()
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

// TODO　改名外键
func NewRelateField(aNames string, relate_table_name string, relate_field_name string, aField IField, relate_topest_table string) *TRelatedField {
	return &TRelatedField{
		name:              aNames,
		RelateTableName:   relate_table_name,
		RelateFieldName:   relate_field_name,
		RelateField:       aField,
		RelateTopestTable: relate_topest_table,
	}
}

func _FieldFormat(str string) string {
	return str
}

func _CharFormat(str string) string {
	return str //`'` + str + `'`
}
func (self *TField) Config() *FieldConfig {
	return self.Config()
}
func (self *TField) Help() string {
	return self._attr_help
}

func (self *TField) ModelName() string {
	return self.model_name
}

// TODO 优化函数名称
func (self *TField) RelatedFieldName() string {
	return self.related_keyfield_name
}

func (self *TField) MiddleFieldName() string {
	return self.middle_keyfield_name
}

// 字段关联的表
func (self *TField) RelatedModelName() string {
	return self.related_model_name
}

// 多对多关系中 记录2表记录关联关系的表
func (self *TField) MiddleModelName() string {
	return self.middle_model_name
}

func (self *TField) Groups() string {
	return self._attr_groups
}
func (self *TField) Readonly(val ...bool) bool {
	if len(val) > 0 {
		self._attr_readonly = val[0]
	}
	return self._attr_readonly
}

func (self *TField) Required(val ...bool) bool {
	if len(val) > 0 {
		self._attr_required = val[0]
	}
	return self._attr_required
}

func (self *TField) Searchable(val ...bool) bool {
	if len(val) > 0 {
		self._attr_required = val[0]
	}
	return self._attr_searchable
}

// orm field type
func (self *TField) Type() string          { return self._attr_type }
func (self *TField) As() string            { return self.as }
func (self *TField) SetAs(dataType string) { self.as = dataType }

// database sql field type
func (self *TField) SQLType() *SQLType               { return &self.SqlType }
func (self *TField) FieldsId() string                { return self._fields_id }
func (self *TField) SymbolChar() string              { return self._symbol_c }
func (self *TField) SymbolFunc() func(string) string { return self._symbol_f }
func (self *TField) Title() string                   { return self._attr_title }
func (self *TField) Translate() bool                 { return self.translate }
func (self *TField) Getter() string                  { return self._getter }
func (self *TField) Setter() string                  { return self._setter }

func (self *TField) GetterFunc(ctx *TFieldContext) error {
	return self._getterFunc(ctx)
}

func (self *TField) SetterFunc(ctx *TFieldContext) error {
	return self._setterFunc(ctx)
}

func (self *TField) Store(val ...bool) bool {
	if len(val) > 0 {
		self._attr_store = val[0]
	}

	return self._attr_store
}

func (self *TField) Default(val ...any) any {
	if len(val) > 0 {
		self._attr_default = utils.ToString(val[0])
	}

	return self._attr_default
}

func (self *TField) DefaultFunc() FieldFunc {
	return self._computeDefault
}

func (self *TField) Size(val ...int) int {
	if len(val) > 0 {
		self._attr_size = val[0]
	}
	return self._attr_size
}

func (self *TField) States(val ...map[string]interface{}) map[string]interface{} {
	if len(val) > 0 {
		self._attr_states = val[0]
	}
	return self._attr_states
}

func (self *TField) Search() bool {
	return self.search
}

func (self *TField) __IsClassicRead() bool {
	return false //self._classic_read
}

func (self *TField) __IsClassicWrite() bool {
	return false //self._classic_write
}

func (self *TField) IsIndex() bool {
	return self.index
}

// TODO 改名称
func (self *TField) FuncMultiName() string {
	return self._func_multi
}

func (self *TField) Fnct_inv() interface{} {
	return self._func_inv
}

// 该字段是不是指向其他model的id
func (self *TField) IsRelatedField(arg ...bool) bool {
	if len(arg) > 0 {
		self.isRelatedField = arg[0]
	}
	return self.isRelatedField
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
	return self._attr_default == "" //&& self._computeDefault == nil
}

func (self *TField) IsUnique() bool {
	return self.isUnique
}

func (self *TField) IsCreated() bool {
	return self.isCreated
}

func (self *TField) IsDeleted() bool {
	return self.isDeleted
}

func (self *TField) IsUpdated() bool {
	return self.isUpdated
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

func (self *TField) IsNamed() bool {
	return self.isNamed
}

func (self *TField) IsInheritedField(arg ...bool) bool {
	if len(arg) > 0 {
		self.isInheritedField = arg[0]
	}
	return self.isInheritedField
}

func (self *TField) UseAttachment() bool {
	return self.attachment
}

func (self *TField) IsAutoJoin() bool {
	return self._auto_join
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
	return self._attr_name
}

func (self *TField) Domain() string {
	return self._attr_domain
}

func (self *TField) Relation() string {
	return self._attr_relation
}
func (self *TField) SetName(name string) {
	self._attr_name = name
}

func (self *TField) SetModelName(name string) {
	self.model_name = name
}

func (self *TField) SetModel(model IModel) {
	self._model = model
}

// 重载
func (self *TField) UpdateDb(ctx *TTagContext) {
}

// """ Return a dictionary that describes the field “self“. """
// 返回字段自己 补充部分属性值
// func (self *TField) GetDescription() (res *TField) {
func (self *TField) GetAttributes(ctx *TTagContext) map[string]interface{} {
	return map[string]interface{}{
		"name":       self._attr_name,
		"store":      self._attr_store,
		"manual":     self._attr_manual,
		"depends":    self._attr_depends,
		"readonly":   self._attr_readonly,
		"required":   self._attr_required,
		"help":       self._attr_help,
		"string":     self._attr_title,
		"size":       self._attr_size,
		"sortable":   self._attr_sortable,
		"searchable": self._attr_searchable,
		"type":       self._attr_type,
		"default":    self._attr_default,
		"related":    self._attr_related,
		"states":     self._attr_states,
		"selection":  self._attr_selection,
		"groups":     self._attr_groups,
		"domain":     self._attr_domain,
		"index":      self.index,
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
		if mehodName := field._getter; mehodName != "" {
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
			if mehodName := field._setter; mehodName != "" {
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
