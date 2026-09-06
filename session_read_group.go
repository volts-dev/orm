package orm

import (
	"fmt"
	"strings"

	"github.com/volts-dev/dataset"
)

// GroupCountField 是 read_group 结果里「本组记录数」那一列的列名，对齐 Odoo 的 __count。
const GroupCountField = "__count"

// groupGranularities 是 `字段:粒度` 分组语法允许的粒度取值（对齐 Odoo 的
// read_group `date:month` 写法）。
//
// **必须是白名单**：粒度最终会作为字面量拼进 `date_trunc('<粒度>', col)`，而
// groupby 整个来自客户端请求。放开任意字符串等于开一个 SQL 注入口子。
var groupGranularities = map[string]bool{
	"hour":    true,
	"day":     true,
	"week":    true,
	"month":   true,
	"quarter": true,
	"year":    true,
}

// SplitGroupBy 拆分 `字段` 或 `字段:粒度` 两种分组写法。
// 粒度非法时原样把整串当字段名返回，由调用方按「字段不存在」报错——比静默降级成
// 按原始时间戳分组好：后者会画出一堆看不出错的、每个时刻自成一组的图。
func SplitGroupBy(spec string) (field, granularity string) {
	i := strings.IndexByte(spec, ':')
	if i < 0 {
		return spec, ""
	}
	f, g := spec[:i], spec[i+1:]
	if !groupGranularities[g] {
		return spec, ""
	}
	return f, g
}

// ReadGroupRequest 是 ReadGroup 的模型级入参，字段命名与 ReadRequest 对齐。
type ReadGroupRequest struct {
	// Model 模型名
	Model string

	// Domain 过滤域，与 ReadRequest.Domain 同款（[]any 或域字符串）
	Domain any

	// Fields 参与聚合的字段（measures）。算子取各字段的 group_operator tag，
	// 未指定则 SUM；聚合不了的字段会被跳过而非报错，见 ReadGroup。
	Fields []string

	// GroupBy 分组字段，可写 `字段` 或 `字段:粒度`（见 SplitGroupBy）。
	// 允许为空——对齐 Odoo，此时返回整个 domain 的一行合计。
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
// 聚合算子取自各字段的 group_operator tag（未指定则 SUM），另加一列 COUNT(*)；
// 日期分组粒度（`date:month`）经 SplitGroupBy 支持。
//
// 目前的边界：粒度分组用 date_trunc 实现，**仅 Postgres 可用**，其余方言会明确报错
// 而不是拼出一条跑不通的 SQL。
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
	if err := self.Statement.Err(); err != nil {
		return nil, err
	}

	// groupBy 为空是合法的：对齐 Odoo，返回**整个 domain 的一行合计**（无 GROUP BY）。
	// graph 视图在没有分组维度时就是这么发的（前端把这一行标成 "Total"），报错会让
	// 图表直接打不开——而它想要的只是一个总计。
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

	for _, spec := range groupBy {
		name, granularity := SplitGroupBy(spec)

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

		expr := qualified
		if granularity != "" {
			if SqlTypes[strings.ToUpper(field.SQLType().Name)] != TIME_TYPE {
				return nil, fmt.Errorf("ReadGroup: granularity %q requires a date/time field, but %q on model %s is %s",
					granularity, name, model.String(), field.SQLType().Name)
			}
			// date_trunc 是 Postgres 的函数，sqlite/mysql 都没有。不拦的话这里会拼出
			// 一条必然报「函数不存在」的 SQL——错在方言不支持，报出来的却像是语法问题。
			if dbType := self.orm.dialect.DBType(); dbType != POSTGRES {
				return nil, fmt.Errorf("ReadGroup: groupby granularity (%q) is only supported on postgres, current dialect is %s",
					granularity, dbType)
			}
			// 别名保持为字段原名（不带 `:粒度`）：_scanRows 按列名回查字段拿转换器，
			// 带冒号的别名既查不到字段、也不是合法标识符。
			expr = fmt.Sprintf("date_trunc('%s',%s)", granularity, qualified)
			alias, err := quoter.QuoteIdent(name)
			if err != nil {
				return nil, err
			}
			selectCols = append(selectCols, expr+" AS "+alias)
		} else {
			selectCols = append(selectCols, expr)
		}

		// GROUP BY / ORDER BY 都用**表达式本身**而不是输出别名：PG 里
		// `date_trunc(...) AS "date"` 与 `GROUP BY date` 并存时，别名会遮蔽原列，
		// 语义随写法漂移。重复表达式虽啰嗦但没有歧义。
		groupCols = append(groupCols, expr)
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
			// ★ 模型上**根本没有**这个字段时也跳过，与下面「聚合不了」的几类同一条规则。
			//
			// 原来这里是直接报错（"字段名写错是调用方的 bug"）。但 kanban 按设计就是
			// 把**整张卡片的字段列表**原样当 measures 传过来，而 arch 是照 Odoo 抄的
			// ——只要里面有一个本仓没实现的字段（pro.tmpl 卡片上的 activity_state 就是，
			// Odoo 那边它来自 mail.activity.mixin），整页分组当场只剩一行错误文本：
			//
			//     ReadGroup: measure field "activity_state" not found on model pro.tmpl
			//
			// 而那行字看不出跟哪个字段、哪张 arch 有关。全仓 kanban 里 kanban_activity
			// 一族有 4 处是这个形状，也就是说这些看板的分组功能全是死的。
			//
			// 代价是调用方把字段名写错时不再报错。所以**必须打一条点名的日志**：
			// 静默跳过等于把一个"合计列凭空消失"的问题变成没有任何线索的问题。
			// （同 session_crwd.go 里 GroupBy 那处 "not found on model %s, ignored"。）
			log.Warnf("ReadGroup: measure field %q not found on model %s, ignored "+
				"(kanban passes the whole card field list as measures; a typo looks the same)",
				name, model.String())
			continue
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
		// 聚合算子取自字段的 group_operator tag（对齐 Odoo），未指定则 SUM。
		// tag 解析期已按白名单校验过，这里不会拼进未知函数名。
		op := field.GroupOperator()
		if op == "" {
			op = "SUM"
		}
		// COALESCE：全 NULL 的组在 SQL 里聚合出 NULL，前端 Number(null) 得 0 尚可，
		// 但 JSON 里的 null 会让「无数据」和「合计为 0」变得不可区分。
		// MIN/MAX 例外——它们的 NULL 表示「该组没有可比较的值」，用 0 顶替会凭空
		// 造出一个最小值，比空更误导。
		switch op {
		case "MIN", "MAX":
			selectCols = append(selectCols, fmt.Sprintf("%s(%s) AS %s", op, qualified, alias))
		default:
			selectCols = append(selectCols, fmt.Sprintf("COALESCE(%s(%s),0) AS %s", op, qualified, alias))
		}
	}

	query, err := self.Statement.where_calc(self.Statement.domain, false, make(map[string]any))
	if err != nil {
		return nil, err
	}
	fromClause, whereClause, whereParams := query.getSql()

	// 软删除过滤：与 _readFromDatabase 共用 softDeleteClause。少了它，被软删的行仍会
	// 计进分组统计，列表视图看不到的记录却出现在报表合计里。
	whereClause = andClause(whereClause, self.softDeleteClause())

	if whereClause != "" {
		whereClause = "WHERE " + whereClause
	}

	// 分组结果按分组键排序，保证同一份数据每次返回的组顺序一致（图表 X 轴、
	// pivot 行序都直接取这个顺序）。模型的默认 _order 是记录级的，对分组无意义。
	// 无分组键时既不能 GROUP BY 也不能 ORDER BY——整表只出一行合计。
	var groupClause, orderClause string
	if len(groupCols) > 0 {
		groupClause = "GROUP BY " + strings.Join(groupCols, ",")
		orderClause = "ORDER BY " + strings.Join(groupCols, ",")
	}

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
		groupClause,
		orderClause,
		limitClause,
		offsetClause,
	)

	return self._query(sqlStr, whereParams...)
}
