package test

import (
	"fmt"
	"time"
	"volts-dev/orm"
)

type (
	// PartnerModel save all the records about a company/person/group
	PartnerModel struct {
		orm.TModel `table:"name('partner.model')"`
		Id         int64  `field:"pk autoincr title('ID') index"`
		Name       string `field:"char() unique index required"`
		Homepage   string `field:"char() size(25)"`
	}

	// CompanyModel save all the records about a shop/subcompany,
	// main details will relate to PartnerModel and mapping all field of PartnerModel
	CompanyModel struct {
		orm.TModel   `table:"name('company.model')"`
		PartnerModel `field:"relate(PartnerId)"`
		PartnerId    int64
		Id           int64  `field:"pk autoincr title('ID') index"`
		Name         string `field:"char() unique index required"`
	}

	UserModel struct {
		orm.TModel `table:"name('user.model')"`
		Id         int64     `field:"pk autoincr title('ID') index"`
		CreateTime time.Time `field:"datetime() created"`
		WriteTime  time.Time `field:"datetime() updated"`

		Name  string `field:"char() size(15) unique index required"`
		Title string `field:"title('Test Title.')"`
		Help  string `field:"help('Technical field, used only to display a help string using multi-rows. 
				 test help 1\"
                 test help 2''
                 test help 3''''
                 test help 4.')"`
		Bool       bool           `field:"bool default(true)"`  // --
		Text       string         `field:"text"`                //
		Float      float32        `field:"float"`               //
		Bin        []byte         `field:"binary() attachment"` //
		Selection  string         `field:"selection('{\"person\":\"Individual\",\"company\":\"Company\"}')"`
		Func       string         `field:"selection(TestSelection)"`
		OneToMany  []*interface{} `field:"one2many(company.model,parent_id) title('Test Title') domain([('active','=',True)])"`
		ManyToOne  int64          `field:"many2one(company.model)"` //-- Company
		ManyToMany []int64        `field:"many2many(company.model,company.user.ref,company_id,user_id)"`
	}

	// A relate reference table between CompanyModel and UserModel
	CompanyUserRef struct {
		orm.TModel `table:"name('company.user.ref')"`
		CompanyId  int64
		UserId     int64
	}
)

func (UserModel) TestSelection(str string) {
	fmt.Println("Arg is ", str)
}

func (self UserModel) etLang() [][]string {
	result := make([][]string, 0)
	result = append(result, []string{"name", "Vectors"})
	result = append(result, []string{"Mode", self.GetModelName()})
	return result
}

var (
	partner *PartnerModel = &PartnerModel{
		Name:     "Partner",
		Homepage: "votls.dev",
	}

	company *CompanyModel = &CompanyModel{
		Name: "Company",
	}

	user *UserModel = &UserModel{
		Name:      "Admin",
		Title:     "Admin",
		Help:      "",
		Bool:      true,
		Text:      "",
		Float:     0.00001,
		Bin:       []byte("aa"),
		Selection: "",
		Func:      "",
		//OneToMany
		//ManyToOne
		//ManyToMany
	}
)
