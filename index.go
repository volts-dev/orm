package orm

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"

	"github.com/volts-dev/orm/dialect"
	"github.com/volts-dev/utils"
)

const (
	IndexType = iota + 1
	UniqueType
	// GinType 倒排索引（Postgres GIN），jsonb 列专用。
	//
	// 不是"更快一点"的问题：jsonb 列上建**普通 btree** 索引，一是 `@>` 用不上它
	// （btree 只认整值比较），二是稍大的文档会直接把 INSERT 打挂——btree 索引行有
	// 2704 字节上限，超了报 `index row size ... exceeds btree version 4 maximum`，
	// 而这个错要到某条记录的规格填得够多时才出现。
	GinType
)

// maxIndexNameLen 是三个方言里最紧的标识符上限（PG 63 字节；MySQL 64；SQLite 无限）。
const maxIndexNameLen = 63

type (
	// database index and unique
	TIndex struct {
		IsRegular bool
		Name      string
		Type      int
		Cols      []string
		// Exprs 表达式键部分（原样 SQL，如 `lower(name)`），渲染时排在 Cols 之后、
		// 各自包一层括号。PG/SQLite 原生；MySQL ≥8.0.13 走 functional key parts。
		Exprs []string
		// Where 部分索引谓词（**不含** WHERE 关键字，如 `state = 'pending'`）；
		// 空串 = 全表索引。PG/SQLite 原生；MySQL 见 dialect_mysql.go 的处置。
		Where string
		// fromDb 标记这条索引是从数据库**反查**出来的，不是哪个结构体声明的。
		// RegisterModel 会把反查模型与结构体模型的索引合并进同一个共享 TModelObject，
		// 之后光看 GetIndexes() 分不清"谁声明的"；_alterTable 靠它识别定义已变的
		// 旧版定义性索引（同逻辑名、不同哈希）并把它删掉。
		fromDb bool
	}

	// IndexSpec 是 ModelBuilder.SetIndexSpec 的声明形态。Cols 与 Exprs 至少给一个。
	//
	// Exprs/Where 是**模型作者写的 SQL 片段**，与字段名同一信任级别，会原样进 DDL；
	// newIndexSpec 只做最基本的拒绝（`;`、注释、不配对的括号/引号），不做语义校验。
	IndexSpec struct {
		Name   string   // 可选；空则按表名+列名生成，定义性索引再追加 _p<hash>
		Unique bool     //
		Cols   []string // 普通列
		Exprs  []string // 表达式键部分
		Where  string   // 部分索引谓词（不含 WHERE）
	}
)

func generate_index_name(indexType int, tableName string, fields []string) string {
	tableName = strings.Replace(tableName, `"`, "", -1)
	tableName = TrimCasedName(tableName, true)

	var fieldName string
	if len(fields) == 1 {
		fieldName = fields[0]
	} else {
		fieldName = TrimCasedName(strings.Join(fields, "_"), true)
	}

	var b strings.Builder
	if indexType == UniqueType {
		b.WriteString(DefaultUniquePrefix)
	} else {
		b.WriteString(DefaultIndexPrefix)
	}

	b.WriteString(tableName)
	b.WriteString("_")
	b.WriteString(fieldName)
	return b.String()
}

// new an index
func newIndex(name string, tableName string, indexType int, fields ...string) *TIndex {
	if name == "" {
		name = generate_index_name(indexType, tableName, fields)
	}

	if fields == nil {
		fields = make([]string, 0)
	}

	return &TIndex{IsRegular: true, Name: name, Type: indexType, Cols: fields}
}

// definitionHashSuffixRe 匹配定义性索引名末尾的 `_p<8 位 hex>`。
var definitionHashSuffixRe = regexp.MustCompile(`_p[0-9a-f]{8}$`)

// hasDefinitionHash 判断一个索引名是否带定义哈希后缀。
func hasDefinitionHash(name string) bool {
	return definitionHashSuffixRe.MatchString(name)
}

// logicalIndexName 去掉定义哈希后缀，得到索引的"逻辑名"：同一份声明改了谓词/表达式，
// 逻辑名不变、哈希变。_alterTable 据此把库里的旧版本认成"同一条索引的过期定义"。
func logicalIndexName(name string) string {
	return definitionHashSuffixRe.ReplaceAllString(name, "")
}

// normalizeSQLFragment 把一段 SQL 片段规整成可比较/可哈希的形态：小写、空白折叠。
func normalizeSQLFragment(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// definitionHash 对（列、表达式、谓词）算一个 8 位 hex 摘要。定义参与命名，于是
// 定义一变名字就变，SyncModel 按名字对账时旧索引自然被识别为过期。
func definitionHash(cols, exprs []string, where string) string {
	h := fnv.New32a()
	for _, c := range cols {
		h.Write([]byte("c:" + normalizeSQLFragment(c) + "\x00"))
	}
	for _, e := range exprs {
		h.Write([]byte("e:" + normalizeSQLFragment(e) + "\x00"))
	}
	h.Write([]byte("w:" + normalizeSQLFragment(where)))
	return fmt.Sprintf("%08x", h.Sum32())
}

// checkSQLFragment 拒绝明显不该出现在索引定义片段里的东西。
func checkSQLFragment(kind, s string) error {
	t := strings.TrimSpace(s)
	if t == "" {
		return fmt.Errorf("index %s must not be empty", kind)
	}
	for _, bad := range []string{";", "--", "/*", "*/"} {
		if strings.Contains(t, bad) {
			return fmt.Errorf("index %s %q must not contain %q", kind, s, bad)
		}
	}
	depth, quotes := 0, 0
	for _, r := range t {
		switch r {
		case '\'':
			quotes++
		case '(':
			if quotes%2 == 0 {
				depth++
			}
		case ')':
			if quotes%2 == 0 {
				depth--
				if depth < 0 {
					return fmt.Errorf("index %s %q has unbalanced parentheses", kind, s)
				}
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("index %s %q has unbalanced parentheses", kind, s)
	}
	if quotes%2 != 0 {
		return fmt.Errorf("index %s %q has unbalanced quotes", kind, s)
	}
	return nil
}

// newIndexSpec 把一份 IndexSpec 落成 TIndex：校验片段、生成带定义哈希的名字。
func newIndexSpec(tableName string, spec IndexSpec) (*TIndex, error) {
	if len(spec.Cols) == 0 && len(spec.Exprs) == 0 {
		return nil, fmt.Errorf("index on %s: need at least one column or expression", tableName)
	}
	for _, c := range spec.Cols {
		if strings.TrimSpace(c) == "" {
			return nil, fmt.Errorf("index on %s: empty column name", tableName)
		}
	}
	exprs := make([]string, 0, len(spec.Exprs))
	for _, e := range spec.Exprs {
		if err := checkSQLFragment("expression", e); err != nil {
			return nil, err
		}
		exprs = append(exprs, strings.TrimSpace(e))
	}
	where := strings.TrimSpace(spec.Where)
	if where != "" {
		if err := checkSQLFragment("predicate", where); err != nil {
			return nil, err
		}
	}

	idxType := IndexType
	if spec.Unique {
		idxType = UniqueType
	}

	name := strings.TrimSpace(spec.Name)
	if name == "" {
		nameParts := spec.Cols
		if len(nameParts) == 0 {
			nameParts = []string{"expr"}
		}
		name = generate_index_name(idxType, tableName, nameParts)
	} else if !strings.HasPrefix(name, DefaultIndexPrefix) && !strings.HasPrefix(name, DefaultUniquePrefix) {
		name = generate_index_name(idxType, tableName, []string{name})
	}

	if len(exprs) > 0 || where != "" {
		// 定义性索引：名字带定义哈希。用户给了自定义名也照追加——否则改了谓词名字
		// 不变，库里那个旧定义永远不会被重建，且没有任何迹象。
		suffix := "_p" + definitionHash(spec.Cols, exprs, where)
		if len(name)+len(suffix) > maxIndexNameLen {
			name = name[:maxIndexNameLen-len(suffix)]
		}
		name += suffix
	}

	return &TIndex{
		IsRegular: true,
		Name:      name,
		Type:      idxType,
		Cols:      append([]string(nil), spec.Cols...),
		Exprs:     exprs,
		Where:     where,
	}, nil
}

func (index *TIndex) GetName(tableName string) string {
	if !strings.HasPrefix(index.Name, DefaultUniquePrefix) &&
		!strings.HasPrefix(index.Name, DefaultIndexPrefix) {

		if index.Name == "" {
			return generate_index_name(index.Type, tableName, index.Cols)
		}
		return generate_index_name(index.Type, tableName, []string{index.Name})
	}

	return index.Name
}

// IsPartial 是否带谓词（部分索引）。
func (index *TIndex) IsPartial() bool {
	return strings.TrimSpace(index.Where) != ""
}

// HasExprs 是否含表达式键部分。
func (index *TIndex) HasExprs() bool {
	return len(index.Exprs) > 0
}

// isDefinitional 判断这条索引的"定义"是否参与命名：带表达式/谓词，或名字本身带
// 定义哈希（内省回来的索引即便没解析出表达式，凭名字也能认出来）。
func (index *TIndex) isDefinitional() bool {
	return index.HasExprs() || index.IsPartial() || hasDefinitionHash(index.Name)
}

// add columns which will be composite index
func (index *TIndex) AddColumn(cols ...string) {
	for _, col := range cols {
		if utils.IndexOf(col, index.Cols...) > -1 {
			continue
		}

		index.Cols = append(index.Cols, col)
	}
}

// Equal 判断两条索引"是不是同一个定义"——SyncModel 据此决定要不要 DROP 再 CREATE。
//
// 定义性索引（表达式/谓词）不能拿文本比：PG 会把 `lower(name)` 规整成
// `lower((name)::text)`、把谓词包上括号和类型转换，文本永远对不上，比了就是每次
// 启动都重建一遍。它们的定义已经哈希进名字里，**同名即同定义**。
func (index *TIndex) Equal(dst *TIndex) bool {
	if index.Type != dst.Type {
		return false
	}

	if index.isDefinitional() || dst.isDefinitional() {
		return index.Name == dst.Name
	}

	if len(index.Cols) != len(dst.Cols) {
		return false
	}

	for i := 0; i < len(index.Cols); i++ {
		var found bool
		for j := 0; j < len(dst.Cols); j++ {
			if index.Cols[i] == dst.Cols[j] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// indexKeyParts 渲染索引键列表：列加引号，表达式原样并各包一层括号
// （PG/SQLite 接受多余括号；MySQL functional key part **要求**括号）。
func indexKeyParts(quoter dialect.Quoter, index *TIndex) string {
	parts := make([]string, 0, len(index.Cols)+len(index.Exprs))
	for _, c := range index.Cols {
		parts = append(parts, quoter.Quote(strings.TrimSpace(c)))
	}
	for _, e := range index.Exprs {
		parts = append(parts, "("+strings.TrimSpace(e)+")")
	}
	return strings.Join(parts, ",")
}

// indexWhereClause 渲染部分索引的 WHERE 子句；非部分索引返回空串。
func indexWhereClause(index *TIndex) string {
	if !index.IsPartial() {
		return ""
	}
	return " WHERE (" + strings.TrimSpace(index.Where) + ")"
}

// identRe 匹配一个裸列名（可带双引号/反引号）。
var identRe = regexp.MustCompile("^[\"`]?[A-Za-z_][A-Za-z0-9_$]*[\"`]?$")

// parseIndexDef 解析一条 CREATE INDEX 语句（PG 的 pg_indexes.indexdef、SQLite 的
// sqlite_master.sql），拆出列、表达式和谓词。ok=false 表示不是能识别的形状。
//
// 括号按深度配对而不是 Split("(")：表达式自带括号，简单切分对 `lower((name)::text)`
// 和 `WHERE (state = 'x'::text)` 都会切错。
func parseIndexDef(def string) (cols, exprs []string, where string, ok bool) {
	upper := strings.ToUpper(def)
	on := strings.Index(upper, " ON ")
	if on < 0 {
		return nil, nil, "", false
	}
	open := strings.IndexByte(def[on:], '(')
	if open < 0 {
		return nil, nil, "", false
	}
	open += on

	depth, quotes, close := 0, 0, -1
	for i := open; i < len(def); i++ {
		switch def[i] {
		case '\'':
			quotes++
		case '(':
			if quotes%2 == 0 {
				depth++
			}
		case ')':
			if quotes%2 == 0 {
				depth--
				if depth == 0 {
					close = i
				}
			}
		}
		if close >= 0 {
			break
		}
	}
	if close < 0 {
		return nil, nil, "", false
	}

	for _, part := range splitTopLevel(def[open+1 : close]) {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		// 去掉排序/NULLS 修饰，只留键本身
		for _, suffix := range []string{" NULLS FIRST", " NULLS LAST", " DESC", " ASC"} {
			if strings.HasSuffix(strings.ToUpper(p), suffix) {
				p = strings.TrimSpace(p[:len(p)-len(suffix)])
			}
		}
		if identRe.MatchString(p) {
			cols = append(cols, strings.Trim(p, "\"`"))
		} else {
			exprs = append(exprs, p)
		}
	}

	rest := def[close+1:]
	if w := strings.Index(strings.ToUpper(rest), " WHERE "); w >= 0 {
		where = strings.TrimSpace(rest[w+len(" WHERE "):])
		where = strings.TrimSuffix(where, ";")
	}
	return cols, exprs, where, true
}

// splitTopLevel 按深度 0 的逗号切分（不切括号内和引号内的逗号）。
func splitTopLevel(s string) []string {
	var (
		out    []string
		depth  int
		quotes int
		start  int
	)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			quotes++
		case '(':
			if quotes%2 == 0 {
				depth++
			}
		case ')':
			if quotes%2 == 0 {
				depth--
			}
		case ',':
			if depth == 0 && quotes%2 == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}
