package orm

import (
	"sort"
	"testing"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
)

/*
非存储、又没有搜索实现的字段进了 domain 时，那枚叶子要换成**恒真叶子**，不能凭空消失。

原来换成的是一个空节点：入栈时一个元素都不 push，叶子从树上没了，而它头上的前缀运算符
还按两个操作数记账——于是日志里成对出现（2026-10-02 真栈，documents.document 的
thumbnail_status、pro.attr.value 的 image）：

	Non-stored field thumbnail_status@documents.document cannot be searched, the leaf is DROPPED
	domain to sql: operator "&" lacks operands, the domain is malformed

后者不只是噪音：缺了一个操作数，**外层**运算符就绑错了对象。`A or (B and X)` 丢掉 X 之后，
`&` 吃掉的是 B 和 A，剩下的 `|` 落空——整条条件变成 `A and B`，筛出来的东西完全不对。

Odoo 的做法是 push TRUE_LEAF（这条筛选「什么都不筛」，日志照样点名），树的形状保持完整。
*/

func tagNames(t *testing.T, fx *x2mFixture, node *domain.TDomainNode) []string {
	t.Helper()
	ds, err := fx.orm.Model("x2m.tag").Domain(node).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read %s: %v", node.String(), err)
	}
	var out []string
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		out = append(out, rec.FieldByName("name").AsString())
		return nil
	})
	sort.Strings(out)
	return out
}

func TestExprDroppedLeaf_KeepsTreeShape(t *testing.T) {
	fx := setupX2m(t)

	// ['|', '&', name=red, computed_label=x, name=blue]  ==  red or blue（X 恒真）
	dom, err := domain.String2Domain(`['|', '&', ('name','=','tag_red'), ('computed_label','=','x'), ('name','=','tag_blue')]`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := tagNames(t, fx, dom); !sameStrings(got, []string{"tag_blue", "tag_red"}) {
		t.Fatalf("得到 %v，应为 [tag_blue tag_red]（空 = 外层 | 绑错了操作数，成了 red and blue）", got)
	}

	// ['&', name=red, computed_label=x]  ==  red
	dom2, err := domain.String2Domain(`['&', ('name','=','tag_red'), ('computed_label','=','x')]`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := tagNames(t, fx, dom2); !sameStrings(got, []string{"tag_red"}) {
		t.Fatalf("得到 %v，应为 [tag_red]", got)
	}
}
