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
