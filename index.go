package orm

import (
	"strings"

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

type (
	// database index and unique
	TIndex struct {
		IsRegular bool
		Name      string
		Type      int
		Cols      []string
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

	return &TIndex{true, name, indexType, fields}
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

// add columns which will be composite index
func (index *TIndex) AddColumn(cols ...string) {
	for _, col := range cols {
		if utils.IndexOf(col, index.Cols...) > -1 {
			continue
		}

		index.Cols = append(index.Cols, col)
	}
}

func (index *TIndex) Equal(dst *TIndex) bool {
	if index.Type != dst.Type {
		return false
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
