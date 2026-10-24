package orm

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/volts-dev/orm/domain"
)

/*
TFieldSearchContext.Origin 的回归：点号路径里，对端字段的 search 钩子必须还认得出
**最外层**是哪个模型发起的查询。

为什么要钉住它：`('article_id.user_has_access','=',True)` 这类记录规则的对端那一段
在派生会话上查，对端模型由 orm.GetModel 现取、不挂上层框架的登录会话。钩子若只看
ctx.Model，就不知道"当前用户"是谁——要么报错，要么（更糟）当成无人而放行或全拒。
上层框架把登录会话挂在最外层模型上，Origin 就是把那一个带下去。
*/

type OrgDoc struct {
	TModel `table:"name('org_doc')"`
	Id     int64  `field:"pk autoincr title('ID')"`
	Code   string `field:"varchar() size(64)"`
}

// 钩子只做两件事：记下看见的 Origin，再把自己翻译成 code 上的一个叶子。
var (
	orgSeenMu sync.Mutex
	orgSeen   []string
)

func (self OrgDoc) OnBuildFields() error {
	if err := self.TModel.OnBuildFields(); err != nil {
		return err
	}
	self.Builder().VarcharField("watch").Store(false).Readonly(true).
		Searcher(func(ctx *TFieldSearchContext) (*domain.TDomainNode, error) {
			name := "<nil>"
			if ctx.Origin != nil {
				name = ctx.Origin.String()
			}
			orgSeenMu.Lock()
			orgSeen = append(orgSeen, name)
			orgSeenMu.Unlock()
			return domain.New("code", ctx.Operator, ctx.Value), nil
		})
	return nil
}

type OrgLine struct {
	TModel `table:"name('org_line')"`
	Id     int64 `field:"pk autoincr title('ID')"`
	DocId  int64 `field:"many2one(org.doc)"`
}

func TestSearcher_OriginSurvivesDottedPath(t *testing.T) {
	ds := testDataSource(t, filepath.Join(t.TempDir(), "org.db"))
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(OrgDoc), new(OrgLine)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	a, err := o.Model("org.doc").Create(map[string]any{"code": "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := o.Model("org.doc").Create(map[string]any{"code": "B"})
	if err != nil {
		t.Fatal(err)
	}
	lineA, err := o.Model("org.line").Create(map[string]any{"doc_id": firstId(t, a)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Model("org.line").Create(map[string]any{"doc_id": firstId(t, b)}); err != nil {
		t.Fatal(err)
	}

	orgSeenMu.Lock()
	orgSeen = nil
	orgSeenMu.Unlock()

	got, err := o.Model("org.line").Domain(domain.New("doc_id.watch", "=", "A")).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// 先确认钩子确实生效了（筛掉了 B 那一行），否则下面的断言没有意义。
	if got == nil || got.Count() != 1 || firstIdAny(got.Keys()) != firstId(t, lineA) {
		t.Fatalf("点号路径上的钩子没有生效：%v", got)
	}

	orgSeenMu.Lock()
	defer orgSeenMu.Unlock()
	if len(orgSeen) == 0 {
		t.Fatal("对端字段的钩子一次都没被调用")
	}
	for _, name := range orgSeen {
		if name != "org.line" {
			t.Fatalf("钩子看见的 Origin = %q，want org.line（最外层发起查询的模型）", name)
		}
	}
}

func firstIdAny(keys []any) int64 {
	if len(keys) == 0 {
		return 0
	}
	switch v := keys[0].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
