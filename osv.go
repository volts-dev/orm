package orm

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/volts-dev/utils"
)

type flag uintptr

const (
	flagKindWidth        = 5 // there are 27 kinds
	flagKindMask    flag = 1<<flagKindWidth - 1
	flagRO          flag = 1 << 5
	flagIndir       flag = 1 << 6
	flagAddr        flag = 1 << 7
	flagMethod      flag = 1 << 8
	flagMethodShift      = 9
)

var (
// 提供类对比
// handlerType = reflect.TypeOf(server.THandler{})
)

type (

	// 存储一个Model的 多层继承
	// TObj 是一个多个同名不同体Model集合，这些不同体(结构体/继承结构)的MOdel最终只是同一个数据表的表现
	// fields：存储所有结构体的字段，即这个对象表的所有字段
	TObj struct {
		// 相同参数
		name               string // model 名称
		Charset            string
		Comment            string
		PrimaryKeys        []string
		indexes            map[string]*TIndex
		CreatedField       map[string]bool
		UpdatedField       string
		DeletedField       string
		VersionField       string
		AutoIncrementField string

		// SQL 参数
		columnsSeq []string //TODO 存储COl名称考虑Remove
		//columnsMap  map[string][]*Column
		//columns     []*Column
		//Cacher      Cacher
		//StoreEngine string

		// object 属性
		uidFieldName      string
		fields            map[string]IField                  // map[field]
		relations         map[string]string                  // many2many many2one... 等关联表
		relatedFields     map[string]*TRelatedField          // 关联字段如 UserId CompanyID
		commonFields      map[string]map[string]IField       //
		methods           map[string]reflect.Type            // map[func] 存储对应的Model 类型 string:函数所在的Models
		object_val        map[reflect.Type]*TModel           // map[Model] 备份对象
		object_types      map[string]map[string]reflect.Type // map[Modul][Model] 存储Models的Type
		defaultValues     map[string]interface{}             // store the default values of model
		fieldsLock        sync.RWMutex
		relationsLock     sync.RWMutex
		defaultValuesLock sync.RWMutex
		relatedFieldsLock sync.RWMutex
		commonFieldsLock  sync.RWMutex
		indexesLock       sync.RWMutex
	}

	TOsv struct {
		orm        *TOrm
		models     map[string]*TObj // 为每个Model存储BaseModel // TODO 名称或许为Objects
		modelsLock sync.RWMutex

		//_models_pool   map[string]sync.Pool // model 对象池
		//_models_fields map[string]map[string]*TField
	}
)

// add an index or an unique to table
func (self *TObj) AddIndex(index *TIndex) {
	self.indexesLock.Lock()
	self.indexes[index.Name] = index
	self.indexesLock.Unlock()
}

// add an index or an unique to table
func (self *TObj) AddField(filed IField) {
	self.fieldsLock.Lock()
	self.fields[filed.Name()] = filed
	self.fieldsLock.Unlock()
}

func (self *TObj) GetRelations() map[string]string {
	self.relationsLock.RLock()
	defer self.relationsLock.RUnlock()
	return self.relations
}

func (self *TObj) GetRelationByName(modelName string) (fieldName string) {
	self.relationsLock.RLock()
	fieldName = self.relations[modelName]
	self.relationsLock.RUnlock()
	return
}

func (self *TObj) SetRelationByName(modelName string, fieldName string) {
	self.relationsLock.Lock()
	self.relations[modelName] = fieldName
	self.relationsLock.Unlock()
	return
}

func (self *TObj) GetFields() map[string]IField {
	self.fieldsLock.RLock()
	defer self.fieldsLock.RUnlock()
	return self.fields
}

func (self *TObj) GetFieldByName(name string) IField {
	self.fieldsLock.RLock()
	defer self.fieldsLock.RUnlock()
	return self.fields[name]
}

func (self *TObj) SetFieldByName(name string, field IField) {
	self.fieldsLock.Lock()
	self.fields[name] = field
	self.fieldsLock.Unlock()
}

func (self *TObj) GetDefault() map[string]interface{} {
	self.defaultValuesLock.RLock()
	defer self.defaultValuesLock.RUnlock()
	return self.defaultValues
}

func (self *TObj) GetDefaultByName(fieldName string) (value interface{}) {
	self.defaultValuesLock.RLock()
	value = self.defaultValues[fieldName]
	self.defaultValuesLock.RUnlock()
	return
}

func (self *TObj) SetDefaultByName(fieldName string, value interface{}) {
	self.defaultValuesLock.Lock()
	self.defaultValues[fieldName] = value
	self.defaultValuesLock.Unlock()
	return
}

func (self *TObj) GetRelatedFields() (all map[string]*TRelatedField) {
	self.relatedFieldsLock.RLock()
	all = self.relatedFields
	self.relatedFieldsLock.RUnlock()
	return
}

func (self *TObj) GetRelatedFieldByName(fieldName string) (field *TRelatedField) {
	self.relatedFieldsLock.RLock()
	field = self.relatedFields[fieldName]
	self.relatedFieldsLock.RUnlock()
	return
}

func (self *TObj) SetRelatedFieldByName(fieldName string, field *TRelatedField) {
	self.relatedFieldsLock.Lock()
	self.relatedFields[fieldName] = field
	self.relatedFieldsLock.Unlock()
	return
}
func (self *TObj) GetCommonFieldByName(fieldName string) (tableField map[string]IField) {
	self.commonFieldsLock.RLock()
	tableField = self.commonFields[fieldName]
	self.commonFieldsLock.RUnlock()
	return
}

func (self *TObj) SetCommonFieldByName(fieldName string, tableName string, field IField) {
	if self.commonFields[fieldName] == nil {
		self.commonFields[fieldName] = make(map[string]IField)
	}

	self.commonFieldsLock.Lock()
	self.commonFields[fieldName][tableName] = field
	self.commonFieldsLock.Unlock()
	return
}

// 创建一个Objects Services
func newOsv(orm *TOrm) (osv *TOsv) {
	osv = &TOsv{
		models: make(map[string]*TObj),
		orm:    orm,
		//	_models_types: make(map[string]map[string]reflect.Type), // 存储Models的Type
		//	_models_pool:  make(map[string]sync.Pool),               //@@@ 改进改为接口 String
	}

	return osv
}

// TODO 重命名函数
// TODO 考虑无效层次Field检测
// 初始化/装备/配置 对象
// 初始化添加所有字段到_fileds 包括关联
// Complete the setup of models.
//
//	This must be called after loading modules and before using the ORM.
//
// /
//
//	:param partial: ``True`` if all models have not been loaded yet.
func (self *TOsv) ___SetupModels() {
	/*	var lNew *TField

		for _, obj := range self.models {

			// 合并关联字段
			for model, _ := range obj.relations {
				if refObj, has := self.models[model]; has {
					// 添加新的字段
					for refname, ref := range refObj.fields {
						if _, has := obj.fields[refname]; !has {
							*lNew = *ref //复制关联字段
							lNew.foreign_field = true
							obj.fields[refname] = ref
						}
					}
				}
			}
	*/
	/* V1
	for _, f := range obj.fields {
		// 如果该字段是关联字段则将关联表的所有字段复制该Model的所有字段
		if f.related {
			lModel := f.comodel_name
			if refObj, has := self.models[lModel]; has {
				// 添加新的字段
				for refname, ref := range refObj.fields {
					if _, has := obj.fields[refname]; !has {
						*lNew = *ref //复制关联字段
						lNew.foreign_field = true
						obj.fields[refname] = ref
					}
				}
			}
		}
	}*/
	//	}
}

// New an object for restore
func (self *TOsv) newObject(name string) *TObj {
	obj := &TObj{
		name:          name,                    // model 名称
		fields:        make(map[string]IField), // map[field]
		relations:     make(map[string]string),
		relatedFields: make(map[string]*TRelatedField),
		commonFields:  make(map[string]map[string]IField),
		//common_fields :make(map[string]*TRelatedField)
		methods:       make(map[string]reflect.Type),            // map[func][] 存储对应的Model 类型 string:函数所在的Models
		object_val:    make(map[reflect.Type]*TModel),           // map[Model] 备份对象
		object_types:  make(map[string]map[string]reflect.Type), // map[Modul][Model] 存储Models的Type
		defaultValues: make(map[string]interface{}),

		CreatedField: make(map[string]bool),
		indexes:      make(map[string]*TIndex),
		columnsSeq:   make([]string, 0),
	}
	/*obj := self.models[name]
	if obj == nil {
		obj = self.newObj(name)
		self.models[name] = obj
	}
	*/
	return obj
}

// register new model to the object service
func (self *TOsv) RegisterModel(region string, model *TModel) error {
	//获得Object 检查是否存在，不存在则创建
	self.modelsLock.RLock()
	obj := self.models[model.name]
	self.modelsLock.RUnlock()

	if obj == nil && model.obj != nil {
		obj = model.obj
	} else {
		new_obj := model.obj

		// # 复制 Indx
		for _, idx := range new_obj.indexes {
			if _, has := obj.indexes[idx.Name]; !has {
				obj.AddIndex(idx)
			}
		}

		// # 复制 Key
		for _, key := range new_obj.PrimaryKeys {
			if utils.InStrings(key, obj.PrimaryKeys...) == -1 {
				obj.PrimaryKeys = append(obj.PrimaryKeys, key)
			}
		}

		for field, on := range new_obj.CreatedField {
			if _, has := obj.CreatedField[field]; !has {
				obj.CreatedField[field] = on
			}
		}

		if obj.DeletedField == "" && new_obj.DeletedField != "" {
			obj.DeletedField = new_obj.DeletedField
		}

		if obj.UpdatedField == "" && new_obj.UpdatedField != "" {
			obj.UpdatedField = new_obj.UpdatedField
		}

		if obj.AutoIncrementField == "" && new_obj.AutoIncrementField != "" {
			obj.AutoIncrementField = new_obj.AutoIncrementField
		}

		if obj.UpdatedField == "" && new_obj.UpdatedField != "" {
			obj.UpdatedField = new_obj.UpdatedField
		}

		if obj.VersionField == "" && new_obj.VersionField != "" {
			obj.VersionField = new_obj.VersionField
		}

		// 覆盖默认值
		obj.defaultValuesLock.Lock()
		for key, val := range new_obj.defaultValues {
			obj.defaultValues[key] = val
		}
		obj.defaultValuesLock.Unlock()

		// #添加字段
		obj.fieldsLock.Lock()
		for name, field := range new_obj.fields {
			obj.fields[name] = field
		}
		obj.fieldsLock.Unlock()

		// #关联表
		obj.relationsLock.Lock()
		for name, field := range new_obj.relations {
			obj.relations[name] = field
		}
		obj.relationsLock.Unlock()

		// #共同字段
		obj.commonFieldsLock.Lock()
		for name, value := range new_obj.commonFields {
			obj.commonFields[name] = value
		}
		obj.commonFieldsLock.Unlock()
	}

	// 为该Model对应的Table
	//	obj.object_table[model.modelType] = model.table

	// 赋值
	if _, has := obj.object_types[region]; !has {
		obj.object_types[region] = make(map[string]reflect.Type)
	}

	//STEP 添加Model 类型
	obj.object_types[region][model.name] = model.modelType

	// 原型Tmodel已经不再需要
	if region != "" {
		delete(obj.object_types, "")
	}
	/*
		//utils.Dbg("RegisterModel Method", aType, aType.NumField(), aType.NumMethod())
		// @添加方法映射到对象里
		var method reflect.Method
		for i := 0; i < model.modelType.NumMethod(); i++ {
			method = model.modelType.Method(i)
			//utils.Dbg("RegisterModel Method", lMethod.Type.In(1).Elem(), handlerType)
			// 参数验证func(self,handler)
			//lMethod.Type.In(1).Elem().String() == handlerType.String()
			//log.Dbg("RegisterModel Method", lMethod.Type.NumIn(), lMethod.Name, lMethod.PkgPath, lMethod.Type)

			//if lMethod.Type.NumIn() == 2 {
			obj.methods[method.Name] = model.modelType // 添加方法对应的Object
			//}
		}
	*/
	obj.mappingMethod(model)
	obj.uidFieldName = model.idField

	self.modelsLock.Lock()
	self.models[model.name] = obj
	self.modelsLock.Unlock()
	return nil
}

func (self *TObj) mappingMethod(model *TModel) {
	//utils.Dbg("RegisterModel Method", aType, aType.NumField(), aType.NumMethod())
	// @添加方法映射到对象里
	var method reflect.Method
	for i := 0; i < model.modelType.NumMethod(); i++ {
		method = model.modelType.Method(i)
		//utils.Dbg("RegisterModel Method", lMethod.Type.In(1).Elem(), handlerType)
		// 参数验证func(self,handler)
		//lMethod.Type.In(1).Elem().String() == handlerType.String()
		//log.Dbg("RegisterModel Method", lMethod.Type.NumIn(), lMethod.Name, lMethod.PkgPath, lMethod.Type)

		//if lMethod.Type.NumIn() == 2 {
		self.methods[method.Name] = model.modelType // 添加方法对应的Object
		//}
	}
}

// 根据Model和Action 执行方法
// Action 必须是XxxXxxx格式
func (self *TOsv) GetMethod(modelName, methodName string) (method *TMethod) {
	modelVal := self.getModelByMethod(modelName, methodName)
	if modelVal.IsValid() { //|| !lM.IsNil()
		// 转换method
		// #必须使用Type才能获取到方法原型已经参数
		method, ok := modelVal.Type().MethodByName(utils.TitleCasedName(methodName))
		if ok && method.Func.IsValid() {
			return NewMethod(methodName, method.Func)
		}
	}

	return nil
}

func (self *TOsv) HasModel(name string) (has bool) {
	_, has = self.models[name]
	return
}

// TODO  TEST 测试是否正确使用路劲作为Modul
func (self *TOsv) GetModel(name string, module ...string) (IModel, error) {
	module_name := "" // "web" // 默认取Web模块注册的Models
	if len(module) > 0 && utils.Trim(module[0]) != "" {
		module_name = utils.Trim(module[0])
	} else {
		//TODO 实现智能选
		/*
			//_, lFilePath, _, ok := runtime.Caller(1)
			//lAppPath := utils.AppDir()
			//lFilePath := utils.CurPath()
			//lFilePath := strings.TrimLeft(utils.CurPath(), utils.AppDir())
			//lDirLst := strings.Split(lFilePath, string(filepath.Separator))
			lFilePath := filepath.FromSlash(utils.CurFilePath()) //strings.TrimLeft(utils.CurFilePath(), utils.AppFilePath())
			lDirLst := strings.Split(lFilePath, string(filepath.Separator))
			if idx := utils.InStrings("module", lDirLst...); idx > -1 {
				lModule = lDirLst[idx+1]
			}
			log.Dbg("getmodel", utils.CurPath(), lModule, lFilePath, utils.AppDir(), lDirLst)
			//if len(lDirLst) > 1 { // && lDirLst[0] == AppModuleDir
			//	lModule = lDirLst[1]
			//}
		*/
	}

	model, err := self.GetModelByModule(module_name, fmtModelName(name))
	if err != nil {
		return nil, err
	}

	if m, ok := model.(IModel); ok {
		return m, nil
	}

	return nil, errors.New("Model is not a interface of IModel")
}

func (self *TOsv) RemoveModel(name string) {
	self.modelsLock.Lock()
	delete(self.models, name)
	self.modelsLock.RUnlock()
}

func (self *TOsv) GetModels() (models []string) {
	models = make([]string, len(self.models))

	idx := 0
	for name := range self.models {
		models[idx] = name
		idx++
	}

	return models
}

// @ name
// @ Session
// @ Registry
func (self *TOsv) NewModel(name string) (model *TModel) {
	model = &TModel{
		name: name,
		//_table:  strings.Replace(name, ".", "_", -1),
		//_fields: make(map[string]IField),
	}

	//mdl._sequence = mdl._table + "_id_seq"

	return
}

// TODO　优化更简洁
// 每次GetModel都会激活初始化对象
func (self *TOsv) initObject(val reflect.Value, atype reflect.Type, obj *TObj, modelName string) {
	if m, ok := val.Interface().(IModel); ok {
		// NOTED <以下代码严格遵守执行顺序>
		model := newModel(modelName, "", val, atype) //self.newModel(sess, model)
		model.idField = obj.uidFieldName
		model.obj = obj
		model.osv = self
		model.orm = self.orm
		model.relations_reload()
		m.setBaseModel(model)
	}
}

// #module 可以为空取默认
func (self *TOsv) getModelByModule(region, model string) (val reflect.Value) {
	var (
		region_name string
		module_map  map[string]reflect.Type
		model_type  reflect.Type
	)

	//获取Model的Object对象
	if obj, has := self.models[model]; has {
		//log.Dbg("getModelByModule1", obj, len(self.models), region, model)

		// 非常重要 检查并返回唯一一个，或指定module_name 循环最后获得的值
		for region_name, module_map = range obj.object_types {
			if region_name == region {
				break
			}
		}

		//log.Dbg("_getModelByModule2", region_name, module_map)

		if model_type, has = module_map[model]; has {
			// 创建对象
			val = reflect.New(model_type)
			self.initObject(val, model_type, obj, model)
			/*
				// 使用接口对TModel进行赋值
				if m, ok := val.Interface().(IModel); ok {
					lModel := NewModel(model, session) //self.newModel(sess, model)
					lModel._fields = obj.fields

					m.setBaseModel(lModel)
					//m.SetName(name)
					//m.SetRegistry(self)
					//web.Warn("使用接口对TModel进行赋值", m, lVal)

					//return m
				}*/
			return val
		}
	}

	return
}

// TODO 继承类Model 的方法调用顺序提取
func (self *TOsv) getModelByMethod(model string, method string) (val reflect.Value) {
	if obj, has := self.models[model]; has {
		if typ, has := obj.methods[utils.TitleCasedName(method)]; has {
			val = reflect.New(typ)
			self.initObject(val, typ, obj, model)
			return
		}
	}

	return
}

func (self *TOsv) Models() map[string]*TObj {
	return self.models
}

func (self *TOsv) GetModelByModule(region, model string) (res IModel, err error) {
	if model == "" {
		return nil, errors.New("Must enter a model name!")
	}

	mod := self.getModelByModule(region, model)
	if mod.IsValid() && !mod.IsNil() {
		if m, ok := mod.Interface().(IModel); ok {
			return m, nil
		}

		log.Panicf(`Model %s@%s is not a standard orm.IModel type,
		please check the name of Fields and Methods,make sure they are correct and not same each other`, model, region)
	}

	return nil, fmt.Errorf("Model %s@%s is not a standard model type of this system", model, region)
}
