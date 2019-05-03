package orm

/*
	one2many:格式为one2many(关联表，字段) 表示该字段存储所有关联表对应字段为本Model的Id值的记录
	many2one:格式many2one(关联表) 用于外键关系，表示该字段对应关联表里的某个记录
	many2many:many2many(关联表，关联多对多表，该Model的字段，管理表字段)多对多一般关系存储于xxx_rel表里对应2个字段
*/
import (
	"bufio"
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"volts-dev/dataset"

	core "github.com/go-xorm/core"
	"github.com/volts-dev/utils"
)

const (
	NAMEDATALEN = 63
)

var (
	BlankStrItf interface{} = ""
	BlankNumItf interface{} = 0

	DbType       string = "postgres"
	DbUser       string = "postgres"
	DbPassword   string = "postgres"
	DbMasterHost string = "localhost:5432"
	DbSlaveHost  string = "localhost:5432"
	// pg:only "require" (default), "verify-full", and "disable" supported
	DbSSLMode   string = "disable" // 字符串
	TestShowSql bool   = true

	cacher_max_item = 1000
	cacher_gc_time  = 15
)

type (
	TOrm struct {
		db              *core.DB
		osv             *TOsv // 对象管理
		dialect         core.Dialect
		dataSource      *DataSource
		FieldIdentifier string // 字段 tag 标记
		TableIdentifier string // 表 tag 标记
		TimeZone        *time.Location
		//models          map[reflect.Type]*TModel
		models map[string]*TModel
		tables map[string]*core.Table // #作为非经典模式参考
		//field_ctrl      map[string]IFieldCtrl // 废弃
		tag_ctrl map[string]ITagController // 废弃
		//nameIndex map[string]*TTable
		nameIndex map[string]*TModel
		//DBName  string      // 绑定的数据库名称
		//DBRead  *orm.Engine // 读写分离
		//DBWrite *orm.Engine // 读写分离

		// #cacher
		cacher *TCacher // TODO 大写

		// #logger
		//logger         *logger.TLogger //TODO 接口
		_show_sql      bool
		_show_sql_time bool
	}
)

/*
 create a new ORM instance
*/
func NewOrm(dataSource *DataSource) (res_orm *TOrm, err error) {
	//func NewOrm(db_driver string, db_src string) (res_orm *TOrm, err error) {
	reg_drvs_dialects()
	db_driver := dataSource.DbType
	db_src := dataSource.toString()
	lDriver := core.QueryDriver(db_driver)
	if lDriver == nil {
		return nil, fmt.Errorf("Unsupported driver name: %v", db_driver)
	}

	lUri, err := lDriver.Parse(db_driver, db_src)
	if err != nil {
		return nil, err
	}

	lDialect := core.QueryDialect(lUri.DbType)
	if lDialect == nil {
		return nil, fmt.Errorf("Unsupported dialect type: %v", lUri.DbType)
	}

	lDb, err := core.Open(db_driver, db_src)
	if err != nil {
		return nil, err
	}

	err = lDialect.Init(lDb, lUri, db_driver, db_src)
	if err != nil {
		return nil, err
	}

	res_orm = &TOrm{
		db:              lDb,
		dialect:         lDialect,
		dataSource:      dataSource,
		FieldIdentifier: "field",
		TableIdentifier: "table",
		models:          make(map[string]*TModel),
		tables:          make(map[string]*core.Table),
		nameIndex:       make(map[string]*TModel),
		//field_ctrl:      make(map[string]IFieldCtrl),
		//tag_ctrl:        make(map[string]func(model *TModel, fld_val reflect.Value, fld *TField, col *core.Column, arg ...string)),
		TimeZone: time.Local,
	}

	// Cacher
	res_orm.cacher = NewCacher()

	// OSV
	res_orm.osv = NewOsv(res_orm)

	res_orm.reverse()
	return
}

// TODO 保持表实时更新到ORM - 由于有些表是由SQL后期创建 导致Orm里缓存不存在改表Sycn时任然执行创建而抛出错误
// 更新现有数据库以及表信息并模拟创建TModel
// 反转Table 到 Model
func (self *TOrm) reverse() error {
	lTables, err := self.DBMetas()
	if err != nil {
		return err
	}

	for _, tb := range lTables {
		logger.Infof("%s found!", tb.Name)

		if _, has := self.tables[tb.Name]; !has {
			self.tables[tb.Name] = tb
			model_name := strings.Replace(tb.Name, "_", ".", -1)
			model_val := reflect.Indirect(reflect.ValueOf(new(TModel)))
			model_type := model_val.Type()

			// new a base model instance
			model := NewModel(model_name, model_val, model_type)
			model.table = tb // piont to the table
			model.is_base = true
			self.models[model_name] = model

			// init all columns to the model
			FieldContext := new(TFieldContext)
			for _, col := range tb.Columns() {
				field := self._newFieldFromSqlType(col.Name, col)
				//logger.Dbg("cccccccccc", field, col.Name, *col)
				field.Base().model_name = model_name

				FieldContext.Orm = self
				FieldContext.Model = model
				FieldContext.Column = col
				FieldContext.Field = field
				FieldContext.Params = nil
				//logger.Dbg("tagMap ", lField.Type(), tagMap[lField.Type()])
				field.Init(FieldContext)

				if col.IsAutoIncrement && col.IsPrimaryKey {
					model.RecordField = field
				}

				// # 设置Help
				if field.Title() == "" {
					field.Base()._attr_title = utils.TitleCasedNameWithSpace(field.Name())
				}

				if field.Base()._attr_help == "" && field.Title() != "" {
					field.Base()._attr_help = field.Title()
				}

				// 为字段添加数据库字段属性
				field.Base().column = col
				model._fields[col.Name] = field
			}
		}
	}

	return nil
}

// DriverName return the current sql driver's name
func (self *TOrm) DriverName() string {
	return self.dialect.DriverName()
}

// Ping tests if database is alive
func (self *TOrm) Ping() error {
	session := self.NewSession()
	defer session.Close()
	logger.Infof("PING DATABASE %v", self.DriverName())
	return session.Ping()
}

// close the entire orm engine
func (self *TOrm) Close() error {
	return self.db.Close()
}

// TZTime change one time to xorm time location
func (self *TOrm) FormatTimeZone(t time.Time) time.Time {
	if !t.IsZero() { // if time is not initialized it's not suitable for Time.In()
		return t.In(self.TimeZone)
	}
	return t
}

func (self *TOrm) ModelByName(name string) *TModel {
	//return self.nameIndex[name]
	return self.models[name]
}

func (self *TOrm) __ModelByType(t reflect.Type) *TModel {
	//return self.models[t]
	return nil
}

// TODO 更新表信息
func (self *TOrm) _updateTable(table string) {
	//for self.Engine.Dialect().GetColumns()
}

// @classic_mode : 使用Model实例为基础
func (self *TOrm) NewSession(classic_mode ...bool) *TSession {
	session := &TSession{
		db:  self.db,
		orm: self,
	}

	if len(classic_mode) > 0 {
		session.IsClassic = classic_mode[0]
	}

	session.init()
	return session
}

// @classic_mode : 使用Model实例为基础
func (self *TOrm) Model(name string) *TSession {
	session := self.NewSession()
	session.IsAutoClose = true
	return session.Model(name)
}

// 返回数据库所有表对象
func (self *TOrm) Tables() map[string]*core.Table {
	return self.tables
}

// QuoteStr Engine's database use which charactor as quote.
// mysql, sqlite use ` and postgres use "
func (self *TOrm) QuoteStr() string {
	return self.dialect.QuoteStr()
}

func (self *TOrm) Quote(keyName string) string {
	return self._fmt_quote(keyName)
}

func (self *TOrm) log_exec_sql(sql string, args []interface{}, executionBlock func() (sql.Result, error)) (sql.Result, error) {
	if self._show_sql {
		b4ExecTime := time.Now()
		res, err := executionBlock()
		execDuration := time.Since(b4ExecTime)
		if len(args) > 0 {
			logger.Infof("[SQL][%vns] %s [args] %v", execDuration.Nanoseconds(), sql, args)
		} else {
			logger.Infof("[SQL][%vns] %s", execDuration.Nanoseconds(), sql)
		}
		return res, err
	} else {
		return executionBlock()
	}
}

// # 将Core自动转换的数据库类型转换成ORM数据类型
func (self *TOrm) _newFieldFromSqlType(name string, col *core.Column) IField {
	switch col.SQLType.Name {
	case core.Bit, core.TinyInt, core.SmallInt, core.MediumInt, core.Int, core.Integer, core.Serial:
		return NewField(name, "int")

	case core.BigInt, core.BigSerial:
		return NewField(name, "bigint")

	case core.Float, core.Real:
		return NewField(name, "float")

	case core.Double:
		return NewField(name, "double")

	case core.Char, core.Varchar, core.NVarchar, core.TinyText, core.Enum, core.Set, core.Uuid, core.Clob:
		return NewField(name, "char")

	case core.Text, core.MediumText, core.LongText:
		return NewField(name, "text")

	case core.Decimal, core.Numeric:
		return NewField(name, "char")

	case core.Bool:
		return NewField(name, "bool")

	case core.DateTime, core.Date, core.Time, core.TimeStamp, core.TimeStampz:
		return NewField(name, "datetime")

	case core.TinyBlob, core.Blob, core.LongBlob, core.Bytea, core.Binary, core.MediumBlob, core.VarBinary:
		return NewField(name, "binary")
	}
	return nil
}

func (self *TOrm) log_query_sql(sql string, args []interface{}, executionBlock func() (*dataset.TDataSet, error)) (*dataset.TDataSet, error) {
	if self._show_sql {
		b4ExecTime := time.Now()
		res, err := executionBlock()
		execDuration := time.Since(b4ExecTime)
		if len(args) > 0 {
			logger.Infof("[SQL][%vns] %s [args] %v", execDuration.Nanoseconds(), sql, args)
		} else {
			logger.Infof("[SQL][%vns] %s", execDuration.Nanoseconds(), sql)
		}
		return res, err
	} else {
		return executionBlock()
	}
}

// contains reports whether the string contains the byte c.
func contains(s string, c byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return true
		}
	}
	return false
}

// #修改支持多行的Tag
// Unquote interprets s as a single-quoted, double-quoted,
// or backquoted Go string literal, returning the string value
// that s quotes.  (If s is single-quoted, it would be a Go
// character literal; Unquote returns the corresponding
// one-character string.)
func unquote(s string) (string, error) {
	n := len(s)
	if n < 2 {
		return "", errors.New("invalid quoted string")
	}
	quote := s[0]
	if quote != s[n-1] {
		return "", errors.New("lost the quote symbol on the end")
	}
	s = s[1 : n-1]

	if quote == '`' {
		if contains(s, '`') {
			return "", errors.New("the '`' symbol is found on the content")
		}
		return s, nil
	}

	if quote != '"' && quote != '\'' {
		return "", errors.New("lost the quote symbol on the begin")
	}

	//if contains(s, '\n') {
	//	//Println("contains(s, '\n')")
	//	return "contains(s, '\n')", strconv.ErrSyntax
	//}

	// Is it trivial?  Avoid allocation.
	if !contains(s, '\\') && !contains(s, quote) {
		switch quote {
		case '"':
			return s, nil
		case '\'':
			r, size := utf8.DecodeRuneInString(s)
			if size == len(s) && (r != utf8.RuneError || size != 1) {
				return s, nil
			}
		}
	}

	var runeTmp [utf8.UTFMax]byte
	buf := make([]byte, 0, 3*len(s)/2) // Try to avoid more allocations.
	for len(s) > 0 {
		c, multibyte, ss, err := strconv.UnquoteChar(s, quote)
		if err != nil {
			return "", err
		}
		s = ss
		if c < utf8.RuneSelf || !multibyte {
			buf = append(buf, byte(c))
		} else {
			n := utf8.EncodeRune(runeTmp[:], c)
			buf = append(buf, runeTmp[:n]...)
		}
		if quote == '\'' && len(s) != 0 {
			// single-quoted must be single character
			return "", strconv.ErrSyntax
		}
	}
	return string(buf), nil
}

func lookup(tag string, key ...string) (value string) {
	// When modifying this code, also update the validateStructTag code
	// in golang.org/x/tools/cmd/vet/structtag.go.

	for tag != "" {
		// Skip leading space.
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}

		// Scan to colon. A space, a quote or a control character is a syntax error.
		// Strictly speaking, control chars include the range [0x7f, 0x9f], not just
		// [0x00, 0x1f], but in practice, we ignore the multi-byte control characters
		// as it is simpler to inspect the tag's bytes than the tag's runes.
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' && tag[i] != 0x7f {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := string(tag[:i])
		tag = tag[i+1:]
		//fmt.Println("tag", tag)
		// Scan quoted string to find value.
		i = 1
		for i < len(tag) && (tag[i] != '"' || (i+1 < len(tag) && tag[i+1] != ' ' && tag[i] == '"')) {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		//fmt.Println("tag", tag)
		qvalue := string(tag[:i+1])
		tag = tag[i+1:]
		//fmt.Println("key", key, name, qvalue)
		if utils.InStrings(name, key...) != -1 {
			value, err := unquote(qvalue)
			if err != nil {
				logger.Errf("parse Tag error: %s, %s : %s", qvalue, value, err.Error())
				break
			}
			return value
		}
	}
	return ""
}

func splitTag(tag string) (tags []string) {
	tag = strings.TrimSpace(tag)
	var hasQuote = false
	var lastIdx = 0
	for i, t := range tag {
		if t == '\'' { // #  t == '(' || t == ')' { //
			hasQuote = !hasQuote
		} else if t == ' ' {
			if lastIdx < i && !hasQuote {
				newtag := strings.TrimSpace(tag[lastIdx:i])

				// #去除换行缩进的空格
				if newtag != "" {
					tags = append(tags, newtag)
				}

				lastIdx = i + 1
			}
		}
	}
	if lastIdx < len(tag) {
		tags = append(tags, strings.TrimSpace(tag[lastIdx:len(tag)]))
	}
	return
}

// TODO 支持Vals 包含空格 one2many(product.attribute.price,空格value_id)
// #　解析 'Tag(vals)' 整个字符串
func parseTag(tag string) (tags []string) {
	tag = strings.TrimSpace(tag)
	var (
		hasQuote          = false
		hasSquareBrackets = false
		//Bracket           = false
		lastIdx = 0
		l       = len(tag)
	)
	for i, t := range tag {
		if t == '\'' {
			hasQuote = !hasQuote
		}
		//fmt.Println(t,i)
		if t == '[' || t == ']' {
			hasSquareBrackets = !hasSquareBrackets
		} else if t == '(' || t == ',' || t == ')' { //处理 Tag(xxx)类型
			//if t == '(' && !Bracket {
			//	Bracket = true
			//}

			if lastIdx < i && !hasQuote && !hasSquareBrackets {
				//tags = append(tags, strings.Trim(strings.TrimSpace(tag[lastIdx:i]), "'"))
				tags = append(tags, strings.TrimSpace(tag[lastIdx:i]))
				lastIdx = i + 1
			}
		} else if i+1 == l { // 处理无括号类型的Tag
			tags = append(tags, strings.TrimSpace(tag[lastIdx:l]))
		}

	}
	//if lastIdx < len(tag) {
	//	tags = append(tags, strings.TrimSpace(tag[lastIdx:len(tag)]))
	//}
	return
}

func (self *TOrm) _fmt_quote(keyName string) string {
	keyName = strings.TrimSpace(keyName)
	if len(keyName) == 0 {
		return keyName
	}

	if string(keyName[0]) == self.dialect.QuoteStr() || keyName[0] == '`' {
		return keyName
	}

	keyName = strings.Replace(keyName, ".", self.dialect.QuoteStr()+"."+self.dialect.QuoteStr(), -1)

	return self.dialect.QuoteStr() + keyName + self.dialect.QuoteStr()
}

// TODO
func (self *TOrm) _now_time(sqlTypeName string) (res_val interface{}, res_time time.Time) {
	res_time = time.Now()
	if self.dialect.DBType() == core.ORACLE {
		return
	}

	if self.TimeZone != nil {
		res_time = res_time.In(self.TimeZone)

	}

	switch sqlTypeName {
	case core.Time:
		s := res_time.Format("2006-01-02 15:04:05") //time.RFC3339
		res_val = s[11:19]
	case core.Date:
		res_val = res_time.Format("2006-01-02")
	case core.DateTime, core.TimeStamp:
		if self.dialect.DBType() == "ql" {
			res_val = res_time
		} else if self.dialect.DBType() == "sqlite3" {
			res_val = res_time.UTC().Format("2006-01-02 15:04:05")
		} else {
			res_val = res_time.Format("2006-01-02 15:04:05")
		}
	case core.TimeStampz:
		if self.dialect.DBType() == core.MSSQL {
			res_val = res_time.Format("2006-01-02T15:04:05.9999999Z07:00")
		} else if self.dialect.DriverName() == "mssql" {
			res_val = res_time
		} else {
			res_val = res_time.Format(time.RFC3339Nano)
		}
	case core.BigInt, core.Int:
		res_val = res_time.Unix()
	default:
		res_val = res_time
	}
	return
}

// FormatTime format time
func (self *TOrm) FormatTime(sqlTypeName string, t time.Time) (v interface{}) {
	return self._format_time(self.TimeZone, sqlTypeName, t)
}

func (self *TOrm) _format_time(tz *time.Location, sqlTypeName string, t time.Time) (v interface{}) {
	if self.dialect.DBType() == core.ORACLE {
		return t
	}
	if tz != nil {
		t = self.FormatTimeZone(t)
	}
	switch sqlTypeName {
	case core.Time:
		s := t.Format("2006-01-02 15:04:05") //time.RFC3339
		v = s[11:19]
	case core.Date:
		v = t.Format("2006-01-02")
	case core.DateTime, core.TimeStamp:
		if self.dialect.DBType() == "ql" {
			v = t
		} else if self.dialect.DBType() == "sqlite3" {
			v = t.UTC().Format("2006-01-02 15:04:05")
		} else {
			v = t.Format("2006-01-02 15:04:05")
		}
	case core.TimeStampz:
		if self.dialect.DBType() == core.MSSQL {
			v = t.Format("2006-01-02T15:04:05.9999999Z07:00")
		} else if self.dialect.DriverName() == "mssql" {
			v = t
		} else {
			v = t.Format(time.RFC3339Nano)
		}
	case core.BigInt, core.Int:
		v = t.Unix()
	default:
		v = t
	}
	return
}

// # 映射结构体与表
func (self *TOrm) mapping(region string, model interface{}) (res_model *TModel) {
	lObjectName := utils.Obj2Name(model)
	lModelName := utils.DotCasedName(lObjectName)
	alt_model_name := lModelName // model别名,当Model使用别名Tag时作用
	lTableName := utils.SnakeCasedName(lObjectName)

	v := reflect.Indirect(reflect.ValueOf(model))
	lType := v.Type()

	// #确认ORM包含该表 以便更新
	//res_model = self.models[lType]
	/*res_model = self.models[lModelName]
	if res_model == nil || res_model.is_base {
		res_model = NewModel(lModelName, v, lType)
	} else {
		res_model._inherits = make([]string, 0) // Append前先清空
	}*/
	// # 不检测是否已经存在于ORM中 直接替换旧
	res_model = NewModel(lModelName, v, lType)

	// # 创建一个原始ORM表
	lTable := core.NewTable(lTableName, lType)
	res_model.table = lTable // # 提前关联为后续Tag函数使用
	res_model.is_base = false

	var (
		lField IField
		//FieldContext            *TFieldContext
		lCol                    *core.Column
		lColName                string
		lMemberName, lFieldName string
		lFieldValue             reflect.Value
		lFieldType              reflect.Type
		lIgonre                 bool
		is_table_tag            bool
	//	is_exit_col             bool
	)

	FieldContext := new(TFieldContext)
	FieldContext.Orm = self
	FieldContext.Model = res_model

	lRelateFields := make([]string, 0)
	for i := 0; i < lType.NumField(); i++ {
		is_table_tag = false
		lIgonre = false
		//is_exit_col = false
		lMemberName = lType.Field(i).Name
		lFieldName = utils.SnakeCasedName(lMemberName)
		lColName = lFieldName
		lFieldValue = v.Field(i)
		lFieldType = lType.Field(i).Type
		lFieldTag := lType.Field(i).Tag

		// # 忽略TModel类字段
		if strings.Index(strings.ToLower(lMemberName), strings.ToLower("TModel")) != -1 {
			lIgonre = true
			is_table_tag = true
		}

		if lCol = lTable.GetColumn(lColName); lCol == nil {
			// 创建Column
			lCol = core.NewColumn(
				lColName,
				lMemberName,
				core.SQLType{"", 0, 0},
				0,
				0,
				true)

			//# 获取Core对应Core的字段类型
			if lFieldValue.CanAddr() {
				if _, ok := lFieldValue.Addr().Interface().(core.Conversion); ok {
					lCol.SQLType = core.SQLType{core.Text, 0, 0}
				}
			}

			if _, ok := lFieldValue.Interface().(core.Conversion); ok {
				lCol.SQLType = core.SQLType{core.Text, 0, 0}
			} else {
				lCol.SQLType = core.Type2SQLType(lFieldType)
			}

			//# 初始调整不同数据库间的属性 size,type...
			self.dialect.SqlType(lCol)
		} else {
			//is_exit_col = true
		}

		// 如果 Field 不存在于ORM中
		if lField = res_model.FieldByName(lFieldName); lField == nil {
			//lField = self.NewFieldFromSqlType(lFieldName, lCol)
			//base_field = NewBaseField(lFieldName, lModelName)
			//logger.Dbg("lField", &lField, lFieldName)
		} else {
			//<** 如果是继承的字段则替换
			//原因：Join时导致Select到的字段为关联表字段而获取不到原本Model的字段如Id,write_time...
			if lField.IsForeignField() {
				//# 共同重叠字段
				//# 将关联表字段移入重叠字段表
				if res_model._common_fields[lFieldName] == nil {
					res_model._common_fields[lFieldName] = make(map[string]IField)
				}

				// 添加Parent表共同字段
				res_model._common_fields[lFieldName][lField.ModelName()] = lField // 将现有表字段添加进重叠字段

				//#替换掉关联字段并添加到重叠字段表里,表示该字段继承的关联表也有.
				new_fld := utils.Clone(lField).(IField)
				new_fld.SetBase(lField.Base())

				// 添加model表共同字段
				new_fld.Base().model_name = alt_model_name
				new_fld.Base().foreign_field = false // # 共同字段非外键字段
				new_fld.Base().common_field = true
				res_model._common_fields[lFieldName][new_fld.ModelName()] = new_fld // 将现有表字段添加进重叠字段
				lField = new_fld
			}
		}

		if lFieldTag != "" {
			// TODO 实现继承表 Inherite
			// TODO 自己映射脱离第三方ORM

			// 解析并变更默认值
			//logger.Dbg("ccc", lFieldName, lCol, lFieldTag)
			var (
				lTag []string
				lStr string
				lLen int
			)

			// 识别拆分Tag字符串
			lStr = lookup(string(lFieldTag), self.FieldIdentifier)
			if lStr == "" {
				lStr = lookup(string(lFieldTag), self.TableIdentifier)
				is_table_tag = true
			}

			// 识别分割Tag各属性
			//logger.Dbg("tags1", lookup(string(lFieldTag), self.FieldIdentifier))
			lTags := splitTag(lStr)
			//logger.Dbg("tags", lTags)

			// 排序Tag并_确保优先执行字段类型属性
			tagMap := make(map[string][]string) // 记录Tag的
			field_type_name := ""
			//attr_name = ""
			for _, key := range lTags {
				//------------------------------------------------------------------------------------------
				//以下代码是为了避免XORM解析不规则字符串为字段名提醒使用者规范Tag数据格式应该注意不用空格
				lTag = parseTag(key)
				// 验证
				logger.Assert(len(lTag) != 0, "Tag parse failed: Model:%s Field:%s Tag:%s Key:%s Result:%v", lModelName, lFieldName, lFieldTag, key, lTag)

				field_type_name = strings.ToLower(lTag[0])
				lStr = strings.Replace(key, field_type_name, "", 1) // 去掉Tag Item
				lStr = strings.TrimLeft(lStr, "(")
				lStr = strings.TrimRight(lStr, ")")
				lLen = len(lStr)
				if lLen > 0 {
					if strings.Index(lStr, " ") != -1 {
						if !strings.HasPrefix(lStr, "'") &&
							!strings.HasSuffix(lStr, "'") {
							logger.Panicf("Model %s's %s tags could no including space ' ' in brackets value whicth it not 'String' type.", lTableName, strings.ToUpper(lFieldName))
						}
					}
				}
				//----------------------------------------------------------------
				tagMap[field_type_name] = lTag //

				// # 根据Tag创建字段

				// 尝试获取新的Field以替换
				if !lIgonre && !is_table_tag && IsFieldType(field_type_name) { // # 当属性非忽略或者BaseModel
					if lField == nil || (lField.Type() != field_type_name) { // #字段实例为空 [或者] 字段类型和当前类型不一致时重建字段实例
						//logger.Dbg("NewField ", lFieldName, field_type_name)
						lField = NewField(lFieldName, field_type_name) // 根据Tag里的 属性类型创建Field实例
					}
				}
			}

			// # 根据Tyep创建字段
			if lField == nil {
				lField = self._newFieldFromSqlType(lFieldName, lCol)
				//logger.Dbg("lField", lField.Name(), lField.ColumnType(), lField.IsForeignField())
				// 必须确保上面的代码能获取到定义的字段类型
				if lField == nil {
					logger.Panicf("must difine the field type for the model field :" + lModelName + "." + lFieldName)
				}
			}
			lField.Base().model_name = alt_model_name
			//logger.Dbg("NewField aa", lField.Base())

			//base := lField.Base()
			//base = base_field
			//lField.SetBase(base)
			//logger.Dbg("NewField aa", lField.Base())
			//logger.Dbg("NewField aa", lField.Type(), base_field.Type(), base_field._attr_type, lField.Base(), &base_field)
			/* 废弃
			// 如果是Base类(TModel)则该字段使用BaseField
			if lIgonre || is_table_tag {
				lField = &BaseField
			}


			*/

			FieldContext.Column = lCol
			FieldContext.Field = lField
			FieldContext.Params = tagMap[lField.Type()]
			lField.Init(FieldContext)

			lIndexs := make(map[string]int)
			isUnique, isIndex := false, false
			for attr, vals := range tagMap {
				if attr == lField.Type() {
					continue // 忽略该Tag
				}

				// 原始ORM映射,理论上无需再次解析只需修改Tag和扩展后的一致即可
				switch strings.ToLower(attr) {
				case "-": // 忽略某些继承者成员
					lIgonre = true
					break
				case "<-":
					lCol.MapType = core.ONLYFROMDB
					break
				case "->":
					lCol.MapType = core.ONLYTODB
					break
				case "_relate": //废弃 关联某表
					if len(vals) > 1 && !is_table_tag {
						//logger.Dbg("relate to:", utils.DotCasedName(lMemberName), utils.SnakeCasedName(lTag[1]))
						// 现在成员名是关联的Model名,Tag 为关联的字段
						res_model._relations[utils.DotCasedName(lMemberName)] = utils.SnakeCasedName(vals[1])
					}
					break
				case "index":
					isIndex = true
					break
				case "unique":
					// 变更XORM
					isUnique = true
					break
				case "extends", "relate": // 忽略某些继承者成员
					lIgonre = true

					if strings.ToLower(attr) == "relate" {
						if len(vals) > 1 {
							lRefFldName := utils.SnakeCasedName(vals[1])
							lRelateFields = append(lRelateFields, lRefFldName)
							//logger.Dbg("relate to:", utils.DotCasedName(lMemberName), lRefFldName)
							// 现在成员名是关联的Model名,Tag 为关联的字段
							res_model._relations[utils.DotCasedName(lMemberName)] = lRefFldName
						}
					} else {
						//  extends
					}

					fallthrough
				default:
					// 执行
					lStr = attr // 获取属性名称

					// 切换到TableTag模式
					if is_table_tag {
						lStr = "table_" + lStr
					}

					// 执行自定义Tag初始化
					tag_ctrl := SelectTagController(lStr)
					if tag_ctrl != nil {
						//if tag, has := self.tag_ctrl[lStr]; has {
						//	tag(res_model, lFieldValue, lField, lCol, vals...)
						//}
						FieldContext.FieldTypeValue = lFieldValue
						FieldContext.Field = lField
						FieldContext.Params = vals
						tag_ctrl(FieldContext)
					} else {
						logger.Warnf("Unknown tag %s from %s:%s", lStr, lModelName, lFieldName)

						//# 其他数据库类型
						if _, ok := core.SqlTypes[strings.ToUpper(attr)]; ok {
							lCol.SQLType = core.SQLType{strings.ToUpper(attr), 0, 0}
						}
					}
				}
			}

			// 处理索引
			if isUnique {
				lIndexs[lCol.Name] = core.UniqueType
			} else if isIndex {
				lIndexs[lCol.Name] = core.IndexType
			}

			for idx_name, idx_type := range lIndexs {
				if index, ok := lTable.Indexes[idx_name]; ok {
					index.AddColumn(lCol.Name)
					lCol.Indexes[index.Name] = core.IndexType
				} else {
					index := core.NewIndex(idx_name, idx_type)
					index.AddColumn(lCol.Name)
					lTable.AddIndex(index)
					lCol.Indexes[index.Name] = core.IndexType
				}
			}
		} else { // # 当Tag为空
			// # 忽略无Tag的匿名继承结构
			if lType.Field(i).Name == lType.Field(i).Type.Name() {
				continue
			}

			if lField == nil {
				lField = self._newFieldFromSqlType(lFieldName, lCol)
				lField.Base().model_name = alt_model_name

				FieldContext.Column = lCol
				FieldContext.Field = lField
				FieldContext.Params = nil
				lField.Init(FieldContext)
			}
		}

		if lCol.IsAutoIncrement && lCol.IsPrimaryKey {
			res_model.RecordField = lField
		}

		if lCol.Length == 0 {
			lCol.Length = lCol.SQLType.DefaultLength
		}

		if lCol.Length2 == 0 {
			lCol.Length2 = lCol.SQLType.DefaultLength2
		}

		// 更新model新名称 并传递给其他Field
		if is_table_tag && res_model.GetModelName() != alt_model_name {
			alt_model_name = res_model.GetModelName()
			lField.Base().model_name = alt_model_name
		}
		// # 设置Help
		if lField.Title() == "" {
			lField.Base()._attr_title = utils.TitleCasedNameWithSpace(lField.Name())
		}

		if lField.Base()._attr_help == "" && lField.Title() != "" {
			lField.Base()._attr_help = lField.Title()
		}

		// #　通过条件过滤不学要的原始字段
		if !lIgonre && lField.Base()._attr_store && lField.Base()._column_type != "" {
			//if !is_exit_col {
			lTable.AddColumn(lCol)
			//} else {
			//	logger.Dbg("is_exit_col", lCol.Name)
			//}

			// 为字段添加数据库字段属性
			lField.Base().column = lCol
		}

		lField.UpdateDb(FieldContext)
		// 添加字段进Table
		if !lIgonre && lField.Type() != "" && lField.Name() != "" {
			res_model._fields[lFieldName] = lField // !!!替代方式
		}
	} // end for

	// 设置关联到外表的字段
	for _, name := range lRelateFields {
		if fld, has := res_model._fields[name]; has {
			fld.Base().related = true
		}
	}

	// #　合并旧的信息到新Table
	if tb, has := self.tables[lTableName]; has {
		// #复制 Col
		for _, col := range tb.Columns() {
			if lTable.GetColumn(col.Name) == nil {
				//logger.Dbg("copy " + col.Name)
				lTable.AddColumn(col)
			}
		}

		// # 复制 Indx
		for _, idx := range tb.Indexes {
			if _, has := lTable.Indexes[idx.Name]; !has {
				lTable.AddIndex(idx)
			}
		}

		// # 复制 Key
		for _, key := range tb.PrimaryKeys {
			if utils.InStrings(key, lTable.PrimaryKeys...) == -1 {
				lTable.PrimaryKeys = append(lTable.PrimaryKeys, key)
			}
		}

		for field, on := range tb.Created {
			if _, has := lTable.Created[field]; !has {
				lTable.Created[field] = on
			}
		}

		if lTable.Deleted == "" && tb.Deleted != "" {
			lTable.Deleted = tb.Deleted
		}

		if lTable.Updated == "" && tb.Updated != "" {
			lTable.Updated = tb.Updated
		}

		if lTable.AutoIncrement == "" && tb.AutoIncrement != "" {
			lTable.AutoIncrement = tb.AutoIncrement
		}

		if lTable.Updated == "" && tb.Updated != "" {
			lTable.Updated = tb.Updated
		}

		if lTable.Version == "" && tb.Version != "" {
			lTable.AutoIncrement = tb.AutoIncrement
		}
	}
	res_model.setBaseTable(lTable)
	// #添加加至列表
	//self.models[res_model._cls_type] = res_model // #Update tables map
	self.models[res_model.GetModelName()] = res_model // #Update tables map
	self.tables[res_model.table.Name] = lTable        // #Update tables map
	self.osv.RegisterModel(region, res_model)

	return
}

func (self *TOrm) HasModel(name string) bool {
	return self.osv.HasModel(name)
}

// TODO　考虑返回错误
// get model object from the orm which registed
func (self *TOrm) GetModel(model string, module ...string) (res_model IModel, err error) {
	return self.osv.GetModel(model, module...)
}

// return the table object
func (self *TOrm) GetTable(table string) (res_table *core.Table) {
	var has bool
	if res_table, has = self.tables[table]; has {
		return res_table
	}
	return nil
}

//  设置指定Tag配置控制器
func (self *TOrm) SetTagCtrl(name string, ctrl ITagController) {
	//	self.tag_ctrl[name] = ctrl
}

/*// 设置指定Field读写控制器
func (self *TOrm) SetFieldCtrl(field_type string, field_ctrl IFieldCtrl) {
	self.field_ctrl[field_type] = field_ctrl
}
*/

// set slave server
func (self *TOrm) SetSlave(db_src string) {

}

// cacher switch of model
func (self *TOrm) SetCacher(table string, open bool) {
	self.cacher.SetStatus(open, table)
}

// turn on the cacher for query
func (self *TOrm) ActiveCacher(sw bool) {
	self.cacher.Active(sw)
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
			logger.Info(query)
			result, err := self.Exec(query)
			results = append(results, result)
			if err != nil {
				return nil, err
				lastError = err
			}
		}
	}

	return results, lastError
}

// TODO remove
// Import SQL DDL file
func (self *TOrm) __ImportFile(ddlPath string) ([]sql.Result, error) {
	file, err := os.Open(ddlPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return self.Import(file)
}

func (self *TOrm) ShowSql(sw ...bool) {
	if len(sw) > 0 {
		self._show_sql = sw[0]
	} else {
		self._show_sql = true
	}
}

// If a table has any reocrd
func (self *TOrm) IsTableEmpty(model string) (bool, error) {
	session := self.NewSession()
	defer session.Close()
	return session.IsEmpty(model)
}

// If a table is exist
func (self *TOrm) IsTableExist(model string) (bool, error) {
	session := self.NewSession()
	defer session.Close()
	return session.IsExist(model)
}

// If a table is exist
func (self *TOrm) IsExist(db string) bool {
	ds := &DataSource{}
	*ds = *self.dataSource
	switch ds.DbType {
	case "postgres":
		ds.DbName = "postgres"
		o, e := NewOrm(ds)
		if e != nil {
			logger.Errf("IsExsit() raise an error:%s", db, e)
			return false
		}

		ds, e := o.Query("SELECT datname FROM pg_database WHERE datname = '" + db + "'")
		if e != nil {
			logger.Errf("IsExsit() raise an error:%s", db, e)
			return false
		}

		return ds.Count() > 0

	case "mysql":
	}

	return false
}

// 删除表
func (self *TOrm) DropTables(models ...string) error {
	session := self.NewSession()
	defer session.Close()

	err := session.Begin()
	if err != nil {
		return err
	}

	for _, model := range models {
		err = session.DropTable(model)
		if err != nil {
			session.Rollback()
			return err
		}
	}
	return session.Commit()
}

func (self *TOrm) __DB() IRawSession {
	session := self.NewSession()
	defer session.Close()
	return session
}

// TODO 根据表依赖关系顺序创建表
// CreateTables create tabls according bean
func (self *TOrm) CreateTables(name ...string) error {
	session := self.NewSession()
	err := session.Begin()
	defer session.Close()
	if err != nil {
		return err
	}

	for _, model := range name {
		err = session.CreateTable(model)
		if err != nil {
			session.Rollback()
			return err
		}
	}
	return session.Commit()
}

// TODO consider to implement truely create db
func (self *TOrm) CreateDatabase(name string) error {
	ds := &DataSource{}
	*ds = *self.dataSource
	switch ds.DbType {
	case "postgres":
		ds.DbName = "postgres"
		o, e := NewOrm(ds)
		if e != nil {
			return e
		}

		session := o.NewSession()
		defer session.Close()

		_, e = session.exec("CREATE DATABASE " + name)
		return e
	case "mysql":

	}

	return nil
}

// build the indexes for model
func (self *TOrm) CreateIndexes(model string) error {
	session := self.NewSession()
	defer session.Close()
	return session.CreateIndexes(model)
}

// build the uniques for model
func (self *TOrm) CreateUniques(model string) error {
	session := self.NewSession()
	defer session.Close()
	return session.CreateUniques(model)
}

// DBMetas Retrieve all tables, columns, indexes' informations from database.
// 从连接的数据库获取数据库及表基本信息
func (self *TOrm) DBMetas() (res_tables []*core.Table, err error) {
	res_tables, err = self.dialect.GetTables()
	if err != nil {
		return nil, err
	}

	for _, table := range res_tables {
		colSeq, cols, err := self.dialect.GetColumns(table.Name)
		if err != nil {
			return nil, err
		}
		for _, name := range colSeq {
			table.AddColumn(cols[name])
		}
		//table.Columns = cols
		//table.ColumnsSeq = colSeq
		indexes, err := self.dialect.GetIndexes(table.Name)
		if err != nil {
			return nil, err
		}
		table.Indexes = indexes

		for _, index := range indexes {
			for _, name := range index.Cols {
				if col := table.GetColumn(name); col != nil {
					col.Indexes[index.Name] = core.IndexType
				} else {
					return nil, fmt.Errorf("Unknown col "+name+" in indexes %v of table", index, table.ColumnsSeq())
				}
			}
		}
	}

	return
}

//# 插入一个新的Table并创建
// 同步更新Model 并返回同步后表 <字段>
//region 区分相同Model名称来自哪个模块，等级
func (self *TOrm) SyncModel(region string, models ...interface{}) (err error) {
	session := self.NewSession()
	defer session.Close()
	return session.SyncModel(region, models...)
}

func (self *TOrm) Query(sql string, params ...interface{}) (ds *dataset.TDataSet, err error) {
	session := self.NewSession()
	defer session.Close()
	return session.Query(sql, params...)
}

// exec raw sql directly
func (self *TOrm) Exec(sql string, params ...interface{}) (sql.Result, error) {
	session := self.NewSession()
	defer session.Close()

	return session.Exec(sql, params...)
}

func reg_drvs_dialects() bool {
	providedDrvsNDialects := map[string]struct {
		dbType     core.DbType
		getDriver  func() core.Driver
		getDialect func() core.Dialect
	}{
		//		"mssql":    {"mssql", func() core.Driver { return &odbcDriver{} }, func() core.Dialect { return &mssql{} }},
		//		"odbc":     {"mssql", func() core.Driver { return &odbcDriver{} }, func() core.Dialect { return &mssql{} }}, // !nashtsai! TODO change this when supporting MS Access
		//		"mysql":    {"mysql", func() core.Driver { return &mysqlDriver{} }, func() core.Dialect { return &mysql{} }},
		//		"mymysql":  {"mysql", func() core.Driver { return &mymysqlDriver{} }, func() core.Dialect { return &mysql{} }},
		"postgres": {"postgres", func() core.Driver { return &pqDriver{} }, func() core.Dialect { return &postgres{} }},
		//		"sqlite3":  {"sqlite3", func() core.Driver { return &sqlite3Driver{} }, func() core.Dialect { return &sqlite3{} }},
		//		"oci8":     {"oracle", func() core.Driver { return &oci8Driver{} }, func() core.Dialect { return &oracle{} }},
		//		"goracle":  {"oracle", func() core.Driver { return &goracleDriver{} }, func() core.Dialect { return &oracle{} }},
	}

	for driverName, v := range providedDrvsNDialects {
		if driver := core.QueryDriver(driverName); driver == nil {
			core.RegisterDriver(driverName, v.getDriver())
			core.RegisterDialect(v.dbType, v.getDialect)
		}
	}
	return true
}
