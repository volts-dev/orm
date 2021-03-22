package domain

import (
	"fmt"
	"testing"
)

var (
	domains = []string{
		`(inherit_id=1 and model='333') or (mode='extension' and active=true) and ("id" in ("a","b") and ("id" in [1])`,
		`['aa','bb','cc']`,
		`[('aa','bb','cc')]`,
		`[('model', '=','%s'),('type', '=', '%s'), ('mode', '=', 'primary')]`,
		`['&', ('active', '=', True), ('value', '!=', 'foo')]`,
		`['|', ('active', '=', True), ('state', 'in', ['open', 'draft'])`,
		`['&', ('active', '=', True), '|', '!', ('state', '=', 'closed'), ('state', '=', 'draft')]`,
		`['|', '|', ('state', '=', 'open'), ('state', '=', 'closed'), ('state', '=', 'draft')]`,
		`['!', '&', '!', ('id', 'in', [42, 666]), ('active', '=', False)]`,
		`['!', ['=', 'domain_id.name', ['&', '...', '...']]]`,
		`[('domain_id.domain_type_id.code', '=', 'incoming'), ('location_id.usage', '!=', 'internal'), ('location_dest_id.usage', '=', 'internal')]`,
		`["|","|",["mode","ilike","tens"],["active","=","true"]]`,
		`["|",["name","ilike","m"],["domain_id","ilike","m"]]`,
	}

	checker = map[string]string{
		domains[0]:  `[&,|,[&,(inherit_id,=,1),(model,=,333)],[&,(mode,=,extension),(active,=,true)],[&,(id,in,[a,b]),(id,in,[1])]]`,
		domains[1]:  `[aa,bb,cc]`,
		domains[2]:  `[aa,bb,cc]`,
		domains[3]:  `[(model,=,%s),(type,=,%s),(mode,=,primary)]`,
		domains[4]:  `[&,(active,=,True),(value,!=,foo)]`,
		domains[5]:  `[|,(active,=,True),(state,in,[open,draft])]`,
		domains[6]:  `[&,(active,=,True),|,!,(state,=,closed),(state,=,draft)]`,
		domains[7]:  `[|,|,(state,=,open),(state,=,closed),(state,=,draft)]`,
		domains[8]:  `[!,&,!,(id,in,[42,666]),(active,=,False)]`,
		domains[9]:  `[!,[=,domain_id.name,[&,...,...]]]`,
		domains[10]: `[(domain_id.domain_type_id.code,=,incoming),(location_id.usage,!=,internal),(location_dest_id.usage,=,internal)]`,
		domains[11]: `[|,|,(mode,ilike,tens),(active,=,true)]`,
		domains[12]: `[|,(name,ilike,m),(domain_id,ilike,m)]`,
	}
)

func TestString2Domain(t *testing.T) {
	printToken = false // print token

	for idx, domain := range domains {

		node, err := String2Domain(domain)
		if err != nil {
			t.Fatal(err)
		}

		//PrintDomain(node)

		result_str := Domain2String(node)

		if result_str != checker[domain] {
			fmt.Println()
			fmt.Printf("---------------------------- #%d ----------------------------", idx)
			fmt.Println()
			t.Logf("Raw: %s", domain)
			t.Logf("New: %s", result_str)
			t.Logf(" %d result is not same!", idx)
		}
	}
}
