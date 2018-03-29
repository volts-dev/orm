package orm

import (
	"fmt"
	"testing"
	"vectors/utils"
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

func TestDomain2StringList(t *testing.T) {
	for idx, domain := range domains {
		fmt.Println()
		fmt.Printf("---------------------------- #%d ----------------------------", idx)
		fmt.Println()
		list := Query2StringList(domain)
		utils.PrintStringList(list)
	}
}

/*
func TestStringList2Domain(t *testing.T) {
	fmt.Printf("---------------------------- #基本 ----------------------------")
	fmt.Println()
	list := utils.NewStringList()
	list.PushString("aa")
	list.PushString("bb")
	list.Push(utils.NewStringList("1", "2", "3"))
	//	llist.Update()
	fmt.Printf("Count:%d Domain:%s", list.Count(), StringList2Domain(list))
	fmt.Println()

	fmt.Printf("---------------------------- #简单多条件 ----------------------------")
	fmt.Println()
	list.Clear()
	list.AddSubList("name", "=", "domain")
	list.AddSubList("age", "=", "18")
	fmt.Printf("Count:%d Domain:%s", list.Count(), StringList2Domain(list))
	fmt.Println()
	utils.PrintStringList(list)
	fmt.Println()

	fmt.Printf("---------------------------- #复杂嵌套 ----------------------------")
	fmt.Println()
	sub1 := utils.NewStringList()
	sub1.PushString("|")
	sub1.AddSubList("name", "=", "domain")
	sub1.AddSubList("age", "=", "18")

	sub2 := utils.NewStringList()
	sub2.PushString("|")
	sub2.AddSubList("name", "=", "domain")
	sub2.AddSubList("age", "=", "[1,2,3]")

	list.Clear()
	list.PushString("|")
	list.Push(sub1)
	list.Push(sub2)

	fmt.Printf("Count:%d Domain:%s", list.Count(), StringList2Domain(list))
	fmt.Println()
	utils.PrintStringList(list)
	fmt.Println()
}
*/
