package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/dataset"
)

// GroupCountField 是 read_group 结果里「本组记录数」那一列的列名，对齐 Odoo 的 __count。
const GroupCountField = "__count"

// ReadGroupRequest 是 ReadGroup 的模型级入参，字段命名与 ReadRequest 对齐。
type ReadGroupRequest struct {
	// Model 模型名
	Model string

	// Domain 过滤域，与 ReadRequest.Domain 同款（[]any 或域字符串）
	Domain any

	// Fields 要聚合求和的数值字段（measures）
	Fields []string

	// GroupBy 分组字段，至少一个
	GroupBy []string

	// Offset/Limit 分的是**组**不是记录；0 表示不限制
	Offset int64
	Limit  int64
}

// ReadGroup 是 ReadGroupRequest 的模型级入口，与 Read/Create 同款：克隆模型拿到
// 自定义结构与事务，再委托给会话级 ReadGroup。
//
// 有意**不**加进 IModel 接口：远程模型对象（TRemoteModelObject）没有本地表可分组，
// 强行进接口只会逼出一个必然报错的实现。调用方按能力断言，拿不到就明确报错。
func (self *TModel) ReadGroup(req *ReadGroupRequest) (*dataset.TDataSet, error) {
	model, err := self.Clone()
	if err != nil {
		return nil, err
	}

	session := model.Tx()
	if session.IsAutoClose {
		defer session.Close()
	}

	if req.Domain != nil {
		session.Domain(req.Domain)
	}
	if req.Limit > 0 || req.Offset > 0 {
		session.Limit(req.Limit, req.Offset)
	}

	return session.ReadGroup(req.GroupBy, req.Fields)
}

// isAggregatableField 判断一个字段能不能被 SUM。
//
// 光看 SQL 类型是不够的：many2one 的列是 bigint，id 列也是 bigint，按数值类型
// 判定会把它们一起求和——一堆外键 id 相加得出的数没有任何意义，却会以 measure
// 的身份出现在报表/看板上，看起来像个正经数字。对齐 Odoo：关系字段没有
// group_operator，不参与聚合。
func isAggregatableField(model IModel, field IField) bool {
	if SqlTypes[strings.ToUpper(field.SQLType().Name)] != NUMERIC_TYPE {
		return false
	}
	switch field.TypeName() {
	case TYPE_O2O, TYPE_O2M, TYPE_M2O, TYPE_M2M:
		return false
	}
	return field.Name() != model.IdField()
}

// ReadGroup 按 groupBy 字段分组统计：每组返回分组字段的值、记录数（GroupCountField）
// 以及各 measure 字段的 SUM。
//
// 为什么必须在服务端做：graph / pivot / kanban 三种视图都要分组聚合，客户端做不到
// ——要么把整表拉下来，要么算不出跨页总计。
//
// 与 Read() 的关键区别是结果行**不是记录**：没有 id，不做计算字段/关系字段的后处理。
// 所以它不走 _read()，而是像 Sum() 一样自己拼 SQL。但 FROM/WHERE 一律经
// where_calc 生成，于是租户 schema 路由、tenant_id 过滤、公司可见性（都由
// BeforeSession→withSession 挂在 domain/Where 上）原样生效，**不会绕过隔离**。
//
// 目前的边界：聚合算子固定为数值字段 SUM 与 COUNT(*)。Odoo 的 group_operator
// （avg/min/max）与日期分组粒度（`date:month`）尚未实现——前端当前也不发这两种请求。
func (self *TSession) ReadGroup(groupBy []string, measures []string) (*dataset.TDataSet, error) {
	model := self.Statement.Model
	if model == nil || len(model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	// 只读聚合，与 Sum 同 Op：withSession 据此只施加过滤、不写 tenant_id/write_id。
	self.Op = OpSum
	if _, err := model.BeforeSession(self); err != nil {
		return nil, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}
	if self.IsDeprecated {
		return nil, ErrInvalidSession
	}

	if len(groupBy) == 0 {
		return nil, fmt.Errorf("ReadGroup: at least one groupby field is required on model %s", model.String())
	}

	quoter := self.orm.dialect.Quoter()

	// 列一律带主表别名限定：domain 命中关系字段时 where_calc 会引入 JOIN，届时裸列名
	// 可能有歧义；且 PG 的 GROUP BY 会优先解析 SELECT 的输出别名，
	// `SUM("x") AS "x"` 与 `GROUP BY x` 同时出现时会解析到聚合结果上（报错或错分组）。
	tableAlias, err := quoter.QuoteIdent(model.Table())
	if err != nil {
		return nil, fmt.Errorf("ReadGroup: invalid table name %q: %w", model.Table(), err)
	}
	qualify := func(name string) (string, error) {
		col, err := quoter.QuoteIdent(name)
		if err != nil {
			return "", err
		}
		return tableAlias + "." + col, nil
	}

	selectCols := make([]string, 0, len(groupBy)+len(measures)+1)
	groupCols := make([]string, 0, len(groupBy))
	grouped := make(map[string]bool, len(groupBy))

	for _, name := range groupBy {
		field := model.GetFieldByName(name)
		if field == nil {
			return nil, fmt.Errorf("ReadGroup: groupby field %q not found on model %s", name, model.String())
		}
		// 非存储字段（getter 计算出来的）在表里没有列，SQL 分不了组。明确报错而不是
		// 静默忽略：忽略会把「按 A 分组」悄悄变成「按全表一组」，读的人看不出差别。
		if !field.Store() {
			return nil, fmt.Errorf("ReadGroup: cannot group by non-stored field %q on model %s", name, model.String())
		}
		switch field.TypeName() {
		case TYPE_O2M, TYPE_M2M:
			return nil, fmt.Errorf("ReadGroup: cannot group by %s field %q on model %s", field.TypeName(), name, model.String())
		}

		qualified, err := qualify(name)
		if err != nil {
			return nil, fmt.Errorf("ReadGroup: invalid groupby field %q: %w", name, err)
		}
		selectCols = append(selectCols, qualified)
		groupCols = append(groupCols, qualified)
		grouped[name] = true
	}

	countAlias, err := quoter.QuoteIdent(GroupCountField)
	if err != nil {
		return nil, err
	}
	selectCols = append(selectCols, "COUNT(*) AS "+countAlias)

	for _, name := range measures {
		if name == GroupCountField || grouped[name] {
			// __count 已单独产出；分组字段本身不参与聚合（对齐 Odoo），
			// 否则 SELECT 会出现两列同名，取值方拿到哪一列全看解码顺序。
			continue
		}
		field := model.GetFieldByName(name)
		if field == nil {
			// 字段名写错是调用方的 bug，明确报出来；下面「聚合不了」的几类则不是。
			return nil, fmt.Errorf("ReadGroup: measure field %q not found on model %s", name, model.String())
		}
		// 聚合不了的字段**跳过而不是报错**，对齐 Odoo「没有 group_operator 的字段
		// 不参与聚合」：kanban 视图按设计会把整张卡片的字段列表（字符/日期/关系
		// 字段混在一起）原样当 measures 传过来，在这里报错会让每个分组看板直接打不开。
		if !field.Store() || !isAggregatableField(model, field) {
			continue
		}

		qualified, err := qualify(name)
		if err != nil {
			return nil, fmt.Errorf("ReadGroup: invalid measure field %q: %w", name, err)
		}
		alias, err := quoter.QuoteIdent(name)
		if err != nil {
			return nil, err
		}
		// COALESCE：全 NULL 的组在 SQL 里 SUM 出 NULL，前端 Number(null) 得 0 尚可，
		// 但 JSON 里的 null 会让「无数据」和「合计为 0」变得不可区分。
		selectCols = append(selectCols, fmt.Sprintf("COALESCE(SUM(%s),0) AS %s", qualified, alias))
	}

	query, err := self.Statement.where_calc(self.Statement.domain, false, make(map[string]any))
	if err != nil {
		return nil, err
	}
	fromClause, whereClause, whereParams := query.getSql()

	// 软删除过滤：与 _readFromDatabase 同款。少了它，被软删的行仍会计进分组统计，
	// 列表视图看不到的记录却出现在报表合计里。
	if deletedField := model.Obj().DeletedField; deletedField != "" {
		quoted := quoter.QuoteIdentMust(deletedField)
		var sdFilter string
		switch self.softDeleteMode {
		case softDeleteFilterActive:
			sdFilter = quoted + " IS NULL"
		case softDeleteOnlyDeleted:
			sdFilter = quoted + " IS NOT NULL"
		}
		if sdFilter != "" {
			if whereClause == "" {
				whereClause = sdFilter
			} else {
				whereClause = whereClause + " AND " + sdFilter
			}
		}
	}

	if whereClause != "" {
		whereClause = "WHERE " + whereClause
	}

	// 分组结果按分组键排序，保证同一份数据每次返回的组顺序一致（图表 X 轴、
	// pivot 行序都直接取这个顺序）。模型的默认 _order 是记录级的，对分组无意义。
	orderClause := "ORDER BY " + strings.Join(groupCols, ",")

	// limit/offset 只在调用方显式设置时施加：分组数通常远小于记录数，
	// 套用 Read 的 DefaultLimit 会把报表悄悄截断。
	var limitClause, offsetClause string
	if self.Statement.LimitClause > 0 {
		limitClause = "LIMIT " + fmt.Sprint(self.Statement.LimitClause)
	}
	if self.Statement.OffsetClause > 0 {
		offsetClause = "OFFSET " + fmt.Sprint(self.Statement.OffsetClause)
	}

	sqlStr := JoinClause(
		"SELECT",
		strings.Join(selectCols, ","),
		"FROM",
		fromClause,
		whereClause,
		"GROUP BY "+strings.Join(groupCols, ","),
		orderClause,
		limitClause,
		offsetClause,
	)

	return self._query(sqlStr, whereParams...)
}
