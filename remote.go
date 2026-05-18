package orm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
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

// =========================================================================
// TRemoteModelObject — IModel implementation for cross-service models
// =========================================================================

// TRemoteModelObject implements IModel for a model whose data lives in
// another service. All writes return ErrRemoteWriteForbidden. Reads are
// implemented in Task 8; this skeleton stubs them.
//
// SQL/DDL lifecycle methods are no-ops because remote models never participate
// in local schema management.
type TRemoteModelObject struct {
	schema   *ModelSchema
	resolver IRemoteResolver
	orm      *TOrm
	osv      *TOsv
	ctx      context.Context

	fieldsByName map[string]IField
	fields       []IField
	tableName    string
	obj          *TModelObject

	mu sync.RWMutex
	tx *TSession
}

// newRemoteModelObject builds a TRemoteModelObject from a schema. The internal
// TModelObject mirror is minimal (name only) — enough to satisfy callers that
// only inspect metadata via Obj().
func newRemoteModelObject(schema *ModelSchema, resolver IRemoteResolver) *TRemoteModelObject {
	rm := &TRemoteModelObject{
		schema:       schema,
		resolver:     resolver,
		fieldsByName: make(map[string]IField, len(schema.Fields)),
		tableName:    schemaNameToTable(schema.Name),
		obj:          &TModelObject{name: schema.Name},
	}
	for _, fs := range schema.Fields {
		f := buildFieldFromSchema(fs, schema.Name)
		rm.fieldsByName[fs.Name] = f
		rm.fields = append(rm.fields, f)
	}
	return rm
}

func schemaNameToTable(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

// buildFieldFromSchema produces a minimal IField from a FieldSchema. The
// resulting field's modelName is set so consumers can introspect origin.
// Relational resolution beyond one hop is not wired — remote model field
// traversal is intentionally limited.
func buildFieldFromSchema(fs FieldSchema, ownerModel string) IField {
	f := &TField{}
	base := f.Base()
	base.name = fs.Name
	base.modelName = ownerModel
	base.typeName = fs.TypeName
	base.required = fs.IsRequired
	base.relatedModelName = fs.RelatedModel
	return f
}

// ----- IModel: metadata accessors -----

func (self *TRemoteModelObject) String() string                  { return self.schema.Name }
func (self *TRemoteModelObject) Table() string                   { return self.tableName }
func (self *TRemoteModelObject) IdField(field ...string) string  { return self.schema.IdField }
func (self *TRemoteModelObject) NameField(field ...string) string {
	if f := self.GetFieldByName("name"); f != nil {
		return "name"
	}
	return self.schema.IdField
}
func (self *TRemoteModelObject) GetFieldByName(name string) IField { return self.fieldsByName[name] }
func (self *TRemoteModelObject) GetFields() []IField               { return self.fields }
func (self *TRemoteModelObject) Obj() *TModelObject                { return self.obj }
func (self *TRemoteModelObject) Osv() *TOsv                        { return self.osv }
func (self *TRemoteModelObject) Orm() *TOrm                        { return self.orm }
func (self *TRemoteModelObject) GetBase() *TModel                  { return nil }
func (self *TRemoteModelObject) GetIndexes() map[string]*TIndex    { return nil }
func (self *TRemoteModelObject) GetColumnsSeq() []string           { return nil }
func (self *TRemoteModelObject) GetPrimaryKeys() []string {
	return []string{self.schema.IdField}
}
func (self *TRemoteModelObject) GetTableDescription() string { return "" }

func (self *TRemoteModelObject) Ctx(c ...context.Context) context.Context {
	if len(c) > 0 && c[0] != nil {
		self.ctx = c[0]
	}
	if self.ctx == nil {
		return context.Background()
	}
	return self.ctx
}

func (self *TRemoteModelObject) Options(opts ...ModelOption) *ModelOptions {
	o := &ModelOptions{Model: self}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (self *TRemoteModelObject) Super() IModel     { return self }
func (self *TRemoteModelObject) Prototype() IModel { return self }

func (self *TRemoteModelObject) Records() *TSession { return self.tx }

func (self *TRemoteModelObject) Tx(s ...*TSession) *TSession {
	self.mu.Lock()
	defer self.mu.Unlock()
	if len(s) > 0 {
		self.tx = s[0]
	}
	return self.tx
}

func (self *TRemoteModelObject) Transaction() *TSession { return self.Tx() }

// ----- IModel: writes (rejected) -----

func (self *TRemoteModelObject) Create(*CreateRequest) ([]any, error) {
	return nil, ErrRemoteWriteForbidden
}
func (self *TRemoteModelObject) Update(*UpdateRequest) (int64, error) {
	return 0, ErrRemoteWriteForbidden
}
func (self *TRemoteModelObject) Delete(*DeleteRequest) (int64, error) {
	return 0, ErrRemoteWriteForbidden
}
func (self *TRemoteModelObject) Upload(*UploadRequest) (int64, error) {
	return 0, ErrRemoteWriteForbidden
}
func (self *TRemoteModelObject) Load(fields []string, records ...any) ([]any, error) {
	return nil, ErrRemoteWriteForbidden
}
func (self *TRemoteModelObject) NameCreate(name string) (*dataset.TDataSet, error) {
	return nil, ErrRemoteWriteForbidden
}

// ----- IModel: reads -----

// Read translates a ReadRequest into a RemoteRequest of Op="read", invokes
// the resolver, and maps Records back into a TDataSet. Field ordering in the
// result follows schema.Fields to keep column order stable across records.
func (self *TRemoteModelObject) Read(req *ReadRequest) (*dataset.TDataSet, error) {
	if self.resolver == nil {
		return nil, fmt.Errorf("orm: remote model %s has no resolver bound", self.schema.Name)
	}
	if req == nil {
		req = &ReadRequest{}
	}

	rreq := &RemoteRequest{
		ModelName: self.schema.Name,
		Op:        "read",
		Fields:    req.Fields,
		Ids:       anySliceToInt64(req.Ids),
		Limit:     int(req.Limit),
		Offset:    int(req.Offset),
	}
	resp, err := self.resolver.Execute(self.Ctx(), rreq)
	if err != nil {
		return nil, fmt.Errorf("orm: remote read %s: %w", self.schema.Name, err)
	}
	return remoteResponseToDataset(resp, self.schema), nil
}

// anySliceToInt64 best-effort converts []any IDs to []int64.
// Non-numeric entries are skipped; convention is that ids are int64.
func anySliceToInt64(in []any) []int64 {
	out := make([]int64, 0, len(in))
	for _, v := range in {
		switch n := v.(type) {
		case int64:
			out = append(out, n)
		case int:
			out = append(out, int64(n))
		case int32:
			out = append(out, int64(n))
		case float64:
			out = append(out, int64(n))
		}
	}
	return out
}

// remoteResponseToDataset builds a TDataSet from a RemoteResponse.
func remoteResponseToDataset(resp *RemoteResponse, schema *ModelSchema) *dataset.TDataSet {
	ds := dataset.NewDataSet()
	if resp == nil {
		return ds
	}
	for _, row := range resp.Records {
		ds.AppendRecord(dataset.NewRecordSet(row))
	}
	return ds
}
func (self *TRemoteModelObject) NameSearch(name string, dom *domain.TDomainNode, operator string, limit int64, nameGetUid string, ctxMap map[string]interface{}) (*dataset.TDataSet, error) {
	return nil, ErrRemoteOpNotSupported
}
func (self *TRemoteModelObject) NameGet(ids []interface{}) (*dataset.TDataSet, error) {
	return nil, ErrRemoteOpNotSupported
}
func (self *TRemoteModelObject) DefaultGet(fields ...string) (map[string]any, error) {
	return nil, ErrRemoteOpNotSupported
}

// ----- IModel: relational queries (not supported across services) -----

func (self *TRemoteModelObject) OneToOne(*TFieldContext) (*dataset.TDataSet, error) {
	return nil, ErrRemoteOpNotSupported
}
func (self *TRemoteModelObject) OneToMany(*TFieldContext) (*dataset.TDataSet, error) {
	return nil, ErrRemoteOpNotSupported
}
func (self *TRemoteModelObject) ManyToOne(*TFieldContext) (*dataset.TDataSet, error) {
	return nil, ErrRemoteOpNotSupported
}
func (self *TRemoteModelObject) ManyToMany(*TFieldContext) (*dataset.TDataSet, error) {
	return nil, ErrRemoteOpNotSupported
}

// ----- IModel: SQL/DDL lifecycle (no-ops; not applicable) -----

func (self *TRemoteModelObject) _setOrm(o *TOrm) {
	self.orm = o
	if o != nil {
		self.osv = o.osv
	}
}
func (self *TRemoteModelObject) _setBaseModel(m *TModel)                       {}
func (self *TRemoteModelObject) _relations_reload()                            {}
func (self *TRemoteModelObject) _onBuildFields() error                         { return nil }
func (self *TRemoteModelObject) OnBuildModel() error                           { return nil }
func (self *TRemoteModelObject) OnBuildFields() error                          { return nil }
func (self *TRemoteModelObject) BeforeSetup() error                            { return nil }
func (self *TRemoteModelObject) AfterSetup() error                             { return nil }
func (self *TRemoteModelObject) BeforeSession(s *TSession) (*TSession, error)  { return s, nil }
func (self *TRemoteModelObject) AfterSession(s *TSession) (*TSession, error)   { return s, nil }
func (self *TRemoteModelObject) Clone(opts ...ModelOption) (IModel, error)    { return self, nil }
func (self *TRemoteModelObject) GetDefault() *sync.Map                         { return &sync.Map{} }
func (self *TRemoteModelObject) GetDefaultByName(name string) interface{}      { return nil }
func (self *TRemoteModelObject) SetDefaultByName(name string, v interface{})   {}
func (self *TRemoteModelObject) SetRecordName(name string)                     {}
func (self *TRemoteModelObject) GetRecordName() string                         { return self.schema.IdField }
func (self *TRemoteModelObject) SetName(n string)                              {}
func (self *TRemoteModelObject) MethodByName(name string) *TMethod             { return nil }
