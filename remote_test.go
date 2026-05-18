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
