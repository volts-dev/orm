package orm

import (
	"fmt"
	"testing"
	//	"github.com/volts-dev/utils"
)

var (
	domains = []string{
		`(inherit_id=1 and model='333') or (mode='extension' and active=true) and ("id" in ("a","b"))`,
		`['aa','bb','cc']`,
		`[('aa','bb','cc')]`,
		`[('model', '=','%s'),('type', '=', '%s'), ('mode', '=', 'primary')]`,
		`['&', ('active', '=', True), ('value', '!=', 'foo')]`,
		`['|', ('active', '=', True), ('state', 'in', ['open', 'draft'])`,
		`['&', ('active', '=', True), '|', '!', ('state', '=', 'closed'), ('state', '=', 'draft')]`,
		" ['|', '|', ('state', '=', 'open'), ('state', '=', 'closed'), ('state', '=', 'draft')]",
		"['!', '&', '!', ('id', 'in', [42, 666]), ('active', '=', False)]",
		" ['!', ['=', 'domain_id.name', ['&', '...', '...']]]",
		"[('domain_id.domain_type_id.code', '=', 'incoming'), ('location_id.usage', '!=', 'internal'), ('location_dest_id.usage', '=', 'internal')]",
		`["|","|",["mode","ilike","tens"],["active","=","true"]]`,
		`["|",["name","ilike","m"],["domain_id","ilike","m"]]`,
	}
)

/*
func TestDomain2StringList(t *testing.T) {
	for idx, domain := range domains {
		fmt.Println()
		fmt.Printf("---------------------------- #%d ----------------------------", idx)
		fmt.Println()
		list := Query2StringList(domain)
		utils.PrintStringList(list)
	}
}
*/
func TestQuery2Domain(t *testing.T) {
	// #1
	fmt.Println(`Qry: "id=?"`)
	domain, err := String2Domain("id=?")
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	if !domain.Item(1).IsTermOperator() {
		t.Fatalf("the first item should be term operator")
	}
	t.Logf("done: Child:%v", domain.Count())

	// #2
	fmt.Println(`Qry: "id=? and id=?"`)
	domain, err = String2Domain("id=? and id=?")
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	if !domain.Item(0).IsDomainOperator() {
		t.Fatalf("the first item should be domain operator")
	}
	t.Logf("done: Child:%v", domain.Count())

	// #3
	fmt.Println(`Qry: ` + domains[0])
	domain, err = String2Domain(domains[0])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	if !domain.Item(0).IsDomainOperator() || !domain.Item(1).IsDomainOperator() {
		t.Fatalf("the items 1,2 should be domain operator")
	}

	if !domain.Item(2).Item(0).IsDomainOperator() {
		t.Fatalf("the item 2's first item should be a domain operator")
	}

	if !domain.Item(3).Item(0).IsDomainOperator() {
		t.Fatalf("the item 3's first item should be a domain operator")
	}

	if domain.Item(4).Item(1).String() != "in" {
		t.Fatalf("the item 4 should be a IN condition")
	}

	// #4
	fmt.Println(`Qry: ` + domains[1])
	domain, err = String2Domain(domains[1])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	if domain.String(0) != "aa" || domain.String(1) != "bb" || domain.String(2) != "cc" {
		t.Fatalf("3 items should be equal to condition")
	}

	// #4
	fmt.Println(`Qry: ` + domains[2])
	domain, err = String2Domain(domains[2])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)
	if domain.String(0) != "aa" || domain.String(1) != "bb" || domain.String(2) != "cc" {
		t.Fatalf("3 items should be equal to condition")
	}

	// #5
	fmt.Println(`Qry: ` + domains[3])
	domain, err = String2Domain(domains[3])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[4])
	domain, err = String2Domain(domains[4])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[5])
	domain, err = String2Domain(domains[5])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[6])
	domain, err = String2Domain(domains[6])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[7])
	domain, err = String2Domain(domains[7])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[8])
	domain, err = String2Domain(domains[8])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[9])
	domain, err = String2Domain(domains[9])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[10])
	domain, err = String2Domain(domains[10])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[11])
	domain, err = String2Domain(domains[11])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)

	// #5
	fmt.Println(`Qry: ` + domains[12])
	domain, err = String2Domain(domains[12])
	if err != nil {
		t.Fatal(err)
	}

	PrintDomain(domain)
}

func TestString2Domain(t *testing.T) {
	domain, err := String2Domain(domains[0])
	if err != nil {
		t.Fatal(err)
	}

	t.Log(domain.Count(), domain.String())

	PrintDomain(domain)

	t.Log(domain.String(0))

}

func TestDomain2String(t *testing.T) {
	fmt.Printf("---------------------------- #基本 ----------------------------")
	fmt.Println()
	list := NewDomainNode()
	list.Push("aa")
	list.Push("bb")
	list.PushNode(NewDomainNode("1", "2", "3"))
	fmt.Printf("Count:%d Domain:%s", list.Count(), Domain2String(list))
	fmt.Println()

	fmt.Printf("---------------------------- #简单多条件 ----------------------------")
	fmt.Println()
	list.Clear()
	list.PushNode(NewDomainNode("name", "=", "domain"))
	list.PushNode(NewDomainNode("age", "=", "18"))
	fmt.Printf("Count:%d Domain:%s", list.Count(), Domain2String(list))
	fmt.Println()
	PrintDomain(list)
	fmt.Println()

	fmt.Printf("---------------------------- #复杂嵌套 ----------------------------")
	fmt.Println()
	sub1 := NewDomainNode()
	sub1.Push("|")
	sub1.PushNode(NewDomainNode("name", "=", "domain"))
	sub1.PushNode(NewDomainNode("age", "=", "18"))

	sub2 := NewDomainNode()
	sub2.Push("|")
	sub2.PushNode(NewDomainNode("name", "=", "domain"))
	sub2.PushNode(NewDomainNode("age", "=", "[1,2,3]"))

	list.Clear()
	list.Push("|")
	list.Push(sub1)
	list.Push(sub2)

	fmt.Printf("Count:%d Domain:%s", list.Count(), Domain2String(list))
	fmt.Println()
	PrintDomain(list)
	fmt.Println()
}
