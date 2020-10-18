package domain

import (
	"fmt"
	"testing"
)

func TestDomain2String(t *testing.T) {
	fmt.Printf("---------------------------- #基本 ----------------------------")
	fmt.Println()
	list := NewDomainNode()
	list.Push("aa")
	list.Push("bb")
	list.Push(NewDomainNode("1", "2", "3"))
	fmt.Printf("Count:%d Domain:%s", list.Count(), Domain2String(list))
	fmt.Println()

	fmt.Printf("---------------------------- #简单多条件 ----------------------------")
	fmt.Println()
	list.Clear()
	list.Push(NewDomainNode("name", "=", "domain"))
	list.Push(NewDomainNode("age", "=", "18"))
	fmt.Printf("Count:%d Domain:%s", list.Count(), Domain2String(list))
	fmt.Println()
	PrintDomain(list)
	fmt.Println()

	fmt.Printf("---------------------------- #复杂嵌套 ----------------------------")
	fmt.Println()
	sub1 := NewDomainNode()
	sub1.Push("|")
	sub1.Push(NewDomainNode("name", "=", "domain"))
	sub1.Push(NewDomainNode("age", "=", "18"))

	sub2 := NewDomainNode()
	sub2.Push("|")
	sub2.Push(NewDomainNode("name", "=", "domain"))
	sub2.Push(NewDomainNode("age", "=", "[1,2,3]"))

	list.Clear()
	list.Push("|")
	list.Push(sub1)
	list.Push(sub2)

	fmt.Printf("Count:%d Domain:%s", list.Count(), Domain2String(list))
	fmt.Println()
	PrintDomain(list)
	fmt.Println()
}
