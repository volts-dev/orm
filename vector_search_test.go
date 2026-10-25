package orm

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/volts-dev/utils"
)

// 向量检索（vector_search.go）：没有向量运算的库上的精确扫描与暴力算出来的逐行一致；PG 上 pgvector 原生
// 检索与扫描逐行一致；带过滤条件走 HNSW 时凑得够 K 行（iterative_scan）。

type VecSearchRec struct {
	TModel `table:"name('vec_search_rec')"`
	Id     int64     `field:"pk autoincr"`
	Grp    int64     `field:"int()"`
	Emb    []float32 `field:"vector() size(8)"`
}

const vecSearchRows = 1200 // 大于两批（vectorScanBatch = 500）

type vecSearchRow struct {
	id, grp int64
	emb     []float32 // nil = NULL
}

// vecSearchData：50 组，每组 24 行；每 97 行一个 NULL、id 13 是零向量。
func vecSearchData() []vecSearchRow {
	r := rand.New(rand.NewSource(20261026))
	rows := make([]vecSearchRow, 0, vecSearchRows)
	for i := 1; i <= vecSearchRows; i++ {
		row := vecSearchRow{id: int64(i), grp: int64(i % 50)}
		switch {
		case i%97 == 0:
		case i == 13:
			row.emb = make([]float32, 8)
		default:
			row.emb = make([]float32, 8)
			for j := range row.emb {
				row.emb[j] = float32(r.NormFloat64())
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func loadVecSearch(t *testing.T, o *TOrm) []vecSearchRow {
	t.Helper()
	if _, err := o.SyncModel("", new(VecSearchRec)); err != nil {
		t.Fatalf("SyncModel: %v", err)
	}
	rows := vecSearchData()
	for start := 0; start < len(rows); start += 200 {
		end := min(start+200, len(rows))
		var vals []string
		var args []any
		for _, r := range rows[start:end] {
			vals = append(vals, "(?, ?, ?)")
			var emb any
			if r.emb != nil {
				emb = FormatVector(r.emb)
			}
			args = append(args, r.id, r.grp, emb)
		}
		if _, err := o.Exec("INSERT INTO vec_search_rec (id, grp, emb) VALUES "+strings.Join(vals, ", "), args...); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	return rows
}

// bruteNearest 是测试自己的暴力解：逐行算 1 − cos，NaN 排最后，同距离按 id。
func bruteNearest(rows []vecSearchRow, q []float32, k int, keep func(vecSearchRow) bool) (ids []int64, dists []float64) {
	type hit struct {
		id int64
		d  float64
	}
	var hs []hit
	for _, r := range rows {
		if r.emb == nil || !keep(r) {
			continue
		}
		var dot, na, nb float64
		for i := range q {
			dot += float64(q[i]) * float64(r.emb[i])
			na += float64(q[i]) * float64(q[i])
			nb += float64(r.emb[i]) * float64(r.emb[i])
		}
		d := math.NaN()
		if na > 0 && nb > 0 {
			d = 1 - math.Max(-1, math.Min(1, dot/math.Sqrt(na*nb)))
		}
		hs = append(hs, hit{r.id, d})
	}
	sort.SliceStable(hs, func(i, j int) bool {
		a, b := hs[i].d, hs[j].d
		if math.IsNaN(a) != math.IsNaN(b) {
			return math.IsNaN(b)
		}
		if a != b {
			return a < b
		}
		return hs[i].id < hs[j].id
	})
	if len(hs) > k {
		hs = hs[:k]
	}
	for _, h := range hs {
		ids = append(ids, h.id)
		dists = append(dists, h.d)
	}
	return
}

func hitIds(hits []VectorHit) []int64 {
	var ids []int64
	for _, h := range hits {
		ids = append(ids, utils.ToInt64(h.Row["id"]))
	}
	return ids
}

type vecSearchCase struct {
	name  string
	where string
	args  []any
	k     int
	keep  func(vecSearchRow) bool
}

func vecSearchCases() []vecSearchCase {
	all := func(vecSearchRow) bool { return true }
	return []vecSearchCase{
		{"不带过滤", "", nil, 7, all},
		{"按组过滤", "grp = ?", []any{3}, 5, func(r vecSearchRow) bool { return r.grp == 3 }},
		{"K 大于命中行数", "grp = ?", []any{13}, 40, func(r vecSearchRow) bool { return r.grp == 13 }}, // 含零向量 id 13
		{"零命中", "grp = ?", []any{99}, 5, func(r vecSearchRow) bool { return false }},
	}
}

var vecSearchQuery = []float32{0.3, -1.2, 0.5, 0.9, -0.1, 0.0, 2.0, -0.7}

func checkHits(t *testing.T, got []VectorHit, wantIds []int64, wantDists []float64, tol float64) {
	t.Helper()
	if fmt.Sprint(hitIds(got)) != fmt.Sprint(wantIds) {
		t.Fatalf("命中 %v\nwant %v", hitIds(got), wantIds)
	}
	for i, h := range got {
		w := wantDists[i]
		if math.IsNaN(w) != math.IsNaN(h.Distance) || (!math.IsNaN(w) && math.Abs(w-h.Distance) > tol) {
			t.Errorf("第 %d 行（id %d）距离 %v，want %v", i, wantIds[i], h.Distance, w)
		}
		for _, k := range []string{vectorValueAlias, vectorKeyAlias, vectorDistanceAlias} {
			if _, ok := h.Row[k]; ok {
				t.Errorf("行里留着内部列 %s", k)
			}
		}
	}
}

func TestNearestByCosine_ScanMatchesBruteForce(t *testing.T) {
	skipUnlessDialect(t, "mysql", "sqlite") // PG 要 pgvector，见 TestNearestByCosine_PgvectorMatchesScan
	o, err := New(WithDataSource(testDataSource(t, filepath.Join(t.TempDir(), "vecsearch.db"))))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	rows := loadVecSearch(t, o)
	if o.VectorIndexSql("vec_search_rec", "vec_search_rec_hnsw", "emb") != "" {
		t.Errorf("没有向量运算的库上不该给出向量索引语句")
	}
	for _, c := range vecSearchCases() {
		t.Run(c.name, func(t *testing.T) {
			s := o.NewSession()
			defer s.Close()
			got, err := s.NearestByCosine(VectorQuery{From: "vec_search_rec", Where: c.where, Args: c.args,
				Select: "id AS id, grp AS grp", Column: "emb", Key: "id", Vector: vecSearchQuery, K: c.k})
			if err != nil {
				t.Fatal(err)
			}
			ids, dists := bruteNearest(rows, vecSearchQuery, c.k, c.keep)
			checkHits(t, got, ids, dists, 1e-9)
		})
	}
	s := o.NewSession()
	defer s.Close()
	if _, err := s.NearestByCosine(VectorQuery{From: "vec_search_rec", Select: "id AS id", Column: "emb", Key: "id",
		Vector: []float32{1, 2}, K: 3}); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Errorf("维数不同应报错（与 pgvector 一致），得 %v", err)
	}
}

// PG 上要 pgvector：本机 5432（不设 ORM_TEST_DB）有；docker 里的测试 PG 没装，跳过。
func TestNearestByCosine_PgvectorMatchesScan(t *testing.T) {
	// 先用一个实例清场再起被测实例：New 时反查进来的上一轮 HNSW 索引（USING hnsw … vector_cosine_ops）
	// 会在表删掉后被 SyncModel 当成要重建的索引。
	ds := pgDataSource(t, "test_orm")
	pre, err := New(WithDataSource(ds))
	if err != nil {
		t.Skipf("postgres: %v", err)
	}
	if _, err := pre.Exec("CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		pre.Close()
		t.Skipf("没有 pgvector: %v", err)
	}
	if _, err := pre.Exec("DROP TABLE IF EXISTS vec_search_rec"); err != nil {
		t.Fatal(err)
	}
	pre.Close()
	o, err := New(WithDataSource(ds))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	rows := loadVecSearch(t, o)

	run := func(s *TSession, c vecSearchCase, ef int) []VectorHit {
		t.Helper()
		got, err := s.NearestByCosine(VectorQuery{From: "vec_search_rec", Where: c.where, Args: c.args,
			Select: "id AS id, grp AS grp", Column: "emb", Key: "id", Vector: vecSearchQuery, K: c.k, EfSearch: ef})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// 没有索引：两条路都是精确的，逐行一致（pgvector 用 float 累加，距离差在 1e-6 量级）。
	for _, c := range vecSearchCases() {
		t.Run("精确/"+c.name, func(t *testing.T) {
			s := o.NewSession()
			defer s.Close()
			native := run(s, c, 0)
			scan, err := s.nearestScan(VectorQuery{From: "vec_search_rec", Where: c.where, Args: c.args,
				Select: "id AS id, grp AS grp", Column: "emb", Key: "id", Vector: vecSearchQuery, K: c.k})
			if err != nil {
				t.Fatal(err)
			}
			ids, dists := bruteNearest(rows, vecSearchQuery, c.k, c.keep)
			checkHits(t, scan, ids, dists, 1e-9)
			checkHits(t, native, ids, dists, 1e-5)
		})
	}

	// 有 HNSW、带一个只筛剩 2% 的条件：ef_search 默认 40 个候选里平均不到一个满足条件，不开 iterative_scan
	// 就凑不够 K 行。强制走索引（enable_seqscan = off），检索借用这个事务。
	idx := o.VectorIndexSql("vec_search_rec", "vec_search_rec_hnsw", "emb")
	if !strings.Contains(idx, "hnsw") {
		t.Fatalf("PG 上的向量索引应是 HNSW：%q", idx)
	}
	if _, err := o.Exec(idx); err != nil {
		t.Fatal(err)
	}
	s := o.NewSession()
	defer s.Close()
	if !o.dialect.(*postgres).pgvectorIterativeScan(s) {
		t.Skip("pgvector < 0.8，没有 iterative_scan")
	}
	if err := s.Begin(); err != nil {
		t.Fatal(err)
	}
	defer s.Rollback(nil)
	if _, err := s.Exec("SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	c := vecSearchCases()[1]
	got := run(s, c, 0)
	ids, _ := bruteNearest(rows, vecSearchQuery, c.k, c.keep)
	if len(got) != c.k {
		t.Fatalf("走 HNSW 带过滤只取回 %d 行（应为 %d）：iterative_scan 没生效", len(got), c.k)
	}
	same := 0
	for _, id := range hitIds(got) {
		for _, w := range ids {
			if id == w {
				same++
			}
		}
	}
	if same < c.k-1 {
		t.Errorf("召回 %d/%d：%v vs 精确 %v", same, c.k, hitIds(got), ids)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Distance < got[i-1].Distance {
			t.Errorf("relaxed_order 的结果没有按距离重排：%v", got)
		}
	}
}

func TestCosineDistance_PgvectorSemantics(t *testing.T) {
	cases := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0}, []float32{1, 0}, 0},
		{[]float32{1, 0}, []float32{0, 1}, 1},
		{[]float32{1, 0}, []float32{-1, 0}, 2},
		{[]float32{1, 1}, []float32{2, 2}, 0}, // 只看方向
		{[]float32{3, 4}, []float32{4, 3}, 1 - 24.0/25},
	}
	for _, c := range cases {
		got, err := cosineDistance(c.a, c.b)
		if err != nil || math.Abs(got-c.want) > 1e-12 {
			t.Errorf("cosineDistance(%v, %v) = %v, %v；want %v", c.a, c.b, got, err, c.want)
		}
	}
	if d, _ := cosineDistance([]float32{0, 0}, []float32{1, 0}); !math.IsNaN(d) {
		t.Errorf("零向量应得 NaN（pgvector 同），得 %v", d)
	}
	if _, err := cosineDistance([]float32{1}, []float32{1, 0}); err == nil {
		t.Errorf("维数不同应报错")
	}
}

func TestKeepNearest_OrderAndNaN(t *testing.T) {
	var top []VectorHit
	for i, d := range []float64{0.5, math.NaN(), 0.1, 0.9, 0.3, math.NaN(), 0.2} {
		top = keepNearest(top, VectorHit{Row: map[string]any{"i": i}, Distance: d}, 4)
	}
	var got []string
	for _, h := range top {
		got = append(got, fmt.Sprint(h.Distance))
	}
	if strings.Join(got, " ") != "0.1 0.2 0.3 0.5" {
		t.Errorf("前 4 个 = %v", got)
	}
	top = nil
	for _, d := range []float64{math.NaN(), 0.4} {
		top = keepNearest(top, VectorHit{Distance: d}, 3)
	}
	if len(top) != 2 || top[0].Distance != 0.4 || !math.IsNaN(top[1].Distance) {
		t.Errorf("NaN 应排在最后：%v", top)
	}
}

func TestVectorQuery_Validate(t *testing.T) {
	ok := VectorQuery{From: "t", Select: "id", Column: "v", Key: "id", Vector: []float32{1}, K: 1}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*VectorQuery){
		"From": func(q *VectorQuery) { q.From = "" }, "Select": func(q *VectorQuery) { q.Select = " " },
		"Column": func(q *VectorQuery) { q.Column = "" }, "Key": func(q *VectorQuery) { q.Key = "" },
		"Vector": func(q *VectorQuery) { q.Vector = nil }, "K": func(q *VectorQuery) { q.K = 0 },
	} {
		q := ok
		mut(&q)
		if q.validate() == nil {
			t.Errorf("缺 %s 应报错", name)
		}
	}
}

// iterative_scan 从 pgvector 0.8.0 起才有；判错成「有」会让 SET 报错、整个检索事务作废，判错成「没有」则
// 带过滤的检索静默漏召回。（原在 ai 模块，随检索挪进 orm。）
func TestExtVersionAtLeast(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"0.8.0", true},
		{"0.8.1", true},
		{"0.10.0", true}, // 按数值比，不按字符串比
		{"1.0", true},
		{"0.7.4", false},
		{"0.5.1", false},
		{" 0.8.0 ", true},
		{"", false},
		{"0", false},
		{"x.y", false},
	}
	for _, c := range cases {
		if got := extVersionAtLeast(c.v, 0, 8); got != c.want {
			t.Errorf("extVersionAtLeast(%q, 0, 8) = %v，想要 %v", c.v, got, c.want)
		}
	}
}
