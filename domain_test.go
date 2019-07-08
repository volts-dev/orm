package orm

import (
	"fmt"
	"testing"
	//	"github.com/volts-dev/utils"
)

var (
	domains = []string{
		`(inherit_id=1 and model='333') or (mode='extension' and active=true) and ("fdsf" in ("a","b"))`,
		"[('aa','bb','cc')]",
		"[('model', '=','%s'),('type', '=', '%s'), ('mode', '=', 'primary')]",
		"['aa','bb','cc']",
		"['&', ('active', '=', True), ('value', '!=', 'foo')]",
		"['|', ('active', '=', True), ('state', 'in', ['open', 'draft'])",
		"['&', ('active', '=', True), '|', '!', ('state', '=', 'closed'), ('state', '=', 'draft')]",
		" ['|', '|', ('state', '=', 'open'), ('state', '=', 'closed'), ('state', '=', 'draft')]",
		"['!', '&', '!', ('id', 'in', [42, 666]), ('active', '=', False)]",
		" ['!', ['=', 'domain_id.name', ['&', '...', '...']]]",
		"[('domain_id.domain_type_id.code', '=', 'incoming'), ('location_id.usage', '!=', 'internal'), ('location_dest_id.usage', '=', 'internal')]",
		`[('model', '=','%s'),('type', '=', '%s'), ('mode', '=', 'primary')]`,
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
	domain, err := Query2Domain("id=? and id=?")
	if err != nil {
		t.Fatal(err)
	}

	t.Log(domain.Count(), domain.String())

	PrintDomain(domain)

	t.Log(domain.String(0))

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
