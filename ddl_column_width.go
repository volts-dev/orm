package orm

import (
	"fmt"
	"strings"
)

// 列宽对账：库里那一列比模型声明的**窄**时的处置。
//
// # 为什么单独拎出来
//
// _alterTable 对"类型对不上"的兜底处置是打一行
// `db type is <X>, struct type is Y` 的警告就过去了。对字符串列那没什么，对数值列
// 却是一整类静默的数据损坏：一个 REAL 列配 float64 的模型，写 12345.67 读回
// 12345.7，全程无错。这行警告既说不清后果，也没告诉人怎么修，于是它在启动日志里
// 被当噪音滑过去——float 建成 real 那个 bug 能活这么久，有它一份。
//
// # 不自动改
//
// ALTER COLUMN ... TYPE 在 PostgreSQL 上要重写整张表并取 ACCESS EXCLUSIVE 锁。
// 在启动路径上对一张大表这么干，是拿一次几分钟的全表阻塞去换一个可以择时做的
// 迁移——比 bug 本身更糟。所以只把话说清楚，把语句给出来，由运维决定什么时候执行。

// numericWidthRank 给同一族的数值类型排宽度。跨族(整型 vs 浮点)不比较——
// 那种不一致是模型写错了类型，不是宽窄问题。
// 键是各方言 GetSqlType 的输出（已大写），同时收 PG 与 MySQL 的写法。
var numericWidthRank = map[string]struct {
	family string
	rank   int
}{
	"REAL":             {"float", 1},
	"FLOAT":            {"float", 1},
	"DOUBLE":           {"float", 2},
	"DOUBLE PRECISION": {"float", 2},
	"SMALLINT":         {"int", 1},
	"MEDIUMINT":        {"int", 2},
	"INT":              {"int", 3},
	"INTEGER":          {"int", 3},
	"BIGINT":           {"int", 4},
}

// isNumericNarrowing 报告 curType（库里那一列）是否比 expectedType（模型声明的）窄。
// 两者必须同族且库侧更窄才算——反过来（库比模型宽）不丢数据，不必惊动任何人。
func isNumericNarrowing(curType, expectedType string) bool {
	cur, ok := numericWidthRank[normalizeSqlTypeName(curType)]
	if !ok {
		return false
	}
	want, ok := numericWidthRank[normalizeSqlTypeName(expectedType)]
	if !ok {
		return false
	}
	return cur.family == want.family && cur.rank < want.rank
}

// normalizeSqlTypeName 去掉长度修饰与大小写差异：`numeric(16,2)` → `NUMERIC`。
func normalizeSqlTypeName(t string) string {
	t = strings.ToUpper(strings.TrimSpace(t))
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	return t
}

// isSizeWidening 报告把列的长度从 cur 改成 want 是不是**放宽**。0 表示不限长
// （`VARCHAR` 不带长度，selection / 没写 size 的字符串字段都是这样）。
//
// 2026-10-23 sys_tenant_archive 的 status 从 varchar(16) 改成 selection 字段，启动日志里是
// 一条 Warn「change size from VARCHAR(16) to VARCHAR(0)」，进了日志中心像是要把列改成
// 零长——其实是改成不限长，PG 上只改元数据、不重写表。放宽照旧自动做、降成 Info；
// 收窄（含不限长 → 有长度）才是会失败或要重写整张表的那种，只告警给语句。
func isSizeWidening(cur, want int) bool {
	if want == 0 {
		return true
	}
	return cur != 0 && want > cur
}

// sqlTypeWithSize 给日志用：`VARCHAR(16)`；不限长时写成 `VARCHAR（不限长）`，不写 `VARCHAR(0)`。
func sqlTypeWithSize(name string, size int) string {
	if size == 0 {
		return name + "（不限长）"
	}
	return fmt.Sprintf("%s(%d)", name, size)
}
