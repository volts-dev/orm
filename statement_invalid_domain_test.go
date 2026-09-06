package orm

import (
	stdErrors "errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/volts-dev/dataset"
	ormerr "github.com/volts-dev/orm/errors"
	"github.com/volts-dev/utils"
)

/*
条件解析失败必须**中断**，不能**降级**。

vectors 里至少七处注释记着同一个坑（core/model/model_controller.go、core/report/columns.go、
core/recordrule/compile.go、core/tenant/remote_records.go、module/registry/.../base_import_util.go…）：
"domain 解析失败是降级不是中断——退化成无条件查询后读回全表且不报错"。

根因在 TStatement.Op()：Where()/Domain()/And()/Or() 为了链式调用没有返回错误的位置，
原来解析失败只 log 一句，条件整个不进树，语句变成"少一个条件的合法查询"。三种形态：

  1. 解析器真的报错 / 参数类型不支持（[]string、map…）——原来 log 后继续；
  2. 解析器对垃圾输入（`((`）不报错、回一棵**空树**——空树 == 没条件，最危险；
  3. 解析器回一棵**元数不平**的树（`['|', leaf]`）——normalize_domain 原来只 log，
     多出来的 '|' 被吞掉、叶子按 AND 生效，结果集"看着正常"却不是写的那个条件。

现在三种都记到 Statement 上（或在 where_calc 里报出），Read/Search/Count/Sum/Write/
Delete/ReadGroup 一律拒绝执行并交回 ErrInvalidDomain。
*/

type idmItem struct {
	TModel `table:"name('idm_item')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
	Age    int    `field:"int()"`
}

func setupIdm(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "idm.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(idmItem)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := o.Model("idm.item").Create(map[string]any{"name": fmt.Sprintf("n%d", i), "age": i}); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

func wantInvalidDomain(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: 本该以 ErrInvalidDomain 拒绝，却成功执行了——条件被静默丢掉", label)
	}
	if !stdErrors.Is(err, ormerr.ErrInvalidDomain) {
		t.Fatalf("%s: 错误类型不对，想要 ErrInvalidDomain，得到 %v", label, err)
	}
}

// 形态 1：参数类型不支持。原来 log 一句 "op not support this query" 然后继续。
func TestInvalidDomain_UnsupportedType(t *testing.T) {
	o := setupIdm(t)
	_, err := o.Model("idm.item").Domain([]string{"age", "=", "1"}).Limit(10).Read()
	wantInvalidDomain(t, `Domain([]string)`, err)
	_, err = o.Model("idm.item").Domain(map[string]any{"age": 1}).Limit(10).Read()
	wantInvalidDomain(t, `Domain(map)`, err)
}

// 形态 2：非空字符串解析成空树。带 Limit 是为了绕开 guardUnscopedRead——
// 那道守卫只拦"没条件又没上限"，而生产上 BeforeSession 追加的 tenant_id 让"有条件"恒成立，
// 这里模拟的正是那种"守卫挡不住"的形状。
func TestInvalidDomain_GarbageParsesToEmpty(t *testing.T) {
	o := setupIdm(t)
	for _, bad := range []string{`((`, `[(`, `)))`} {
		_, err := o.Model("idm.item").Where(bad).Limit(10).Read()
		wantInvalidDomain(t, fmt.Sprintf("Where(%q)", bad), err)
	}
}

// 空白/空括号是**合法的空条件**，不是错误——请求体里 domain 缺省就是 "" 或 "[]"。
func TestInvalidDomain_BlankIsNoop(t *testing.T) {
	o := setupIdm(t)
	for _, blank := range []string{``, `  `, `[]`, `()`, `[ ]`} {
		rs, err := o.Model("idm.item").Domain(blank).Limit(-1).Read()
		if err != nil {
			t.Fatalf("Domain(%q) 应为无操作，却报错 %v", blank, err)
		}
		if rs.Count() != 5 {
			t.Fatalf("Domain(%q) 应读回全部 5 行，得到 %d", blank, rs.Count())
		}
	}
	rs, err := o.Model("idm.item").Domain(nil).Limit(-1).Read()
	if err != nil || rs.Count() != 5 {
		t.Fatalf("Domain(nil) 应为无操作读回 5 行，得到 %d / %v", rs.Count(), err)
	}
}

// 形态 3：元数不平。`['|', leaf]` 缺一项，原来只 log 一句 "syntactically not correct"。
func TestInvalidDomain_ArityMismatch(t *testing.T) {
	o := setupIdm(t)
	_, err := o.Model("idm.item").Domain(`['|',('age','=',1)]`).Read()
	wantInvalidDomain(t, `['|', leaf]`, err)
	_, err = o.Model("idm.item").Domain([]any{"&", []any{"age", "=", 1}}).Read()
	wantInvalidDomain(t, `["&", leaf]`, err)
}

// 每个执行入口都得拒绝，不只是 Read。
func TestInvalidDomain_AllExecutorsRefuse(t *testing.T) {
	o := setupIdm(t)
	bad := []string{"age", "=", "1"} // unsupported type → Op 记错

	_, _, err := o.Model("idm.item").Domain(bad).Search()
	wantInvalidDomain(t, "Search", err)

	_, err = o.Model("idm.item").Domain(bad).Count()
	wantInvalidDomain(t, "Count", err)

	_, err = o.Model("idm.item").Domain(bad).Sum("age")
	wantInvalidDomain(t, "Sum", err)

	_, err = o.Model("idm.item").Domain(bad).Write(map[string]any{"age": 99})
	wantInvalidDomain(t, "Write", err)

	_, err = o.Model("idm.item").Domain(bad).Delete()
	wantInvalidDomain(t, "Delete", err)

	_, err = o.Model("idm.item").Domain(bad).ReadGroup([]string{"name"}, []string{"age"})
	wantInvalidDomain(t, "ReadGroup", err)

	// 反证：数据一行没少、一行没改。
	rs, err := o.Model("idm.item").Limit(-1).Read()
	if err != nil {
		t.Fatal(err)
	}
	if rs.Count() != 5 {
		t.Fatalf("坏条件的 Delete 不该删掉任何行，剩 %d", rs.Count())
	}
	rs.Range(func(_ int, rec *dataset.TRecordSet) error {
		if utils.ToInt(rec.GetByField("age")) == 99 {
			t.Fatalf("坏条件的 Write 不该改到任何行")
		}
		return nil
	})
}

// 语句上的错误随执行复位：同一会话下一条正常查询不受污染。
func TestInvalidDomain_ResetsAfterExecution(t *testing.T) {
	o := setupIdm(t)
	sess := o.NewSession()
	defer sess.Close()
	_, err := sess.Model("idm.item").Domain([]string{"x"}).Limit(10).Read()
	wantInvalidDomain(t, "first", err)

	rs, err := sess.Model("idm.item").Where("age>?", 2).Read()
	if err != nil {
		t.Fatalf("上一条的错误不该带到下一条：%v", err)
	}
	if rs.Count() != 2 {
		t.Fatalf("age>2 应为 2 行，得到 %d", rs.Count())
	}
}

// 第一个错误才是根因，后来的不覆盖。
func TestStatement_FailKeepsFirstError(t *testing.T) {
	st := &TStatement{}
	e1, e2 := fmt.Errorf("first"), fmt.Errorf("second")
	st.fail(e1)
	st.fail(e2)
	if st.Err() != e1 {
		t.Fatalf("Err() 应为第一个错误，得到 %v", st.Err())
	}
	st.fail(nil)
	if st.Err() != e1 {
		t.Fatalf("fail(nil) 不该清掉已记录的错误")
	}
}

func TestIsBlankDomainString(t *testing.T) {
	for _, s := range []string{"", " ", "[]", "()", "[ ( ) ]", "\n\t", "[],"} {
		if !isBlankDomainString(s) {
			t.Errorf("%q 应判为空条件", s)
		}
	}
	for _, s := range []string{"((x", "[('a','=',1)]", "a=1", "?", "((", ")))", "[(", "(]", "[)"} {
		if isBlankDomainString(s) {
			t.Errorf("%q 不该判为空条件", s)
		}
	}
}
