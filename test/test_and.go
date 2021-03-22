package test

import (
	"fmt"

	"github.com/volts-dev/orm/domain"
)

func (self *Testchain) And() *Testchain {
	self.PrintSubject("And")
	model, err := self.Orm.GetModel("user_model")
	if err != nil {
		self.Fatal(err)
	}

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
		self.Fatal(err)
	}
	if ds.IsEmpty() {
		self.Fatalf("the action Read() return %d!", ds.Count())
	}

	return self
}
