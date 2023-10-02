package orm

import (
	"fmt"
	"strings"
)

// IFmter is an interface to Formatter SQL
type (
	IFmter interface {
		Do(sql string, dialect IDialect, model *TModel) string
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

func (s *QuoteFmter) Do(sql string, dialect IDialect, model *TModel) string {
	return strings.Replace(sql, "`", string(dialect.Quoter().Prefix), -1)
}

func (i *IdFmter) Do(sql string, dialect IDialect, model *TModel) string {
	quoter := dialect.Quoter()
	if model != nil && len(model.GetPrimaryKeys()) == 1 {
		sql = strings.Replace(sql, " `(id)` ", " "+quoter.Quote(model.GetPrimaryKeys()[0])+" ", -1)
		sql = strings.Replace(sql, " "+quoter.Quote("(id)")+" ", " "+quoter.Quote(model.GetPrimaryKeys()[0])+" ", -1)
		return strings.Replace(sql, " (id) ", " "+quoter.Quote(model.GetPrimaryKeys()[0])+" ", -1)
	}
	return sql
}

func (s *HolderFmter) Do(sql string, dialect IDialect, model *TModel) string {
	segs := strings.Split(sql, "?")
	size := len(segs)
	res := ""
	for i, c := range segs {
		if i < size-1 {
			res += c + fmt.Sprintf("%s%v", s.Prefix, i+s.Start)
		}
	}
	res += segs[size-1]
	return res
}
