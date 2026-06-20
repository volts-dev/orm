package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestScanStructByName_ExtraColumnNoPanic 回归测试 new-correctness-2：
// 当结果集存在 struct 没有对应字段的列时，ScanStructByName 必须走 EmptyScanner
// 兜底而不是 panic。修复前 fieldByName 对未命中列返回 reflect.Zero(structType)，
// 其 IsValid()=true 但不可寻址，导致 f.Addr() panic。
func TestScanStructByName_ExtraColumnNoPanic(t *testing.T) {
	db, err := Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO t (id, name) VALUES (1, 'a')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT id, name, 99 AS extra FROM t`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type Row struct {
		Id   int64
		Name string
		// 故意没有 Extra 字段
	}
	if !rows.Next() {
		t.Fatal("expected a row")
	}
	var r Row
	if err := rows.ScanStructByName(&r); err != nil { // 修复前此处 panic
		t.Fatalf("ScanStructByName: %v", err)
	}
	if r.Id != 1 || r.Name != "a" {
		t.Fatalf("unexpected scan result: %+v", r)
	}
}

// TestScanMap_ConcurrentNoRace 回归测试 new-concurrency-3：
// 多个 goroutine 共用同一 *DB 并发 ScanMap 时不得发生 data race。修复前 ScanMap 通过
// 挂在共享 *DB 上的环形缓冲（reflectNew）为每列取地址，并发 Scan 写入同一底层数组。
// 用临时文件库以保证多连接共享同一数据库。
func TestScanMap_ConcurrentNoRace(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "race.db")
	db, err := Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(8)

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 1; i <= 20; i++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO t (id, name) VALUES (?, ?)`, i, fmt.Sprintf("n%d", i)); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// 宽列查询：每次 ScanMap 抓取大量槽位（共享环形缓冲只有 200 槽），
	// 多 goroutine 同时在飞 + 强制回绕，使旧实现的并发写竞态必现。
	const cols = 64
	parts := make([]string, cols)
	for i := range parts {
		parts[i] = fmt.Sprintf("id AS c%d", i)
	}
	query := "SELECT " + strings.Join(parts, ", ") + " FROM t"

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 50; k++ {
				rows, err := db.QueryContext(ctx, query)
				if err != nil {
					continue
				}
				for rows.Next() {
					m := map[string]any{}
					if err := rows.ScanMap(&m); err != nil {
						break
					}
				}
				rows.Close()
			}
		}()
	}
	wg.Wait()
}
