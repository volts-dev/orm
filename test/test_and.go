package test

import (
	"fmt"
	"testing"

	"github.com/volts-dev/orm/domain"

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
	node := domain.NewDomainNode()
	for i := 0; i < 5; i++ {
		node.AND(domain.NewDomainNode("name", "=", i))
	}
	domain.PrintDomain(node)
	fmt.Println(domain.Domain2String(node))
	ds, err := model.Records().Domain(node).Read()
	if err != nil {
		t.Fatal(err)
	}
	if ds.IsEmpty() {
		t.Fatalf("the action Read() return %d!", ds.Count())
	}
}
