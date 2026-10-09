package orm

import (
	"fmt"
	"strings"
	"testing"
)

// holderOld 是改写前的实现（逐段 += 拼接），留作对照：两者输出必须逐字节相同。
func holderOld(sql, prefix string, start int) string {
	segs := strings.Split(sql, "?")
	res := ""
	for i, c := range segs {
		if i < len(segs)-1 {
			res += c + fmt.Sprintf("%s%v", prefix, i+start)
		}
	}
	return res + segs[len(segs)-1]
}

func TestHolderFmter_SameAsBefore(t *testing.T) {
	f := &HolderFmter{Prefix: "$", Start: 1}
	for _, sql := range []string{
		"", "SELECT 1", "?", "??", "a=? AND b=?", "x?", "?y", "INSERT INTO t VALUES (?,?),(?,?)",
		"WHERE name = '?' AND id = ?", // 字面量里的 ? 照旧也换（行为不变，只是更快）
	} {
		if got, want := f.Do(sql, nil, nil), holderOld(sql, "$", 1); got != want {
			t.Errorf("Do(%q) = %q, want %q", sql, got, want)
		}
	}
	big := "INSERT INTO t (a,b) VALUES " + strings.TrimSuffix(strings.Repeat("(?,?),", 3000), ",")
	if got, want := f.Do(big, nil, nil), holderOld(big, "$", 1); got != want {
		t.Fatal("big statement differs")
	}
}

// 成批写入一条语句几万个占位符：从前平方级，这里要线性。
func BenchmarkHolderFmter_BulkInsert(b *testing.B) {
	sql := "INSERT INTO t (a,b,c,d,e,f,g,h,i,j,k,l,m) VALUES " + strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?,?,?,?,?,?,?),", 2000), ",")
	f := &HolderFmter{Prefix: "$", Start: 1}
	for i := 0; i < b.N; i++ {
		f.Do(sql, nil, nil)
	}
}

func BenchmarkHolderFmter_BulkInsertOld(b *testing.B) {
	sql := "INSERT INTO t (a,b,c,d,e,f,g,h,i,j,k,l,m) VALUES " + strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?,?,?,?,?,?,?),", 2000), ",")
	for i := 0; i < b.N; i++ {
		holderOld(sql, "$", 1)
	}
}
