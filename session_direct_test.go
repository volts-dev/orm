package orm

import "testing"

func TestDirectReadBypassesResultCache(t *testing.T) {
	for _, kind := range []string{"read", "search", "count"} {
		t.Run(kind, func(t *testing.T) {
			o := newCacheOrm(t)
			defer o.Close()
			read := func(direct bool) int {
				s := o.NewSession()
				defer s.Close()
				s.Model("cache.model").Limit(-1)
				if direct {
					s.Direct()
				}
				switch kind {
				case "read":
					ds, err := s.Read()
					if err != nil {
						t.Fatal(err)
					}
					return ds.Count()
				case "search":
					ids, _, err := s.Search()
					if err != nil {
						t.Fatal(err)
					}
					return len(ids)
				default:
					n, err := s.Count()
					if err != nil {
						t.Fatal(err)
					}
					return n
				}
			}
			if read(false) != 1 {
				t.Fatal("fixture")
			}
			// External writes leave ORM's optional query cache stale.
			if _, err := o.Exec("DELETE FROM cache_model"); err != nil {
				t.Fatal(err)
			}
			if read(false) != 1 {
				t.Fatal("test did not populate the result cache")
			}
			if read(true) != 0 {
				t.Fatal("Direct reused stale SQL result")
			}
			if read(false) != 1 {
				t.Fatal("Direct unexpectedly wrote through to result cache")
			}
		})
	}
}
func TestDirectReadInheritedByDerivedSessions(t *testing.T) {
	o := newCacheOrm(t)
	defer o.Close()
	parent := o.NewSession().Direct()
	defer parent.Close()
	child := parent.Clone()
	defer child.Close()
	if !child.directRead {
		t.Fatal("clone dropped direct read")
	}
	other := o.NewSession()
	defer other.Close()
	other.inheritIdentity(parent)
	if !other.directRead {
		t.Fatal("relation lost direct read")
	}
	if o.NewSession().directRead {
		t.Fatal("Direct leaked to another request")
	}
}
