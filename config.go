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

		// BigNumberToString, when true, converts big number fields (BigInt, Decimal, etc.) to string
		// in AsMap output to avoid JavaScript precision loss.
		BigNumberToString bool

		// DisableSchemaSync, 为 true 时 SyncModel 走"生产快路径":只把模型
		// 映射并注册进 osv(路由/CRUD 元数据照常可用),跳过整库反查(DBMetas)
		// 与建表/改表/建索引等全部 DDL。适用于结构已由部署期迁移保证的生产环境,
		// 可省掉每模块一次的全 schema 内省。默认 false(保持自动同步)。
		//
		// 契约:开启后不再自动建表/补列,库结构必须先由迁移就位,否则运行期查询
		// 会因缺表/缺列失败。需要跑一次同步时(如上线新版本),关掉本开关启动一次
		// 或用专门的迁移入口。
		DisableSchemaSync bool

		// 连接池。全部是"0 = 不设"，New() 在打开连接后、第一次查库之前套上。
		//
		// MaxOpenConns 是本进程同时占用的连接数上限。不设的话 database/sql 永不排队，
		// 负载多大就开多少条——所有进程加起来撞的是库的 max_connections，那时
		// 报错的是**别的**进程。设了之后等连接会排队，排多久由 AcquireTimeout 管
		//（见 core/pool.go）；两个要一起设。
		MaxOpenConns int
		// MaxIdleConns 是池里留着不关的空闲连接数。不设时是标准库默认的 2。
		MaxIdleConns int
		// ConnMaxLifetime / ConnMaxIdleTime 到点回收连接（前者按建立时刻，后者按空闲
		// 时长）。中间有会掐空闲连接的设备（NAT、LB、连接池代理）时要设。
		ConnMaxLifetime time.Duration
		ConnMaxIdleTime time.Duration
		// AcquireTimeout 是等一条空闲连接的上限，超了返回 core.ErrPoolTimeout。
		// 不设的话池满时会无限期地等。
		AcquireTimeout time.Duration

		// 行范围（见 row_scope.go）。RowScopeSetting 为空时整个机制关闭。
		RowScopeSetting string
		RowScopeMode    RowScopeMode
		OnRowScopeEvent func(RowScopeEvent)

		// 特权连接池（见 privileged.go）。为 nil 时特权会话走主池。
		PrivilegedDataSource   *TDataSource
		PrivilegedMaxOpenConns int
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

// WithBigNumberToString enables automatic conversion of big number fields to string.
func WithBigNumberToString(on bool) Option {
	return func(cfg *Config) {
		cfg.BigNumberToString = on
	}
}

// WithDisableSchemaSync 开关生产快路径。见 Config.DisableSchemaSync。
func WithDisableSchemaSync(on bool) Option {
	return func(cfg *Config) {
		cfg.DisableSchemaSync = on
	}
}

// WithMaxOpenConns 设置连接池上限。见 Config.MaxOpenConns。
func WithMaxOpenConns(n int) Option {
	return func(cfg *Config) {
		cfg.MaxOpenConns = n
	}
}

// WithMaxIdleConns 设置池里保留的空闲连接数。见 Config.MaxIdleConns。
func WithMaxIdleConns(n int) Option {
	return func(cfg *Config) {
		cfg.MaxIdleConns = n
	}
}

// WithConnMaxLifetime 设置一条连接最长能被复用多久。
func WithConnMaxLifetime(d time.Duration) Option {
	return func(cfg *Config) {
		cfg.ConnMaxLifetime = d
	}
}

// WithConnMaxIdleTime 设置一条连接最长能空闲多久。
func WithConnMaxIdleTime(d time.Duration) Option {
	return func(cfg *Config) {
		cfg.ConnMaxIdleTime = d
	}
}

// WithAcquireTimeout 设置等连接的上限。见 Config.AcquireTimeout。
func WithAcquireTimeout(d time.Duration) Option {
	return func(cfg *Config) {
		cfg.AcquireTimeout = d
	}
}
