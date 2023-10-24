package orm

import (
	"errors"
	"strings"

	"github.com/volts-dev/orm/core"
	"github.com/volts-dev/utils"
)

type (
	TSession struct {
		orm *TOrm
		db  *core.DB
		tx  *core.Tx // 由Begin 传递而来

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

		// 存储Create创建时Name字段索引缓存供Many2One.OnWrite使用
		// @格式:[field]id
		CacheNameIds map[string]any
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
	self.CacheNameIds = nil
	return nil
}

func (self *TSession) New() *TSession {
	session := self.orm.NewSession()
	session.IsClassic = true
	return session.Model(self.Statement.model.String())
}

func (self *TSession) Clone() *TSession {
	session := self.orm.NewSession()
	session.tx = self.tx
	session.IsAutoCommit = self.IsAutoCommit // 默认情况下单个SQL是不用事务自动
	session.IsAutoClose = self.IsAutoClose
	session.AutoResetStatement = self.AutoResetStatement
	session.IsCommitedOrRollbacked = self.IsCommitedOrRollbacked
	session.Prepared = self.Prepared
	session.CacheNameIds = self.CacheNameIds
	return session
}

// Close release the connection from pool
func (self *TSession) Close() {
	if self.db != nil {
		// When Close be called, if session is a transaction and do not call
		// Commit or Rollback, then call Rollback.
		if self.tx != nil && !self.IsCommitedOrRollbacked {
			self.Rollback(nil)
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

// synchronize structs to database tables
func (self *TSession) SyncModel(region string, models ...IModel) (modelNames []string, err error) {
	// NOTE [SyncModel] 这里获取到的Model是由数据库信息创建而成.并不包含所有字段继承字段.
	exitsModels, err := self.orm.DBMetas() // 获取基本数据库信息
	if err != nil {
		return nil, err
	}

	modelNames = make([]string, 0)
	for _, mod := range models {
		model, err := self.orm.mapping(mod)
		if err != nil {
			return nil, err
		}
		if model == nil {
			continue
		}

		// 注册到对象服务
		err = self.orm.osv.RegisterModel(region, model)
		if err != nil {
			return nil, err
		}

		modelName := model.String()
		self.Model(modelName, region)        // #设置该Session的Model/Table
		exitsModel := exitsModels[modelName] // 数据库存在的
		if exitsModel == nil {
			model.BeforeSetup()

			// 如果数据库不存在改Model对应的表则创建
			//err = self.StoreEngine(s.Statement.StoreEngine).CreateTable(bean)
			if err = self.CreateTable(modelName); err != nil {
				return modelNames, err
			}

			if err = self.CreateUniques(modelName); err != nil {
				return modelNames, err
			}

			if err = self.CreateIndexes(modelName); err != nil {
				return modelNames, err
			}

			model.AfterSetup()
		} else {
			if err = self._alterTable(model, exitsModel.(*TModel)); err != nil {
				return modelNames, err
			}

			// 剔除已经修改保留未修改作为下面注册
			//delete(exitsModels, modelName)
		}

		modelNames = append(modelNames, modelName)
		delete(exitsModels, modelName)
	}

	// 确保获得其他没被注册的模型
	for _, m := range exitsModels {
		if !self.orm.osv.HasModel(m.String()) {
			if err = self.orm.osv.RegisterModel(region, m.(*TModel)); err != nil {
				return nil, err
			}
		}
	}

	return modelNames, nil
}

// return the orm instance
func (self *TSession) Orm() *TOrm {
	return self.orm
}

func (self *TSession) Models() *TSession {
	return self
}

// CreateTable create a table according a bean
// TODO考虑添加参数 达到INHERITS
func (self *TSession) CreateTable(model string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	mod, err := self.orm.GetModel(model)
	if err != nil {
		return err
	}

	self.Statement.model = mod

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

	res, err := self._exec(self.Statement.generate_create_table())
	if err != nil {
		return err
	}

	// 更新Model表
	if cnt, err := res.RowsAffected(); err == nil && cnt > 0 {
		self.orm.reverse()
	}

	return err
}

// CreateUniques create uniques
func (self *TSession) CreateUniques(model string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	mod, err := self.orm.GetModel(model)
	if err != nil {
		return err
	}

	self.Statement.model = mod
	for _, sql := range self.Statement.generate_unique() {
		_, err := self._exec(sql)
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

	mod, err := self.orm.GetModel(model)
	if err != nil {
		return err
	}

	self.Statement.model = mod

	sqls, err := self.Statement.generate_index()
	if err != nil {
		return err
	}

	for _, sql := range sqls {
		if _, err := self._exec(sql); err != nil {
			return err
		}
	}

	return nil
}

// drop table will drop table if exist, if drop failed, it will return error
func (self *TSession) DropTable(name string) (err error) {
	var needDrop = true
	if !self.orm.dialect.SupportDropIfExists() {
		sql, args := self.orm.dialect.TableCheckSql(name)
		results, err := self._query(sql, args...)
		if err != nil {
			return err
		}
		needDrop = results.Count() > 0
	}

	if needDrop {
		sql := self.orm.dialect.DropTableSql(name)
		res, err := self._exec(sql)
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

// TODO 缓存方式
func (self *TSession) Cache() *TSession {
	return self
}

// TODO 缓存
// NoCache ask this session do not retrieve data from cache system and
// get data from database directly.
func (self *TSession) Direct() *TSession {
	return self
}

// LastSQL returns last query information
func (self *TSession) LastSQL() (string, []interface{}) {
	return self.lastSQL, self.lastSQLArgs
}

// IsTableExist if a table is exist
func (self *TSession) IsExist(model ...string) (bool, error) {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	model_name := ""
	if len(model) > 0 {
		model_name = model[0]
	} else if self.Statement.model != nil {
		model_name = self.Statement.model.String()
	} else {
		return false, errors.New("model should not be blank")
	}

	tableName := strings.Replace(model_name, ".", "_", -1)
	tableName = utils.SnakeCasedName(tableName)
	sql, args := self.orm.dialect.TableCheckSql(tableName)
	lDs, err := self._query(sql, args...)
	if err != nil {
		return false, err
	}

	return lDs.Count() > 0, nil
}

func (self *TSession) IsIndexExist(tableName, idxName string, unique bool) (bool, error) {
	defer self._resetStatement()
	if self.IsAutoClose {
		defer self.Close()
	}
	/*
		var idx string
		if unique {
			idx = generate_index_name(UniqueType, tableName, []string{idxName})
		} else {
			idx = generate_index_name(IndexType, tableName, []string{idxName})
		}
	*/
	sqlStr, args := self.orm.dialect.IndexCheckSql(tableName, idxName)
	results, err := self._query(sqlStr, args...)

	// NOTE:数据库不予许不同表使用同一索引名称 索引名称必须是数据库唯一的
	// 如果出现relation "XXX" already exists并且results.Count()==0说明索引名称已经被占用！
	return results.Count() > 0, err
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

// 重制Statement防止参数重用
func (self *TSession) _resetStatement() {
	if self.AutoResetStatement {
		self.Statement.Init()
	}
}

/* #
* @model:提供新Session
* @newModel:Model映射后的新表结构
* @oldModel:当前数据库的表结构
 */
func (self *TSession) _alterTable(newModel, oldModel *TModel) (err error) {
	orm := self.orm
	tableName := newModel.table

	{ // 字段修改
		var cur_field IField
		for _, field := range newModel.GetFields() {
			cur_field = oldModel.GetFieldByName(field.Name())

			if cur_field != nil {
				expectedType := orm.dialect.GetSqlType(field)
				curType := orm.dialect.GetSqlType(cur_field)
				if expectedType != curType {
					//TODO 修改数据类型
					// 如果是修改字符串到
					if expectedType == Text && strings.HasPrefix(curType, Varchar) ||
						expectedType == Varchar && strings.HasPrefix(curType, Char) {
						log.Warnf("Table <%s> column <%s> change type from %s to %s\n", tableName, field.Name(), curType, expectedType)
						_, err = self.Exec(orm.dialect.ModifyColumnSql(tableName, field))

					} else if strings.HasPrefix(curType, Char) && strings.HasPrefix(expectedType, Varchar) {
						// 如果是同是字符串 则检查长度变化 for mysql
						if cur_field.Size() < field.Size() {
							log.Warnf("Table <%s> column <%s> change type from varchar(%d) to varchar(%d)\n", tableName, field.Name(), cur_field.Size(), field.Size())
							_, err = self.Exec(orm.dialect.ModifyColumnSql(tableName, field))
						}
						//}
						//其他
					} else {
						if !(strings.HasPrefix(curType, expectedType) && curType[len(expectedType)] == '(') {
							log.Warnf("Table <%s> column <%s> db type is <%s>, struct type is %s", tableName, field.Name(), curType, expectedType)
						}
					}

					if err != nil {
						log.Err(err)
						err = nil
					}
				}
				// 如果是同是字符串 则检查长度变化 for mysql
				//if orm.dialect.DBType() == MYSQL {
				if cur_field.Size() < field.Size() {
					log.Warnf("Table <%s> column <%s> change type from %s(%d) to %s(%d)\n",
						tableName, field.Name(), cur_field.SQLType().Name, cur_field.Size(), field.SQLType().Name, field.Size())
					_, err = self.Exec(orm.dialect.ModifyColumnSql(tableName, field))
					if err != nil {
						log.Err(err)
						err = nil
					}
				}
				//}

				//
				if strings.ToLower(field.Default()) != strings.ToLower(cur_field.Default()) {
					log.Warnf("nochange: Table <%s> Column <%s> db default is <%v>, model default is <%v>",
						tableName, field.Name(), cur_field.Default(), field.Default())
				}

				if field.Required() != cur_field.Required() {
					log.Warnf("nochange: Table <%s> Column <%s> db required is <%v>, model required is <%v>",
						tableName, field.Name(), cur_field.Required(), field.Required())
				}

				// 如果现在表无该字段则添加
			} else {
				/* 这里必须过滤掉 NOTE [SyncModel] 里提及的特殊字段 */
				if field.Store() && !field.IsInheritedField() {
					//session := self.orm.NewSession()
					//session.Model(newModel.String())
					//TODO # 修正上面指向错误Model
					//session.Statement.model = newModel
					err = self._addColumn(field.Name())
				}
				if err != nil {
					log.Err(err)
					err = nil
				}
			}
		}
	}

	{ // 表修改
		var foundIndexNames = make(map[string]bool)
		var addedNames = make(map[string]*TIndex)

		// TODO 主键是否可以修改

		// 检查更新索引 先取消索引载添加需要的
		// 取消Idex
		curIndexs := oldModel.GetIndexes()
		var existIndex *TIndex
		for name, index := range newModel.GetIndexes() {
			// 匹配数据库索引
			existIndex = curIndexs[name]

			// 现有的idex
			if existIndex != nil {
				if !existIndex.Equal(index) { // 类型不同则重新创建
					sql := orm.dialect.DropIndexUniqueSql(tableName, existIndex)
					if _, err = self.Exec(sql); err != nil {
						return err
					}
					addedNames[name] = index // 加入列表稍后再添加
				}
				foundIndexNames[name] = true
			} else {
				addedNames[name] = index // 加入列表稍后再添加
			}
		}

		// 清除已经作删除的索引
		for name, index := range curIndexs {
			if foundIndexNames[name] != true {
				sql := orm.dialect.DropIndexUniqueSql(tableName, index)
				if _, err = self.Exec(sql); err != nil {
					return err
				}
			}
		}

		// 重新添加索引
		for name, index := range addedNames {
			//session := orm.NewSession()
			//self.Model(newModel.String())
			//TODO # 修正上面指向错误Model
			//self.Statement.model = newModel
			if index.Type == UniqueType {
				err = self._addUnique(tableName, name)

			} else if index.Type == IndexType {
				err = self._addIndex(tableName, name)
			}

			if err != nil {
				return err
			}
		}

	}
	return
}

// 内部调用
func (self *TSession) _getModel(modelName string, origin ...string) (model IModel, err error) {
	model, err = self.orm.osv.GetModel(modelName, origin...)

	/* 继承事务状态 */
	if model != nil {
		// 详情查看Begin()初始化流程
		s := model.Records()
		s.IsAutoCommit = self.IsAutoCommit
		s.IsCommitedOrRollbacked = self.IsCommitedOrRollbacked
		s.tx = self.tx
		model.Tx(s)
	}

	return
}

func (self *TSession) _addColumn(colName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	col := self.Statement.model.GetFieldByName(colName)
	sql, args := self.Statement.generate_add_column(col)
	_, err := self._exec(sql, args...)
	return err
}

func (self *TSession) _addIndex(tableName, idxName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}

	index := self.Statement.model.GetIndexes()[idxName]
	sql := self.orm.dialect.CreateIndexUniqueSql(tableName, index)
	_, err := self._exec(sql)
	return err
}

func (self *TSession) _addUnique(tableName, uqeName string) error {
	defer self.Statement.Init()
	if self.IsAutoClose {
		defer self.Close()
	}
	index := self.Statement.model.GetIndexes()[uqeName]
	sql := self.orm.dialect.CreateIndexUniqueSql(tableName, index)
	_, err := self._exec(sql)
	return err
}
