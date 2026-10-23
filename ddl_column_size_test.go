package orm

import "testing"

// 改成不限长（selection / 没写 size 的字符串）是放宽，不能被当成"改成零长"去告警；
// 收窄才不自动做。
func TestIsSizeWidening(t *testing.T) {
	cases := []struct {
		cur, want int
		widen     bool
	}{
		{16, 0, true},   // varchar(16) → selection（不限长）：2026-10-23 sys_tenant_archive.status
		{2048, 0, true}, // catalog_mfr.logo
		{16, 32, true},  // 加长
		{32, 16, false}, // 缩短
		{0, 64, false},  // 不限长 → 有长度：收窄
	}
	for _, c := range cases {
		if got := isSizeWidening(c.cur, c.want); got != c.widen {
			t.Errorf("isSizeWidening(%d, %d) = %v, want %v", c.cur, c.want, got, c.widen)
		}
	}
	if got := sqlTypeWithSize("VARCHAR", 0); got != "VARCHAR（不限长）" {
		t.Errorf("sqlTypeWithSize(VARCHAR, 0) = %q", got)
	}
}
