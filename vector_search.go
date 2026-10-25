package orm

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

// 向量检索：按余弦距离取最近的 K 行（MySQL 兼容 P6，2026-10-26）。
//
// 两条路，结果一样（同一份数据、同一个 K，两边取回的是同样的行、同样的距离，用例逐行比过）：
//
//   - 库里有向量运算（Features().Vector，即 PG + pgvector）：`ORDER BY col <=> ? LIMIT K`，走 HNSW 索引。
//     pgvector ≥ 0.8 时先在同一事务里 `SET LOCAL hnsw.iterative_scan = relaxed_order`——带过滤条件时，
//     索引扫出来的前 ef_search 个候选被条件筛掉大半，不开它就凑不够 K 行、一声不响地漏召回（按租户过滤的
//     合成数据上 23.2% → 99.8%）。relaxed_order 不保证顺序，结果在这里再按距离排一次。
//   - 没有（MySQL 8.0：没有向量类型，列落 JSON；SQLite）：按 Key 分批扫过滤后的行，在 Go 里算距离、只留
//     前 K 个。精确检索，代价是过滤后行数 × 维数——agent 的知识库是几百到几千块，一次一两千万次乘加；
//     几十万行以上要换带向量索引的库（MySQL 9 的 VECTOR、HeatWave，或者 PG）。
//
// 距离的口径与 pgvector 的 `<=>` 一致：1 − 余弦相似度，相似度截在 [−1, 1]；有一边是零向量得 NaN、排在最后；
// 维数不同报错。

// VectorQuery 是一次「按余弦距离取最近的 K 行」。From / Where / Select / Column / Key 都是 SQL 片段，
// 原样拼进语句，调用方负责限定 schema、加引号——**不要把外部输入拼进来**，值走 Args。
type VectorQuery struct {
	From   string // FROM 之后的部分：表与 JOIN，例如 `ai_embedding e JOIN ai_agent_source s ON …`
	Where  string // 过滤条件，? 占位，可空
	Args   []any  // Where 的参数
	Select string // 要取回的列（SELECT 列表），例如 `e.content AS content, s.name AS source_name`
	Column string // 向量列，例如 `e.embedding_vector`；值为 NULL 的行不参与
	// Key 是一列唯一、可比大小的列（例如 `e.id`）：没有向量运算的库上按它分批扫。有向量运算的库上不用，
	// 照样必填——免得只在 MySQL 上才报错。
	Key    string
	Vector []float32
	K      int
	// EfSearch 是 HNSW 一次搜索保留的候选数（pgvector 的 hnsw.ef_search，默认 40）：越大召回越高、越慢。
	// 0 = 不设。只在 iterative_scan 可用（pgvector ≥ 0.8）时才设，与 iterative_scan 一起生效。
	EfSearch int
}

// VectorHit 是一行命中：Row 是 Select 里的列，Distance 是余弦距离（越小越近）。
type VectorHit struct {
	Row      map[string]any
	Distance float64
}

// vectorSearcher：库里有向量运算的方言（PG + pgvector）。
type vectorSearcher interface {
	// cosineDistanceSql 是 column 与查询向量的余弦距离表达式，含一个 ? 占位（查询向量的文本）。
	cosineDistanceSql(column string) string
	// vectorSearchSettings 是检索前要在同一事务里发的设置语句；空 = 不必开事务。
	vectorSearchSettings(s *TSession, efSearch int) []string
	// vectorIndexSql 建余弦距离的向量索引（幂等）。
	vectorIndexSql(table, name, column string) string
}

// 没有向量运算的库上一批扫多少行。1536 维的 JSON 一行约 20KB，一批约 10MB。
const vectorScanBatch = 500

const (
	vectorDistanceAlias = "orm_distance"
	vectorValueAlias    = "orm_vec"
	vectorKeyAlias      = "orm_key"
)

func (q *VectorQuery) validate() error {
	switch {
	case strings.TrimSpace(q.From) == "":
		return errors.New("NearestByCosine: From is empty")
	case strings.TrimSpace(q.Select) == "":
		return errors.New("NearestByCosine: Select is empty")
	case strings.TrimSpace(q.Column) == "":
		return errors.New("NearestByCosine: Column is empty")
	case strings.TrimSpace(q.Key) == "":
		return errors.New("NearestByCosine: Key is empty (needed to scan on databases without vector operators)")
	case len(q.Vector) == 0:
		return errors.New("NearestByCosine: query vector is empty")
	case q.K <= 0:
		return fmt.Errorf("NearestByCosine: K must be positive, got %d", q.K)
	}
	return nil
}

func (q *VectorQuery) where() string {
	w := q.Column + " IS NOT NULL"
	if strings.TrimSpace(q.Where) != "" {
		w = "(" + q.Where + ") AND " + w
	}
	return w
}

// NearestByCosine 按余弦距离取最近的 q.K 行，按距离升序。见文件头。
//
// PG 上要发 SET LOCAL 时：会话不在事务里就开一个、用完回滚（只读）；已经在事务里就借用它，设置留到那个
// 事务结束（只影响之后的向量索引扫描）。
func (self *TSession) NearestByCosine(q VectorQuery) ([]VectorHit, error) {
	if self.IsAutoClose {
		defer self.Close()
	}
	if err := q.validate(); err != nil {
		return nil, err
	}
	if vs, ok := self.orm.dialect.(vectorSearcher); ok && self.orm.dialect.Features().Vector {
		return self.nearestNative(vs, q)
	}
	return self.nearestScan(q)
}

func (self *TSession) nearestNative(vs vectorSearcher, q VectorQuery) ([]VectorHit, error) {
	if stmts := vs.vectorSearchSettings(self, q.EfSearch); len(stmts) > 0 {
		if self.IsAutoCommit {
			if err := self.Begin(); err != nil {
				return nil, err
			}
			defer self.Rollback(nil)
		}
		for _, st := range stmts {
			if _, err := self._exec(st); err != nil {
				return nil, err
			}
		}
	}
	lit := FormatVector(q.Vector)
	dist := vs.cosineDistanceSql(q.Column)
	sqlStr := fmt.Sprintf("SELECT %s, %s AS %s FROM %s WHERE %s ORDER BY %s LIMIT ?",
		q.Select, dist, vectorDistanceAlias, q.From, q.where(), dist)
	args := make([]any, 0, len(q.Args)+3)
	args = append(args, lit)
	args = append(args, q.Args...)
	args = append(args, lit, q.K)
	ds, err := self._query(sqlStr, args...)
	if err != nil {
		return nil, err
	}
	var hits []VectorHit
	if ds != nil {
		ds.Range(func(_ int, rec *dataset.TRecordSet) error {
			row := rec.AsMap()
			d := vectorDistanceValue(row[vectorDistanceAlias])
			delete(row, vectorDistanceAlias)
			hits = append(hits, VectorHit{Row: row, Distance: d})
			return nil
		})
	}
	sortVectorHits(hits)
	return hits, nil
}

func (self *TSession) nearestScan(q VectorQuery) ([]VectorHit, error) {
	top := make([]VectorHit, 0, q.K+1)
	var last any
	for {
		where, args := q.where(), append([]any(nil), q.Args...)
		if last != nil {
			where += " AND " + q.Key + " > ?"
			args = append(args, last)
		}
		sqlStr := fmt.Sprintf("SELECT %s, %s AS %s, %s AS %s FROM %s WHERE %s ORDER BY %s LIMIT %d",
			q.Select, q.Column, vectorValueAlias, q.Key, vectorKeyAlias, q.From, where, q.Key, vectorScanBatch)
		ds, err := self._query(sqlStr, args...)
		if err != nil {
			return nil, err
		}
		n := 0
		if ds != nil {
			err = ds.Range(func(_ int, rec *dataset.TRecordSet) error {
				n++
				row := rec.AsMap()
				last = row[vectorKeyAlias]
				v, ok := ParseVector(row[vectorValueAlias]).([]float32)
				if !ok {
					return fmt.Errorf("NearestByCosine: %s of row %v is not a vector", q.Column, last)
				}
				d, err := cosineDistance(q.Vector, v)
				if err != nil {
					return err
				}
				delete(row, vectorValueAlias)
				delete(row, vectorKeyAlias)
				top = keepNearest(top, VectorHit{Row: row, Distance: d}, q.K)
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		if n < vectorScanBatch || last == nil {
			break
		}
	}
	return top, nil
}

// keepNearest 把 h 插进按距离升序、最多 k 个的 top。
func keepNearest(top []VectorHit, h VectorHit, k int) []VectorHit {
	if len(top) == k && !vectorLess(h.Distance, top[k-1].Distance) {
		return top
	}
	i := sort.Search(len(top), func(i int) bool { return vectorLess(h.Distance, top[i].Distance) })
	top = append(top, VectorHit{})
	copy(top[i+1:], top[i:])
	top[i] = h
	if len(top) > k {
		top = top[:k]
	}
	return top
}

// vectorLess 是距离的序：NaN 最大（与 PG 的 float8 排序一致）。
func vectorLess(a, b float64) bool {
	switch {
	case math.IsNaN(a):
		return false
	case math.IsNaN(b):
		return true
	}
	return a < b
}

func sortVectorHits(hits []VectorHit) {
	sort.SliceStable(hits, func(i, j int) bool { return vectorLess(hits[i].Distance, hits[j].Distance) })
}

func vectorDistanceValue(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case []byte:
		v = string(x)
	}
	if s, ok := v.(string); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return f
		}
	}
	return math.NaN()
}

// cosineDistance 是 pgvector `<=>` 的口径：1 − a·b / (|a||b|)，相似度截在 [−1, 1]，零向量得 NaN。
func cosineDistance(a, b []float32) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("different vector dimensions %d and %d", len(a), len(b))
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return math.NaN(), nil
	}
	sim := dot / math.Sqrt(na*nb)
	if sim > 1 {
		sim = 1
	} else if sim < -1 {
		sim = -1
	}
	return 1 - sim, nil
}

// VectorIndexSql 是在 table 的向量列 column 上建余弦距离索引的语句（幂等），名字 name。库里没有向量索引
// （MySQL 8.0、SQLite）时返回空串——那里检索是精确扫描，不需要索引。table 原样拼进语句，调用方负责限定与引号。
//
// PG 上是 HNSW，不是 ivfflat：ivfflat 的聚类中心取自建索引那一刻的数据，在空表上建（模块安装时多半如此）
// 召回只剩一两成；HNSW 没有训练步骤。
func (self *TOrm) VectorIndexSql(table, name, column string) string {
	if vs, ok := self.dialect.(vectorSearcher); ok && self.dialect.Features().Vector {
		return vs.vectorIndexSql(table, name, column)
	}
	return ""
}

// —— PG + pgvector ————————————————————————————————————————————————————————————————————

func (db *postgres) cosineDistanceSql(column string) string {
	return "(" + column + " <=> ?::vector)"
}

func (db *postgres) vectorIndexSql(table, name, column string) string {
	return fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (%s vector_cosine_ops)", name, table, column)
}

func (db *postgres) vectorSearchSettings(s *TSession, efSearch int) []string {
	if !db.pgvectorIterativeScan(s) {
		return nil
	}
	stmts := []string{"SET LOCAL hnsw.iterative_scan = relaxed_order"}
	if efSearch > 0 {
		stmts = append(stmts, "SET LOCAL hnsw.ef_search = "+strconv.Itoa(efSearch))
	}
	return stmts
}

// pgvectorIterativeScan 报告 pgvector 是否 ≥ 0.8（iterative_scan 从 0.8.0 起才有；更老的版本 SET 这个参数
// 会报错，把整个事务带进 aborted 状态）。只缓存查成功的结果——一次查询出错就永久当成「没有」，检索会一声
// 不响地退回漏召回的那种。
func (db *postgres) pgvectorIterativeScan(s *TSession) bool {
	switch db.iterativeScan.Load() {
	case 1:
		return true
	case 2:
		return false
	}
	ds, err := s._query(`SELECT extversion FROM pg_extension WHERE extname = 'vector'`)
	if err != nil || ds == nil || ds.Count() == 0 {
		return false
	}
	ok := extVersionAtLeast(utils.ToString(ds.Record().GetByField("extversion")), 0, 8)
	if ok {
		db.iterativeScan.Store(1)
	} else {
		db.iterativeScan.Store(2)
	}
	return ok
}

// extVersionAtLeast 比较 "0.8.1" 这类扩展版本号的前两段（按数值，不按字符串）。
func extVersionAtLeast(v string, major, minor int) bool {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 3)
	if len(parts) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}
