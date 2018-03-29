package orm

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"vectors/logger"
	"vectors/utils"

	core "github.com/go-xorm/core"
)

type (
	// BaseModel 接口
	IModel interface {
		// private
		setBaseModel(model *TModel) //赋值初始化BaseModel
		relations_reload()

		//fields_get(allfields map[string]*TField, attributes []string, context map[string]string) (fields map[string]interface{})
		//check_access_rights(operation string) bool

		// public
		GetBase() *TModel
		GetBaseModel() *TModel

		// 对象被创建时
		Default(field string, val ...interface{}) (res interface{}) // 默认值修改获取
		One2many(ids, model string, fieldKey string) *TDataSet
		Many2many(detail_model, ref_model string, key_id, ref_id string) *TDataSet
		Inherits() []string
		GetModelName() string
		GetTableName() string
		GetTableDescription() string

		SetRecordName(n string)
		SetName(n string)
		//		SetRegistry(reg *TRegistry)
		//		Session() *TSession
		//Field(field string, val ...*TField) (result *TField) // 作废
		MethodByName(method string) *TMethod
		FieldByName(field string, val ...IField) (result IField)
		GetFields() map[string]IField
		RelateFieldByName(field string, val ...*TRelateField) (res *TRelateField)
		RelateFields() map[string]*TRelateField
		Relations() map[string]string

		Records() *TSession
		//Create(values map[string]interface{}) (id int64)
		//Read(ids []string, fields []string) *TDataSet
		//Write(ids []string, values interface{}) (err error)
		//update(vals map[string]interface{}, where string, args ...interface{}) (id int64)
		//Unlink(ids ...string) bool
		Osv() *TOsv
		Orm() *TOrm
		NameGet(ids []string) [][]string
		//Search(domain string, offset int64, limit int64, order string, count bool, context map[string]interface{}) []string
		//SearchRead(domain string, fields []string, offset int64, limit int64, order string, context map[string]interface{}) *TDataSet
		SearchName(name string, domain string, operator string, limit int64, name_get_uid string, context map[string]interface{}) *TDataSet
		//SearchCount(domain string, context map[string]interface{}) int
	}

	IModelPretected interface {
		IModel
		db_ref_table() *core.Table
	}

	// 所有成员都是Unexportable 小写,避免映射JSON,XML,ORM等时发生错误
	/*
	* 	字段命名规格 ："_xxx" "小写" 避免和子继承类覆盖
	* 	方法命名规格 ："GetXXX","SetXXX","XXByXX"
	 */
	TModel struct {
		// # common fields for all table
		//Id         int64     `field:"pk autoincr"`
		//CreateId   int64     `field:"int"`
		//CreateTime time.Time `field:"datetime created"` //-- Created on
		//WriteId    int64     `field:"int"`
		//WriteTime  time.Time `field:"datetime updated"`

		_cls_type  reflect.Type  // # Model 反射类
		_cls_value reflect.Value // # Model 反射值 供某些程序调用方法
		_name      string        // # xx.xx 映射在OSV的名称
		//_table         string                       // # xx_xx 映射在数据库上的名称
		_inherits      []string                     // # Pg数据库表继承 #考虑废弃
		_fields        map[string]IField            // # model的所有字段
		_relations     map[string]string            // # 存放关联表和个关联字段many2many many2one... 等关联表
		_relate_fields map[string]*TRelateField     // # 存放所有关联表字段由OSV初始化
		_common_fields map[string]map[string]IField // # [fld][tbl]
		_parent_name   string                       // #! 父表中的字段名称
		_parent_store  bool                         // #! 是否有父系关联 比如类目，菜单
		_default       map[string]interface{}       // # Model 字段默认值
		_sequence      string                       //
		_order         string                       //
		_module        string                       // # 属于哪个模块所有
		_rec_name      string                       // # 记录的名称字段 如字段Name,Title,PartNo
		_auto          bool                         // # True # create database backend
		_transient     bool                         // # 暂时的
		_description   string                       // # 描述
		is_base        bool                         // #该Model是否是基Model,并非扩展Model
		// # 核心对象
		orm         *TOrm
		osv         *TOsv
		statement   *TStatement
		session     *TSession
		table       *core.Table // TODO 大写 传送给Core包使用
		RecordField IField      //TODO 改名称RecordIdKeyField 表的唯一主键字段 自增/主键/唯一 如：Id

		// # 锁
		_fields_lock        sync.RWMutex
		_relations_lock     sync.RWMutex
		_relate_fields_lock sync.RWMutex
		_common_fields_lock sync.RWMutex

		// TODO
		fields  *TFieldsSet
		methods *TMethodsSet
	}
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

// @ name Sys.View
// @ Session
// @ Registry
func NewModel(name string, model_value reflect.Value, model_type reflect.Type) (mdl *TModel) {
	mdl = &TModel{
		_cls_type:  model_type,
		_cls_value: model_value,
		_name:      name,
		//_table:         strings.Replace(name, ".", "_", -1),
		_fields:        make(map[string]IField),
		_relate_fields: make(map[string]*TRelateField),
		_common_fields: make(map[string]map[string]IField),
		_relations:     make(map[string]string),
		_inherits:      make([]string, 0),
		_order:         "id",
		_default:       make(map[string]interface{}),
		_auto:          true,

		//registry:        session.Registry,
		//session:         session,
	}

	//mdl._sequence = mdl._table + "_id_seq"

	return
}

func (self *TModel) setBaseModel(model *TModel) {
	*self = *model
}

// 设置初始化Table变更后的信息
func (self *TModel) setBaseTable(table *core.Table) {
	self.table = table
	self._sequence = self.table.Name + "_id_seq"
}

func (self *TModel) SetRecordName(n string) {
	self._rec_name = n
}

// 获取主键字段名称
func (self *TModel) GetKeyName() string {
	return self.GetRecordName()
}

//_rec_name_fallback(self):
func (self *TModel) GetRecordName() string {
	// # if self._rec_name is set, it belongs to self._fields
	if self._rec_name != "" {
		return self._rec_name
	}
	return "id"
}

// pravite
func (self *TModel) SetName(n string) {
	self._name = n
	//self._table = strings.Replace(n, ".", "_", -1)
	self.table.Name = strings.Replace(n, ".", "_", -1)
}

// 获取Model名称
func (self *TModel) GetModelName() string {
	return self._name
}

// 返回Model的描述字符串
func (self *TModel) GetTableDescription() string {
	return self._description
}

// 实际注册的model原型
func (self *TModel) Interface() interface{} {
	return self._cls_value.Interface()
}

func (self *TModel) GetBase() *TModel {
	return self
}

// 废弃
func (self *TModel) GetBaseModel() *TModel {
	return self
}

func (self *TModel) Module() string {
	return self._module
}

// 获取Model方法
func (self *TModel) MethodByName(method string) (res_method *TMethod) {
	res_method = self.osv.GetMethod(self.GetModelName(), method)
	return
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
// 底层数据查询接口
func (self *TModel) _query(sql string, params ...interface{}) (res []map[string]interface{}, err error) {
	lRows, err := self.orm.DB().Query(sql, params)
	logger.LogErr(err)

	defer lRows.Close()
	for lRows.Next() {
		tempMap := make(map[string]interface{})
		err = lRows.ScanMap(tempMap)
		if !logger.LogErr(err) {
			res = append(res, tempMap)
		}
	}
	err = lRows.Err()
	logger.LogErr(err)

	return res, err
}
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

	query := self._where_calc(args, false, context)
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
		logger.LogErr(err)
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
	if logger.LogErr(err) {
		return nil
	}
	return res.Keys()
}
*/
func (self *TModel) _search_name(name string, args *utils.TStringList, operator string, limit int64, access_rights_uid string, context map[string]interface{}) (ds *TDataSet) {
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
			utils.Logger.Warn("Cannot execute name_search, no _rec_name defined on %s", self._name)
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

/*
   convert ``value`` from the cache to a value as returned by method
   :meth:`BaseModel.read`

   :param bool use_name_get: when True, value's diplay name will
       be computed using :meth:`BaseModel.name_get`, if relevant
       for the field
*/
func (self *TModel) convert_to_read(field IField, value interface{}, use_name_get bool) (result interface{}) {
	switch field.Type() {
	case "selection":
		lMehodName := field.Compute()
		//logger.Dbg("selection:", lMehodName, self._cls_value.MethodByName(lMehodName))
		if m := self.MethodByName(lMehodName); m != nil {
			/*		//if m := self._cls_value.MethodByName(lMehodName); m.IsValid() {
					//logger.Dbg("selection:", m, self._cls_value)
					m.SetArgs(self._cls_value)
					if m.Call() {
						if res, ok := m.AsInterface().([][]string); ok {
							field.Selections(res...)
						}
					}

					/*results := m.Call([]reflect.Value{self._cls_value}) //
					//logger.Dbg("selection:", results)
					if len(results) == 1 {
						//fld.Selection, _ = results[0].Interface().([][]string)
						if res, ok := results[0].Interface().([][]string); ok {
							field.Selections(res...)
						}

					}*/
		}
	case "one2many":
		break
	case "many2one":
		if use_name_get && value != nil && value != BlankNumItf && value != BlankStrItf {
			//# evaluate name_get() as superuser, because the visibility of a
			//# many2one field value (id and name) depends on the current record's
			//# access rights, and not the value's access rights.

			//   value_sudo = value.sudo()
			//# performance trick: make sure that all records of the same
			//# model as value in value.env will be prefetched in value_sudo.env
			// value_sudo.env.prefetch[value._name].update(value.env.prefetch[value._name])
			lModel, err := self.osv.GetModel(field.Base().RelateModelName()) // #i
			if err != nil {
				logger.LogErr(err, "@convert_to_read")
			}

			if lModel != nil {
				lId := utils.Itf2Str(value)
				//logger.Dbg("CTR:", field.RelateModelName(), lModel, value, lId)
				// 验证返回Id,Name 或者 空
				id_name := lModel.NameGet([]string{lId})
				if len(id_name) > 0 {
					return id_name[0]
				} else {
					return nil
				}

			} else {
				// # Should not happen, unless the foreign key is missing.
			}

		}
		return value
	default:
		if value != nil {
			return value
		} else {
			return nil
		}
	}
	return
}

// 获取字段值 for m2m,selection,
// return :map[string]interface{} 可以是map[id]map[field]vals,map[string]map[xxx][]string,
func (self *TModel) __field_value_get(ids []string, fields []*TField, values *TDataSet, context map[string]interface{}) (result map[string]map[string]interface{}) {
	lField := fields[0]
	switch lField.Type() {
	case "one2many":
		//if self._context:
		//    context = dict(context or {})
		//    context.update(self._context)

		//# retrieve the records in the comodel
		comodel, err := self.osv.GetModel(lField.RelateModelName()) //obj.pool[self._obj].browse(cr, user, [], context)
		if err != nil {
			logger.LogErr(err)
		}
		inverse := lField.RelateFieldName()
		//domain = self._domain(obj) if callable(self._domain) else self._domain
		// domain = domain + [(inverse, 'in', ids)]
		domain := fmt.Sprintf(`[('%s', 'in', [%s])]`, inverse, strings.Join(ids, ","))
		//records_ids := comodel.Search(domain, 0, 0, "", false, nil)
		lDs, _ := comodel.Records().Domain(domain).Read() // #i
		records_ids := lDs.Keys()
		// result = {id: [] for id in ids}
		//# read the inverse of records without prefetching other fields on them
		result = make(map[string]map[string]interface{})

		for _, id := range ids {
			for _, f := range fields {
				result[id] = make(map[string]interface{})
				result[id][f.Name()] = map[string][]string{id: records_ids}
			}
		}

		return result
	case "many2many": // "many2one" is classic write
	case "selection":
	}
	return
}

//根据名称创建简约记录
func (self *TModel) name_create() {

}

// 获得id和名称
func (self *TModel) _name_get(ids, fields []string) (result *TDataSet) {
	//result = self.Read(ids, fields)
	result, _ = self.Records().Select(fields...).Ids(ids...).Read()
	return
}

func (self *TModel) NameGet(ids []string) (result [][]string) {
	name := self._rec_name
	result = make([][]string, 0)

	if f := self.FieldByName(name); f != nil {
		// error

		//lDs := self.Read(ids, []string{"id", name})
		lDs, _ := self.Records().Select("id", name).Ids(ids...).Read()
		lDs.First()
		for !lDs.Eof() {
			val := []string{lDs.FieldByName("id").AsString(), lDs.FieldByName(name).AsString()}
			result = append(result, val)
			lDs.Next()
		}
	} else {
		for _, id := range ids {
			val := []string{id, fmt.Sprintf("%s,%s", self.GetModelName(), id)}
			result = append(result, val)
		}
	}

	return
}

func (self *TModel) SearchName(name string, domain string, operator string, limit int64, access_rights_uid string, context map[string]interface{}) (result *TDataSet) {
	if operator == "" {
		operator = "ilike"
	}

	if limit == 0 {
		limit = 100
	}

	if access_rights_uid == "" {
		//	access_rights_uid = self.session.AuthInfo("id")
	}

	lDomain := Query2StringList(domain)

	// 使用默认 name 字段
	if self._rec_name == "" {
		if fld := self.FieldByName("name"); fld != nil {
			self._rec_name = "name"
		}
	}
	// 检测name 字段
	if self._rec_name == "" {
		logger.Logger.Err("Cannot execute name_search, no _rec_name defined on %s", self._name)
		//logger.Dbg("SearchName:", name, domain, lDomain.String())
		return nil
	}

	if name == "" && operator != "ilike" {
		lNew := utils.NewStringList()
		lNew.PushString(self._rec_name, operator, name)
		lDomain.Push(lNew)
	}

	lNameField := ""
	if fld := self.FieldByName(self._rec_name); fld != nil {
		lNameField = self._rec_name
	} else {
		lNameField = self._name
	}

	//logger.Dbg("SearchName:", lNameField, lDomain.String())
	//access_rights_uid = name_get_uid or user
	// 获取匹配的Ids
	//lIds := self._search(lDomain, nil, 0, limit, "", false, access_rights_uid, context)
	lDs, _ := self.Records().Domain(lDomain.String()).Limit(int(limit)).Read()
	lIds := lDs.Keys()
	//result = self.Read(lIds, []string{"id", lNameField})
	result, _ = self.Records().Select("id", lNameField).Ids(lIds...).Read()
	return result //self.name_get(lIds, []string{"id", lNameField}) //self.SearchRead(lDomain.String(), []string{"id", lNameField}, 0, limit, "", context)
}

// 更新单一字段
func (self *TModel) _WriteField(id int64, field *TField, value string, rel_context map[string]interface{}) {
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

func (self *TModel) GetTableName() string {
	return self.table.Name
}

func (self *TModel) Inherits() []string {
	return self._inherits
}

func (self *TModel) db_ref_table() *core.Table {
	return self.table
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

func (self *TModel) Default(field string, val ...interface{}) (res interface{}) {
	if len(val) > 0 {
		self._default[field] = val[0]
		return val[0]
	} else {
		return self._default[field]
	}
	return
}

// # query model's field name XxxXxx as xxx_xxx
func (self *TModel) FieldByName(field string, val ...IField) (res IField) {
	//logger.Dbg("TModel.Field", len(val), field, val, val != nil)
	//logger.Dbg("TModel.Field2", len(self._fields), self._fields[field], self._fields)
	if val != nil {
		self._fields_lock.Lock()
		defer self._fields_lock.Unlock()
		self._fields[field] = val[0]
		return val[0]
	} else {
		self._fields_lock.RLock()
		defer self._fields_lock.RUnlock()
		return self._fields[field]
	}
	return
}

// 获得拥有该字段的所有表
func (self *TModel) CommonFieldByName(field string, val ...map[string]IField) (res map[string]IField) {
	if val != nil {
		self._common_fields_lock.Lock()
		defer self._common_fields_lock.Unlock()
		self._common_fields[field] = val[0]
		return val[0]
	} else {
		self._common_fields_lock.RLock()
		defer self._common_fields_lock.RUnlock()
		return self._common_fields[field]
	}
	return
}

// get the relate field by name.
func (self *TModel) RelateFieldByName(name string, fields ...*TRelateField) (res *TRelateField) {
	//logger.Dbg("TModel.Field", len(val), field, val, val != nil)
	//logger.Dbg("TModel.Field2", len(self._fields), self._fields[field], self._fields)

	if len(fields) > 0 {
		self._relate_fields_lock.Lock()
		defer self._relate_fields_lock.Unlock()
		if name == "" {
			for _, field := range fields {
				self._relate_fields[field.name] = field
			}
		} else {
			self._relate_fields[name] = fields[0]
		}
	} else {
		self._relate_fields_lock.RLock()
		defer self._relate_fields_lock.RUnlock()
		return self._relate_fields[name]
	}
	return
}

func (self *TModel) GetFields() map[string]IField {
	return self._fields
}

func (self *TModel) RelateFields() map[string]*TRelateField {
	return self._relate_fields
}

func (self *TModel) Relations() map[string]string {
	return self._relations
}

// Fields returns the fields collection of this model
func (self *TModel) Fields_() *TFieldsSet {
	return self.fields
}

// Methods returns the methods collection of this model
func (self *TModel) GetMethods() *TMethodsSet {
	return self.methods
}

// TODO 废除因为继承的一致性冲突
func (self *TModel) Osv() *TOsv {
	return self.osv
}

// TODO 废除因为继承的一致性冲突
func (self *TModel) Orm() *TOrm {
	return self.orm
}

// Provide api to query records from cache or database
func (self *TModel) Records() *TSession {
	lSession := self.orm.NewSession()
	lSession.model = self
	//lSession.Statement.TableNameClause = self.GetTableName()
	lSession.IsClassic = true

	return lSession.Model(self.GetModelName())
}

// Provide api to query records from cache or database
func (self *TModel) Db() *TSession {
	lSession := self.orm.NewSession()
	lSession.model = self
	return lSession
}

//""" Recompute the _inherit_fields mapping. """
//TODO 移动到ORM里实现
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
		fielss            map[string]IField
		relate_fields     map[string]*TRelateField
		tmp_relate_fields []*TRelateField
		size              int
		idx               int
	)

	for tbl, fld := range self._relations {
		//logger.Dbg("_relations_reload", tbl, strings.Replace(tbl, "_", ".", -1))
		rel_model, err := self.osv.GetModel(tbl) // #i //TableByName(tbl)
		if err != nil {
			logger.Logger.Err("Relation model %v can not find in osv or didn't register front of %v", tbl, self.GetTableName())
		}

		rel_model.relations_reload()
		fielss = rel_model.GetFields()
		relate_fields = rel_model.RelateFields()
		size = len(fielss) + len(relate_fields)
		tmp_relate_fields = make([]*TRelateField, size) // 临时

		idx = 0
		for name, field := range fielss {
			tmp_relate_fields[idx] = NewRelateField(name, tbl, fld, field, tbl)
			idx++
		}

		for name, source := range relate_fields {
			tmp_relate_fields[idx] = NewRelateField(name, tbl, fld, source.RelateField, source.RelateTopestTable)
			idx++
		}

		self.RelateFieldByName("", tmp_relate_fields...)
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

//     """ Determine inherited fields. """
// 添加关联字段到Model
func (self *TModel) _add_inherited_fields() {

	//# determine candidate inherited fields
	//	var fields = make([]*TField, 0)
	var lNew IField
	for parent_model, _ := range self._relations {
		parent, err := self.osv.GetModel(parent_model) // #i
		if err != nil {
			logger.LogErr(err, "@_add_inherited_fields")
		}

		for refname, ref := range parent.GetFields() {
			//# inherited fields are implemented as related fields, with the
			//# following specific properties:
			//#  - reading inherited fields should not bypass access rights
			//#  - copy inherited fields iff their original field is copied
			if has := self.FieldByName(refname); has != nil {
				lNew = utils.Clone(ref).(IField)
				//*lNew = *ref //复制关联字段
				lNew.IsForeignField(true)
				self.FieldByName(refname, ref)
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

// 获取外键所有Child关联2222222记录
//TODO ids []string
func (self *TModel) One2many(ids, model string, fieldKey string) (ds *TDataSet) {
	if model != "" && fieldKey != "" {
		lMOdelObj, err := self.osv.GetModel(model) // #i
		if err != nil {
			logger.Dbg("func GetById():", reflect.TypeOf(lMOdelObj), model)
		}

		lDomain := fmt.Sprintf(`[('%s', 'in', [%s])]`, fieldKey, ids)
		//logger.Dbg("One2many", lDomain)
		//ds = lMOdelObj.SearchRead(lDomain, nil, 0, 0, "", nil)
		ds, _ = lMOdelObj.Records().Domain(lDomain).Read()
	}
	return
}

func (self *TModel) Many2many(detail_model, ref_model string, key_id, ref_id string) *TDataSet {
	return nil
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
func (self *TModel) _select_column_data() *TDataSet {
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
           AND a.atttypid=t.oid`, self.GetTableName())
	logger.LogErr(err)

	return lDs

}

func (self *TModel) _table_exist() bool {
	lDs, err := self.orm.Query(`SELECT relname FROM pg_class WHERE relkind IN ('r','v') AND relname=%s`, self.GetTableName())
	logger.LogErr(err)
	return lDs.Count() > 0
}

// 由ORM接管
func (self *TModel) _create_table() {
	//   cr.execute('CREATE TABLE "%s" (id SERIAL NOT NULL, PRIMARY KEY(id))' % (self._table,))
	//   cr.execute(("COMMENT ON TABLE \"%s\" IS %%s" % self._table), (self._description,))
	//   _schema.debug("Table '%s': created", self._table)

}

//转换
func (self *TModel) _validate(vals map[string]interface{}) {
	for key, val := range vals {
		if f := self.FieldByName(key); f != nil && !f.IsRelated() {
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
				//logger.Dbg("datetime", key, val, utils.Itf2Time(val))
			case "many2one":
				// TODO 支持多种数据类型
				//self.osv.GetModel(f.relmodel_name)
				vals[key] = utils.Itf2Int(val)
			}
		}
	}
}
