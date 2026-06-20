package cacher

import (
	"sync"
	"testing"
)

// TestRace_StatusMap_AUDIT 证明 status map 在 SetStatus(写) 与 GetBySql(读) 之间
// 无锁并发访问，触发 race detector / concurrent map read+write。
// 对应审计清单 P0-#2 (cacher/cacher.go:204,229)。
func TestRace_StatusMap_AUDIT(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}

	const table = "user"
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				c.SetStatus(true, table) // 无锁写 status map
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = c.GetBySql(table, "SELECT 1", nil) // 无锁读 status map
			}
		}()
	}
	wg.Wait()
}

// TestRace_TTL_AUDIT 证明 ttl 字段在 SetTTL(写) 与 GetTTL(读) 之间无锁并发。
// 对应审计清单 P2-#13 (cacher/cacher.go:177,190)。
func TestRace_TTL_AUDIT(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				c.SetTTL(120) // 无锁写 ttl
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = c.GetTTL() // 无锁读 ttl
			}
		}()
	}
	wg.Wait()
}
