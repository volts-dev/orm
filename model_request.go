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
		Domain any

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
		Offset int64

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

		// SubFields 为每个关系字段(m2o/o2m/m2m)提供嵌套的子读取规格，使单次 read
		// 即可把每个关系字段所需的子字段内嵌进返回数据集，而无需各组件独立调用 API：
		//   m2o          -> 子记录(map)，仅含 Fields 指定列(如 display_name)
		//   many2many标签 -> 子记录列表，仅含 Fields 指定列(如 display_name,color)
		//   one2many内嵌list -> 子记录列表，含 list 视图可见列
		// 键为本模型上的关系字段名；值的 Fields/Domain/SubFields 描述其 comodel 的读取，
		// SubFields 可递归以支持多层(如 o2m 行内的 m2o 列)。
		SubFields map[string]*ReadRequest
	}

	// 支持多条数据更新
	UpdateRequest struct {
		Context context.Context
		Ids     []any    // 多条数据记录
		Data    []any    // 多条数据记录
		Fields  []string // 指定查询和返回字段
		Domain  any      // update 支持查询条件
		Model   string   // *
		Method  string
	}

	DeleteRequest struct {
		Context context.Context
		Ids     []any  // 多条数据记录
		Domain  any    // delete 支持查询条件
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

	return session.Create(req.Data...)
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
	// Offset is a direct SQL offset (0-based); negative values are treated as 0.
	if req.Offset < 0 {
		req.Offset = 0
	}

	if req.Domain != nil {
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
			return nil, fmt.Errorf("must point the field name for one2many@%s read!", req.Model)
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
			return nil, fmt.Errorf("must point the field name for many2many@%s read!", req.Model)
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

	fields := req.Fields
	if len(req.SubFields) > 0 {
		// 注入嵌套子读取规格，并确保带子规格的关系字段出现在 Select 中，
		// 否则它们不会进入 computedFields，OnRead 派发会跳过、无法内嵌子记录。
		session.subReads = req.SubFields
		fields = withSubFieldNames(fields, req.SubFields)
	}

	session.Select(fields...)

	if len(req.Ids) > 0 {
		session.Ids(req.Ids...)
	}

	session.UseNameGet = req.UseNameGet

	rs := session.Limit(req.Limit, req.Offset).OrderBy(strings.Join(req.OrderBy, ",")).Sort(req.Sort...)
	if req.ClassicRead {
		rs = rs.Classic()
	}
	return rs.Read()
}

// withSubFieldNames 返回 fields 与 subFields 的键的并集(保持原有顺序、去重)，
// 确保每个带嵌套子规格的关系字段都在 Select 列表里，从而进入关系字段派发被内嵌。
func withSubFieldNames(fields []string, subFields map[string]*ReadRequest) []string {
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		seen[f] = true
	}
	out := fields
	for name := range subFields {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// ensureFields 在 fields 前补齐缺失的 required 字段(去重、保持 required 在前)。
// 关系字段的子读取必须带上 id 及反向 FK 等连接键，否则无法按键分组回填到父记录。
func ensureFields(fields []string, required ...string) []string {
	if len(fields) == 0 {
		return fields // 空表示读取全部字段，连接键天然包含
	}
	present := make(map[string]bool, len(fields))
	for _, f := range fields {
		present[f] = true
	}
	var prefix []string
	for _, r := range required {
		if r != "" && !present[r] {
			present[r] = true
			prefix = append(prefix, r)
		}
	}
	if len(prefix) == 0 {
		return fields
	}
	return append(prefix, fields...)
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

	if req.Domain != nil && req.Domain != "" {
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
		return nil, session.Rollback(err) // 回滚并释放连接，避免已 Begin 的事务泄露
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
					return 0, tx2.Rollback(err) // 回滚并释放连接，避免事务泄露（与 EOF 分支一致）
				}

				if err := tx2.Commit(); err != nil {
					return 0, tx2.Rollback(err)
				}
			} else {
				/* EOF 分支也需要事务保护 */
				model2, err := self.Clone()
				if err != nil {
					return 0, err
				}
				tx2 := model2.Tx()
				tx2.Begin()
				ids, err = model2.Create(&CreateRequest{Data: utils.MapToAnyList(datas...)})
				if err != nil {
					return 0, tx2.Rollback(err)
				}
				if err := tx2.Commit(); err != nil {
					return 0, tx2.Rollback(err)
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
			if utils.IndexOf(i, ignoreIdx...) == -1 { // 过滤不合法字段
				record[header] = line[i]
			}
		}
		datas = append(datas, record)
		count++
	}

	return total, nil
}

// relAnchorKey 返回本记录上"锚定关系读取"的字段名。
// 普通关系字段用本模型主键；_inherits 委托来的关系字段(IsInherited)则改用委托外键
// (如 partner_id)——因为 o2m/m2m 关系锚定在父模型主键上，本记录通过该 FK 持有父主键值。
// 不是继承字段、或解析不到委托 FK 时回退到本模型主键，保持原有行为。
func relAnchorKey(ctx *TFieldContext) string {
	if ctx.Field != nil && ctx.Field.IsInherited() {
		if fk := ctx.Model.Obj().GetRelationByName(ctx.Field.ModelName()); fk != "" {
			return fk
		}
	}
	return ctx.Model.IdField()
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
	rs := relateModel.Records()
	// 同 ManyToOne：Records() 新会话不继承调用方 schema，schema 隔离租户下子读取
	// 会落错 schema 查空。
	if ctx.Session != nil {
		rs.SetSchema(ctx.Session.Schema)
	}
	rs = rs.Ids(ids)
	if ctx.ClassicRead {
		rs = rs.Classic()
	}
	group, err := rs.Read()
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
		ids = unique(ds.Keys(relAnchorKey(ctx)))
	}

	if len(ids) == 0 {
		return nil, nil
	}
	field := ctx.Field

	if !field.IsRelated() || field.TypeName() != TYPE_O2M {
		return nil, fmt.Errorf("could not call model func OneToMany(%v,%v) from a not OneToMany field %v@%v!", ids, ctx.Field.Name(), field.IsRelated(), field.TypeName())
	}

	// # retrieve the lines in the comodel

	relModelName := field.RelatedModelName()
	relFieldName := field.RelatedKeyName()
	relateModel, err := self.orm.GetModel(relModelName)
	if err != nil {
		return nil, err
	}

	// 反向键须是指回本表的 many2one；one2one(委托继承/_inherits)本质是带唯一约束的
	// many2one，其 FK 列同样物理存在(store=true)，故一并接受，使委托表可作 o2m 目标。
	rel_filed := relateModel.GetFieldByName(relFieldName)
	if rel_filed == nil || (rel_filed.TypeName() != TYPE_M2O && rel_filed.TypeName() != TYPE_O2O) {
		return nil, fmt.Errorf("the relate model <%s> field <%s> is not a many2one/one2one back-reference for one2many.", relModelName, relFieldName)
	}

	session := relateModel.Records()
	// 同 ManyToOne:Records() 新会话不继承调用方 schema,非默认 schema 租户下子读取
	// 会落错 schema,o2m 列表悄悄返回空。
	if ctx.Session != nil {
		session.SetSchema(ctx.Session.Schema)
	}
	session.UseNameGet = ctx.UseNameGet /* 使用 */
	if len(ctx.SubFields) > 0 {
		// 透传下一层嵌套规格，支持 o2m 行内的 m2o/m2m 列再内嵌(多层)。
		session.subReads = ctx.SubFields
	}
	// 子读取必须带上 id 与反向 FK(relFieldName)，否则 OnRead 无法按反向键分组回填。
	selFields := ensureFields(ctx.Fields, relateModel.IdField(), relFieldName)
	groups, err := session.Select(selFields...).In(relFieldName, ids...).Read()
	if err != nil {
		log.Errf("OneToMany field %s search relate model %s failed", field.Name(), relateModel.String())
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
		if !field.IsRelated() || field.TypeName() != TYPE_M2O {
			return nil, fmt.Errorf("could not call model func One2many(%v,%v) from a not One2many field %v@%v!", ids, field.Name(), field.IsRelated(), field.TypeName())
		}

		relateModelName := field.RelatedModelName()
		relateModel, err := self.orm.GetModel(relateModelName, WithContext(ctx.Model.Options().Context))
		if err != nil {
			return nil, err
		}

		var group *dataset.TDataSet
		if ctx.ClassicRead {
			sub := relateModel.Records()
			// Records() 起一个全新会话,不继承调用方 session 的 schema。非默认 schema
			// 的租户(如 VectorsSystem 用 "system")下,子读取会落到 search_path 默认
			// schema(通常 public)查 comodel,找不到匹配行——classic 内嵌悄悄失败,
			// 字段只剩裸 id(不报错,前端表现为"m2o 读不出详情/写完读不到")。
			if ctx.Session != nil {
				sub.SetSchema(ctx.Session.Schema)
			}
			if len(ctx.Fields) > 0 {
				// 限定 comodel 列范围(如仅 display_name)；id 必须带上以便按主键分组回填。
				sub.Select(ensureFields(ctx.Fields, relateModel.IdField())...)
			}
			if len(ctx.SubFields) > 0 {
				// 透传下一层嵌套规格，支持 m2o 目标记录自身的关系列内嵌(多层)。
				sub.subReads = ctx.SubFields
			}
			group, err = sub.Ids(ids...).Read()
		} else if ctx.UseNameGet {
			group, err = relateModel.NameGet(ids)
		}
		if err != nil {
			log.Errf("Many2one field %s search relate model %s failed", field.Name(), relateModel.String())
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
		ids = unique(ds.Keys(relAnchorKey(ctx)))
	}

	field := ctx.Field

	if !field.IsRelated() || field.TypeName() != TYPE_M2M {
		return nil, fmt.Errorf("could not call model func ManyToMany(%v,%v) from a not ManyToMany field %v@%v!", ids, field.Name(), field.IsRelated(), field.TypeName())
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

	if len(ids) == 0 && (domainNode == nil || domainNode.Count() == 0) {
		return nil, nil
	}

	// # retrieve the lines in the comodel
	relModelName := field.RelatedModelName() //# 字段关联表名
	relFieldName := field.RelatedKeyName()
	midModelName := field.JoinModelName() //# 字段M2m关系表名
	midFieldName := field.JoinSourceKey()

	// 检测关联Model合法性
	orm := self.orm
	if !orm.HasModel(relModelName) || !orm.HasModel(midModelName) {
		return nil, fmt.Errorf("the models are not correctable for ManyToMany(%s,%s)!", relModelName, midModelName)
	}

	sess := NewSession(orm)
	defer sess.Close()
	/* 复制必要字段 */
	if ctx.Session != nil {
		ctx.Session.setsLock.RLock()
		sess.Sets = ctx.Session.Sets
		ctx.Session.setsLock.RUnlock()
		// NewSession 起的新会话不继承调用方 schema。非默认 schema 租户(如
		// VectorsSystem 用 "system")下,下面手工拼接的 FROM/JOIN 裸表名(不经过
		// where_calc/qualifiedTable 那套 schema 限定)会落到 search_path 默认
		// schema(通常 public),m2m 悄悄查空——company_ids/group_ids 之类字段读出
		// 来是 []而非真实数据(不报错,表现为"关系字段没有返回")。
		sess.SetSchema(ctx.Session.Schema)
	}

	//table_name := field.comodel_name//sess.Statement.TableName()
	midTableName := fmtTableName(midModelName)
	relTableName := fmtTableName(relModelName)
	// ClassicRead 分支下 from_c(经 where_calc/qualifiedTable 生成)已经带 schema
	// 前缀；这里手工拼接的 midTableName/relTableName 引用不经过那条路径,必须单独
	// 补上,否则两边 schema 不一致导致 JOIN 落空。
	if sess.Schema != "" {
		midTableName = sess.Schema + "." + midTableName
		relTableName = sess.Schema + "." + relTableName
	}
	query := ""
	order_by := ""
	placeholder := JoinPlaceholder("?", ",", len(ids))
	limit := ""
	if field.Base().m2mLimit > 0 {
		limit = fmt.Sprintf("LIMIT %v", field.Base().m2mLimit)
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

		// Tx() reuses the inherited transaction (propagated by Clone) so this
		// read can see writes that the caller has not yet committed; falls
		// back to a fresh session when there is no transaction.
		ds, err := model.Tx().Select(id_field, name).Ids(ids...).Limit(utils.ToInt64(len(ids))).Read()
		if err != nil {
			return nil, err
		}

		return ds, nil
	}

	ds := dataset.NewDataSet()
	for _, id := range ids {
		ds.NewRecord(map[string]any{
			id_field: id,
			name:     self.String(),
		})
	}

	return ds, nil
}

// search record by name field only
func (self *TModel) NameSearch(name string, domainNode *domain.TDomainNode, operator string, limit int64, access_rights_uid string, context map[string]any) (result *dataset.TDataSet, err error) {
	if operator == "" {
		operator = "ilike"
	}

	if limit == 0 {
		limit = DefaultLimit
	}

	// TODO: if access_rights_uid == "" { access_rights_uid = self.session.AuthInfo("id") }

	/* */
	if (domainNode == nil || domainNode.Count() == 0) && name == "" {
		return nil, log.Errf("Cannot execute name_search without the query params such like Name value or Domain!")
	}

	// 使用默认 name 字段
	rec_name_field := self.GetRecordName()
	if rec_name_field == "" {
		return nil, log.Errf("Cannot execute name_search, no nameField defined on model %s", self.name)
	}

	/* 添加 name 查询语句 */
	if name != "" {
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

	// Reuse the inherited transaction (see TModel.Clone) so name_search invoked
	// inside an open write transaction can match against uncommitted rows.
	result, err = model.Tx().Select(self.idField, rec_name_field).Domain(domainNode).Limit(limit).Read()
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
			// ctx.Model 必须是完整的外层模型(prototype)而非裸 TModel 基类，
			// 否则 defaultFunc 里对上层接口(如 module.IModel)的断言会 panic
			mdl := IModel(self)
			if self.prototype != nil {
				mdl = self.prototype
			}
			if self.super != nil {
				mdl = self.super
			}
			ctx := &TFieldContext{
				//Session: self,
				Model: mdl,
				//Dataset: data,
				Field: field,
				//Value:   value,
				//Ids:     ids,
			}

			if err := fn(ctx); err != nil {
				return nil, err
			}

			value = ctx.values
		} else if ok, v := self.GetDefaultByName(fieldName); ok {
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
	// Offset is a direct SQL offset (0-based); negative values are treated as 0.
	if req.Offset < 0 {
		req.Offset = 0
	}
	session.UseNameGet = req.UseNameGet
	return session.
		Select(req.Fields...).
		Funcs(req.Funcs...).
		Limit(req.Limit, req.Offset).
		Sort(req.Sort...).
		GroupBy(req.GroupBy...).
		Read()
}
