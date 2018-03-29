package orm

import (
	"fmt"
	"testing"
	"vectors/utils"
)

func TestDomain(t *testing.T) {
	var domain string
	domain = `[('act_window_id', 'in', [1])]`
	fmt.Println(domain)
	ls := Query2StringList(domain)
	utils.PrintStringList(ls)

	ls = normalize_domain(ls)
	utils.PrintStringList(ls)

	ls = distribute_not(ls)
	utils.PrintStringList(ls)
}
