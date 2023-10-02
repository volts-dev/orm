package test

import (
	"fmt"
	"time"

	"github.com/volts-dev/orm"
)

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
		Users        []interface{} `field:"one2many(user_model,many_to_one) title('Test Title') domain([('active','=',True)])"`
	}

	UserModel struct {
		orm.TModel   `table:""`
		PartnerModel `field:"relate(PartnerId)"`
		PartnerId    int64     `field:"one2one(partner_model)"`
		CompanyId    int64     `field:"many2one(company_model)"` //-- Company
		Id           int64     `field:"pk autoincr title('ID') index"`
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
		Int       int           `field:"int() default(1)"`    // --
		Bool      bool          `field:"bool default(true)"`  // --
		Text      string        `field:"text"`                //
		Float     float32       `field:"float"`               //
		Bin       []byte        `field:"binary() attachment"` //
		Selection string        `field:"selection('{\"person\":\"Individual\",\"company\":\"Company\"}')"`
		Func      string        `field:"selection(TestSelection)"`
		Companies []interface{} `field:"many2many(company_model,company_id,user_id)"`
	}
)

func (UserModel) TestSelection() [][]string {
	fmt.Println("Arg is T")
	return [][]string{
		{"AA", "aa"},
		{"BB", "bb"},
	}
}

func (self UserModel) SetLang() [][]string {
	result := make([][]string, 0)
	result = append(result, []string{"name", "Vectors"})
	result = append(result, []string{"Mode", self.String()})
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
		Name:  "Admin",
		Title: "Admin",
		//Help:      "",
		//Bool: true,
		//Text:      "",
		Float:     0.00001,
		Bin:       []byte("aa"),
		Selection: "",
		Func:      "",
		//OneToMany
		//ManyToOne
		//ManyToMany
	}
)
