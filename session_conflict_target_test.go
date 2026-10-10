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
			// 复合唯一索引的列没有一列带 field 级 unique 标志，调用方收集到的
			// uniqueFields 必然为空；此前这里「不介入」，冲突目标回落到雪花 id，
			// ON CONFLICT 永远不触发——sys_model_data 的 (tenant_id, module, name)
			// 就是这样让失败的模块安装一重试就撞 23505 的。
			name:         "没有唯一字段、但有被本次 INSERT 完整覆盖的唯一索引时选它",
			indexes:      composite,
			insertFields: []string{"id", "name", "attribute_id"},
			uniqueFields: nil,
			want:         "name,attribute_id",
		},
		{
			name:         "没有唯一字段、唯一索引又没被完整覆盖时仍不介入",
			indexes:      composite,
			insertFields: []string{"id", "name"},
			uniqueFields: nil,
			want:         "",
		},
		{
			name: "没有唯一字段、只有非唯一索引时不介入",
			indexes: map[string]*TIndex{
				"idx_name": {Name: "idx_name", Type: IndexType, Cols: []string{"name", "attribute_id"}},
			},
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

// group_operator tag 的白名单在**注册期**校验：算子最终作为字面量拼进 SQL，
// 早失败能在模块加载阶段暴露拼写错误，而不是等某张报表打不开才发现。
func TestTagGroupOperator(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"'avg'", "AVG", false},
		{"'SUM'", "SUM", false},
		{"min", "MIN", false}, // 不带引号也接受
		{"'MAX'", "MAX", false},
		{"'count'", "COUNT", false},
		{"'median'", "", true},            // 不在白名单
		{"'; DROP TABLE x; --", "", true}, // 注入尝试必须被拒
	}
	for _, c := range cases {
		f := &TField{}
		f.name = "amount"
		err := tag_group_operator(&TTagContext{Field: f, Params: []string{c.in}})
		if c.wantErr {
			if err == nil {
				t.Errorf("group_operator(%q) 应被拒绝，却通过了", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("group_operator(%q) 意外报错: %v", c.in, err)
			continue
		}
		if got := f.GroupOperator(); got != c.want {
			t.Errorf("group_operator(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// 不写 tag 时保持空串，由 ReadGroup 回落成 SUM——不能默认就写死 SUM，
// 否则「未指定」和「显式指定 SUM」无从区分。
func TestTagGroupOperator_EmptyParams(t *testing.T) {
	f := &TField{}
	if err := tag_group_operator(&TTagContext{Field: f}); err != nil {
		t.Fatal(err)
	}
	if got := f.GroupOperator(); got != "" {
		t.Errorf("未指定时应为空串，实得 %q", got)
	}
}

// group_operator 要随字段元数据下发（小写，同 Odoo 的 aggregator）：电子表格透视表按它合度量，
// 不下发时评分这类 avg 列被当成求和（eLearning 看板「点评」显示 20/5）。没写 tag 就不带这个键。
func TestFieldAttributes_GroupOperator(t *testing.T) {
	f := &TField{}
	f.name = "rating"
	if err := tag_group_operator(&TTagContext{Field: f, Params: []string{"'avg'"}}); err != nil {
		t.Fatal(err)
	}
	if got := f.attributes()["group_operator"]; got != "avg" {
		t.Errorf("attributes()[group_operator] = %v，期望 avg", got)
	}
	if _, ok := (&TField{}).attributes()["group_operator"]; ok {
		t.Error("没写 group_operator 时不该下发这个键")
	}
}

// 部分索引（带 WHERE 谓词）不能当 ON CONFLICT 的目标：Postgres 只在语句里把同一个
// 谓词重复一遍时才认它，光给列名报 42P10。
//
// 真栈战绩（2026-09-20）：ai 模块在 sys_action 上建了
// `UNIQUE (tenant_id, ai_tool_name) WHERE use_in_ai IS TRUE`（照抄 Odoo），
// 于是**整个模块装不上**，而错误文案既不提索引名也不提"部分索引"，看着像 XML 写错了。
func TestExpandToUniqueIndex_SkipsPartialIndex(t *testing.T) {
	insert := []string{"id", "tenant_id", "ai_tool_name", "name"}

	partial := map[string]*TIndex{
		"ir_act_server_ai_tool_name_unique": {
			Name: "ir_act_server_ai_tool_name_unique", Type: UniqueType,
			Cols: []string{"tenant_id", "ai_tool_name"}, Where: "use_in_ai IS TRUE",
		},
	}
	// 唯一可选的索引是部分索引 → 不介入，原样返回（上层回落到主键）。
	if got := strings.Join(expandToUniqueIndex(partial, insert, nil), ","); got != "" {
		t.Fatalf("部分索引不该被选作冲突目标，却拿到 %q", got)
	}
	// 调用方识别出了唯一字段时也一样：宁可回落到那些字段，也不能指向部分索引。
	if got := strings.Join(expandToUniqueIndex(partial, insert, []string{"ai_tool_name"}), ","); got != "ai_tool_name" {
		t.Fatalf("应原样返回入参，却拿到 %q", got)
	}

	// 同一张表上另有一个全表唯一索引时，选它。
	both := map[string]*TIndex{
		"ir_act_server_ai_tool_name_unique": partial["ir_act_server_ai_tool_name_unique"],
		"zz_full_unique": {
			Name: "zz_full_unique", Type: UniqueType, Cols: []string{"tenant_id", "name"},
		},
	}
	if got := strings.Join(expandToUniqueIndex(both, insert, nil), ","); got != "tenant_id,name" {
		t.Fatalf("应选中全表唯一索引，却拿到 %q", got)
	}
}
