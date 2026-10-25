package orm

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MySQL 的按日期粒度截断 / 取日期部分（read_group 的 `date:month`、domain 里的 `date.month_number`；MySQL 兼容，
// 2026-10-26）。口径与 PG 的 granularityExpr 一样：
//
//   - datetime（tz 非空）：列里是 UTC 的墙上时刻，CONVERT_TZ 换到那个时区再截断，截断后的本地起点再换回 UTC；
//   - date：截断后仍是 DATE；
//   - 周从周一开始（PG date_trunc('week')）；iso_week_number 是 ISO 周（WEEK(x, 3)）；day_of_week 周日 = 0
//     （PG EXTRACT(dow)）。
//
// ★ CONVERT_TZ 认地名时区（'Asia/Shanghai'）要库里装了时区表（mysql.time_zone_name）。官方 Docker 镜像装了；没装的库
// 上 CONVERT_TZ 回 NULL、整组落进「空」。第一次用时探一次：没装就退回「此刻这个时区的固定偏移」（'+08:00'）并告警一次
// ——没有夏令时的时区（中国）结果一样，有夏令时的会在切换前后差一小时。装时区表：mysql_tzinfo_to_sql。

func (db *mysql) granularitySql(qualified, granularity string, isDate bool, tz string) string {
	local := qualified
	if !isDate && tz != "" {
		local = db.convertTz(qualified, "'+00:00'", tz, true)
	}
	if part, ok := numberGranularities[granularity]; ok {
		var f string
		switch part {
		case "day":
			f = "DAYOFMONTH(%s)"
		case "dow":
			f = "(DAYOFWEEK(%s) - 1)"
		case "doy":
			f = "DAYOFYEAR(%s)"
		case "week":
			f = "WEEK(%s, 3)"
		case "month":
			f = "MONTH(%s)"
		case "quarter":
			f = "QUARTER(%s)"
		case "year":
			f = "YEAR(%s)"
		case "hour":
			f = "HOUR(%s)"
		case "minute":
			f = "MINUTE(%s)"
		case "second":
			f = "SECOND(%s)"
		default:
			f = "NULL + 0 * %s"
		}
		return "CAST(" + fmt.Sprintf(f, local) + " AS SIGNED)"
	}
	var expr string
	switch granularity {
	case "hour":
		expr = fmt.Sprintf("CAST(DATE_FORMAT(%s, '%%Y-%%m-%%d %%H:00:00') AS DATETIME)", local)
	case "minute": // 只给 GranularitySql 用（read_group 不按分钟分组）
		expr = fmt.Sprintf("CAST(DATE_FORMAT(%s, '%%Y-%%m-%%d %%H:%%i:00') AS DATETIME)", local)
	case "week":
		expr = fmt.Sprintf("CAST(DATE_SUB(DATE(%[1]s), INTERVAL WEEKDAY(%[1]s) DAY) AS DATETIME)", local)
	case "month":
		expr = fmt.Sprintf("CAST(DATE_FORMAT(%s, '%%Y-%%m-01') AS DATETIME)", local)
	case "quarter":
		expr = fmt.Sprintf("CAST(MAKEDATE(YEAR(%[1]s), 1) + INTERVAL (QUARTER(%[1]s) - 1) QUARTER AS DATETIME)", local)
	case "year":
		expr = fmt.Sprintf("CAST(DATE_FORMAT(%s, '%%Y-01-01') AS DATETIME)", local)
	default: // day
		expr = fmt.Sprintf("CAST(DATE(%s) AS DATETIME)", local)
	}
	switch {
	case isDate:
		return "CAST(" + expr + " AS DATE)"
	case tz != "":
		return db.convertTz(expr, "", tz, false)
	}
	return expr
}

// convertTz：toLocal 为真时 UTC → tz，否则 tz → UTC。tz 已经过白名单校验（validTz / time.LoadLocation）。
func (db *mysql) convertTz(expr, _ string, tz string, toLocal bool) string {
	zone := "'" + tz + "'"
	if !db.hasNamedTimeZones() {
		zone = "'" + fixedOffset(tz) + "'"
	}
	if toLocal {
		return "CONVERT_TZ(" + expr + ", '+00:00', " + zone + ")"
	}
	return "CONVERT_TZ(" + expr + ", " + zone + ", '+00:00')"
}

var (
	mysqlTzOnce   sync.Map // *mysql → *mysqlTzState
	mysqlTzWarned sync.Once
)

type mysqlTzState struct {
	once sync.Once
	ok   bool
}

// hasNamedTimeZones 报告库里有没有时区表（见文件头）。查不了（连接问题）按「有」：不因为一次查询失败就永久退回固定偏移。
func (db *mysql) hasNamedTimeZones() bool {
	v, _ := mysqlTzOnce.LoadOrStore(db, &mysqlTzState{})
	st := v.(*mysqlTzState)
	st.once.Do(func() {
		st.ok = true
		if db.queryer == nil {
			return
		}
		rows, err := db.queryer.QueryContext(context.Background(), "SELECT CONVERT_TZ('2026-01-01 00:00:00', '+00:00', 'Asia/Shanghai') IS NULL")
		if err != nil {
			return
		}
		defer rows.Close()
		var missing bool
		if rows.Next() && rows.Scan(&missing) == nil && missing {
			st.ok = false
			mysqlTzWarned.Do(func() {
				log.Warnf("mysql: time zone tables are not loaded (CONVERT_TZ with a named zone returns NULL); grouping by date in a user time zone falls back to the zone's current fixed offset — load them with mysql_tzinfo_to_sql")
			})
		}
	})
	return st.ok
}

// fixedOffset 是 tz 此刻的 UTC 偏移（'+08:00'）。
func fixedOffset(tz string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "+00:00"
	}
	_, off := time.Now().In(loc).Zone()
	sign := '+'
	if off < 0 {
		sign, off = '-', -off
	}
	return fmt.Sprintf("%c%02d:%02d", sign, off/3600, off%3600/60)
}
