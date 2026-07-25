package orm

import (
	"strings"
	"testing"
)

// ON CONFLICT 的冲突目标必须**恰好**对应一个已存在的唯一约束。
// 此前的实现只取零散唯一字段里的第一个（且来自 map 遍历、顺序随机），
// 复合唯一索引下生成 `ON CONFLICT ("其中一列")`，Postgres 必报 42P10
// "there is no unique or exclusion constraint matching the ON CONFLICT
// specification"。真栈表现：pro.attr.value 的 demo 数据历史累计 0 行。
func TestExpandToUniqueIndex(t *testing.T) {
	composite := map[string]*TIndex{
		"pro_attr_value_uq": {Name: "pro_attr_value_uq", Type: UniqueType, Cols: []string{"name", "attribute_id"}},
	}

	cases := []struct {
		name         string
		indexes      map[string]*TIndex
		insertFields []string
		uniqueFields []string
		want         string
	}{
		{
			name:         "复合唯一索引补全成整组",
			indexes:      composite,
			insertFields: []string{"id", "name", "attribute_id", "sequence"},
			uniqueFields: []string{"name"}, // 调用方只识别出其中一列
			want:         "name,attribute_id",
		},
		{
			name:         "另一列被识别出来时结果相同（顺序取索引自身列序）",
			indexes:      composite,
			insertFields: []string{"id", "name", "attribute_id"},
			uniqueFields: []string{"attribute_id"},
			want:         "name,attribute_id",
		},
		{
			name:         "索引有列不在本次 INSERT 里则不可用，原样返回",
			indexes:      composite,
			insertFields: []string{"id", "name"}, // 缺 attribute_id
			uniqueFields: []string{"name"},
			want:         "name",
		},
		{
			name: "单列唯一索引照常工作",
			indexes: map[string]*TIndex{
				"uq_code": {Name: "uq_code", Type: UniqueType, Cols: []string{"code"}},
			},
			insertFields: []string{"id", "code"},
			uniqueFields: []string{"code"},
			want:         "code",
		},
		{
			name: "非唯一索引不参与",
			indexes: map[string]*TIndex{
				"idx_name": {Name: "idx_name", Type: IndexType, Cols: []string{"name", "attribute_id"}},
			},
			insertFields: []string{"id", "name", "attribute_id"},
			uniqueFields: []string{"name"},
			want:         "name",
		},
		{
			name: "与本次唯一字段无关的唯一索引不该被选中",
			indexes: map[string]*TIndex{
				"uq_other": {Name: "uq_other", Type: UniqueType, Cols: []string{"other_a", "other_b"}},
			},
			insertFields: []string{"id", "name", "other_a", "other_b"},
			uniqueFields: []string{"name"},
			want:         "name",
		},
		{
			name:         "没有唯一字段时不介入",
			indexes:      composite,
			insertFields: []string{"id", "name", "attribute_id"},
			uniqueFields: nil,
			want:         "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Join(expandToUniqueIndex(c.indexes, c.insertFields, c.uniqueFields), ",")
			if got != c.want {
				t.Errorf("得到 %q，期望 %q", got, c.want)
			}
		})
	}
}

// 多个唯一索引都可用时，必须每次选中同一个——否则同一模型在不同进程/不同次
// 运行会生成不同的 ON CONFLICT 目标，故障时好时坏、无从复现。
func TestExpandToUniqueIndex_Deterministic(t *testing.T) {
	indexes := map[string]*TIndex{
		"uq_b": {Name: "uq_b", Type: UniqueType, Cols: []string{"name", "b"}},
		"uq_a": {Name: "uq_a", Type: UniqueType, Cols: []string{"name", "a"}},
		"uq_c": {Name: "uq_c", Type: UniqueType, Cols: []string{"name", "c"}},
	}
	insert := []string{"id", "name", "a", "b", "c"}

	first := strings.Join(expandToUniqueIndex(indexes, insert, []string{"name"}), ",")
	for i := 0; i < 50; i++ {
		got := strings.Join(expandToUniqueIndex(indexes, insert, []string{"name"}), ",")
		if got != first {
			t.Fatalf("第 %d 次得到 %q，首次是 %q —— 选取不确定", i, got, first)
		}
	}
	if first != "name,a" {
		t.Errorf("应按索引名排序取首个（uq_a），实得 %q", first)
	}
}
