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
	"go/ast"
	"io"
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
)

type (
	TOrm struct {
		db          *core.DB
		dialect     core.Dialect
		dataSource  *DataSource
		osv         *TOsv // 对象管理
		models      map[string]*TModel
		tables      map[string]*core.Table // #作为非经典模式参考
		nameIndex   map[string]*TModel
		showSql     bool
		showSqlTime bool

		// public
		Cacher          *TCacher // TODO 大写
		FieldIdentifier string   // 字段 tag 标记
		TableIdentifier string   // 表 tag 标记
		TimeZone        *time.Location
	}
)

/*
 create a new ORM instance
*/
func NewOrm(dataSource *DataSource) (*TOrm, error) {
	reg_drvs_dialects()
	db_driver := dataSource.DbType
	db_src := dataSource.toString()
	driver := core.QueryDriver(db_driver)
	if driver == nil {
		return nil, fmt.Errorf("Unsupported driver name: %v", db_driver)
	}

	uri, err := driver.Parse(db_driver, db_src)
	if err != nil {
		return nil, err
	}

	dialect := core.QueryDialect(uri.DbType)
	if dialect == nil {
		return nil, fmt.Errorf("Unsupported dialect type: %v", uri.DbType)
	}

	db, err := core.Open(db_driver, db_src)
	if err != nil {
		return nil, err
	}

	err = dialect.Init(db, uri, db_driver, db_src)
	if err != nil {
		return nil, err
	}

	orm := &TOrm{
		db:              db,
		dialect:         dialect,
		dataSource:      dataSource,
		models:          make(map[string]*TModel),
		tables:          make(map[string]*core.Table),
		nameIndex:       make(map[string]*TModel),
		FieldIdentifier: "field",
		TableIdentifier: "table",
		TimeZone:        time.Local,
	}

	// Cacher
	orm.Cacher = NewCacher()

	// OSV
	orm.osv = NewOsv(orm)

	orm.reverse()

	return orm, nil
}

// TODO 保持表实时更新到ORM - 由于有些表是由SQL后期创建 导致Orm里缓存不存在改表Sycn时任然执行创建而抛出错误
// 更新现有数据库以及表信息并模拟创建TModel
// 反转Table 到 Model
func (self *TOrm) reverse() error {
	tables, err := self.DBMetas()
	if err != nil {
		return err
	}

	for _, tb := range tables {
		logger.Infof("%s found in database!", tb.Name)

		if _, has := self.tables[tb.Name]; !has {
			self.tables[tb.Name] = tb
			model_name := strings.Replace(tb.Name, "_", ".", -1)
			model_val := reflect.Indirect(reflect.ValueOf(new(TModel)))
			model_type := model_val.Type()

			// new a base model instance
			model := NewModel(model_name, model_val, model_type)
			model.obj = self.osv.newObject(model_name)
			model.table = tb // piont to the table
			model.is_base = true
			self.models[model_name] = model

			// init all columns to the model
			field_context := new(TFieldContext)
			for _, col := range tb.Columns() {
				field := self.newFieldFromSqlType(col.Name, col)
				//logger.Dbg("cccccccccc", field, col.Name, *col)
				field.Base().model_name = model_name

				field_context.Orm = self
				field_context.Model = model
				field_context.Column = col
				field_context.Field = field
				field_context.Params = nil
				//logger.Dbg("tagMap ", field.Type(), tagMap[field.Type()])
				field.Init(field_context)

				if col.IsAutoIncrement && col.IsPrimaryKey {
					model.idField = field.Name()
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
				model.obj.SetFieldByName(col.Name, field)
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
	logger.Infof("PING DATABASE %s@%s", self.dataSource.DbName, self.DriverName())
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

// TODO 190816 考虑废弃
func (self *TOrm) ModelByName(name string) *TModel {
	return self.models[name]
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

// 使用Model实例为基础
func (self *TOrm) Model(modelName string) *TSession {
	session := self.NewSession()
	session.IsAutoClose = true
	return session.Model(modelName)
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
	return self.fmtQuote(keyName)
}

func (self *TOrm) log_exec_sql(sql string, args []interface{}, executionBlock func() (sql.Result, error)) (sql.Result, error) {
	if self.showSql {
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
func (self *TOrm) newFieldFromSqlType(name string, col *core.Column) IField {
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
	if self.showSql {
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

// format the model name to the same
func fmtModelName(name string) string {
	return utils.SnakeCasedName(utils.Trim(name))
}

// format the field name to the same
func fmtFieldName(name string) string {
	return utils.SnakeCasedName(utils.Trim(name))
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

func (self *TOrm) fmtQuote(keyName string) string {
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
func (self *TOrm) nowTime(sqlTypeName string) (res_val interface{}, res_time time.Time) {
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
	return self.formatTime(self.TimeZone, sqlTypeName, t)
}

func (self *TOrm) formatTime(tz *time.Location, sqlTypeName string, t time.Time) (v interface{}) {
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
	object_name := utils.Obj2Name(model)
	model_name := fmtModelName(object_name)
	alt_model_name := model_name // model别名,当Model使用别名Tag时作用
	table_name := model_name     // utils.SnakeCasedName(object_name)
	v := reflect.Indirect(reflect.ValueOf(model))
	lType := v.Type()

	model_object := self.osv.newObject(model_name)
	table := core.NewTable(table_name, lType)  // 创建一个原始ORM表
	res_model = NewModel(model_name, v, lType) // 不检测是否已经存在于ORM中 直接替换旧
	res_model.obj = model_object
	res_model.table = table // 提前关联为后续Tag函数使用
	res_model.is_base = false
	var (
		field                   IField
		column                  *core.Column
		lColName                string
		lMemberName, lFieldName string
		lFieldValue             reflect.Value
		lFieldType              reflect.Type
		igonre                  bool
		is_table_tag            bool
	)

	field_context := new(TFieldContext)
	field_context.Orm = self
	field_context.Model = res_model
	relate_fields := make([]string, 0)
	for i := 0; i < lType.NumField(); i++ {
		lMemberName = lType.Field(i).Name

		// filter out the unexport field
		if !ast.IsExported(lMemberName) {
			continue
		}

		is_table_tag = false
		igonre = false
		lFieldName = fmtFieldName(lMemberName)
		lColName = lFieldName
		lFieldValue = v.Field(i)
		lFieldType = lType.Field(i).Type
		field_tag := lType.Field(i).Tag

		//logger.Dbg("%s:%s %s", model_name, lMemberName, lColName)

		// # 忽略TModel类字段
		if strings.Index(strings.ToLower(lMemberName), "tmodel") != -1 {
			igonre = true
			is_table_tag = true
		}

		if column = table.GetColumn(lColName); column == nil {
			// 创建Column
			column = core.NewColumn(
				lColName,
				lMemberName,
				core.SQLType{"", 0, 0},
				0,
				0,
				true)

			//# 获取Core对应Core的字段类型
			if lFieldValue.CanAddr() && lFieldValue.Addr().CanInterface() {
				if _, ok := lFieldValue.Addr().Interface().(core.Conversion); ok {
					column.SQLType = core.SQLType{core.Text, 0, 0}
				}
			}

			if _, ok := lFieldValue.Interface().(core.Conversion); ok {
				column.SQLType = core.SQLType{core.Text, 0, 0}

			} else {
				column.SQLType = core.Type2SQLType(lFieldType)

			}

			//# 初始调整不同数据库间的属性 size,type...
			self.dialect.SqlType(column)
		}

		// 如果 Field 不存在于ORM中
		if field = res_model.GetFieldByName(lFieldName); field != nil {
			//** 如果是继承的字段则替换
			//原因：Join时导致Select到的字段为关联表字段而获取不到原本Model的字段如Id,write_time...

			if field.IsInheritedField() {
				//# 共同重叠字段
				//# 将关联表字段移入重叠字段表
				// 将现有表字段添加进重叠字段
				res_model.obj.SetCommonFieldByName(lFieldName, field.ModelName(), field) // 添加Parent表共同字段

				//#替换掉关联字段并添加到重叠字段表里,表示该字段继承的关联表也有.
				new_fld := utils.Clone(field).(IField)
				new_fld.SetBase(field.Base())

				// 添加model表共同字段
				new_fld.Base().model_name = alt_model_name
				new_fld.Base().isInheritedField = false                                      // # 共同字段非外键字段
				res_model.obj.SetCommonFieldByName(lFieldName, new_fld.ModelName(), new_fld) // 将现有表字段添加进重叠字段
				field = new_fld
			}
		}

		if field_tag != "" {
			// TODO 实现继承表 Inherite
			// 解析并变更默认值
			var (
				lTag []string
				lStr string
				lLen int
			)

			// 识别拆分Tag字符串
			lStr = lookup(string(field_tag), self.FieldIdentifier)
			if lStr == "" {
				lStr = lookup(string(field_tag), self.TableIdentifier)
				is_table_tag = true
			}

			// 识别分割Tag各属性
			//logger.Dbg("tags1", lookup(string(field_tag), self.FieldIdentifier))
			tags := splitTag(lStr)

			// 排序Tag并_确保优先执行字段类型属性
			tagMap := make(map[string][]string) // 记录Tag的
			field_type_name := ""
			//attr_name = ""
			for _, key := range tags {
				//****************************************
				//NOTE 以下代码是为了避免XORM解析不规则字符串为字段名提醒使用者规范Tag数据格式应该注意不用空格
				lTag = parseTag(key)

				// 验证
				logger.Assert(len(lTag) != 0, "Tag parse failed: Model:%s Field:%s Tag:%s Key:%s Result:%v", model_name, lFieldName, field_tag, key, lTag)

				field_type_name = strings.ToLower(lTag[0])
				lStr = strings.Replace(key, field_type_name, "", 1) // 去掉Tag Item
				lStr = strings.TrimLeft(lStr, "(")
				lStr = strings.TrimRight(lStr, ")")
				lLen = len(lStr)
				if lLen > 0 {
					if strings.Index(lStr, " ") != -1 {
						if !strings.HasPrefix(lStr, "'") &&
							!strings.HasSuffix(lStr, "'") {
							logger.Panicf("Model %s's %s tags could no including space ' ' in brackets value whicth it not 'String' type.", table_name, strings.ToUpper(lFieldName))
						}
					}
				}
				//****************************************
				tagMap[field_type_name] = lTag[1:] //

				// # 根据Tag创建字段

				// 尝试获取新的Field以替换
				if !igonre && !is_table_tag && IsFieldType(field_type_name) { // # 当属性非忽略或者BaseModel
					if field == nil || (field.Type() != field_type_name) { // #字段实例为空 [或者] 字段类型和当前类型不一致时重建字段实例
						field = NewField(lFieldName, field_type_name) // 根据Tag里的 属性类型创建Field实例
					}
				}
			}

			// # check again 根据Tyep创建字段
			if field == nil {
				field = self.newFieldFromSqlType(lFieldName, column)
				if field == nil { // 必须确保上面的代码能获取到定义的字段类型
					logger.Panicf("must difine the field type for the model field :" + model_name + "." + lFieldName)
				}
			}
			field.Base().model_name = alt_model_name

			field_context.Column = column
			field_context.Field = field
			field_context.Params = tagMap[field.Type()]
			field.Init(field_context)

			lIndexs := make(map[string]int)
			isUnique, isIndex := false, false
			for attr, vals := range tagMap {
				if attr == field.Type() {
					continue // 忽略该Tag
				}

				// 原始ORM映射,理论上无需再次解析只需修改Tag和扩展后的一致即可
				switch strings.ToLower(attr) {
				case "-": // 忽略某些继承者成员
					igonre = true
					break
				case "<-":
					column.MapType = core.ONLYFROMDB
					break
				case "->":
					column.MapType = core.ONLYTODB
					break
				case "index":
					isIndex = true
					break
				case "unique":
					// 变更XORM
					isUnique = true
					break
				case "extends", "relate": // 忽略某些继承者成员
					igonre = true
					fallthrough
				default:
					// 执行
					lStr = attr // 获取属性名称

					// 切换到TableTag模式
					if is_table_tag {
						lStr = "table_" + lStr
					}

					// 执行自定义Tag初始化
					tag_ctrl := GetTagControllerByName(lStr)
					if tag_ctrl != nil {
						field_context.FieldTypeValue = lFieldValue
						field_context.Field = field
						field_context.Params = vals
						tag_ctrl(field_context)
					} else {
						// check not field type also
						if _, has := field_creators[lStr]; has {
							// already init by field creators
							break
						}

						//# 其他数据库类型
						if _, ok := core.SqlTypes[strings.ToUpper(attr)]; ok {
							column.SQLType = core.SQLType{strings.ToUpper(attr), 0, 0}
							break
						}

						logger.Warnf("Unknown tag %s from %s:%s", lStr, model_name, lFieldName)
					}
				}
			}

			// 处理索引
			if isUnique {
				lIndexs[column.Name] = core.UniqueType
			} else if isIndex {
				lIndexs[column.Name] = core.IndexType
			}

			for idx_name, idx_type := range lIndexs {
				if index, ok := table.Indexes[idx_name]; ok {
					index.AddColumn(column.Name)
					column.Indexes[index.Name] = core.IndexType
				} else {
					index := core.NewIndex(idx_name, idx_type)
					index.AddColumn(column.Name)
					table.AddIndex(index)
					column.Indexes[index.Name] = core.IndexType
				}
			}
		} else { // # 当Tag为空
			// # 忽略无Tag的匿名继承结构
			if lType.Field(i).Name == lType.Field(i).Type.Name() {
				continue
			}

			if field == nil {
				field = self.newFieldFromSqlType(lFieldName, column)
				field.Base().model_name = alt_model_name

				field_context.Column = column
				field_context.Field = field
				field_context.Params = nil
				field.Init(field_context)
			}
		}

		if column.IsAutoIncrement && column.IsPrimaryKey {
			res_model.idField = field.Name()
		}

		if column.Length == 0 {
			column.Length = column.SQLType.DefaultLength
		}

		if column.Length2 == 0 {
			column.Length2 = column.SQLType.DefaultLength2
		}

		// 更新model新名称 并传递给其他Field
		if is_table_tag && res_model.GetModelName() != alt_model_name {
			alt_model_name = res_model.GetModelName()
			field.Base().model_name = alt_model_name
		}
		// # 设置Help
		if field.Title() == "" {
			field.Base()._attr_title = utils.TitleCasedNameWithSpace(field.Name())
		}

		if field.Base()._attr_help == "" && field.Title() != "" {
			field.Base()._attr_help = field.Title()
		}

		// #　通过条件过滤不学要的原始字段
		if !igonre && field.Base()._attr_store && field.Base()._column_type != "" {
			//if !is_exit_col {
			table.AddColumn(column)
			//} else {
			//	logger.Dbg("is_exit_col", column.Name)
			//}

			// 为字段添加数据库字段属性
			field.Base().column = column
		}

		field.UpdateDb(field_context)
		// 添加字段进Table
		if !igonre && field.Type() != "" && field.Name() != "" {
			res_model.obj.SetFieldByName(lFieldName, field) // !!!替代方式
		}
	} // end for

	// 设置关联到外表的字段
	for _, name := range relate_fields {
		if fld := res_model.obj.GetFieldByName(name); fld != nil {
			fld.Base().IsRelatedField(true)
		}
	}

	// #　合并旧的信息到新Table
	if tb, has := self.tables[table_name]; has {
		// #复制 Col
		for _, col := range tb.Columns() {
			if table.GetColumn(col.Name) == nil {
				table.AddColumn(col)
			}
		}

		// # 复制 Indx
		for _, idx := range tb.Indexes {
			if _, has := table.Indexes[idx.Name]; !has {
				table.AddIndex(idx)
			}
		}

		// # 复制 Key
		for _, key := range tb.PrimaryKeys {
			if utils.InStrings(key, table.PrimaryKeys...) == -1 {
				table.PrimaryKeys = append(table.PrimaryKeys, key)
			}
		}

		for field, on := range tb.Created {
			if _, has := table.Created[field]; !has {
				table.Created[field] = on
			}
		}

		if table.Deleted == "" && tb.Deleted != "" {
			table.Deleted = tb.Deleted
		}

		if table.Updated == "" && tb.Updated != "" {
			table.Updated = tb.Updated
		}

		if table.AutoIncrement == "" && tb.AutoIncrement != "" {
			table.AutoIncrement = tb.AutoIncrement
		}

		if table.Updated == "" && tb.Updated != "" {
			table.Updated = tb.Updated
		}

		if table.Version == "" && tb.Version != "" {
			table.AutoIncrement = tb.AutoIncrement
		}
	}
	res_model.setBaseTable(table)

	// #添加加至列表
	//self.models[res_model.modelType] = res_model // #Update tables map
	self.models[res_model.GetModelName()] = res_model // #Update tables map
	self.tables[res_model.table.Name] = table         // #Update tables map

	// 注册到对象服务
	self.osv.RegisterModel(region, res_model)

	return
}

func (self *TOrm) HasModel(name string) bool {
	return self.osv.HasModel(name)
}

// get model object from the orm which registed
func (self *TOrm) GetModel(modelName string, origin ...string) (model IModel, err error) {
	return self.osv.GetModel(modelName, origin...)
}

// return the mane of all models
func (self *TOrm) GetModels() []string {
	return self.osv.GetModels()
}

// return the table object
func (self *TOrm) GetTable(tableName string) *core.Table {
	if table, has := self.tables[tableName]; has {
		return table
	}
	return nil
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

func (self *TOrm) ShowSql(sw ...bool) {
	if len(sw) > 0 {
		self.showSql = sw[0]
	} else {
		self.showSql = true
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
func (self *TOrm) DropTables(names ...string) error {
	session := self.NewSession()
	defer session.Close()

	err := session.Begin()
	if err != nil {
		return err
	}

	for _, model := range names {
		err = session.DropTable(model)
		if err != nil {
			session.Rollback()
			return err
		}
	}
	return session.Commit()
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
func (self *TOrm) CreateIndexes(modelName string) error {
	session := self.NewSession()
	defer session.Close()
	return session.CreateIndexes(modelName)
}

// build the uniques for model
func (self *TOrm) CreateUniques(modelName string) error {
	session := self.NewSession()
	defer session.Close()
	return session.CreateUniques(modelName)
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
func (self *TOrm) SyncModel(region string, models ...interface{}) (modelNames []string, err error) {
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
