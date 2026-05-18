package orm

import "time"

type (
	Option func(*Config)

	Config struct {
		DataSource         *TDataSource
		TimeZone           *time.Location
		ModelTemplate      *ModelTemplate
		FieldIdentifier    string // 字段 tag 标记
		TableIdentifier    string // 表 tag 标记
		ShowSql            bool
		ShowSqlTime        bool
		AutoCreateDatabase bool

		// RemoteResolver, when non-nil, lets Freeze resolve model references
		// that aren't registered locally. See remote.go / osv_freeze.go.
		RemoteResolver IRemoteResolver

		// StrictModelResolution makes Freeze return an error when any pending
		// ref remains unresolved after Phase 3. Recommended for CI/dev.
		// Default false (resilient cold-start in prod).
		StrictModelResolution bool
	}
)

func newConfig(opts ...Option) *Config {
	cfg := &Config{
		ModelTemplate:   &ModelTemplate{},
		TimeZone:        time.Local,
		ShowSqlTime:     true,
		FieldIdentifier: FieldIdentifier,
		TableIdentifier: TableIdentifier,
	}

	cfg.Init(opts...)
	return cfg
}

func WithModelOptions(opts ...ModelOption) Option {
	return func(cfg *Config) {
		cfg.ModelTemplate.AddOption(opts...)
	}
}
func (self *Config) Init(opts ...Option) {
	for _, opt := range opts {
		opt(self)
	}
}

func WithDataSource(ds *TDataSource) Option {
	return func(cfg *Config) {
		cfg.DataSource = ds
	}
}
func WithShowSql(on bool) Option {
	return func(cfg *Config) {
		cfg.ShowSql = on
	}
}

func WithFieldTag(tagName string) Option {
	return func(cfg *Config) {
		cfg.FieldIdentifier = tagName
	}
}

func WithTableTag(tableName string) Option {
	return func(cfg *Config) {
		cfg.TableIdentifier = tableName
	}
}

func WithAutoCreateDatabase(on bool) Option {
	return func(cfg *Config) {
		cfg.AutoCreateDatabase = on
	}
}

// WithRemoteResolver injects a host-provided IRemoteResolver implementation
// (e.g. vectors' resolver). A nil resolver disables Phase 3 remote resolution.
func WithRemoteResolver(r IRemoteResolver) Option {
	return func(cfg *Config) {
		cfg.RemoteResolver = r
	}
}

// WithStrictModelResolution toggles Freeze's Phase 4 verification.
// In strict mode, any unresolved ref after Phase 3 causes Freeze to error.
func WithStrictModelResolution(b bool) Option {
	return func(cfg *Config) {
		cfg.StrictModelResolution = b
	}
}
