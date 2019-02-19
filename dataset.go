package orm

/** 数据集
 */

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"
	"vectors/logger"
	"vectors/utils"
)

type (
	TFieldSet struct {
		//DataSet *TDataSet
		RecSet  *TRecordSet
		Name    string
		IsValid bool // the field is using on dataset or temp field
	}

	TRecordSet struct {
		DataSet       *TDataSet
		Fields        []string
		Values        []interface{} // []string
		ClassicValues []interface{} // 存储经典字段值
		NameIndex     map[string]int
		Length        int
		IsEmpty       bool
	}

	TDataSet struct {
		Name   string                // table name
		Data   []*TRecordSet         // []map[string]interface{}
		Fields map[string]*TFieldSet //保存字段
		//Delta // 修改过的
		KeyField     string                 // 主键字段
		RecordsIndex map[string]*TRecordSet // 主键引索 // for RecordByKey() Keys()
		Position     int                    // 游标
		//Count int

		FieldCount int //字段数

		classic   bool // 是否存储着经典模式的数据 many2one字段会显示ID和Name
		_pos_lock sync.RWMutex
	}
)

var (
//blank_RecordSet = NewRecordSet(nil)
//blank_FieldSet = NewRecordSet(nil)
)

func newFieldSet(name string, recset *TRecordSet) (res_recset *TFieldSet) {
	return &TFieldSet{
		RecSet:  recset,
		Name:    name,
		IsValid: false,
	}
}

func NewRecordSet(record ...map[string]interface{}) (res_recset *TRecordSet) {
	res_recset = &TRecordSet{
		//DataSet:       dataSet,
		Fields:        make([]string, 0),
		Values:        make([]interface{}, 0),
		ClassicValues: make([]interface{}, 0),
		NameIndex:     make(map[string]int),
		Length:        0,
	}

	if len(record) == 0 {
		res_recset.IsEmpty = true
		return
	}

	var lIdx int
	for field, val := range record[0] {
		lIdx = len(res_recset.Fields)
		res_recset.NameIndex[field] = lIdx // 先于 lRec.Fields 添加不需 -1
		res_recset.Fields = append(res_recset.Fields, field)
		res_recset.Values = append(res_recset.Values, val)

	}
	//#优先计算长度供Get Set设置
	res_recset.Length = lIdx + 1
	res_recset.IsEmpty = false

	return
}

func NewDataSet() *TDataSet {
	return &TDataSet{
		Position: 0,
		//	KeyField:     "id",
		Data:         make([]*TRecordSet, 0),
		Fields:       make(map[string]*TFieldSet),
		RecordsIndex: make(map[string]*TRecordSet),
		//Count: 0,
	}
}

func (self *TFieldSet) AsInterface(src ...interface{}) (result interface{}) {
	if len(src) != 0 {
		self.RecSet._setByName(self, self.Name, src[0], false)
		return src[0]
	}

	if self == nil {
		return nil
	} else {
		return self.RecSet._getByName(self.Name, false)
	}

	logger.Logger.Err("Can not covert value into interface{} since FieldSet is nil!")
	return
}

func (self *TFieldSet) AsClassic(src ...interface{}) (result interface{}) {
	if len(src) != 0 {
		self.RecSet._setByName(self, self.Name, src[0], true)
		return src[0]
	}
	//fmt.Println("AsString", self, self.Name)
	if self != nil {
		return self.RecSet._getByName(self.Name, true)
	}

	logger.Logger.Err("Can not covert value into interface{} since FieldSet is nil!")
	return
}

func (self *TFieldSet) AsString(src ...string) (result string) {
	if len(src) != 0 {
		self.RecSet._setByName(self, self.Name, src[0], false)
		return src[0]
	}
	if self == nil {
		return ""
	} else {
		return utils.Itf2Str(self.RecSet._getByName(self.Name, false))
	}

	logger.Logger.Err("Can not covert value into string since FieldSet is nil!")
	panic("")
	return ""
}

func (self *TFieldSet) AsInteger(src ...int64) (result int64) {
	if len(src) != 0 {
		self.RecSet._setByName(self, self.Name, src[0], false)
		return src[0]
	}

	if self == nil {
		return 0
	} else {
		return utils.Itf2Int64(self.RecSet._getByName(self.Name, false))
	}
	logger.Logger.Err("Can not covert value into int64 since FieldSet is nil!")
	return 0
}

func (self *TFieldSet) AsBoolean(src ...bool) (result bool) {
	if len(src) != 0 {
		self.RecSet._setByName(self, self.Name, src[0], false)
		return src[0]
	}

	if self == nil {
		return false
	} else {
		return utils.Itf2Bool(self.RecSet._getByName(self.Name, false))
	}

	logger.Logger.Err("Can not covert value into bool since FieldSet is nil!")
	return false
}

func (self *TFieldSet) AsDateTime(src ...time.Time) (result time.Time) {
	if len(src) != 0 {
		self.RecSet._setByName(self, self.Name, src[0].Format(time.RFC3339), false)
		return src[0]
	}

	if self == nil {
		return time.Time{}
	} else {
		return utils.Itf2Time(self.RecSet._getByName(self.Name, false))
	}
	logger.Logger.Err("Can not covert value into time.Time since FieldSet is nil!")
	return
}

func (self *TFieldSet) AsFloat(src ...float64) (result float64) {
	if len(src) != 0 {
		self.RecSet._setByName(self, self.Name, src[0], false)
		return src[0]
	}

	if self == nil {
		return 0.0
	} else {
		return utils.Itf2Float(self.RecSet._getByName(self.Name, false))
	}
	logger.Logger.Err("Can not covert value into float64 since FieldSet is nil!")
	return
}

// TODO 函数改为非Exported
func (self *TRecordSet) Get(index int, classic bool) interface{} {
	if index >= self.Length {
		return ""
	}

	if classic {
		return self.ClassicValues[index]
	} else {
		return self.Values[index]
	}

	return nil
}

// TODO 函数改为非Exported
func (self *TRecordSet) Set(index int, value interface{}, classic bool) bool {
	if index >= self.Length {
		return false
	}

	if classic {
		self.ClassicValues[index] = value
	} else {
		self.Values[index] = value
	}

	return true
}

func (self *TRecordSet) _getByName(name string, classic bool) interface{} {
	if index, ok := self.NameIndex[name]; ok {
		return self.Get(index, classic)
	}

	return ""
}

func (self *TRecordSet) _setByName(fs *TFieldSet, name string, value interface{}, classic bool) bool {
	//字段被纳入Dataset.Fields
	fs.IsValid = true

	if index, ok := self.NameIndex[name]; ok {
		return self.Set(index, value, classic)
	} else {
		self.NameIndex[name] = len(self.Values)
		self.Fields = append(self.Fields, name)
		//self.Values = append(self.Values, value) //TODO
		if classic {
			self.ClassicValues = append(self.ClassicValues, value)
		} else {
			self.Values = append(self.Values, value)
			//self.ClassicValues = append(self.ClassicValues, nil)
		}

		self.Length = len(self.Values)
	}

	return true
}

func (self *TRecordSet) GetByIndex(index int) (res *TFieldSet) {
	// 检查零界
	if index >= self.Length {
		return
	}
	field := self.Fields[index]
	if self.DataSet != nil {
		// 检查零界
		if len(self.DataSet.Fields) != self.Length {
			return
		}

		res = self.DataSet.Fields[field]
		res.RecSet = self
		return //self.Values[index]
	} else {
		// 创建一个空的
		res = newFieldSet(field, self)
		res.IsValid = field != ""
		/*res = &TFieldSet{
			//DataSet: self.DataSet,
			RecSet: self,
			Name:   field,
			IsNil:  true,
		}*/
	}

	return nil
}

// 获取某个
func (self *TRecordSet) GetByName(name string) (field *TFieldSet) {
	var has bool

	// 优先验证Dataset
	if self.DataSet != nil {
		if field, has = self.DataSet.Fields[name]; has {
			if field != nil {
				field.RecSet = self
				return //self.Values[index]
			}
		}
	} else {

		// 创建一个空的
		field = newFieldSet(name, self)
		field.IsValid = utils.InStrings(name, self.Fields...) != -1
		/*field = &TFieldSet{
			//DataSet: self.DataSet,
			RecSet: self,
			Name:   name,
			//IsNil:  true,
		}*/
	}

	return
}

func (self *TRecordSet) AsStrMap() (res map[string]string) {
	res = make(map[string]string)
	for idx, field := range self.Fields {
		res[field] = utils.Itf2Str(self.Values[idx])
	}

	return
}

func (self *TRecordSet) AsItfMap() (res map[string]interface{}) {
	res = make(map[string]interface{})

	//fmt.Println("AsItfMap:", len(self.Fields), len(self.Values))
	//logger.Dbg("AsItfMap:", len(self.Fields), len(self.Values))
	for idx, field := range self.Fields {
		res[field] = self.Values[idx]
	}

	return
}

func (self *TRecordSet) AsJson() (res string) {
	js, err := json.Marshal(self.AsItfMap())
	logger.LogErr(err)
	return string(js)
}

func (self *TRecordSet) AsXml() (res string) {

	return
}

func (self *TRecordSet) AsCsv() (res string) {

	return
}

// terget must be a pointer value
func (self *TRecordSet) AsStruct(target interface{}, classic ...bool) {
	// 使用经典数据模式
	lClassic := false
	if len(classic) > 0 {
		lClassic = classic[0]
	}

	lStruct := reflect.Indirect(reflect.ValueOf(target))
	if lStruct.Kind() == reflect.Ptr {
		lStruct = lStruct.Elem()
	}
	//logger.Dbg("AsStruct", lStruct, lStruct.Kind())
	for idx, name := range self.Fields {
		lFieldValue := lStruct.FieldByName(utils.TitleCasedName(name))
		if !lFieldValue.IsValid() || !lFieldValue.CanSet() {
			//logger.Logger.Err("table %v's column %v is not valid or cannot set", self.DataSet.Name, name)
			continue
		}

		//lFieldType := lFieldValue.Type()
		var lItfVal interface{}
		var lVal reflect.Value
		if lClassic {
			//self.DataSet.
			//lVal = reflect.ValueOf(self.ClassicValues[idx])
			lItfVal = self.ClassicValues[idx]
		} else {
			//lVal = reflect.ValueOf(self.Values[idx])
			lItfVal = self.Values[idx]
		}

		// 不设置Nil值
		if lItfVal == nil {
			continue
		}

		//logger.Dbg("AsStruct", name, lFieldValue.Type(), lItfVal, reflect.TypeOf(lItfVal), lVal, self.Values[idx])
		if lFieldValue.Type().Kind() != reflect.TypeOf(lItfVal).Kind() {
			switch lFieldValue.Type().Kind() {
			case reflect.Bool:
				lItfVal = utils.Itf2Bool(lItfVal)
			case reflect.String:
				lItfVal = utils.Itf2Str(lItfVal)
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				lItfVal = utils.Itf2Int64(lItfVal)
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				lItfVal = utils.Itf2Int64(lItfVal)
			case reflect.Float32:
				lItfVal = utils.Itf2Float32(lItfVal)
			case reflect.Float64:
				lItfVal = utils.Itf2Float(lItfVal)
			//case reflect.Array, reflect.Slice:
			case reflect.Struct:
				var c_TIME_DEFAULT time.Time
				TimeType := reflect.TypeOf(c_TIME_DEFAULT)
				if lFieldValue.Type().ConvertibleTo(TimeType) {
					lItfVal = utils.Itf2Time(lItfVal)
				}
			default:
				logger.Logger.Errf("Unsupported struct type %v", lFieldValue.Type().Kind())
				continue
			}
		}

		lVal = reflect.ValueOf(lItfVal)
		lFieldValue.Set(lVal)
	}

}

func (self *TRecordSet) MergeToStrMap(target map[string]string) (res map[string]string) {
	for idx, field := range self.Fields {
		target[field] = utils.Itf2Str(self.Values[idx])
	}

	return target
}

//TODO  当TDataSet无数据是返回错误
//TODO HasField()bool
func (self *TDataSet) FieldByName(field string) (fieldSet *TFieldSet) {
	var has bool
	if fieldSet, has = self.Fields[field]; has {
		//fmt.Println("FieldByName has", fieldSet, fieldSet)
		fieldSet.RecSet = self.Record() // self.Data[self.Position]
		return
	} else {
		// 创建一个空的
		fieldSet = newFieldSet(field, self.Record())
		/*fieldSet = &TFieldSet{
			//			DataSet: self,
			Name:   field,
			RecSet: self.Record(), // self.Data[self.Position],
			IsNil:  true,
		}*/
	}

	return
}

//
func (self *TDataSet) IsEmpty() bool {
	return len(self.Data) == 0
}

//
func (self *TDataSet) Count() int {
	return len(self.Data)
}

func (self *TDataSet) First() {
	self._pos_lock.Lock()
	defer self._pos_lock.Unlock()
	self.Position = 0
}

func (self *TDataSet) Next() {
	self._pos_lock.Lock()
	defer self._pos_lock.Unlock()
	self.Position++
}

//废弃 TODO last
func (self *TDataSet) Eof() bool {
	return self.Position == len(self.Data)
}

func (self *TDataSet) EOF() bool {
	return self.Position == len(self.Data)
}

// Get current record
func (self *TDataSet) Record() *TRecordSet {
	if len(self.Data) == 0 {
		return NewRecordSet()
	}

	if rs := self.Data[self.Position]; rs != nil {
		return rs
	}

	return nil
}

// #检验字段合法
//TODO 简化
func (self *TDataSet) check_fields(record *TRecordSet) error {
	// #优先记录该数据集的字段
	//fmt.Println("check_fields", len(self.Fields), len(record.Fields), self.Count())
	if len(self.Fields) == 0 && self.Count() < 1 {
		for _, field := range record.Fields {
			if field != "" { // TODO 不应该有空值 需检查
				//fmt.Println("field", field)
				fieldSet := newFieldSet(field, nil)
				/*fieldSet := &TFieldSet{
					//					DataSet: self,
					//RecSet:  self.Data[self.Position],
					Name: field,
				}*/
				self.Fields[field] = fieldSet
			}
		}

		//# 添加字段长度
		self.FieldCount = len(self.Fields)
		return nil
	}

	//#检验字段合法
	for _, field := range record.Fields {
		if field != "" {
			if _, has := self.Fields[field]; !has {
				return fmt.Errorf("field %v is not in dataset!", field)
			}
		}
	}

	return nil
}

//#添加新纪录 当Dataset 记录为空时,第一条记录的字段将成为其他记录的标准
func (self *TDataSet) AppendRecord(Record ...*TRecordSet) error {
	var fields map[string]int
	for _, rec := range Record {
		if rec == nil {
			continue
		}

		if fields == nil {
			fields = rec.NameIndex
		}

		if err := self.check_fields(rec); err != nil {
			logger.Logger.Errf(`TDataSet.AppendRecord():%v`, err.Error())

		} else { //#TODO 考虑是否为复制
			rec.DataSet = self //# 将其归为
			self.Data = append(self.Data, rec)
			self.Position = len(self.Data) - 1
		}
	}

	return nil
}

//push row to dataset
func (self *TDataSet) NewRecord(Record map[string]interface{}) bool {
	//var lRec *TRecordSet
	//logger.Dbg("idex", Record)
	lRec := NewRecordSet(Record)

	//if err := self.check_fields(lRec); err != nil {
	//	logger.Logger.ErrLn(err.Error())
	//}

	self.AppendRecord(lRec)
	//	var err error
	/*	lValue := ""
		for field, val := range Record {
			if val == nil {
				lValue = ""
			} else {
				rawValue := reflect.Indirect(reflect.ValueOf(val))
				//if row is null then ignore
				if rawValue.Interface() == nil {
					continue
				}

				lValue, err = val2Str(&rawValue)
				if logger.LogErr(err) {
					return false
				}

			}
			//Record[field] = data
			lRec.NameIndex[field] = len(lRec.Fields) // 先于 lRec.Fields 添加不需 -1
			lRec.Fields = append(lRec.Fields, field)
			lRec.Values = append(lRec.Values, lValue)

			if self.KeyField != "" {
				if field == self.KeyField || field == "id" {
					self.RecordsIndex[lValue] = lRec //保存ID 对应的 Record
				}
			}

		}
	*/
	/* # 非Count查询时提供多行索引
	if self.KeyField != "" && len(lRec.Fields) > 1 && lRec.GetByName("count") == nil {
		lIdSet := lRec.GetByName(self.KeyField)
		if lIdSet != nil {
			self.RecordsIndex[lIdSet.AsString()] = lRec //保存ID 对应的 Record
		}
	}
	*/

	return true
}

func (self *TDataSet) Delete(idx ...int) bool {
	cnt := len(self.Data)
	if cnt == 0 {
		return true
	}

	pos := self.Position
	if len(idx) > 0 {
		pos = idx[0]
	}

	// 超出边界
	if pos >= cnt || pos < 0 {
		return false
	}

	self.Data = append(self.Data[:pos], self.Data[pos+1:]...)

	return true
}

func (self *TDataSet) DeleteRecord(Key string) bool {
	return true
}

//考虑
func (self *TDataSet) EditRecord(Key string, Record map[string]interface{}) bool {
	return true
}

func (self *TDataSet) RecordByField(field string, val interface{}) (rec *TRecordSet) {
	if field == "" || val == nil {
		return nil
	}

	for _, rec = range self.Data {
		i := rec.NameIndex[field]
		if rec.Values[i] == val {
			return rec
		}
	}
	return
}

// 获取对应KeyFieldd值
func (self *TDataSet) RecordByKey(Key string, key_field ...string) (rec *TRecordSet) {
	if len(self.RecordsIndex) == 0 {
		if self.KeyField == "" {
			if len(key_field) == 0 {
				logger.Logger.Err(`You should point out the key_field name!`) //#重要提示
			} else {
				if !self.SetKeyField(key_field[0]) {
					logger.Logger.Errf(`Set key_field fail when call RecordByKey(key_field:%v)!`, key_field[0])
				}
			}
		} else {
			if !self.SetKeyField(self.KeyField) {
				logger.Logger.Errf(`Set key_field fail when call RecordByKey(self.KeyField:%v)!`, self.KeyField)
			}
		}
	}

	//idx := self.RecordsIndex[Key]
	return self.RecordsIndex[Key]
}

func (self *TDataSet) SetKeyField(key_field string) bool {
	// # 非空或非Count查询时提供多行索引
	if self.Count() == 0 || (self.FieldByName(key_field) == nil && len(self.Record().Fields) == 1 && self.Record().GetByName("count") != nil) {
		return false
	}

	self.KeyField = key_field

	// #全新
	self.RecordsIndex = make(map[string]*TRecordSet)

	// #赋值
	for _, rec := range self.Data {
		//fmt.Println("idccc", key_field, rec, len(self.RecordsIndex))
		lIdSet := rec.GetByName(key_field)
		//fmt.Println("idccc", key_field, lIdSet, len(self.RecordsIndex))
		if lIdSet != nil {
			self.RecordsIndex[lIdSet.AsString()] = rec //保存ID 对应的 Record
		}
	}

	return true
}

func (self *TDataSet) IsClassic() bool {
	return self.classic
}

// 返回所有记录的主键值
func (self *TDataSet) Keys(field ...string) (res []string) {
	// #默认
	lKeyField := "id"

	if self.KeyField != "" {
		lKeyField = self.KeyField
	}

	// #新的Key
	if len(field) > 0 {
		lKeyField = field[0]
	}

	if self.KeyField == lKeyField {
		if self.Count() > 0 && len(self.RecordsIndex) == 0 {
			self.SetKeyField(self.KeyField)
		}
	} else {
		self.SetKeyField(lKeyField)
	}

	res = make([]string, 0)
	for key, _ := range self.RecordsIndex {
		res = append(res, key)
	}

	return
}

func __equal2Str(val1 string, val2 interface{}) bool {
	rawValue := reflect.Indirect(reflect.ValueOf(val2))
	//if row is null then ignore
	if rawValue.Interface() == nil {

	}
	lValue, err := val2Str(&rawValue)
	if err != nil {
		return false
	}

	return val1 == lValue
}

func val2Str(rawValue *reflect.Value) (data string, err error) {
	data, err = rft2val(rawValue)
	if err != nil {
		return
	}
	return
}

func rft2val(rawValue *reflect.Value) (str string, err error) {
	aa := reflect.TypeOf((*rawValue).Interface())
	vv := reflect.ValueOf((*rawValue).Interface())
	switch aa.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		str = strconv.FormatInt(vv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		str = strconv.FormatUint(vv.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		str = strconv.FormatFloat(vv.Float(), 'f', -1, 64)
	case reflect.String:
		str = vv.String()
	case reflect.Array, reflect.Slice:
		switch aa.Elem().Kind() {
		case reflect.Uint8:
			data := rawValue.Interface().([]byte)
			str = string(data)
		default:
			err = fmt.Errorf("Unsupported struct type %v", vv.Type().Name())
		}
	//时间类型
	case reflect.Struct:
		var c_TIME_DEFAULT time.Time
		TimeType := reflect.TypeOf(c_TIME_DEFAULT)
		if aa.ConvertibleTo(TimeType) {
			str = vv.Convert(TimeType).Interface().(time.Time).Format(time.RFC3339Nano)
		} else {
			err = fmt.Errorf("Unsupported struct type %v", vv.Type().Name())
		}
	case reflect.Bool:
		str = strconv.FormatBool(vv.Bool())
	case reflect.Complex128, reflect.Complex64:
		str = fmt.Sprintf("%v", vv.Complex())
	/* TODO: unsupported types below
	   case reflect.Map:
	   case reflect.Ptr:
	   case reflect.Uintptr:
	   case reflect.UnsafePointer:
	   case reflect.Chan, reflect.Func, reflect.Interface:
	*/
	default:
		err = fmt.Errorf("Unsupported struct type %v", vv.Type().Name())
	}
	return
}
