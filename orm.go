package orm

/*
	one2many:格式为one2many(关联表，字段) 表示该字段存储所有关联表对应字段为本Model的Id值的记录
	many2one:格式many2one(关联表) 用于外键关系，表示该字段对应关联表里的某个记录
	many2many:many2many(关联表，关联多对多表，该Model的字段，管理表字段)多对多一般关系存储于xxx_rel表里对应2个字段
*/
import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"go/token"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/cacher"
	"github.com/volts-dev/orm/core"
	"github.com/volts-dev/utils"
	"github.com/volts-dev/volts/logger"
)

var log = logger.New("orm")

const (
	NAMEDATALEN = 63
)

var (
	BlankStrItf interface{} = ""
	BlankNumItf interface{} = 0
)

type (
	// Conversion is an interface. A type implements Conversion will according
	// the custom method to fill into database and retrieve from database.
	// TODO 取缔
	Conversion interface {
		FromDB([]byte) error
		ToDB() ([]byte, error)
	}

	TOrm struct {
		config    *Config
		dialect   IDialect
		db        *core.DB
		osv       *TOsv // 对象管理
		nameIndex map[string]*TModel
		connected bool

		context context.Context
		// public
		Cacher *cacher.TCacher
	}
)

// TODO 使用函数配置参数
/*
 create a new ORM instance
*/
func New(opt ...Option) (*TOrm, error) {
	cfg := newConfig(opt...)
	dialect := QueryDialect(cfg.DataSource.DbType)
	if dialect == nil {
		return nil, fmt.Errorf("Unsupported dialect type: %v", cfg.DataSource.DbType)
	}

	db, err := core.Open(cfg.DataSource.DbType, cfg.DataSource.toString())
	if err != nil {
		return nil, err
	}

	err = dialect.Init(db, cfg.DataSource)
	if err != nil {
		return nil, err
	}

	log.Infof("Connected database %s", cfg.DataSource.DbName)

	orm := &TOrm{
		context:   context.Background(),
		config:    cfg,
		db:        db,
		dialect:   dialect,
		nameIndex: make(map[string]*TModel),
	}

	// Cacher
	orm.Cacher, err = cacher.New()
	if err != nil {
		log.Trace(err)
		return nil, err
	}

	// OSV
	orm.osv = newOsv(orm)

	if orm.IsExist(cfg.DataSource.DbName) {
		err = orm._reverse()
		if err != nil {
			return nil, err
		}
	} else {
		orm.connected = false
		log.Warnf("the orm is disconnected with database %s", cfg.DataSource.DbName)
	}

	return orm, nil
}

func (self *TOrm) Config() *Config {
	return self.config
}

// DriverName return the current sql driver's name
func (self *TOrm) DriverName() string {
	return self.dialect.DriverName()
}

// Ping tests if database is alive
func (self *TOrm) Ping() error {
	session := NewSession(self)
	defer session.Close()
	log.Infof("Ping database %s@%s", self.config.DataSource.DbName, self.DriverName())
	return session.Ping()
}

// close the entire orm engine
func (self *TOrm) Close() error {
	// TODO more
	return self.db.Close()
}

// TZTime change one time to time location
func (self *TOrm) FormatTimeZone(t time.Time) time.Time {
	if !t.IsZero() { // if time is not initialized it's not suitable for Time.In()
		return t.In(self.config.TimeZone)
	}
	return t
}

func (self *TOrm) FormatModelName(name string) string {
	return fmtModelName(name)
}

func (self *TOrm) FormatTableName(name string) string {
	return fmtTableName(name)
}

// format the field name to the same
func (self *TOrm) FormatFieldName(name string) string {
	return fmtFieldName(name)
}

// @classic_mode : 使用Model实例为基础
func (self *TOrm) NewSession(classic_mode ...bool) *TSession {
	session := NewSession(self)

	if len(classic_mode) > 0 {
		session.IsClassic = classic_mode[0]
	}

	return session
}

// 使用Model实例为基础
func (self *TOrm) Model(modelName string) *TSession {
	session := NewSession(self)
	session.IsAutoClose = true
	return session.Model(modelName)
}

// QuoteStr Engine's database use which charactor as quote.
// mysql, sqlite use ` and postgres use "
func (self *TOrm) Quote(v string) string {
	return self.dialect.Quoter().Quote(v)
}

// FormatTime format time
func (self *TOrm) FormatTime(sqlTypeName string, t time.Time) (v interface{}) {
	return self._formatTime(self.config.TimeZone, sqlTypeName, t)
}

// FIXME 使用o2o需要插入顺序
// # 插入一个新的Table并创建
// 同步更新Model 并返回同步后表 <字段>
// region 区分相同Model名称来自哪个模块，等级
func (self *TOrm) SyncModel(region string, models ...IModel) (modelNames []string, err error) {
	if models == nil {
		return
	}

	session := NewSession(self)
	defer session.Close()
	session.Begin()

	modelNames, err = session.SyncModel(region, models...)
	if err != nil {
		return nil, err
	}

	if err = session.Commit(); err != nil {
		if err = session.Rollback(err); err != nil {
			return nil, err
		}
	}

	return modelNames, nil
}

func (self *TOrm) HasModel(name string) bool {
	return self.osv.HasModel(name)
}

// get model object from the orm which registed
func (self *TOrm) GetModel(modelName string, opts ...ModelOption) (model IModel, err error) {
	return self.osv.GetModel(modelName, opts...)
}

// return the mane of all models
func (self *TOrm) GetModels() []string {
	return self.osv.GetModels()
}

// SetConnMaxLifetime sets the maximum amount of time a connection may be reused.
func (self *TOrm) SetConnMaxLifetime(d time.Duration) {
	self.db.SetConnMaxLifetime(d)
}

// Import SQL DDL file
func (self *TOrm) Import(r io.Reader) ([]sql.Result, error) {
	var results []sql.Result
	var lastError error
	scanner := bufio.NewScanner(r)

	semiColSpliter := func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}

		if i := bytes.IndexByte(data, ';'); i >= 0 {
			return i + 1, data[0:i], nil
		}

		// If we're at EOF, we have a final, non-terminated line. Return it.
		if atEOF {
			return len(data), data, nil
		}

		// Request more data.
		return 0, nil, nil
	}

	scanner.Split(semiColSpliter)

	for scanner.Scan() {
		query := strings.Trim(scanner.Text(), " \t\n\r")
		if len(query) > 0 {
			log.Info(query)
			result, err := self.Exec(query)
			results = append(results, result)
			if err != nil {
				return nil, err
				//lastError = err
			}
		}
	}

	return results, lastError
}

// Is the orm connected database
func (self *TOrm) Connected() bool {
	return self.connected
}

func (self *TOrm) IsIndexExist(tableName string, idxName string, unique bool) (bool, error) {
	session := NewSession(self)
	defer session.Close()
	return session.IsIndexExist(tableName, idxName, unique)
}

// If a table has any reocrd
func (self *TOrm) IsTableEmpty(tableName string) (bool, error) {
	session := NewSession(self)
	defer session.Close()
	return session.IsEmpty(tableName)
}

// If a table is exist
func (self *TOrm) IsTableExist(tableName string) (bool, error) {
	session := NewSession(self)
	defer session.Close()
	return session.IsExist(tableName)
}

// If a database is exist
func (self *TOrm) IsExist(name string) bool {
	return self.dialect.IsDatabaseExist(self.context, name)
}

// 删除表
func (self *TOrm) DropTables(names ...string) error {
	session := NewSession(self)
	defer session.Close()

	err := session.Begin()
	if err != nil {
		return err
	}

	for _, model := range names {
		err = session.DropTable(model)
		if err != nil {
			return session.Rollback(err)
		}
	}

	if err := session.Commit(); err != nil {
		return err
	}

	log.Infof("Drop model table %s success!", names)
	return nil
}

// TODO 根据表依赖关系顺序创建表
// CreateTables create tabls according bean
func (self *TOrm) CreateTables(names ...string) error {
	session := NewSession(self)
	defer session.Close()

	err := session.Begin()
	if err != nil {
		return err
	}

	for _, model := range names {
		err = session.CreateTable(model)
		if err != nil {
			return session.Rollback(err)
		}
	}

	if err := session.Commit(); err != nil {
		return err
	}

	log.Infof("Create model table %s success!", names)
	return nil
}

// create database
func (self *TOrm) CreateDatabase(name string) error {
	err := self.dialect.CreateDatabase(self.db.DB, self.context, name)
	if err != nil {
		return err
	}

	log.Infof("Create Database %s success!", name)
	return nil
}

// build the indexes for model
func (self *TOrm) CreateIndexes(modelName string) error {
	session := NewSession(self)
	defer session.Close()
	return session.CreateIndexes(modelName)
}

// build the uniques for model
func (self *TOrm) CreateUniques(modelName string) error {
	session := NewSession(self)
	defer session.Close()
	return session.CreateUniques(modelName)
}

// DBMetas Retrieve all tables, columns, indexes' informations from database.
// 从连接的数据库获取数据库及表基本信息
func (self *TOrm) DBMetas() (map[string]IModel, error) {
	models, err := self.dialect.GetModels(self.context)
	if err != nil {
		return nil, err
	}

	modelLst := make(map[string]IModel)
	for _, model := range models {
		model, err = self._modelMetas(model)
		if err != nil {
			return nil, err
		}
		/* TODO 搞清楚Indexes作用
		for _, index := range indexes {
			for _, name := range index.Cols {
				if field := model.GetFieldByName(name); field != nil {
					field.Base().Indexes[index.Name] = IndexType
				} else {
					return nil, fmt.Errorf("Unknown field "+name+" in indexes %v of model", index, model.GetColumnsSeq())
				}
			}
		}
		*/
		modelLst[model.String()] = model
	}

	return modelLst, nil
}

func (self *TOrm) Query(sql string, params ...interface{}) (*TDataset, error) {
	session := NewSession(self)
	defer session.Close()
	return session.Query(sql, params...)
}

// Exec raw sql directly
func (self *TOrm) Exec(sql string, params ...interface{}) (sql.Result, error) {
	session := NewSession(self)
	defer session.Close()
	return session.Exec(sql, params...)
}

// # 映射结构体与表
func (self *TOrm) _mapping(model interface{}) (*TModel, error) {
	model_value := reflect.Indirect(reflect.ValueOf(model))
	model_type := model_value.Type()
	if model_type.Kind() != reflect.Struct {
		return nil, fmt.Errorf("please make sure model is a struct! but not %v@%v", model_type.Name(), model_type.String())
	}

	object_name := utils.Obj2Name(model)
	model_name := fmtModelName(object_name)
	model_alt_name := model_name // model别名,当Model使用别名Tag时作用
	model_object := self.osv.newObject(model_name)

	res_model := newModel(model_name, "", model_value, model_type, nil) // 不检测是否已经存在于ORM中 直接替换旧
	res_model.obj = model_object
	res_model.isCustomModel = true

	var (
		err             error
		field           IField
		field_name      string
		field_tag       string
		field_value     reflect.Value
		field_type      reflect.Type
		field_type_name string
		sql_type        SQLType
		member_name     string
		tagMaps         map[string][]string // 记录Tag的
		tagsOrder       []string
		isSuper         bool
		tagCtx          *TTagContext
	)

	for i := 0; i < model_type.NumField(); i++ {
		{ // 初始化循环变量值
			isSuper = false
			field_type_name = ""
		}

		member_name = model_type.Field(i).Name

		// filter out the unexport field
		if !token.IsExported(member_name) {
			continue
		}

		field_name = fmtFieldName(member_name)
		field_value = model_value.Field(i)
		field_type = model_type.Field(i).Type
		field_tag = string(model_type.Field(i).Tag)

		if field_tag == "" {
			tagMaps = nil
			tagsOrder = nil

			// # 忽略无Tag的匿名继承结构
			if member_name == field_type.Name() {
				isSuper = true
				// TODO 验证继承的加载合法情况
				continue
			}
		} else {
			// 识别拆分Tag字符串
			tagMaps = make(map[string][]string)
			tagsOrder = make([]string, 0)

			var is_table_tag bool
			tag_str := lookup(string(field_tag), self.config.FieldIdentifier)
			if tag_str == "" {
				tag_str = lookup(string(field_tag), self.config.TableIdentifier)
				is_table_tag = true
				isSuper = true
			}

			// 识别分割Tag各属性
			tags := splitTag(tag_str)

			// 排序Tag并_确保优先执行字段类型属性
			var type_name string
			var attrs []string
			for _, key := range tags {
				//****************************************
				//NOTE 以下代码是为了避免解析不规则字符串为字段名提醒使用者规范Tag数据格式应该注意不用空格
				attrs = parseTag(key)

				// 验证
				if len(attrs) == 0 {
					return nil, fmt.Errorf("Tag parse failed: Model:%s Field:%s Tag:%s Key:%s Result:%v", model_name, field_name, field_tag, key, attrs)
				}
				type_name = strings.ToLower(attrs[0])
				tag_str = strings.Replace(key, type_name, "", 1) // 去掉Tag Item
				tag_str = strings.TrimLeft(tag_str, "(")
				tag_str = strings.TrimRight(tag_str, ")")
				ln := len(tag_str)
				if ln > 0 {
					if strings.Index(tag_str, " ") != -1 {
						if !strings.HasPrefix(tag_str, "'") &&
							!strings.HasSuffix(tag_str, "'") {
							return nil, fmt.Errorf("Model %s's %s tags could no including space ' ' in brackets value whicth it not 'String' type.", model_name, strings.ToUpper(field_name))
						}
					}
				}
				//****************************************
				tagMaps[type_name] = attrs[1:] //
				tagsOrder = append(tagsOrder, type_name)

				if !is_table_tag && IsFieldType(type_name) {
					field_type_name = type_name
				}
			}
		}

		// 更新超级类
		if isSuper {
			// TODO 考虑是否需要
			//	aaa := field_value.Addr().Interface().(IModel)
			//res_model.super = aaa // TODO 带路径的类名称
		}

		// # 忽略TModel类字段
		tagCtx = &TTagContext{
			Orm:            self,
			Model:          res_model,
			FieldTypeValue: field_value,
			ModelValue:     model_value,
		}

		if strings.Index(strings.ToLower(member_name), "tmodel") != -1 {
			// 执行tag处理
			err = self._handleTags(tagCtx, tagMaps, tagsOrder, "table")
			if err != nil {
				return nil, err
			}

			// 更新model新名称 并传递给其他Field
			if res_model.String() != model_alt_name {
				model_alt_name = res_model.String()
			}
		} else {
			//if column = model_object.GetFieldByName(lColName); column == nil {
			field = res_model.GetFieldByName(field_name)
			if field == nil {
				/* 由GO数据类型转为ORM数据类型 */
				sql_type = GoType2SQLType(field_type)
				field, err = NewField(field_name, WithSQLType(sql_type))
				if err != nil {
					return nil, err
				}

			} else {
				//** 如果是继承的字段则替换
				//原因：Join时导致Select到的字段为关联表字段而获取不到原本Model的字段如Id,write_time...
				if field.IsInheritedField() {
					//# 共同重叠字段
					//# 将关联表字段移入重叠字段表
					// 将现有表字段添加进重叠字段
					res_model.obj.SetCommonFieldByName(field_name, field.ModelName(), field) // 添加Parent表共同字段

					//#替换掉关联字段并添加到重叠字段表里,表示该字段继承的关联表也有.
					new_fld := utils.Clone(field).(IField)
					new_fld.SetBase(field.Base())

					// 添加model表共同字段
					new_fld.Base().model_name = model_alt_name
					new_fld.Base().isInheritedField = false                                      // # 共同字段非外键字段
					new_fld.Base()._attr_store = true                                            // # 存储共同字段非外键字段
					res_model.obj.SetCommonFieldByName(field_name, new_fld.ModelName(), new_fld) // 将现有表字段添加进重叠字段
					field = new_fld
				}
			}

			// # 根据Tag创建字段
			// 尝试获取新的Field以替换
			if IsFieldType(field_type_name) { // # 当属性非忽略或者BaseModel
				if field == nil || (field.Type() != field_type_name) { // #字段实例为空 [或者] 字段类型和当前类型不一致时重建字段实例
					field, err = NewField(field_name, WithFieldType(field_type_name)) // 根据Tag里的 属性类型创建Field实例
					if err != nil {
						return nil, err
					}
				}
			}

			// # check again 根据Tyep创建字段
			if field == nil { // 必须确保上面的代码能获取到定义的字段类型
				return nil, errors.New("must difine the field type for the model field :" + model_name + "." + field_name)
			}

			if field.SQLType().Name == "" {
				field.Base().SqlType = GoType2SQLType(field_type)
			}

			// 更新model新名称
			field.Base().model_name = model_alt_name

			/* 执行field初始化 */
			tagCtx.Field = field
			tagCtx.Params = tagMaps[field.Type()]
			field.Init(tagCtx)

			/* 同步model和数据库的SqlType */
			self.dialect.SyncToSqlType(tagCtx)

			// 执行tag处理
			err := self._handleTags(tagCtx, tagMaps, tagsOrder, "")
			if err != nil {
				return nil, err
			}

			if field.Base().isAutoIncrement && field.Base().isPrimaryKey {
				res_model.idField = field.Name()
			}

			if field.Base()._attr_size == 0 {
				field.Base()._attr_size = field.SQLType().DefaultLength
			}

			//if field.Base().Length2 == 0 {
			//	field.Base().Length2 = field.SQLType().DefaultLength2
			//}

			// # 设置Help
			if field.Title() == "" {
				field.Base()._attr_title = utils.TitleCasedNameWithSpace(field.Name())
			}

			if field.Base()._attr_help == "" && field.Title() != "" {
				field.Base()._attr_help = field.Title()
			}

			// REmove #　通过条件过滤不学要的原始字段
			/*
				if field.IsColumn() && field.Base()._attr_store && field.SQLType().Name != "" {
					//if !is_exit_col {
					////table.AddColumn(column)
					//} else {
					//	log.Dbg("is_exit_col", column.Name)
					//}

					// 为字段添加数据库字段属性
					////field.Base().column = column
					field.Base().isColumn = true
				}
			*/

			//field.UpdateDb(tagCtx)

			// 添加字段进Table
			if field.Type() != "" && field.Name() != "" {
				res_model.obj.SetFieldByName(field_name, field) // !!!替代方式
			}
		}
	}

	return res_model, nil
}

// TODO 保持表实时更新到ORM - 由于有些表是由SQL后期创建 导致Orm里缓存不存在改表Sycn时任然执行创建而抛出错误
// 更新现有数据库以及表信息并模拟创建TModel
// 反转Table 到 Model
func (self *TOrm) _reverse() error {
	models, err := self.DBMetas()
	if err != nil {
		return err
	}

	for _, model := range models {

		/* remove
		model, err := self.GetModel(mod.GetName())
		if err != nil {
			return err
		}

		if model == nil {
			model_name := mod.GetName() // strings.Replace(mod.Name, "_", ".", -1)
			model_val := reflect.Indirect(reflect.ValueOf(new(TModel)))
			model_type := model_val.Type()

			// new a base model instance
			model := newModel(model_name, model_val, model_type)
			model.obj = self.osv.newObject(model_name)
			model.is_base = true
		}
		*/

		if err = self.osv.RegisterModel("", model.GetBase()); err != nil {
			return err
		}
		log.Infof("table %s found in database!", model.Table())
	}

	return nil
}

func (self *TOrm) _modelMetas(model IModel) (IModel, error) {
	modelName := model.String()
	tableName := model.Table()
	modelObject := self.osv.newObject(model.String())
	model.GetBase().obj = modelObject
	model.GetBase().isCustomModel = false

	colSeq, fields, err := self.dialect.GetFields(self.context, tableName)
	if err != nil {
		return nil, err
	}

	// TODO 充实model pk id 等选项
	for _, name := range colSeq {
		if field, has := fields[name]; has {
			field.Base().model_name = modelName
			// 主键三大特征
			// TODO 与复合主键中找到ID主键
			if !field.IsCompositeKey() && field.IsPrimaryKey() && field.Required() && (field.IsUnique() || field.IsAutoIncrement()) {
				model.IdField(field.Name())
				model.Obj().uidFieldName = field.Name()
			}

			if field.Base()._attr_type == "" {
				field.Base()._attr_type = field.SQLType().Name
			}

			/*
				//没有起到作用
				//无法鉴别来自数据库的字段是否id字段或者name字段
				switch f := field.(type) {
				case *TIdField:
					model.IdField(f.Name())
					model.Obj().uidFieldName = f.Name()
				case *TNameField:
					model.NameField(f.Name())
					model.Obj().nameField = f.Name()
				}
			*/

			field.Base().isColumn = true
			// 数据库的字段都是存储类型
			field.Base()._attr_store = true

			modelObject.fields.Store(field.Name(), field)
		}
	}

	indexes, err := self.dialect.GetIndexes(self.context, tableName)
	if err != nil {
		return nil, err
	}

	modelObject.indexes = indexes

	return model, nil
}

func (self *TOrm) _formatTime(tz *time.Location, sqlTypeName string, t time.Time) (v interface{}) {
	if self.dialect.DBType() == ORACLE {
		return t
	}
	if tz != nil {
		t = self.FormatTimeZone(t)
	}
	switch sqlTypeName {
	case Time:
		s := t.Format("2006-01-02 15:04:05") //time.RFC3339
		v = s[11:19]
	case Date:
		v = t.Format("2006-01-02")
	case DateTime, TimeStamp:
		if self.dialect.DBType() == "ql" {
			v = t
		} else if self.dialect.DBType() == "sqlite3" {
			v = t.UTC().Format("2006-01-02 15:04:05")
		} else {
			v = t.Format("2006-01-02 15:04:05")
		}
	case TimeStampz:
		if self.dialect.DBType() == MSSQL {
			v = t.Format("2006-01-02T15:04:05.9999999Z07:00")
		} else if self.dialect.DriverName() == "mssql" {
			v = t
		} else {
			v = t.Format(time.RFC3339Nano)
		}
	case BigInt, Int:
		v = t.Unix()
	default:
		v = t
	}
	return
}

// TODO
func (self *TOrm) _nowTime(sqlTypeName string) (res_val interface{}, res_time time.Time) {
	res_time = time.Now()
	if self.dialect.DBType() == ORACLE {
		return
	}

	if self.config.TimeZone != nil {
		res_time = res_time.In(self.config.TimeZone)

	}

	switch sqlTypeName {
	case Time:
		s := res_time.Format("2006-01-02 15:04:05") //time.RFC3339
		res_val = s[11:19]
	case Date:
		res_val = res_time.Format("2006-01-02")
	case DateTime, TimeStamp:
		if self.dialect.DBType() == "ql" {
			res_val = res_time
		} else if self.dialect.DBType() == "sqlite3" {
			res_val = res_time.UTC().Format("2006-01-02 15:04:05")
		} else {
			res_val = res_time.Format("2006-01-02 15:04:05")
		}
	case TimeStampz:
		if self.dialect.DBType() == MSSQL {
			res_val = res_time.Format("2006-01-02T15:04:05.9999999Z07:00")
		} else if self.dialect.DriverName() == "mssql" {
			res_val = res_time
		} else {
			res_val = res_time.Format(time.RFC3339Nano)
		}
	case BigInt, Int:
		res_val = res_time.Unix()
	default:
		res_val = res_time
	}
	return
}

func (self *TOrm) _handleTags(fieldCtx *TTagContext, tags map[string][]string, order []string, prefix string) error {
	if tags == nil {
		return nil
	}

	field := fieldCtx.Field
	var tag_str string
	var tagCtrl ITagController
	for _, tagName := range order {
		if field != nil && tagName == field.Type() {
			continue // 忽略该Tag
		}

		// 原始ORM映射,理论上无需再次解析只需修改Tag和扩展后的一致即可
		switch tagName {
		case "-": // 忽略某些继承者成员
			return nil
		default:
			// 切换到TableTag模式
			if prefix != "" {
				tag_str = prefix + "_" + tagName
			} else {
				tag_str = tagName // 获取属性名称
			}

			// 执行自定义Tag初始化
			if tagCtrl = GetTagControllerByName(tag_str); tagCtrl != nil {
				fieldCtx.Params = tags[tagName]
				if err := tagCtrl(fieldCtx); err != nil {
					return err
				}
			} else {
				// check not field type also
				if _, has := field_creators[tag_str]; has {
					// already init by field creators
					break
				}

				log.Warnf("Couldn't handle unknown tag < %s > on %s@%s", tag_str, fieldCtx.Model.String(), field.Name())
			}
		}
	}

	return nil
}

func (self *TOrm) _logExecSql(sql string, args []interface{}, executionBlock func() (sql.Result, error)) (sql.Result, error) {
	if self.config.ShowSql {
		b4ExecTime := time.Now()
		res, err := executionBlock()
		execDuration := time.Since(b4ExecTime)
		if len(args) > 0 {
			log.Infof("[SQL][%d ms] %s [args] %v", execDuration.Microseconds(), sql, args)
		} else {
			log.Infof("[SQL][%d ms] %s", execDuration.Microseconds(), sql)
		}
		return res, err
	} else {
		return executionBlock()
	}
}

func (self *TOrm) _logQuerySql(sql string, args []interface{}, executionBlock func() (*dataset.TDataSet, error)) (*dataset.TDataSet, error) {
	if self.config.ShowSql {
		b4ExecTime := time.Now()
		res, err := executionBlock()
		execDuration := time.Since(b4ExecTime)
		if len(args) > 0 {
			log.Infof("[SQL][%d ms] %s [args] %v", execDuration.Microseconds(), sql, args)
		} else {
			log.Infof("[SQL][%d ms] %s", execDuration.Microseconds(), sql)
		}
		return res, err
	} else {
		return executionBlock()
	}
}
