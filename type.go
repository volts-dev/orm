package orm

import (
	"reflect"
	"strings"
	"time"

	"github.com/volts-dev/utils"
)

var (
	// !NOTE! GOLANG BASE DATATYPE treat following var as interal const values, these are used for reflect.TypeOf comparison
	g_DATATYPE_STRING_DEFAULT     string
	g_DATATYPE_BOOL_DEFAULT       bool
	g_DATATYPE_BYTE_DEFAULT       byte
	g_DATATYPE_COMPLEX64_DEFAULT  complex64
	g_DATATYPE_COMPLEX128_DEFAULT complex128
	g_DATATYPE_FLOAT32_DEFAULT    float32
	g_DATATYPE_FLOAT64_DEFAULT    float64
	g_DATATYPE_INT64_DEFAULT      int64
	g_DATATYPE_UINT64_DEFAULT     uint64
	g_DATATYPE_INT32_DEFAULT      int32
	g_DATATYPE_UINT32_DEFAULT     uint32
	g_DATATYPE_INT16_DEFAULT      int16
	g_DATATYPE_UINT16_DEFAULT     uint16
	g_DATATYPE_INT8_DEFAULT       int8
	g_DATATYPE_UINT8_DEFAULT      uint8
	g_DATATYPE_INT_DEFAULT        int
	g_DATATYPE_UINT_DEFAULT       uint
	g_DATATYPE_TIME_DEFAULT       time.Time

	// GOLANG DATATYPE TYPE
	IntType        = reflect.TypeOf(g_DATATYPE_INT_DEFAULT)
	Int8Type       = reflect.TypeOf(g_DATATYPE_INT8_DEFAULT)
	Int16Type      = reflect.TypeOf(g_DATATYPE_INT16_DEFAULT)
	Int32Type      = reflect.TypeOf(g_DATATYPE_INT32_DEFAULT)
	Int64Type      = reflect.TypeOf(g_DATATYPE_INT64_DEFAULT)
	UintType       = reflect.TypeOf(g_DATATYPE_UINT_DEFAULT)
	Uint8Type      = reflect.TypeOf(g_DATATYPE_UINT8_DEFAULT)
	Uint16Type     = reflect.TypeOf(g_DATATYPE_UINT16_DEFAULT)
	Uint32Type     = reflect.TypeOf(g_DATATYPE_UINT32_DEFAULT)
	Uint64Type     = reflect.TypeOf(g_DATATYPE_UINT64_DEFAULT)
	Float32Type    = reflect.TypeOf(g_DATATYPE_FLOAT32_DEFAULT)
	Float64Type    = reflect.TypeOf(g_DATATYPE_FLOAT64_DEFAULT)
	Complex64Type  = reflect.TypeOf(g_DATATYPE_COMPLEX64_DEFAULT)
	Complex128Type = reflect.TypeOf(g_DATATYPE_COMPLEX128_DEFAULT)
	StringType     = reflect.TypeOf(g_DATATYPE_STRING_DEFAULT)
	BoolType       = reflect.TypeOf(g_DATATYPE_BOOL_DEFAULT)
	ByteType       = reflect.TypeOf(g_DATATYPE_BYTE_DEFAULT)
	BytesType      = reflect.SliceOf(ByteType)
	TimeType       = reflect.TypeOf(g_DATATYPE_TIME_DEFAULT)

	// GOLANG DATATYPE TYPE PTR
	PtrIntType        = reflect.PtrTo(IntType)
	PtrInt8Type       = reflect.PtrTo(Int8Type)
	PtrInt16Type      = reflect.PtrTo(Int16Type)
	PtrInt32Type      = reflect.PtrTo(Int32Type)
	PtrInt64Type      = reflect.PtrTo(Int64Type)
	PtrUintType       = reflect.PtrTo(UintType)
	PtrUint8Type      = reflect.PtrTo(Uint8Type)
	PtrUint16Type     = reflect.PtrTo(Uint16Type)
	PtrUint32Type     = reflect.PtrTo(Uint32Type)
	PtrUint64Type     = reflect.PtrTo(Uint64Type)
	PtrFloat32Type    = reflect.PtrTo(Float32Type)
	PtrFloat64Type    = reflect.PtrTo(Float64Type)
	PtrComplex64Type  = reflect.PtrTo(Complex64Type)
	PtrComplex128Type = reflect.PtrTo(Complex128Type)
	PtrStringType     = reflect.PtrTo(StringType)
	PtrBoolType       = reflect.PtrTo(BoolType)
	PtrByteType       = reflect.PtrTo(ByteType)
	PtrTimeType       = reflect.PtrTo(TimeType)

	// ITF_TYPE 作为Scan从数据库扫描存储的数据容器
	ITF_TYPE = reflect.ValueOf(new(interface{})).Type().Elem()
)

// SQL types
type SQLType struct {
	Name           string
	DefaultLength  int
	DefaultLength2 int
}

const (
	// TODO remove
	SQLITE = "sqlite3"
	MYSQL  = "mysql"
	MSSQL  = "mssql"
	ORACLE = "oracle"

	UNKNOW_TYPE = iota
	TEXT_TYPE
	BLOB_TYPE
	TIME_TYPE
	NUMERIC_TYPE
)

var (
	// 各数据库数据类型
	Bit              = "BIT"
	TinyInt          = "TINYINT"
	SmallInt         = "SMALLINT"
	MediumInt        = "MEDIUMINT"
	Int              = "INT"     // ORM
	Integer          = "INTEGER" //#
	BigInt           = "BIGINT"  // ORM
	Enum             = "ENUM"
	Set              = "SET"
	Char             = "CHAR" // ORM
	Varchar          = "VARCHAR"
	NChar            = "NCHAR"
	NVarchar         = "NVARCHAR"
	TinyText         = "TINYTEXT"
	Text             = "TEXT" // ORM
	NText            = "NTEXT"
	Clob             = "CLOB"
	MediumText       = "MEDIUMTEXT"
	LongText         = "LONGTEXT"
	Uuid             = "UUID"
	UniqueIdentifier = "UNIQUEIDENTIFIER"
	SysName          = "SYSNAME"
	Date             = "DATE"     // ORM
	DateTime         = "DATETIME" // ORM
	SmallDateTime    = "SMALLDATETIME"
	Time             = "TIME"
	TimeStamp        = "TIMESTAMP"
	TimeStampz       = "TIMESTAMPZ"
	Decimal          = "DECIMAL"
	Numeric          = "NUMERIC"
	Money            = "MONEY"
	SmallMoney       = "SMALLMONEY"
	Real             = "REAL"
	Float            = "FLOAT" // ORM
	Double           = "DOUBLE"
	Binary           = "BINARY"
	VarBinary        = "VARBINARY"
	TinyBlob         = "TINYBLOB"
	Blob             = "BLOB" // ORM
	MediumBlob       = "MEDIUMBLOB"
	LongBlob         = "LONGBLOB"
	Bytea            = "BYTEA"
	Bool             = "BOOL"    // ORM
	Boolean          = "BOOLEAN" //#
	Serial           = "SERIAL"
	BigSerial        = "BIGSERIAL"
	Json             = "JSON" // ORM
	Jsonb            = "JSONB"
	// new
	TYPE_O2O = "one2one"
	TYPE_O2M = "one2many"
	TYPE_M2O = "many2one"
	TYPE_M2M = "many2many"

	SqlTypes = map[string]int{
		Bool: NUMERIC_TYPE,

		Serial:    NUMERIC_TYPE,
		BigSerial: NUMERIC_TYPE,

		Bit:       NUMERIC_TYPE,
		TinyInt:   NUMERIC_TYPE,
		SmallInt:  NUMERIC_TYPE,
		MediumInt: NUMERIC_TYPE,
		Int:       NUMERIC_TYPE,
		Integer:   NUMERIC_TYPE,
		BigInt:    NUMERIC_TYPE,

		Decimal:    NUMERIC_TYPE,
		Numeric:    NUMERIC_TYPE,
		Real:       NUMERIC_TYPE,
		Float:      NUMERIC_TYPE,
		Double:     NUMERIC_TYPE,
		Money:      NUMERIC_TYPE,
		SmallMoney: NUMERIC_TYPE,

		Enum:  TEXT_TYPE,
		Set:   TEXT_TYPE,
		Json:  TEXT_TYPE,
		Jsonb: TEXT_TYPE,

		Char:       TEXT_TYPE,
		NChar:      TEXT_TYPE,
		Varchar:    TEXT_TYPE,
		NVarchar:   TEXT_TYPE,
		TinyText:   TEXT_TYPE,
		Text:       TEXT_TYPE,
		NText:      TEXT_TYPE,
		MediumText: TEXT_TYPE,
		LongText:   TEXT_TYPE,
		Uuid:       TEXT_TYPE,
		Clob:       TEXT_TYPE,
		SysName:    TEXT_TYPE,

		Date:          TIME_TYPE,
		DateTime:      TIME_TYPE,
		Time:          TIME_TYPE,
		TimeStamp:     TIME_TYPE,
		TimeStampz:    TIME_TYPE,
		SmallDateTime: TIME_TYPE,

		Binary:    BLOB_TYPE,
		VarBinary: BLOB_TYPE,

		TinyBlob:         BLOB_TYPE,
		Blob:             BLOB_TYPE,
		MediumBlob:       BLOB_TYPE,
		LongBlob:         BLOB_TYPE,
		Bytea:            BLOB_TYPE,
		UniqueIdentifier: BLOB_TYPE,
	}

	// TODO remove
	FieldTypes = map[string]string{
		// 布尔
		"BOOL": "boolean",
		// 整数
		"INT":     "integer",
		"INTEGER": "integer",
		"BIGINT":  "integer",

		"CHAR":     "char",
		"VARCHAR":  "char",
		"NVARCHAR": "char",
		"TEXT":     "text",

		"MEDIUMTEXT": "text",
		"LONGTEXT":   "text",

		"DATE":       "date",
		"DATETIME":   "datetime",
		"TIME":       "datetime",
		"TIMESTAMP":  "datetime",
		"TIMESTAMPZ": "datetime",

		//Decimal = "DECIMAL"
		//Numeric = "NUMERIC"
		"REAL":   "float",
		"FLOAT":  "float",
		"DOUBLE": "float",

		"VARBINARY":  "binary",
		"TINYBLOB":   "binary",
		"BLOB":       "binary",
		"MEDIUMBLOB": "binary",
		"LONGBLOB":   "binary",
		"JSON":       "json",
		"reference":  "reference",
	}
)

// 转换值到字段输出数据类型
func value2FieldTypeValue(field IField, value interface{}) interface{} {
	type_name := field.As()
	if type_name == "" {
		type_name = field.Type()
	}

	switch strings.ToUpper(type_name) {
	case Bit, TinyInt, SmallInt, MediumInt, Int, Integer, Serial:
		return utils.Itf2Int(value)
	case BigInt, BigSerial:
		return utils.Itf2Int64(value)
	case Float, Real:
		return utils.Itf2Float32(value)
	case Double:
		return utils.Itf2Float(value)
	case Char, NChar, Varchar, NVarchar, TinyText, Text, NText, MediumText, LongText, Enum, Set, Uuid, Clob, SysName:
		return utils.Itf2Str(value)
	case TinyBlob, Blob, LongBlob, Bytea, Binary, MediumBlob, VarBinary, UniqueIdentifier:
		return value // TODO 1
	case Bool:
		return utils.Itf2Bool(value)
	case DateTime, Date, Time, TimeStamp, TimeStampz, SmallDateTime:
		return utils.Itf2Time(value)
	case Decimal, Numeric, Money, SmallMoney:
		return value // TODO 2
	default:
		return value
	}
}

// 转换值到字段数据库类型
func value2SqlTypeValue(field IField, value interface{}) interface{} {
	type_name := strings.ToUpper(field.SQLType().Name)
	switch type_name {
	case Bool:
		return utils.Itf2Bool(value)
	case Int, BigInt:
		return utils.Itf2Int64(value)
	case Char, Text:
		return utils.Itf2Str(value)
	//case Blob: // TODO Blob
	case Time:
		return utils.Itf2Time(value)
	case Json: // TODO Json

	default:
		return value
	}

	return value
}

// Type2SQLType generate SQLType acorrding Go's type
func Type2SQLType(t reflect.Type) (st SQLType) {
	if typ, ok := t.(reflect.Type); ok {
		switch k := typ.Kind(); k {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
			st = SQLType{Int, 0, 0}
		case reflect.Int64, reflect.Uint64:
			st = SQLType{BigInt, 0, 0}
		case reflect.Float32:
			st = SQLType{Float, 0, 0}
		case reflect.Float64:
			st = SQLType{Double, 0, 0}
		case reflect.Complex64, reflect.Complex128:
			st = SQLType{Varchar, 64, 0}
		case reflect.Array, reflect.Slice, reflect.Map:
			if t.Elem() == reflect.TypeOf(g_DATATYPE_BYTE_DEFAULT) {
				st = SQLType{Blob, 0, 0}
			} else {
				st = SQLType{Text, 0, 0}
			}
		case reflect.Bool:
			st = SQLType{Bool, 0, 0}
		case reflect.String:
			st = SQLType{Varchar, 255, 0}
		case reflect.Struct:
			if t.ConvertibleTo(TimeType) {
				st = SQLType{DateTime, 0, 0}
			} else {
				// TODO need to handle association struct
				st = SQLType{Text, 0, 0}
			}
		case reflect.Ptr:
			st = Type2SQLType(t.Elem())
		default:
			st = SQLType{Text, 0, 0}
		}
	}

	return
}

// default sql type change to go types
func SQLType2Type(st SQLType) reflect.Type {
	name := strings.ToUpper(st.Name)
	switch name {
	case Bit, TinyInt, SmallInt, MediumInt, Int, Integer, Serial:
		return reflect.TypeOf(1)
	case BigInt, BigSerial:
		return reflect.TypeOf(int64(1))
	case Float, Real:
		return reflect.TypeOf(float32(1))
	case Double:
		return reflect.TypeOf(float64(1))
	case Char, NChar, Varchar, NVarchar, TinyText, Text, NText, MediumText, LongText, Enum, Set, Uuid, Clob, SysName:
		return reflect.TypeOf("")
	case TinyBlob, Blob, LongBlob, Bytea, Binary, MediumBlob, VarBinary, UniqueIdentifier:
		return reflect.TypeOf([]byte{})
	case Bool:
		return reflect.TypeOf(true)
	case DateTime, Date, Time, TimeStamp, TimeStampz, SmallDateTime:
		return reflect.TypeOf(g_DATATYPE_TIME_DEFAULT)
	case Decimal, Numeric, Money, SmallMoney:
		return reflect.TypeOf("")
	default:
		return reflect.TypeOf("")
	}
}

func (s *SQLType) IsType(st int) bool {
	if t, ok := SqlTypes[s.Name]; ok && t == st {
		return true
	}
	return false
}

func (s *SQLType) IsText() bool {
	return s.IsType(TEXT_TYPE)
}

func (s *SQLType) IsBlob() bool {
	return s.IsType(BLOB_TYPE)
}

func (s *SQLType) IsTime() bool {
	return s.IsType(TIME_TYPE)
}

func (s *SQLType) IsNumeric() bool {
	return s.IsType(NUMERIC_TYPE)
}

func (s *SQLType) IsJson() bool {
	return s.Name == Json || s.Name == Jsonb
}
