package orm

import "time"

type (
	Option func(*Config)

	Config struct {
		DataSource      *TDataSource
		TimeZone        *time.Location
		FieldIdentifier string // 字段 tag 标记
		TableIdentifier string // 表 tag 标记
		ShowSql         bool
		ShowSqlTime     bool
	}
)

func newConfig(opts ...Option) *Config {
	cfg := &Config{
		TimeZone:        time.Local,
		ShowSqlTime:     true,
		FieldIdentifier: FieldIdentifier,
		TableIdentifier: TableIdentifier,
	}

	cfg.Init(opts...)
	return cfg
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
