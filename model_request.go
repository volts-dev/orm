package orm

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

type (
	CreateRequest struct {
		Context context.Context
		Data    []any // 多条数据记录
		Model   string
		Method  string
	}

	ReadRequest struct {
		Ids         []any
		Domain      string
		Field       string   // for relate method
		Fields      []string // 指定查询和返回字段
		Limit       int64
		Offset      int64
		Model       string
		Sort        []string
		Method      string
		UseNameGet  bool
		ClassicRead bool //
	}

	// 支持多条数据更新
	UpdateRequest struct {
		Context context.Context
		Ids     []any // 多条数据记录
		Data    []any // 多条数据记录
		Model   string
		Method  string
	}

	DeleteRequest struct {
		Context context.Context
		Ids     []any // 多条数据记录
		Model   string
		Method  string
	}

	UploadRequest struct {
		Model    string
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
	session := self.Tx()
	if session.IsAutoClose {
		defer session.Close()
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
	session := self.Tx()
	if session.IsAutoClose {
		defer session.Close()
	}
	session.Select(req.Fields...)

	if len(req.Ids) > 0 {
		session.Ids(req.Ids...)
	}

	if req.Domain != "" {
		session.Domain(req.Domain)
	}

	return session.Limit(req.Limit, req.Offset).Sort(req.Sort...).Read(req.ClassicRead)
}

// #被重载接口
func (self *TModel) Update(req *UpdateRequest) (int64, error) {
	session := self.Tx()
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
	session := self.Tx()
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
func (self *TModel) Load(field []string, records ...any) (ids []any, err error) {
	// 写入记录
	model, err := self.Orm().GetModel(self.String())
	if err != nil {
		return nil, err
	}

	// 由model初始化事物
	tx := model.Tx()
	tx.Begin()

	ids, err = model.Create(&CreateRequest{Data: records})
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		if e := tx.Rollback(err); e != nil {
			return nil, err
		}
	}

	return ids, nil
}

// #被重载接口
func (self *TModel) Uplaod(req *UploadRequest) (int64, error) {
	model, err := self.Clone()
	if err != nil {
		return 0, err
	}

	tx := model.Tx()
	if tx.IsAutoClose {
		defer tx.Close()
	}

	r := csv.NewReader(bytes.NewReader(req.Content))
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
	for {
		if count >= 1000 || isEOF {
			count = 0

			ids, err := model.Create(&CreateRequest{Data: utils.MapToAnyList(datas...)})
			if err != nil {
				return 0, err
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
		ids = ctx.Ids
	} else if ds.Count() != 0 {
		ids = ds.Keys(field.Name())
		if len(ids) == 0 {
			return nil, nil
		}
	}

	relateModel, err := self.orm.GetModel(field.RelateModelName())
	if err != nil {
		// # Should not happen, unless the foreign key is missing.
		return nil, err
	}

	group, err := relateModel.NameGet(ids)
	//group, err := relateModel.Records().Ids(ids).Read(ctx.ClassicRead)
	if err != nil {
		return nil, err
	}

	return group, nil
	/*
		if ds != nil {
			relateModel, err := ctx.Session.Orm().osv.GetModel(field.RelateModelName())
			if err != nil {
				// # Should not happen, unless the foreign key is missing.
				return nil, err
			}

			ds.First()
			for !ds.Eof() {
				// 获取关联表主键
				relId := ds.FieldByName(field.Name()).AsInterface()

				// igonre blank value
				if utils.IsBlank(relId) {
					if field.Required() {
						return nil, fmt.Errorf("the Many2One field %s:%s is required!", field.ModelName(), field.Name())
					}

					ds.Next()
					continue
				}

				relateDataset, err := relateModel.NameGet([]interface{}{relId})
				if err != nil {
					return nil, err
				}

				ds.FieldByName(field.Name()).AsInterface([]interface{}{relateDataset.FieldByName(relateModel.IdField()).AsInterface(), relateDataset.FieldByName(relateModel.GetRecordName()).AsInterface()})
				ds.Next()
			}
		}

		return data, nil
	*/
}

// 获取外键所有Child关联记录
func (self *TModel) OneToMany(ctx *TFieldContext) (*dataset.TDataSet, error) {
	field := ctx.Field
	ds := ctx.Dataset
	var ids []any
	if len(ctx.Ids) > 0 {
		ids = ctx.Ids
	} else if ds.Count() != 0 {
		ids = ds.Keys(ctx.Model.IdField())
		if len(ids) == 0 {
			return nil, nil
		}
	}

	if !field.IsRelatedField() || field.Type() != TYPE_O2M {
		return nil, fmt.Errorf("could not call model func OneToMany(%v,%v) from a not OneToMany field %v@%v!", ids, ctx.Field.Name(), field.IsRelatedField(), field.Type())
	}

	// # retrieve the lines in the comodel

	relModelName := field.RelateModelName()
	relFieldName := field.RelateFieldName()
	relateModel, err := self.orm.GetModel(relModelName)
	if err != nil {
		return nil, err
	}

	rel_filed := relateModel.GetFieldByName(relFieldName)
	if rel_filed == nil || rel_filed.Type() != TYPE_M2O {
		return nil, fmt.Errorf("the relate model <%s> field <%s> is not OneToMany type.", relModelName, relFieldName)
	}

	groups, err := relateModel.Records().In(relFieldName, ids...).Read(ctx.ClassicRead)
	if err != nil {
		log.Errf("OneToMany field %s search relate model %s faild", field.Name(), relateModel.String())
		return nil, err
	}

	return groups, nil
}

// many child -> one parent
func (self *TModel) ManyToOne(ctx *TFieldContext) (*dataset.TDataSet, error) {
	if !ctx.UseNameGet {
		// do nothing
		return nil, nil
	}

	field := ctx.Field
	ds := ctx.Dataset
	var ids []any
	if len(ctx.Ids) > 0 {
		ids = ctx.Ids
	} else if ds.Count() != 0 {
		ids = ds.Keys(field.Name())
		if len(ids) == 0 {
			return nil, nil
		}
	}

	// 检测字段是否合格
	if !field.IsRelatedField() || field.Type() != TYPE_M2O {
		return nil, fmt.Errorf("could not call model func One2many(%v,%v) from a not One2many field %v@%v!", ids, field.Name(), field.IsRelatedField(), field.Type())
	}

	relateModelName := field.RelateModelName()
	relateModel, err := self.orm.GetModel(relateModelName)
	if err != nil {
		return nil, err
	}

	group, err := relateModel.NameGet(ids)
	//group, err := relateModel.Records().Ids(ids...).Read(ctx.ClassicRead)
	if err != nil {
		log.Errf("Many2one field %s search relate model %s faild", field.Name(), relateModel.String())
		return nil, err
	}

	return group, nil
}

// return : map[id]record
func (self *TModel) ManyToMany(ctx *TFieldContext) (*dataset.TDataSet, error) {
	field := ctx.Field
	ds := ctx.Dataset
	var ids []any
	if len(ctx.Ids) > 0 {
		ids = ctx.Ids
	} else if ds.Count() != 0 {
		ids = ds.Keys(ctx.Model.IdField())
		if len(ids) == 0 {
			return nil, nil
		}
	}

	if !field.IsRelatedField() || field.Type() != TYPE_M2M {
		return nil, fmt.Errorf("could not call model func ManyToMany(%v,%v) from a not ManyToMany field %v@%v!", ids, field.Name(), field.IsRelatedField(), field.Type())
	}

	// # retrieve the lines in the comodel
	orm := self.orm
	relModelName := field.RelateModelName() //# 字段关联表名
	relFieldName := field.RelateFieldName()
	midModelName := field.MiddleModelName() //# 字段M2m关系表名
	midFieldName := field.MiddleFieldName()

	// 检测关联Model合法性
	if !orm.HasModel(relModelName) || !orm.HasModel(midModelName) {
		return nil, fmt.Errorf("the models are not correctable for ManyToMany(%s,%s)!", relModelName, midModelName)
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
	placeholder := JoinPlaceholder("?", ",", len(ids))
	//Many2many('res.lang', 'website_lang_rel', 'website_id', 'lang_id')
	//SELECT {rel}.{id1}, {rel}.{id2} FROM {rel}, {from_c} WHERE {where_c} AND {rel}.{id1} IN %s AND {rel}.{id2} = {tbl}.id {order_by} {limit} OFFSET {offset}
	query := JoinClause(
		"SELECT",
		midTableName+"."+relFieldName+","+midTableName+"."+midFieldName,
		"FROM",
		midTableName+","+from_c,
		"WHERE",
		where_c, //WHERE
		"AND",
		midTableName+"."+relFieldName,
		"IN ("+placeholder+") AND",
		midTableName+"."+midFieldName+"="+relTableName+".id",
		order_by, limit, offset,
	)
	params := append(where_params, ids...) // # 添加 IDs 作为参数

	// # 获取字段关联表的字符
	group := orm.Cacher.GetBySql(cacher_table_name, query, params)
	if group == nil {
		// TODO 只查询缺的记录不查询所有
		// # 如果缺省缓存记录重新查询
		group, err = sess.Query(query, params...)
		if err != nil {
			return nil, err
		}

		// # store result in cache
		orm.Cacher.PutBySql(cacher_table_name, query, where_params, group) // # 添加Sql查询结果
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
	if self.obj.GetFieldByName(self.nameField) != nil {
		id, err := self.Create(&CreateRequest{Data: []any{map[string]interface{}{
			self.nameField: name,
		}}})
		if err != nil {
			return nil, fmt.Errorf("cannot execute name_create, create name faild %s", err.Error())

		}
		return self.NameGet([]interface{}{id})
	} else {
		return nil, fmt.Errorf("Cannot execute name_create, no nameField defined on %s", self.name)
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
		return nil, log.Errf("Cannot execute name_search, no nameField defined on %s", self.name)
	}

	if name == "" && operator != "ilike" {
		lNew := utils.NewStringList()
		lNew.PushString(rec_name_field, operator, name)
		_domain.Push(lNew)
	}

	//access_rights_uid = name_get_uid or user
	// 获取匹配的Ids
	//lIds := self._search(lDomain, nil, 0, limit, "", false, access_rights_uid, context)
	result, err = self.Records().Select(self.idField, rec_name_field).Domain(_domain.String()).Limit(limit).Read()
	if err != nil {
		return nil, err
	}

	return result, nil //self.name_get(lIds, []string{"id", lNameField}) //self.SearchRead(lDomain.String(), []string{"id", lNameField}, 0, limit, "", context)
}
