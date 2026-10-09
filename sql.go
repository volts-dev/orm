package orm

import (
	"strconv"
	"strings"
)

// IFmter is an interface to Formatter SQL
type (
	IFmter interface {
		Do(sql string, dialect IDialect, model IModel) string
	}

	// TODO remove 考虑移除一般用不到
	// IdFmter filter SQL replace (id) to primary key column name
	IdFmter struct {
	}

	// QuoteFmter format SQL replace ` to database's own quote character
	QuoteFmter struct {
	}

	// HolderFmter filter SQL replace ?, ? ... to $1, $2 ...
	HolderFmter struct {
		Prefix string
		Start  int
	}
)

func (s *QuoteFmter) Do(sql string, dialect IDialect, model IModel) string {
	return strings.Replace(sql, "`", string(dialect.Quoter().Prefix), -1)
}

func (i *IdFmter) Do(sql string, dialect IDialect, model IModel) string {
	if model != nil && len(model.GetPrimaryKeys()) == 1 {
		quoter := dialect.Quoter().Quote
		sql = strings.Replace(sql, " `(id)` ", " "+quoter(model.GetPrimaryKeys()[0])+" ", -1)
		sql = strings.Replace(sql, " "+quoter("(id)")+" ", " "+quoter(model.GetPrimaryKeys()[0])+" ", -1)
		return strings.Replace(sql, " (id) ", " "+quoter(model.GetPrimaryKeys()[0])+" ", -1)
	}
	return sql
}

// Do 把 `?` 依次换成 `$1`、`$2`…。
//
// 从前是 `res += …` 逐段拼接：平方级。成批写入的一条多行 INSERT 有几万个占位符、几十万字节，
// 每拼一段都把前面整串复制一遍——器件库导入每批 2000 行在这里耗掉约 0.7 秒，比执行 SQL 本身还久。
func (s *HolderFmter) Do(sql string, dialect IDialect, model IModel) string {
	n := strings.Count(sql, "?")
	if n == 0 {
		return sql
	}
	var b strings.Builder
	b.Grow(len(sql) + n*(len(s.Prefix)+6))
	i := s.Start
	for {
		j := strings.IndexByte(sql, '?')
		if j < 0 {
			b.WriteString(sql)
			return b.String()
		}
		b.WriteString(sql[:j])
		b.WriteString(s.Prefix)
		b.WriteString(strconv.Itoa(i))
		i++
		sql = sql[j+1:]
	}
}
