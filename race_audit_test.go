package orm

import (
	"strconv"
	"sync"
	"testing"
)

// TestRace_SessionSets_AUDIT 证明单个 *TSession 在 goroutine 间共享时，
// SetMustFieldValue 对 self.Sets map 的写入无锁保护。
// 对应审计清单 P0-#4 (session.go:219-222)。
func TestRace_SessionSets_AUDIT(t *testing.T) {
	o := setupIntegrationOrm(t)
	sess := o.Model("bench.model") // 单个共享 session

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				sess.SetMustFieldValue("k"+strconv.Itoa(i), j, true) // 无锁写 self.Sets
			}
		}()
	}
	wg.Wait()
}
