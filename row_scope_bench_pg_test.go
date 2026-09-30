package orm

import "testing"

// 行范围的代价：Enforce 下自动提交的读要包一个隐式事务（BEGIN / SET LOCAL / 语句 /
// COMMIT，多三次往返）；显式事务里只在范围第一次出现时多一次。
//
//	go test -run '^$' -bench BenchmarkPG_RowScope -benchtime 3s .
func BenchmarkPG_RowScope(b *testing.B) {
	run := func(b *testing.B, o *TOrm, scoped bool) {
		s := o.NewSession()
		defer s.Close()
		if scoped {
			s.SetRowScope(42)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := s.Query("SELECT nspname FROM pg_catalog.pg_namespace WHERE oid = ?", 11); err != nil {
				b.Fatal(err)
			}
		}
	}
	off := pgPoolOrm(b, nil)
	enforce := pgPoolOrm(b, nil, WithRowScope(rowScopeSetting, RowScopeEnforce))

	b.Run("off", func(b *testing.B) { run(b, off, true) })
	b.Run("enforce-autocommit", func(b *testing.B) { run(b, enforce, true) })
	b.Run("enforce-in-tx", func(b *testing.B) {
		s := enforce.NewSession().SetRowScope(42)
		defer s.Close()
		if err := s.Begin(); err != nil {
			b.Fatal(err)
		}
		defer s.Rollback(nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := s.Query("SELECT nspname FROM pg_catalog.pg_namespace WHERE oid = ?", 11); err != nil {
				b.Fatal(err)
			}
		}
	})
}
