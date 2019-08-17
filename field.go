package orm

import (
	"reflect"
	"strings"
	"sync"

	//	"time"

	"volts-dev/dataset"

	"github.com/go-xorm/core"
)

const (
	FIELD_TYPE_BOOL = "bool"

	// Types for model fields
	NoType    Type = ""
	Binary    Type = "binary"
	Boolean   Type = "boolean"
	Char      Type = "char"
	Date      Type = "date"
	DateTime  Type = "datetime"
	Float     Type = "float"
	HTML      Type = "html"
	Integer   Type = "integer"
	Many2Many Type = "many2many"
	Many2One  Type = "many2one"
	One2Many  Type = "one2many"
	One2One   Type = "one2one"
	Rev2One   Type = "rev2one"
	Reference Type = "reference"
	Selection Type = "selection"
	Text      Type = "text"
)

var (
	// 注册的Writer类型函数接口
	field_creators = make(map[string]func() IField)

	FieldTypes = map[string]string{
		// 布尔
		"BOOL": "boolean",
		// 整数
		"INT":     "integer",
		"INTEGER": "integer",
		"BIGINT":  "integer",

		"CHAR":     "char",
		"VARCHAR":  "char",
		"NVARCHAR": "char",
		"TEXT":     "text",

		"MEDIUMTEXT": "text",
		"LONGTEXT":   "text",

		"DATE":       "date",
		"DATETIME":   "datetime",
		"TIME":       "datetime",
		"TIMESTAMP":  "datetime",
		"TIMESTAMPZ": "datetime",

		//Decimal = "DECIMAL"
		//Numeric = "NUMERIC"
		"REAL":   "float",
		"FLOAT":  "float",
		"DOUBLE": "float",

		"VARBINARY":  "binary",
		"TINYBLOB":   "binary",
		"BLOB":       "binary",
		"MEDIUMBLOB": "binary",
		"LONGBLOB":   "binary",
		"JSON":       "json",
		"reference":  "reference",
	}
)

type (
	// A Type defines a type of a model's field
	Type string

	/* The field descriptor contains the field definition, and manages accesses
	   and assignments of the corresponding field on records. The following
	   attributes may be provided when instanciating a field:

	   :param string: the label of the field seen by users (string); if not
	       set, the ORM takes the field name in the class (capitalized).

	   :param help: the tooltip of the field seen by users (string)

	   :param readonly: whether the field is readonly (boolean, by default ``False``)

	   :param required: whether the value of the field is required (boolean, by
	       default ``False``)

	   :param index: whether the field is indexed in database (boolean, by
	       default ``False``)

	   :param default: the default value for the field; this is either a static
	       value, or a function taking a recordset and returning a value

	   :param states: a dictionary mapping state values to lists of UI attribute-value
	       pairs; possible attributes are: 'readonly', 'required', 'invisible'.
	       Note: Any state-based condition requires the ``state`` field value to be
	       available on the client-side UI. This is typically done by including it in
	       the relevant views, possibly made invisible if not relevant for the
	       end-user.

	   :param groups: comma-separated list of group xml ids (string); this
	       restricts the field access to the users of the given groups only

	   :param bool copy: whether the field value should be copied when the record
	       is duplicated (default: ``True`` for normal fields, ``False`` for
	       ``one2many`` and computed fields, including property fields and
	       related fields)

	   :param string oldname: the previous name of this field, so that ORM can rename
	       it automatically at migration

	   .. _field-computed:

	   .. rubric:: Computed fields

	   One can define a field whose value is computed instead of simply being
	   read from the database. The attributes that are specific to computed
	   fields are given below. To define such a field, simply provide a value
	   for the attribute ``compute``.

	   :param compute: name of a method that computes the field

	   :param inverse: name of a method that inverses the field (optional)

	   :param search: name of a method that implement search on the field (optional)

	   :param store: whether the field is stored in database (boolean, by
	       default ``False`` on computed fields)

	   :param compute_sudo: whether the field should be recomputed as superuser
	       to bypass access rights (boolean, by default ``False``)

	   The methods given for ``compute``, ``inverse`` and ``search`` are model
	   methods. Their signature is shown in the following example::

	       upper = fields.Char(compute='_compute_upper',
	                           inverse='_inverse_upper',
	                           search='_search_upper')

	       @api.depends('name')
	       def _compute_upper(self):
	           for rec in self:
	               rec.upper = rec.name.upper() if rec.name else False

	       def _inverse_upper(self):
	           for rec in self:
	               rec.name = rec.upper.lower() if rec.upper else False

	       def _search_upper(self, operator, value):
	           if operator == 'like':
	               operator = 'ilike'
	           return [('name', operator, value)]

	   The compute method has to assign the field on all records of the invoked
	   recordset. The decorator :meth:`openerp.api.depends` must be applied on
	   the compute method to specify the field dependencies; those dependencies
	   are used to determine when to recompute the field; recomputation is
	   automatic and guarantees cache/database consistency. Note that the same
	   method can be used for several fields, you simply have to assign all the
	   given fields in the method; the method will be invoked once for all
	   those fields.

	   By default, a computed field is not stored to the database, and is
	   computed on-the-fly. Adding the attribute ``store=True`` will store the
	   field's values in the database. The advantage of a stored field is that
	   searching on that field is done by the database itself. The disadvantage
	   is that it requires database updates when the field must be recomputed.

	   The inverse method, as its name says, does the inverse of the compute
	   method: the invoked records have a value for the field, and you must
	   apply the necessary changes on the field dependencies such that the
	   computation gives the expected value. Note that a computed field without
	   an inverse method is readonly by default.

	   The search method is invoked when processing domains before doing an
	   actual search on the model. It must return a domain equivalent to the
	   condition: ``field operator value``.

	   .. _field-related:

	   .. rubric:: Related fields

	   The value of a related field is given by following a sequence of
	   relational fields and reading a field on the reached model. The complete
	   sequence of fields to traverse is specified by the attribute

	   :param related: sequence of field names

	   Some field attributes are automatically copied from the source field if
	   they are not redefined: ``string``, ``help``, ``readonly``, ``required`` (only
	   if all fields in the sequence are required), ``groups``, ``digits``, ``size``,
	   ``translate``, ``sanitize``, ``selection``, ``comodel_name``, ``domain``,
	   ``context``. All semantic-free attributes are copied from the source
	   field.

	   By default, the values of related fields are not stored to the database.
	   Add the attribute ``store=True`` to make it stored, just like computed
	   fields. Related fields are automatically recomputed when their
	   dependencies are modified.

	   .. _field-company-dependent:

	   .. rubric:: Company-dependent fields

	   Formerly known as 'property' fields, the value of those fields depends
	   on the company. In other words, users that belong to different companies
	   may see different values for the field on a given record.

	   :param company_dependent: whether the field is company-dependent (boolean)

	   .. _field-incremental-definition:

	   .. rubric:: Incremental definition

	   A field is defined as class attribute on a model class. If the model
	   is extended (see :class:`~openerp.models.Model`), one can also extend
	   the field definition by redefining a field with the same name and same
	   type on the subclass. In that case, the attributes of the field are
	   taken from the parent class and overridden by the ones given in
	   subclasses.

	   For instance, the second class below only adds a tooltip on the field
	   ``state``::

	       class First(models.Model):
	           _name = 'foo'
	           state = fields.Selection([...], required=True)

	       class Second(models.Model):
	           _inherit = 'foo'
	           state = fields.Selection(help="Blah blah blah")

	*/
	/*
		// 废弃
		IFieldCtrl interface {
			Write(session *TSession, id string, fields *TField, value string, rel_context map[string]interface{}) interface{} //(res map[string]map[string]interface{}) // 字段数据保存
			Read(session *TSession, field *TField, dataset *dataset.TDataSet, rel_context map[string]interface{}) interface{}         // (res map[string]map[string]interface{})         // 字段数据获取
		}
	*/

	// The context for Tag
	TFieldContext struct {
		Orm            *TOrm
		Model          IModel // required
		Field          IField // required
		FieldTypeValue reflect.Value
		Column         *core.Column
		Params         []string // 属性参数 int(<params>)
	}

	TFieldEventContext struct {
		Session *TSession
		Model   IModel
		//FieldTypeValue reflect.Value
		//Column         *core.Column
		Field IField
		// the current id of current record
		Id interface{}
		// the current value of the field
		Value   interface{}
		Dataset *dataset.TDataSet // 数据集将被修改
		Context map[string]interface{}
	}

	IField interface {
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
		Column() *core.Column
		ColumnType() string // the sql type
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
		_symbol_c         string              // Format 符号 "%s,%d..."
		_symbol_f         func(string) string // Format 自定义函数
		_auto_join        bool                //
		_inherit          bool                // 是否继承该字段指向的Model的多有字段
		_args             map[string]string   // [Tag]val 里的参数
		_setup_done       string              // 字段安装完成步骤 Base,full
		isCreated         bool                //# 时间字段自动更新日期
		isUpdated         bool                //
		isInheritedField  bool                // 该字段是否关联表的字段 relate
		isRelatedField    bool                // 该字段是否关联表的外键字段
		___common_field   bool                // 废弃 所有表共有的字段
		___related        bool                //
		automatic         bool                // 是否是自动创建的字段 ("magic" field)
		model_name        string              // 字段所在的模型名称
		comodel_name      string              // 连接的数据模型 关联字段的模型名称 字段关联的Model # name of the model of values (if relational)
		relmodel_name     string              // 关系表数据模型 字段关联的Model和字段的many2many关系表Model
		cokey_field_name  string              // 关联字段所在的表的主键
		relkey_field_name string              // M2M 表示被关联表的主键字段
		primary_key       bool                //
		auto_increment    bool                //
		index             bool                // whether the field is indexed in database
		search            bool                // allow searching on self only if the related field is searchable
		translate         bool                //???

		// published exportable
		_attr_name              string                 // name of the field
		_attr_store             bool                   // 字段值是否保存到数据库
		_attr_manual            bool                   //
		_attr_depends           []string               //
		_attr_readonly          bool                   // 只读
		_attr_required          bool                   // 字段不为空
		_attr_help              string                 //
		_attr_title             string                 // 字段的Title
		_attr_size              int64                  // 长度大小
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
		column       *core.Column
		_column_type string // #存储 column 类型 当该字段值非空时数据将直接存入数据库,而非计算值
		//_classic_read  bool   //废弃 # 默认true 是否常规直接从数据库读取 相反使用et()处理
		//_classic_write bool   //废弃 # 默认true 是否常规直接写入数据库 相反使用Set()处理
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

	// FieldsCollection is a collection of Field instances in a model.
	// 字段集合
	TFieldsSet struct {
		sync.RWMutex
		model                *TModel
		registryByName       map[string]*IField
		registryByJSON       map[string]*IField
		computedFields       []*IField
		computedStoredFields []*IField
		relatedFields        []*IField
		bootstrapped         bool
	}
)

// IsRelationType returns true if this type is a relation.
func (t Type) IsRelationType() bool {
	return t == Many2Many || t == Many2One || t == One2Many || t == One2One || t == Rev2One
}

// IsFKRelationType returns true for relation types
// that are stored in the model's table (i.e. M2O and O2O)
func (t Type) IsFKRelationType() bool {
	return t == Many2One || t == One2One
}

// IsNonStoredRelationType returns true for relation types
// that are not stored in the model's table (i.e. M2M, O2M and R2O)
func (t Type) IsNonStoredRelationType() bool {
	return t == Many2Many || t == One2Many || t == Rev2One
}

// IsReverseRelationType returns true for relation types
// that are stored in the comodel's table (i.e. O2M and R2O)
func (t Type) IsReverseRelationType() bool {
	return t == One2Many || t == Rev2One
}

// Is2OneRelationType returns true for relation types
// that point to a single comodel record (i.e. M2O, O2O and R2O)
func (t Type) Is2OneRelationType() bool {
	return t == Many2One || t == One2One || t == Rev2One
}

// Is2ManyRelationType returns true for relation types
// that point to multiple comodel records (i.e. M2M and O2M)
func (t Type) Is2ManyRelationType() bool {
	return t == Many2Many || t == One2Many
}

// DefaultGoType returns this Type's default Go type
func (t Type) DefaultGoType() reflect.Type {
	switch t {
	case NoType:
		return reflect.TypeOf(nil)
	case Binary, Char, Text, HTML, Selection:
		return reflect.TypeOf(*new(string))
	case Boolean:
		return reflect.TypeOf(true)
	case Date:
	//	return reflect.TypeOf(*new(time.Date))
	case DateTime:
	//	return reflect.TypeOf(*new(time.DateTime))
	case Float:
		return reflect.TypeOf(*new(float64))
	case Integer, Many2One, One2One, Rev2One:
		return reflect.TypeOf(*new(int64))
	case One2Many, Many2Many:
		return reflect.TypeOf(*new([]int64))
	}
	return reflect.TypeOf(nil)
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
		_symbol_c: "%s",
		_symbol_f: _FieldFormat,
		// _deprecated: false,
		//_classic_read:  true,
		//_classic_write: true,
		// model_name:     model_name,
		_column_type: field_type,
		_attr_name:   name,
		_attr_type:   field_type,
		_attr_store:  true,
	}
}

func IsFieldType(type_name string) (res bool) {
	_, res = field_creators[type_name]
	return
}

func NewField(name, type_name string) IField {
	creator, ok := field_creators[type_name]
	if !ok {
		//fmt.Errorf("cache: unknown adapter name %q (forgot to import?)", name)
		return nil
	}
	fld := creator()
	fld.SetBase(newBaseField(name, type_name))
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

func (self *TField) Type() string {
	return self._attr_type
}

func (self *TField) ColumnType() string {
	return self._column_type
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

func (self *TField) Column() *core.Column {
	return self.column
}

// 该字段是不是指向其他model的id
func (self *TField) IsRelatedField(arg ...bool) bool {
	if len(arg) > 0 {
		self.isRelatedField = arg[0]
	}
	return self.isRelatedField
}

//
func (self *TField) IsInheritedField(arg ...bool) bool {
	if len(arg) > 0 {
		self.isInheritedField = arg[0]
	}
	return self.isInheritedField
}

func (self *TField) ___IsCommonField(arg ...bool) bool {
	if len(arg) > 0 {
		self.___common_field = arg[0]
	}
	return self.___common_field
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
	if ctx.Dataset != nil {
		return nil
	}

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
