package orm

import (
	"context"
	"strings"
	"testing"
)

// 冻结之后补一个存储字段：同一个进程里此后的读、写、按它过滤、按它排序全部立刻可用。
//
// 这是 vectors 扩展字段（别的模块声明、落在属主表上的列）的地基：属主进程启动完、模型
// 集合早已冻结，贡献方的声明才到。如果补上来的字段要等重启才生效，或者读得到写不进，
// 那一整套设计就不成立。
func TestAddField_AfterFreezeIsReadableWritableSearchable(t *testing.T) {
	o := setupIntegrationOrm(t)
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	// 补字段之前就存在的行：新列上是空的，读它不能报错。
	oldIds, err := o.Model("bench.model").Create(map[string]any{"name": "before", "age": 1})
	if err != nil {
		t.Fatalf("create before AddField: %v", err)
	}

	added, err := o.AddField("bench.model", "warn_msg", "text() title('Warning Message')")
	if err != nil {
		t.Fatalf("AddField: %v", err)
	}
	if !added {
		t.Fatal("AddField 返回 added=false，而这个字段此前并不存在")
	}

	f := o.Model("bench.model").Statement.Model.GetFieldByName("warn_msg")
	if f == nil {
		t.Fatal("补上的字段在模型上查不到")
	}
	if f.Label() != "Warning Message" {
		t.Fatalf("title tag 没有生效: %q", f.Label())
	}

	if _, err := o.Model("bench.model").Create(map[string]any{"name": "b", "warn_msg": "beta"}); err != nil {
		t.Fatalf("create with the new field: %v", err)
	}
	if _, err := o.Model("bench.model").Create(map[string]any{"name": "a", "warn_msg": "alpha"}); err != nil {
		t.Fatalf("create with the new field: %v", err)
	}
	if _, err := o.Model("bench.model").Ids(oldIds...).Write(map[string]any{"warn_msg": "gamma"}); err != nil {
		t.Fatalf("write the new field on a pre-existing row: %v", err)
	}

	ds, err := o.Model("bench.model").Domain(`[('warn_msg','=','gamma')]`).Read()
	if err != nil {
		t.Fatalf("search on the new field: %v", err)
	}
	if ds.Count() != 1 || ds.FieldByName("name").AsString() != "before" {
		t.Fatalf("按新字段过滤应只命中补字段前就在的那一行，实得 %d 行", ds.Count())
	}

	ds, err = o.Model("bench.model").OrderBy("warn_msg").Limit(-1).Read()
	if err != nil {
		t.Fatalf("order by the new field: %v", err)
	}
	var got []string
	for ds.First(); !ds.Eof(); ds.Next() {
		got = append(got, ds.FieldByName("warn_msg").AsString())
	}
	if strings.Join(got, ",") != "alpha,beta,gamma" {
		t.Fatalf("按新字段排序: %v", got)
	}
}

// 同名同类型再补一次是 no-op；同名换类型必须拒绝 —— 替换一个正在被读写的字段对象不安全。
func TestAddField_IdempotentAndRefusesTypeChange(t *testing.T) {
	o := setupIntegrationOrm(t)
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if _, err := o.AddField("bench.model", "x_flag", "bool()"); err != nil {
		t.Fatalf("first AddField: %v", err)
	}
	added, err := o.AddField("bench.model", "x_flag", "bool()")
	if err != nil || added {
		t.Fatalf("再补一次同名同类型应是 (false, nil)，实得 (%v, %v)", added, err)
	}
	if _, err := o.AddField("bench.model", "x_flag", "text()"); err == nil {
		t.Fatal("同名换类型应当报错，却被接受了")
	}
	// 结构体上声明的字段同样受保护。
	if _, err := o.AddField("bench.model", "name", "int()"); err == nil {
		t.Fatal("拿 AddField 改掉结构体字段 name 的类型应当报错")
	}
}

// many2one 指向本进程已有的模型：补上来就能按 id 写读，与结构体上声明的 many2one 一样。
func TestAddField_Many2OneToLocalModel(t *testing.T) {
	o := setupIntegrationOrm(t)
	if err := o.Freeze(context.Background()); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if _, err := o.AddField("bench.model", "parent_id", "many2one(bench.model) title('Parent')"); err != nil {
		t.Fatalf("AddField many2one: %v", err)
	}
	parent, err := o.Model("bench.model").Create(map[string]any{"name": "parent"})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if _, err := o.Model("bench.model").Create(map[string]any{"name": "child", "parent_id": parent[0]}); err != nil {
		t.Fatalf("create child with many2one: %v", err)
	}
	ds, err := o.Model("bench.model").Domain(`[('name','=','child')]`).Read()
	if err != nil || ds.Count() != 1 {
		t.Fatalf("read child: %v (%d rows)", err, ds.Count())
	}
	if ds.FieldByName("parent_id").AsInteger() == 0 {
		t.Fatalf("many2one 值没有写进去: %v", ds.FieldByName("parent_id").AsInterface())
	}
}

// 不认识的模型、没有类型的 tag：报错，不 panic、不 Fatal —— 这是运行期调用，
// 一条写歪的声明不能带走整个进程。
func TestAddField_BadInputIsAnErrorNotAFatal(t *testing.T) {
	o := setupIntegrationOrm(t)
	if _, err := o.AddField("no.such.model", "x", "text()"); err == nil {
		t.Fatal("不存在的模型应当报错")
	}
	if _, err := o.AddField("bench.model", "x", "title('no type')"); err == nil {
		t.Fatal("没有字段类型的 tag 应当报错")
	}
}

// 标题、说明里的括号与撇号原样保留 —— 扩展字段的标题是从声明原文拼进 tag 的，
// 结尾是撇号的文本曾被剥掉最后那个撇号。
func TestAddField_TitleAndHelpKeepParensAndApostrophes(t *testing.T) {
	o := setupIntegrationOrm(t)
	if _, err := o.AddField("bench.model", "x_msg", `text() title('Message (for) Sales ''Order''') help('Shown when (x) happens')`); err != nil {
		t.Fatalf("AddField: %v", err)
	}
	f := o.Model("bench.model").Statement.Model.GetFieldByName("x_msg")
	if f.Label() != "Message (for) Sales 'Order'" {
		t.Fatalf("title = %q", f.Label())
	}
	if f.Base().description != "Shown when (x) happens" {
		t.Fatalf("help = %q", f.Base().description)
	}
}
