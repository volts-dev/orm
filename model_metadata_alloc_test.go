package orm

import (
	"fmt"
	"sync"
	"testing"
)

type uncomparableMetadataField struct {
	IField
	data []byte
}

func metadataObject(n int) *TModelObject {
	obj := &TModelObject{relatedFields: map[string]*TRelatedField{}}
	for i := 0; i < n; i++ {
		name := fmt.Sprint("field_", i)
		obj.fields.Store(name, &TField{name: name})
	}
	return obj
}

func TestMetadataFieldEnumerationAlwaysReadsCurrentFields(t *testing.T) {
	obj := metadataObject(150)
	first := obj.GetFields()
	first[0] = nil // the returned slice is owned
	for i := 0; i < 20; i++ {
		obj.fields.Delete(fmt.Sprint("field_", i))
	}
	newField := &TField{name: "new_field"}
	obj.fields.Store("new_field", newField)
	replacement := &TField{name: "field_149"}
	obj.fields.Store("field_149", replacement)
	second := obj.GetFields()
	if len(second) != 131 {
		t.Fatalf("stale count: %d", len(second))
	}
	seen := map[string]IField{}
	for _, f := range second {
		if f == nil {
			t.Fatal("slice aliases caller")
		}
		seen[f.Name()] = f
	}
	if seen["new_field"] != newField || seen["field_149"] != replacement || seen["field_0"] != nil {
		t.Fatal("stale field contents")
	}
}

func TestMetadataRelatedMappingRefresh(t *testing.T) {
	obj := metadataObject(0)
	field := &TField{name: "parent_field"}
	obj.refreshRelatedField("child_field", "parent", "parent_id", field, "root")
	first := obj.GetRelatedFieldByName("child_field")
	obj.refreshRelatedField("child_field", "parent", "parent_id", field, "root")
	if obj.GetRelatedFieldByName("child_field") != first {
		t.Fatal("identical mapping rebuilt")
	}
	for _, change := range []struct {
		table, link, root string
		field             IField
	}{
		{"other", "parent_id", "root", field}, {"other", "other_id", "root", field},
		{"other", "other_id", "new_root", field}, {"other", "other_id", "new_root", &TField{name: "replacement"}},
	} {
		before := obj.GetRelatedFieldByName("child_field")
		obj.refreshRelatedField("child_field", change.table, change.link, change.field, change.root)
		after := obj.GetRelatedFieldByName("child_field")
		if before == after || after.RelatedField != change.field || after.RelatedTableName != change.table || after.RelatedFieldName != change.link || after.RelatedRootModel != change.root {
			t.Fatal("changed mapping ignored")
		}
	}
	if first.RelatedTableName != "parent" || first.RelatedField != field {
		t.Fatal("old reader's mapping mutated")
	}
	// Custom value fields may have slices; equality must not panic or retain them.
	custom := uncomparableMetadataField{IField: field, data: []byte("x")}
	obj.refreshRelatedField("custom", "parent", "id", custom, "parent")
	old := obj.GetRelatedFieldByName("custom")
	obj.refreshRelatedField("custom", "parent", "id", custom, "parent")
	if old == obj.GetRelatedFieldByName("custom") {
		t.Fatal("uncomparable custom field cached")
	}
}

func TestMetadataConcurrentEnumerationAndRefresh(t *testing.T) {
	obj := metadataObject(150)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				for _, f := range obj.GetFields() {
					obj.refreshRelatedField(f.Name(), "parent", "parent_id", f, "parent")
				}
				obj.GetRelatedFields()
			}
		}()
	}
	wg.Wait()
	if len(obj.GetRelatedFields()) != 150 {
		t.Fatal("incomplete mappings")
	}
}

func BenchmarkMetadataEnumeration(b *testing.B) {
	for _, current := range []bool{false, true} {
		b.Run(fmt.Sprint(current), func(b *testing.B) {
			obj := metadataObject(150)
			obj.GetFields()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var fields []IField
				if current {
					fields = obj.GetFields()
				} else {
					fields = make([]IField, 0)
					obj.fields.Range(func(_, value any) bool { fields = append(fields, value.(IField)); return true })
				}
				if len(fields) != 150 {
					b.Fatal("missing fields")
				}
			}
		})
	}
}

func BenchmarkMetadataRelatedRefresh(b *testing.B) {
	for _, current := range []bool{false, true} {
		b.Run(fmt.Sprint(current), func(b *testing.B) {
			obj := metadataObject(150)
			fields := obj.GetFields()
			for _, f := range fields {
				obj.refreshRelatedField(f.Name(), "parent", "parent_id", f, "parent")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, f := range fields {
					if current {
						obj.refreshRelatedField(f.Name(), "parent", "parent_id", f, "parent")
					} else {
						obj.SetRelatedFieldByName(f.Name(), NewRelatedField(f.Name(), "parent", "parent_id", f, "parent"))
					}
				}
			}
		})
	}
}
