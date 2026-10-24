package orm

import (
	"fmt"
	"regexp"
	"strings"
	"time"

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

// numberGranularities 是 Odoo 17+ 的「取一部分」粒度：值是一个整数（几号、第几周、几月……），
// 不是截断后的时间点。键是请求里的写法，值是 Postgres `EXTRACT(<part> FROM …)` 的 part。
// 同样是白名单 —— part 会作为字面量拼进 SQL。
var numberGranularities = map[string]string{
	"day_of_month":    "day",
	"day_of_week":     "dow",
	"day_of_year":     "doy",
	"iso_week_number": "week",
	"month_number":    "month",
	"quarter_number":  "quarter",
	"year_number":     "year",
	"hour_number":     "hour",
	"minute_number":   "minute",
	"second_number":   "second",
}

// IsNumberGranularity 报告粒度是不是「取一部分」那一类（结果是整数，见 numberGranularities）。
func IsNumberGranularity(g string) bool { _, ok := numberGranularities[g]; return ok }

// SplitGroupBy 拆分 `字段` 或 `字段:粒度` 两种分组写法。
// 粒度非法时原样把整串当字段名返回，由调用方按「字段不存在」报错——比静默降级成
// 按原始时间戳分组好：后者会画出一堆看不出错的、每个时刻自成一组的图。
func SplitGroupBy(spec string) (field, granularity string) {
	i := strings.IndexByte(spec, ':')
	if i < 0 {
		return spec, ""
	}
	f, g := spec[:i], spec[i+1:]
	if !groupGranularities[g] && !IsNumberGranularity(g) {
		return spec, ""
	}
	return f, g
}

// SplitMeasure 拆 Fields（measures）里的一项：`字段` 或 `字段:后缀`。后缀转小写原样返回，
// 由调用方判它是算子（aggregateOps，Odoo ≤16 read_group 的 `sale_price:sum`）还是分组粒度
// （`date:month` —— 分组写法跟着混进了 fields，不是度量）。结果列名取字段名，与 Odoo 一致。
func SplitMeasure(spec string) (name, op string) {
	spec = strings.TrimSpace(spec)
	i := strings.IndexByte(spec, ':')
	if i < 0 {
		return spec, ""
	}
	return spec[:i], strings.ToLower(spec[i+1:])
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

	// Aggregates 是 Odoo 19 formatted_read_group 的度量写法：`字段:算子`（sum / avg / min / max /
	// count / count_distinct / bool_and / bool_or / array_agg / array_agg_distinct / sum_currency）
	// 或 `__count`。给了它就不再看 Fields：算子由**调用方**指定（透视表里用户选的），不取字段的
	// group_operator。结果列名见 AggregateAlias（`amount_total:sum` → `amount_total__sum`）。
	Aggregates []string

	// Tz 是 datetime 字段按粒度分组时截断所在的时区（Odoo 按用户时区：上海的「10 月」从
	// 10-01 00:00 +08 开始，不是 UTC 的零点）。空 = 按 UTC 截断（旧行为）。必须是合法的 IANA 名，
	// 会作为字面量进 SQL，非法的直接报错。date 字段不受影响。
	Tz string
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

	if len(req.Aggregates) > 0 || req.Tz != "" {
		return session.readGroup(req.GroupBy, req.Fields, &readGroupOptions{aggregates: req.Aggregates, tz: req.Tz})
	}
	return session.ReadGroup(req.GroupBy, req.Fields)
}

// readGroupOptions 是 ReadGroupRequest 里 Odoo 19 那两项（Aggregates / Tz）。nil = 旧行为。
type readGroupOptions struct {
	aggregates []string
	tz         string
}

// aggregateOps 是 Aggregates 允许的算子（白名单：会作为字面量进 SQL）。
var aggregateOps = map[string]string{
	"sum": "SUM", "avg": "AVG", "min": "MIN", "max": "MAX", "count": "COUNT",
	"count_distinct": "COUNT(DISTINCT", "bool_and": "BOOL_AND", "bool_or": "BOOL_OR",
	"array_agg": "ARRAY_AGG", "array_agg_distinct": "ARRAY_AGG(DISTINCT",
	// Odoo 的 sum_currency 是「换算成公司币种再求和」。本仓没有按行汇率换算的列，
	// 退化成同币种求和（前端只在字段带 currency_field 时才会要它，本仓的字段元数据不带）。
	"sum_currency": "SUM",
}

var reTz = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+\-/]{0,63}$`)

// validTz 时区名是否能安全地拼进 SQL：形状对，且 Go 认得（与 Postgres 同一份 IANA 库）。
func validTz(tz string) bool {
	if !reTz.MatchString(tz) {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
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
	return self.readGroup(groupBy, measures, nil)
}

func (self *TSession) readGroup(groupBy []string, measures []string, opts *readGroupOptions) (*dataset.TDataSet, error) {
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
	// m2m 分组键要 LEFT JOIN 关系表（见下面 TYPE_M2M 那一支）。
	var m2mJoins []string

	for _, spec := range groupBy {
		name, granularity := SplitGroupBy(spec)

		field := model.GetFieldByName(name)
		if field == nil {
			return nil, fmt.Errorf("ReadGroup: groupby field %q not found on model %s", name, model.String())
		}
		// many2many（有关系表的）：一条记录按它的**每一个**对端各进一组，一个都没有的
		// 进「空」组（对齐 Odoo 的 read_group：发票行按「税格」分组，挂两个格子的行两组
		// 里都有）。必须在下面的 Store() 判断之前 —— m2m 在本表里没有列，Store() 为假。
		if field.TypeName() == TYPE_M2M && m2mHasRelationTable(field) {
			if granularity != "" {
				return nil, fmt.Errorf("ReadGroup: granularity %q is not valid on many2many field %q", granularity, name)
			}
			join, expr, err := self.m2mGroupJoin(model, field, tableAlias)
			if err != nil {
				return nil, err
			}
			alias, err := quoter.QuoteIdent(name)
			if err != nil {
				return nil, err
			}
			m2mJoins = append(m2mJoins, join)
			selectCols = append(selectCols, expr+" AS "+alias)
			groupCols = append(groupCols, expr)
			grouped[name] = true
			continue
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
		cdGroup := IsCompanyDependent(field)
		if cdGroup {
			// 按当前公司那一格分组，不是按整列 jsonb（见 field_company.go）。
			if qualified, err = self.companyExpr(qualified, field); err != nil {
				return nil, fmt.Errorf("ReadGroup: groupby field %q: %w", name, err)
			}
		}

		expr := qualified
		if granularity != "" {
			if SqlTypes[strings.ToUpper(field.SQLType().Name)] != TIME_TYPE {
				return nil, fmt.Errorf("ReadGroup: granularity %q requires a date/time field, but %q on model %s is %s",
					granularity, name, model.String(), field.SQLType().Name)
			}
			// date_trunc 是 Postgres 的函数，sqlite/mysql 都没有。不拦的话这里会拼出
			// 一条必然报「函数不存在」的 SQL——错在方言不支持，报出来的却像是语法问题。
			if !self.orm.dialect.Features().DateTrunc {
				return nil, fmt.Errorf("ReadGroup: groupby granularity (%q) needs Features.DateTrunc (postgres), current dialect is %s",
					granularity, self.orm.dialect.DBType())
			}
			isDate := strings.EqualFold(field.SQLType().Name, "DATE")
			tz := ""
			if opts != nil && opts.tz != "" && !isDate {
				if !validTz(opts.tz) {
					return nil, fmt.Errorf("ReadGroup: invalid time zone %q", opts.tz)
				}
				tz = opts.tz
			}
			// 别名保持为字段原名（不带 `:粒度`）：_scanRows 按列名回查字段拿转换器，
			// 带冒号的别名既查不到字段、也不是合法标识符。
			if _, ok := numberGranularities[granularity]; ok {
				// 「取一部分」：结果是整数，别名另起（`<字段>__<粒度>`），不套字段的时间转换器。
				expr = granularityExpr(qualified, granularity, isDate, tz)
				alias, err := quoter.QuoteIdent(GranularityAlias(name, granularity))
				if err != nil {
					return nil, err
				}
				selectCols = append(selectCols, expr+" AS "+alias)
				groupCols = append(groupCols, expr)
				grouped[name] = true
				continue
			}
			expr = granularityExpr(qualified, granularity, isDate, tz)
			alias, err := quoter.QuoteIdent(name)
			if err != nil {
				return nil, err
			}
			selectCols = append(selectCols, expr+" AS "+alias)
		} else if cdGroup {
			// 表达式没有列名，要显式起别名：_scanRows 按列名回查字段拿转换器。
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

	if opts != nil && len(opts.aggregates) > 0 {
		cols, err := self.aggregateColumns(model, opts.aggregates, qualify)
		if err != nil {
			return nil, err
		}
		selectCols = append(selectCols, cols...)
		measures = nil
	}
	measured := map[string]bool{}
	for _, spec := range measures {
		// `字段:后缀`：后缀是算子就按它聚合（列名仍是字段名）；是分组粒度就说明这是一条
		// 分组写法混进了 fields，不是度量，静默跳过。见 SplitMeasure。
		name, specOp := SplitMeasure(spec)
		if specOp != "" {
			if _, isAgg := aggregateOps[specOp]; !isAgg {
				if !groupGranularities[specOp] && !IsNumberGranularity(specOp) {
					log.Warnf("ReadGroup: measure %q on model %s has an unknown aggregate operator, ignored", spec, model.String())
				}
				continue
			}
		}
		if name == GroupCountField || grouped[name] || measured[name] {
			// __count 已单独产出；分组字段本身不参与聚合（对齐 Odoo），
			// 否则 SELECT 会出现两列同名，取值方拿到哪一列全看解码顺序。
			// 同一字段写了两次（`qty` 与 `qty:max`）只认第一次，理由同上。
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
		if specOp != "" {
			// 调用方点了算子：与 Aggregates 那条路同一套类型规则与 SQL（aggregateColumn），
			// 只是列名取字段名。
			col, ok, err := self.aggregateColumn(model, field, name, specOp, name, qualify)
			if err != nil {
				return nil, err
			}
			if ok {
				selectCols = append(selectCols, col)
				measured[name] = true
			}
			continue
		}
		if !field.Store() || !isAggregatableField(model, field) {
			continue
		}
		measured[name] = true

		qualified, err := qualify(name)
		if err != nil {
			return nil, fmt.Errorf("ReadGroup: invalid measure field %q: %w", name, err)
		}
		if IsCompanyDependent(field) {
			if qualified, err = self.companyExpr(qualified, field); err != nil {
				return nil, fmt.Errorf("ReadGroup: measure field %q: %w", name, err)
			}
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

	// 有 m2m 分组键时，先把过滤好的本表行包成子查询、再 LEFT JOIN 关系表。
	// 不能直接在 fromClause 后面接 JOIN：domain 命中关系字段时 where_calc 给的是逗号
	// 分隔的多表 FROM，`a, b LEFT JOIN rel ON rel.x = a.id` 里 JOIN 只绑 b，PG 报
	// "invalid reference to FROM-clause entry"。子查询保留本表名作别名，上面按
	// tableAlias 限定的列不用改。
	if len(m2mJoins) > 0 {
		fromClause = "(SELECT " + tableAlias + ".* FROM " + fromClause + " " + whereClause + ") AS " +
			tableAlias + " " + strings.Join(m2mJoins, " ")
		whereClause = ""
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

// m2mGroupJoin 按 m2m 字段分组要接的 LEFT JOIN 与分组表达式（关系表里指向对端的那一列）。
func (self *TSession) m2mGroupJoin(model IModel, field IField, tableAlias string) (join, expr string, err error) {
	quoter := self.orm.dialect.Quoter()
	relTable := fmtTableName(field.JoinModelName())
	own, other := field.JoinSourceKey(), field.RelatedKeyName()
	if relTable == "" || own == "" || other == "" {
		return "", "", fmt.Errorf("ReadGroup: incomplete relation metadata for many2many %s@%s (table=%q owner=%q key=%q)",
			field.Name(), field.ModelName(), relTable, own, other)
	}
	relQ, err := quoter.QuoteIdent(relTable)
	if err != nil {
		return "", "", err
	}
	// 与 TExpression.qualify 同一规则：会话/ORM 带 schema 就补上（搜索路径之外再保险一层）。
	if self.Schema != "" {
		relQ = self.Schema + "." + relQ
	} else if self.orm != nil && self.orm.Schema != "" {
		relQ = self.orm.Schema + "." + relQ
	}
	aliasQ, err := quoter.QuoteIdent("rg_" + field.Name())
	if err != nil {
		return "", "", err
	}
	ownQ, err := quoter.QuoteIdent(own)
	if err != nil {
		return "", "", err
	}
	otherQ, err := quoter.QuoteIdent(other)
	if err != nil {
		return "", "", err
	}
	idQ, err := quoter.QuoteIdent(model.IdField())
	if err != nil {
		return "", "", err
	}
	join = fmt.Sprintf("LEFT JOIN %s AS %s ON %s.%s = %s.%s", relQ, aliasQ, aliasQ, ownQ, tableAlias, idQ)
	return join, aliasQ + "." + otherQ, nil
}

// AggregateAlias 是一项度量在结果集里的列名：`字段:算子` → `字段__算子`。
//
// 不直接用写法本身：冒号不是合法标识符（QuoteIdent 拒绝），而 _scanRows 按列名回查字段。
// 调用方（vectors 的 formatted_read_group）按它取值，再把键换回 Odoo 的写法。
func AggregateAlias(spec string) string { return strings.Replace(spec, ":", "__", 1) }

// GranularityAlias 是「取一部分」粒度（numberGranularities）分组列的列名：`字段__粒度`。
func GranularityAlias(field, granularity string) string { return field + "__" + granularity }

// aggregateColumns 把 Odoo 19 的度量写法（`字段:算子`）拼成 SELECT 列，列名见 AggregateAlias。
//
// 规则对齐 Odoo 的 _read_group_select：
//   - `__count` 已由 COUNT(*) 单独产出，这里跳过；
//   - 算子不在白名单 → 报错（会进 SQL）；
//   - 字段不存在或非存储 → 跳过并记日志（与 Fields 那条路同一规矩：透视表会把整份定义里的度量
//     原样发来，一个没实现的字段不该让整张表打不开）；
//   - sum / avg 只给数值列；count / count_distinct / array_agg 任何存储列（含 many2one）；
//     min / max 数值与时间；bool_and / bool_or 布尔。不合的跳过并记日志。
func (self *TSession) aggregateColumns(model IModel, specs []string, qualify func(string) (string, error)) ([]string, error) {
	out := make([]string, 0, len(specs))
	seen := map[string]bool{}
	for _, spec := range specs {
		if spec == GroupCountField || seen[spec] {
			continue
		}
		seen[spec] = true
		i := strings.LastIndexByte(spec, ':')
		if i <= 0 {
			return nil, fmt.Errorf("ReadGroup: aggregate %q must be written as field:operator", spec)
		}
		name, op := spec[:i], spec[i+1:]
		if _, ok := aggregateOps[op]; !ok {
			return nil, fmt.Errorf("ReadGroup: invalid aggregate operator %q in %q", op, spec)
		}
		field := model.GetFieldByName(name)
		if field == nil {
			log.Warnf("ReadGroup: aggregate %q on model %s skipped (no such stored field)", spec, model.String())
			continue
		}
		col, ok, err := self.aggregateColumn(model, field, name, op, AggregateAlias(spec), qualify)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, col)
		}
	}
	return out, nil
}

// aggregateColumn 拼一项「字段 + 算子」度量的 SELECT 列（`<expr> AS <alias>`）。字段聚合不了
// （非存储、类型不合）时记日志并回 ok=false。op 必须已在 aggregateOps 白名单里。
// Aggregates（列名 `字段__算子`）与 Fields 里的 `字段:算子`（列名就是字段名）共用这一份。
func (self *TSession) aggregateColumn(model IModel, field IField, name, op, aliasName string, qualify func(string) (string, error)) (string, bool, error) {
	spec := name + ":" + op
	fn, ok := aggregateOps[op]
	if !ok {
		return "", false, fmt.Errorf("ReadGroup: invalid aggregate operator %q in %q", op, spec)
	}
	if !field.Store() {
		log.Warnf("ReadGroup: aggregate %q on model %s skipped (no such stored field)", spec, model.String())
		return "", false, nil
	}
	sqlKind := SqlTypes[strings.ToUpper(field.SQLType().Name)]
	switch op {
	case "sum", "avg", "sum_currency":
		if !isAggregatableField(model, field) {
			log.Warnf("ReadGroup: aggregate %q on model %s skipped (not numeric)", spec, model.String())
			return "", false, nil
		}
	case "min", "max":
		if sqlKind != NUMERIC_TYPE && sqlKind != TIME_TYPE {
			log.Warnf("ReadGroup: aggregate %q on model %s skipped (not numeric/time)", spec, model.String())
			return "", false, nil
		}
	case "bool_and", "bool_or":
		if t := strings.ToUpper(field.SQLType().Name); t != "BOOL" && t != "BOOLEAN" {
			log.Warnf("ReadGroup: aggregate %q on model %s skipped (not boolean)", spec, model.String())
			return "", false, nil
		}
	}
	qualified, err := qualify(name)
	if err != nil {
		return "", false, fmt.Errorf("ReadGroup: invalid aggregate field %q: %w", name, err)
	}
	if IsCompanyDependent(field) {
		if qualified, err = self.companyExpr(qualified, field); err != nil {
			return "", false, fmt.Errorf("ReadGroup: aggregate field %q: %w", name, err)
		}
	}
	alias, err := self.orm.dialect.Quoter().QuoteIdent(aliasName)
	if err != nil {
		return "", false, err
	}
	var expr string
	switch {
	case strings.HasSuffix(fn, "(DISTINCT"):
		expr = fmt.Sprintf("%s %s)", fn, qualified)
	case fn == "SUM" || fn == "AVG":
		// COALESCE 同 Fields 那条路：全 NULL 的组回 0，而不是 null（「无数据」与「合计为 0」在
		// 前端分不出来时，0 更不误导）。MIN / MAX 不套，理由同那里。
		expr = fmt.Sprintf("COALESCE(%s(%s),0)", fn, qualified)
	default:
		expr = fmt.Sprintf("%s(%s)", fn, qualified)
	}
	return expr + " AS " + alias, true, nil
}

// granularityExpr 是按粒度分组的那一列的 SQL（Postgres）。
//
//   - datetime（tz 非空）：列里是 UTC 的无时区时间戳，先转成那个时区的本地时间再截断，截断后的本地
//     起点再换回 UTC —— 下钻域与列里的值同一口径（Odoo 按用户时区：上海的「10 月」从 9 月 30 日
//     16:00 UTC 开始）。
//   - date：没有时区，但 **date_trunc 没有 date 版本**：直接传 date 进去，Postgres 选的是
//     timestamptz 那个重载，按会话时区把当天零点当成本地时刻 —— 会话时区在 UTC 以东时，3 月 1 日
//     读回来是 2 月 28 日 16:00Z，透视表 / 图表的每个月整体错一档（2026-10-10 真栈：3 月 15 日的
//     分录落进「2026 年 2 月」）。先转成无时区的 timestamp 再截断，结果再转回 date。
//   - 「取一部分」（month_number、day_of_week…）：EXTRACT 出整数。
func granularityExpr(qualified, granularity string, isDate bool, tz string) string {
	local := qualified
	if isDate {
		local = fmt.Sprintf("CAST(%s AS timestamp)", qualified)
	} else if tz != "" {
		local = fmt.Sprintf("((%s AT TIME ZONE 'UTC') AT TIME ZONE '%s')", qualified, tz)
	}
	if part, ok := numberGranularities[granularity]; ok {
		return fmt.Sprintf("CAST(EXTRACT(%s FROM %s) AS INTEGER)", part, local)
	}
	expr := fmt.Sprintf("date_trunc('%s',%s)", granularity, local)
	switch {
	case isDate:
		expr = fmt.Sprintf("CAST(%s AS date)", expr)
	case tz != "":
		expr = fmt.Sprintf("((%s AT TIME ZONE '%s') AT TIME ZONE 'UTC')", expr, tz)
	}
	return expr
}
