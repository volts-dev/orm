package orm

import (
	"testing"
)

func TestOsvMarkPending(t *testing.T) {
	osv := &TOsv{}
	osv.markPending(fieldRef{
		fromModel: "website.page",
		fieldName: "AuthorId",
		toModel:   "sys.user",
		fieldType: TYPE_M2O,
	})
	osv.markPending(fieldRef{
		fromModel: "website.page",
		fieldName: "TagIds",
		toModel:   "website.tag",
		fieldType: TYPE_M2M,
	})

	if got := len(osv.pendingRefs); got != 2 {
		t.Fatalf("expected 2 pending refs, got %d", got)
	}
	if osv.pendingRefs[0].toModel != "sys.user" {
		t.Fatalf("unexpected first ref: %+v", osv.pendingRefs[0])
	}
}

func TestOsvMarkPendingConcurrent(t *testing.T) {
	osv := &TOsv{}
	const N = 100
	done := make(chan struct{})
	for i := 0; i < N; i++ {
		go func(i int) {
			osv.markPending(fieldRef{
				fromModel: "m",
				fieldName: "f",
				toModel:   "t",
				fieldType: TYPE_M2O,
			})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < N; i++ {
		<-done
	}
	if got := len(osv.pendingRefs); got != N {
		t.Fatalf("expected %d refs after concurrent markPending, got %d", N, got)
	}
}
