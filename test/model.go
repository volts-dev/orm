package test

import (
	"fmt"
	"time"
	"vectors/orm"
)

type (
	Model1 struct {
		orm.TModel `table:"name('sys.action')"`
		Id         int64     `field:"pk autoincr title('ID') index"`
		CreateId   int64     `field:"int"`
		CreateTime time.Time `field:"datetime() created"`
		WriteId    int64     `field:"int"`
		WriteTime  time.Time `field:"datetime() updated"`
		Type       string    `field:"selection('{\"person\":\"Individual\",\"company\":\"Company\"}')"`
		Lang       string    `field:"selection(GetLang)"`
	}

	Model2 struct {
		orm.TModel `table:"name('res.company')"`
		ResPartner `field:"relate(PartnerId)"`
		Id         int64     `field:"pk autoincr"`
		CreateId   int64     `field:"int"`
		CreateTime time.Time `field:"datetime created"` //-- Created on
		WriteId    int64     `field:"int"`
		WriteTime  time.Time `field:"datetime updated"`

		Name       string `field:"char() unique index required"`
		PartnerId  int64  `field:"int"` // -- Related Company
		CurrencyId int64  `field:"int"`

		Logo      []byte //-- Logo Web
		Font      int64  `field:"int"`       //-- Font
		AccountNo string `field:"varchar()"` // -- Account No.
		ParentId  int64  `field:"int"`       //-- Parent Company
		Email     string `field:"varchar()"` // -- Email

		CustomFooter      bool   `field:"bool"`        // -- Custom Footer
		Phone             string `field:"varchar(64)"` // -- Phone
		ReportFooter      string `field:"text"`        // -- Report Footer
		ReportHeader      string `field:"text"`        // -- RML Header
		ReportPaperFormat string `field:"varchar()"`   // -- Paper Format
		ReportHeader1     string `field:"text"`        // -- Company Tagline
		ReportHeader2     string `field:"text"`        // -- RML Internal Header
		ReportHeader3     string `field:"text"`        // -- RML Internal Header for Landscape Reports
		CompanyRegistry   string `field:"varchar(64)"` // -- Company Registry
	}

	//用户表
	ResCompanyUserRel struct {
		orm.TModel
		UserId    int64 `field:"int required"` // -- user
		CompanyId int64 `field:"int required"` // -- Related Company
	}

	//用户表
	ResUser struct {
		orm.TModel
		ResPartner `field:"relate(PartnerId)"`
		Id         int64     `field:"pk autoincr title('ID') index"`
		CreateId   int64     `field:"int"`
		CreateTime time.Time `field:"datetime() created"`
		WriteId    int64     `field:"int"`
		WriteTime  time.Time `field:"datetime() updated"`
		CompanyId  int64     `field:"many2one(res.company) required(true) title('Current Company') help('The company this user is currently working for.')"` //-- Company
		CompanyIds []int64   `field:"many2many(res.company,res.company.user.rel,user_id,company_id) title('Allowed Companies')"`
		PartnerId  int64     `field:"many2one(res.partner) required(true) title('Related Partner') help('Partner-related data of the user')"` // -- Related Company
		//	GroupId    []*ResGroup `field:"- many2many(res.group,res.group.user.rel,user_id,group_id) title('Groups')"`
		ActionId    int64  `field:"many2one(sys.action)  title('Home Action') help('If specified, this action will be opened at log on for this user, in addition to the standard menu.')"` //-- Company
		Passport    string `field:"title('Passport Name') required(true) help('Used to log into the system')"`                                                                              // --  Account
		Password    string `field:"title('Password') required(true) default(0) help('Keep empty if you don''t want the user to be able to connect on the system.')"`
		NewPassword string `field:"title('Set Password') help('Specify a value only when creating a user or if you''re
		                 changing the user''s password, otherwise leave empty. After
		                 a change of password, the user has to login again.')"`
		LoginTime string `field:"datetime() updated title('Latest Connection')"`
		//		Role          []*Role   `orm:"rel(m2m)"`
		// 测试 default
		Signature     string `field:"text title('Signature') default('none')"`
		PasswordCrypt string // -- Encrypted Password
		Share         bool   `field:"title('External user with limited access, created only for the purpose of sharing data.')` // -- Share User
		Active        bool   `field:"default(true) title('Active')"`

		//Email    string `field:"varchar(32)"`
		//Remark   string `field:"varchar(200)"`
		//Status int `field:"int(2)"`
		//Customer bool

	}

	ResPartner struct {
		orm.TModel `table:"name('')"`
		Id         int64     `field:"pk autoincr"`
		CreateId   int64     `field:"int"`
		CreateTime time.Time `field:"datetime() created"` //-- Created on
		WriteId    int64     `field:"int"`
		WriteTime  time.Time `field:"datetime() updated"`

		CompanyId   int64  `field:"many2one(res.company) title('Company') required(true)"` //-- Company
		CompanyType string `field:"selection('{\"person\":\"Individual\",\"company\":\"Company\"}') title('Company Type') help('Technical field, used only to display a boolean using a radio
                 button. As for Odoo v9 RadioButton cannot be used on boolean
                 fields, this one serves as interface. Due to the old API
                 limitations with interface compute field, we implement it
                 by hand instead of a true compute field. When migrating to
                 the new API the code should be simplified.')"`
		BankId []*interface{} `field:"one2many(res.partner.bank,partner_id)"`

		Name        string         `field:"title('Name') required index"`
		DisplayName string         `field:"compute() char() title('Name')"`                 // -- Name
		Title       string         `field:"many2one(res.partner.title) title('Title')"`     // -- Title
		Comment     string         `field:"text()"`                                         // -- Notes
		Ean13       string         `field:"char(13) title('Barcode')"`                      //  -- EAN13
		Color       int64          `field:"title('Color Index')"`                           // -- Color Index
		Street      string         `field:"char() title('Street')"`                         // -- Street
		Street2     string         `field:"char() title('Street2')"`                        // -- Street2
		City        string         `field:"char() title('City')"`                           // -- City
		Zip         string         `field:"char() title('Zip')"`                            // -- Zip
		Function    string         `field:"char() title('Job Position')"`                   // -- Job Position
		CountryId   int64          `field:"many2one(res.country) title('Country')"`         // -- Country
		ParentId    int64          `field:"many2one(res.partner) title('Related Company')"` // -- Related Company
		ChildIds    []*interface{} `field:"one2many(res.partner,parent_id) title('Contacts') domain([('active','=',True)])"`
		CategoryId  []*interface{} `field:"many2many(res.partner.category,res.partner.category.rel,partner_id,category_id) title('Tags')"`
		Ref         string         `field:"char() title('Contact Referenc')"`                                                                                                                                                                                                                                                                                                                                    // -- Contact Reference
		Email       string         `field:"char() title('Email')"`                                                                                                                                                                                                                                                                                                                                               //-- Email
		IsCompany   bool           `field:"char() title('Is a Company') help('Check if the contact is a company, otherwise it is a person')"`                                                                                                                                                                                                                                                                    // -- Is a Company
		Website     string         `field:"char() title('Website') help('Website of Partner or Company')"`                                                                                                                                                                                                                                                                                                       // -- Website
		Customer    bool           `field:"char() title('Customer') help('Check this box if this contact is a customer.')"`                                                                                                                                                                                                                                                                                      //-- Customer
		Supplier    bool           `field:"char() title('Supplier') help('Check this box if this contact is a vendor. If it''s not checked, purchase people will not see it when encoding a purchase order.')"`                                                                                                                                                                                                  // -- Supplier
		Employee    string         `field:"char() title('Employee') help('Check this box if this contact is an Employee.')"`                                                                                                                                                                                                                                                                                     //-- Employee
		CreditLimit float32        `field:"float title('Credit Limit')"`                                                                                                                                                                                                                                                                                                                                         // -- Credit Limit
		Active      bool           `field:"bool title('Active')"`                                                                                                                                                                                                                                                                                                                                                // -- Active
		Tz          string         `field:"selection(GetTz) title('Timezone') help('The partner''s timezone, used to output proper date and time values inside printed reports.
								                 It is important to set a value for this field. You should use the same timezone
								                 that is otherwise used to pick and render date and time values: your computer''s timezone.')"` // -- Timezone
		Lang                   string    `field:"selection(GetLang) title('Language') help('If the selected language is loaded in the system, all documents related to this contact will be printed in this language. If not, it will be English.')"`                                                                           //-- Language
		Phone                  string    `field:"char() title('Phone')"`                                                                                                                                                                                                                                                        //-- Phone
		Mobile                 string    `field:"char() title('Mobile')"`                                                                                                                                                                                                                                                       // -- Mobile
		Fax                    string    `field:"char() title('Fax')"`                                                                                                                                                                                                                                                          // -- Fax
		Type                   string    `field:"selection('{\"contact\":\"Contact\",\"invoice\":\"Invoice address\",\"delivery\":\"Shipping address\",\"other\":\"Other address\"}') title('Address Type') help('Used to select automatically the right address according to the context in sales and purchases documents.')"` // -- Address Type
		UseParentAddress       bool      `field:"bool title('Use Company Address') help('Select this if you want to set company''s address information  for this contact')"`                                                                                                                                                    // -- Use Company Address
		UserId                 int64     `field:"many2one(res.user) title('Salesperson') help('The internal user that is in charge of communicating with this contact if any.')"`                                                                                                                                               // -- Salesperson
		Birthdate              string    `field:"char() title('Birthdate')"`                                                                                                                                                                                                                                                    // -- Birthdate
		Vat                    string    `field:"char() title('TIN') help('Tax Identification Number. Fill it if the company is subjected to taxes. Used by the some of the legal statements.')"`                                                                                                                               // -- TIN
		StateId                int64     `field:"many2one(res.country.state) title('State')"`                                                                                                                                                                                                                                   // -- State
		CommercialPartnerId    int64     `field:"int title('Commercial Entity')"`                                                                                                                                                                                                                                               // -- Commercial Entity
		NotifyEmail            string    `field:"char() title('Receive Inbox Notifications by Email')"`                                                                                                                                                                                                                         // -- Receive Inbox Notifications by Email
		MessageLastPost        time.Time `field:"datetime() title('Last Message Date') updated "`                                                                                                                                                                                                                               // -- Last Message Date
		OptOut                 bool      `field:"bool title('Opt-Out')"`                                                                                                                                                                                                                                                        //  -- Opt-Out
		SectionId              int64     `field:"int title('Sales Team')"`                                                                                                                                                                                                                                                      // -- Sales Team
		SignupType             string    `field:"char() title('Signup Token Type')"`                                                                                                                                                                                                                                            // -- Signup Token Type
		SignupExpiration       time.Time `field:"datetime()  title('Signup Expiration') updated"`                                                                                                                                                                                                                               // -- Signup Expiration
		SignupToken            string    `field:"char() title('Token')"`                                                                                                                                                                                                                                                        // -- Signup Token
		LastReconciliationDate time.Time `field:"datetime()  title('Latest Full Reconciliation Date') updated"`                                                                                                                                                                                                                 // -- Latest Full Reconciliation Date
		VatSubjected           bool      `field:"bool title('VAT Legal Statement')"`                                                                                                                                                                                                                                            // -- VAT Legal Statement
		DebitLimit             float32   `field:"float title('Payable Limit')"`                                                                                                                                                                                                                                                 //-- Payable Limit
		Image                  []byte    `field:"binary() title('Image') attachment help('This field holds the image used as avatar for this contact, limited to 1024x1024px')"`                                                                                                                                                // -- Image
		//image_medium bytea, -- Medium-sized image
		//image_small bytea, -- Small-sized image
		// date date, -- Date

	}
)

func (self Model1) etLang() [][]string {
	result := make([][]string, 0)
	result = append(result, []string{"name", "Vectors"})
	result = append(result, []string{"Mode", self.ModelName()})
	return result
}

func (ResUser) CallTest(str string) {
	fmt.Println("Arg is ", str)
}
