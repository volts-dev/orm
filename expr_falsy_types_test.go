package orm

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/dataset"
)

/*
`('x','=',False)` 在**非关系字段**上的回归。

expr_m2o_false_test.go 管的是 many2one 那一半。这个文件管剩下的一半 —— 2026-08-23
把全工作区 XML 里这条写法逐条分拣（220 处真 domain，另 92 处挂在 bool 上无害）时
发现，修复当时只认 many2one/one2one，别的类型仍旧把 false 当普通右值绑进 `x = ?`。

后果按列类型分成两种，**其中一种不报错**：

	timestamp  → pq: invalid input syntax for type timestamp: "false" (22007)   500
	bigint     → pq: invalid input syntax for type bigint: "false"    (22P02)   500
	varchar    → PG 把参数当文本，跑成 `x = 'false'`                            静默筛错

第三种最坏，真栈实测（2026-08-23，PG 13）：

	calendar_event 共 7 行，其中 privacy 为空串的 2 行
	('privacy','=',False) 应回那 2 行，实回 **0 行**
	日志里只有一条正常的 INFO SQL：calendar_event."privacy" = $1 [false]

界面上表现为"这个筛选器点了没反应"，没有任何东西指向 expr.go。

# 这些用例跑在 sqlite 上，为什么仍然抓得住

sqlite 不做类型检查，PG 上 500 的那两种在这里都变成"静默回错行"（与 m2o 那份
测试头部记录的方言差异一致）。所以判据一律是**集合本身**，不是 err != nil。

数值列要额外当心：sqlite 把 'false' 折算成 0，于是坏代码 `num = 'false'` 恰好
命中 num=0 那行。样本里必须同时有 **NULL 行**，否则数值那条反证是绿的。

反证实测（把 expr.go 的分支条件还原成只认 TYPE_M2O/TYPE_O2O）：

	('txt','=',False)     得到 []                 应为 [empty_row null_row]
	('txt','!=',False)    得到 [empty_row full]   应为 [full]
	('num','=',False)     得到 [empty_row]        应为 [empty_row null_row]
	('amount','=',False)  得到 [empty_row]        应为 [empty_row null_row]
	('dt','=',False)      得到 []                 应为 [empty_row null_row]

`('num','!=',False)` 和 `('dt','!=',False)` 修改前后都绿，**其余的 `!=` 不是**：
`x != 'false'` 对 NULL 行返回 NULL（不匹配），碰巧与"非空"一致；但文本列的空串
`'' != 'false'` 是真，于是 empty_row 混进结果 —— 文本那条 `!=` 是真反证。
数值那条是 sqlite 把 'false' 折成 0 才恰好对上，PG 上它是 22P02。
所以别照着"`!=` 都没事"去删用例。
*/

type FalsyRow struct {
	TModel `table:"name('falsy_row')"`
	Id     int64   `field:"pk autoincr title('ID')"`
	Name   string  `field:"varchar() size(64)"`
	Txt    string  `field:"varchar() size(64)"`
	Num    int64   `field:"int()"`
	Amount float64 `field:"double()"`
	Dt     string  `field:"datetime"`
	Flag   bool    `field:"bool"`
}

// setupFalsyTypes 造三行：一行真有值，一行整排 NULL，一行"空但不是 NULL"
// （文本空串 / 数值 0）—— 后两种在真库里都真实存在，来源不同（create 省略字段
// 落 NULL；界面清空文本框落 ”；数值控件清空落 0）。
func setupFalsyTypes(t *testing.T) *TOrm {
	t.Helper()
	ds := &TDataSource{DbType: "sqlite", DbName: filepath.Join(t.TempDir(), "falsy.db")}
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatalf("orm.New: %v", err)
	}
	if _, err := o.SyncModel("", new(FalsyRow)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	exec := func(sql string, args ...any) {
		if _, err := o.Exec(sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO falsy_row ("name","txt","num","amount","dt","flag") VALUES (?,?,?,?,?,?)`,
		"full", "hello", 7, 1.5, "2026-08-23 10:00:00", true)
	exec(`INSERT INTO falsy_row ("name") VALUES (?)`, "null_row")
	exec(`INSERT INTO falsy_row ("name","txt","num","amount","flag") VALUES (?,?,?,?,?)`,
		"empty_row", "", 0, 0.0, false)
	return o
}

func falsyNames(t *testing.T, o *TOrm, dom any) []string {
	t.Helper()
	ds, err := o.Model("falsy.row").Domain(dom).Limit(-1).Read()
	if err != nil {
		t.Fatalf("read %v: %v", dom, err)
	}
	var out []string
	if ds != nil {
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			out = append(out, rec.FieldByName("name").AsString())
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func checkFalsy(t *testing.T, o *TOrm, dom any, want []string, hint string) {
	t.Helper()
	got := falsyNames(t, o, dom)
	if !sameStrings(got, want) {
		t.Errorf("%v 得到 %v，应为 %v\n（%s）", dom, got, want, hint)
	}
}

// 文本：空串和 NULL 都算空。这是静默筛错的那一类，真栈上 calendar_event.privacy 中招。
func TestFalsy_TextEqualsFalse(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "=", false}, []string{"empty_row", "null_row"},
		"回空说明还在跑 `txt = 'false'`；只剩 null_row 说明修法停在 IS NULL，漏了空串")
}

func TestFalsy_TextNotEqualsFalse(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "!=", false}, []string{"full"},
		"混进 empty_row 说明 `<> ''` 那半边没生效")
}

// 数值：0 和 NULL 都算空（Odoo 语义里 0 是 falsy）。PG 上原来是 22P02 直接 500。
func TestFalsy_NumberEqualsFalse(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"num", "=", false}, []string{"empty_row", "null_row"},
		"只剩 empty_row 是 sqlite 把 'false' 折成 0 的假象——PG 上这条是 500")
	checkFalsy(t, o, []any{"amount", "=", false}, []string{"empty_row", "null_row"},
		"浮点列同理")
}

func TestFalsy_NumberNotEqualsFalse(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"num", "!=", false}, []string{"full"},
		"混进 empty_row 说明 `<> 0` 那半边没生效")
}

// 时间：没有"零值"落库形态，只有 NULL。空串塞进 timestamp 本身就是 22007，
// 所以这里**不能**套文本那条 `OR x = ”`。
func TestFalsy_DatetimeEqualsFalse(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"dt", "=", false}, []string{"empty_row", "null_row"},
		"empty_row 的 dt 没写过值，落的就是 NULL；回空说明还在跑 `dt = 'false'`")
}

func TestFalsy_DatetimeNotEqualsFalse(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"dt", "!=", false}, []string{"full"},
		"这条在修复前后都应为 [full]，是护栏不是反证")
}

// bool 刻意不走新分支：下方原有的 Bool 分支已经是对的。改坏它的表现是
// `('flag','=',False)` 漏掉 NULL 行。
func TestFalsy_BoolStillUsesItsOwnBranch(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"flag", "=", false}, []string{"empty_row", "null_row"},
		"bool 的 `= False` 必须仍是 `IS NULL or = false`")
	checkFalsy(t, o, []any{"flag", "!=", false}, []string{"full"}, "bool 的反向")
}

// 普通比较不能被新分支吞掉：右值是 false **以外**的东西时，一切照旧。
func TestFalsy_OrdinaryComparisonsUnaffected(t *testing.T) {
	o := setupFalsyTypes(t)
	checkFalsy(t, o, []any{"txt", "=", "hello"}, []string{"full"}, "普通等值比较被吞了")
	checkFalsy(t, o, []any{"num", "=", 7}, []string{"full"}, "数值等值比较被吞了")
	checkFalsy(t, o, []any{"num", ">", 0}, []string{"full"}, "非 =/!= 的算符被吞了")
}

// 谓词生成本身的单元测试。selection / jsonb 这两类不方便用 sqlite 建样本，
// 但它们的分派恰恰最容易写错：selection 存的是 varchar，jsonb 套上文本那条
// `= ”` 会变成 22P02。
func TestFalsy_PredicateDispatch(t *testing.T) {
	cases := []struct {
		typeName string
		wantSub  string // 期望片段
		wantOk   bool
	}{
		{TYPE_M2O, `<= 0`, true},
		{TYPE_O2O, `<= 0`, true},
		{TYPE_SELECTION, `= ''`, true},
		{Varchar, `= ''`, true},
		{Text, `= ''`, true},
		{BigInt, `= 0`, true},
		{Double, `= 0`, true},
		{DateTime, `IS NULL`, true},
		{Date, `IS NULL`, true},
		{Bytea, `IS NULL`, true},
		{Jsonb, `IS NULL`, true},
		{TYPE_JSONB, `IS NULL`, true},
		{Bool, ``, false},    // 交回原有的 Bool 分支
		{Boolean, ``, false}, //
		{"某种没见过的类型", ``, false},
	}
	for _, c := range cases {
		got, ok := isFalsyValue(c.typeName, "t", "c")
		if ok != c.wantOk {
			t.Errorf("isFalsyValue(%q) ok=%v，应为 %v", c.typeName, ok, c.wantOk)
			continue
		}
		if !ok {
			continue
		}
		if !strings.Contains(got, c.wantSub) {
			t.Errorf("isFalsyValue(%q) = %q，应含 %q", c.typeName, got, c.wantSub)
		}
		// json 绝不能套上文本那条：`jsonb = ''` 本身就是 22P02。
		if (c.typeName == Jsonb || c.typeName == Json || c.typeName == TYPE_JSONB) &&
			strings.Contains(got, `= ''`) {
			t.Errorf("isFalsyValue(%q) = %q —— json 列不能比空串", c.typeName, got)
		}
	}
	// 正反两支必须成对：认得 falsy 就必须认得 truthy，否则 `!=` 那条会静默回落
	// 到 `x != ?` 老路，而 `=` 已经修好了 —— 半修比不修更难查。
	for _, c := range cases {
		_, okF := isFalsyValue(c.typeName, "t", "c")
		_, okT := isTruthyValue(c.typeName, "t", "c")
		if okF != okT {
			t.Errorf("类型 %q 的 isFalsyValue/isTruthyValue 支持度不一致（%v vs %v）",
				c.typeName, okF, okT)
		}
	}
}

// ---------------------------------------------------------------- 字符串 domain 路径
//
// 上面所有用例喂的都是 Go 原生 `false`（`[]any{"txt","=",false}`），走的是调用方
// 直接构造叶子那条路。**但 XML 里的 domain 是字符串**，要先过 domain/parser.go：
//
//	<field name="domain_force">[('user_id', '=', False)]</field>
//	<filter domain="[('active', '=', False)]"/>
//
// 走到 leaf_to_sql 时右值的**类型**不同，而这一路上有两个彼此独立的机制在兜底，
// 2026-08-24 逐个拆掉实测过：
//
//  1. domain/parser.go:333 `value := strings.ToLower(item.Val)` 把不带引号的
//     `False` 折成 `"false"`，再 `list.Push(value == "true")` 推一个真 Go bool；
//  2. 折叠没生效时右值是**字符串** `"False"`，而闸门 `utils.IsBoolItf` 对字符串
//     走的是 `strconv.ParseBool`，它认 `"False"`/`"True"` —— 于是照样进 falsy 分支。
//
// 只拆①仍然全绿（②接住了），只拆②也全绿（①接住了）。**两个一起坏才显形**，
// 而那时上面每一条 Go bool 用例都还是绿的：它们喂的是原生 `false`，两个机制
// 一个都不经过。
//
// 反证（parser 不折叠 + 闸门收紧成只认 Go 原生 bool —— 后者是最可能的一次
// "顺手收严"）：上面 8 条全绿，只有本条红，报的正是
//
//	[('txt','=',False)] 得到 []，应为 [empty_row null_row]
//
// 2026-08-24 全工作区分拣：live 目录（排除 modules/x、.history 这些死目录）里
// 这个写法有 493 处，其中 216 处在 `domain=`、40 处在 `domain_force=`。没有一条
// 是用 Go 原生 bool 写的 —— 也就是说上面那批用例覆盖的是**一条真实调用方都没有**
// 的入口，而真正在用的这条从来没被测过。
func TestFalsy_StringDomainPath(t *testing.T) {
	o := setupFalsyTypes(t)
	// 与 Go bool 那批逐条对齐：同一个语义换成 XML 的写法，结果必须一模一样。
	for _, c := range []struct {
		dom  string
		want []string
		hint string
	}{
		{`[('txt','=',False)]`, []string{"empty_row", "null_row"},
			"回空说明两个兜底机制一起坏了（见上），跑成了 txt = 'False'"},
		{`[('txt','!=',False)]`, []string{"full"}, "混进 empty_row 说明 `<> ''` 那半边没生效"},
		{`[('num','=',False)]`, []string{"empty_row", "null_row"}, "数值列同理；只剩 empty_row 是 sqlite 把 'False' 折成 0 的假象"},
		{`[('amount','=',False)]`, []string{"empty_row", "null_row"}, "浮点列同理"},
		{`[('dt','=',False)]`, []string{"empty_row", "null_row"}, "时间列在 PG 上原来是 22007 直接 500"},
		{`[('flag','=',False)]`, []string{"empty_row", "null_row"}, "bool 走的是它自己那条老分支，不该被这条路影响"},
		// 双引号、多余空白：XML 属性里两种引号都有人写。
		{`[("txt", "=", False)]`, []string{"empty_row", "null_row"}, "双引号 + 空白的写法必须等价"},
		// 小写 false：parser 的 ToLower 让两种写法同义，这里盯住它。
		{`[('txt','=',false)]`, []string{"empty_row", "null_row"}, "小写 false 与 False 必须同义"},
	} {
		checkFalsy(t, o, c.dom, c.want, c.hint)
	}
}

// ---------------------------------------------------------------- 类型覆盖闸门
//
// 上面的用例是按"本仓现在用到的类型"逐个举例，举例天生举不全。这一条反过来问：
// **ORM 能产生的每一种类型，isFalsyValue 认不认得？**
//
// 认不得的后果不是报错，是 falsyOk 返回 false → 整条 leaf 回落到通用路径
// `x = ?` 并把 false 当普通右值绑进去，也就是本次修复之前的行为：PG 上按列类型
// 分成 22P02 / 22007（500）和"静默筛错"三种。加一个新字段类型时没人会想到来改
// expr.go，所以这里把**允许掉出去的集合**钉死，多一个就红。
//
// 2026-08-24 实测：58 种类型里掉出去 5 种，下面逐条记了为什么。
func TestFalsy_EveryTypeIsDispatchedOrExempt(t *testing.T) {
	// 允许掉出 isFalsyValue 的类型，以及各自的去处。
	// 往这张表里加东西之前先确认：那个类型的 `= False` 到底由谁负责？
	exempt := map[string]string{
		Bool:    "交给 leaf_to_sql 里原有的 Bool 分支（同一语义只留一个出口）",
		Boolean: "同 Bool",
		TYPE_O2M: "x2many 在更前面就被 resolveX2manyLeaf 接管（isX2many 排在 !Store() 之前），" +
			"到不了这里；它的 False 语义见 expr_x2many.go 的 isDomainFalse",
		TYPE_M2M: "同 one2many",

		// ★ 这条不是"有意豁免"，是**已知未处理**。
		//   数组列上的 `= False`（Odoo 语义是"空数组"）会落到通用路径，PG 上是
		//   `arr = 'false'` —— 报错还是筛错取决于元素类型。本仓目前没有任何字段
		//   声明成数组（2026-08-24 查 sys_model_field 的 5583 个 (模型,字段) 组合，
		//   ARRAY 一个没有），所以是理论缺口不是现存 bug。
		//   真要用数组列了，正确谓词是 `(x IS NULL OR cardinality(x) = 0)`，
		//   但那是 PG 方言，得先想清楚 sqlite/mysql 怎么办 —— 别顺手加一条。
		Array: "已知未处理：本仓无数组字段，用到了必须先补 isFalsyValue（见注释）",
	}

	seen := map[string]bool{}
	var all []string
	add := func(ts ...string) {
		for _, x := range ts {
			if !seen[x] {
				seen[x] = true
				all = append(all, x)
			}
		}
	}
	for k := range SqlTypes {
		add(k)
	}
	add(TYPE_M2O, TYPE_O2O, TYPE_O2M, TYPE_M2M, TYPE_SELECTION,
		TYPE_JSONB, TYPE_PROPERTIES, TYPE_PROPERTIES_DEFINITION)
	sort.Strings(all)

	for _, ty := range all {
		_, ok := isFalsyValue(ty, "t", "c")
		_, allowed := exempt[ty]
		if !ok && !allowed {
			t.Errorf("类型 %q 掉出了 isFalsyValue —— 它上面的 ('x','=',False) 会回落到 `x = ?`，"+
				"PG 上按列类型是 500 或静默筛错。\n"+
				"要么在 isFalsyValue/isTruthyValue 里给它一条谓词，"+
				"要么把它加进本用例的 exempt 表并写清楚由谁负责。", ty)
		}
		if ok && allowed {
			t.Errorf("类型 %q 已经被 isFalsyValue 处理了，但还留在 exempt 表里 —— "+
				"删掉那一行，免得下次有人照着它误判。", ty)
		}
	}
}
