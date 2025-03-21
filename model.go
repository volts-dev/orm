package orm

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

const (
	// maximum number of prefetched records
	PREFETCH_MAX = 200

	// ERROR
	MODEL_NOT_FOUND = ""
)

var (
	// special columns automatically created by the ORM
	LOG_ACCESS_COLUMNS = []string{"create_id", "create_date", "write_id", "write_date"}
	MAGIC_COLUMNS      = append(LOG_ACCESS_COLUMNS, "id")
)

type (
	ModelTemplate struct {
		options []ModelOption
	}

	// 基础Model接口
	IModel interface {
		// Pravite Interface:
		// retrieve the lines in the comodel
		// --------------------- private ---------------------
		_setOrm(o *TOrm)
		_setBaseModel(model *TModel) //赋值初始化BaseModel
		_relations_reload()
		_addField(field IField)
		_onBuildFields() error

		// --------------------- public ---------------------

		String() string // model name in orm like "base.user"
		Table() string  // table name in database like "base_user"
		// 获取继承的模型
		// 用处:super 用于方便调用不同层级模型的方法/查询等
		Super() IModel
		Prototype() IModel
		Clone(options ...ModelOption) (IModel, error)

		// 包含指定表模型的事务 如若开启了事务这里非nil 反之亦然
		Records() *TSession                // new a orm records session for query
		Tx(session ...*TSession) *TSession // 返回/配置当前会话事物
		Transaction() *TSession            //
		Osv() *TOsv
		Obj() *TModelObject
		Orm() *TOrm
		Options(Option ...ModelOption) *ModelOptions
		//fields_get(allfields map[string]*TField, attributes []string, context map[string]string) (fields map[string]interface{})
		//check_access_rights(operation string) bool
		Ctx(...context.Context) context.Context //Context
		GetIndexes() map[string]*TIndex
		GetBase() *TModel // get the base model object
		GetColumnsSeq() []string
		GetPrimaryKeys() []string

		// 对象被创建时
		GetDefault() *sync.Map // map[string]interface{}
		GetDefaultByName(fieldName string) (value interface{})
		SetDefaultByName(fieldName string, value interface{}) // 默认值修改获取

		// return the model name
		//GetModelName() string
		//GetTableName() string
		GetTableDescription() string
		SetRecordName(fieldName string)
		GetRecordName() string
		SetName(n string)
		//		SetRegistry(reg *TRegistry)
		//		Session() *TSession
		//Field(field string, val ...*TField) (result *TField) // 作废
		MethodByName(method string) *TMethod
		GetFieldByName(name string) IField
		GetFields() []IField
		//RelateFieldByName(field string, val ...*TRelatedField) (res *TRelatedField)
		//RelateFields() map[string]*TRelatedField
		//Relations() map[string]string
		NameField(field ...string) string
		IdField(field ...string) string

		// CRUD 不带事务的
		Create(req *CreateRequest) ([]any, error)
		Read(req *ReadRequest) (*dataset.TDataSet, error)
		Update(req *UpdateRequest) (int64, error)
		Delete(req *DeleteRequest) (int64, error)
		Upload(req *UploadRequest) (int64, error)

		// 关联查询函数
		// 主表[字段所在的表]字段值是关联表其中之一条记录,关联表字段相当于主表或其他表的补充扩展或共同字段
		// 特性：存储,外键在主表,主表类似于继承了关联表的多有字段
		// 例子：合作伙伴里有个人和公司,他们都有名称,联系方式,地址等共同信息 这些信息可以又关联表存储
		OneToOne(*TFieldContext) (*dataset.TDataSet, error)
		/*	Object A can have one or many of objects B (E.g A person can have many cars).
			Relationship: A -> B = One -> Many = One2Many
			(You can select many cars while creating a person).	*/
		OneToMany(*TFieldContext) (*dataset.TDataSet, error)
		/*	Object B can only have one object of A.
			(E.g A car is owned by one person also many cars can be owned by the same person).
			Relationship: B -> A = Many -> One = Many2One	*/
		ManyToOne(*TFieldContext) (*dataset.TDataSet, error)
		// 字段值是中间表中绑定的多条关联表记录集(多条记录)
		ManyToMany(*TFieldContext) (*dataset.TDataSet, error)

		// UTF file only
		Load(field []string, records ...any) (ids []any, err error)
		NameCreate(name string) (*dataset.TDataSet, error)
		NameGet(ids []interface{}) (*dataset.TDataSet, error)
		DefaultGet(...string) (map[string]any, error)
		//Search(domain string, offset int64, limit int64, order string, count bool, context map[string]interface{}) []string
		//SearchRead(domain string, fields []string, offset int64, limit int64, order string, context map[string]interface{}) *dataset.TDataSet
		NameSearch(name string, domain *domain.TDomainNode, operator string, limit int64, name_get_uid string, context map[string]interface{}) (*dataset.TDataSet, error)
		//SearchCount(domain string, context map[string]interface{}) int

		/* Lifecycle

			SyncModel()
				|
			BeforeSetup()
				|
		create table to database
				|
			AfterSetup()
				｜
			OnBuildFields
				｜
			GetModel()
				｜
			OnBuildModel()
				|
			  model

		*/
		// 初始化模型
		// mapping -> init -> object
		OnBuildModel() error
		OnBuildFields() error

		// TODO 未完成
		BeforeSession(*TSession) (*TSession, error)
		AfterSession(*TSession) (*TSession, error)
		BeforeSetup() error
		AfterSetup() error
	}

	ModelOption  func(*ModelOptions)
	ModelOptions struct {
		Model   IModel // 模型自己
		Module  string //TODO 更名称 属于哪个模块所有
		Order   []string
		Context context.Context
	}

	// 所有成员都是Unexportable 小写,避免映射JSON,XML,ORM等时发生错误
	/*
	* 	字段命名规格 ："_xxx" "小写" 避免和子继承类覆盖
	* 	方法命名规格 ："GetXXX","SetXXX","XXByXX"
	 */
	TModel struct {
		prototype      IModel
		super          IModel        // 继承的Model
		modelType      reflect.Type  // Model 反射类
		modelValue     reflect.Value // Model 反射值 供某些程序调用方法
		orm            *TOrm         //
		osv            *TOsv         // 对象服务
		obj            *TModelObject //
		options        *ModelOptions
		transaction    *TSession //
		name           string    // the model name (in dot-notation, module namespace "xx.xx") 映射在OSV的名称
		table          string    // mapping table name
		description    string    // 描述
		idField        string    // the field name which is the UID represent a record
		recName        string    // the field name which is the name represent a record @examples: Name,Title,PartNo
		recNamesSearch string    // names_search会搜索的字段
		isCustomModel  bool      // 该Model是否是基Model,并非扩展Model
		beforeSession  func(*TSession) error
		afterSession   func(*TSession) error

		// below vars must name as "_xxx" to avoid mixed inherited-object's vars
		_parent_name  string // #! 父表中的字段名称
		_parent_store bool   // #! 是否有父系关联 比如类目，菜单
		_sequence     string //
		_auto         bool   // # True # create database backend
		_transient    bool   // # 暂时的
		//_relate       bool
	}
)

func WithFunc(fn func(opts *ModelOptions)) ModelOption {
	return func(opts *ModelOptions) {
		fn(opts)
	}
}

func WithContext(ctx context.Context) ModelOption {
	return func(opts *ModelOptions) {
		opts.Context = ctx
	}
}

func WithModuleName(name string) ModelOption {
	return func(opts *ModelOptions) {
		opts.Module = name
	}
}

func newModelOptions(model IModel) (res *ModelOptions) {
	return &ModelOptions{
		Model:   model,
		Context: context.Background(),
	}

}

func (tpl *ModelTemplate) AddOption(opts ...ModelOption) {
	tpl.options = append(tpl.options, opts...)
}

// 新建模型 不带其他信息
// @ Session
// @ Registry
func newModel(name, tableName string, modelValue reflect.Value, modelType reflect.Type, options *ModelOptions) (model *TModel) {
	if len(name) > 0 && len(tableName) == 0 {
		tableName = fmtTableName(name)
	}

	if len(name) == 0 && len(tableName) > 0 {
		name = fmtModelName(tableName)
	}

	if options == nil {
		options = newModelOptions(nil)
	}

	model = &TModel{
		name:           name,
		table:          tableName,
		modelType:      modelType,
		modelValue:     modelValue,
		options:        options,
		recNamesSearch: DefaultNameField,
		_auto:          true,
	}

	model.options.Model = model
	//mdl._sequence = mdl._table + "_id_seq"

	return model
}

func (self *TModel) OnBuildModel() error {

	return nil
}

func (self *TModel) OnBuildFields() error {

	return nil
}

/*
// TODO 包含同步上个下文Session
super() 函数是用于调用父类(超类)的一个方法。
super 是用来解决多重继承问题的，直接用类名调用父类方法在使用单继承的时候没问题，但是如果使用多继承，会涉及到查找顺序（MRO）、重复调用（钻石继承）等种种问题。
MRO 就是类的方法解析顺序表, 其实也就是继承父类方法时的顺序表。
*/
func (self *TModel) Super() IModel {
	return self.super
}

func (self *TModel) Prototype() IModel {
	return self.prototype
}

// 克隆一个新的Model包含现有事物Tx和Context
func (self *TModel) Clone(options ...ModelOption) (IModel, error) {
	model, err := self.osv.GetModel(self.String())
	if err != nil {
		return nil, err
	}
	model.Ctx(self.options.Context)
	//model.Tx(self.transaction.Clone())
	model.Options(options...)
	return model, nil
}

// TODO 废除因为继承的一致性冲突
func (self *TModel) Osv() *TOsv {
	return self.osv
}

func (self *TModel) Obj() *TModelObject {
	return self.obj
}

// TODO 废除因为继承的一致性冲突
func (self *TModel) Orm() *TOrm {
	return self.orm
}

// Provide api to query records from cache or database
func (self *TModel) Db() *TSession {
	session := NewSession(self.orm)
	/* 提供参考Model*/
	session.Statement.Model = self.prototype
	/* 从Model获取必要信息 */
	return session.Model(self.String())
}

func (self *TModel) Tx(session ...*TSession) *TSession {
	if len(session) > 0 {
		if s := session[0]; s != nil {
			/* 提供参考Model*/
			s.Statement.Model = self.prototype
			/* 从Model获取必要信息 */
			self.transaction = s.Model(self.String())
			return self.transaction
		}
	}

	if self.transaction == nil {
		self.transaction = self.Records()
	}

	return self.transaction
}

// 上下文
func (self *TModel) Ctx(context ...context.Context) context.Context {
	if len(context) > 0 {
		if ctx := context[0]; ctx != nil {
			self.options.Context = ctx
			return self.options.Context
		}
	}

	return self.options.Context
}

func (self *TModel) Transaction() *TSession {
	return self.transaction
}

func (self *TModel) Builder() *ModelBuilder {
	return newModelBuilder(self.orm, self)
}

// Provide api to query records from cache or database
func (self *TModel) Records() *TSession {
	session := NewSession(self.orm)
	/* 提供参考Model*/
	session.Statement.Model = self.prototype
	/* 从Model获取必要信息 */
	return session.Model(self.String())
}

func (self *TModel) SetRecordName(fieldName string) {
	self.recName = fieldName
}

// return the field name of record's name field
func (self *TModel) GetRecordName() string {
	// # if self.recName is set, it belongs to self._fields
	if fld := self.GetFieldByName(self.recName); fld == nil {
		if self.recName == "" {
			if fld := self.GetFieldByName(DefaultNameField); fld != nil {
				self.recName = DefaultNameField
			} else if self.idField != "" {
				self.recName = self.idField
			} else {
				self.recName = DefaultIdField
			}
		}
	}

	return self.recName
}

// TODO 优化
func (self *TModel) GetColumnsSeq() []string {
	return self.obj.columnsSeq
}

// TODO 优化
func (self *TModel) GetPrimaryKeys() []string {
	return self.obj.PrimaryKeys
}

// pravite
func (self *TModel) SetName(name string) {
	self.name = name
}

// 返回Model的描述字符串
func (self *TModel) GetTableDescription() string {
	return self.description
}

func (self *TModel) GetIndexes() map[string]*TIndex {
	return self.obj.indexes
}

// 实际注册的model原型
func (self *TModel) Interface() interface{} {
	return self.modelValue.Interface()
}

func (self *TModel) GetBase() *TModel {
	return self
}

func (self *TModel) Module() string {
	return self.options.Module
}

// return the method object of model by name
func (self *TModel) MethodByName(methodName string) *TMethod {
	return self.osv.GetMethod(self.String(), methodName)
}

//-------------- 路由方法 --------------------
/*
   Attempt to construct an appropriate ORDER BY clause based on order_spec, which must be
   a comma-separated list of valid field names, optionally followed by an ASC or DESC direction.

   :raise ValueError in case order_spec is malformed
*/
func _generate_order_by(order_spec, query *TQuery) {

}

/*
        order_by_clause := ""
		if order_spec==""{
			order_spec=self._order
			}

        if order_spec!=""{
            order_by_elements = self._generate_order_by_inner(self._table, order_spec, query)
            if order_by_elements:
                order_by_clause = ",".join(order_by_elements)
}
        return order_by_clause and (' ORDER BY %s ' % order_by_clause) or ''
*/

// 删除记录
/* unlink()

   Deletes the records of the current set

   :raise AccessError: * if user has no unlink rights on the requested object
                       * if user tries to bypass access rules for unlink on the requested object
   :raise UserError: if the record is default property for other records
*/ /*
func (self *TModel) Unlink(ids ...string) bool {
	return self._unlink(ids...)
}*/

func (self *TModel) GetName() string {
	return self.name
}

// mapping table name which in database
func (self *TModel) Table() string {
	return self.table
}

func (self *TModel) String() string {
	return self.name
}

func (self *TModel) Options(options ...ModelOption) *ModelOptions {
	if self.options == nil {
		self.options = newModelOptions(self)
	}

	for _, opt := range options {
		opt(self.options)
	}

	return self.options
}

func (self *TModel) GetDefault() *sync.Map {
	return self.obj.GetDefault()
}

func (self *TModel) GetDefaultByName(fieldName string) (value interface{}) {
	return self.obj.GetDefaultByName(fieldName)
}

func (self *TModel) SetDefaultByName(fieldName string, value interface{}) {
	self.obj.SetDefaultByName(fieldName, value)
}

func (self *TModel) GetFieldByName(name string) IField {
	return self.obj.GetFieldByName(name)
}

// Fields returns the fields collection of this model
func (self *TModel) GetFields() []IField {
	return self.obj.GetFields()
}

func (self *TModel) NameField(field ...string) string {
	if len(field) > 0 {
		self.recName = field[0]
	}
	return self.recName
}

func (self *TModel) IdField(field ...string) string {
	if len(field) > 0 {
		self.idField = field[0]
	}
	return self.idField
}

// Methods returns the methods collection of this model
func (self *TModel) GetMethods() *TMethodsSet {
	//TODO
	return nil // self.methods
}

func (self *TModel) BeforeSession(session *TSession) (*TSession, error) {
	return session, nil
}

func (self *TModel) AfterSession(session *TSession) (*TSession, error) {
	return session, nil
}

func (self *TModel) BeforeSetup() error { return nil }
func (self *TModel) AfterSetup() error  { return nil }

func (self *TModel) _onBuildFields() error {
	if self.super != nil {
		if err := self.super._onBuildFields(); err != nil {
			return err
		}
	}

	return self.prototype.OnBuildFields()
}

func (self *TModel) _addField(field IField) {
	field.Base().model_name = self.name
	self.obj.AddField(field)
}

func (self *TModel) _setOrm(o *TOrm) {
	self.orm = o
}

func (self *TModel) _setBaseModel(model *TModel) {
	*self = *model
	self._sequence = self.name + "_id_seq"
	//self.super = model
}

// """ Recompute the _inherit_fields mapping. """
// TODO 移动到ORM里实现
// 重载关联表字段到_relate_fields里 _relate_fields的赋值在此实现
// 条件必须所有Model都注册到OSV里
func (self *TModel) _relations_reload() {
	/*
	   cls._inherit_fields = struct = {}
	   for parent_model, parent_field in cls._inherits.iteritems():
	       parent = cls.pool[parent_model]
	       parent._inherits_reload()
	       for name, column in parent._columns.iteritems():
	           struct[name] = (parent_model, parent_field, column, parent_model)
	       for name, source in parent._inherit_fields.iteritems():
	           struct[name] = (parent_model, parent_field, source[2], source[3])
	*/
	//TODO  锁安全
	var (
		fielss        []IField
		relate_fields map[string]*TRelatedField
		tbl, fld      string
	)

	self.obj.GetRelations().Range(func(key, value any) bool {
		tbl = key.(string)
		fld = value.(string)

		rel_model, err := self.osv.GetModel(tbl) //
		if err != nil {
			log.Errf("Relation model %v can not find in osv or didn't register front of %v", tbl, self.String())
		}

		rel_model._relations_reload()
		fielss = rel_model.GetFields()
		relate_fields = rel_model.Obj().GetRelatedFields() //RelateFields()

		for _, field := range fielss {
			name := field.Name()
			self.obj.SetRelatedFieldByName(name, NewRelateField(name, tbl, fld, field, tbl))

		}

		for name, source := range relate_fields {
			self.obj.SetRelatedFieldByName(name, NewRelateField(name, tbl, fld, source.RelateField, source.RelateTopestTable))
		}

		/*
			self._relate_fields_lock.Lock()
			for name, field := range rel_model.Fields() {
				self._relate_fields[name] = NewRelateField(name, tbl, fld, field, tbl)
			}

			for name, source := range rel_model.RelateFields() {
				self._relate_fields[name] = NewRelateField(name, tbl, fld, source.RelateField, source.RelateTopestTable)
			}
			self._relate_fields_lock.Unlock()
		*/
		return true
	})
}

//	""" Determine inherited fields. """
//
// 添加关联字段到Model
func (self *TModel) _add_inherited_fields() {

	//# determine candidate inherited fields
	//	var fields = make([]*TField, 0)
	var lNew IField
	var parent_model string
	self.obj.GetRelations().Range(func(key, value any) bool {
		parent_model = key.(string)
		parent, err := self.osv.GetModel(parent_model) // #i
		if err != nil {
			log.Err(err, "@_add_inherited_fields")
		}

		for _, ref := range parent.GetFields() {
			refname := ref.Name()
			//# inherited fields are implemented as related fields, with the
			//# following specific properties:
			//#  - reading inherited fields should not bypass access rights
			//#  - copy inherited fields iff their original field is copied
			if has := self.obj.GetFieldByName(refname); has != nil {
				lNew = utils.Clone(ref).(IField)
				lNew.IsInheritedField(true)
				self.obj.SetFieldByName(refname, ref)
			}
		}

		return true
	})

	/*
	   for parent_model, parent_field in self._inherits.iteritems():
	       parent = self.env[parent_model]
	       for name, field in parent._fields.iteritems():

	           fields[name] = field.new(
	               inherited=True,
	               related=(parent_field, name),
	               related_sudo=False,
	               copy=field.copy,
	           )

	   # add inherited fields that are not redefined locally
	   for name, field in fields.iteritems():
	       if name not in self._fields:
	           self._add_field(name, field)
	*/
}

func (self *TModel) ___getRelate(ctx *TFieldContext) (*dataset.TDataSet, error) {
	field := ctx.Field
	if !field.IsRelatedField() {
		return nil, fmt.Errorf("the field %s must related field, but not %s!", ctx.Field.Name(), field.Type())
	}

	switch field.Type() {
	case TYPE_O2O:
		return self.OneToOne(ctx)
	case TYPE_O2M:
		return self.OneToMany(ctx)
	case TYPE_M2O:
		return self.ManyToOne(ctx)
	case TYPE_M2M:
		return self.ManyToMany(ctx)
	}

	return nil, fmt.Errorf("the type <%s> of relate field not implemented!", field.Type())
}

/*
Call _field_create and, unless _auto is False:

  - create the corresponding table in database for the model,
  - possibly add the parent columns in database,
  - possibly add the columns 'create_uid', 'create_date', 'write_uid',
    'write_date' in database if _log_access is True (the default),
  - report on database columns no more existing in _columns,
  - remove no more existing not null constraints,
  - alter existing database columns to match _columns,
  - create database tables to match _columns,
  - add database indices to match _columns,
  - save in self._foreign_keys a list a foreign keys to create (see
    _auto_end).
*/
func (self *TModel) __select_column_data() *dataset.TDataSet {
	//# attlen is the number of bytes necessary to represent the type when
	// # the type has a fixed size. If the type has a varying size attlen is
	//# -1 and atttypmod is the size limit + 4, or -1 if there is no limit.
	/* cr.execute("SELECT c.relname,a.attname,a.attlen,a.atttypmod,a.attnotnull,a.atthasdef,t.typname,CASE WHEN a.attlen=-1 THEN (CASE WHEN a.atttypmod=-1 THEN 0 ELSE a.atttypmod-4 END) ELSE a.attlen END as size " \
	           "FROM pg_class c,pg_attribute a,pg_type t " \
	           "WHERE c.relname=%s " \
	           "AND c.oid=a.attrelid " \
	           "AND a.atttypid=t.oid", (self._table,))
			  return dict(map(lambda x: (x['attname'], x),cr.dictfetchall()))
	*/
	lDs, err := self.orm.Query(`SELECT c.relname,a.attname,a.attlen,a.atttypmod,a.attnotnull,a.atthasdef,t.typname,CASE WHEN a.attlen=-1 THEN (CASE WHEN a.atttypmod=-1 THEN 0 ELSE a.atttypmod-4 END) ELSE a.attlen END as size
           FROM pg_class c,pg_attribute a,pg_type t
           WHERE c.relname=%s 
           AND c.oid=a.attrelid
           AND a.atttypid=t.oid`, self.table)
	log.Err(err)

	return lDs

}

// 转换
func (self *TModel) _validate(vals map[string]interface{}) {
	for key, val := range vals {
		if f := self.GetFieldByName(key); f != nil && !f.IsRelatedField() {
			switch f.Type() {
			case "boolean":
				vals[key] = utils.ToBool(val)
			case "integer":
				vals[key] = utils.ToInt(val)
			case "float":
				vals[key] = utils.ToFloat64(val)
			case "char", "text":
				vals[key] = utils.ToString(val)
			//case "blob":
			//	vals[key] = utils.ToBool(val)
			case "datetime", "date":
				vals[key] = utils.ToTime(val)
			case "many2one":
				// TODO 支持多种数据类型
				//self.osv.GetModel(f.relModelName)
				vals[key] = utils.ToInt(val)
			}
		}
	}
}
