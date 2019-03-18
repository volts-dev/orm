package orm

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/go-xorm/core"
	"github.com/volts-dev/logger"
	"github.com/volts-dev/utils"
	"github.com/volts-dev/volts/server"
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
//handlerType = reflect.TypeOf(server.THandler{})
)

type (

	// 存储一个Model的 多层继承
	// TObj 是一个多个同名不同体Model集合，这些不同体(结构体/继承结构)的MOdel最终只是同一个数据表的表现
	// fields：存储所有结构体的字段，即这个对象表的所有字段
	TObj struct {
		name          string                       // model 名称
		fields        map[string]IField            // map[field]
		relations     map[string]string            // many2many many2one... 等关联表
		relate_fields map[string]*TRelateField     // 关联字段如 UserId CompanyID
		common_fields map[string]map[string]IField // 关联字段如 UserId CompanyID

		methods      map[string]reflect.Type            // map[func] 存储对应的Model 类型 string:函数所在的Models
		object_val   map[reflect.Type]*TModel           // map[Model] 备份对象
		object_types map[string]map[string]reflect.Type // map[Modul][Model] 存储Models的Type
		object_table map[reflect.Type]*core.Table
		//id_caches    cache.ICacher //废弃 缓存Id 对应记录
		//sql_caches   cache.ICacher //废弃 缓存Sql

	}

	TOsv struct {
		models map[string]*TObj // 为每个Model存储BaseModel // TODO 名称或许为Objects
		orm    *TOrm
		//_models_pool   map[string]sync.Pool // model 对象池
		//_models_fields map[string]map[string]*TField

		//Registry *TRegistry
		//Reader *TOrm
		//Writer *TOrm
	}
)

// 创建一个Objects Services
func NewOsv(orm *TOrm) (res *TOsv) {
	res = &TOsv{
		models: make(map[string]*TObj),
		orm:    orm,
		//	_models_types: make(map[string]map[string]reflect.Type), // 存储Models的Type
		//	_models_pool:  make(map[string]sync.Pool),               //@@@ 改进改为接口 String

		//Registry: aRegistry,
	}
	//utils.Dbg("NewOsv", res)
	//res.Orm = NewOrm()
	return
}

//TODO 重命名函数
//TODO 考虑无效层次Field检测
// 初始化/装备/配置 对象
// 初始化添加所有字段到_fileds 包括关联
// Complete the setup of models.
//    This must be called after loading modules and before using the ORM.
///
//     :param partial: ``True`` if all models have not been loaded yet.
//
func (self *TOsv) SetupModels() {
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

// 注册新的Model
func (self *TOsv) RegisterModel(region string, aModel *TModel) {
	var lObj *TObj
	var lMethod reflect.Method

	logger.Dbg("RegisterModel:", region, aModel._name, aModel._cls_type, aModel._cls_type.PkgPath())
	//获得Object 检查是否存在，不存在则创建
	if obj, has := self.models[aModel._name]; !has {
		lObj = self.new_obj(aModel._name)
		lObj.name = aModel._name // Model 名称
		self.models[aModel._name] = lObj
		logger.Dbg("!has", region, aModel._name)
	} else {
		lObj = obj
	}

	// 为该Model对应的Table
	lObj.object_table[aModel._cls_type] = aModel.table

	// 赋值
	if _, has := lObj.object_types[region]; !has {
		lObj.object_types[region] = make(map[string]reflect.Type)
	}

	//STEP 添加Model 类型
	lObj.object_types[region][aModel._name] = aModel._cls_type

	//utils.Dbg("RegisterModel Method", aType, aType.NumField(), aType.NumMethod())
	// @添加方法映射到对象里
	for i := 0; i < aModel._cls_type.NumMethod(); i++ {
		lMethod = aModel._cls_type.Method(i)
		//utils.Dbg("RegisterModel Method", lMethod.Type.In(1).Elem(), handlerType)
		// 参数验证func(self,handler)
		//lMethod.Type.In(1).Elem().String() == handlerType.String()
		//logger.Dbg("RegisterModel Method", lMethod.Type.NumIn(), lMethod.Name, lMethod.PkgPath, lMethod.Type)

		//if lMethod.Type.NumIn() == 2 {
		lObj.methods[lMethod.Name] = aModel._cls_type // 添加方法对应的Object
		//}

	}

	// #添加字段
	for name, field := range aModel._fields {
		lObj.fields[name] = field
	}

	// #关联表
	for name, field := range aModel._relations {
		lObj.relations[name] = field
	}

	// #关联字段
	for name, field := range aModel._relate_fields {
		lObj.relate_fields[name] = field
	}

	// #共同字段
	for name, field := range aModel._common_fields {
		lObj.common_fields[name] = field
	}
}

func (self *TOsv) new_obj(name string) (obj *TObj) {
	obj = &TObj{
		name:          name,                    // model 名称
		fields:        make(map[string]IField), // map[field]
		relations:     make(map[string]string),
		relate_fields: make(map[string]*TRelateField),
		common_fields: make(map[string]map[string]IField),
		//common_fields :make(map[string]*TRelateField)
		methods:      make(map[string]reflect.Type),            // map[func][] 存储对应的Model 类型 string:函数所在的Models
		object_val:   make(map[reflect.Type]*TModel),           // map[Model] 备份对象
		object_types: make(map[string]map[string]reflect.Type), // map[Modul][Model] 存储Models的Type
		object_table: make(map[reflect.Type]*core.Table),

		//id_caches:  cache.NewMemoryCache(),
		//sql_caches: cache.NewMemoryCache(),
	}
	return
}

// 根据Model和Action 执行方法
// Action 必须是XxxXxxx格式
func (self *TOsv) GetMethod(model, name string) (res_md *TMethod) {
	lModel := self._getModelByMethod(model, name)

	//web.Debug("CallModelHandler", utils.TitleCasedName(name))
	//web.Debug("CallModelHandler", lM.IsNil())
	//web.Debug("CallModelHandler", lM == reflect.Zero(lM.Type()))
	logger.Dbg("GetMethod", lModel)
	if lModel.IsValid() { //|| !lM.IsNil()
		// 转换method
		// #必须使用Type才能获取到方法原型已经参数
		method, ok := lModel.Type().MethodByName(utils.TitleCasedName(name))
		if ok && method.Func.IsValid() {
			logger.Dbg("GetMethod", name, lModel.MethodByName(utils.TitleCasedName(name)), method.Func, method)

			res_md = NewMethod(name, method.Func)
			return
		}
	}
	return
}

func (self *TOsv) HasModel(name string) (res bool) {
	_, res = self.models[name]
	return
}

//TODO  TEST 测试是否正确使用路劲作为Modul
func (self *TOsv) GetModel(model string, module ...string) (IModel, error) {
	lModule := "" // "web" // 默认取Web模块注册的Models
	//logger.Dbg("getmodel", model, lModule, module)
	if len(module) > 0 && utils.Trim(module[0]) != "" {
		lModule = utils.Trim(module[0])
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
			logger.Dbg("getmodel", utils.CurPath(), lModule, lFilePath, utils.AppDir(), lDirLst)
			//if len(lDirLst) > 1 { // && lDirLst[0] == AppModuleDir
			//	lModule = lDirLst[1]
			//}
		*/
	}

	ml, err := self.GetModelByModule(lModule, model)
	if err != nil {
		return nil, err
	}

	if m, ok := ml.(IModel); ok {
		return m, nil
	}

	return nil, errors.New("Model is a interface of IModel")
}

// @ name
// @ Session
// @ Registry
func (self *TOsv) NewModel(name string) (mdl *TModel) {
	mdl = &TModel{
		_name: name,
		//_table:  strings.Replace(name, ".", "_", -1),
		_fields: make(map[string]IField),
	}

	//mdl._sequence = mdl._table + "_id_seq"

	return
}

//TODO　优化更简洁
// 每次GetModel都会激活初始化对象
func (self *TOsv) _initObject(val reflect.Value, atype reflect.Type, obj *TObj, model string) {
	if m, ok := val.Interface().(IModel); ok {
		// <以下代码严格遵守执行顺序>
		lModel := NewModel(model, val, atype) //self.newModel(sess, model)
		lModel.osv = self
		lModel.orm = self.orm
		//logger.Dbg("_initObject", len(obj.object_table), obj.object_table[atype])
		lModel.table = obj.object_table[atype]

		// lModel._fields = obj.fields
		lModel._fields_lock.Lock()
		for key, val := range obj.fields {
			lModel._fields[key] = val

			if key == "name" {
				lModel.SetRecordName("name")
			}
		}
		lModel._fields_lock.Unlock()

		lModel._relations_lock.Lock()
		for key, val := range obj.relations {
			lModel._relations[key] = val
		}
		lModel._relations_lock.Unlock()

		lModel._relate_fields_lock.Lock()
		for key, val := range obj.relate_fields {
			lModel._relate_fields[key] = val
		}
		lModel._relate_fields_lock.Unlock()

		lModel._common_fields_lock.Lock()
		for key, val := range obj.common_fields {
			t := make(map[string]IField)
			for n, f := range val {
				t[n] = f
			}
			lModel._common_fields[key] = t
		}
		lModel._common_fields_lock.Unlock()

		//		lModel.id_caches = obj.id_caches
		//		lModel.sql_caches = obj.sql_caches
		lModel.relations_reload()

		//lModel._cls_type = atype
		m.setBaseModel(lModel)
		//m.SetName(name)
		//m.SetRegistry(self)
		//web.Warn("使用接口对TModel进行赋值", m, lVal)

		//return m
	}
}

// #module 可以为空取默认
func (self *TOsv) _getModelByModule(region, model string) (val reflect.Value) {
	var (
		has         bool
		obj         *TObj
		module_name string
		module_map  map[string]reflect.Type
		model_type  reflect.Type
	)

	//获取Model的Object对象
	if obj, has = self.models[model]; has {
		logger.Dbg("_getModelByModule1", obj, len(self.models), region, model)

		// 非常重要 检查并返回唯一一个，或指定module_name 循环最后获得的值
		for module_name, module_map = range obj.object_types {
			if module_name == region {
				break
			}
		}
		logger.Dbg("_getModelByModule2", module_name, module_map)

		if model_type, has = module_map[model]; has {
			// 创建对象
			val = reflect.New(model_type)
			logger.Dbg("_getModelByModule3", val, model_type)
			//web.Warn("使用接口对TModel进行赋值", module, model, val)
			self._initObject(val, model_type, obj, model)
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
func (self *TOsv) _getModelByMethod(model string, method string) (val reflect.Value) {
	var (
		has   bool
		obj   *TObj
		lType reflect.Type
	)

	if obj, has = self.models[model]; has {
		//web.Debug("_getModelByMethod", model, method, utils.TitleCasedName(method), obj.methods)
		if lType, has = obj.methods[utils.TitleCasedName(method)]; has {
			val = reflect.New(lType)
			self._initObject(val, lType, obj, model)
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

	lM := self._getModelByModule(region, model)
	if lM.IsValid() && !lM.IsNil() {
		if m, ok := lM.Interface().(IModel); ok {
			return m, nil
		}

		logger.Panicf(`Model %s@%s is not a standard orm.IModel type,
		please check the name of Fields and Methods,make sure they are correct and not same each other`, model, region)
	}

	return nil, fmt.Errorf("Model %s@%s is not a standard model type of this system", model, region)
}

// NOTUSE
func (self *TOsv) ContainsModel(m string) (has bool) {
	_, has = self.models[m]
	return
}

// 废弃 根据Model和Action 执行方法
// Action 必须是XxxXxxx格式
func (self *TOsv) CallModelHandler(handler *server.TWebHandler, model, method string) bool {
	lM := self._getModelByMethod(model, method)

	if lM.IsValid() { //|| !lM.IsNil()
		// 转换method
		lMothod := lM.MethodByName(utils.TitleCasedName(method))

		if lMothod.IsValid() {
			//lMothod = reflect.ValueOf(lMothod.Interface())
			//TODO: 验证是否Handler func(handler *web.THandler)
			//TODO: 返回失败

			// call
			lMothod.Call([]reflect.Value{reflect.ValueOf(handler)})
			return true
		}
	}
	return false
}
