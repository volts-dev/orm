package orm

import (
	"context"
	"fmt"
	"testing"

	_ "github.com/lib/pq"
)

// batchProbeA/B 覆盖批量内省要保真的几类列：自增主键、varchar(带 unique 索引)、
// 定长/变长、整型、带默认值。
type batchProbeA struct {
	TModel `table:"name('batch_probe_a')"`
	Id     int64  `field:"pk autoincr"`
	Name   string `field:"varchar(64)"`
	Code   string `field:"varchar(32) unique"`
	Qty    int    `field:"int"`
}

type batchProbeB struct {
	TModel   `table:"name('batch_probe_b')"`
	Id       int64  `field:"pk autoincr"`
	Title    string `field:"varchar(128)"`
	Enabled  bool   `field:"bool"`
	ParentId int64  `field:"bigint"`
}

func newProbeOrm(t *testing.T) *TOrm {
	t.Helper()
	ds := pgDataSource(t, "test_orm")
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres unavailable, skipping batch-introspect equivalence: %v", err)
	}
	if _, err := o.SyncModel("test", new(batchProbeA), new(batchProbeB)); err != nil {
		t.Fatalf("SyncModel probes: %v", err)
	}
	return o
}

func fieldSig(f IField) string {
	st := f.SQLType()
	return fmt.Sprintf("name=%s type=%s/%d/%d default=%v required=%v pk=%v unique=%v autoincr=%v composite=%v",
		f.Name(), st.Name, st.DefaultLength, st.DefaultLength2,
		f.Default(), f.Required(), f.IsPrimaryKey(), f.IsUnique(), f.IsAutoIncrement(), f.IsCompositeKey())
}

// TestBatchIntrospectMatchesPerTable 证明 IBatchIntrospect(GetAllFields/GetAllIndexes)
// 对当前 schema 每张表产出的列序、字段属性、索引，与逐表 GetFields/GetIndexes 完全一致。
// 这是把 DBMetas 批量化的安全护栏：只要两条路装配一致,批量化就只是"更快地取同样的数"。
func TestBatchIntrospectMatchesPerTable(t *testing.T) {
	o := newProbeOrm(t)
	ctx := context.Background()
	pg, ok := o.dialect.(*postgres)
	if !ok {
		t.Fatalf("expected *postgres dialect, got %T", o.dialect)
	}

	models, err := pg.GetModels(ctx, nil)
	if err != nil {
		t.Fatalf("GetModels: %v", err)
	}

	batchSeq, batchFields, err := pg.GetAllFields(ctx, nil)
	if err != nil {
		t.Fatalf("GetAllFields: %v", err)
	}
	batchIdx, err := pg.GetAllIndexes(ctx, nil)
	if err != nil {
		t.Fatalf("GetAllIndexes: %v", err)
	}

	// 只对本用例受控、单 schema 的探针表做严格逐字段/逐索引比对。test_orm 里
	// 残留有 schema-isolation 测试建的同名表(跨 public/其它 schema)；逐表 GetFields
	// 曾因未钉住 pg_class 的 schema 把它们的列/主键重复计数(2026-10-19 已补 n.nspname)。
	// 批量版覆盖广度由整套 postgres 集成用例(现已全走批量路径)保证。
	probeTables := map[string]bool{"batch_probe_a": true, "batch_probe_b": true}

	checked := 0
	for _, model := range models {
		table := model.Table()
		if !probeTables[table] {
			continue
		}

		// --- 列：逐表基准 ---
		// 比对"去重后的字段集合 + 每字段签名 + 索引集合"；colSeq 不重复由
		// TestIntrospectOneRowPerColumn 单独钉住。
		// 逐表 GetFields 用 WithModel 绑定字段，其 Default() 走 boundModel.obj；
		// 复刻 _modelMetas 在取字段前先挂上 obj，否则读默认值会 nil 解引用。
		model.GetBase().obj = o.osv.newObject(model.String())
		_, wantFields, err := pg.GetFields(ctx, nil, model)
		if err != nil {
			t.Fatalf("GetFields(%s): %v", table, err)
		}

		gotFields := batchFields[table]
		if len(gotFields) != len(wantFields) {
			t.Fatalf("table %s: field count batch=%d per-table=%d", table, len(gotFields), len(wantFields))
		}
		for name, wf := range wantFields {
			gf, has := gotFields[name]
			if !has {
				t.Fatalf("table %s: batch missing field %q", table, name)
			}
			if fieldSig(gf) != fieldSig(wf) {
				t.Fatalf("table %s field %q mismatch:\n batch: %s\n perTb: %s", table, name, fieldSig(gf), fieldSig(wf))
			}
		}
		// colSeq(去重后)应与批量版逐一对应,且批量版本身无重复。
		seen := make(map[string]bool, len(batchSeq[table]))
		for _, c := range batchSeq[table] {
			if seen[c] {
				t.Fatalf("table %s: batch colSeq has duplicate %q", table, c)
			}
			seen[c] = true
			if _, has := gotFields[c]; !has {
				t.Fatalf("table %s: batch colSeq %q not in fields map", table, c)
			}
		}

		// --- 索引：逐表基准 ---
		wantIdx, err := pg.GetIndexes(ctx, nil, table)
		if err != nil {
			t.Fatalf("GetIndexes(%s): %v", table, err)
		}
		gotIdx := batchIdx[table]
		if len(gotIdx) != len(wantIdx) {
			t.Fatalf("table %s: index count batch=%d per-table=%d\n batch=%v\n perTb=%v",
				table, len(gotIdx), len(wantIdx), gotIdx, wantIdx)
		}
		for name, wi := range wantIdx {
			gi, has := gotIdx[name]
			if !has {
				t.Fatalf("table %s: batch missing index %q", table, name)
			}
			wsig := fmt.Sprintf("%s/%d/%v/%v", wi.Name, wi.Type, wi.Cols, wi.IsRegular)
			gsig := fmt.Sprintf("%s/%d/%v/%v", gi.Name, gi.Type, gi.Cols, gi.IsRegular)
			if wsig != gsig {
				t.Fatalf("table %s index %q mismatch:\n batch: %s\n perTb: %s", table, name, gsig, wsig)
			}
		}
		checked++
	}

	if checked < 2 {
		t.Fatalf("expected both probe tables checked, got %d — probe sync likely failed", checked)
	}
	t.Logf("batch/per-table equivalence verified across %d probe tables", checked)
}

// TestIntrospectOneRowPerColumn 钉住反查 SQL「每列只出一行」：id 同时在主键与一个复合唯一约束里，
// 旧写法（LEFT JOIN pg_constraint）给它出两行（PostgreSQL 18 起还有 NOT NULL 那一行），colSeq 里
// id 重复，fields 里留下哪一行看执行计划——PG 18 上大多数表留下的是 NOT NULL 那行，主键标记丢了，
// 反查出来的原型模型没有 IdKey。见 GetFields 的注释。
func TestIntrospectOneRowPerColumn(t *testing.T) {
	o := newProbeOrm(t)
	ctx := context.Background()
	pg := o.dialect.(*postgres)

	const table = "intro_multi_con_probe"
	if _, err := o.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
		t.Fatalf("drop: %v", err)
	}
	t.Cleanup(func() { o.Exec(`DROP TABLE IF EXISTS ` + table) })
	if _, err := o.Exec(`CREATE TABLE ` + table + ` (id BIGINT NOT NULL PRIMARY KEY, code VARCHAR(16) NOT NULL, UNIQUE (id, code))`); err != nil {
		t.Fatalf("create: %v", err)
	}

	check := func(path string, seq []string, fields map[string]IField) {
		t.Helper()
		seen := map[string]bool{}
		for _, c := range seq {
			if seen[c] {
				t.Fatalf("%s: colSeq 里 %q 出现了不止一次：%v", path, c, seq)
			}
			seen[c] = true
		}
		id := fields["id"]
		if id == nil || !id.IsPrimaryKey() || !id.IsUnique() || id.IsCompositeKey() {
			t.Fatalf("%s: id 应是单主键（pk、unique、非复合），得到 %v", path, id)
		}
		if code := fields["code"]; code == nil || !code.IsUnique() || code.IsPrimaryKey() {
			t.Fatalf("%s: code 在唯一约束里、不是主键，得到 %v", path, code)
		}
	}

	allSeq, allFields, err := pg.GetAllFields(ctx, nil)
	if err != nil {
		t.Fatalf("GetAllFields: %v", err)
	}
	check("GetAllFields", allSeq[table], allFields[table])

	models, err := pg.GetModels(ctx, nil)
	if err != nil {
		t.Fatalf("GetModels: %v", err)
	}
	for _, m := range models {
		if m.Table() != table {
			continue
		}
		m.GetBase().obj = o.osv.newObject(m.String())
		seq, fields, err := pg.GetFields(ctx, nil, m)
		if err != nil {
			t.Fatalf("GetFields: %v", err)
		}
		check("GetFields", seq, fields)
		return
	}
	t.Fatalf("GetModels 里没有 %s", table)
}
