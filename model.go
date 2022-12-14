package orm

import (
	"encoding/csv"
	"fmt"
	"io"
	"reflect"
	"strings"

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
	// BaseModel 接口
	IModel interface {
		// 获取继承的模型
		// 用处:super 用于方便调用不同层级模型的方法/查询等
		Super() IModel
		// 包含指定表模型的事务
		Tx() *TSession

		// private
		setOrm(o *TOrm)
		setBaseModel(model *TModel) //赋值初始化BaseModel
		relations_reload()

		//fields_get(allfields map[string]*TField, attributes []string, context map[string]string) (fields map[string]interface{})
		//check_access_rights(operation string) bool
		GetIndexes() map[string]*TIndex
		// public
		String() string   // model name in orm like "base.user"
		Table() string    // table name in database like "base_user"
		GetBase() *TModel // get the base model object
		GetColumnsSeq() []string
		GetPrimaryKeys() []string

		// 对象被创建时
		GetDefault() map[string]interface{}
		GetDefaultByName(fieldName string) (value interface{})
		SetDefaultByName(fieldName string, value interface{}) // 默认值修改获取
		/*	Object A can have one or many of objects B (E.g A person can have many cars).
			Relationship: A -> B = One -> Many = One2Many
			(You can select many cars while creating a person).	*/
		One2many(ids []interface{}, fieldName string) (*dataset.TDataSet, error)
		/*	Object B can only have one object of A.
			(E.g A car is owned by one person also many cars can be owned by the same person).
			Relationship: B -> A = Many -> One = Many2One	*/
		Many2one(ids []interface{}, fieldName string) (ds *dataset.TDataSet, err error)
		Many2many(ids []interface{}, fieldName string) (map[interface{}]*dataset.TDataSet, error)

		AddField(field IField)
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
		GetFields() map[string]IField
		//RelateFieldByName(field string, val ...*TRelatedField) (res *TRelatedField)
		//RelateFields() map[string]*TRelatedField
		//Relations() map[string]string
		IdField(field ...string) string

		// new a orm records session for query
		Records() *TSession

		// UTF file only
		Load(file io.Reader, breakcount ...int) (map[int]string, error)

		// Create 不带事务的
		Create(records ...any) ([]any, error)
		// Read 不带事务的
		Read(domain string, ids []interface{}, fields []string, limit int, sort string) (*dataset.TDataSet, error)
		// Update 不带事务的
		Update(values ...interface{}) (int64, error)
		// Delete 不带事务的
		Delete(ids ...interface{}) (int64, error)
		//Write(ids []string, values interface{}) (err error)
		//update(vals map[string]interface{}, where string, args ...interface{}) (id int64)
		//Unlink(ids ...string) bool
		Osv() *TOsv
		Obj() *TObj
		Orm() *TOrm
		//NameGet(ids []string) [][]string
		NameCreate(name string) (*dataset.TDataSet, error)
		NameGet(ids []interface{}) (*dataset.TDataSet, error)
		//Search(domain string, offset int64, limit int64, order string, count bool, context map[string]interface{}) []string
		//SearchRead(domain string, fields []string, offset int64, limit int64, order string, context map[string]interface{}) *dataset.TDataSet
		SearchName(name string, domain string, operator string, limit int64, name_get_uid string, context map[string]interface{}) (*dataset.TDataSet, error)
		//SearchCount(domain string, context map[string]interface{}) int
		BeforeSetup()
		AfterSetup()
	}

	// ???
	IModelPretected interface {
		IModel
	}

	// 所有成员都是Unexportable 小写,避免映射JSON,XML,ORM等时发生错误
	/*
	* 	字段命名规格 ："_xxx" "小写" 避免和子继承类覆盖
	* 	方法命名规格 ："GetXXX","SetXXX","XXByXX"
	 */
	TModel struct {
		// # 核心对象
		modelType   reflect.Type  // # Model 反射类
		modelValue  reflect.Value // # Model 反射值 供某些程序调用方法
		orm         *TOrm
		osv         *TOsv
		obj         *TObj
		super       IModel    // 继承的Model名称
		transaction *TSession // 事务

		name  string // # xx.xx 映射在OSV的名称
		table string // mapping table name
		// below vars must name as "_xxx" to avoid mixed inherited-object's vars
		//_fields       map[string]IField // # model的所有字段
		_parent_name  string // #! 父表中的字段名称
		_parent_store bool   // #! 是否有父系关联 比如类目，菜单
		_sequence     string //
		_order        string //
		_module       string // # 属于哪个模块所有
		_rec_name     string // the field name which is the name represent a record @examples: Name,Title,PartNo
		idField       string // the field name which is the UID represent a record
		_auto         bool   // # True # create database backend
		_transient    bool   // # 暂时的
		_description  string // # 描述
		is_base       bool   // #该Model是否是基Model,并非扩展Model
	}
)

// 新建模型 不带其他信息
// @ Session
// @ Registry
func newModel(name, tableName string, modelValue reflect.Value, modelType reflect.Type) (model *TModel) {
	if len(name) > 0 && len(tableName) == 0 {
		tableName = fmtTableName(name)
	}

	if len(name) == 0 && len(tableName) > 0 {
		name = fmtModelName(tableName)
	}

	model = &TModel{
		name:       name,
		table:      tableName,
		modelType:  modelType,
		modelValue: modelValue,
		_order:     "id",
		_auto:      true,
	}

	//mdl._sequence = mdl._table + "_id_seq"

	return model
}

/*
// TODO 包含同步上个下文Session
super() 函数是用于调用父类(超类)的一个方法。
super 是用来解决多重继承问题的，直接用类名调用父类方法在使用单继承的时候没问题，但是如果使用多继承，会涉及到查找顺序（MRO）、重复调用（钻石继承）等种种问题。
MRO 就是类的方法解析顺序表, 其实也就是继承父类方法时的顺序表。
*/
func (self *TModel) Super() IModel {
	/*//mod, err := self.Session().GetModel(self.GetName())
	su, err := self.Orm().GetModel(self.super)
	if err != nil {
		log.Errf("create product record failed:%s", err.Error())
	}
	return su*/
	return self.super
}

func (self *TModel) Tx() *TSession {
	if self.transaction == nil {
		self.transaction = self.Records()
	}
	return self.transaction
}

// #被重载接口 创建记录 提供给继承
func (self *TModel) Create(records ...any) ([]any, error) {
	recs := self.Tx()
	if recs.IsAutoClose {
		defer recs.Close()
	}

	ids := make([]any, len(records))
	for i, d := range records {
		id, err := recs.Create(d)
		if err != nil {
			return nil, recs.Rollback(err)
		}
		ids[i] = id
	}

	return ids, nil
}

// #被重载接口
func (self *TModel) Read(domain string, ids []interface{}, fields []string, limit int, sort string) (*dataset.TDataSet, error) {
	recs := self.Tx()
	if recs.IsAutoClose {
		defer recs.Close()
	}

	if limit == 0 {
		limit = 10
	}

	recs.Select(fields...)
	cnt := len(ids)
	if cnt > 0 {
		if cnt > 250 { // 限制ids获取量
			limit = 250
		}
		return recs.Ids(ids...).Limit(int64(limit)).Sort(strings.Split(sort, ",")...).Read()
	}

	if domain != "" {
		recs.Domain(domain)
	}

	return recs.Limit(int64(limit)).Sort(strings.Split(sort, ",")...).Read()
}

// #被重载接口
func (self *TModel) Update(values ...interface{}) (int64, error) {
	recs := self.Tx()
	if recs.IsAutoClose {
		defer recs.Close()
	}

	var effectCount int64
	for _, d := range values {
		id, err := recs.Write(d)
		if err != nil {
			return 0, err
		}
		effectCount += id
	}

	return effectCount, nil
}

// #被重载接口
func (self *TModel) Delete(ids ...any) (int64, error) {
	recs := self.Tx()
	if recs.IsAutoClose {
		defer recs.Close()
	}

	effectCount, err := recs.Delete(ids...)
	if err != nil {
		return 0, err
	}

	return effectCount, nil
}

func (self *TModel) setOrm(o *TOrm) {
	self.orm = o
}

func (self *TModel) setBaseModel(model *TModel) {
	*self = *model
	self._sequence = self.name + "_id_seq"

}

func (self *TModel) SetRecordName(fieldName string) {
	self._rec_name = fieldName
}

// return the field name of record's name field
func (self *TModel) GetRecordName() string {
	// # if self._rec_name is set, it belongs to self._fields
	if fld := self.GetFieldByName(self._rec_name); fld == nil {
		if self._rec_name == "" {
			if fld := self.GetFieldByName("name"); fld != nil {
				self._rec_name = "name"
			} else {
				self._rec_name = self.idField
			}
		}
	}

	return self._rec_name
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
	return self._description
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
	return self._module
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

/*
// :param access_rights_uid: optional user ID to use when checking access rights
// (not for ir.rules, this is only for ir.model.access)

	func (self *TModel) _search(args *utils.TStringList, fields []string, offset int64, limit int64, order string, count bool, access_rights_uid string, context map[string]interface{}) (result []string) {
		var (
			//		fields_str string
			where_str  string
			limit_str  string
			offset_str string
			query_str  string
			order_by   string
			err        error
		)

		if context == nil {
			context = make(map[string]interface{})
		}

		//	self.check_access_rights("read")

		// 如果有返回字段
		//if fields != nil {
		//	fields_str = strings.Join(fields, ",")
		//} else {
		//	fields_str = `*`
		//}

		query := self.where_calc(args, false, context)
		order_by = self._generate_order_by(order, query, context) // TODO 未完成
		from_clause, where_clause, where_clause_params := query.get_sql()
		if where_clause == "" {
			where_str = ""
		} else {
			where_str = fmt.Sprintf(` WHERE %s`, where_clause)
		}

		if count {
			// Ignore order, limit and offset when just counting, they don't make sense and could
			// hurt performance
			query_str = `SELECT count(1) FROM ` + from_clause + where_str
			lRes, err := self.orm.SqlQuery(query_str, where_clause_params...)
			log.Err(err)
			return []string{lRes.FieldByName("count").AsString()}
		}

		if limit > 0 {
			limit_str = fmt.Sprintf(` limit %d`, limit)
		}
		if offset > 0 {
			offset_str = fmt.Sprintf(` offset %d`, offset)
		}

		query_str = fmt.Sprintf(`SELECT "%s".id FROM `, self._table) + from_clause + where_str + order_by + limit_str + offset_str
		web.Debug("_search", query_str, where_clause_params)
		res, err := self.orm.SqlQuery(query_str, where_clause_params...)
		if log.Err(err) {
			return nil
		}
		return res.Keys()
	}
*/
func (self *TModel) __search_name(name string, args *utils.TStringList, operator string, limit int64, access_rights_uid string, context map[string]interface{}) (ds *dataset.TDataSet) {
	/*	// private implementation of name_search, allows passing a dedicated user
		// for the name_get part to solve some access rights issues
		if args == nil {
			args = NewStringList()
		}

		if operator == "" {
			operator = "ilike"
		}

		if limit < 1 {
			limit = 100
		}
		// optimize out the default criterion of ``ilike ''`` that matches everything
		if self._rec_name == "" {
			utils.log.Warn("Cannot execute name_search, no _rec_name defined on %s", self.name)
		} else if name != "" && operator != "ilike" {
			lDomain := NewStringList()
			lDomain.PushString(self._rec_name)
			lDomain.PushString(operator)
			lDomain.PushString(name)
			args.Push(lDomain)
		}

		if access_rights_uid == "" {
			access_rights_uid = self.session.AuthInfo("id")
		}

		web.Debug("_searc_name", args.String())
		lIds := self._search(args, []string{"name"}, 0, limit, "", false, access_rights_uid, context)
		//ds = self.SearchRead(args.String(), []string{"name"}, 0, limit, "", context)
		return self.name_get(lIds, []string{"name"})
	*/
	return
}

//根据名称创建简约记录
/*
name_create(name) -> record

Create a new record by calling :meth:`~.create` with only one value
provided: the display name of the new record.

The new record will be initialized with any default values
applicable to this model, or provided through the context. The usual
behavior of :meth:`~.create` applies.

:param name: display name of the record to create
:rtype: tuple
:return: the :meth:`~.name_get` pair value of the created record
*/
func (self *TModel) NameCreate(name string) (*dataset.TDataSet, error) {
	if self.obj.GetFieldByName(self._rec_name) != nil {
		id, err := self.Create(map[string]interface{}{
			self._rec_name: name,
		})
		if err != nil {
			return nil, log.Errf("cannot execute name_create, create name faild %s", err.Error())

		}
		return self.NameGet([]interface{}{id})
	} else {
		return nil, log.Errf("Cannot execute name_create, no _rec_name defined on %s", self.name)
	}
}

// 获得id和名称
func (self *TModel) NameGet(ids []interface{}) (*dataset.TDataSet, error) {
	name := self.GetRecordName()
	id_field := self.idField
	if f := self.GetFieldByName(name); f != nil {
		ds, err := self.Records().Select(id_field, name).Ids(ids...).Read()
		if err != nil {
			return nil, err
		}

		return ds, nil
	} else {
		ds := dataset.NewDataSet()
		for _, id := range ids {
			ds.NewRecord(map[string]interface{}{
				id_field: id,
				name:     self.String(),
			})
		}

		return ds, nil
	}

	//return nil, fmt.Errorf("%s Call NameGet() failure! Arg: %v", self.GetName(), ids)
}

// search record by name field only
func (self *TModel) SearchName(name string, domain_str string, operator string, limit int64, access_rights_uid string, context map[string]interface{}) (result *dataset.TDataSet, err error) {
	if operator == "" {
		operator = "ilike"
	}

	if limit == 0 {
		limit = 100
	}

	if access_rights_uid == "" {
		//	access_rights_uid = self.session.AuthInfo("id")
	}

	_domain, err := domain.String2Domain(domain_str)
	if err != nil {
		return nil, err
	}

	// 使用默认 name 字段
	rec_name_field := self.GetRecordName()
	if rec_name_field == "" {
		return nil, log.Errf("Cannot execute name_search, no _rec_name defined on %s", self.name)
	}

	if name == "" && operator != "ilike" {
		lNew := utils.NewStringList()
		lNew.PushString(rec_name_field, operator, name)
		_domain.Push(lNew)
	}

	//log.Dbg("SearchName:", lNameField, lDomain.String())
	//access_rights_uid = name_get_uid or user
	// 获取匹配的Ids
	//lIds := self._search(lDomain, nil, 0, limit, "", false, access_rights_uid, context)
	result, err = self.Records().Select(self.idField, rec_name_field).Domain(_domain.String()).Limit(limit).Read()
	if err != nil {
		return nil, err
	}

	return result, nil //self.name_get(lIds, []string{"id", lNameField}) //self.SearchRead(lDomain.String(), []string{"id", lNameField}, 0, limit, "", context)
}

// 更新单一字段
func (self *TModel) __WriteField(id int64, field *TField, value string, rel_context map[string]interface{}) {
	//self._update_field(id, field, value, rel_context)
}

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

/*
Import given data in given module

	This method is used when importing data via client menu.

	Example of fields to import for a sale.order::

	    .id,                         (=database_id)
	    partner_id,                  (=name_search)
	    order_line/.id,              (=database_id)
	    order_line/name,
	    order_line/product_id/id,    (=xml id)
	    order_line/price_unit,
	    order_line/product_uom_qty,
	    order_line/product_uom/id    (=xml_id)

	This method returns a 4-tuple with the following structure::

	    (return_code, errored_resource, error_message, unused)

	* The first item is a return code, it is ``-1`` in case of
	  import error, or the last imported row number in case of success
	* The second item contains the record data dict that failed to import
	  in case of error, otherwise it's 0
	* The third item contains an error message string in case of error,
	  otherwise it's 0
	* The last item is currently unused, with no specific semantics

	:param fields: list of fields to import
	:param datas: data to import
	:param mode: 'init' or 'update' for record creation
	:param current_module: module name
	:param noupdate: flag for record creation
	:param filename: optional file to store partial import state for recovery
	:returns: 4-tuple in the form (return_code, errored_resource, error_message, unused)
	:rtype: (int, dict or 0, str or 0, str or 0)
*/
func (self *TModel) import_data() {

}

// @Return map: row index in csv file fail and error message
func (self *TModel) Load(file io.Reader, breakcount ...int) (map[int]string, error) {
	onceInsert := 50000
	if breakcount != nil {
		onceInsert = breakcount[0]
	}

	csvFile := csv.NewReader(file)
	header, err := csvFile.Read()
	if err != nil {
		return nil, log.Err(err)
	}

	// 建立过滤索引
	ignoreIdx := make([]int, 0)
	for idx, name := range header {
		if self.GetFieldByName(name) == nil {
			ignoreIdx = append(ignoreIdx, idx)
		}
	}

	row := 0 // 当前csv 行
	errRows := make(map[int]string)
	var successRows []int
	rec := dataset.NewRecordSet()
	isEof := false
	for !isEof {
		count := 0
		rec.Reset()
		successRows = make([]int, 0)

		session := self.Records()
		session.Begin()
		for count < onceInsert {
			row++

			line, err := csvFile.Read()
			if err != nil {
				if err == io.EOF {
					isEof = true
					break
				}
				errRows[row] = log.Err(err).Error()
				break
			}

			// 写入记录
			for idx, val := range line {
				if utils.InInts(idx, ignoreIdx...) == -1 { // 过滤不合法字段
					rec.FieldByName(header[idx]).AsInterface(val)
				}
			}

			id, err := session.Create(rec.AsItfMap())
			if id == nil || err != nil {
				errRows[row] = log.Err(err).Error()
				break
			}

			successRows = append(successRows, row)
			count++
		}

		err = session.Commit()
		if err != nil {
			log.Err(err)

			// 添加提交失败的行
			for _, r := range successRows {
				errRows[r] = "Commit fail!"
			}

			e := session.Rollback(err)
			if e != nil {
				log.Err(e)
			}
		}
	}

	return errRows, nil
}

func (self *TModel) GetDefault() map[string]interface{} {
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
func (self *TModel) GetFields() map[string]IField {
	return self.obj.GetFields()
}

func (self *TModel) AddField(field IField) {
	self.obj.AddField(field)
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

// TODO 废除因为继承的一致性冲突
func (self *TModel) Osv() *TOsv {
	return self.osv
}

func (self *TModel) Obj() *TObj {
	return self.obj
}

// TODO 废除因为继承的一致性冲突
func (self *TModel) Orm() *TOrm {
	return self.orm
}

// Provide api to query records from cache or database
func (self *TModel) Records() *TSession {
	lSession := self.orm.NewSession()
	lSession.IsClassic = true

	return lSession.Model(self.String())
}

// Provide api to query records from cache or database
func (self *TModel) Db() *TSession {
	lSession := self.orm.NewSession()
	lSession.Statement.model = self
	return lSession
}

// """ Recompute the _inherit_fields mapping. """
// TODO 移动到ORM里实现
// 重载关联表字段到_relate_fields里 _relate_fields的赋值在此实现
// 条件必须所有Model都注册到OSV里
func (self *TModel) relations_reload() {
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
		fielss        map[string]IField
		relate_fields map[string]*TRelatedField
	)

	for tbl, fld := range self.obj.GetRelations() {
		//log.Dbg("_relations_reload", tbl, strings.Replace(tbl, "_", ".", -1))
		rel_model, err := self.osv.GetModel(tbl) // #i //TableByName(tbl)
		if err != nil {
			log.Errf("Relation model %v can not find in osv or didn't register front of %v", tbl, self.String())
		}

		rel_model.relations_reload()
		fielss = rel_model.GetFields()
		relate_fields = rel_model.Obj().GetRelatedFields() //RelateFields()

		for name, field := range fielss {
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
	}

}

//	""" Determine inherited fields. """
//
// 添加关联字段到Model
func (self *TModel) _add_inherited_fields() {

	//# determine candidate inherited fields
	//	var fields = make([]*TField, 0)
	var lNew IField
	for parent_model := range self.obj.GetRelations() {
		parent, err := self.osv.GetModel(parent_model) // #i
		if err != nil {
			log.Err(err, "@_add_inherited_fields")
		}

		for refname, ref := range parent.GetFields() {
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
	}

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

// TODO 返回值待确定
func (self *TModel) GetRelate(ids []interface{}, fieldName string) (ds *dataset.TDataSet, err error) {
	field := self.GetFieldByName(fieldName)
	typ := field.Type()
	if !field.IsRelatedField() {
		return nil, fmt.Errorf("could not call model func One2many(%v,%v) from a not One2many field %v@%v!", ids, fieldName, field.IsRelatedField(), field.Type())
	}

	switch typ {
	//case TYPE_O2O:
	//return self.One2one(ids, fieldName)
	case TYPE_O2M:
		return self.One2many(ids, fieldName)
	case TYPE_M2O:
		return self.Many2one(ids, fieldName)
	case TYPE_M2M:
		//return self.Many2many(ids, fieldName)
	}

	return nil, fmt.Errorf("the type <%s> of relate field not implemented!", typ)
}

// 获取外键所有Child关联记录
func (self *TModel) One2many(ids []interface{}, fieldName string) (ds *dataset.TDataSet, err error) {
	field := self.GetFieldByName(fieldName)
	if !field.IsRelatedField() || field.Type() != TYPE_O2M {
		return nil, fmt.Errorf("could not call model func One2many(%v,%v) from a not One2many field %v@%v!", ids, fieldName, field.IsRelatedField(), field.Type())
	}

	// # retrieve the lines in the comodel

	relModelName := field.RelateModelName()
	relkey_field_name := field.RelateFieldName()
	relateModel, err := self.orm.GetModel(relModelName)
	if err != nil {
		return nil, err
	}

	rel_filed := relateModel.GetFieldByName(relkey_field_name)
	if rel_filed.Type() != TYPE_M2O {
		return nil, log.Errf("the relate model <%s> field <%s> is not many2one type.", relModelName, relkey_field_name)
	}

	recs := relateModel.Tx()
	if recs.IsAutoClose {
		defer recs.Close()
	}
	ds, err = recs.In(relkey_field_name, ids...).Read()
	if err != nil {
		log.Errf("One2Many field %s search relate model %s faild", field.Name(), relateModel.String())
		return nil, err
	}

	return ds, nil
}

func (self *TModel) Many2one(ids []interface{}, fieldName string) (ds *dataset.TDataSet, err error) {

	field := self.GetFieldByName(fieldName)
	if !field.IsRelatedField() || field.Type() != TYPE_M2O {
		return nil, fmt.Errorf("could not call model func One2many(%v,%v) from a not One2many field %v@%v!", ids, fieldName, field.IsRelatedField(), field.Type())
	}

	relateModelName := field.RelateModelName()
	relateModel, err := self.orm.GetModel(relateModelName)
	if err != nil {
		return nil, err
	}

	recs := relateModel.Tx()
	if recs.IsAutoClose {
		defer recs.Close()
	}
	ds, err = recs.Ids(ids...).Read()
	if err != nil {
		log.Errf("Many2one field %s search relate model %s faild", field.Name(), relateModel.String())
		return nil, err
	}

	return ds, nil
}

// return : map[id]record
func (self *TModel) Many2many(ids []interface{}, fieldName string) (map[interface{}]*dataset.TDataSet, error) {
	field := self.GetFieldByName(fieldName)
	if !field.IsRelatedField() || field.Type() != TYPE_M2M {
		return nil, fmt.Errorf("could not call model func One2many(%v,%v) from a not One2many field %v@%v!", ids, fieldName, field.IsRelatedField(), field.Type())
	}

	// # retrieve the lines in the comodel
	orm := self.orm
	relModelName := field.RelateModelName() //# 字段关联表名
	relFieldName := field.RelateFieldName()
	midModelName := field.MiddleModelName() //# 字段M2m关系表名
	midFieldName := field.MiddleFieldName()

	// 检测关联Model合法性
	if !orm.HasModel(relModelName) || !orm.HasModel(midModelName) {
		return nil, fmt.Errorf("the models are not correctable for many2many")
	}

	domainNode, err := domain.String2Domain(field.Domain())
	if err != nil {
		return nil, err
	}

	sess := orm.NewSession()
	defer sess.Close()

	//table_name := field.comodel_name//sess.Statement.TableName()
	sess.Model(relModelName)
	wquery, err := sess.Statement.where_calc(domainNode, false, nil)
	if err != nil {
		return nil, err
	}
	order_by := sess.Statement.generate_order_by(wquery, nil)
	from_c, where_c, where_params := wquery.getSql()
	if where_c == "" {
		where_c = "1=1"
	}

	limit := ""
	if field.Base().limit > 0 {
		limit = fmt.Sprintf("LIMIT %v", field.Base().limit)
	}

	offset := ""

	midTableName := fmtTableName(midModelName)
	relTableName := fmtTableName(relModelName)
	// the table name in cacher
	cacher_table_name := relTableName + "_" + from_c

	//Many2many('res.lang', 'website_lang_rel', 'website_id', 'lang_id')
	//SELECT {rel}.{id1}, {rel}.{id2} FROM {rel}, {from_c} WHERE {where_c} AND {rel}.{id1} IN %s AND {rel}.{id2} = {tbl}.id {order_by} {limit} OFFSET {offset}
	query := fmt.Sprintf(
		`SELECT %s.%s, %s.%s FROM %s, %s WHERE %s AND %s.%s IN (?) AND %s.%s = %s.id %s %s %s`,
		midTableName, relFieldName, midTableName, midFieldName,
		midTableName, from_c, // FROM
		where_c,                    //WHERE
		midTableName, relFieldName, // AND
		midTableName, midFieldName, relTableName, //AND
		order_by, limit, offset,
	)

	result := make(map[interface{}]*dataset.TDataSet)
	var res_ds *dataset.TDataSet
	for _, id := range ids {
		//ds.First()
		//for !ds.Eof() {
		//id := ds.FieldByName(id_field).AsInterface()

		// # 添加 IDs 作为参数
		params := append(where_params, id)

		// # 获取字段关联表的字符
		res_ds = orm.Cacher.GetBySql(cacher_table_name, query, params)
		if res_ds == nil {
			// TODO 只查询缺的记录不查询所有
			// # 如果缺省缓存记录重新查询

			ds, err := sess.Query(query, params...)
			if err != nil {
				return nil, err
				//continue
			}

			// # store result in cache
			orm.Cacher.PutBySql(cacher_table_name, query, where_params, ds) // # 添加Sql查询结果
			res_ds = ds
		}

		result[id] = res_ds
	}

	return result, nil
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
			//webgo.Debug("_Validate", key, val, f._type)
			switch f.Type() {
			case "boolean":
				vals[key] = utils.Itf2Bool(val)
			case "integer":
				vals[key] = utils.Itf2Int(val)
			case "float":
				vals[key] = utils.Itf2Float(val)
			case "char", "text":
				vals[key] = utils.Itf2Str(val)
			//case "blob":
			//	vals[key] = utils.Itf2Bool(val)
			case "datetime", "date":
				vals[key] = utils.Itf2Time(val)
				//log.Dbg("datetime", key, val, utils.Itf2Time(val))
			case "many2one":
				// TODO 支持多种数据类型
				//self.osv.GetModel(f.relModelName)
				vals[key] = utils.Itf2Int(val)
			}
		}
	}
}
func (self *TModel) BeforeSetup() {}
func (self *TModel) AfterSetup()  {}
