package orm

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/core"
)

// search and return the id list only
func (self *TSession) Search() ([]interface{}, int64, error) {
	defer self._resetStatement()
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, 0, ErrInvalidSession
	}

	return self._search("", nil)
}

// Query a raw sql and return records as dataset
func (self *TSession) Query(sql string, paramStr ...interface{}) (*dataset.TDataSet, error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	return self._query(sql, paramStr...)
}

// Exec raw sql
func (self *TSession) Exec(sql_str string, args ...interface{}) (sql.Result, error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	return self._exec(sql_str, args...)
}

func (self *TSession) Count() (int, error) {
	model := self.Statement.Model
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

	sql, args, err := self.Statement.generate_sum(fieldName)
	if err != nil {
		return 0, err
	}

	ds, err := self._query(sql, args...)
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
func (self *TSession) _search(access_rights_uid string, context map[string]interface{}) (res_ids []interface{}, count int64, err error) {
	var (
		//fields_str string
		//where_str    string
		limit_str           string
		offset_str          string
		from_clause         string
		where_clause        string
		query_str           string
		order_by            string
		where_clause_params []interface{}
		query               *TQuery
	)

	if context == nil {
		context = make(map[string]interface{})
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
	if self.Statement.IsCount {
		// 添加支持Count函数
		// TODO 优化成自动
		self.Statement.Funcs("count")
		var count int64
		// Ignore order, limit and offset when just counting, they don't make sense and could
		// hurt performance
		query_str = `SELECT count(1) FROM ` + from_clause + where_clause
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

	// #调用缓存
	res_ds := self.orm.Cacher.GetBySql(table_name, query_str, where_clause_params)
	if res_ds == nil {
		res, err := self._query(query_str, where_clause_params...)
		if err != nil {
			return nil, 0, err
		}
		res_ids = res.Keys(self.Statement.IdKey)
		self.orm.Cacher.PutBySql(table_name, query_str, where_clause_params, res)
	} else {
		res_ids = res_ds.Keys(self.Statement.IdKey)
	}

	return res_ids, int64(len(res_ids)), nil
}

func (self *TSession) _query(sql string, paramStr ...interface{}) (*dataset.TDataSet, error) {
	defer self._resetStatement()
	for _, filter := range self.orm.dialect.Fmter() {
		sql = filter.Do(sql, self.orm.dialect, self.Statement.Model)
	}

	return self.orm.logQuerySql(sql, paramStr, func() (*dataset.TDataSet, error) {
		if self.IsAutoCommit {
			return self._queryWithOrg(sql, paramStr...)
		}
		return self._queryWithTx(sql, paramStr...)
	})
}

func (self *TSession) _queryWithOrg(sql_str string, args ...interface{}) (*dataset.TDataSet, error) {
	var rows *core.Rows
	var err error
	if self.Prepared {
		stmt, err := self._doPrepare(sql_str)
		if err != nil {
			return nil, err
		}
		rows, err = stmt.Query(args...)
		if err != nil {
			return nil, err
		}
	} else {
		rows, err = self.db.Query(sql_str, args...)
		if err != nil {
			return nil, err
		}
	}

	return self._scanRows(rows)
}

func (self *TSession) _queryWithTx(query string, params ...interface{}) (*dataset.TDataSet, error) {
	rows, err := self.tx.QueryContext(context.Background(), query, params...)
	if err != nil {
		return nil, err
	}

	return self._scanRows(rows)
}

// Exec raw sql
func (self *TSession) _exec(sql_str string, args ...interface{}) (sql.Result, error) {
	defer self._resetStatement()
	for _, filter := range self.orm.dialect.Fmter() {
		sql_str = filter.Do(sql_str, self.orm.dialect, self.Statement.Model)
	}

	return self.orm.logExecSql(sql_str, args, func() (sql.Result, error) {
		if self.IsAutoCommit {
			// FIXME: oci8 can not auto commit (github.com/mattn/go-oci8)
			if self.orm.dialect.DBType() == ORACLE {
				self.Begin()
				r, err := self.tx.ExecContext(self.context, sql_str, args...)
				if err != nil {
					return nil, err
				}

				if err = self.Commit(); err != nil {
					return nil, err
				}

				return r, err
			}
			return self._execWithOrg(sql_str, args...)
		}
		return self._execWithTx(sql_str, args...)
	})
}

// Execute sql
func (self *TSession) _execWithOrg(query string, args ...interface{}) (sql.Result, error) {
	if self.Prepared {
		var stmt *core.Stmt

		stmt, err := self._doPrepare(query)
		if err != nil {
			return nil, err
		}

		return stmt.ExecContext(self.context, args...)
	}

	return self.db.ExecContext(self.context, query, args...)
}

func (self *TSession) _execWithTx(sql string, args ...interface{}) (sql.Result, error) {
	return self.tx.ExecContext(self.context, sql, args...)
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
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}

		length := len(cols)
		vals := make([]interface{}, length)

		var value interface{}
		var field IField
		defer rows.Close()
		for rows.Next() {
			// TODO 优化不使用MAP
			rec := dataset.NewRecordSet()
			//rec.Fields(cols...)

			// 创建数据容器
			for idx := range cols {
				vals[idx] = reflect.New(ITF_TYPE).Interface()
			}

			// 采集数据
			err = rows.Scan(vals...)
			if err != nil {
				return nil, err
			}

			// 存储到数据集
			for idx, name := range cols {
				// !NOTE! 转换数据类型输出
				if self.Statement.Model != nil { // TODO exec,query 的SQL不包含Model
					field = self.Statement.Model.GetFieldByName(name)
					if field != nil {
						value = field.onConvertToRead(self, cols, vals, idx)
					} else {
						value = nil // 初始化
						// 处理函数字段 Count 等
						for _, funcName := range self.Statement.FuncsClause {
							if strings.HasPrefix(funcName, name) { // TODO 这里需要更高效的判断
								value = *vals[idx].(*interface{})
							}
						}

						if value == nil {
							value = *vals[idx].(*interface{})
						}
					}
				} else {
					value = *vals[idx].(*interface{})
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
