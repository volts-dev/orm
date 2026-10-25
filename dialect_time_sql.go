package orm

import (
	"fmt"
)

// 手写 SQL 里与时刻有关、两家写法不同的几样（MySQL 兼容，2026-10-26）。给模块的报表 SQL 用（客服报表、工时……）。
// 默认（PG）写法写在包级函数里，MySQL 实现 timeSqler 覆盖；SQLite 不支持这一组。
//
//	                         PG                                      MySQL
//	GranularitySql(t,"hour") date_trunc('hour', t)                   CAST(DATE_FORMAT(t, '%Y-%m-%d %H:00:00') AS DATETIME)
//	SecondsBetweenSql(a, b)  EXTRACT(epoch FROM (b) - (a))           TIMESTAMPDIFF(MICROSECOND, a, b) / 1000000.0
//	TimestampTextSql(t)      to_char(t, 'YYYY-MM-DD HH24:MI:SS')      DATE_FORMAT(t, '%Y-%m-%d %H:%i:%s')
//	TimestampParamSql()      CAST(? AS timestamp)                    CAST(? AS DATETIME(6))
//	StringAggOrderedSql      string_agg(CAST(e AS TEXT), s ORDER BY) GROUP_CONCAT(e ORDER BY … SEPARATOR s)

type timeSqler interface {
	secondsBetweenSql(from, to string) string
	timestampTextSql(expr string) string
	timestampParamSql() string
	stringAggOrderedSql(expr, sep, orderBy string) string
}

// GranularitySql 是 expr（datetime，UTC 墙上时刻，不换时区）按粒度截断后的时刻，或「取一部分」的整数
// （numberGranularities 的键：hour_number、day_of_week……）。粒度同 read_group（groupGranularities）再加 minute。
func GranularitySql(d IDialect, expr, granularity string) (string, error) {
	if _, ok := numberGranularities[granularity]; !ok && !groupGranularities[granularity] && granularity != "minute" {
		return "", fmt.Errorf("orm: unknown granularity %q", granularity)
	}
	if !d.Features().DateTrunc {
		return "", fmt.Errorf("orm: date granularity needs Features.DateTrunc, current dialect is %s", d.String())
	}
	return granularitySql(d, expr, granularity, false, ""), nil
}

// SecondsBetweenSql 是 to − from 的秒数（带小数）。任一边 NULL 得 NULL。
func SecondsBetweenSql(d IDialect, from, to string) string {
	if t, ok := d.(timeSqler); ok {
		return t.secondsBetweenSql(from, to)
	}
	return "EXTRACT(epoch FROM (" + to + ") - (" + from + "))"
}

// TimestampTextSql 是时刻 expr 的文本 'YYYY-MM-DD HH:MM:SS'（24 小时制，不带小数）。
func TimestampTextSql(d IDialect, expr string) string {
	if t, ok := d.(timeSqler); ok {
		return t.timestampTextSql(expr)
	}
	return "to_char(" + expr + ", 'YYYY-MM-DD HH24:MI:SS')"
}

// TimestampParamSql 是一个时刻参数（含一个 ?，参数给 'YYYY-MM-DD HH:MM:SS[.ffffff]' 文本）：PG 的 ? 在算术里推不出类型，
// 要显式转；MySQL 不认 CAST AS timestamp。
func TimestampParamSql(d IDialect) string {
	if t, ok := d.(timeSqler); ok {
		return t.timestampParamSql()
	}
	return "CAST(? AS timestamp)"
}

// StringAggOrderedSql 是按 orderBy 排序、用 sep 连起来的文本（sep 是常量）。MySQL 上受 group_concat_max_len 限制
// （orm 的连接设成 16MB，见 datasource.go）。
func StringAggOrderedSql(d IDialect, expr, sep, orderBy string) string {
	if t, ok := d.(timeSqler); ok {
		return t.stringAggOrderedSql(expr, sep, orderBy)
	}
	return "string_agg(CAST((" + expr + ") AS TEXT), " + sqlStringLiteral(sep) + " ORDER BY " + orderBy + ")"
}

func (db *mysql) secondsBetweenSql(from, to string) string {
	return "(TIMESTAMPDIFF(MICROSECOND, " + from + ", " + to + ") / 1000000.0)"
}

func (db *mysql) timestampTextSql(expr string) string {
	return "DATE_FORMAT(" + expr + ", '%Y-%m-%d %H:%i:%s')"
}

func (db *mysql) timestampParamSql() string { return "CAST(? AS DATETIME(6))" }

func (db *mysql) stringAggOrderedSql(expr, sep, orderBy string) string {
	return "GROUP_CONCAT((" + expr + ") ORDER BY " + orderBy + " SEPARATOR " + mysqlStringLiteral(sep) + ")"
}
