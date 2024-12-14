package orm

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type (
	___Paginator struct {
		PageIndex int64 `json:"page"  `   //当前页数
		PageSize  int64 `json:"limit"   ` //每页多少条
		//Offset int64 `json:"offset" ` //偏移量
		Total int64 `json:"total" ` //总页数
	}

	// Model字段请求
	FieldRequest struct {
		Fields []string
		Model  string
	}

	OnConflict struct {
		Fields []string
		//Where        Where
		//TargetWhere  Where
		OnConstraint string
		DoNothing    bool
		DoUpdates    []string
		UpdateAll    bool
	}

	CreateRequest struct {
		Context    context.Context
		Data       []any  // * 多条数据记录
		Model      string // *
		Method     string
		OnConflict OnConflict
	}

	// ReadRequest 结构体用于定义读取请求的参数，它扩展了 Paginator 结构体的功能并增加了特定的查询和排序选项。
	ReadRequest struct {
		// Paginator 是一个可以被继承的结构，用于分页查询
		//Paginator

		// Ids 是一个切片，可以存放任何类型的ID
		Ids []any

		// Domain 是一个字符串，用于指定查询的域
		Domain string

		// Field 是一个字符串，用于关联方法
		Field string

		// Fields 是一个字符串切片，用于指定查询和返回的字段
		Fields []string

		// Funcs 是一个字符串切片，表示要应用的SQL函数
		Funcs []string

		// GroupBy 是一个字符串切片，用于指定分组依据的字段
		GroupBy []string

		// OrderBy 是一个字符串切片，用于指定排序依据的字段
		OrderBy []string

		// Page 当前页数
		Page int64 `json:"page"`

		// Limit 每页多少条记录，-1则无限制
		Limit int64 `json:"limit"`

		// Model 是一个字符串，用于指定查询的模型
		Model string

		// Sort 是一个字符串切片，用于指定排序方式
		Sort []string

		// Method 是一个字符串，用于指定查询方法
		Method string

		// UseNameGet 是一个布尔值，用于控制是否查询Many2one字段的[Id,Name]，默认为false
		UseNameGet bool

		// ClassicRead 是一个布尔值，用于控制是否查询Many2one的所有字段，默认为false
		ClassicRead bool
	}

	// 支持多条数据更新
	UpdateRequest struct {
		Context context.Context
		Ids     []any    // 多条数据记录
		Data    []any    // 多条数据记录
		Fields  []string // 指定查询和返回字段
		Domain  string   // update 支持查询条件
		Model   string   // *
		Method  string
	}

	DeleteRequest struct {
		Context context.Context
		Ids     []any  // 多条数据记录
		Domain  string // delete 支持查询条件
		Model   string // *
		Method  string
	}

	UploadRequest struct {
		Model    string // *
		ResModel string
		ResField string
		ResId    any
		FileName string
		FileSize int64
		Content  []byte
	}
)

// #被重载接口 创建记录 提供给继承
func (self *TModel) Create(req *CreateRequest) ([]any, error) {
	model, err := self.Clone() /* 克隆首要目的获得自定义模型结构和事务*/
	if err != nil {
		return nil, err
	}

	session := model.Tx()
	if session.IsAutoClose {
		defer session.Close()
	}

	if req.OnConflict.Fields != nil || req.OnConflict.DoUpdates != nil || req.OnConflict.DoNothing || req.OnConflict.UpdateAll || req.OnConflict.OnConstraint != "" {
		session.OnConflict(&req.OnConflict)
	}

	ids := make([]any, len(req.Data))
	for i, d := range req.Data {
		id, err := session.Create(d)
		if err != nil {
			return nil, session.Rollback(err)
		}
		ids[i] = id
	}

	return ids, nil
}

// #被重载接口

// func (self *TModel) Read(domain string, ids []interface{}, fields []string, limit int, sort string) (*dataset.TDataSet, error) {
func (self *TModel) Read(req *ReadRequest) (*dataset.TDataSet, error) {
	model, err := self.Clone() /* 克隆首要目的获得自定义模型结构和事务*/
	if err != nil {
		return nil, err
	}

	session := model.Tx()
	if session.IsAutoClose {
		defer session.Close()
	}
	// 确保第一页
	if req.Page < 1 {
		req.Page = 1
	}

	if req.Domain != "" {
		session.Domain(req.Domain)
	}

	switch strings.ToLower(req.Method) {
	case "count":
		count, err := session.Count()
		if err != nil {
			return nil, err
		}
		return dataset.NewDataSet(dataset.WithData(map[string]any{
			"count": count,
		})), nil
	case "one2many":
		if req.Field == "" {
			return nil, fmt.Errorf("must pionted the field name for one2many@%s read!", req.Model)
		}

		return self.OneToMany(&TFieldContext{
			Ids:         req.Ids,
			Field:       self.GetFieldByName(req.Field),
			Fields:      req.Fields,
			UseNameGet:  req.UseNameGet,
			ClassicRead: req.ClassicRead,
		})
	case "many2many":
		if req.Field == "" {
			return nil, fmt.Errorf("must pionted the field name for many2many@%s read!", req.Model)
		}

		return self.ManyToMany(&TFieldContext{
			Model:   self,
			Ids:     req.Ids,
			Field:   self.GetFieldByName(req.Field),
			Fields:  req.Fields,
			Session: session,
			//UseNameGet:  req.UseNameGet,
			//ClassicRead: req.ClassicRead,
		})
	case "groupby":
		return self.GroupBy(req)
	}

	session.Select(req.Fields...)

	if len(req.Ids) > 0 {
		session.Ids(req.Ids...)
	}

	session.UseNameGet = req.UseNameGet

	return session.Limit(req.Limit, req.Page-1).OrderBy(strings.Join(req.OrderBy, ",")).Sort(req.Sort...).Read(req.ClassicRead)
}

// #被重载接口
func (self *TModel) Update(req *UpdateRequest) (int64, error) {
	model, err := self.Clone() /* 克隆首要目的获得自定义模型结构和事务*/
	if err != nil {
		return 0, err
	}

	session := model.Tx()
	if session.IsAutoClose {
		defer session.Close()
	}

	var effectCount int64

	// 更新多个ID上的数据
	if len(req.Ids) > 0 {
		if len(req.Data) != 1 {
			return 0, fmt.Errorf("can't update multi data to multi ids!")
		}
		data := req.Data[0]
		for _, id := range req.Ids {
			id, err := session.Ids(id).Write(data)
			if err != nil {
				return 0, err
			}
			effectCount += id
		}
		return effectCount, nil
	}

	if req.Domain != "" {
		session.Domain(req.Domain)
	}

	for _, d := range req.Data {
		id, err := session.Write(d)
		if err != nil {
			return 0, err
		}
		effectCount += id
	}

	return effectCount, nil
}

// #被重载接口
func (self *TModel) Delete(req *DeleteRequest) (int64, error) {
	model, err := self.Clone() /* 克隆首要目的获得自定义模型结构和事务*/
	if err != nil {
		return 0, err
	}

	session := model.Tx()
	if session.IsAutoClose {
		defer session.Close()
	}

	effectCount, err := session.Delete(req.Ids...)
	if err != nil {
		return 0, err
	}

	return effectCount, nil
}

// 带事务加载上传数据
// @Return map: row index in csv file fail and error message
func (self *TModel) Load(field []string, records ...any) ([]any, error) {
	model, err := self.Clone() /* 克隆首要目的获得自定义模型结构和事务*/
	if err != nil {
		return nil, err
	}

	session := model.Tx()
	session.Begin()

	ids, err := model.Create(&CreateRequest{Data: records})
	if err != nil {
		return nil, err
	}

	if err = session.Commit(); err != nil {
		if e := session.Rollback(err); e != nil {
			return nil, err
		}
	}

	return ids, nil
}

// #被重载接口
func (self *TModel) Upload(req *UploadRequest) (int64, error) {
	model, err := self.Clone() /* 克隆首要目的获得自定义模型结构和事务*/
	if err != nil {
		return 0, err
	}

	session := model.Tx()
	if session.IsAutoClose {
		defer session.Close()
	}

	fallback := unicode.UTF8.NewDecoder()
	unicode.BOMOverride(fallback)
	rd := transform.NewReader(bytes.NewReader(req.Content), unicode.BOMOverride(fallback))

	r := csv.NewReader(rd)
	r.LazyQuotes = true // 支持引号
	header, err := r.Read()
	if err != nil {
		return 0, err
	}

	// 建立过滤索引
	ignoreIdx := make([]int, 0)
	for idx, name := range header {
		if self.GetFieldByName(name) == nil {
			ignoreIdx = append(ignoreIdx, idx)
		}
	}

	if len(header) == len(ignoreIdx) {
		return 0, fmt.Errorf("the upload data must contain header!")
	}

	var datas []map[string]any
	count := 0
	var isEOF bool
	var total int64
	var ids []any
	for {
		if count >= 1000 || isEOF {
			count = 0

			// 提前提交到数据库
			if !isEOF {
				/* 新的事务提交到数据库 */
				model, err := self.Clone()
				if err != nil {
					return 0, err
				}

				//tx2 := model.Tx(session.Clone())
				tx2 := model.Tx()
				tx2.Begin()

				ids, err = model.Create(&CreateRequest{Data: utils.MapToAnyList(datas...)})
				if err != nil {
					return 0, err
				}

				if err := tx2.Commit(); err != nil {
					return 0, tx2.Rollback(err)
				}
			} else {
				ids, err = model.Create(&CreateRequest{Data: utils.MapToAnyList(datas...)})
				if err != nil {
					return 0, err
				}
			}

			datas = make([]map[string]any, 0)
			total += int64(len(ids))
		}

		line, err := r.Read()
		if err != nil {
			if err == io.EOF {
				if count > 0 {
					isEOF = true
					continue
				}
				break
			}
			return 0, err
		}

		record := make(map[string]any)
		for i, header := range header {
			//if utils.InInts(i, ignoreIdx...) == -1 { // 过滤不合法字段
			record[header] = line[i]
			//}
		}
		datas = append(datas, record)
		count++
	}

	return total, nil
}

func (self *TModel) OneToOne(ctx *TFieldContext) (*dataset.TDataSet, error) {
	if !ctx.UseNameGet {
		// do nothing
		return nil, nil
	}

	field := ctx.Field
	ds := ctx.Dataset
	var ids []any
	if len(ctx.Ids) > 0 {
		ids = unique(ctx.Ids)
	} else if ds.Count() != 0 {
		ids = unique(ds.Keys(field.Name()))
	}
	if len(ids) == 0 {
		return nil, nil
	}

	relateModel, err := self.orm.GetModel(field.RelatedModelName(), WithContext(ctx.Model.Options().Context))
	if err != nil {
		// # Should not happen, unless the foreign key is missing.
		return nil, err
	}

	//group, err := relateModel.NameGet(ids)
	group, err := relateModel.Records().Ids(ids).Read(ctx.ClassicRead)
	if err != nil {
		return nil, err
	}

	return group, nil
}

// 获取外键所有Child关联记录
func (self *TModel) OneToMany(ctx *TFieldContext) (*dataset.TDataSet, error) {
	ds := ctx.Dataset
	var ids []any
	if len(ctx.Ids) > 0 {
		ids = unique(ctx.Ids)
	} else if ds.Count() != 0 {
		ids = unique(ds.Keys(ctx.Model.IdField()))
	}

	if len(ids) == 0 {
		return nil, nil
	}
	field := ctx.Field

	if !field.IsRelatedField() || field.Type() != TYPE_O2M {
		return nil, fmt.Errorf("could not call model func OneToMany(%v,%v) from a not OneToMany field %v@%v!", ids, ctx.Field.Name(), field.IsRelatedField(), field.Type())
	}

	// # retrieve the lines in the comodel

	relModelName := field.RelatedModelName()
	relFieldName := field.RelatedFieldName()
	relateModel, err := self.orm.GetModel(relModelName)
	if err != nil {
		return nil, err
	}

	rel_filed := relateModel.GetFieldByName(relFieldName)
	if rel_filed == nil || rel_filed.Type() != TYPE_M2O {
		return nil, fmt.Errorf("the relate model <%s> field <%s> is not OneToMany type.", relModelName, relFieldName)
	}

	session := relateModel.Records()
	session.UseNameGet = ctx.UseNameGet /* 使用 */
	groups, err := session.Select(ctx.Fields...).In(relFieldName, ids...).Read(false)
	if err != nil {
		log.Errf("OneToMany field %s search relate model %s faild", field.Name(), relateModel.String())
		return nil, err
	}

	return groups, nil
}

// many child -> one parent
func (self *TModel) ManyToOne(ctx *TFieldContext) (*dataset.TDataSet, error) {
	if ctx.ClassicRead || ctx.UseNameGet {
		field := ctx.Field
		ds := ctx.Dataset
		var ids []any
		if len(ctx.Ids) > 0 {
			ids = unique(ctx.Ids)
		} else if ds.Count() != 0 {
			ids = unique(ds.Keys(field.Name()))
		}
		if len(ids) == 0 {
			return nil, nil
		}

		// 检测字段是否合格
		if !field.IsRelatedField() || field.Type() != TYPE_M2O {
			return nil, fmt.Errorf("could not call model func One2many(%v,%v) from a not One2many field %v@%v!", ids, field.Name(), field.IsRelatedField(), field.Type())
		}

		relateModelName := field.RelatedModelName()
		relateModel, err := self.orm.GetModel(relateModelName, WithContext(ctx.Model.Options().Context))
		if err != nil {
			return nil, err
		}

		var group *dataset.TDataSet
		if ctx.ClassicRead {
			group, err = relateModel.Records().Ids(ids...).Read(false)
			//group, err := relateModel.Records().Ids(ids...).Read(ctx.ClassicRead)
		} else if ctx.UseNameGet {
			group, err = relateModel.NameGet(ids)
		}
		if err != nil {
			log.Errf("Many2one field %s search relate model %s faild", field.Name(), relateModel.String())
			return nil, err
		}

		return group, nil
	}

	// do nothing
	return nil, nil
}

// return : map[id]record
func (self *TModel) ManyToMany(ctx *TFieldContext) (*dataset.TDataSet, error) {
	ds := ctx.Dataset
	var ids []any
	if len(ctx.Ids) > 0 {
		ids = unique(ctx.Ids)
	} else if ds.Count() != 0 {
		ids = unique(ds.Keys(ctx.Model.IdField()))
	}

	field := ctx.Field

	if !field.IsRelatedField() || field.Type() != TYPE_M2M {
		return nil, fmt.Errorf("could not call model func ManyToMany(%v,%v) from a not ManyToMany field %v@%v!", ids, field.Name(), field.IsRelatedField(), field.Type())
	}

	var err error
	var domainNode *domain.TDomainNode
	if ctx.Domain != "" {
		domainNode, err = domain.String2Domain(ctx.Domain, ds)
		if err != nil {
			return nil, err
		}
	}

	if expr := field.Domain(); expr != "" {
		node, err := domain.String2Domain(expr, ds)
		if err != nil {
			return nil, err
		}

		if domainNode == nil {
			domainNode = node
		} else {
			domainNode.AND(node)
		}
	}

	if len(ids) == 0 && domainNode.Count() == 0 {
		return nil, nil
	}

	// # retrieve the lines in the comodel
	relModelName := field.RelatedModelName() //# 字段关联表名
	relFieldName := field.RelatedFieldName()
	midModelName := field.MiddleModelName() //# 字段M2m关系表名
	midFieldName := field.MiddleFieldName()

	// 检测关联Model合法性
	orm := self.orm
	if !orm.HasModel(relModelName) || !orm.HasModel(midModelName) {
		return nil, fmt.Errorf("the models are not correctable for ManyToMany(%s,%s)!", relModelName, midModelName)
	}

	sess := NewSession(orm)
	defer sess.Close()
	/* 复制必要字段 */
	if ctx.Session != nil {
		sess.Sets = ctx.Session.Sets
	}

	//table_name := field.comodel_name//sess.Statement.TableName()
	midTableName := fmtTableName(midModelName)
	relTableName := fmtTableName(relModelName)
	query := ""
	order_by := ""
	placeholder := JoinPlaceholder("?", ",", len(ids))
	limit := ""
	if field.Base().limit > 0 {
		limit = fmt.Sprintf("LIMIT %v", field.Base().limit)
	}

	var params []any

	//Many2many('res.lang', 'website_lang_rel', 'website_id', 'lang_id')
	//SELECT {rel}.{id1}, {rel}.{id2} FROM {rel}, {from_c} WHERE {where_c} AND {rel}.{id1} IN %s AND {rel}.{id2} = {tbl}.id {order_by} {limit} OFFSET {offset}

	/* 经典模式返回关联表数据 */
	if ctx.ClassicRead {
		sess.Model(relModelName)
		wquery, err := sess.Statement.where_calc(domainNode, false, nil)
		if err != nil {
			return nil, err
		}
		order_by = sess.Statement.generate_order_by(wquery, nil)
		from_c, where_c, where_params := wquery.getSql()

		if where_c == "" {
			where_c = "1=1"
		}

		query = JoinClause(
			"SELECT",
			midTableName+".*,"+relTableName+".*",
			"FROM",
			midTableName+","+from_c,
			"WHERE",
			where_c, //WHERE
			"AND",
			midTableName+"."+midFieldName,
			"IN ("+placeholder+")",
			"AND",
			midTableName+"."+relFieldName+"="+relTableName+".id",
			order_by, limit,
		)

		params = append(where_params, ids...) // # 添加 IDs 作为参数
	} else {
		sess.Model(midModelName)
		query = JoinClause(
			"SELECT",
			midTableName+"."+relFieldName+","+midTableName+"."+midFieldName,
			"FROM",
			midTableName,
			"WHERE",
			midTableName+"."+midFieldName,
			"IN ("+placeholder+")",
			order_by, limit,
		)
		params = ids
	}

	// # 获取字段关联表的字符
	// the table name in cacher
	cacher_table_name := midTableName + "_" + relTableName
	group := orm.Cacher.GetBySql(cacher_table_name, query, params)
	if group == nil {
		// TODO 只查询缺的记录不查询所有
		// # 如果缺省缓存记录重新查询

		group, err = sess.Query(query, params...)
		if err != nil {
			return nil, err
		}

		// # store result in cache
		orm.Cacher.PutBySql(cacher_table_name, query, params, group) // # 添加Sql查询结果
	}

	return group, nil
}

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
	if self.obj.GetFieldByName(self.recName) != nil {
		model, err := self.Clone()
		if err != nil {
			return nil, err
		}

		ids, err := model.Create(&CreateRequest{Data: []any{map[string]any{
			self.recName: name,
		}}})
		if err != nil {
			return nil, fmt.Errorf("cannot execute name_create, create name faild %s", err.Error())

		}
		return self.NameGet(ids)
	} else {
		return nil, fmt.Errorf("Cannot execute name_create, no nameField defined on %s", self.name)
	}
}

// 获得id和名称
func (self *TModel) NameGet(ids []any) (*dataset.TDataSet, error) {
	name := self.GetRecordName()
	id_field := self.idField
	if f := self.GetFieldByName(name); f != nil {
		model, err := self.Clone()
		if err != nil {
			return nil, err
		}

		ds, err := model.Records().Select(id_field, name).Ids(ids...).Read()
		if err != nil {
			return nil, err
		}

		return ds, nil
	}

	ds := dataset.NewDataSet()
	for _, id := range ids {
		ds.NewRecord(map[string]interface{}{
			id_field: id,
			name:     self.String(),
		})
	}

	return ds, nil
}

// search record by name field only
func (self *TModel) NameSearch(name string, domainNode *domain.TDomainNode, operator string, limit int64, access_rights_uid string, context map[string]interface{}) (result *dataset.TDataSet, err error) {
	if operator == "" {
		operator = "ilike"
	}

	if limit == 0 {
		limit = DefaultLimit
	}

	if access_rights_uid == "" {
		//	access_rights_uid = self.session.AuthInfo("id")
	}

	/* */
	if domainNode.Count() == 0 && name == "" {
		return nil, log.Errf("Cannot execute name_search without the query params such like Name value or Domain!")
	}

	// 使用默认 name 字段
	rec_name_field := self.GetRecordName()
	if rec_name_field == "" {
		return nil, log.Errf("Cannot execute name_search, no nameField defined on model %s", self.name)
	}

	/* 添加 name 查询语句 */
	if name != "" {
		if operator == "" {
			operator = "ilike"
		}
		if domainNode == nil {
			domainNode = domain.New(rec_name_field, operator, name)
		} else {
			domainNode.AND(domain.New(rec_name_field, operator, name))
		}
	}

	//access_rights_uid = name_get_uid or user
	// 获取匹配的Ids
	model, err := self.Clone()
	if err != nil {
		return nil, err
	}

	result, err = model.Records().Select(self.idField, rec_name_field).Domain(domainNode).Limit(limit).Read()
	if err != nil {
		return nil, err
	}

	return result, nil //self.name_get(lIds, []string{"id", lNameField}) //self.SearchRead(lDomain.String(), []string{"id", lNameField}, 0, limit, "", context)
}

// 计算并获得字段默认值
func (self *TModel) DefaultGet(fields ...string) (map[string]any, error) {
	data := make(map[string]any)
	for _, fieldName := range fields {
		field := self.GetFieldByName(fieldName)
		if field == nil {
			continue
		}

		var value any
		if fn := field.DefaultFunc(); fn != nil {
			ctx := &TFieldContext{
				//Session: self,
				Model: self.super,
				//Dataset: data,
				Field: field,
				//Value:   value,
				//Ids:     ids,
			}

			if err := fn(ctx); err != nil {
				return nil, err
			}

			value = ctx.values
		} else if v := self.GetDefaultByName(fieldName); v != nil {
			value = v
		}

		data[fieldName] = field.onConvertToWrite(nil, value)
	}

	return data, nil
}

func (self *TModel) GroupBy(req *ReadRequest) (*dataset.TDataSet, error) {
	model, err := self.Clone() /* 克隆首要目的获得自定义模型结构和事务*/
	if err != nil {
		return nil, err
	}

	session := model.Tx()
	if session.IsAutoClose {
		defer session.Close()
	}
	// 确保第一页
	if req.Page < 1 {
		req.Page = 1
	}
	session.UseNameGet = req.UseNameGet
	return session.
		Select(req.Fields...).
		Funcs(req.Funcs...).
		Limit(req.Limit, req.Page-1).
		Sort(req.Sort...).
		GroupBy(req.GroupBy...).
		Read()
}
