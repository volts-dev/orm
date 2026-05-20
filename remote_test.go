package orm

import (
	"context"
	"testing"
)

func TestIRemoteResolverInterfaceShape(t *testing.T) {
	var _ IRemoteResolver = (*mockRemoteResolver)(nil)
}

func TestModelSchemaFields(t *testing.T) {
	s := &ModelSchema{
		Name:       "sys.user",
		Version:    1,
		IdField:    "id",
		Fields:     []FieldSchema{{Name: "id", TypeName: "int", SqlType: "INT8"}},
		SourceNode: "tcp://127.0.0.1:9000",
	}
	if s.Name != "sys.user" || s.IdField != "id" {
		t.Fatalf("unexpected schema fields: %+v", s)
	}
}

func TestErrRemoteWriteForbidden(t *testing.T) {
	if ErrRemoteWriteForbidden == nil {
		t.Fatal("ErrRemoteWriteForbidden must be defined")
	}
	if ErrRemoteWriteForbidden.Error() == "" {
		t.Fatal("ErrRemoteWriteForbidden must have non-empty message")
	}
}

// mockRemoteResolver is a no-op implementation used across remote tests.
type mockRemoteResolver struct {
	lookupCalled int
	execCalled   int
	schemas      map[string]*ModelSchema
	execFn       func(ctx context.Context, req *RemoteRequest) (*RemoteResponse, error)
}

func (m *mockRemoteResolver) LookupSchema(ctx context.Context, name string) (*ModelSchema, error) {
	m.lookupCalled++
	if s, ok := m.schemas[name]; ok {
		return s, nil
	}
	return nil, ErrRemoteModelNotFound
}

func (m *mockRemoteResolver) Execute(ctx context.Context, req *RemoteRequest) (*RemoteResponse, error) {
	m.execCalled++
	if m.execFn != nil {
		return m.execFn(ctx, req)
	}
	return &RemoteResponse{}, nil
}

// ----- TRemoteModelObject tests -----

func TestRemoteModel_ImplementsIModel(t *testing.T) {
	var _ IModel = (*TRemoteModelObject)(nil)
}

func TestRemoteModel_String(t *testing.T) {
	rm := newRemoteModelObject(&ModelSchema{Name: "sys.user", IdField: "id"}, nil)
	if rm.String() != "sys.user" {
		t.Fatalf("expected String()=sys.user, got %q", rm.String())
	}
}

func TestRemoteModel_Table(t *testing.T) {
	rm := newRemoteModelObject(&ModelSchema{Name: "sys.user", IdField: "id"}, nil)
	if rm.Table() != "sys_user" {
		t.Fatalf("expected Table()=sys_user, got %q", rm.Table())
	}
}

func TestRemoteModel_IdField(t *testing.T) {
	rm := newRemoteModelObject(&ModelSchema{Name: "sys.user", IdField: "id"}, nil)
	if rm.IdField() != "id" {
		t.Fatalf("expected IdField()=id, got %q", rm.IdField())
	}
}

func TestRemoteModel_CreateReturnsForbidden(t *testing.T) {
	rm := newRemoteModelObject(&ModelSchema{Name: "sys.user", IdField: "id"}, &mockRemoteResolver{})
	_, err := rm.Create(&CreateRequest{})
	if err != ErrRemoteWriteForbidden {
		t.Fatalf("expected ErrRemoteWriteForbidden, got %v", err)
	}
}

func TestRemoteModel_UpdateReturnsForbidden(t *testing.T) {
	rm := newRemoteModelObject(&ModelSchema{Name: "sys.user", IdField: "id"}, &mockRemoteResolver{})
	_, err := rm.Update(&UpdateRequest{})
	if err != ErrRemoteWriteForbidden {
		t.Fatalf("expected ErrRemoteWriteForbidden, got %v", err)
	}
}

func TestRemoteModel_DeleteReturnsForbidden(t *testing.T) {
	rm := newRemoteModelObject(&ModelSchema{Name: "sys.user", IdField: "id"}, &mockRemoteResolver{})
	_, err := rm.Delete(&DeleteRequest{})
	if err != ErrRemoteWriteForbidden {
		t.Fatalf("expected ErrRemoteWriteForbidden, got %v", err)
	}
}

func TestRemoteModel_Read_BasicRoundtrip(t *testing.T) {
	resolver := &mockRemoteResolver{
		execFn: func(ctx context.Context, req *RemoteRequest) (*RemoteResponse, error) {
			if req.Op != "read" {
				t.Errorf("expected Op=read, got %s", req.Op)
			}
			if req.ModelName != "sys.user" {
				t.Errorf("expected ModelName=sys.user, got %s", req.ModelName)
			}
			if len(req.Ids) != 1 || req.Ids[0] != 7 {
				t.Errorf("expected Ids=[7], got %+v", req.Ids)
			}
			return &RemoteResponse{
				Records: []map[string]any{
					{"id": int64(7), "name": "Alice"},
				},
				Count: 1,
			}, nil
		},
	}
	rm := newRemoteModelObject(&ModelSchema{
		Name:    "sys.user",
		IdField: "id",
		Fields: []FieldSchema{
			{Name: "id", TypeName: "int"},
			{Name: "name", TypeName: "chars"},
		},
	}, resolver)

	ds, err := rm.Read(&ReadRequest{Ids: []any{int64(7)}, Fields: []string{"name"}})
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if ds == nil || ds.Count() != 1 {
		t.Fatalf("expected 1 record, got count=%d", ds.Count())
	}
	rec := ds.Record()
	if got, _ := rec.GetByField("name").(string); got != "Alice" {
		t.Fatalf("expected name=Alice, got %v", rec.GetByField("name"))
	}
	if resolver.execCalled != 1 {
		t.Fatalf("expected 1 Execute call, got %d", resolver.execCalled)
	}
}

func TestRemoteModel_Read_NilResolverErrors(t *testing.T) {
	rm := newRemoteModelObject(&ModelSchema{Name: "sys.user", IdField: "id"}, nil)
	_, err := rm.Read(&ReadRequest{Ids: []any{int64(1)}})
	if err == nil {
		t.Fatal("expected error when resolver is nil")
	}
}

func TestRemoteModel_GetFieldByName(t *testing.T) {
	rm := newRemoteModelObject(&ModelSchema{
		Name:    "sys.user",
		IdField: "id",
		Fields: []FieldSchema{
			{Name: "id", TypeName: "int", SqlType: "INT8"},
			{Name: "name", TypeName: "chars", SqlType: "VARCHAR(255)"},
		},
	}, nil)
	f := rm.GetFieldByName("name")
	if f == nil {
		t.Fatal("expected field 'name' to be found, got nil")
	}
	if f.Name() != "name" {
		t.Fatalf("expected field name='name', got %q", f.Name())
	}
	if rm.GetFieldByName("nonexistent") != nil {
		t.Fatal("expected nil for missing field")
	}
}
