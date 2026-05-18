package orm

import (
	"context"
	"errors"
)

var (
	// ErrRemoteWriteForbidden is returned by TRemoteModelObject's Create/Update/Delete/Upload.
	// 调用方必须走业务自定义的远程 client，禁止 ORM 隐式发起跨服务写。
	ErrRemoteWriteForbidden = errors.New("orm: write operation forbidden on remote model; use the owning service's client")

	// ErrRemoteModelNotFound is returned by IRemoteResolver.LookupSchema
	// when no service provides the requested model.
	ErrRemoteModelNotFound = errors.New("orm: remote model not found in registry")

	// ErrOsvFrozen is returned by RegisterModel after Freeze has been called.
	ErrOsvFrozen = errors.New("orm: osv is frozen, cannot register new models")

	// ErrRemoteOpNotSupported is returned by TRemoteModelObject methods that
	// are not applicable to remote models (e.g. SyncModel, BeforeSetup).
	ErrRemoteOpNotSupported = errors.New("orm: operation not supported on remote model")
)

// IRemoteResolver is the port ORM exposes for host frameworks (e.g. vectors).
// Implementations are responsible for service discovery, RPC transport, caching.
// ORM itself does not import any of these concerns.
type IRemoteResolver interface {
	// LookupSchema is invoked during osv.Freeze for any model name not
	// registered locally. Implementations should return ErrRemoteModelNotFound
	// when no service provides the model.
	LookupSchema(ctx context.Context, modelName string) (*ModelSchema, error)

	// Execute is invoked on every CRUD operation against a TRemoteModelObject.
	Execute(ctx context.Context, req *RemoteRequest) (*RemoteResponse, error)
}

// ModelSchema is a wire-neutral description of a model's structure.
// Hosts are responsible for serializing this across transports.
type ModelSchema struct {
	Name       string        // e.g. "sys.user"
	Version    uint64        // bumped by the owning service when schema changes
	IdField    string        // typically "id"
	Fields     []FieldSchema // ordered as declared
	SourceNode string        // host-filled: address/identifier of the providing service node
}

// FieldSchema describes a single field within a ModelSchema.
type FieldSchema struct {
	Name         string // field name on the model
	TypeName     string // ORM type tag: "chars" / "many2one" / ...
	SqlType      string // SQL column type as serialized by the owning service
	IsRequired   bool
	RelatedModel string // only set for many2one/one2many/many2many
}

// RemoteRequest is a CRUD operation request sent to a remote model.
// Op values: "read" / "create" / "write" / "unlink" / "search".
type RemoteRequest struct {
	ModelName string
	Op        string
	Ids       []int64                // used by read/write/unlink
	Values    map[string]interface{} // used by create/write
	Domain    [][]interface{}        // used by search (Odoo-style domain)
	Fields    []string               // used by read (returned field filter)
	Limit     int
	Offset    int
}

// RemoteResponse is the result returned by IRemoteResolver.Execute.
type RemoteResponse struct {
	Records []map[string]interface{}
	Ids     []int64
	Count   int64
}
