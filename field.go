package orm

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"volts-dev/dataset"

	"github.com/volts-dev/utils"
)

const (
	// Index
	IndexType = iota + 1
	UniqueType

	// 字段读写模式
	TWOSIDES = iota + 1
	ONLYTODB
	ONLYFROMDB

	FIELD_TYPE_BOOL = "bool"
)

var (
	// 注册的Writer类型函数接口
	field_creators = make(map[string]func() IField)
)

type (
	/*
		// 废弃
		IFieldCtrl interface {
			Write(session *TSession, id string, fields *TField, value string, rel_context map[string]interface{}) interface{} //(res map[string]map[string]interface{}) // 字段数据保存
			Read(session *TSession, field *TField, dataset *dataset.TDataSet, rel_context map[string]interface{}) interface{}         // (res map[string]map[string]interface{})         // 字段数据获取
		}
	*/
	// database index
	TIndex struct {
		IsRegular bool
		Name      string
		Type      int
		Cols      []string
	}

	// The context for Tag
	TFieldContext struct {
		Orm            *TOrm
		Model          IModel // required
		Field          IField // required
		FieldTypeValue reflect.Value
		Params         []string // 属性参数 int(<params>)
	}

	TFieldEventContext struct {
		Session *TSession
		Model   IModel
		//FieldTypeValue reflect.Value
		Field IField
		// the current id of current record
		Id interface{}
		// the current value of the field
		Value   interface{}
		Dataset *dataset.TDataSet // 数据集将被修改
		Context map[string]interface{}
	}

	IField interface {
		String(d IDialect) string
		StringNoPk(d IDialect) string
		IsPrimaryKey() bool
		IsAutoIncrement() bool
		IsCreated() bool
		IsDeleted() bool
		IsUpdated() bool
		IsCascade() bool
		IsVersion() bool
		SQLType() *SQLType
		Init(ctx *TFieldContext) // call when parse the field tag
		Base() *TField           // return itself

		// attributes func
		Name() string // name of field in database
		Type() string //
		Groups() string
		Readonly(val ...bool) bool
		Required(val ...bool) bool
		Searchable(val ...bool) bool
		Store(val ...bool) bool
		Size(val ...int) int
		Default(val ...interface{}) interface{}
		States(val ...map[string]interface{}) map[string]interface{}
		Domain() string
		Translatable() bool
		Search() bool
		Title() string

		// 获取Field所有属性值
		UpdateDb(ctx *TFieldContext)
		GetAttributes(ctx *TFieldContext) map[string]interface{}
		SetName(name string)
		SetModel(name string)
		SetBase(field *TField)
		//ColumnType() string // the sql type
		Compute() string
		SymbolChar() string
		SymbolFunc() func(string) string
		ModelName() string
		RelateModelName() string
		RelateFieldName() string
		MiddleFieldName() string
		MiddleModelName() string // 多对多关系中 记录2表记录关联关系的表
		FieldsId() string
		IsIndex() bool
		//IsRelated() bool
		IsRelatedField(arg ...bool) bool
		IsInheritedField(arg ...bool) bool
		//IsCommonField(arg ...bool) bool
		IsAutoJoin() bool
		//IsClassicRead() bool
		//IsClassicWrite() bool

		UseAttachment() bool

		// raw I/O event of field when it be read/write.
		// [原始数据] 处理读取数据库的原始数据
		OnRead(ctx *TFieldEventContext) error // (res map[string]map[string]interface{})         // 字段数据获取
		// [原始数据] 处理写入数据库原始数据
		OnWrite(ctx *TFieldEventContext) error //(res map[string]map[string]interface{}) // 字段数据保存

		// classic I/O event of the field. It will be call when using classic query. READ/WRITE the relate data FROM/TO its relation table
		// the RETURN value is the value of field.
		//[经典数据] 从原始数据转换提供Classical数据读法,数据修剪,Many2One显示Names表等
		//OnConvertToRead(ctx *TFieldEventContext) interface{}  // TODO compute
		OnConvertToWrite(ctx *TFieldEventContext) (interface{}, error) // TODO 不返回或者返回错误
	}

	TField struct {
		// 共同属性
		isPrimaryKey    bool //
		isAutoIncrement bool
		isColumn        bool
		isCreated       bool //# 时间字段自动更新日期
		isUpdated       bool //
		isDeleted       bool
		isCascade       bool
		isVersion       bool

		defaultIsEmpty bool
		Comment        string
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

		_symbol_c         string              // Format 符号 "%s,%d..."
		_symbol_f         func(string) string // Format 自定义函数
		_auto_join        bool                //
		_inherit          bool                // 是否继承该字段指向的Model的多有字段
		_args             map[string]string   // [Tag]val 里的参数
		_setup_done       string              // 字段安装完成步骤 Base,full
		isInheritedField  bool                // 该字段是否关联表的字段 relate
		isRelatedField    bool                // 该字段是否关联表的外键字段
		automatic         bool                // 是否是自动创建的字段 ("magic" field)
		model_name        string              // 字段所在的模型名称
		comodel_name      string              // 连接的数据模型 关联字段的模型名称 字段关联的Model # name of the model of values (if relational)
		relmodel_name     string              // 关系表数据模型 字段关联的Model和字段的many2many关系表Model
		cokey_field_name  string              // 关联字段所在的表的主键
		relkey_field_name string              // M2M 表示被关联表的主键字段
		index             bool                // whether the field is indexed in database
		search            bool                // allow searching on self only if the related field is searchable
		translate         bool                //???

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
		_attr_size              int                    // 长度大小
		_attr_sortable          bool                   // 可排序
		_attr_searchable        bool                   //
		_attr_type              string                 // view 字段类型
		_attr_default           interface{}            // default(recs) returns the default value
		_attr_related           string                 // ???
		_attr_relation          string                 // 关系表
		_attr_states            map[string]interface{} // 传递 UI 属性
		_attr_selection         [][]string             //
		_attr_company_dependent bool                   // ???
		_attr_change_default    bool                   // ???
		_attr_domain            string
		// private membership
		_attr_groups string //???
		deprecated   string //???
		ondelete     string // 当这个字段指向的资源删除时将发生。预定义值：cascade，set null，restrict，no action，set default。默认值：set null

		//# Tag标记变量
		//_column_type string // #存储 column 类型 当该字段值非空时数据将直接存入数据库,而非计算值
		//_func          string      //是一个计算字段值的方法或函数。必须在声明函数字段前声明它。
		_func_inv     interface{} // ??? 函数,handler #是一个允许设置这个字段值的函数或方法。
		_func_multi   string      //默认为空 参见Model:calendar_attendee - for function field 一个组名。所有的有相同multi参数的字段将在一个单一函数调用中计算
		_func_search  string      //允许你在这个字段上定义搜索功能
		_compute      string      //# 字段值的计算函数函数必须是Model的 document = fields.Char(compute='_get_document', inverse='_set_document')
		_compute_sudo bool        //# whether field should be recomputed as admin		_related       string      //nickname = fields.Char(related='user_id.partner_id.name', store=True)
		_oldname      string      //# the previous name of this field, so that ORM can rename it automatically at migration

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
)

func (index *TIndex) XName(tableName string) string {
	if !strings.HasPrefix(index.Name, "UQE_") &&
		!strings.HasPrefix(index.Name, "IDX_") {
		tableName = strings.Replace(tableName, `"`, "", -1)
		tableName = strings.Replace(tableName, `.`, "_", -1)
		if index.Type == UniqueType {
			return fmt.Sprintf("UQE_%v_%v", tableName, index.Name)
		}
		return fmt.Sprintf("IDX_%v_%v", tableName, index.Name)
	}
	return index.Name
}

// add columns which will be composite index
func (index *TIndex) AddColumn(cols ...string) {
	for _, col := range cols {
		index.Cols = append(index.Cols, col)
	}
}

func (index *TIndex) Equal(dst *TIndex) bool {
	if index.Type != dst.Type {
		return false
	}
	if len(index.Cols) != len(dst.Cols) {
		return false
	}

	for i := 0; i < len(index.Cols); i++ {
		var found bool
		for j := 0; j < len(dst.Cols); j++ {
			if index.Cols[i] == dst.Cols[j] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// new an index
func NewIndex(name string, indexType int) *TIndex {
	return &TIndex{true, name, indexType, make([]string, 0)}
}

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

func newBaseField(name, field_type string) *TField {
	return &TField{
		//		Indexes:   make(map[string]int),
		_symbol_c: "%s",
		_symbol_f: _FieldFormat,
		// _deprecated: false,
		//_classic_read:  true,
		//_classic_write: true,
		// model_name:     model_name,
		//		_column_type: field_type,
		_attr_name:  name,
		_attr_type:  field_type,
		_attr_store: true,
	}
}

func IsFieldType(type_name string) (res bool) {
	_, res = field_creators[type_name]
	return
}

// sqlType:接受数据类型SQLType/string
func NewField(name string, sqlType interface{}) IField {
	var type_name string
	var new_field *TField

	if typ, ok := sqlType.(string); ok {
		type_name = typ
		new_field = newBaseField(name, typ)
	}

	if typ, ok := sqlType.(SQLType); ok {
		switch typ.Name {
		case Bit, TinyInt, SmallInt, MediumInt, Int, Integer, Serial:
			type_name = "int"
		case BigInt, BigSerial:
			type_name = "bigint"
		case Float, Real:
			type_name = "float"
		case Double:
			type_name = "double"
		case Char, Varchar, NVarchar, TinyText, Enum, Set, Uuid, Clob:
			type_name = "char"
		case Text, MediumText, LongText:
			type_name = "text"
		case Decimal, Numeric:
			type_name = "char"
		case Bool:
			type_name = "bool"
		case DateTime, Date, Time, TimeStamp, TimeStampz:
			type_name = "datetime"
		case TinyBlob, Blob, LongBlob, Bytea, Binary, MediumBlob, VarBinary:
			type_name = "binary"
		}

		new_field = newBaseField(name, type_name)
		new_field.SqlType = sqlType.(SQLType)

	}

	if type_name == "" {
		logger.Errf("the sqltype %v is not supported!", sqlType)
		return nil
	}

	creator, ok := field_creators[type_name]
	if !ok {
		//fmt.Errorf("cache: unknown adapter name %q (forgot to import?)", name)
		return nil
	}

	fld := creator()
	fld.SetBase(new_field)
	return fld
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

// String generate column description string according dialect
func (self *TField) String(d IDialect) string {
	sql := d.QuoteStr() + self.Name() + d.QuoteStr() + " "

	sql += d.GenSqlType(self) + " "

	if self.isPrimaryKey {
		sql += "PRIMARY KEY "
		if self.isAutoIncrement {
			sql += d.AutoIncrStr() + " "
		}
	}

	if self._attr_size != 0 {
		sql += "DEFAULT " + utils.IntToStr(self._attr_size) + " "
	}

	if d.ShowCreateNull() {
		if self._attr_required {
			sql += "NOT NULL "
		} else {
			sql += "NULL "
		}
	}

	return sql
}

// StringNoPk generate column description string according dialect without primary keys
func (self *TField) StringNoPk(d IDialect) string {
	sql := d.QuoteStr() + self.Name() + d.QuoteStr() + " "

	sql += d.GenSqlType(self) + " "

	if self._attr_size != 0 {
		sql += "DEFAULT " + utils.IntToStr(self._attr_size) + " "
	}

	if d.ShowCreateNull() {
		if self._attr_required {
			sql += "NOT NULL "
		} else {
			sql += "NULL "
		}
	}

	return sql
}

func (self *TField) Compute() string {
	return self._compute
}

func (self *TField) ModelName() string {
	return self.model_name
}

//TODO 优化函数名称
func (self *TField) RelateFieldName() string {
	return self.cokey_field_name
}

func (self *TField) MiddleFieldName() string {
	return self.relkey_field_name
}

// 字段关联的表
func (self *TField) RelateModelName() string {
	return self.comodel_name
}

// 多对多关系中 记录2表记录关联关系的表
func (self *TField) MiddleModelName() string {
	return self.relmodel_name
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
func (self *TField) Type() string {
	return self._attr_type
}

// database sql field type
func (self *TField) SQLType() *SQLType {
	return &self.SqlType
}

func (self *TField) FieldsId() string {
	return self._fields_id
}

func (self *TField) SymbolChar() string {
	return self._symbol_c
}

func (self *TField) SymbolFunc() func(string) string {
	return self._symbol_f
}

func (self *TField) Title() string {
	return self._attr_title
}

func (self *TField) Translate() bool {
	return self.translate
}

func (self *TField) Store(val ...bool) bool {
	if len(val) > 0 {
		self._attr_store = val[0]
	}

	return self._attr_store
}

func (self *TField) Default(val ...interface{}) interface{} {
	if len(val) > 0 {
		self._attr_default = val[0]
	}

	return self._attr_default
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

func (self *TField) Translatable() bool {
	return self.translate
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

//TODO 改名称
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

func (self *TField) IsAutoIncrement() bool {
	return self.isAutoIncrement
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

//
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
	*res = *self
	return
}

func (self *TField) Init(ctx *TFieldContext) {

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

func (self *TField) SetModel(name string) {
	self.model_name = name
}

// 跟新字段到数据库 索引 唯一等
func (self *TField) UpdateDb(ctx *TFieldContext) {

}

//""" Return a dictionary that describes the field ``self``. """
// 返回字段自己 补充部分属性值
//func (self *TField) GetDescription() (res *TField) {
func (self *TField) GetAttributes(ctx *TFieldContext) map[string]interface{} {
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
	}
}

func (self *TField) SetAttributes(name string) {

}

// 设置字段值
func (self *TField) __OnWrite(ctx *TFieldEventContext) interface{} {
	//ctx.Session.orm.Exec(fmt.Sprintf("UPDATE "+ctx.Session.model.TableName()+" SET "+ctx.Field.Name()+"="+ctx.Field.SymbolChar()+" WHERE id=%v", ctx.Field.SymbolFunc()(utils.Itf2Str(ctx.Value)), ctx.Id))
	return nil
}

// 设置字段获得的值
// TODO:session *TSession, 某些地方无法提供session或者没有必要用到
// """ Read the value of ``self`` on ``records``, and store it in cache. """
func (self *TField) __OnRead(ctx *TFieldEventContext) interface{} {
	//logger.Warn("undefined filed Read method !")
	//ctx.Dataset.FieldByName(ctx.Field.Name()).AsInterface()
	return nil
}

/*
   """ Convert ``value`` from the record format to the format returned by
   method :meth:`BaseModel.read`.

   :param bool use_name_get: when True, the value's display name will be
       computed using :meth:`BaseModel.name_get`, if relevant for the field
   """
*/
func (self *TField) OnRead(ctx *TFieldEventContext) error {
	return nil
}

/*
   """ Convert ``value`` from the record format to the format of method
   :meth:`BaseModel.write`.
   """
*/
func (self *TField) OnWrite(ctx *TFieldEventContext) error {
	return nil
}

func (self *TField) OnConvertToWrite(ctx *TFieldEventContext) (interface{}, error) {
	return ctx.Value, nil
}
