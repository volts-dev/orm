package orm

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadOnlyAttributes_MatchesLiveShapeAndRuntimeDefaults(t *testing.T) {
	f := &TVarcharField{TField: TField{name: "cached_probe", typeName: "varchar", label: "Initial", store: true}}
	ctx := &TTagContext{Field: f}
	for name, change := range map[string]func(){
		"help": func() { f.description = "Long help" }, "label": func() { f.label = "Changed" }, "store": func() { f.store = false },
		"manual": func() { f.manual = true }, "readonly": func() { f.readonly = true }, "required": func() { f.required = true },
		"sortable": func() { f.sortable = true }, "searchable": func() { f.searchable = true }, "index": func() { f.isIndexed = true },
		"size": func() { f.size = 78 }, "type": func() { f.typeName = "text" }, "related": func() { f.relatedPath = "parent.value" },
		"groups": func() { f.permissionGroups = "group.private" }, "domain": func() { f.domain = "[('active','=',True)]" },
		"translate": func() { f.translatable = true }, "inherited": func() { f.isInherited = true }, "relation": func() { f.relatedModelName = "test.parent"; f.relatedKeyName = "pid" },
		"digits": func() { f.SetDigits(16, 3) }, "digits_usage": func() { f.digitsUsage = "Price" },
		"min_digits": func() { f.SetMinDisplayDigits(2) }, "min_usage": func() { f.minDisplayDigitsUsage = "Cost" },
		"aggregate": func() { f.groupOperator = "AVG" }, "default": func() { f.SetDefault("runtime value") },
	} {
		t.Run(name, func(t *testing.T) {
			before := f.ReadOnlyAttributes(ctx)
			change()
			got, want := f.ReadOnlyAttributes(ctx), f.Attributes(ctx)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("cached shape differs: got=%v want=%v", got, want)
			}
			if reflect.DeepEqual(got, before) {
				t.Fatalf("mutation %s not visible", name)
			}
		})
	}
}
func TestReadOnlyAttributes_ComplexValuesAndCustomOverridesRemainUncached(t *testing.T) {
	f := &TVarcharField{TField: TField{name: "complex_probe", typeName: "varchar", staticDefault: map[string]any{"x": 1}}}
	ctx := &TTagContext{Field: f}
	first := f.ReadOnlyAttributes(ctx)
	first["label_mutation"] = true
	if _, ok := f.ReadOnlyAttributes(ctx)["label_mutation"]; ok {
		t.Fatal("mutable default was cached")
	}
	custom := &attributeOverrideProbe{}
	c := &TTagContext{Field: custom}
	a, b := custom.ReadOnlyAttributes(c), custom.ReadOnlyAttributes(c)
	if a["call"] == b["call"] {
		t.Fatal("custom Attributes override frozen")
	}
}

type attributeOverrideProbe struct {
	TField
	calls int
}

func (f *attributeOverrideProbe) Attributes(*TTagContext) map[string]any {
	f.calls++
	return map[string]any{"call": f.calls}
}
func TestReadOnlyAttributes_RelationAndOriginalOwnedMap(t *testing.T) {
	f := &TMany2OneField{TRelational: TRelational{TField: TField{name: "related_probe", typeName: TYPE_M2O, relatedModelName: "res.partner", store: true, digits: []int{16, 2}}}}
	ctx := &TTagContext{Field: f}
	shared := f.ReadOnlyAttributes(ctx)
	if !reflect.DeepEqual(shared, f.Attributes(ctx)) {
		t.Fatal("relational attribute lost")
	}
	ordinary := f.Attributes(ctx)
	ordinary["string"] = "caller value"
	if shared["string"] == "caller value" {
		t.Fatal("Attributes API stopped returning independent maps")
	}
	f.digits[1] = 4
	newer := f.ReadOnlyAttributes(ctx)
	if shared["digits"].([]int)[1] != 2 || newer["digits"].([]int)[1] != 4 {
		t.Fatal("cached nested precision references mutable field slice")
	}
}
func TestReadOnlyAttributes_BoundsAndReclaim(t *testing.T) {
	new(TField).ReclaimFieldAttributeCache(true)
	for i := 0; i < fieldAttributeMaxEntries+100; i++ {
		f := &TCharField{TField: TField{name: fmt.Sprintf("bound_%d", i), typeName: "varchar"}}
		f.ReadOnlyAttributes(&TTagContext{Field: f})
	}
	count, bytes := new(TField).FieldAttributeCacheUsage()
	if count > fieldAttributeMaxEntries || bytes > fieldAttributeMaxBytes || count == 0 {
		t.Fatalf("unbounded store: %d %d", count, bytes)
	}
	new(TField).ReclaimFieldAttributeCache(false)
	smaller, _ := new(TField).FieldAttributeCacheUsage()
	if smaller >= count {
		t.Fatal("trim did not remove entries")
	}
	new(TField).ReclaimFieldAttributeCache(true)
	empty, size := new(TField).FieldAttributeCacheUsage()
	if empty != 0 || size != 0 {
		t.Fatal("purge retained entries")
	}
	huge := &TCharField{TField: TField{name: "oversized", staticDefault: strings.Repeat("x", fieldAttributeMaxEntryBytes)}}
	huge.ReadOnlyAttributes(&TTagContext{Field: huge})
	empty, _ = new(TField).FieldAttributeCacheUsage()
	if empty != 0 {
		t.Fatal("oversized default ignored byte bound")
	}
}

type attributeSelectionContext struct {
	TModel
	label string
}

func (m attributeSelectionContext) Choices() [][]string { return [][]string{{"option", m.label}} }
func TestSelectionAttributes_ConcurrentContextsDoNotPublishChoices(t *testing.T) {
	field := &TSelectionField{TField: TField{name: "dynamic", typeName: TYPE_SELECTION, getterMethod: "Choices", selection: [][]string{{"static", "base"}}}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m := &attributeSelectionContext{label: fmt.Sprint(i)}
			m.modelValue = reflect.ValueOf(m)
			for j := 0; j < 100; j++ {
				if err := field.OnRead(&TFieldContext{Model: m, Field: field}); err != nil {
					t.Error(err)
					return
				}
				got := field.ReadOnlyAttributes(&TTagContext{Model: m, Field: field})["selection"].([][]string)
				if got[0][1] != m.label {
					t.Errorf("another context's choices: got=%v want=%s", got, m.label)
					return
				}
			}
		}(i)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("selection metadata stalled")
	}
	if field.selection[0][1] != "base" {
		t.Fatal("dynamic choices written into shared field")
	}
}
func BenchmarkFieldAttributesMetadata(b *testing.B) {
	f := &TMany2OneField{TRelational: TRelational{TField: TField{name: "partner_id", typeName: TYPE_M2O, relatedModelName: "res.partner", label: "Customer", description: "Related customer record", store: true, sortable: true, searchable: true}}}
	ctx := &TTagContext{Field: f}
	f.ReadOnlyAttributes(ctx)
	b.Run("owned-existing", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			attrs := f.Attributes(ctx)
			if attrs["name"] != "partner_id" {
				b.Fatal("bad result")
			}
		}
	})
	b.Run("immutable-hit", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			attrs := f.ReadOnlyAttributes(ctx)
			if attrs["name"] != "partner_id" {
				b.Fatal("bad result")
			}
		}
	})
}

type defaultPauseModel struct {
	IModel
	calls           atomic.Int64
	started, finish chan struct{}
}

func (m *defaultPauseModel) GetDefaultByName(string) (bool, any) {
	if m.calls.Add(1) == 2 {
		close(m.started)
		<-m.finish
	}
	return true, "stable default"
}
func TestReadOnlyAttributes_ReclaimRejectsInflightRefill(t *testing.T) {
	new(TField).ReclaimFieldAttributeCache(true)
	m := &defaultPauseModel{started: make(chan struct{}), finish: make(chan struct{})}
	f := &TCharField{TField: TField{name: "reclaim_flight", boundModel: m}}
	done := make(chan struct{})
	go func() { f.ReadOnlyAttributes(&TTagContext{Field: f}); close(done) }()
	select {
	case <-m.started:
	case <-time.After(2 * time.Second):
		t.Fatal("attribute build did not pause")
	}
	new(TField).ReclaimFieldAttributeCache(true)
	close(m.finish)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("attribute build did not finish")
	}
	entries, _ := new(TField).FieldAttributeCacheUsage()
	if entries != 0 {
		t.Fatal("old in-flight descriptor repopulated purged cache")
	}
}
