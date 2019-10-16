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
	// Conversion is an interface. A type implements Conversion will according
	// the custom method to fill into database and retrieve from database.
	// TODO 取缔
	Conversion interface {
		FromDB([]byte) error
		ToDB() ([]byte, error)
	}

	TOrm struct {
		dialect     IDialect
		db          *sql.DB
		dataSource  *TDataSource
		osv         *TOsv // 对象管理
		nameIndex   map[string]*TModel
		showSql     bool
		showSqlTime bool
		connected   bool

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
func NewOrm(dataSource *TDataSource) (*TOrm, error) {
	dialect := QueryDialect(dataSource.DbType)
	if dialect == nil {
		return nil, fmt.Errorf("Unsupported dialect type: %v", dataSource.DbType)
	}

	db, err := sql.Open(dataSource.DbType, dataSource.toString())
	if err != nil {
		return nil, err
	}

	err = dialect.Init(db, dataSource)
	if err != nil {
		return nil, err
	}

	logger.Infof("Connected database %s", dataSource.DbName)

	orm := &TOrm{
		db:              db,
		dialect:         dialect,
		dataSource:      dataSource,
		nameIndex:       make(map[string]*TModel),
		FieldIdentifier: "field",
		TableIdentifier: "table",
		TimeZone:        time.Local,
	}

	// Cacher
	orm.Cacher = newCacher()

	// OSV
	orm.osv = newOsv(orm)

	if orm.IsExist(dataSource.DbName) {
		err = orm.reverse()
		if err != nil {
			return nil, err
		}
	} else {
		orm.connected = false
		logger.Warnf("the orm is disconnected with database %s", dataSource.DbName)
	}

	return orm, nil
}

// TODO 保持表实时更新到ORM - 由于有些表是由SQL后期创建 导致Orm里缓存不存在改表Sycn时任然执行创建而抛出错误
// 更新现有数据库以及表信息并模拟创建TModel
// 反转Table 到 Model
func (self *TOrm) reverse() error {
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
			model := NewModel(model_name, model_val, model_type)
			model.obj = self.osv.newObject(model_name)
			model.is_base = true
		}
		*/

		self.osv.RegisterModel("", model.GetBase())
		logger.Infof("%s found in database!", model.GetName())
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

// TZTime change one time to time location
func (self *TOrm) FormatTimeZone(t time.Time) time.Time {
	if !t.IsZero() { // if time is not initialized it's not suitable for Time.In()
		return t.In(self.TimeZone)
	}
	return t
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

// QuoteStr Engine's database use which charactor as quote.
// mysql, sqlite use ` and postgres use "
func (self *TOrm) QuoteStr() string {
	return self.dialect.QuoteStr()
}

func (self *TOrm) ___Quote(keyName string) string {
	return self.___fmtQuote(keyName)
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

func (self *TOrm) ___fmtQuote(keyName string) string {
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
	if self.dialect.DBType() == ORACLE {
		return
	}

	if self.TimeZone != nil {
		res_time = res_time.In(self.TimeZone)

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

// FormatTime format time
func (self *TOrm) FormatTime(sqlTypeName string, t time.Time) (v interface{}) {
	return self.formatTime(self.TimeZone, sqlTypeName, t)
}

func (self *TOrm) formatTime(tz *time.Location, sqlTypeName string, t time.Time) (v interface{}) {
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

// # 映射结构体与表
func (self *TOrm) mapping(region string, model interface{}) (res_model *TModel) {
	object_name := utils.Obj2Name(model)
	model_name := fmtModelName(object_name)
	model_alt_name := model_name // model别名,当Model使用别名Tag时作用
	model_value := reflect.Indirect(reflect.ValueOf(model))
	model_type := model_value.Type()
	model_object := self.osv.newObject(model_name)

	res_model = NewModel(model_name, model_value, model_type) // 不检测是否已经存在于ORM中 直接替换旧
	res_model.obj = model_object
	res_model.is_base = false

	var (
		field          IField
		field_name     string
		field_tag      string
		field_value    reflect.Value
		field_type     reflect.Type
		field_sql_type string
		sql_type       SQLType
		member_name    string
	)

	for i := 0; i < model_type.NumField(); i++ {
		member_name = model_type.Field(i).Name

		// filter out the unexport field
		if !ast.IsExported(member_name) {
			continue
		}

		field_name = fmtFieldName(member_name)
		field_value = model_value.Field(i)
		field_type = model_type.Field(i).Type
		field_tag = string(model_type.Field(i).Tag)
		field_sql_type = ""

		// 解析并变更默认值
		var tagMap map[string][]string // 记录Tag的
		if field_tag == "" {
			// # 忽略无Tag的匿名继承结构
			if model_type.Field(i).Name == model_type.Field(i).Type.Name() {
				continue
			}
		} else {
			// 识别拆分Tag字符串
			var is_table_tag bool

			tag_str := lookup(string(field_tag), self.FieldIdentifier)
			if tag_str == "" {
				tag_str = lookup(string(field_tag), self.TableIdentifier)
				is_table_tag = true
			}

			// 识别分割Tag各属性
			//logger.Dbg("tags1", lookup(string(field_tag), self.FieldIdentifier))
			tags := splitTag(tag_str)

			// 排序Tag并_确保优先执行字段类型属性
			var field_type_name string
			var attrs []string
			tagMap = make(map[string][]string)
			for _, key := range tags {
				//****************************************
				//NOTE 以下代码是为了避免解析不规则字符串为字段名提醒使用者规范Tag数据格式应该注意不用空格
				attrs = parseTag(key)

				// 验证
				logger.Assert(len(attrs) != 0, "Tag parse failed: Model:%s Field:%s Tag:%s Key:%s Result:%v", model_name, field_name, field_tag, key, attrs)

				field_type_name = strings.ToLower(attrs[0])
				tag_str = strings.Replace(key, field_type_name, "", 1) // 去掉Tag Item
				tag_str = strings.TrimLeft(tag_str, "(")
				tag_str = strings.TrimRight(tag_str, ")")
				ln := len(tag_str)
				if ln > 0 {
					if strings.Index(tag_str, " ") != -1 {
						if !strings.HasPrefix(tag_str, "'") &&
							!strings.HasSuffix(tag_str, "'") {
							logger.Panicf("Model %s's %s tags could no including space ' ' in brackets value whicth it not 'String' type.", model_name, strings.ToUpper(field_name))
						}
					}
				}
				//****************************************
				tagMap[field_type_name] = attrs[1:] //
				if !is_table_tag && IsFieldType(field_type_name) {
					field_sql_type = field_type_name
				}
			}
		}

		//logger.Dbg("%s:%s %s", model_name, member_name, lColName)

		// # 忽略TModel类字段
		field_context := new(TFieldContext)
		field_context.Orm = self
		field_context.Model = res_model
		field_context.FieldTypeValue = field_value
		if strings.Index(strings.ToLower(member_name), "tmodel") != -1 {
			// 执行tag处理
			self.handleTags(field_context, tagMap, "table")

			// 更新model新名称 并传递给其他Field
			if res_model.GetName() != model_alt_name {
				model_alt_name = res_model.GetName()
			}
		} else {

			//if column = model_object.GetFieldByName(lColName); column == nil {
			if field = res_model.GetFieldByName(field_name); field == nil {
				sql_type = Type2SQLType(field_type)
				field = NewField(field_name, sql_type)

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
					res_model.obj.SetCommonFieldByName(field_name, new_fld.ModelName(), new_fld) // 将现有表字段添加进重叠字段
					field = new_fld
				}
			}

			// # 根据Tag创建字段
			// 尝试获取新的Field以替换
			if IsFieldType(field_sql_type) { // # 当属性非忽略或者BaseModel
				if field == nil || (field.Type() != field_sql_type) { // #字段实例为空 [或者] 字段类型和当前类型不一致时重建字段实例
					field = NewField(field_name, field_sql_type) // 根据Tag里的 属性类型创建Field实例
				}
			}

			// # check again 根据Tyep创建字段
			if field == nil { // 必须确保上面的代码能获取到定义的字段类型
				logger.Panicf("must difine the field type for the model field :" + model_name + "." + field_name)
			}

			if field.SQLType().Name == "" {
				field.Base().SqlType = Type2SQLType(field_type)
			}

			// 更新model新名称
			field.Base().model_name = model_alt_name

			// 执行field初始化
			field_context.Field = field
			field_context.Params = tagMap[field.Type()]
			field.Init(field_context)

			// 执行tag处理
			self.handleTags(field_context, tagMap, "")

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
					//	logger.Dbg("is_exit_col", column.Name)
					//}

					// 为字段添加数据库字段属性
					////field.Base().column = column
					field.Base().isColumn = true
				}
			*/

			field.UpdateDb(field_context)

			// 添加字段进Table
			if field.Type() != "" && field.Name() != "" {
				res_model.obj.SetFieldByName(field_name, field) // !!!替代方式
			}
		}
	} // end for

	// 注册到对象服务
	self.osv.RegisterModel(region, res_model)

	return
}

func (self *TOrm) handleTags(fieldCtx *TFieldContext, tags map[string][]string, prefix string) {
	field := fieldCtx.Field

	for attr, vals := range tags {
		if field != nil && attr == field.Type() {
			continue // 忽略该Tag
		}

		// 原始ORM映射,理论上无需再次解析只需修改Tag和扩展后的一致即可
		switch strings.ToLower(attr) {
		case "-": // 忽略某些继承者成员
			goto EXIT
		default:
			// 执行
			tag_str := attr // 获取属性名称

			// 切换到TableTag模式
			if prefix != "" {
				tag_str = prefix + "_" + tag_str
			}

			// 执行自定义Tag初始化
			tag_ctrl := GetTagControllerByName(tag_str)
			if tag_ctrl != nil {
				fieldCtx.Params = vals
				tag_ctrl(fieldCtx)
			} else {
				// check not field type also
				if _, has := field_creators[tag_str]; has {
					// already init by field creators
					break
				}

				logger.Warnf("Unknown tag %s from %s:%s", tag_str, fieldCtx.Model.GetName(), field.Name())
			}
		}
	}

EXIT:
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

// is the orm connected database
func (self *TOrm) Connected() bool {
	return self.connected
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
func (self *TOrm) IsExist(name string) bool {
	return self.dialect.IsDatabaseExist(name)
	/*
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
	*/
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

// create database
func (self *TOrm) CreateDatabase(name string) error {
	return self.dialect.CreateDatabase(name)
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
func (self *TOrm) DBMetas() (models []IModel, err error) {
	models, err = self.dialect.GetModels()
	if err != nil {
		return nil, err
	}

	for _, model := range models {
		model_name := model.GetName()
		model_object := self.osv.newObject(model_name)
		model.GetBase().obj = model_object
		//self.osv.RegisterModel("", model.GetBase())

		colSeq, fields, err := self.dialect.GetFields(model_name)
		if err != nil {
			return nil, err
		}

		for _, name := range colSeq {
			model.AddField(fields[name])
		}
		//model.Columns = cols
		//model.ColumnsSeq = colSeq
		indexes, err := self.dialect.GetIndexes(model_name)
		if err != nil {
			return nil, err
		}

		model.Obj().indexes = indexes

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
