package orm

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/core"
)

// search and return the id list only
func (self *TSession) Search() ([]any, int64, error) {
	if self.IsDeprecated {
		return nil, 0, ErrInvalidSession
	}

	// 确保资源清理
	defer func() {
		self._resetStatement()
		if self.IsAutoClose {
			self.Close()
		}
	}()

	// Search 此前**完全没有上限**（_search 里是 `if LimitClause > 0`），
	// 枚举整张表的 id 一直是放行的——DefaultLimit 那道"防扫全表"从来没覆盖到这条路。
	// 这里补上与 Read 同一道守卫。
	if err := self.guardUnscopedRead(); err != nil {
		return nil, 0, err
	}

	return self._search("", nil)
}

// Query a raw sql and return records as dataset
func (self *TSession) Query(sql string, paramStr ...any) (*dataset.TDataSet, error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	return self._query(sql, paramStr...)
}

// Exec raw sql
func (self *TSession) Exec(sql_str string, args ...any) (sql.Result, error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	return self._exec(sql_str, args...)
}

func (self *TSession) Count() (int, error) {
	model := self.Statement.Model
	self.Op = OpCount
	if _, err := model.BeforeSession(self); err != nil {
		return 0, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	//defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	self.Statement.IsCount = true

	_, count, err := self._search("", nil)
	if err != nil {
		return 0, err
	}

	return int(count), nil
}

// TODO sum
// Sum sum the records by some column. bean's non-empty fields are conditions.
func (self *TSession) Sum(fieldName string) (float64, error) {
	model := self.Statement.Model
	self.Op = OpSum
	if _, err := model.BeforeSession(self); err != nil {
		return 0, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	// SUM 是聚合，没有可锁的行。这里主动校验，让 .ForUpdate().Sum() 明确报错，
	// 而不是把锁悄悄丢掉——后者正是 ForUpdate() 从前的行为。
	if _, err := self.lockClause(true); err != nil {
		return 0, err
	}

	// 校验字段并引用，防止注入
	if self.Statement.Model.GetFieldByName(fieldName) == nil {
		return 0, fmt.Errorf("Sum: field %s not found on model %s", fieldName, self.Statement.Model.String())
	}
	col, err := self.orm.dialect.Quoter().QuoteIdent(fieldName)
	if err != nil {
		return 0, fmt.Errorf("Sum: invalid field %s: %w", fieldName, err)
	}

	// 复用 where_calc 生成 from/where（与 Count 路径一致），构造真实的 SUM 查询
	query, err := self.Statement.where_calc(self.Statement.domain, false, make(map[string]any))
	if err != nil {
		return 0, err
	}
	from_clause, where_clause, where_clause_params := query.getSql()
	if where_clause != "" {
		where_clause = fmt.Sprintf(` WHERE %s`, where_clause)
	}

	query_str := fmt.Sprintf(`SELECT COALESCE(SUM(%s),0) AS sum FROM `, col) + from_clause + where_clause

	ds, err := self._query(query_str, where_clause_params...)
	if err != nil {
		return 0, err
	}

	if ds.Count() > 0 {
		return ds.FieldByName("sum").AsFloat(), nil
	}

	return 0, nil
}

// 查询所有符合条件的主键/索引值
// :param access_rights_uid: optional user ID to use when checking access rights
// (not for ir.rules, this is only for ir.model.access)
func (self *TSession) _search(access_rights_uid string, context map[string]any) (res_ids []any, count int64, err error) {
	var (
		//fields_str string
		//where_str    string
		limit_str           string
		offset_str          string
		from_clause         string
		where_clause        string
		query_str           string
		order_by            string
		where_clause_params []any
		query               *TQuery
	)

	if context == nil {
		context = make(map[string]any)
	}
	//	self.check_access_rights("read")

	//if self.IsClassic {
	// 如果有返回字段
	//if fields != nil {
	//	fields_str = strings.Join(fields, ",")
	//} else {
	//	fields_str = `*`
	//}

	query, err = self.Statement.where_calc(self.Statement.domain, false, context)
	if err != nil {
		return nil, 0, err
	}

	order_by = self.Statement.generate_order_by(query, context) // TODO 未完成
	from_clause, where_clause, where_clause_params = query.getSql()

	if where_clause != "" {
		where_clause = fmt.Sprintf(` WHERE %s`, where_clause)
	}

	table_name := self.Statement.Model.Table()

	// 行锁：Count 是聚合，没有可锁的行，lockClause 会明确报错而不是悄悄不锁。
	lock_clause, err := self.lockClause(self.Statement.IsCount || len(self.Statement.GroupByClause) > 0)
	if err != nil {
		return nil, 0, err
	}
	locking := self.Statement.Lock.IsLocking()

	if self.Statement.IsCount {
		// 添加支持Count函数
		// TODO 优化成自动
		self.Statement.Funcs("count")
		var count int64
		// Ignore order, limit and offset when just counting, they don't make sense and could
		// hurt performance
		query_str = `SELECT count(1) AS count FROM ` + from_clause + where_clause
		res_ds := self.orm.Cacher.GetBySql(table_name, query_str, where_clause_params)
		if res_ds == nil {
			lRes, err := self._query(query_str, where_clause_params...)
			if err != nil {
				return nil, 0, err
			}
			//res_ids = []interface{}{lRes.FieldByName("count").AsInterface()}
			count = lRes.FieldByName("count").AsInteger()
			// #存入缓存
			self.orm.Cacher.PutBySql(table_name, query_str, where_clause_params, lRes)
		} else {
			//res_ids = res_ds.Keys(self.Statement.IdKey)
			count = res_ds.FieldByName("count").AsInteger()
		}

		return nil, count, nil
	}

	if self.Statement.LimitClause > 0 {
		limit_str = fmt.Sprintf(` limit %d`, self.Statement.LimitClause)
	}
	if self.Statement.OffsetClause > 0 {
		offset_str = fmt.Sprintf(` offset %d`, self.Statement.OffsetClause)
	}

	//var lAutoIncrKey = "id"
	//if col := self.Statement.Table.AutoIncrColumn(); col != nil {
	//	lAutoIncrKey = col.Name
	//}
	quoter := self.orm.dialect.Quoter()
	query_str = fmt.Sprintf(`SELECT %s.%s FROM `, quoter.Quote(self.Statement.Model.Table()), quoter.Quote(self.Statement.IdKey)) + from_clause + where_clause + order_by + limit_str + offset_str
	if lock_clause != "" {
		query_str += " " + lock_clause
	}

	// 加锁查询绕开结果缓存：命中缓存就等于这条 SELECT 没发出去，锁也就没加上。
	var res_ds *dataset.TDataSet
	if !locking {
		// #调用缓存
		res_ds = self.orm.Cacher.GetBySql(table_name, query_str, where_clause_params)
	}
	if res_ds == nil {
		res, err := self._query(query_str, where_clause_params...)
		if err != nil {
			return nil, 0, err
		}
		res_ids = res.Keys(self.Statement.IdKey)
		if !locking {
			self.orm.Cacher.PutBySql(table_name, query_str, where_clause_params, res)
		}
	} else {
		res_ids = res_ds.Keys(self.Statement.IdKey)
	}

	return res_ids, int64(len(res_ids)), nil
}

func (self *TSession) _query(sql string, paramStr ...any) (*dataset.TDataSet, error) {
	if err := self.ensureOpen(); err != nil {
		return nil, err
	}
	defer self._resetStatement()
	for _, filter := range self.orm.dialect.Fmter() {
		sql = filter.Do(sql, self.orm.dialect, self.Statement.Model)
	}

	// LastSQL() 的取值来源。此前 lastSQL/lastSQLArgs **全仓没有一处赋值**，
	// LastSQL() 恒返回空串——又一个"看着能用其实是空壳"的 API（同 ForUpdate()）。
	// 记录的是 Fmter 处理后、真正发给驱动的那条语句。
	self.lastSQL, self.lastSQLArgs = sql, paramStr

	return self.orm._logQuerySql(sql, paramStr, func() (*dataset.TDataSet, error) {
		if self.IsAutoCommit {
			return self._queryWithOrg(sql, paramStr...)
		}
		return self._queryWithTx(sql, paramStr...)
	})
}

func (self *TSession) _queryWithOrg(sql_str string, args ...any) (*dataset.TDataSet, error) {
	var rows *core.Rows
	var err error

	// 会话带 schema：SET LOCAL 与语句必须落在同一条连接上，所以包一个隐式事务。
	// 这里**绕开 Prepared**——预编译语句同样在池上，绑不住那条连接；正确性优先。
	if stmt := self.searchPathSql(); stmt != "" {
		return self.queryInSchemaScope(stmt, sql_str, args...)
	}

	if self.Prepared {
		stmt, err := self._doPrepare(sql_str)
		if err != nil {
			return nil, err
		}
		defer stmt.Close() // 确保stmt在函数退出时被关闭

		rows, err = stmt.QueryContext(self.context, args...)
		if err != nil {
			return nil, self.orm.dialect.MapError(err)
		}
	} else {
		rows, err = self.db.QueryContext(self.context, sql_str, args...)
		if err != nil {
			return nil, self.orm.dialect.MapError(err)
		}
	}

	return self._scanRows(rows)
}

func (self *TSession) _queryWithTx(query string, params ...any) (*dataset.TDataSet, error) {
	rows, err := self.tx.QueryContext(self.context, query, params...)
	if err != nil {
		return nil, self.orm.dialect.MapError(err)
	}

	return self._scanRows(rows)
}

// Exec raw sql
func (self *TSession) _exec(sql_str string, args ...any) (sql.Result, error) {
	if err := self.ensureOpen(); err != nil {
		return nil, err
	}
	defer self._resetStatement()
	for _, filter := range self.orm.dialect.Fmter() {
		sql_str = filter.Do(sql_str, self.orm.dialect, self.Statement.Model)
	}

	self.lastSQL, self.lastSQLArgs = sql_str, args

	// 任何改动库结构的语句都要让 DBMetas 反查缓存失效(见 TOrm.metaCache)。
	// 在执行前就判定关键字,执行成功后再递增 epoch;失败/回滚也递增属过度失效,
	// 安全无害。DML(insert/update/...)不触发,不影响缓存命中。
	ddl := isDDL(sql_str)

	res, err := self.orm._logExecSql(sql_str, args, func() (sql.Result, error) {
		if self.IsAutoCommit {
			// FIXME: oci8 can not auto commit (github.com/mattn/go-oci8)
			if self.orm.dialect.DBType() == ORACLE {
				if err := self.Begin(); err != nil {
					return nil, err
				}

				r, err := self.tx.ExecContext(self.context, sql_str, args...)
				if err != nil {
					self.Rollback(err)
					return nil, self.orm.dialect.MapError(err)
				}

				if err = self.Commit(); err != nil {
					self.Rollback(err)
					return nil, err
				}

				return r, err
			}

			return self._execWithOrg(sql_str, args...)
		}

		return self._execWithTx(sql_str, args...)
	})

	if err == nil && ddl {
		self.orm.metaEpoch.Add(1)
	}

	return res, err
}

// isDDL 粗判一条语句是否会改动库结构(据以让 DBMetas 缓存失效)。只看首个
// 关键字,宁多勿漏——多一次内省是纯性能损耗,漏一次会返回过期结构。
func isDDL(sql_str string) bool {
	s := strings.TrimSpace(sql_str)
	// 跳过行首的 -- 注释与空白,取第一个真正的关键字
	for strings.HasPrefix(s, "--") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = strings.TrimSpace(s[i+1:])
		} else {
			return false
		}
	}
	end := len(s)
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '(' {
			end = i
			break
		}
	}
	switch strings.ToUpper(s[:end]) {
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME", "COMMENT":
		return true
	}
	return false
}

// Execute sql
func (self *TSession) _execWithOrg(query string, args ...any) (sql.Result, error) {
	// 同 _queryWithOrg：带 schema 的自动提交会话包一个隐式事务，让 SET LOCAL 生效。
	if stmt := self.searchPathSql(); stmt != "" {
		return self.execInSchemaScope(stmt, query, args...)
	}

	if self.Prepared {
		stmt, err := self._doPrepare(query)
		if err != nil {
			return nil, err
		}
		defer stmt.Close()

		res, err := stmt.ExecContext(self.context, args...)
		if err != nil {
			return nil, self.orm.dialect.MapError(err)
		}
		return res, nil
	}

	res, err := self.db.ExecContext(self.context, query, args...)
	if err != nil {
		return nil, self.orm.dialect.MapError(err)
	}
	return res, nil
}

func (self *TSession) _execWithTx(sql string, args ...any) (sql.Result, error) {
	res, err := self.tx.ExecContext(self.context, sql, args...)
	if err != nil {
		return nil, self.orm.dialect.MapError(err)
	}
	return res, nil
}

func (self *TSession) _doPrepare(sql string) (*core.Stmt, error) {
	return self.db.PrepareContext(self.context, sql)
}

// scan data to a slice's pointer, slice's length should equal to columns' number
func (self *TSession) _scanRows(rows *core.Rows) (*TDataset, error) {
	// #无论如何都会返回一个Dataset
	res_dataset := dataset.NewDataSet()
	// #提供必要的IdKey/
	if self.Statement.IdKey != "" {
		res_dataset.KeyField = self.Statement.IdKey //设置主键
	}

	if rows != nil {
		defer rows.Close() // 确保在函数退出时关闭rows

		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}

		hasModel := self.Statement.Model != nil // TODO exec,query 的SQL不包含Model

		// Scan 容器只建一次：holders 存值，vals 存指向它们的 *any。复用是安全的
		// ——每行都在本次 Scan 之后、下次 Scan 之前就把值**拷贝**出去
		// (onConvertToRead 与下面的读取都是解引用取值)，没有任何人留存这些指针。
		// 此前每行每列都 reflect.New(ITF_TYPE) 造一个新容器，纯属浪费。
		holders := make([]any, len(cols))
		vals := make([]any, len(cols))
		for idx := range holders {
			vals[idx] = &holders[idx]
		}

		// 列 → 字段 的解析和格式化器的选定都是**行无关**的，只做一次。此前放在
		// 行循环内：每行每列一次 GetFieldByName 查找 + 一次 SetFieldFormater 重复
		// 写入同一个 formatter，开销随行数线性放大。
		fields := make([]IField, len(cols))
		// formats 是**按列**定好的输出格式化器，读到值就地套用。
		//
		// 从前这些格式化器是挂在数据集上的(SetFieldFormater)，只有 TRecordSet.AsMap()
		// 会去查它——GetByField / GetByIndex / AsJson 之外的一切取值路径、以及
		// TDataSet.GroupBy 的分组键，拿到的都是**未格式化**的原值。于是同一列同一行
		// 有两个值：AsMap 给字符串 "2082891348767150080"，GetByField 给 int64；空外键
		// AsMap 给 ""，GetByField 给 0。BigNumberToString 因此只是"看起来打开了"。
		//
		// 代价不是理论上的：o2m / m2m 的 OnRead 都得自己再 utils.ToString 一遍子记录
		// 的 id(否则内嵌 id 列表是裸 int64，前端 JSON 一过 >2^53 就改位)，那两处
		// idAsStr 就是这个洞的补丁。补丁只能补到看得见的地方。
		//
		// 故把格式化**前移到取数当场**：值进 recordset 之前就已经是最终形态，此后
		// 所有读法必然一致。关系字段 OnRead 之后塞进来的复合值(map / []any)天然不
		// 经过这里，也就不再需要 AsMap 那个"只对标量套格式化器"的保护。
		//
		// 那两处 idAsStr 保留：对**远端** comodel(TRemoteModelObject.Read 直接由 RPC
		// 行拼数据集，不走这里)它们仍是唯一的转换点；本地读则退化成对字符串再
		// ToString 一次的空操作。
		formats := make([]func(any) any, len(cols))
		// bigNumPending 标记「这一列还没定过 formatter，且可能需要按大数转字符串」。
		// 只对没有模型字段对应的列(Count 等函数列)成立：它要看实际扫到的值是不是
		// int64，只能进了行循环才知道，但定一次就够。
		bigNumPending := make([]bool, len(cols))
		if hasModel {
			for idx, name := range cols {
				field := self.Statement.Model.GetFieldByName(name)
				fields[idx] = field
				if field == nil {
					// #兼容没有使用 as tag 的大数转换为字符串
					bigNumPending[idx] = self.orm.config.BigNumberToString
					continue
				}

				typeName := field.OutputAs() // as tag 指定输出格式
				if typeName == "" {
					if self.orm.config.BigNumberToString && isBigNumberField(field) {
						// 只有关系字段(外键)的 0 才归空串=「没有关联」；普通 int64
						// 数据列的 0 是合法值，必须原样输出 "0"。详见
						// converterBigNumberToString。
						formats[idx] = converterBigNumberToString(field.IsRelated())
					}
					continue
				}
				formats[idx] = converter(typeName)
			}
		}

		for rows.Next() {
			// 采集数据
			if err = rows.Scan(vals...); err != nil {
				return nil, err
			}

			// 存储到数据集
			// TODO 优化不使用MAP
			rec := dataset.NewRecordSet()
			for idx, name := range cols {
				var value any
				// !NOTE! 转换数据类型输出
				if field := fields[idx]; field != nil {
					value = field.onConvertToRead(self, cols, vals, idx)
				} else {
					value = holders[idx]
					if bigNumPending[idx] {
						if _, ok := value.(int64); ok {
							// 函数列(Count 等)的 0 是合法值，不归空串。
							formats[idx] = converterBigNumberToString(false)
						}
						// 无论这一行是不是 int64 都不再重试：同一列的 SQL 类型固定，
						// 首行判不出来后面也判不出来。
						bigNumPending[idx] = false
					}
				}

				// 就地套用。首行也在内——formats[idx] 若是上面这一行刚定下的，
				// 当前这个值同样要过一遍，否则第一行会与其余行形态不同。
				if f := formats[idx]; f != nil && value != nil {
					value = f(value)
				}

				if !rec.SetByField(name, value, false) {
					return nil, fmt.Errorf("add %s value to recordset fail.", name)
				}
			}

			res_dataset.AppendRecord(rec)
		}
	}

	res_dataset.First()
	return res_dataset, nil
}
