package orm

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

type (
	TSession struct {
		orm *TOrm
		db  *sql.DB
		tx  *sql.Tx // 由Begin 传递而来

		Statement              TStatement
		IsDeprecated           bool // sometime the session did not reach the request it shoud be deprecated
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

func (self *TSession) init() error {
	self.Statement.Init()
	self.Statement.session = self
	self.IsAutoCommit = true // 默认情况下单个SQL是不用事务自动
	self.IsAutoClose = false
	self.AutoResetStatement = true
	self.IsCommitedOrRollbacked = false
	self.Prepared = false
	return nil
}

// Close release the connection from pool
func (self *TSession) Close() {
	if self.db != nil {
		// When Close be called, if session is a transaction and do not call
		// Commit or Rollback, then call Rollback.
		if self.tx != nil && !self.IsCommitedOrRollbacked {
			self.Rollback()
		}

		self.db = nil
		self.tx = nil
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

func (self *TSession) Commit() error {
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		if err := self.tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

// Rollback when using transaction, you can rollback if any error
func (self *TSession) Rollback() error {
	if !self.IsAutoCommit && !self.IsCommitedOrRollbacked {
		//session.saveLastSQL(session.Engine.dialect.RollBackStr())
		self.IsCommitedOrRollbacked = true
		return self.tx.Rollback()
	}
	return nil
}

// Query a raw sql and return records as dataset
func (self *TSession) Query(sql string, paramStr ...interface{}) (*dataset.TDataSet, error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	return self.query(sql, paramStr...)
}

func (self *TSession) query(sql string, paramStr ...interface{}) (*dataset.TDataSet, error) {
	for _, filter := range self.orm.dialect.Fmter() {
		sql = filter.Do(sql, self.orm.dialect, self.Statement.model)
	}

	return self.orm.log_query_sql(sql, paramStr, func() (*dataset.TDataSet, error) {
		if self.IsAutoCommit {
			return self.queryWithOrg(sql, paramStr...)
		}
		return self.queryWithTx(sql, paramStr...)

	})
}

func (self *TSession) doPrepare(sql string) (*sql.Stmt, error) {
	stmt, err := self.db.Prepare(sql)
	if err != nil {
		return nil, err
	}

	return stmt, err
}

// scan data to a slice's pointer, slice's length should equal to columns' number
func (self *TSession) scanRows(rows *sql.Rows) (res_dataset *dataset.TDataSet, err error) {
	// #无论如何都会返回一个Dataset
	res_dataset = dataset.NewDataSet()
	// #提供必要的IdKey/
	if self.Statement.IdKey != "" {
		res_dataset.KeyField = self.Statement.IdKey //设置主键
	}

	if rows != nil {
		cols, err := rows.Columns()
		if err != nil {
			return res_dataset, err
		}

		length := len(cols)
		vals := make([]interface{}, length)

		var value interface{}
		var field IField
		defer rows.Close()
		for rows.Next() {
			// TODO 优化不使用MAP
			rec := dataset.NewRecordSet()
			rec.Fields(cols...)

			// 创建数据容器
			for idx, _ := range cols {
				vals[idx] = reflect.New(ITF_TYPE).Interface()
			}

			// 采集数据
			err = rows.Scan(vals...)
			if err != nil {
				return res_dataset, err
			}

			// 存储到数据集
			for idx, name := range cols {
				// !NOTE! 转换数据类型输出
				if self.Statement.model != nil { // TODO exec,query 的SQL不包含Model
					field = self.Statement.model.GetFieldByName(name)
					if field != nil {
						value = field.onConvertToRead(self, reflect.ValueOf(vals[idx]).Elem().Interface())
					}
				}

				if !rec.SetByName(name, value, false) {
					return nil, fmt.Errorf("add %s value to recordset fail.", name)
				}
			}

			res_dataset.AppendRecord(rec)
		}
	}

	res_dataset.First()
	return res_dataset, nil
}

func (self *TSession) queryWithOrg(sql_str string, args ...interface{}) (res_dataset *dataset.TDataSet, res_err error) {
	var (
		rows *sql.Rows
		stmt *sql.Stmt
	)

	if self.Prepared {
		stmt, res_err = self.doPrepare(sql_str)
		if res_err != nil {
			return
		}
		rows, res_err = stmt.Query(args...)
		if res_err != nil {
			return
		}
	} else {
		rows, res_err = self.db.Query(sql_str, args...)
		if res_err != nil {
			return
		}
	}

	return self.scanRows(rows)
}

func (self *TSession) queryWithTx(query string, params ...interface{}) (res_dataset *dataset.TDataSet, res_err error) {
	var rows *sql.Rows

	rows, res_err = self.tx.QueryContext(context.Background(), query, params...)
	if res_err != nil {
		return
	}

	return self.scanRows(rows)
}

// Exec raw sql
func (self *TSession) Exec(sql_str string, args ...interface{}) (sql.Result, error) {
	//defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	return self.exec(sql_str, args...)
}

// Exec raw sql
func (self *TSession) exec(sql_str string, args ...interface{}) (sql.Result, error) {
	for _, filter := range self.orm.dialect.Fmter() {
		sql_str = filter.Do(sql_str, self.orm.dialect, self.Statement.model)
	}

	return self.orm.log_exec_sql(sql_str, args, func() (sql.Result, error) {
		if self.IsAutoCommit {
			// FIXME: oci8 can not auto commit (github.com/mattn/go-oci8)
			if self.orm.dialect.DBType() == ORACLE {
				self.Begin()
				r, err := self.tx.Exec(sql_str, args...)
				self.Commit()
				return r, err
			}
			return self.execWithOrg(sql_str, args...)
		}
		return self.execWithTx(sql_str, args...)
	})

}

// Execute sql
func (self *TSession) execWithOrg(query string, args ...interface{}) (res sql.Result, err error) {
	if self.Prepared {
		var stmt *sql.Stmt

		stmt, err = self.doPrepare(query)
		if err != nil {
			return
		}

		res, err = stmt.Exec(args...)
		if err != nil {
			return
		}
		return
	}

	return self.db.Exec(query, args...)
}

func (self *TSession) execWithTx(sql string, args ...interface{}) (sql.Result, error) {
	return self.tx.Exec(sql, args...)

}

// synchronize structs to database tables
func (self *TSession) SyncModel(region string, models ...interface{}) (modelNames []string, err error) {
	exits_models, err := self.orm.DBMetas() // 获取基本数据库信息
	if err != nil {
		return nil, err
	}

	modelNames = make([]string, 0)
	for _, mod := range models {
		model := self.orm.mapping(region, mod)
		model_name := model.GetName()

		var exits_model IModel // 数据库存在的
		for _, md := range exits_models {
			if strings.ToLower(md.GetName()) == strings.ToLower(model_name) {
				exits_model = md
				break
			}
		}

		// #设置该Session的Model/Table
		self.Model(model_name, region)

		if exits_model == nil {
			// 如果数据库不存在改Model对应的表则创建
			//err = self.StoreEngine(s.Statement.StoreEngine).CreateTable(bean)
			err = self.CreateTable(model_name)
			if err != nil {
				return modelNames, err
			}

			err = self.CreateUniques(model_name)
			if err != nil {
				return modelNames, err
			}

			err = self.CreateIndexes(model_name)
			if err != nil {
				return modelNames, err
			}
		} else {
			err := self.alterTable(model, exits_model.(*TModel))
			if err != nil {
				return modelNames, err
			}
		}

		modelNames = append(modelNames, model_name)
	}

	return modelNames, nil
}

/* #
* @model:提供新Session
* @newModel:Model映射后的新表结构
* @oldModel:当前数据库的表结构
 */
func (self *TSession) alterTable(newModel, oldModel *TModel) (err error) {
	orm := self.orm
	lTableName := newModel.GetName()

	var cur_field IField
	for _, field := range newModel.GetFields() {
		cur_field = nil

		for _, col2 := range oldModel.GetFields() {
			if strings.ToLower(field.Name()) == strings.ToLower(col2.Name()) {
				cur_field = col2
				break
			}
		}

		if cur_field != nil {
			expectedType := orm.dialect.GenSqlType(field)
			curType := orm.dialect.GenSqlType(cur_field)
			if expectedType != curType {
				// 修改数据类型
				// 如果是修改字符串到
				if expectedType == Text && strings.HasPrefix(curType, Varchar) {
					// currently only support mysql & postgres
					//if orm.dialect.DBType() == MYSQL ||
					//	orm.dialect.DBType() == POSTGRES {
					logger.Infof("Table %s column %s change type from %s to %s\n", lTableName, field.Name(), curType, expectedType)
					_, err = orm.Exec(orm.dialect.ModifyColumnSql(lTableName, field))
					//} else {
					//	logger.Warnf("Table %s column %s db type is %s, struct type is %s\n", lTableName, field.Name(), curType, expectedType)
					//}

					// 如果是同是字符串 则检查长度变化 for mysql
				} else if strings.HasPrefix(curType, Varchar) && strings.HasPrefix(expectedType, Varchar) {
					//if orm.dialect.DBType() == MYSQL {
					if cur_field.Size() < field.Size() {
						logger.Infof("Table %s column %s change type from varchar(%d) to varchar(%d)\n", lTableName, field.Name(), cur_field.Size(), field.Size())
						_, err = orm.Exec(orm.dialect.ModifyColumnSql(lTableName, field))
					}
					//}
					//其他
				} else {
					if !(strings.HasPrefix(curType, expectedType) && curType[len(expectedType)] == '(') {
						logger.Warnf("Table %s column %s db type is %s, struct type is %s", lTableName, field.Name(), curType, expectedType)
					}
				}
				// 如果是同是字符串 则检查长度变化 for mysql
			} else if expectedType == Varchar {
				//if orm.dialect.DBType() == MYSQL {
				if cur_field.Size() < field.Size() {
					logger.Infof("Table %s column %s change type from varchar(%d) to varchar(%d)\n",
						lTableName, field.Name(), cur_field.Size(), field.Size())
					_, err = orm.Exec(orm.dialect.ModifyColumnSql(lTableName, field))
				}
				//}
			}

			//
			if field.Default() != cur_field.Default() {
				logger.Warnf("Table %s Column %s db default is %s, struct default is %s",
					lTableName, field.Name(), cur_field.Default(), field.Default())
			}

			if field.Required() != cur_field.Required() {
				logger.Warnf("Table %s Column %s db required is %v, struct required is %v",
					lTableName, field.Name(), cur_field.Required(), field.Required())
			}

			// 如果现在表无该字段则添加
		} else {
			lSession := orm.NewSession()
			lSession.Model(newModel.GetName())
			//TODO # 修正上面指向错误Model
			lSession.Statement.model = newModel
			err = lSession.addColumn(field.Name())
		}

		if err != nil {
			return err
		}
	}

	var foundIndexNames = make(map[string]bool)
	var addedNames = make(map[string]*TIndex)

	// 检查更新索引 先取消索引载添加需要的
	// 取消Idex
	for name, index := range newModel.GetIndexes() {
		var cur_index *TIndex
		for name2, index2 := range oldModel.GetIndexes() {
			if index.Equal(index2) {
				cur_index = index2
				foundIndexNames[name2] = true
				break
			}
		}

		// 现有的idex
		if cur_index != nil {
			if cur_index.Type != index.Type { // 类型不同则清除
				sql := orm.dialect.DropIndexSql(lTableName, cur_index)
				_, err = orm.Exec(sql)
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
	for name2, index2 := range oldModel.GetIndexes() {
		if _, ok := foundIndexNames[name2]; !ok { // 在当前数据表且不再新数据表里的索引都要清除
			sql := orm.dialect.DropIndexSql(lTableName, index2)
			_, err = orm.Exec(sql)
			if err != nil {
				return err
			}
		}
	}

	// 重新添加索引
	for name, index := range addedNames {
		if index.Type == UniqueType {
			lSession := orm.NewSession()
			lSession.Model(newModel.GetName())
			//TODO # 修正上面指向错误Model
			//			lSession.model = newModel
			lSession.Statement.model = newModel
			defer lSession.Close()
			err = lSession.addUnique(lTableName, name)

		} else if index.Type == IndexType {
			lSession := orm.NewSession()
			lSession.Model(newModel.GetName())
			//TODO # 修正上面指向错误Model
			//			lSession.model = newModel
			lSession.Statement.model = newModel
			defer lSession.Close()
			err = lSession.addIndex(lTableName, name)
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

	model_name := ""
	if len(model) > 0 {
		model_name = model[0]
	} else if self.Statement.model != nil {
		model_name = self.Statement.model.name
	} else {
		return false, errors.New("model should not be blank")
	}

	tableName := strings.Replace(model_name, ".", "_", -1)
	tableName = utils.SnakeCasedName(tableName)
	sql, args := self.orm.dialect.TableCheckSql(tableName)
	lDs, err := self.query(sql, args...)
	if err != nil {
		return false, err
	}

	return lDs.Count() > 0, nil
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

// return the orm instance
func (self *TSession) Orm() *TOrm {
	return self.orm
}

//
func (self *TSession) Model(model string, region ...string) *TSession {
	var mod IModel
	var err error

	// #如果Session已经预先指定Model
	if self.Statement.model != nil && self.Statement.model.GetName() == model {
		mod = self.Statement.model
	} else {
		mod, err = self.orm.GetModel(model, region...)
		if err != nil {
			logger.Panicf(err.Error())
			self.IsDeprecated = true
		}
	}

	self.IsClassic = true
	self.Statement.model = mod.GetBase()
	//	self.Statement.TableNameClause = mod.GetName()

	// # 主键
	self.Statement.IdKey = self.Statement.model.idField

	/* TODO 删除  不可能会出现
	if md = nil {
		self.IsClassic = false
		lTableName := utils.SnakeCasedName(strings.Replace(model, ".", "_", -1))
		//logger.Err("Model %s is not a standard model type of this system", lTableName)
		self.Statement.Table = self.orm.tables[lTableName]
		if self.Statement.Table == nil {
			logger.Errf("the table is not in database.")
			self.IsDeprecated = true
			return nil
		}
		self.Statement.AltTableNameClause = lTableName
		self.Statement.TableNameClause = lTableName

		// # 主键
		self.Statement.IdKey = "id"
		col := self.Statement.Table.GetFieldByName(self.Statement.Table.obj.AutoIncrementField)
		if col != nil && ((!col.Nullable && col.Base().isPrimaryKey && col.Base().isAutoIncrement) ||
			(!col.Base().Nullable && col.Base().isAutoIncrement)) {
			self.Statement.IdKey = self.Statement.Table.obj.AutoIncrementField
		}
	}
	*/

	// ### id key must exist
	if self.Statement.IdKey == "" {
		logger.Errf("the statement must have a Id key exist!")
		self.IsDeprecated = true
	}

	return self
}

// TODO 在生成的SQL语句前加sql
func (self *TSession) Prefix(sql string) *TSession {
	return self

}

// TODO 在生成的SQL语句后加sqlS
func (self *TSession) Suffix(sql string) *TSession {
	return self

}

// select filed or select all using * symbol
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
func (self *TSession) Ids(ids ...interface{}) *TSession {
	self.Statement.Ids(ids...)
	return self
}

func (self *TSession) Desc(fileds ...string) *TSession {
	self.Statement.Desc(fileds...)
	return self
}

// Where condition
// Example: Where("id==?",1)
// 支持Domain 返回解析为Domain
func (self *TSession) Where(clause string, args ...interface{}) *TSession {
	self.Statement.Where(clause, args...)
	return self
}

// And provides custom query condition.
func (self *TSession) And(clause string, args ...interface{}) *TSession {
	self.Statement.And(clause, args...)
	return self
}

// Or provides custom query condition.
func (self *TSession) Or(clause string, args ...interface{}) *TSession {
	self.Statement.Or(clause, args...)
	return self
}

func (self *TSession) In(clause string, args ...interface{}) *TSession {
	self.Statement.In(clause, args...)
	return self
}

func (self *TSession) NotIn(clause string, args ...interface{}) *TSession {
	self.Statement.NotIn(clause, args...)
	return self
}

// set the pointed field value for create/write operations
func (self *TSession) Set(fieldName string, value interface{}) *TSession {
	self.Statement.Set(fieldName, value)
	return self
}

/* support domain string and list objec
[('foo', '=', 'bar')]
foo = 'bar'

[('id', 'in', [1,2,3])]
id in (1, 2, 3)

[('field', '=', 'value'), ('field', '<>', 42)]
( field = 'value' AND field <> 42 )

[('&', ('field', '<', 'value'), ('field', '>', 'value'))]
( field < 'value' AND field > 'value' )

[('|', ('field', '=', 'value'), ('field', '=', 'value'))]
( field = 'value' OR field = 'value' )

[('&', ('field1', '=', 'value'), ('field2', '=', 'value'), ('|', ('field3', '<>', 'value'), ('field4', '=', 'value')))]
( field1 = 'value' AND field2 = 'value' AND ( field3 <> 'value' OR field4 = 'value' ) )

[('&', ('|', ('a', '=', 1), ('b', '=', 2)), ('|', ('c', '=', 3), ('d', '=', 4)))]
( ( a = 1 OR b = 2 ) AND ( c = 3 OR d = 4 ) )

[('|', (('a', '=', 1), ('b', '=', 2)), (('c', '=', 3), ('d', '=', 4)))]
( ( a = 1 AND b = 2 ) OR ( c = 3 AND d = 4 ) )               */
func (self *TSession) Domain(domain interface{}, args ...interface{}) *TSession {
	self.Statement.Domain(domain, args...)
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

func (self *TSession) Limit(limit int64, offset ...int64) *TSession {
	self.Statement.Limit(limit, offset...)
	return self
}

func (self *TSession) create(src interface{}) (res_id interface{}, res_err error) {
	if len(self.Statement.model.GetName()) < 1 {
		return nil, ErrTableNotFound
	}

	// 解析数据
	var vals map[string]interface{}
	vals, res_err = self.validateValues(src)
	if res_err != nil {
		return nil, res_err
	}

	// 拆分数据
	lNewVals, lRefVals, lNewTodo, res_err := self.separateValues(vals, self.Statement.Fields, self.Statement.NullableFields, true, true)
	if res_err != nil {
		return nil, res_err
	}

	// 创建关联数据
	for tbl, rel_vals := range lRefVals {
		if len(rel_vals) == 0 {
			continue // # 关系表无数据更新则忽略
		}

		//utils.Dbg(" rel table", tbl)
		// ???删除关联外键
		//if _, has := vals[self.model._relations[tbl]]; has {
		//	delete(vals, self.model._relations[tbl])
		//}

		// 创建或者更新关联表记录
		lMdlObj, err := self.orm.osv.GetModel(tbl) // #i
		if err != nil {
			return nil, err
		}

		// 获取管理表UID
		record_id := rel_vals[lMdlObj.IdField()]
		if record_id == nil || utils.IsBlank(record_id) {
			effect, err := lMdlObj.Records().Create(rel_vals)
			if err != nil {
				return nil, err
			}
			record_id = effect
		} else {
			lMdlObj.Records().Ids(record_id).Write(rel_vals)
		}

		lNewVals[self.Statement.model.obj.GetRelationByName(tbl)] = record_id
	}

	// 被设置默认值的字段赋值给Val
	for k, v := range self.Statement.model.obj.GetDefault() {
		if lNewVals[k] == nil {
			lNewVals[k] = v //fmt. lFld._symbol_c
		}
	}

	// #验证数据类型
	//TODO 需要更准确安全
	self.Statement.model._validate(lNewVals)

	id_field := self.Statement.model.idField
	fields := make([]string, 0)
	params := make([]interface{}, 0)
	// 字段,值
	for k, v := range lNewVals {
		if v == nil { // 过滤nil 的值
			continue
		}

		if k == id_field {
			res_id = v
		}

		fields = append(fields, k)
		params = append(params, v)
	}

	var res sql.Result
	var err error
	sql, isQuery := self.Statement.generate_insert(fields)
	if isQuery {
		ds, err := self.query(sql, params...)
		if err != nil {
			return nil, err
		}

		res_id = ds.Record().FieldByIndex(0).AsInteger()
	} else {
		res, err = self.Exec(sql, params...)
		if err != nil {
			return nil, err
		}

		// 支持递增字段返回ID
		if len(self.Statement.model.idField) > 0 {
			res_id, res_err = res.LastInsertId()
			if res_err != nil {
				return nil, res_err
			}
		}
	}

	if self.IsClassic {
		// 更新关联字段
		for _, name := range lNewTodo {
			lField := self.Statement.model.GetFieldByName(name)
			//logger.Dbg("create lNewTodo", name, lField, res_id)
			if lField != nil {
				lField.OnWrite(&TFieldEventContext{
					Session: self,
					Model:   self.Statement.model,
					Id:      res_id,
					Field:   lField,
					Value:   vals[name]})
			}
		}
	}

	if res_id != nil {
		//更新缓存
		table_name := self.Statement.model.GetName()
		lRec := dataset.NewRecordSet(nil, lNewVals)
		self.orm.Cacher.PutById(table_name, utils.IntToStr(res_id), lRec) //for create

		// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
		self.orm.Cacher.ClearByTable(table_name) //for create
	}

	return res_id, nil
}

// TODO
// 验证输入的数据
func (self *TSession) validateValues(values interface{}) (map[string]interface{}, error) {
	var result map[string]interface{}
	if values != nil {
		result = self.convertItf2ItfMap(values)
		if len(result) == 0 {
			return nil, fmt.Errorf("can't support this type of values: %v", values)
		}

		result = utils.MergeMaps(self.Statement.Sets, result)

	} else {
		if len(self.Statement.Sets) == 0 {
			return nil, fmt.Errorf("must submit the values for update")
		}

		result = self.Statement.Sets
	}

	return result, nil
}

// TODO FN
// 分配值并补全ID,Update,Create字段值
// separate data for difference type of update
//, includeVersion bool, includeUpdated bool, includeNil bool,
//	includeAutoIncr bool, allUseBool bool, useAllCols bool,
//	mustColumnMap map[string]bool, nullableMap map[string]bool,
//	columnMap map[string]bool, update, unscoped bool
// includePkey is the values inclduing key
func (self *TSession) separateValues(vals map[string]interface{}, mustFields map[string]bool, nullableFields map[string]bool, includeNil bool, mustPkey bool) (map[string]interface{}, map[string]map[string]interface{}, []string, error) {
	//!!! create record not need to including pk

	// 用于更新本Model的实际数据
	/*    # list of column assignments defined as tuples like:
	      #   (column_name, format_string, column_value)
	      #   (column_name, sql_formula)
	      # Those tuples will be used by the string formatting for the INSERT
	      # statement below.
	      ('id', "nextval('%s')" % self._sequence),*/
	new_vals := make(map[string]interface{})
	rel_vals := make(map[string]map[string]interface{})
	upd_todo := make([]string, 0) // function 字段组 采用其他存储方式

	// 保存关联表用于更新创建关联表数据
	for tbl, field_name := range self.Statement.model.obj.GetRelations() {
		rel_vals[tbl] = make(map[string]interface{}) //NOTE 新建空Map以防Nil导致内存出错
		if val, has := vals[field_name]; has && val != nil {
			//if val, has := vals[self.Statement.model.obj.GetRelationByName(tbl)]; has && val != nil {
			rel_id := val //新建新的并存入已经知道的ID
			if rel_id != nil {
				rel_vals[tbl][self.Statement.model.idField] = rel_id //utils.Itf2Str(vals[self.model._relations[tbl]])
			}
		}
	}

	// 处理常规字段
	for name, field := range self.Statement.model.GetFields() {
		if field == nil {
			continue
		}

		// TODO 保留审视 // ignore AutoIncrement field
		//	if col != nil && !mustPkey && (col.IsAutoIncrement || col.IsPrimaryKey) {
		if field != nil && field.IsAutoIncrement() {
			continue //!!! do no use any AutoIncrement field's value
		}

		// ** 格式化IdField数据 生成UID
		if mustPkey {
			if f, ok := field.(*TIdField); ok {
				new_vals[name] = f.OnCreate(&TFieldEventContext{
					Session: self,
					Model:   self.Statement.model,
					Field:   field,
					Id:      utils.IntToStr(0),
					Value:   vals[name]},
				)
			}
		}

		// update time zone to create and update tags' fields
		if mustPkey && field.Base().isCreated {
			lTimeItfVal, _ := self.orm.nowTime(field.Type()) //TODO 优化预先生成日期
			vals[name] = lTimeItfVal

		} else if field.Base().isCreated {
			// 包含主键的数据,说明已经是被创建过了,则不补全该字段
			continue

		} else if field.Base().isUpdated {
			lTimeItfVal, _ := self.orm.nowTime(field.Type()) //TODO 优化预先生成日期
			vals[name] = lTimeItfVal
		}

		// 以下处理非强字段
		is_must_field := mustFields[name]
		lNullableField := nullableFields[name]
		if val, has := vals[name]; has {
			// 过滤可以为空的字段空字段
			//logger.Dbg("## XXX:", name, val, has, val == nil, utils.IsBlank(val))
			if !is_must_field && !lNullableField && !includeNil && (val == nil || utils.IsBlank(val)) {
				continue
			}

			//logger.Dbg("## VV:", name, col.SQLType.IsNumeric())
			if field != nil && field.SQLType().IsNumeric() {
				//logger.Dbg("## VV:", name, val, blank, reflect.TypeOf(val), val == blank)
				if utils.IsBlank(val) {
					continue
				}
			}

			// TODO 优化确认代码位置  !NOTE! 转换值为数据库类型
			val = field.onConvertToWrite(self, val)

			// #相同名称的字段分配给对应表
			comm_tables := self.Statement.model.obj.GetCommonFieldByName(name) // 获得拥有该字段的所有表
			if comm_tables != nil {
				// 为各表预存值
				for tbl, _ := range comm_tables {
					if tbl == self.Statement.model.GetName() {
						new_vals[name] = val // 为当前表添加共同字段值

					} else if rel_vals[tbl] != nil {
						rel_vals[tbl][name] = val // 为关联表添加共同字段值

					}
				}

				continue //* 字段分配完毕
			}

			//#*** 非Model固有字段归为关联表字段 2个判断缺一不可
			//#1 判断是否是关联表可能性
			//#2 判断是否Model和关联Model都有该字段
			///rel_fld := self.model.RelateFieldByName(name)
			///if rel_fld != nil && field.IsRelatedField() {
			//comm_field := self.model.obj.GetCommonFieldByName(name)
			if field.IsInheritedField() {
				// 如果是继承字段移动到tocreate里创建记录，因本Model对应的数据没有该字段
				lTableName := field.ModelName() // rel_fld.RelateTableName
				rel_vals[lTableName][name] = val

			} else {
				if field.Store() && field.SQLType().Name != "" {
					// TODO 格式化值 区分Classic
					new_vals[name] = val // field.SymbolFunc()(utils.Itf2Str(val))
				} else {
					//# 大型复杂计算字段
					upd_todo = append(upd_todo, name)
				}

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
				if !field.IsInheritedField() && field.Type() == "selection" && val != nil {
					self._check_selection_field_value(field, val) //context
				}
			}
		}
	}

	return new_vals, rel_vals, upd_todo, nil
}

// #fix:由于更新可能只对少数字段更新,更新不适合使用缓存
func (self *TSession) write(src interface{}, context map[string]interface{}) (res_effect int64, res_err error) {
	if len(self.Statement.model.GetName()) < 1 {
		return -1, ErrTableNotFound
	}

	var (
		ids      []interface{}
		lRefVals map[string]map[string]interface{}
		lNewTodo []string
	)

	var lNewVals map[string]interface{}
	lNewVals, res_err = self.validateValues(src)
	if res_err != nil {
		return 0, res_err
	}

	// #获取Ids
	if len(self.Statement.IdParam) > 0 {
		ids = self.Statement.IdParam
	} else {
		idField := self.Statement.model.idField
		if id, has := lNewVals[idField]; has {
			ids = []interface{}{id}

		} else if self.Statement.domain.Count() > 0 {
			var err error
			ids, err = self.search("", nil)
			if err != nil {
				return 0, err
			}

			if len(ids) == 0 {
				return 0, fmt.Errorf("no records matched")
			}

		} else {
			return 0, fmt.Errorf("At least have one of Where()|Domain()|Ids() condition to locate for writing update")
		}
	}

	// the PK condition status
	includePkey := len(ids) > 0
	if !includePkey {
		return 0, fmt.Errorf("must have ids or qury clause")
	}

	if self.IsClassic {
		//???
		for field, _ := range lNewVals {
			var fobj IField
			fobj = self.Statement.model.GetFieldByName(field)
			if fobj == nil {
				lF := self.Statement.model.obj.GetRelatedFieldByName(field)
				if lF != nil {
					fobj = lF.RelateField
				}
			}

			if fobj == nil {
				continue
			}
		}

		lNewVals, lRefVals, lNewTodo, res_err = self.separateValues(lNewVals, self.Statement.Fields, self.Statement.NullableFields, false, !includePkey)
		if res_err != nil {
			return 0, res_err
		}
	}

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

			values += self.orm.dialect.Quote(k) + "=?"
			params = append(params, v)
		}

		if values == "" {
			return 0, fmt.Errorf("must have values")
		}

		// add in ids data
		in_vals := strings.Repeat("?,", len(ids)-1) + "?"
		params = append(params, ids...)

		// format sql
		sql := fmt.Sprintf(`UPDATE "%s" SET %s WHERE %s IN (%s)`, self.Statement.model.GetName(), values, self.Statement.IdKey, in_vals)
		res, err := self.exec(sql, params...)
		if err != nil {
			return 0, err
		}

		res_effect, res_err = res.RowsAffected()
		if res_err != nil {
			return 0, res_err
		}

		/*table_name := self.Statement.model.GetName()
		//lCacher := self.orm.Cacher.RecCacher(self.Statement.model.GetName()) // for write
		//if lCacher != nil {
		for _, id := range ids {
			if id != "" {
				//更新缓存
				//lKey := self.generate_caches_key(self.Statement.model.GetName(), id)
				lRec := NewRecordSet(nil, lNewVals)
				self.orm.Cacher.PutById(table_name, id, lRec)
			}
		}*/
		//}

	}

	// 更新关联表
	for tbl, ref_vals := range lRefVals {
		lFldName := self.Statement.model.obj.GetRelationByName(tbl)

		nids := make([]interface{}, 0)
		// for sub_ids in cr.split_for_in_conditions(ids):
		//     cr.execute('select distinct "'+col+'" from "'+self._table+'" ' \
		//               'where id IN %s', (sub_ids,))
		//    nids.extend([x[0] for x in cr.fetchall()])

		// add in ids data
		in_vals := strings.Repeat("?,", len(ids)-1) + "?"
		lSql := fmt.Sprintf(`SELECT distinct "%s" FROM "%s" WHERE %s IN(%s)`, lFldName, self.Statement.model.GetName(), self.Statement.IdKey, in_vals)
		lDs, err := self.orm.Query(lSql, ids...)
		if err != nil {
			logger.Err(err)
		}

		lDs.First()
		for !lDs.Eof() {
			nids = append(nids, lDs.FieldByName(lFldName).AsInterface())
			lDs.Next()
		}

		if len(ref_vals) > 0 { //# 重新写入关联数据
			lMdlObj, err := self.orm.GetModel(tbl) // #i
			if err != nil {
				logger.Err(err)
			}

			lMdlObj.Records().Ids(nids...).Write(ref_vals) //TODO 检查是否真确使用
		}
	}

	// 更新计算字段
	for _, name := range lNewTodo {
		//logger.Dbg(name)
		lField := self.Statement.model.GetFieldByName(name)
		if lField != nil {
			err := lField.OnWrite(&TFieldEventContext{
				Session: self,
				Model:   self.Statement.model,
				Id:      ids[0], // TODO 修改获得更合理
				Field:   lField,
				Value:   lNewVals[name],
			})
			if err != nil {
				logger.Err(err)

			}
		}
	}

	return
}

func (self *TSession) Create(src interface{}, classic_create ...bool) (res_id interface{}, res_err error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, fmt.Errorf("the session of query is invalid!")
	}

	var classic bool
	if len(classic_create) > 0 {
		self.IsClassic = classic
	}

	return self.create(src)
}

// TODO 接受多值 dataset
// TODO 当只有M2M被更新时不更新主数据倒数据库
// start to write data from the database
func (self *TSession) Write(data interface{}, classic_write ...bool) (int64, error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	if len(classic_write) > 0 {
		self.IsClassic = classic_write[0]
	}
	return self.write(data, nil)
}

// start to read data from the database
func (self *TSession) Read(classic_read ...bool) (*dataset.TDataSet, error) {
	// reset after complete
	//defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, ErrInvalidSession
	}

	if len(classic_read) > 0 {
		self.IsClassic = classic_read[0]
	}

	return self.read()
}

func (self *TSession) read() (*dataset.TDataSet, error) {
	if len(self.Statement.model.GetName()) < 1 {
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

	// get data ids with query
	var ids []interface{}
	if len(self.Statement.IdParam) > 0 {
		ids = self.Statement.IdParam
	} else {
		var err error
		ids, err = self.search("", nil)
		if err != nil {
			return nil, err
		}

		if len(ids) == 0 {
			return dataset.NewDataSet(), nil
		}
	}

	// 字段分类
	// 验证Select * From
	if len(self.Statement.Fields) > 0 {
		for name, allowed := range self.Statement.Fields {
			if !allowed {
				continue
			}

			fld := self.Statement.model.obj.GetFieldByName(name)
			if fld != nil && !fld.IsRelatedField() { //如果是本Model的字段
				stored = append(stored, name)
			} else if fld != nil {
				computed = append(computed, name)

				if fld.IsRelatedField() { // and field.base_field.column:
					inherited = append(inherited, name)
				}
			} else {
				logger.Warnf(`%s.read() with unknown field '%s'`, self.Statement.model.GetName(), name)
			}
		}
	} else {
		for name, field := range self.Statement.model.GetFields() {
			if field != nil {
				if field.IsRelatedField() {
					inherited = append(inherited, name)
				} else {
					stored = append(stored, name)
				}
			}
		}
	}

	// 获取数据库数据
	//# fetch stored fields from the database to the cache
	dataset, _, err := self.readFromDatabase(ids, stored, inherited)
	if err != nil {
		return nil, err
	}

	// 处理经典字段数据
	if self.IsClassic {
		// 处理那些数据库不存在的字段：company_ids...
		//# retrieve results from records; this takes values from the cache and
		// # computes remaining fields
		name_fields := make([]IField, 0)
		for _, name := range stored {
			fld := self.Statement.model.obj.GetFieldByName(name)
			if fld != nil {
				name_fields = append(name_fields, fld)
			}
		}

		for _, name := range computed {
			fld := self.Statement.model.obj.GetFieldByName(name)
			if fld != nil {
				name_fields = append(name_fields, fld)
			}
		}

		//TODO　执行太多SQL
		for _, field := range name_fields {
			//logger.Dbg("aa", rec_id, name)

			field.OnRead(&TFieldEventContext{
				Session: self,
				Model:   self.Statement.model,
				Field:   field,
				//Id:      rec_id,
				//Value:   val,
				Dataset: dataset,
			})
			//logger.Dbg("convert_to_read:", name, val, dataset.Count(), rec_id, dataset.FieldByName("id").AsString(), dataset.Position, dataset.Eof(), res_dataset.FieldByName(name).AsString(), dataset.FieldByName(name).AsInterface(), field)
		}
	}

	dataset.First() // 返回游标0
	dataset.Classic(self.IsClassic)
	return dataset, nil
}

func (self *TSession) Count() (int, error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	self.Statement.IsCount = true

	ids, err := self.search("", nil)
	if err != nil {
		return 0, err
	}

	return len(ids), nil
}

// TODO sum
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
		var sql string
		var args []interface{}
		var err error
		if len(self.statement.RawSQL) == 0 {
			sql, args, err = self.statement.generate_sum(columnNames...)
			if err != nil {
				return err
			}
		} else {
			sql = self.statement.RawSQL
			args = self.statement.RawParams
		}

		session.queryPreprocess(&sql, args...)

		if isSlice {
			if session.isAutoCommit {
				err = session.DB().QueryRow(sql, args...).ScanSlice(res)
			} else {
				err = session.tx.QueryRow(sql, args...).ScanSlice(res)
			}
		} else {
			if session.isAutoCommit {
				err = session.DB().QueryRow(sql, args...).Scan(res)
			} else {
				err = session.tx.QueryRow(sql, args...).Scan(res)
			}
		}

		if err == sql.ErrNoRows || err == nil {
			return nil
		}
	*/
	return 0, nil
}

// TODO 根据条件删除
// delete records
func (self *TSession) Delete(ids ...interface{}) (res_effect int64, err error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	// TODO 为什么用len
	if len(self.Statement.model.GetName()) < 1 {
		return 0, ErrTableNotFound
	}
	//self._check_model()

	// get id list
	if len(ids) > 0 {
		self.Statement.IdParam = append(self.Statement.IdParam, ids...)
	} else {
		var err error
		ids, err = self.search("", nil)
		if err != nil {
			return 0, err
		}
	}

	// get the model id field name
	id_field := self.Statement.model.idField

	//#1 删除目标Model记录
	sql := fmt.Sprintf(`DELETE FROM %s WHERE %s in (%s); `, self.Statement.model.GetName(), id_field, idsToSqlHolder(ids...))
	res, err := self.exec(sql, ids...)
	if err != nil {
		return 0, err
	}

	if cnt, err := res.RowsAffected(); err != nil || (int(cnt) != len(ids)) {
		self.Rollback()
		return 0, ErrDeleteFailed
	}

	table_name := self.Statement.model.GetName()
	//lCacher := self.orm.Cacher.RecCacher(self.Statement.model.GetName()) // for del
	//if lCacher != nil {
	for _, id := range ids {
		//lCacher.Remove(id)
		self.orm.Cacher.RemoveById(table_name, id)
	}
	//}
	// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
	//lCacher = self.orm.Cacher.SqlCacher(self.Statement.model.GetName()) // for del
	//lCacher.Clear()
	self.orm.Cacher.ClearByTable(self.Statement.model.GetName())

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
	//tableName := strings.Replace(model, ".", "_", -1)
	//tableName = utils.SnakeCasedName(tableName)
	//fmt.Println("CreateTable", tableName, self.orm.GetTable(tableName))
	//self.Statement.Table = self.orm.GetTable(tableName)
	mod, err := self.orm.GetModel(model)
	if err != nil {
		return err
	}

	self.Statement.model = mod.GetBase() //self.orm.GetTable(tableName)

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
	lSql = self.Statement.generate_create_table()
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

	//tableName := strings.Replace(model, ".", "_", -1)
	//tableName = utils.SnakeCasedName(tableName)
	//self.Statement.Table = self.orm.GetTable(tableName)
	mod, err := self.orm.GetModel(model)
	if err != nil {
		return err
	}

	self.Statement.model = mod.GetBase() //self.orm.GetTable(tableName)

	lSqls := self.Statement.generate_unique()
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

	//tableName := strings.Replace(model, ".", "_", -1)
	//tableName = utils.SnakeCasedName(tableName)
	mod, err := self.orm.GetModel(model)
	if err != nil {
		return err
	}

	self.Statement.model = mod.GetBase() //self.orm.GetTable(tableName)

	lSqls := self.Statement.generate_index()
	for _, sql := range lSqls {
		_, err := self.exec(sql)
		if err != nil {
			return err
		}
	}

	return nil
}

// drop table will drop table if exist, if drop failed, it will return error
func (self *TSession) DropTable(name string) (err error) {
	var needDrop = true
	/*if !session.Engine.dialect.SupportDropIfExists() {
		sql, args := session.Engine.dialect.TableCheckSql(tableName)
		results, err := session.query(sql, args...)
		if err != nil {
			return err
		}
		needDrop = len(results) > 0
	}
	*/
	if needDrop {
		sql := self.orm.dialect.DropTableSql(name)
		res, err := self.exec(sql)
		if err != nil {
			return err
		}

		if cnt, err := res.RowsAffected(); err == nil && cnt > 0 {
			model, err := self.orm.GetModel(name)
			if err != nil {
				return err
			}

			if model.GetBase().is_base { // 只移除Table生成的Model
				self.orm.osv.RemoveModel(name)
			}
		}

		return err
	}

	return
}

func (self *TSession) addColumn(colName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	col := self.Statement.model.GetFieldByName(colName)
	//logger.Dbg("addColumn", self.Statement.Table.Type, colName, col)
	sql, args := self.Statement.generate_add_column(col)
	_, err := self.exec(sql, args...)
	return err
}

func (self *TSession) addIndex(tableName, idxName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	index := self.Statement.model.GetIndexes()[idxName]
	sql := self.orm.dialect.CreateIndexSql(tableName, index)

	_, err := self.exec(sql)
	return err
}

func (self *TSession) addUnique(tableName, uqeName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}
	index := self.Statement.model.GetIndexes()[uqeName]
	sql := self.orm.dialect.CreateIndexSql(tableName, index)
	_, err := self.exec(sql)
	return err
}

// 作废 更新单一字段
func (self *TSession) _update_field(id interface{}, field *TField, value string, rel_context map[string]interface{}) {
	self.exec(fmt.Sprintf("UPDATE "+self.Statement.model.GetName()+" SET "+field.Name()+"="+field.SymbolChar()+" WHERE id=%v", field.SymbolFunc()(value), id))
}

// Check whether value is among the valid values for the given
//    selection/reference field, and raise an exception if not.
func (self *TSession) _check_selection_field_value(field IField, value interface{}) {
	//   field = self._fields[field]
	// field.convert_to_cache(value, self)
}

func (self *TSession) _check_model() bool {
	if self.Statement.model == nil {
		panic("Must point out a Model for continue")
		return false
	}

	return true
}

// search and return the id list only
func (self *TSession) Search() ([]interface{}, error) {
	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, ErrInvalidSession
	}

	return self.search("", nil)
}

// 查询所有符合条件的主键/索引值
// :param access_rights_uid: optional user ID to use when checking access rights
// (not for ir.rules, this is only for ir.model.access)
func (self *TSession) search(access_rights_uid string, context map[string]interface{}) ([]interface{}, error) {
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

	//logger.Dbg("_search", self.Statement.Domain, StringList2Domain(self.Statement.Domain))
	var err error
	query, err = self.Statement.where_calc(self.Statement.domain, false, context)
	if err != nil {
		return nil, err
	}

	order_by = self.Statement.generate_order_by(query, context) // TODO 未完成
	from_clause, where_clause, where_clause_params = query.get_sql()
	//logger.Dbg("from_clause", from_clause)
	//logger.Dbg("where_clause", where_clause)
	//logger.Dbg("where_clause_params", where_clause_params)
	//} else {
	//	from_clause = self.Statement.model.GetName()
	//	//where_clause, where_clause_params = self.Statement.WhereClause, self.Statement.Params //self.Statement.generate_query(context, true, true, false, true, true, true, true, nil)
	//	where_clause, where_clause_params = self.Statement.WhereClause, self.Statement.Params //self.Statement.generate_query(context, true, true, false, true, true, true, true, nil)
	//}

	if where_clause != "" {
		where_clause = fmt.Sprintf(` WHERE %s`, where_clause)
	}

	table_name := self.Statement.model.GetName()
	if self.Statement.IsCount {
		// Ignore order, limit and offset when just counting, they don't make sense and could
		// hurt performance
		query_str = `SELECT count(1) FROM ` + from_clause + where_clause

		res_ids := self.orm.Cacher.GetBySql(table_name, query_str, where_clause_params)
		if len(res_ids) < 1 {
			lRes, err := self.query(query_str, where_clause_params...)
			if err != nil {
				return nil, err
			}

			res_ids = []interface{}{lRes.FieldByName("count").AsInterface()}

			// #存入缓存
			self.orm.Cacher.PutBySql(table_name, query_str, where_clause_params, res_ids...)
		}

		return res_ids, nil
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

	query_str = fmt.Sprintf(`SELECT %s.%s FROM `, self.orm.dialect.Quote(self.Statement.model.GetName()), self.orm.dialect.Quote(self.Statement.IdKey)) + from_clause + where_clause + order_by + limit_str + offset_str
	//	web.Debug("_search", query_str, where_clause_params)

	// #调用缓存
	res_ids := self.orm.Cacher.GetBySql(table_name, query_str, where_clause_params)
	if len(res_ids) < 1 {
		res, err := self.query(query_str, where_clause_params...)
		if err != nil {
			return nil, err
		}
		res_ids = res.Keys(self.Statement.IdKey)
		//logger.Dbg("_search", res.KeyField, res.Count(), res.Keys(), res_ids)

		self.orm.Cacher.PutBySql(table_name, query_str, where_clause_params, res_ids...)
	}

	return res_ids, nil
}

//# ids_less 缺少的ID
func (self *TSession) readFromCache(ids []interface{}) (res []*dataset.TRecordSet, ids_less []interface{}) {
	res, ids_less = self.orm.Cacher.GetByIds(self.Statement.model.GetName(), ids...)
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
func (self *TSession) readFromDatabase(ids []interface{}, field_names, inherited_field_names []string) (res_ds *dataset.TDataSet, res_sql string, err error) {
	// # 从缓存里获得数据
	records, less_ids := self.readFromCache(ids)

	// # 补缺缓存没有的数据
	if len(less_ids) > 0 {
		table_name := self.Statement.model.GetName()
		//# make a query object for selecting ids, and apply security rules to it
		//占位符先提供固定预置值，后面根据值获得Idx并修改值到正确实时值
		//param_ids := "idholder" //占位符先提供固定预置值，后面根据值获得Idx并修改值到正确实时值
		//query(['"%s"' % self._table], ['"%s".id IN %%s' % self._table], [param_ids])
		//query := NewQuery([]string{self.model.TableName()},
		//	[]string{fmt.Sprintf(`"%s".id IN (?)`, self.model.TableName())},
		//	[]interface{}{param_ids}, nil, nil) //占位符先提供固定预置值，后面根据值获得Idx并修改值到正确实时值                                                                                  // object()
		query := NewQuery(
			[]string{table_name},
			[]string{fmt.Sprintf(
				`%s.%s IN (%s)`,
				self.orm.dialect.Quote(table_name),
				self.orm.dialect.Quote(self.Statement.IdKey),
				idsToSqlHolder(ids...))},
			ids,
			nil,
			nil,
		)

		order_str := self.Statement.generate_order_by(query, nil)

		//qual_names = map(qualify, set(fields_pre + [self._fields['id']]))
		qual_names := make([]string, 0)

		if self.IsClassic {
			//对可迭代函数'iterable'中的每一个元素应用‘function’方法，将结果作为list返回
			//# determine the fields that are stored as columns in tables;
			fields := make([]IField, 0)
			fields_pre := make([]IField, 0)
			for _, name := range field_names {
				if f := self.Statement.model.obj.GetFieldByName(name); f != nil {
					fields = append(fields, f)
				}
			}

			for _, name := range inherited_field_names {
				if f := self.Statement.model.obj.GetFieldByName(name); f != nil {
					fields = append(fields, f)
				}
			}

			//	当字段为field.base_field.column.translate可调用即是translate为回调函数而非Bool值时不加入Join
			for _, fld := range fields {
				//if fld.IsClassicRead() && !(fld.IsRelatedField() && false) { //用false代替callable(field.base_field.column.translate)
				if fld.Store() && fld.SQLType().Name != "" && !(fld.IsRelatedField() && false) { //用false代替callable(field.base_field.column.translate)
					fields_pre = append(fields_pre, fld)
				}
			}

			for _, f := range fields_pre {
				qual_names = append(qual_names, self.qualify(f, query))
			}

		} else {
			qual_names = self.Statement.generate_fields()
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

		// 获得Id占位符索引
		id_len := len(self.Statement.IdParam)
		res_ds, err = self.Query(res_sql, params...) //cr.execute(res_sql, params)
		if err != nil {
			return nil, "", err
		}

		// NOTE 当非IDS()读取时报告错误
		if id_len == 0 && res_ds.Count() != len(less_ids) {
			//# if not you need
			return nil, "", logger.Errf(`query result including %v records are not what you expectd! %v`, res_ds.Count(), len(less_ids))
		}

		// TODO 带优化或者简去
		//if !dataset.SetKeyField(self.Statement.IdKey) {
		//	logger.Err(`set key_field fail when call RecordByKey(key_field:%v)!`, res_ds.KeyField)
		//}

		for !res_ds.Eof() {
			rec := res_ds.Record()
			// # 添加进入缓存
			self.orm.Cacher.PutById(table_name, rec.FieldByName(self.Statement.IdKey).AsString(), rec)
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
					logger.Err(`query result didn't including record (%v)!`, id)
				}

				// #添加进入数据集
				err = res_ds.AppendRecord(rec)
				if err != nil {
					logger.Err(err.Error())
				}

				// # 添加进入缓存
				self.orm.Cacher.PutById(table_name, id, rec)
			}
		*/
	} else {
		// # init dataset
		res_ds = dataset.NewDataSet()
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
		/*var field IField
		for _, name := range field_names {
			field = self.model.FieldByName(name)
			field.OnRead(&TFieldEventContext{
				Session: self,
				Model:   self.model,
				Field:   field,
				Dataset: res_ds})
		}
		*/
	}

	return res_ds, res_sql, nil
}

func (self *TSession) convertStruct2Itfmap(src interface{}) (res_map map[string]interface{}) {
	var (
		lField           reflect.StructField
		lFieldType       reflect.Type
		lFieldValue      reflect.Value
		lIsRequiredField bool
		lCol             *TField

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
		lName = fmtFieldName(lField.Name)

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
					lName = fmtFieldName(lTag[1])
				}
			case "extends", "relate":
				//				IsStruct = true
				if (lFieldValue.Kind() == reflect.Ptr && lFieldValue.Elem().Kind() == reflect.Struct) ||
					lFieldValue.Kind() == reflect.Struct {
					m := self.convertStruct2Itfmap(lFieldValue.Interface())

					for col, val := range m {
						res_map[col] = val
					}

					//
					goto CONTINUE
				}
			}
		}

		// 字段必须在数据库里
		if fld := self.Statement.model.obj.GetFieldByName(lName); fld == nil {
			continue
		} else {
			lCol = fld.Base()
			//废弃
			//if lCol == nil {
			//	continue
			//}
		}

		//logger.Dbg("field#", lName, lFieldType, lFieldValue)
		switch lFieldType.Kind() {
		case reflect.Struct:
			if lFieldType.ConvertibleTo(TimeType) {
				t := lFieldValue.Convert(TimeType).Interface().(time.Time)
				if !lIsRequiredField && (t.IsZero() || !lFieldValue.IsValid()) {
					continue
				}
				lValue = self.orm.FormatTime(lCol.SQLType().Name, t)
			} else {
				if lCol.SQLType().IsJson() {
					if lCol.SQLType().IsText() {
						bytes, err := json.Marshal(lFieldValue.Interface())
						if err != nil {
							logger.Errf("IsJson", err)
							continue
						}
						lValue = string(bytes)
					} else if lCol.SQLType().IsBlob() {
						var bytes []byte
						var err error
						bytes, err = json.Marshal(lFieldValue.Interface())
						if err != nil {
							logger.Errf("IsBlob", err)
							continue
						}
						lValue = bytes
					}
				} else {
					// any other
					logger.Err("other field type ", lName)
				}
			}
		}
		//logger.Dbg("field#2", lName, lFieldType, lFieldValue)
		lValue = lFieldValue.Interface()
		res_map[lName] = lValue

	CONTINUE:
	}

	return
}

// # transfer struct to Itf map and record model name if could
// #1 限制字段使用 2.添加Model
func (self *TSession) convertItf2ItfMap(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}

	// 创建 Map
	value_type := reflect.TypeOf(value)
	if value_type.Kind() == reflect.Ptr || value_type.Kind() == reflect.Struct {
		// # change model of the session
		if self.Statement.model == nil {
			model_name := fmtModelName(utils.Obj2Name(value))
			if model_name != "" {
				self.Model(model_name)
			}
		}

		return self.convertStruct2Itfmap(value)
	} else if value_type.Kind() == reflect.Map {
		if m, ok := value.(map[string]interface{}); ok {
			return m
		} else if m, ok := value.(map[string]string); ok {
			res_map := make(map[string]interface{})

			for key, val := range m {
				res_map[key] = val // 格式化为字段类型
			}

			return res_map
		}
	}

	return nil
}

//# the query may involve several tables: we need fully-qualified names
func (self *TSession) qualify(field IField, query *TQuery) string {
	res := self.Statement.inherits_join_calc(self.Statement.model.GetName(), field.Name(), query)
	/*
		if field.Type == "binary" { // && (context.get('bin_size') or context.get('bin_size_' + col)):
			//# PG 9.2 introduces conflicting pg_size_pretty(numeric) -> need ::cast
			res = fmt.Sprintf(`pg_size_pretty(length(%s)::bigint)`, res)
		}*/
	//utils.Dbg("qualify:", field.Name)
	return fmt.Sprintf(`%s as "%s"`, res, field.Name())
}

// TODO 缓存
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
