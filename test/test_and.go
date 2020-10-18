package test

import (
	"fmt"
	"testing"

	"github.com/volts-dev/orm"
)

func TestAnd(title string, t *testing.T) {
	PrintSubject(title, "And()")
	test_and(test_orm, t)
}

func test_and(o *orm.TOrm, t *testing.T) {
	model, err := o.GetModel("user_model")
	if err != nil {
		t.Fatal(err)
	}
	/*
		// 测试Select 默认所有
		ds, err := model.Records().Where("id=?", 1).Read()
		if err != nil {
			t.Fatal(err)
		}

		// 测试Select 默认所有
		ds, err = model.Records().Where("id=? and title=?", 1, "admin").Read()
		if err != nil {
			t.Fatal(err)
		}

		ds, err = model.Records().Where("id=? and title=? or help=?", 1, "admin", "您好!").Read()
		if err != nil {
			t.Fatal(err)
		}

		// 测试Select 所有
		ds, err = model.Records().Where("id=?", 2).And("name=?", "test").Read()
		if err != nil {
			t.Fatal(err)
		}

		// 测试Select 所有
		ds, err = model.Records().Where("id=?", 2).And("name=?", "test").Or("help=?", "您好!").Read()
		if err != nil {
			t.Fatal(err)
		}
	*/
	// 测试Select 所有
	fmt.Println("check domain combie domain")
	domain := `[&,('help','=','您好!'),('title', 'in', ['中国','aa','bb']]`
	ds, err := model.Records().Where("id=?", 1).Domain(domain).Read()
	if err != nil {
		t.Fatal(err)
	}
	if ds.IsEmpty() {
		t.Fatalf("the action Read() return %d!", ds.Count())
	}
}
