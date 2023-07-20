package domain

import (
	"fmt"
	"testing"

	"github.com/volts-dev/dataset"
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

	sub3 := NewDomainNode()
	keys := NewDomainNode("1")
	sub3.IN("id", keys)
	PrintDomain(sub3)
	sub4 := NewDomainNode()
	for i := 0; i < 5; i++ {
		sub4.AND(NewDomainNode("f", "=", i))
	}
	PrintDomain(sub4)
	fmt.Println(Domain2String(sub4))

}

func TestIsNul(t *testing.T) {
	node, err := String2Domain("actice = true", nil)
	if err != nil {
		t.Fatal(err)
	}

	node2, err := String2Domain("domain IS not NULL", nil)
	if err != nil {
		t.Fatal(err)
	}

	node.AND(node2)

	result_str := Domain2String(node)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("New: %s", result_str)
}

func TestVar(t *testing.T) {
	ctx := dataset.NewDataSet(dataset.WithData(map[string]any{
		"preperty": []int64{4234234, 2342342},
	}))

	node, err := String2Domain(`['|', ('active', '=', True), ('state', 'in', preperty)`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	result_str := Domain2String(node)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("New: %s", result_str)
}
