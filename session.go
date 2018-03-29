package orm

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"vectors/logger"
	"vectors/utils"

	"github.com/go-xorm/core"
)

type (
	TExecResult int64

	IRawSession interface {
		CreateIndexes(model string) error
		CreateTable(model string) error
		CreateUniques(model string) error
		DropTable(model string) (err error)
		Exec(sql_str string, args ...interface{}) (sql.Result, error)
		Query(sqlStr string, paramStr ...interface{}) (res_dataset *TDataSet, err error)
		Ping() error
		IsEmpty(model string) (bool, error)
		IsExist(model ...string) (bool, error)
	}

	TSession struct {
		// TODO 自己实现
		db *core.DB
		tx *core.Tx // 由Begin 传递而来

		orm       *TOrm
		model     *TModel
		Statement TStatement

		IsAutoCommit           bool // dflt is true
		IsAutoClose            bool
		IsCommitedOrRollbacked bool
		AutoResetStatement     bool
		IsClassic              bool // #使用Model实例为参数
		Prepared               bool

		lastSQL     string
		lastSQLArgs []interface{} // 储存有序值
	}
)

func (v TExecResult) LastInsertId() (int64, error) {
	return int64(v), nil
}

func (v TExecResult) RowsAffected() (int64, error) {
	return int64(v), nil
}

func (self *TSession) init() error {
	self.Statement.Init()
	self.Statement.Session = self
	self.IsAutoCommit = true // 默认情况下单个SQL是不用事务自动
	self.IsAutoClose = false
	self.AutoResetStatement = true
	self.IsCommitedOrRollbacked = false
	self.Prepared = false
	return nil
}

/*******************************************************************
    DB 实现接口
*******************************************************************/
// Close release the connection from pool
func (self *TSession) Close() {
	//for _, v := range self.stmtCache {
	//	v.Close()
	//}

	if self.db != nil {
		// When Close be called, if session is a transaction and do not call
		// Commit or Rollback, then call Rollback.
		if self.tx != nil && !self.IsCommitedOrRollbacked {
			self.Rollback()
		}
		self.db = nil
		self.tx = nil
		//self.stmtCache = nil
		self.init()

	}
}

// Ping test if database is ok
func (self *TSession) Ping() error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	return self.db.Ping()
}

// Begin a transaction
func (self *TSession) Begin() error {
	// 当第一次调用时才修改Tx
	if self.IsAutoCommit {
		tx, err := self.db.Begin()
		if err != nil {
			return err
		}
		self.IsAutoCommit = false
		self.IsCommitedOrRollbacked = false
		self.tx = tx
	}

	return nil
}

func (self *TSession) Commit() (err error) {
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		if err = self.tx.Commit(); err != nil {
			return
		}

	}
	return
}

// Rollback When using transaction, you can rollback if any error
func (self *TSession) Rollback() error {
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		//session.saveLastSQL(session.Engine.dialect.RollBackStr())
		self.IsCommitedOrRollbacked = true
		return self.tx.Rollback()
	}
	return nil
}

// Query a raw sql and return records as []map[string][]byte
func (self *TSession) Query(sqlStr string, paramStr ...interface{}) (res_dataset *TDataSet, err error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	return self.query(sqlStr, paramStr...)
}

func (self *TSession) query(sqlStr string, paramStr ...interface{}) (res_dataset *TDataSet, err error) {

	for _, filter := range self.orm.dialect.Filters() {
		sqlStr = filter.Do(sqlStr, self.orm.dialect, self.Statement.Table)
	}

	return self.orm.log_query_sql(sqlStr, paramStr, func() (*TDataSet, error) {
		if self.IsAutoCommit {
			return self.org_query(sqlStr, paramStr...)
		}
		return self.tx_query(sqlStr, paramStr...)

	})

}

func (self *TSession) do_prepare(sqlStr string) (stmt *core.Stmt, err error) {
	stmt, err = self.db.Prepare(sqlStr)
	if err != nil {
		return nil, err
	}

	return
}

func (self *TSession) org_query(sql_str string, args ...interface{}) (res_dataset *TDataSet, res_err error) {
	var (
		lRows *core.Rows
		stmt  *core.Stmt
	)

	if self.Prepared {
		stmt, res_err = self.do_prepare(sql_str)
		if res_err != nil {
			return
		}
		lRows, res_err = stmt.Query(args...)
		if res_err != nil {
			return
		}
	} else {
		lRows, res_err = self.db.Query(sql_str, args...)
		if res_err != nil {
			return
		}
	}

	// #无论如何都会返回一个Dataset
	res_dataset = NewDataSet()
	// #提供必要的IdKey
	if self.Statement.IdKey != "" {
		res_dataset.KeyField = self.Statement.IdKey //"id" //设置主键 TODO:可以做到动态
	} else {

	}

	if lRows != nil {
		defer lRows.Close()
		for lRows.Next() {
			tempMap := make(map[string]interface{})
			res_err = lRows.ScanMap(&tempMap)
			if res_err != nil {
				return
			}

			res_dataset.NewRecord(tempMap)
		}
	}

	return
}

func (self *TSession) tx_query(sqlStr string, params ...interface{}) (res_dataset *TDataSet, res_err error) {
	var (
		lRows *core.Rows
	)

	lRows, res_err = self.tx.Query(sqlStr, params...)
	//logger.Dbg("lRows1", lRows, res_err)
	if res_err != nil {
		return
	}
	// 无论如何都会返回一个Dataset
	res_dataset = NewDataSet()
	if self.Statement.IdKey != "" {
		res_dataset.KeyField = self.Statement.IdKey //"id" //设置主键 TODO:可以做到动态
	}

	//res_dataset.SetKeyField(self.Statement.IdKey) // 默认不设置
	if lRows != nil {
		defer lRows.Close()
		for lRows.Next() {
			tempMap := make(map[string]interface{})
			res_err = lRows.ScanMap(&tempMap)
			if res_err != nil {
				return
			}

			//logger.Dbg("txQuery tempMap :", tempMap)
			res_dataset.NewRecord(tempMap)
		}
	}
	return
}

// Exec raw sql
func (self *TSession) Exec(sql_str string, args ...interface{}) (sql.Result, error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	return self.exec(sql_str, args...)
}

// Exec raw sql
func (self *TSession) exec(sql_str string, args ...interface{}) (sql.Result, error) {
	for _, filter := range self.orm.dialect.Filters() {
		sql_str = filter.Do(sql_str, self.orm.dialect, self.Statement.Table)
	}

	// 过滤Pg 的插入语句
	if self.orm.dialect.DriverName() == "postgres" && strings.Count(strings.ToLower(sql_str), "returning") == 1 {
		res, err := self.query(sql_str, args...)
		//utils.Logger.DebugLn("exexex", res, err)
		if !logger.LogErr(err) && res.Count() > 0 {
			id := res.Record().GetByIndex(0).AsInteger()
			return TExecResult(id), err
		}
		return nil, err
	}

	return self.orm.log_exec_sql(sql_str, args, func() (sql.Result, error) {
		if self.IsAutoCommit {
			// FIXME: oci8 can not auto commit (github.com/mattn/go-oci8)
			if self.orm.dialect.DBType() == core.ORACLE {
				self.Begin()
				r, err := self.tx.Exec(sql_str, args...)
				self.Commit()
				return r, err
			}
			return self.org_exec(sql_str, args...)
		}
		return self.tx_exec(sql_str, args...)
	})

}

// Execute sql
func (self *TSession) org_exec(sqlStr string, args ...interface{}) (res sql.Result, err error) {
	if self.Prepared {
		var stmt *core.Stmt

		stmt, err = self.do_prepare(sqlStr)
		if err != nil {
			return
		}

		res, err = stmt.Exec(args...)
		if err != nil {
			return
		}
		return
	}

	return self.db.Exec(sqlStr, args...)
}

func (self *TSession) tx_exec(sqlStr string, args ...interface{}) (sql.Result, error) {
	//for _, filter := range session.Engine.dialect.Filters() {
	//	sqlStr = filter.Do(sqlStr, session.Engine.dialect, session.Statement.RefTable)
	//}

	//session.saveLastSQL(sqlStr, args...)

	return self.tx.Exec(sqlStr, args...)

}

// Sync2 synchronize structs to database tables
func (self *TSession) SyncModel(region string, models ...interface{}) error {
	// 获取基本数据库信息
	tables, err := self.orm.DBMetas()
	if err != nil {
		return err
	}

	for _, model := range models {
		lModel := self.orm.mapping(region, model)
		lModelName := lModel.GetModelName()
		lTableName := lModel.GetTableName()

		var lTable *core.Table // 数据库存在的
		for _, tb := range tables {
			if strings.ToLower(tb.Name) == strings.ToLower(lTableName) {
				lTable = tb
				break
			}
		}

		// #设置该Session的Model/Table
		self.Model(lModel.GetModelName(), region)

		if lTable == nil {
			// 如果数据库不存在改Model对应的表则创建
			//err = self.StoreEngine(s.Statement.StoreEngine).CreateTable(bean)
			err = self.CreateTable(lModelName)
			if err != nil {
				return err
			}

			err = self.CreateUniques(lModelName)
			if err != nil {
				return err
			}

			err = self.CreateIndexes(lModelName)
			if err != nil {
				return err
			}
		} else {
			err := self.alter_table(lModel, lModel.table, lTable)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

/* #
* @model:提供新Session
* @new_tb:Model映射后的新表结构
* @cur_tb:当前数据库的表结构
 */
func (self *TSession) alter_table(model *TModel, new_tb, cur_tb *core.Table) (err error) {
	lOrm := self.orm
	lTableName := new_tb.Name
	var cur_col *core.Column
	for _, col := range new_tb.Columns() {
		// 调出现有Col
		cur_col = nil
		for _, col2 := range cur_tb.Columns() {
			if strings.ToLower(col.Name) == strings.ToLower(col2.Name) {
				cur_col = col2
				break
			}
		}

		if cur_col != nil {
			expectedType := lOrm.dialect.SqlType(col)
			curType := lOrm.dialect.SqlType(cur_col)
			if expectedType != curType {
				// 修改数据类型
				// 如果是修改字符串到
				if expectedType == core.Text && strings.HasPrefix(curType, core.Varchar) {
					// currently only support mysql & postgres
					if lOrm.dialect.DBType() == core.MYSQL ||
						lOrm.dialect.DBType() == core.POSTGRES {
						logger.Logger.Info("Table %s column %s change type from %s to %s\n",
							lTableName, col.Name, curType, expectedType)
						_, err = lOrm.Exec(lOrm.dialect.ModifyColumnSql(new_tb.Name, col))
					} else {
						logger.Logger.Warn("Table %s column %s db type is %s, struct type is %s\n",
							lTableName, col.Name, curType, expectedType)
					}

					// 如果是同是字符串 则检查长度变化 for mysql
				} else if strings.HasPrefix(curType, core.Varchar) && strings.HasPrefix(expectedType, core.Varchar) {
					if lOrm.dialect.DBType() == core.MYSQL {
						if cur_col.Length < col.Length {
							logger.Logger.Info("Table %s column %s change type from varchar(%d) to varchar(%d)\n",
								lTableName, col.Name, cur_col.Length, col.Length)
							_, err = lOrm.Exec(lOrm.dialect.ModifyColumnSql(lTableName, col))
						}
					}
					//其他
				} else {
					if !(strings.HasPrefix(curType, expectedType) && curType[len(expectedType)] == '(') {
						logger.Logger.Warn("Table %s column %s db type is %s, struct type is %s",
							lTableName, col.Name, curType, expectedType)
					}
				}
				// 如果是同是字符串 则检查长度变化 for mysql
			} else if expectedType == core.Varchar {
				if lOrm.dialect.DBType() == core.MYSQL {
					if cur_col.Length < col.Length {
						logger.Logger.Info("Table %s column %s change type from varchar(%d) to varchar(%d)\n",
							lTableName, col.Name, cur_col.Length, col.Length)
						_, err = lOrm.Exec(lOrm.dialect.ModifyColumnSql(lTableName, col))
					}
				}
			}

			//
			if col.Default != cur_col.Default {
				logger.Logger.Warn("Table %s Column %s db default is %s, struct default is %s",
					lTableName, col.Name, cur_col.Default, col.Default)
			}
			if col.Nullable != cur_col.Nullable {
				logger.Logger.Warn("Table %s Column %s db nullable is %v, struct nullable is %v",
					lTableName, col.Name, cur_col.Nullable, col.Nullable)
			}

			// 如果现在表无该字段则添加
		} else {
			lSession := lOrm.NewSession()
			lSession.Model(model.GetModelName())
			//TODO # 修正上面指向错误Model
			lSession.model = model
			lSession.Statement.Table = new_tb
			defer lSession.Close()
			err = lSession.add_column(col.Name)
		}
		if err != nil {
			return err
		}
	}

	var foundIndexNames = make(map[string]bool)
	var addedNames = make(map[string]*core.Index)

	// 检查更新索引 先取消索引载添加需要的
	// 取消Idex
	for name, index := range new_tb.Indexes {
		var cur_index *core.Index
		for name2, index2 := range cur_tb.Indexes {
			if index.Equal(index2) {
				cur_index = index2
				foundIndexNames[name2] = true
				break
			}
		}

		// 现有的idex
		if cur_index != nil {
			if cur_index.Type != index.Type { // 类型不同则清除
				sql := lOrm.dialect.DropIndexSql(lTableName, cur_index)
				_, err = lOrm.Exec(sql)
				if err != nil {
					return err
				}
				cur_index = nil
			}
		} else {
			addedNames[name] = index // 加入列表稍后再添加
		}
	}

	// 清除已经作删除的索引
	for name2, index2 := range cur_tb.Indexes {
		if _, ok := foundIndexNames[name2]; !ok { // 在当前数据表且不再新数据表里的索引都要清除
			sql := lOrm.dialect.DropIndexSql(lTableName, index2)
			_, err = lOrm.Exec(sql)
			if err != nil {
				return err
			}
		}
	}

	// 重新添加索引
	for name, index := range addedNames {
		if index.Type == core.UniqueType {
			lSession := lOrm.NewSession()
			lSession.Model(model.GetModelName())
			//TODO # 修正上面指向错误Model
			lSession.model = model
			lSession.Statement.Table = new_tb
			defer lSession.Close()
			err = lSession.add_unique(lTableName, name)
		} else if index.Type == core.IndexType {
			lSession := lOrm.NewSession()
			lSession.Model(model.GetModelName())
			//TODO # 修正上面指向错误Model
			lSession.model = model
			lSession.Statement.Table = new_tb
			defer lSession.Close()
			err = lSession.add_index(lTableName, name)
		}
		if err != nil {
			return err
		}
	}
	return
}

// IsTableExist if a table is exist
func (self *TSession) IsExist(model ...string) (bool, error) {
	//self._check_model()

	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	lModelName := ""
	if len(model) > 0 {
		lModelName = model[0]
	} else if self.model != nil {
		lModelName = self.model._name
	} else {
		return false, errors.New("model should not be blank")
	}

	tableName := strings.Replace(lModelName, ".", "_", -1)
	tableName = utils.SnakeCasedName(tableName)
	sqlStr, args := self.orm.dialect.TableCheckSql(tableName)
	lDs, err := self.query(sqlStr, args...)

	return lDs.Count() > 0, err
}

// IsTableEmpty if table have any records
func (self *TSession) IsEmpty(model string) (bool, error) {
	defer self.Statement.Init()

	if self.IsAutoClose {
		defer self.Close()
	}

	lCount, err := self.Model(model).Count()
	return lCount == 0, err
}

/*******************************************************************
    ORM 实现接口
*******************************************************************/
func (self *TSession) Orm() *TOrm {
	return self.orm
}

//
func (self *TSession) Model(model string, region ...string) *TSession {
	var md IModel
	var err error
	// #如果Session已经预先指定Model
	if self.model != nil {
		md = self.model
	} else {
		md, err = self.orm.GetModel(model, region...)
		if err != nil {
			logger.Panic(err.Error())
		}
	}

	if md != nil {
		self.IsClassic = true
		self.model = md.GetBaseModel()
		self.Statement.Table = self.model.table
		self.Statement.TableNameClause = md.GetTableName()

		// # 主键
		self.Statement.IdKey = "id"
		if self.model.RecordField != nil {
			self.Statement.IdKey = self.model.RecordField.Name()
		}

		return self
	} else {
		self.IsClassic = false
		lTableName := utils.SnakeCasedName(strings.Replace(model, ".", "_", -1))
		//logger.Logger.Err("Model %s is not a standard model type of this system", lTableName)
		self.Statement.Table = self.orm.tables[lTableName]
		if self.Statement.Table == nil {
			logger.Logger.ErrLn("the table is not in database.")
			return nil
		}
		self.Statement.AltTableNameClause = lTableName
		self.Statement.TableNameClause = lTableName

		// # 主键
		self.Statement.IdKey = "id"
		col := self.Statement.Table.AutoIncrColumn()
		if col != nil && ((!col.Nullable && col.IsPrimaryKey && col.IsAutoIncrement) ||
			(!col.Nullable && col.IsAutoIncrement)) {
			self.Statement.IdKey = self.Statement.Table.AutoIncrement
		}

	}

	return self
}

// 选择字段
func (self *TSession) Select(fields ...string) *TSession {
	self.Statement.Select(fields...)

	return self
}

// Omit Only not use the paramters as select or update columns
func (self *TSession) Omit(fields ...string) *TSession {
	self.Statement.Omit(fields...)
	return self
}

// Id provides converting id as a query condition
func (self *TSession) Ids(ids ...string) *TSession {
	self.Statement.Ids(ids...)
	return self
}

func (self *TSession) Desc(fileds ...string) *TSession {
	self.Statement.Desc(fileds...)
	return self
}

// Where 条件
// 支持Domain 返回解析为Domain
func (self *TSession) Where(where_clause string, args ...interface{}) *TSession {
	self.Statement.Where(where_clause, args...)
	return self
}

// And provides custom query condition.
func (self *TSession) And(query_clause string, args ...interface{}) *TSession {
	self.Statement.And(query_clause, args...)
	return self
}

// Or provides custom query condition.
func (self *TSession) Or(query_clause string, args ...interface{}) *TSession {
	self.Statement.Or(query_clause, args...)
	return self
}

func (self *TSession) In(query_clause string, args ...interface{}) *TSession {
	self.Statement.In(query_clause, args...)
	return self
}

func (self *TSession) NotIn(query_clause string, args ...interface{}) *TSession {
	self.Statement.NotIn(query_clause, args...)
	return self
}

// 支持Domain 返回解析为Domain
func (self *TSession) Domain(domain interface{}) *TSession {
	self.Statement.Domains(domain)
	return self
}

// GroupBy Generate Group By statement
func (self *TSession) GroupBy(keys string) *TSession {
	self.Statement.GroupBy(keys)
	return self
}

func (self *TSession) OrderBy(order string) *TSession {
	self.Statement.OrderBy(order)
	return self
}

func (self *TSession) Limit(limit int, offset ...int) *TSession {
	self.Statement.Limit(limit, offset...)
	return self
}

//转换
func (self *TSession) _validate(vals map[string]interface{}) {
	for key, val := range vals {
		if f := self.model.FieldByName(key); f != nil && !f.IsRelated() {
			//web.Debug("_Validate", key, val, f._type)
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

			case "many2one":
				// TODO 支持多种数据类型
				//self.osv.GetModel(f.relmodel_name)
				vals[key] = utils.Itf2Int(val)
			}
		}
	}
}

func (self *TSession) _create(src interface{}, classic_create bool) (res_id int64, res_err error) {
	if len(self.Statement.TableName()) < 1 {
		return 0, ErrTableNotFound
	}

	vals := self.itf_to_itfmap(src)
	if len(vals) == 0 {
		res_err = fmt.Errorf("can't support this type of values: %v", src)
		return 0, res_err
	}

	// the list of tuples used in this formatting corresponds to
	// tuple(field_name, format, value)
	// In some case, for example (id, create_date, write_date) we does not
	// need to read the third value of the tuple, because the real value is
	// encoded in the second value (the format).

	var (
		fields = ""
		values = ""
		params = make([]interface{}, 0)
		/*		UserId int64

				// 用于更新本Model的实际数据
				new_vals = map[string]interface{}{
				/*    # list of column assignments defined as tuples like:
				      #   (column_name, format_string, column_value)
				      #   (column_name, sql_formula)
				      # Those tuples will be used by the string formatting for the INSERT
				      # statement below.
				      ('id', "nextval('%s')" % self._sequence),*/
		//		}
		// tocreate[table][field]=value
		// tocreate = make(map[string]map[string]interface{})
		//rel_vals       = make(map[string]map[string]interface{})
		//upd_todo       = make([]string, 0) // function 字段组 采用其他存储方式
		//unknown_fields = make([]string, 0)

		lNewVals map[string]interface{}
		lRefVals map[string]map[string]interface{}
		lNewTodo []string
	)

	lNewVals, lRefVals, lNewTodo = self._separate_values(vals, self.Statement.Fields, self.Statement.NullableFields, true, false)

	//
	for tbl, rel_vals := range lRefVals {
		if len(rel_vals) == 0 {
			continue // # 关系表无数据更新则忽略
		}

		//utils.Dbg(" rel table", tbl)
		// ???删除关联外键
		//if _, has := vals[self.model._relations[tbl]]; has {
		//	delete(vals, self.model._relations[tbl])
		//}

		// 获取管理表ID字段
		record_id := ""
		record_id = utils.Itf2Str(rel_vals["id"])
		//logger.Dbg("record_id", record_id)

		// 创建或者更新关联表记录
		lMdlObj, err := self.orm.osv.GetModel(tbl) // #i
		if err == nil {
			//logger.Dbg("record_id", record_id)
			if record_id == "" || record_id == "0" {
				effect, err := lMdlObj.Records().Create(rel_vals)
				if logger.LogErr(err) {

				}
				record_id = utils.IntToStr(effect)
			} else {
				lMdlObj.Records().Ids(record_id).Write(rel_vals)
			}
		} else {
			logger.LogErr(err)
		}

		lNewVals[self.model._relations[tbl]] = record_id
	}

	/*  #Start : Set bool fields to be False if they are not touched(to make search more powerful)
		    bool_fields = [x for x in self._columns.keys() if self._columns[x]._type=='boolean']
	        for bool_field in bool_fields:
	            if bool_field not in vals:
	                vals[bool_field] = False
	        #End
	*/

	// 被设置默认值的字段赋值给Val
	for k, v := range self.model._default {
		if lNewVals[k] == nil {
			lNewVals[k] = v //fmt. lFld._symbol_c
		}
	}

	// #验证数据类型
	//TODO 需要更准确安全
	self.model._validate(lNewVals)

	// 字段,值
	for k, v := range lNewVals {
		if v == nil { // 过滤nil 的值
			continue
		}

		if fields != "" {
			fields += ","
			values += ","
		}
		fields += self.orm.Quote(k)

		values += "?" // 注意冒号
		params = append(params, v)
	}

	lReturnKey := ""
	//logger.Dbg("TableByType", self.model._cls_type, self.orm.TableByType(self.model._cls_type))
	//lRecField := self.orm.TableByType(self.model._cls_type).RecordField
	lRecField := self.orm.Quote(self.orm.GetTable(self.Statement.TableName()).AutoIncrement)
	//lRecField := self.orm.Quote(self.model.RecordField.Name)
	if lRecField != "" {
		lReturnKey = ` RETURNING ` + lRecField
	}

	sql := `INSERT INTO ` + self.Statement.TableName() + ` (` + fields + `) VALUES (` + values + `) ` + lReturnKey
	//sql = fmt.Sprintf(sql, params...)
	res, err := self.Exec(sql, params...)
	if err != nil {
		return res_id, err
	}

	res_id, res_err = res.LastInsertId()
	if res_err != nil {
		return
	}

	if self.IsClassic || classic_create {
		// 更新关联字段
		for _, name := range lNewTodo {
			lField := self.model.FieldByName(name)
			logger.Dbg("create lNewTodo", name, lField, res_id)
			if lField != nil {
				lField.OnConvertToWrite(
					&TFieldEventContext{
						Session: self,
						Model:   self.model,
						Id:      utils.IntToStr(res_id),
						Field:   lField,
						Value:   vals[name]})
				/*
					// result += self._columns[name].set(cr, self, id_new, name, vals[name], user, rel_context) or []
					//self._update_field(res_id, lField, utils.IntToStr(vals[name]), nil)
					//			logger.Dbg("id:", lIds, id)

					lField.OnWrite(&TFieldEventContext{
						Session: self,
						Model:   self.model,
						Id:      utils.IntToStr(res_id),
						Field:   lField,
						Value:   utils.IntToStr(vals[name])})
				*/
			}
		}
	}

	if res_id != 0 {
		//更新缓存
		table_name := self.Statement.TableName()
		lRec := NewRecordSet(nil, lNewVals)
		self.orm.cacher.PutById(table_name, utils.IntToStr(res_id), lRec) //for create
		// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
		self.orm.cacher.ClearByTable(table_name) //for create
	}

	return
}

func (self *TSession) _generate_caches_key(model, key interface{}) string {
	return fmt.Sprintf(`%v:%v`, model, key)
}

//, includeVersion bool, includeUpdated bool, includeNil bool,
//	includeAutoIncr bool, allUseBool bool, useAllCols bool,
//	mustColumnMap map[string]bool, nullableMap map[string]bool,
//	columnMap map[string]bool, update, unscoped bool
func (self *TSession) _separate_values(vals map[string]interface{}, must_fields map[string]bool, nullable_fields map[string]bool, include_nil bool, include_pkey bool) (new_vals map[string]interface{}, rel_vals map[string]map[string]interface{}, upd_todo []string) {
	// #@@ create record not need to including pk
	//var lIncludePK bool = true
	//if len(include_pkey) > 0 {
	///	lIncludePK = include_pkey[0]
	//}
	// 用于更新本Model的实际数据
	/*    # list of column assignments defined as tuples like:
	      #   (column_name, format_string, column_value)
	      #   (column_name, sql_formula)
	      # Those tuples will be used by the string formatting for the INSERT
	      # statement below.
	      ('id', "nextval('%s')" % self._sequence),*/
	new_vals = make(map[string]interface{})
	rel_vals = make(map[string]map[string]interface{})
	upd_todo = make([]string, 0) // function 字段组 采用其他存储方式
	//unknown_fields = make([]string, 0)

	// 保存关联表用于更新创建关联表数据
	for tbl, _ := range self.model._relations {
		//logger.Dbg("_relations", tbl)
		rel_vals[tbl] = make(map[string]interface{}) // 新建空Map以防Nil导致内存出错

		if val, has := vals[self.model._relations[tbl]]; has && val != nil {
			//新建新的并存入已经知道的ID
			rel_id := utils.Itf2Str(val)
			//logger.Dbg("has record_id", vals[self.model._relations[tbl]])

			if rel_id != "0" && rel_id != "" { //TODO 强制ORM使用Id作为主键
				rel_vals[tbl]["id"] = rel_id //utils.Itf2Str(vals[self.model._relations[tbl]])
			}

		}
	}

	// 处理常规字段
	for name, field := range self.model.GetFields() {
		if field == nil {
			continue
		}

		lCol := field.Base().column
		if lCol != nil && !include_pkey && (lCol.IsAutoIncrement || lCol.IsPrimaryKey) {
			continue // no use
		}
		//logger.Dbg("_separate_values XXX:", name, field)
		// 格式化数据
		// update time zone to create and update tags' fields
		if !include_pkey && field.Base()._is_created {
			lTimeItfVal, _ := self.orm._now_time(field.Type())
			vals[name] = lTimeItfVal

		} else if field.Base()._is_created {
			continue

		} else if field.Base()._is_updated {
			lTimeItfVal, _ := self.orm._now_time(field.Type())
			vals[name] = lTimeItfVal
		}

		lMustField := must_fields[name]
		lNullableField := nullable_fields[name]
		if val, has := vals[name]; has {
			// 过滤可以为空的字段空字段
			//logger.Dbg("## XXX:", name, val, has, val == nil, utils.IsBlank(val))
			if !lMustField && !lNullableField && !include_nil && (val == nil || utils.IsBlank(val)) {
				continue
			}

			//logger.Dbg("## VV:", name, lCol.SQLType.IsNumeric())
			if lCol != nil && lCol.SQLType.IsNumeric() {
				var blank interface{} = "0"
				//logger.Dbg("## VV:", name, val, blank, reflect.TypeOf(val), val == blank)
				blank = 0
				//logger.Dbg("## VV:", name, val, blank, reflect.TypeOf(val), val == blank)
				if val == blank {
					//logger.Dbg("## VV:", name)
					continue
				}
			}

			// TODO 优化锁
			// #相同名称的字段分配给对应表
			comm_tables := self.model.CommonFieldByName(name) // 获得拥有该字段的所有表
			if comm_tables != nil {
				// 为各表预存值

				for tbl, _ := range comm_tables {
					//logger.Dbg("lComm:", comm_tables, tbl, name, self.model._table, self.model.GetModelName())
					if tbl == self.model.GetModelName() {
						new_vals[name] = val // 为当前表添加共同字段

					} else if rel_vals[tbl] != nil {
						rel_vals[tbl][name] = val // 为关联表添加共同字段

					}
				}

				continue // 字段分配完毕
			}
			//utils.Dbg("write@:", key)
			//utils.Dbg("write@:", field.foreign_field)
			//utils.Dbg("write@:", self._relate_fields[key])
			//  ffield = self._fields.get(field)
			//  if ffield and ffield.deprecated:
			//      _logger.warning('Field %s.%s is deprecated: %s', self._name, field, ffield.deprecated)
			//#*** 非Model固有字段归为关联表字段 2个判断缺一不可
			//#1 判断是否是关联表可能性
			//#2 判断是否Model和关联Model都有该字段
			if rel_fld := self.model.RelateFieldByName(name); rel_fld != nil && field.IsForeignField() {
				// 如果是继承字段移动到tocreate里创建记录，因本Model对应的数据没有该字段
				lTableName := rel_fld.RelateTableName
				//logger.Dbg("lTableName", lTableName)
				rel_vals[lTableName][name] = val

				//updend = append(updend, name)
			} else {
				if field.Store() && field.ColumnType() != "" {
					new_vals[name] = field.SymbolFunc()(utils.Itf2Str(val))
				} else {
					upd_todo = append(upd_todo, name)
				}
				//utils.Dbg("write:", key)
				/*
					if field.IsClassicWrite() && field.Base().Fnct_inv() == nil {
						if !field.Translatable() { //TODO totranslate &&

							new_vals[name] = field.SymbolFunc()(utils.Itf2Str(val))

							//direct = append(direct, name)
						} else {
							upd_todo = append(upd_todo, name)
						}
					}
				*/
				if !field.IsForeignField() && field.Type() == "selection" && val != nil {
					self._check_selection_field_value(field, val) //context
				}
			}
		}
	}

	return
}

// #fix:由于更新可能只对少数字段更新,更新不适合使用缓存
func (self *TSession) _write(src interface{}, context map[string]interface{}) (res_effect int64, res_err error) {
	if len(self.Statement.TableName()) < 1 {
		return 0, ErrTableNotFound
	}

	var (
		lIds     []string
		lNewVals map[string]interface{}
		lRefVals map[string]map[string]interface{}
		lNewTodo []string
	)

	//self._check_model()

	vals := self.itf_to_itfmap(src)
	//logger.Dbg("Write11", self.Statement.Ids, len(self.Statement.Ids), lValues)
	// 检查合法
	if len(vals) == 0 {
		res_err = fmt.Errorf("can't shupport this type of values")
		return
	}
	lNewVals = vals // #默认
	orm := self.orm

	// #获取Ids
	if len(self.Statement.IdParam) > 0 {
		lIds = self.Statement.IdParam
	} else if self.Statement.Domain.Count() > 0 {
		lIds = self._search("", nil)
	} else {
		orm.logger.Err("At least have one of where|domain|ids condition to locate for update")
		return
	}

	//logger.Dbg("_write ids", lIds)
	if len(lIds) == 0 {
		return
	}

	if self.IsClassic {
		//???
		for field, _ := range vals {
			var fobj IField
			fobj = self.model.FieldByName(field)
			if fobj == nil {
				lF := self.model.RelateFieldByName(field)
				if lF != nil {
					fobj = lF.RelateField
				}
			}

			if fobj == nil {
				continue
			}

			/*
						   groups = fobj.write
				            if groups:
				                edit = False
				                for group in groups:
				                    module = group.split(".")[0]
				                    grp = group.split(".")[1]
				                    cr.execute("select count(*) from res_groups_users_rel where gid IN (select res_id from ir_model_data where name='%s' and module='%s' and model='%s') and uid=%s" % \
				                               (grp, module, 'res.groups', user))
				                    readonly = cr.fetchall()
				                    if readonly[0][0] >= 1:
				                        edit = True
				                        break
				                    elif readonly[0][0] == 0:
				                        edit = False
				                    else:
				                        edit = False

				                if not edit:
				                    vals.pop(field)*/
		}

		//》》》》》》》》》》》》》》》》》
		/*
		   		result = self._store_get_values(cr, user, ids, vals.keys(), context) or []

		           # for recomputing new-style fields
		           recs = self.browse(cr, user, ids, context)
		           modified_fields = list(vals)
		           if self._log_access:
		               modified_fields += ['write_date', 'write_uid']
		           recs.modified(modified_fields)

		           parents_changed = []
		           parent_order = self._parent_order or self._order
		           if self._parent_sFieldtore and (self._parent_name in vals) and not context.get('defer_parent_store_computation'):
		               # The parent_left/right computation may take up to
		               # 5 seconds. No need to recompute the values if the
		               # parent is the same.
		               # Note: to respect parent_order, nodes must be processed in
		               # order, so ``parents_changed`` must be ordered properly.
		               parent_val = vals[self._parent_name]
		               if parent_val:
		                   query = "SELECT id FROM %s WHERE id IN %%s AND (%s != %%s OR %s IS NULL) ORDER BY %s" % \
		                                   (self._table, self._parent_name, self._parent_name, parent_order)
		                   cr.execute(query, (tuple(ids), parent_val))
		               else:
		                   query = "SELECT id FROM %s WHERE id IN %%s AND (%s IS NOT NULL) ORDER BY %s" % \
		                                   (self._table, self._parent_name, parent_order)
		                   cr.execute(query, (tuple(ids),))
		               parents_changed = map(operator.itemgetter(0), cr.fetchall())
		*/

		var (
		//		updates  = make(map[string]interface{})
		//	direct   = make([]string, 0)
		//	upd_todo = make([]string, 0)
		//	updend   = make([]string, 0)
		)
		//totranslate := false //context.get('lang') and context['lang'] != 'en_US'

		lNewVals, lRefVals, lNewTodo = self._separate_values(vals, self.Statement.Fields, self.Statement.NullableFields, false, true)

		/*
			for name, field := range self.model.GetFields() {
				if field == nil {
					continue
				}

				if field._is_created || field._is_updated {
					val, _ := self.orm._now_time(field._type)
					vals[name] = val
				}

				if val, has := vals[name]; has {
					// TODO 优化锁
					// #相同名称的字段分配给对应表
					lComm := self.model.CommonFieldByName(name)
					if lComm != nil {
						logger.Dbg("_creataa:", name, lComm)
						for tbl, _ := range lComm {
							// 确保添加字段为表
							if rel_vals[tbl] != nil {
								rel_vals[tbl][name] = val
							}
						}

						continue // 字段分配完毕
					}
					//utils.Dbg("write@:", key)
					//utils.Dbg("write@:", field.foreign_field)
					//utils.Dbg("write@:", self._relate_fields[key])
					//  ffield = self._fields.get(field)
					//  if ffield and ffield.deprecated:
					//      _logger.warning('Field %s.%s is deprecated: %s', self._name, field, ffield.deprecated)
					if rel_fld := self.model.RelateFieldByName(name); rel_fld == nil && !field.IsForeignField() {
						//utils.Dbg("write:", key)
						if len(field.Selection) == 0 && val != nil {
							self._check_selection_field_value(field, val) //context
						}

						if field.IsClassicWrite() && field.Fnct_inv() == nil {
							if totranslate && field.Translatable() {

							} else {
								updates[name] = field.SymbolFunc()(utils.Itf2Str(val))
							}
							direct = append(direct, name)
						} else {
							upd_todo = append(upd_todo, name)
						}
					} else {
						updend = append(updend, name)
					}
				}
			}


			for key, _ := range vals {
				field := self.model.FieldByName(key)
				if field == nil {
					continue
				}

				//utils.Dbg("write@:", key)
				//utils.Dbg("write@:", field.foreign_field)
				//utils.Dbg("write@:", self._relate_fields[key])
				//  ffield = self._fields.get(field)
				//  if ffield and ffield.deprecated:
				//      _logger.warning('Field %s.%s is deprecated: %s', self._name, field, ffield.deprecated)
				if rel_fld := self.model.RelateFieldByName(key); rel_fld == nil && !field.IsForeignField() {
					//utils.Dbg("write:", key)
					if len(field.Selection) == 0 && vals[field.Name] != nil {
						self._check_selection_field_value(field, vals[field.Name]) //context
					}

					if field.IsClassicWrite() && field.Fnct_inv() == nil {
						if totranslate && field.Translatable() {

						} else {
							updates[key] = field.SymbolFunc()(utils.Itf2Str(vals[key]))
						}
						direct = append(direct, key)
					} else {
						upd_todo = append(upd_todo, key)
					}
				} else {
					updend = append(updend, key)
				}
			}
		*/

	}
	//updates["create_id"] = UserId
	//updates["write_id"] = UserId
	//direct = append(direct, "write_id")
	//direct = append(direct, "write_date")
	//vals["create_date"] = //由ORM替代
	//vals["write_date"] =

	// 被设置默认值的字段赋值给Val
	//for k, v := range self._default {
	//	if updates[k] == nil {
	//		updates[k] = v
	//	}
	//}

	if len(lNewVals) > 0 {
		//#更新
		//self.check_access_rule(cr, user, ids, 'write', context=context)

		params := make([]interface{}, 0)
		values := ""

		// TODO 验证数据类型
		//self._validate(lNewVals)

		// 拼SQL
		for k, v := range lNewVals {
			if values != "" {
				values += ","
			}

			values += self.orm.Quote(k) + "=?"
			params = append(params, v)
		}

		if values == "" {
			return
		}
		/*
			if len(args) > 0 {
				isAloneSlice := false
				if len(args) == 1 {
					switch args[0].(type) {
					case []interface{}:
						params = append(params, args[0].([]interface{})...)
						isAloneSlice = true
					}
				}
				if !isAloneSlice {
					for _, v := range args {
						params = append(params, v)
					}
				}
			}
		*/
		var (
			lSql string
		)
		// sql := `UPDATE ` + self.model._table + ` SET ` + values + ` WHERE id IN (` + sub_ids + `)`
		lSql = fmt.Sprintf(`UPDATE "%s" SET %s WHERE %s IN (%s)`, self.Statement.TableName(), values, self.Statement.IdKey, strings.Join(lIds, ","))
		//logger.Dbg("create:", lSql)
		res, err := self.exec(lSql, params...)
		if err != nil {
			return 0, err
		}

		res_effect, res_err = res.RowsAffected()
		if res_err != nil {
			return
		}

		/*table_name := self.Statement.TableName()
		//lCacher := self.orm.cacher.RecCacher(self.Statement.TableName()) // for write
		//if lCacher != nil {
		for _, id := range lIds {
			if id != "" {
				//更新缓存
				//lKey := self._generate_caches_key(self.Statement.TableName(), id)
				lRec := NewRecordSet(nil, lNewVals)
				self.orm.cacher.PutById(table_name, id, lRec)
			}
		}*/
		//}

	}

	// 更新关联表
	for tbl, ref_vals := range lRefVals {
		lFldName := self.model._relations[tbl]

		nids := make([]string, 0)
		// for sub_ids in cr.split_for_in_conditions(ids):
		//     cr.execute('select distinct "'+col+'" from "'+self._table+'" ' \
		//               'where id IN %s', (sub_ids,))
		//    nids.extend([x[0] for x in cr.fetchall()])
		lSql := fmt.Sprintf(`SELECT distinct "%s" FROM "%s" WHERE %s IN(%s)`, lFldName, self.model.GetTableName(), self.Statement.IdKey, strings.Join(lIds, ","))
		lDs, err := self.orm.Query(lSql)
		if !logger.LogErr(err) {
			lDs.First()
			for !lDs.Eof() {
				nids = append(nids, lDs.FieldByName(lFldName).AsString())
				lDs.Next()
			}
		}

		if len(ref_vals) > 0 { //# 重新写入关联数据
			lMdlObj, err := self.orm.GetModel(tbl) // #i
			if err == nil {
				//lMdlObj.Write(nids, v) //TODO 检查是否真确使用 因为nids为空的话是创建而非更新
				lMdlObj.Records().Ids(nids...).Write(ref_vals) //TODO 检查是否真确使用
				//self.pool[table].write(cr, user, nids, v, context)
			} else {
				logger.LogErr(err)
			}
		}
	}

	// 更新计算字段
	for _, name := range lNewTodo {
		lField := self.model.FieldByName(name)
		if lField != nil {
			lField.OnConvertToWrite(
				&TFieldEventContext{
					Session: self,
					Model:   self.model,
					//Id:     lIds,
					Field: lField,
					Value: vals[name], //utils.IntToStr(vals[name]),
				})

			/*for _, id := range lIds {


					// result += self._columns[name].set(cr, self, id_new, name, vals[name], user, rel_context) or []
					//logger.Dbg("id:", lIds, id)
					//if ctrl, has := self.orm.field_ctrl[lField.Type]; has {
					//ctrl.Write(self, id, lField, utils.IntToStr(vals[name]), nil)
					//} else {
					lField.OnWrite(
						&TFieldEventContext{
							Session: self,
							Model:   self.model,
							Id:      utils.IntToStr(id),
							Field:   lField,
							Value:   vals[name], //utils.IntToStr(vals[name]),
						})
					//self._update_field(id, lField, utils.IntToStr(vals[name]), nil)
					//}

			}*/
		}
	}
	/*
		unknown_fields := make([]string, 0)
		for table, fld_name := range self.model._relations {
			//lFldName := self.model._relations[table]
			nids := make([]string, 0)
			// for sub_ids in cr.split_for_in_conditions(ids):
			//     cr.execute('select distinct "'+col+'" from "'+self._table+'" ' \
			//               'where id IN %s', (sub_ids,))
			//    nids.extend([x[0] for x in cr.fetchall()])
			lDs, err := self.orm.Query(`select distinct "%s" from "%s" where id IN(%s)`, fld_name, self.model.TableName(), strings.Join(lIds, ","))
			if !logger.LogErr(err) {
				lDs.First()
				for !lDs.Eof() {
					nids = append(nids, lDs.FieldByName(fld_name).AsString())
					lDs.Next()
				}
			}

			v := make(map[string]interface{})
			for _, fld_name := range updend {
				if rel_fld := self.model.RelateFieldByName(fld_name); rel_fld != nil && rel_fld.RelateTableName == table {
					v[fld_name] = vals[fld_name]
					//unknown_fields.remove(val) TODO
				}

			}

		}

		if len(unknown_fields) > 0 {
			logger.Logger.Err("No such field(s) in model %s: %s.", self.model._name, strings.Join(unknown_fields, ","))
		}
	*/

	return
}

func (self *TSession) Create(src interface{}, classic_create ...bool) (res_id int64, res_err error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	var classic bool
	if len(classic_create) > 0 {
		classic = classic_create[0]
		self.IsClassic = classic
	}

	return self._create(src, classic)
}

// TODO 接受多值
func (self *TSession) Write(src interface{}, classic_write ...bool) (res_effect int64, res_err error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	if len(classic_write) > 0 {
		self.IsClassic = classic_write[0]
	}
	return self._write(src, nil)
}

func (self *TSession) Read(classic_read ...bool) (res_dataset *TDataSet, res_err error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	var classic bool
	if len(classic_read) > 0 {
		classic = classic_read[0]
		self.IsClassic = classic
	}
	return self._read(classic)
}

func (self *TSession) _read(classic_read bool) (res_dataset *TDataSet, res_err error) {
	if len(self.Statement.TableName()) < 1 {
		return nil, ErrTableNotFound
	}

	//self._check_model()

	// TODO: check access rights 检查权限
	//	self.check_access_rights("read")
	//	fields = self._check_field_access_rights("read", fields, nil)

	self.Statement.Limit(0)

	//# split fields into stored and computed fields
	stored := make([]string, 0) // 可存于数据的字段
	inherited := make([]string, 0)
	computed := make([]string, 0) // 数据库没有的字段
	var lIds []string

	// 获取Ids
	if len(self.Statement.IdParam) > 0 {
		lIds = self.Statement.IdParam
	} else {
		lIds = self._search("", nil)
		if len(lIds) == 0 {
			res_dataset = NewDataSet()
			return
		}
	}

	//if self.IsClassic {
	// 验证Select * From
	if len(self.Statement.Fields) > 0 {
		for name, allowed := range self.Statement.Fields {
			if !allowed {
				continue
			}

			fld := self.model.FieldByName(name)
			if fld != nil && !fld.IsForeignField() { //如果是本Model的字段
				//utils.Dbg("stored:", name)
				stored = append(stored, name)
			} else if fld != nil {
				//utils.Dbg("computed:", name)
				computed = append(computed, name)

				//field := self.model.FieldByName(name)
				if fld.IsForeignField() { // and field.base_field.column:
					//utils.Dbg("inherited:", name)
					inherited = append(inherited, name)
				}
			} else {
				//_logger.warning("%s.read() with unknown field '%s'", self._name, name)
				logger.Logger.Warn(`%s.read() with unknown field '%s'`, self.model.GetModelName(), name)
			}

		}
	} else {
		for name, field := range self.model.GetFields() {
			if field != nil {
				if field.IsForeignField() {
					inherited = append(inherited, name)
				} else {
					stored = append(stored, name)
				}
			}
		}
	}
	//}

	//# fetch stored fields from the database to the cache
	res_dataset, _ = self._read_from_database(lIds, stored, inherited)

	// 处理那些数据库不存在的字段：company_ids...
	//# retrieve results from records; this takes values from the cache and
	// # computes remaining fields
	name_fields := make(map[string]IField)
	for _, name := range stored {
		fld := self.model.FieldByName(name)
		if fld != nil {
			name_fields[name] = fld
		}
	}
	for _, name := range computed {
		fld := self.model.FieldByName(name)
		if fld != nil {
			name_fields[name] = fld
		}
	}

	// 获取ManytoOne的Name
	//use_name_get := classic_read

	res_dataset.First()
	for !res_dataset.Eof() {
		rec_id := res_dataset.FieldByName("id").AsString()
		for name, field := range name_fields {
			/*			//if field.IsClassicWrite() {
						//if ctrl, has := self.orm.field_ctrl[field.Type]; has {
						//	ctrl.Read(self, field, res_ds, nil)
						//} else {
						field.Read(self, field, res_ds, nil)
						//}
						//}
			*/

			// 计算新值
			val := field.OnConvertToRead(&TFieldEventContext{
				Session: self,
				Model:   self.model,
				Field:   field,
				Id:      rec_id,
				Value:   res_dataset.FieldByName(name).AsInterface(),
				Dataset: res_dataset,
			})
			//logger.Dbg("convert_to_read:", name, val, res_dataset.Count(), rec_id, res_dataset.FieldByName("id").AsString(), res_dataset.Position, res_dataset.Eof(), res_dataset.FieldByName(name).AsString(), res_dataset.FieldByName(name).AsInterface(), field)

			res_dataset.FieldByName(name).AsInterface(val)
		}

		res_dataset.Next()
	}

	res_dataset.First() // 返回游标0
	res_dataset.classic = classic_read
	return
}

func (self *TSession) Count() (int, error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	self.Statement.IsCount = true

	lCount := self._search("", nil)
	return utils.StrToInt(lCount[0]), nil
}

// Sum sum the records by some column. bean's non-empty fields are conditions.
func (self *TSession) Sum(colName string) (float64, error) {
	/*	defer self.Statement.Init()
		if self.IsAutoClose {
			defer self.Close()
		}

		v := reflect.ValueOf(res)
		if v.Kind() != reflect.Ptr {
			return errors.New("need a pointer to a variable")
		}

		var isSlice = v.Elem().Kind() == reflect.Slice
		var sqlStr string
		var args []interface{}
		var err error
		if len(self.statement.RawSQL) == 0 {
			sqlStr, args, err = self.statement._generate_sum(columnNames...)
			if err != nil {
				return err
			}
		} else {
			sqlStr = self.statement.RawSQL
			args = self.statement.RawParams
		}

		session.queryPreprocess(&sqlStr, args...)

		if isSlice {
			if session.isAutoCommit {
				err = session.DB().QueryRow(sqlStr, args...).ScanSlice(res)
			} else {
				err = session.tx.QueryRow(sqlStr, args...).ScanSlice(res)
			}
		} else {
			if session.isAutoCommit {
				err = session.DB().QueryRow(sqlStr, args...).Scan(res)
			} else {
				err = session.tx.QueryRow(sqlStr, args...).Scan(res)
			}
		}

		if err == sql.ErrNoRows || err == nil {
			return nil
		}
	*/
	return 0, nil
}

// 删除
func (self *TSession) Delete(ids ...string) (res_effect int64, err error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	if len(self.Statement.TableName()) < 1 {
		return 0, ErrTableNotFound
	}
	//self._check_model()

	var lIds []string

	// 添加ID
	self.Statement.IdParam = append(self.Statement.IdParam, ids...)

	// 获取Ids
	if len(self.Statement.IdParam) > 0 {
		lIds = self.Statement.IdParam
	} else {
		lIds = self._search("", nil)
	}

	//#1 删除目标Model记录
	lSql := fmt.Sprintf(`DELETE FROM %s WHERE id in (%s); `, self.Statement.TableName(), strings.Join(lIds, ","))
	res, err := self.exec(lSql)
	if err != nil {
		fmt.Errorf(err.Error())
		return 0, err
	}

	if cnt, err := res.RowsAffected(); err != nil || (int(cnt) != len(lIds)) {
		self.Rollback()
		return 0, ErrDeleteFailed
	}

	table_name := self.Statement.TableName()
	//lCacher := self.orm.cacher.RecCacher(self.Statement.TableName()) // for del
	//if lCacher != nil {
	for _, id := range lIds {
		//lCacher.Remove(id)
		self.orm.cacher.RemoveById(table_name, id)
	}
	//}
	// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
	//lCacher = self.orm.cacher.SqlCacher(self.Statement.TableName()) // for del
	//lCacher.Clear()
	self.orm.cacher.ClearByTable(self.Statement.TableName())

	return res.RowsAffected()
}

// CreateTable create a table according a bean
// TODO考虑添加参数 达到INHERITS
func (self *TSession) CreateTable(model string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	var lSql string
	tableName := strings.Replace(model, ".", "_", -1)
	tableName = utils.SnakeCasedName(tableName)
	//fmt.Println("CreateTable", tableName, self.orm.GetTable(tableName))
	self.Statement.Table = self.orm.GetTable(tableName)
	/*未完成
	for _,tb_name:=range self.model.Inherits(){
		tb:=self.orm.GetTable(tb_name)
		if tb!=nil{
			// 匹对Col
			for _,col:=range tb.Columns(){
				self.Statement.Table.
			}
		}
	}


	if len(self.model.Inherits())>0{
		var tb:=self.Statement.Table


	}	*/
	lSql = self.Statement._generate_create_table()

	if self.IsClassic {
		// 实现PG的继承
		model_name := strings.Replace(model, "_", ".", -1)
		model_name = utils.DotCasedName(model_name)
		lModel := self.orm.models[model_name] // TODO 并发
		if lModel != nil {
			//lInherits := lTable.Inherits
			if len(lModel._inherits) > 0 && strings.EqualFold(self.orm.dialect.DriverName(), "postgres") {
				lSql += "INHERITS  ( "
				lSql += strings.Join(lModel._inherits, ",")
				lSql += " ) "
			}
		}
	}

	//logger.Dbg("createOneTable", lModel, lSql)
	res, err := self.exec(lSql)
	if err != nil {
		return err
	}
	// 更新Model表
	if cnt, err := res.RowsAffected(); err == nil && cnt > 0 {
		self.Orm().reverse()
	}
	return err
}

// CreateUniques create uniques
func (self *TSession) CreateUniques(model string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	tableName := strings.Replace(model, ".", "_", -1)
	tableName = utils.SnakeCasedName(tableName)
	self.Statement.Table = self.orm.GetTable(tableName)

	lSqls := self.Statement._generate_unique()
	//logger.Dbg("CreateUniques", lSqls)
	for _, sql := range lSqls {
		_, err := self.exec(sql)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateIndexes create indexes
func (self *TSession) CreateIndexes(model string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	tableName := strings.Replace(model, ".", "_", -1)
	tableName = utils.SnakeCasedName(tableName)
	self.Statement.Table = self.orm.GetTable(tableName)

	lSqls := self.Statement._generate_index()
	for _, sql := range lSqls {
		_, err := self.exec(sql)
		if err != nil {
			return err
		}
	}
	return nil
}

// drop table will drop table if exist, if drop failed, it will return error
func (self *TSession) DropTable(model string) (err error) {
	var needDrop = true
	/*if !session.Engine.dialect.SupportDropIfExists() {
		sqlStr, args := session.Engine.dialect.TableCheckSql(tableName)
		results, err := session.query(sqlStr, args...)
		if err != nil {
			return err
		}
		needDrop = len(results) > 0
	}
	*/
	if needDrop {
		tableName := strings.Replace(model, ".", "_", -1)
		tableName = utils.SnakeCasedName(tableName)
		sqlStr := self.orm.dialect.DropTableSql(tableName)
		//logger.Dbg("DropTable", sqlStr)
		res, err := self.exec(sqlStr)
		if err != nil {
			return err
		}

		if cnt, err := res.RowsAffected(); err == nil && cnt > 0 {
			model_name := strings.Replace(model, "_", ".", -1)
			model := self.Orm().models[model_name]
			if model.is_base { // 只移除Table生成的Model
				delete(self.Orm().models, model_name)
			}
		}
		return err
	}

	return
}

func (self *TSession) add_column(colName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	col := self.Statement.Table.GetColumn(colName)
	//logger.Dbg("add_column", self.Statement.Table.Type, colName, col)
	sql, args := self.Statement._generate_add_column(col)
	_, err := self.exec(sql, args...)
	return err
}

func (self *TSession) add_index(tableName, idxName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}
	index := self.Statement.Table.Indexes[idxName]
	sqlStr := self.orm.dialect.CreateIndexSql(tableName, index)

	_, err := self.exec(sqlStr)
	return err
}

func (self *TSession) add_unique(tableName, uqeName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}
	index := self.Statement.Table.Indexes[uqeName]
	sqlStr := self.orm.dialect.CreateIndexSql(tableName, index)
	_, err := self.exec(sqlStr)
	return err
}

// 作废 更新单一字段
func (self *TSession) _update_field(id interface{}, field *TField, value string, rel_context map[string]interface{}) {
	self.exec(fmt.Sprintf("UPDATE "+self.model.GetTableName()+" SET "+field.Name()+"="+field.SymbolChar()+" WHERE id=%v", field.SymbolFunc()(value), id))
}

// Check whether value is among the valid values for the given
//    selection/reference field, and raise an exception if not.
func (self *TSession) _check_selection_field_value(field IField, value interface{}) {
	//   field = self._fields[field]
	// field.convert_to_cache(value, self)
}

func (self *TSession) _check_model() bool {
	if self.model == nil {
		panic("Must point out a Model for continue")
		return false
	}

	return true
}

// #search and return ids only
func (self *TSession) Search() (res_ids []string) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}
	return self._search("", nil)
}

// 查询所有符合条件的主键/索引值
// :param access_rights_uid: optional user ID to use when checking access rights
// (not for ir.rules, this is only for ir.model.access)
func (self *TSession) _search(access_rights_uid string, context map[string]interface{}) (res_ids []string) {
	var (
		//fields_str string
		//where_str    string
		limit_str    string
		offset_str   string
		from_clause  string
		where_clause string
		query_str    string
		order_by     string
		//		err        error
		where_clause_params []interface{}
		query               *TQuery
		//		lDomain             *utils.TStringList
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
	/*	废弃
		if self.Statement.Domain.Count() > 0 {
				lDomain = self.Statement.Domain //!!!!!!!!!!!!!! utils.Query2StringList(self.Statement.Domain)
				//logger.Dbg("_search", lDomain)
			} else if self.Statement.WhereClause != "" {
				lDomain = Sql2Domain(self.Statement.WhereClause)
			}
	*/
	logger.Dbg("_search", self.Statement.Domain, StringList2Domain(self.Statement.Domain))
	query = self.Statement._where_calc(self.Statement.Domain, false, context)
	order_by = self.Statement._generate_order_by(query, context) // TODO 未完成
	from_clause, where_clause, where_clause_params = query.get_sql()
	logger.Dbg("from_clause", from_clause)
	logger.Dbg("where_clause", where_clause)
	logger.Dbg("where_clause_params", where_clause_params)
	//} else {
	//	from_clause = self.Statement.TableName()
	//	//where_clause, where_clause_params = self.Statement.WhereClause, self.Statement.Params //self.Statement._generate_query(context, true, true, false, true, true, true, true, nil)
	//	where_clause, where_clause_params = self.Statement.WhereClause, self.Statement.Params //self.Statement._generate_query(context, true, true, false, true, true, true, true, nil)
	//}

	if where_clause != "" {
		where_clause = fmt.Sprintf(` WHERE %s`, where_clause)
	}

	table_name := self.Statement.TableName()
	if self.Statement.IsCount {
		// Ignore order, limit and offset when just counting, they don't make sense and could
		// hurt performance
		query_str = `SELECT count(1) FROM ` + from_clause + where_clause

		res_ids := self.orm.cacher.GetBySql(table_name, query_str, where_clause_params)
		if len(res_ids) < 1 {
			lRes, err := self.query(query_str, where_clause_params...)
			if logger.LogErr(err) {
				return []string{"0"}
			}
			res_ids = []string{lRes.FieldByName("count").AsString()}

			// #存入缓存
			self.orm.cacher.PutBySql(table_name, query_str, where_clause_params, res_ids...)
		}

		return res_ids
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

	query_str = fmt.Sprintf(`SELECT %s.%s FROM `, self.orm.Quote(self.Statement.TableName()), self.orm.Quote(self.Statement.IdKey)) + from_clause + where_clause + order_by + limit_str + offset_str
	//	web.Debug("_search", query_str, where_clause_params)

	// #调用缓存

	res_ids = self.orm.cacher.GetBySql(table_name, query_str, where_clause_params)
	if len(res_ids) < 1 {
		res, err := self.query(query_str, where_clause_params...)
		if logger.LogErr(err) {
			return nil
		}
		res_ids = res.Keys(self.Statement.IdKey)
		//logger.Dbg("_search", res.KeyField, res.Count(), res.Keys(), res_ids)

		self.orm.cacher.PutBySql(table_name, query_str, where_clause_params, res_ids...)
	}

	return
}

//# ids_less 缺少的ID
func (self *TSession) _read_from_cache(ids []string) (res []*TRecordSet, ids_less []string) {
	res, ids_less = self.orm.cacher.GetByIds(self.Statement.TableName(), ids...)
	return
}

/*
   """ Read the given fields of the records in ``self`` from the database,
       and store them in cache. Access errors are also stored in cache.

       :param field_names: list of column names of model ``self``; all those
           fields are guaranteed to be read
       :param inherited_field_names: list of column names from parent
           models; some of those fields may not be read
   """
*/
// 从数据库读取记录并保存到缓存中
// :param field_names: Model的所有字段
// :param inherited_field_names:关联父表的所有字段
func (self *TSession) _read_from_database(ids []string, field_names, inherited_field_names []string) (res_ds *TDataSet, res_sql string) {
	// # 从缓存里获得数据
	records, less_ids := self._read_from_cache(ids)

	// # 补缺缓存没有的数据
	if len(less_ids) > 0 {
		table_name := self.Statement.TableName()
		//# make a query object for selecting ids, and apply security rules to it
		//占位符先提供固定预置值，后面根据值获得Idx并修改值到正确实时值
		//param_ids := "idholder" //占位符先提供固定预置值，后面根据值获得Idx并修改值到正确实时值
		//query(['"%s"' % self._table], ['"%s".id IN %%s' % self._table], [param_ids])
		//query := NewQuery([]string{self.model.TableName()},
		//	[]string{fmt.Sprintf(`"%s".id IN (?)`, self.model.TableName())},
		//	[]interface{}{param_ids}, nil, nil) //占位符先提供固定预置值，后面根据值获得Idx并修改值到正确实时值                                                                                  // object()
		query := NewQuery([]string{table_name},
			[]string{fmt.Sprintf(`%s.%s IN (%s)`,
				self.orm.Quote(table_name),
				self.orm.Quote(self.Statement.IdKey), strings.Join(ids, `,`))},
			[]interface{}{}, nil, nil)
		//self._apply_ir_rules(query, 'read')
		order_str := self.Statement._generate_order_by(query, nil)

		//qual_names = map(qualify, set(fields_pre + [self._fields['id']]))
		qual_names := make([]string, 0)

		if self.IsClassic {
			//对可迭代函数'iterable'中的每一个元素应用‘function’方法，将结果作为list返回
			//# determine the fields that are stored as columns in tables;
			fields := make([]IField, 0)
			fields_pre := make([]IField, 0)
			for _, name := range field_names {
				//logger.Dbg("XXXXX1", name)
				if f := self.model.FieldByName(name); f != nil {
					fields = append(fields, f)
				}
			}

			for _, name := range inherited_field_names {
				//logger.Dbg("XXXXX2", name)
				if f := self.model.FieldByName(name); f != nil {
					fields = append(fields, f)
				}
			}

			//	当字段为field.base_field.column.translate可调用即是translate为回调函数而非Bool值时不加入Join
			for _, fld := range fields {
				//if fld.IsClassicRead() && !(fld.IsForeignField() && false) { //用false代替callable(field.base_field.column.translate)
				if fld.Store() && fld.ColumnType() != "" && !(fld.IsForeignField() && false) { //用false代替callable(field.base_field.column.translate)
					//logger.Dbg("PRE:", fld.Name)
					fields_pre = append(fields_pre, fld)
				}
			}

			for _, f := range fields_pre {
				qual_names = append(qual_names, self.qualify(f, query))
			}

		} else {
			qual_names = self.Statement._generate_fields()
		}

		// # determine the actual query to execute
		from_clause, where_clause, params := query.get_sql()
		//logger.Dbg("from_clause ", from_clause)
		//logger.Dbg("where_clause ", where_clause)
		//logger.Dbg("params ", params)
		res_sql = fmt.Sprintf(`SELECT %s FROM %s WHERE %s %s`,
			strings.Join(qual_names, ","),
			from_clause,
			where_clause,
			order_str,
		)
		//logger.Dbg("read sql", res_sql)

		//res_ds:=NewDataSet()
		var err error
		// 获得Id占位符索引
		///lIdx := utils.IdxOfItfs("idholder", params...)
		//for _, sub_ids := range ids {
		///params[lIdx] = strings.Join(ids, ",")
		// tuple(sub_ids)
		res_ds, err = self.Query(res_sql, params...) //cr.execute(res_sql, params)
		if err != nil {
			logger.Logger.Err(err.Error())
		}

		// # 报告错误记录
		if res_ds.Count() != len(less_ids) {
			//# if not you need
			logger.Logger.Err(`query result including %v records are not what you expectd! %v`, res_ds.Count(), len(less_ids))

		}

		// TODO 带优化或者简去
		//if !dataset.SetKeyField(self.Statement.IdKey) {
		//	logger.Logger.Err(`set key_field fail when call RecordByKey(key_field:%v)!`, res_ds.KeyField)
		//}

		for !res_ds.Eof() {
			rec := res_ds.Record()
			// # 添加进入缓存
			self.orm.cacher.PutById(table_name, rec.GetByName(self.Statement.IdKey).AsString(), rec)
			res_ds.Next()
		}

		//# 必须是合法位置上
		res_ds.First()

		// #添加进入数据集
		res_ds.AppendRecord(records...)
		/*
			for _, id := range less_ids {
				rec := dataset.RecordByKey(id)

				// # 报告缺失记录
				if rec == nil {
					logger.Logger.Err(`query result didn't including record (%v)!`, id)
				}

				// #添加进入数据集
				err = res_ds.AppendRecord(rec)
				if err != nil {
					logger.Logger.Err(err.Error())
				}

				// # 添加进入缓存
				self.orm.cacher.PutById(table_name, id, rec)
			}
		*/
	} else { // # init dataset
		res_ds = NewDataSet()
		//res_ds.KeyField = self.Statement.IdKey //#重要配Key置
		//res_ds.SetKeyField(self.Statement.IdKey)//# 废除因为无效果
		res_ds.AppendRecord(records...)
	}

	if res_ds != nil {
		ids = res_ds.Keys(self.Statement.IdKey)
	}

	// TODO:BELOW 下面进行而外的数据格式化和补充 可部分ORM里实现180180
	if len(ids) > 0 {
		/*   # translate the fields if necessary
		     if context.get('lang'):
		         for field in fields_pre:
		             if not field.inherited and callable(field.column.translate):
		                 f = field.name
		                 translate = field.get_trans_func(fetched)
		                 for vals in res_ds:
		                     vals[f] = translate(vals['id'], vals[f])


		*/

		// 激活[字段原始数据]Ready事件
		var field IField
		for _, name := range field_names {
			field = self.model.FieldByName(name)
			field.OnRead(&TFieldEventContext{
				Session: self,
				Model:   self.model,
				Field:   field,
				Dataset: res_ds})
		}

	}
	/*


	   # Warn about deprecated fields now that fields_pre and fields_post are computed
	   for f in field_names:
	       column = self._columns[f]
	       if column.deprecated:
	           _logger.warning('Field %s.%s is deprecated: %s', self._name, f, column.deprecated)

	   # store result in cache
	   for vals in result:
	       record = self.browse(vals.pop('id'))
	       record._cache.update(record._convert_to_cache(vals, validate=False))

	   # store failed values in cache for the records that could not be read
	   missing = self - fetched
	   if missing:
	       extras = fetched - self
	       if extras:
	           raise AccessError(
	               _("Database fetch misses ids ({}) and has extra ids ({}), may be caused by a type incoherence in a previous request").format(
	                   ', '.join(map(repr, missing._ids)),
	                   ', '.join(map(repr, extras._ids)),
	               ))
	       # mark non-existing records in missing
	       forbidden = missing.exists()
	       if forbidden:
	           # store an access error exception in existing records
	           exc = AccessError(
	               _('The requested operation cannot be completed due to security restrictions. Please contact your system administrator.\n\n(Document type: %s, Operation: %s)') % \
	               (self._name, 'read')
	           )
	           forbidden._cache.update(FailedValue(exc))
	*/
	return
}

func (self *TSession) struct_to_itfmap(src interface{}) (res_map map[string]interface{}) {
	var (
		lField           reflect.StructField
		lFieldType       reflect.Type
		lFieldValue      reflect.Value
		lIsRequiredField bool
		lCol             *core.Column

		lName  string
		lValue interface{} //

	)

	res_map = make(map[string]interface{})
	v := reflect.ValueOf(src)

	// if pointer get the underlying element≤
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		panic("not struct")
	}

	lType := v.Type()

	lToOmitFields := len(self.Statement.Fields) > 0
	//	lOmitFields := make([]string, 0) // 有效字段
	for i := 0; i < lType.NumField(); i++ {
		lField = lType.Field(i)
		lName = utils.SnakeCasedName(lField.Name)

		lIsRequiredField = true
		if lToOmitFields {
			// 强制过滤已经设定的字段是否作为Query使用
			if b, ok := self.Statement.Fields[lName]; ok {
				if !b {
					continue
				}
			}
		}

		lFieldType = lField.Type
		lFieldValue = v.FieldByName(lField.Name)

		var (

		//			IsStruct bool
		// lFinalVal interface{}
		)

		// we can't access the value of unexported fields
		if lField.PkgPath != "" {
			continue
		}

		// don't check if it's omitted
		var tag string
		if tag = lField.Tag.Get(self.orm.FieldIdentifier); tag == "-" {
			continue
		}

		lTags := splitTag(tag)
		for _, tag := range lTags {
			lTag := parseTag(tag)
			switch strings.ToLower(lTag[0]) {
			case "name":
				if len(lTag) > 1 {
					lName = utils.SnakeCasedName(lTag[1])
				}
			case "extends", "relate":
				//				IsStruct = true
				if (lFieldValue.Kind() == reflect.Ptr && lFieldValue.Elem().Kind() == reflect.Struct) ||
					lFieldValue.Kind() == reflect.Struct {
					m := self.struct_to_itfmap(lFieldValue.Interface())

					for col, val := range m {
						res_map[col] = val
					}
				}
			}
		}

		// 字段必须在数据库里
		if fld := self.model.FieldByName(lName); fld == nil {
			continue
		} else {
			lCol = fld.Base().column
			//废弃
			//if lCol == nil {
			//	continue
			//}
		}

		//logger.Dbg("field#", lName, lFieldType, lFieldValue)
		switch lFieldType.Kind() {
		case reflect.Struct:

			if lFieldType.ConvertibleTo(core.TimeType) {
				t := lFieldValue.Convert(core.TimeType).Interface().(time.Time)
				if !lIsRequiredField && (t.IsZero() || !lFieldValue.IsValid()) {
					continue
				}
				lValue = self.orm.FormatTime(lCol.SQLType.Name, t)
			} else {
				if lCol.SQLType.IsJson() {
					if lCol.SQLType.IsText() {
						bytes, err := json.Marshal(lFieldValue.Interface())
						if err != nil {
							logger.Logger.ErrLn("IsJson", err)
							continue
						}
						lValue = string(bytes)
					} else if lCol.SQLType.IsBlob() {
						var bytes []byte
						var err error
						bytes, err = json.Marshal(lFieldValue.Interface())
						if err != nil {
							logger.Logger.ErrLn("IsBlob", err)
							continue
						}
						lValue = bytes
					}
				} else {
					// any other
					logger.Logger.Err("other field type ", lName)
				}
			}
		}
		//logger.Dbg("field#2", lName, lFieldType, lFieldValue)
		lValue = lFieldValue.Interface()
		res_map[lName] = lValue
	}

	return
}

// # transfer struct to Itf map and record model name if could
// #1 限制字段使用 2.添加Model

func (self *TSession) itf_to_itfmap(src interface{}) (res_map map[string]interface{}) {
	// 创建 Map
	lSrcType := reflect.TypeOf(src)

	if lSrcType.Kind() == reflect.Ptr || lSrcType.Kind() == reflect.Struct {
		//logger.Dbg("itf_to_itfmap", lSrcType.Kind(), self.model.GetModelName())
		//res_map = utils.Map(src)

		// # change model of the session
		if self.model == nil {
			lModelName := utils.DotCasedName(utils.Obj2Name(src))
			//logger.Dbg("itf_to_itfmap lModelName", lModelName)
			if lModelName != "" {
				self.Model(lModelName)
			}
		}

		res_map = self.struct_to_itfmap(src)

	} else if lSrcType.Kind() == reflect.Map {
		if m, ok := src.(map[string]interface{}); ok {
			res_map = m
		} else if m, ok := src.(map[string]string); ok {
			res_map = make(map[string]interface{})
			for key, val := range m {
				res_map[key] = val // 格式化为字段类型
			}
		}
	}

	return
}

// 获取字段值 for m2m,selection,
// return :map[string]interface{} 可以是map[id]map[field]vals,map[string]map[xxx][]string,
func (self *TSession) __field_value_get(ids []string, fields []*TField, values *TDataSet, context map[string]interface{}) (result map[string]map[string]interface{}) {
	lField := fields[0]
	switch lField.Type() {
	case "one2many":
		//if self._context:
		//    context = dict(context or {})
		//    context.update(self._context)

		//# retrieve the records in the comodel
		comodel, err := self.orm.osv.GetModel(lField.RelateModelName()) //obj.pool[self._obj].browse(cr, user, [], context)
		if err != nil {
		}
		inverse := lField.RelateFieldName()
		//domain = self._domain(obj) if callable(self._domain) else self._domain
		// domain = domain + [(inverse, 'in', ids)]
		domain := fmt.Sprintf(`[('%s', 'in', [%s])]`, inverse, strings.Join(ids, ","))
		//records_ids := comodel.Search(domain, 0, 0, "", false, nil)
		lDs, _ := comodel.Records().Domain(domain).Read()
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

//# the query may involve several tables: we need fully-qualified names
func (self *TSession) qualify(field IField, query *TQuery) string {
	res := self.Statement._inherits_join_calc(self.Statement.TableName(), field.Name(), query)
	/*
		if field.Type == "binary" { // && (context.get('bin_size') or context.get('bin_size_' + col)):
			//# PG 9.2 introduces conflicting pg_size_pretty(numeric) -> need ::cast
			res = fmt.Sprintf(`pg_size_pretty(length(%s)::bigint)`, res)
		}*/
	//utils.Dbg("qualify:", field.Name)
	return fmt.Sprintf(`%s as "%s"`, res, field.Name())
}

// NoCache ask this session do not retrieve data from cache system and
// get data from database directly.
func (self *TSession) Direct() *TSession {

	return self
}

// NoCascade indicate that no cascade load child object
func (self *TSession) NoCascade() *TSession {
	self.Statement.UseCascade = false
	return self
}

// ForUpdate Set Read/Write locking for UPDATE
func (self *TSession) ForUpdate() *TSession {
	self.Statement.IsForUpdate = true
	return self
}

// LastSQL returns last query information
func (self *TSession) LastSQL() (string, []interface{}) {
	return self.lastSQL, self.lastSQLArgs
}

// Join join_operator should be one of INNER, LEFT OUTER, CROSS etc - this will be prepended to JOIN
func (self *TSession) Join(joinOperator string, tablename interface{}, condition string, args ...interface{}) *TSession {
	self.Statement.Join(joinOperator, tablename, condition, args...)
	return self
}