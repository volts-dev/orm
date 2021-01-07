# ORM
The Volts ORM library for Golang, aims to be developer friendly.

# Overview
* Domain Parser (String type filter)
* Dataset (Full data type covert interface)
* Developer Friendly

# 1.定义数据表模型
`
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
`

# 2.同步映射模型到ORM
这里不需要当心同步模型的顺序,"test"只是区分这些模型在不同包或者文件夹的标志,可以是任何字符串
`
	Orm, err = orm.NewOrm(DataSource)
	if err != nil {
		// todo ...
	}
	
	Orm.SyncModel("test",
		new(PartnerModel),
		new(CompanyModel),
		new(UserModel),
	)
`

# 3.获取表模型 
这里几乎可以在任何包里调用并获取模型,而不必担心golang包的交叉引用限制.参数"test"可以省略
`
	model, err := Orm.GetModel("user_model","test")
	if err != nil {
		// todo ...
	}
`

#自定义字段类型
#自定义返回TDataset 数据集

一个model表示一系列的函数接口的封装
一个model对应一些基本的功能，一般地，对应与一张表的操作。
传入参数，传出字典（而不是包装后的类）。即全部操作直接对应model里面的方法。
model最后统一在manager里面实例化，需要被调用的model绑定到manager里面。
一些高层的功能另外用model来揉合（调用胶水层）