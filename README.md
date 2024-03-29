Warning: This package is a work in progress. After some preliminary testing, I think I might change a lot of how this package is structured.
The Volts'ORM library for Golang, aims to be developer friendly.

#### Overview
* Domain Parser (String type filter)
* Dataset (Full data type covert interface)
* Developer Friendly

#### 可扩展定制范围
* 支持Model扩展 (扩展处理方法和接口等,让model可带有其他中间件等特殊功能)
* 支持Tag扩展 (id,int,many2many...)
* 支持数据库扩展 (目前只有postgresql)
* 支持字段格式转换扩展 (字段读写数据库时格式化)

#### 1.定义数据表模型
```
type (
	// PartnerModel save all the records about a company/person/group
	PartnerModel struct {
		orm.TModel `table:"name('partner_model')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"char() unique index required"`
		Homepage   string `field:"char() size(25)"`
	}

	// CompanyModel save all the records about a shop/subcompany,
	// main details will relate to PartnerModel and mapping all field of PartnerModel
	CompanyModel struct {
		orm.TModel   `table:"name('company_model')"`
		PartnerModel `field:"relate(PartnerId)"`
		PartnerId    int64         `field:"one2one(partner_model)"`
		Id           int64         `field:"pk autoincr title('ID') index"`
		Name         string        `field:"char() unique index required"`
		OneToMany    []interface{} `field:"one2many(user_model,many_to_one) title('Test Title') domain([('active','=',True)])"`
	}

	UserModel struct {
		orm.TModel   `table:""`
		PartnerModel `field:"relate(PartnerId)"`
		PartnerId    int64     `field:"one2one(partner_model)"`
		CompanyId    int64     `field:"many2one(company_model)"` //-- Company
		Id           int64     `field:"pk autoincr type(char) title('ID') index"`
		Uid          int64     `field:"Id() pk  title('ID') index"`
		CreateTime   time.Time `field:"datetime() created"`
		WriteTime    time.Time `field:"datetime() updated"`

		Name  string `field:"char() size(15) unique index required"`
		Title string `field:"title('Test Title.')"`
		Help  string `field:"help('Technical field, used only to display a help string using multi-rows. 
				 test help 1\"
                 test help 2''
                 test help 3''''
                 test help 4.')"`

		// all data types
		Int        int           `field:"int() default(1)"`    // --
		Bool       bool          `field:"bool default(true)"`  // --
		Text       string        `field:"text"`                //
		Float      float32       `field:"float"`               //
		Bin        []byte        `field:"binary() attachment"` //
		Selection  string        `field:"selection('{\"person\":\"Individual\",\"company\":\"Company\"}')"`
		Func       string        `field:"selection(TestSelection)"`
		ManyToMany []interface{} `field:"many2many(company_model,company_id,user_id)"`
	}
)
```

#### 2.同步映射模型到ORM
这里不需要当心同步模型的顺序,"test"只是区分这些模型在不同包或者文件夹的标志,可以是任何字符串
```
	Orm, err = orm.NewOrm(DataSource)
	if err != nil {
		// todo ...
	}
	
	Orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
	)
```

#### 3.获取表模型 
这里几乎可以在任何包里调用并获取模型,而不必担心golang包的交叉引用限制.参数"test"可以省略
```
	model, err := Orm.GetModel("user_model","test")
	if err != nil {
		// todo ...
	}
```
#### 4.数据查询修改
```
	dataset, err := model.Records().Read()
	if err != nil {
		// todo ...
	}

	// 获取数据量
	if dataset.Count() == 0 {
		self.Fatal("please add some record first!")
	}
	
	// 遍历数据集
	dataset.Frist()
	if !dataset.Eof(){
		name:=dataset.Record().FieldByName("name").AsString("123")
	   	fmt.Println(name)	
		
		dataset.Next()
	}
	
	// 获取当前字段值
	id:=dataset.FieldByName("id").AsString() // the value is string
	id:=dataset.FieldByName("id").AsInteger() // the value is int
	id:=dataset.FieldByName("id").AsInterface() // the value is interface{}
	
	// 修改数据
	dataset.FieldByName("id").AsString("123") // the value is string
	
	// 写回数据库
	model.Records().Write(dataset.Record().AsItfMap())
	
	更多请查询Test目录测试例子
	....

```
#### 扩展模型方法
数据表模型可以通过继承IModel(接口)/TModel(实现)达到为每个数据模型扩展方法的基模型
```
	IBaseModel interface{}{
		orm.IModel
		ReadFrist100()*TDataset,err
		...
	}
	
	TBaseModel struct{
		orm.TModel
		...
	}
	
	func(self *TBaseModel)ReadFrist100()*TDataset,err{
		// todo...
	}
	
	// 新接口获取模型
	GetModel(name string)IBaseModel{
		model,_:=orm.GetModel(modelName, module_name)
		
		return model.(IBaseModel)
	}
```
#### 数据表模型继承基模型
```
	UserModel struct{
		TBaseModel
		...
	}
	
	// 映射注册
	orm.SyncModel("",new(UserModel))
```

#### 获取模型
```
	user:=GetModel("user_model")
	ds,_:=user.ReadFrist100()
```
#自定义字段类型
#自定义返回TDataset 数据集

一个model表示一系列的函数接口的封装
一个model对应一些基本的功能，一般地，对应与一张表的操作。
传入参数，传出字典（而不是包装后的类）。即全部操作直接对应model里面的方法。
model最后统一在manager里面实例化，需要被调用的model绑定到manager里面。
一些高层的功能另外用model来揉合（调用胶水层）