package orm

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	ormerr "github.com/volts-dev/orm/errors"
)

// 行范围（row scope）：让**库自己**也知道一条语句是替谁跑的。
//
// # 为什么 ORM 要管这个
//
// 租户隔离原来只有一道门：宿主（vectors）在 BeforeSession 里给每条 ORM 查询加
// `tenant_id = ?`。手写的 Exec/Query、没挂登录会话的模型调用都不经过那里——那一类
// 语句出一个 bug，读写的就是别的租户的行，而且不报错。
//
// 第二道门放在库里：表上开行级安全（RLS），策略写成
//
//	USING (tenant_id = current_setting('app.tenant_id')::bigint)
//
// 库只认 current_setting，所以每条语句发出去之前，连接上得先有 `app.tenant_id`。
// 这就是本文件做的事：会话记着"我替哪个范围跑"（SetRowScope），进事务时发一条
// `SET LOCAL app.tenant_id = '…'`。
//
// # 必须是 SET LOCAL
//
// 与 search_path 同一个理由（见 session_schema.go）：会话级 SET 会留在池化的连接上，
// 下一个借到它的请求就顶着上一个租户的身份——那正是这道门要防的事。事务级设置在
// 提交或回滚时自动消失；自动提交的语句因此要包进一个隐式事务，代价是每条多三次往返
// （BEGIN / SET / COMMIT）。显式事务只多一次，而且只在范围第一次出现或变了的时候。
//
// # 三档
//
//   - RowScopeOff：什么都不做（默认）。
//   - RowScopeAudit：会话上的范围照记，但**不发给库**；没设范围就跑的语句报给
//     OnRowScopeEvent。用来在开 RLS 之前找出所有会被它拦下的地方，行为一个字节不变。
//   - RowScopeEnforce：发。没设范围的语句照样报，库上开了 RLS 的话它们会被拒。
//
// # 特权会话
//
// 有一类活本来就跨范围：结构同步、全库回填、平台控制面、逐租户扫描的定时任务。
// 它们走 NewPrivilegedSession()——配了 PrivilegedDataSource 时是另一个连接池、
// 另一个库身份（RLS 下是带 BYPASSRLS 的那个），没配时就是同一个池。特权会话既不发
// 范围、也不报事件：它是"我知道我在跨范围"的显式声明。
type RowScopeMode uint8

const (
	RowScopeOff RowScopeMode = iota
	RowScopeAudit
	RowScopeEnforce
)

func (m RowScopeMode) String() string {
	switch m {
	case RowScopeAudit:
		return "audit"
	case RowScopeEnforce:
		return "enforce"
	}
	return "off"
}

// ParseRowScopeMode 认 off / audit / enforce（大小写不敏感，空串是 off）。
func ParseRowScopeMode(s string) (RowScopeMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "off":
		return RowScopeOff, nil
	case "audit":
		return RowScopeAudit, nil
	case "enforce":
		return RowScopeEnforce, nil
	}
	return RowScopeOff, fmt.Errorf("orm: unknown row scope mode %q (want off, audit or enforce)", s)
}

type RowScopeEventKind uint8

const (
	// RowScopeUnscoped：普通会话上没设范围就执行了一条语句。
	RowScopeUnscoped RowScopeEventKind = iota + 1
	// RowScopeDDL：结构变更跑在普通会话上。配了特权连接池时，普通身份没有改表的
	// 权限，这类语句要改走 NewPrivilegedSession。
	RowScopeDDL
)

func (k RowScopeEventKind) String() string {
	switch k {
	case RowScopeUnscoped:
		return "unscoped"
	case RowScopeDDL:
		return "ddl"
	}
	return "unknown"
}

// RowScopeEvent 是报给 OnRowScopeEvent 的一条语句。回调在执行语句的 goroutine 上
// **同步**调用，调用栈就是发起这条语句的那一条——宿主靠它定位到调用点。回调必须
// 廉价：每条符合条件的语句都会调一次。
type RowScopeEvent struct {
	Kind RowScopeEventKind
	SQL  string
	InTx bool
}

// rowScopeSettingRe：自定义配置项必须带一个点（postgres 的约定，如 app.tenant_id），
// 且只许标识符字符——它会被拼进 SET LOCAL，不能是任意字符串。
var rowScopeSettingRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*$`)

func validateRowScopeSetting(name string) error {
	if name == "" || rowScopeSettingRe.MatchString(name) {
		return nil
	}
	return fmt.Errorf("orm: row scope setting %q must look like prefix.name (lower-case identifier characters)", name)
}

// WithRowScope 打开行范围。setting 是库上的自定义配置项名（如 "app.tenant_id"）。
func WithRowScope(setting string, mode RowScopeMode) Option {
	return func(cfg *Config) {
		cfg.RowScopeSetting = setting
		cfg.RowScopeMode = mode
	}
}

// WithRowScopeHook 设置事件回调，见 RowScopeEvent。
func WithRowScopeHook(fn func(RowScopeEvent)) Option {
	return func(cfg *Config) {
		cfg.OnRowScopeEvent = fn
	}
}

// txState 是一个事务的共享状态：拥有事务的会话与从它 Clone 出来、借用这个事务的
// 会话手里是同一个指针。
type txState struct {
	// rowScope 是本事务里已经 SET LOCAL 过的范围值；"" 表示还没设过。
	rowScope string
	// done：拥有者已经提交或回滚。借用者看到它就退回自动提交，而不是往一个已经
	// 结束的 *sql.Tx 上发语句。
	done atomic.Bool
}

// SetRowScope 声明本会话替哪个范围（租户）跑。Enforce 下它决定库上的
// current_setting；Audit 下只用来判断"有没有设"。
func (self *TSession) SetRowScope(v int64) *TSession {
	self.rowScope = v
	self.hasRowScope = true
	return self
}

// ClearRowScope 撤掉本会话上显式设的范围（之后回落到 context 里的，见
// ContextWithRowScope）。已经在事务里 SET LOCAL 过的值留到事务结束。
func (self *TSession) ClearRowScope() *TSession {
	self.rowScope = 0
	self.hasRowScope = false
	return self
}

// RowScope 返回本会话的范围：显式设的优先，其次是 context 里带的。
func (self *TSession) RowScope() (int64, bool) {
	if self.hasRowScope {
		return self.rowScope, true
	}
	if self.context != nil {
		return RowScopeFromContext(self.context)
	}
	return 0, false
}

type rowScopeCtxKey struct{}

// ContextWithRowScope 让范围跟着 context 走。
//
// 为什么还要这一条：一个请求里的会话不全是同一处建的。关系字段的子读取、名称查找、
// 默认值求值……ORM 在很多地方用 model.Records() 现起一个新会话，那些地方拿不到调用方
// 的会话，但都拿得到模型的 context（Records / Db 会把它带进新会话）。宿主在请求入口
// 把范围放进 context，这些派生会话就自动带上，不必一处一处去补。
func ContextWithRowScope(ctx context.Context, v int64) context.Context {
	return context.WithValue(ctx, rowScopeCtxKey{}, v)
}

// RowScopeFromContext 取 ContextWithRowScope 放进去的范围。
func RowScopeFromContext(ctx context.Context) (int64, bool) {
	if ctx == nil {
		return 0, false
	}
	v, ok := ctx.Value(rowScopeCtxKey{}).(int64)
	return v, ok
}

// IsPrivileged 报告本会话是否来自 NewPrivilegedSession。
func (self *TSession) IsPrivileged() bool {
	return self.privileged
}

// RowScopeMode 返回当前档位。
func (self *TOrm) RowScopeMode() RowScopeMode {
	if self.config.RowScopeSetting == "" {
		return RowScopeOff
	}
	return self.config.RowScopeMode
}

// sendsRowScope：本会话的范围要不要发给库。
func (self *TSession) sendsRowScope() bool {
	if self.privileged || self.orm == nil || self.orm.RowScopeMode() != RowScopeEnforce {
		return false
	}
	_, ok := self.RowScope()
	return ok
}

// rowScopeSql 返回设置本会话范围的语句；不需要发时返回空串。
func (self *TSession) rowScopeSql() string {
	if !self.sendsRowScope() || self.orm.dialect == nil {
		return ""
	}
	v, _ := self.RowScope()
	return self.orm.dialect.RowScopeSql(self.orm.config.RowScopeSetting, v)
}

// settingsSql 把本会话在一个事务开头要设的东西拼成一个字符串：search_path 与范围。
// 两条都不带参数，lib/pq 走简单查询协议，一次往返。
func (self *TSession) settingsSql() string {
	path, scope := self.searchPathSql(), self.rowScopeSql()
	switch {
	case path == "":
		return scope
	case scope == "":
		return path
	}
	return path + "; " + scope
}

// ensureTxRowScope 在事务里、下一条语句之前，把范围补设上（范围在 Begin 之后才
// 定下来，或者中途换了）。已经是这个值就什么都不发。
func (self *TSession) ensureTxRowScope() error {
	stmt := self.rowScopeSql()
	if stmt == "" || self.tx == nil {
		return nil
	}
	v, _ := self.RowScope()
	want := strconv.FormatInt(v, 10)
	if self.txs != nil && self.txs.rowScope == want {
		return nil
	}
	if _, err := self.tx.ExecContext(self.context, stmt); err != nil {
		return self.orm.dialect.MapError(err)
	}
	if self.txs != nil {
		self.txs.rowScope = want
	}
	return nil
}

// RowScopeMissingError 是 ErrRowScopeMissing 的具体形状：带着库的原文。
type RowScopeMissingError struct {
	Setting string
	Err     error
}

func (e *RowScopeMissingError) Error() string {
	return fmt.Sprintf("%s (set %s on the session, or use a privileged session for cross-scope work): %v",
		ormerr.ErrRowScopeMissing.Error(), e.Setting, e.Err)
}

func (e *RowScopeMissingError) Unwrap() []error { return []error{ormerr.ErrRowScopeMissing, e.Err} }

// explainRowScope 把"没设范围被行级安全拒了"的库原文换成 ErrRowScopeMissing。
// 只在会话确实没设范围、档位是 Enforce 时才换——原文本身有歧义（见
// postgres.rowScopeMissing）。
func (self *TSession) explainRowScope(err error) error {
	if err == nil || self.privileged || self.orm == nil || self.orm.RowScopeMode() != RowScopeEnforce {
		return err
	}
	if _, ok := self.RowScope(); ok {
		return err
	}
	d, ok := self.orm.dialect.(interface {
		rowScopeMissing(err error, setting string) bool
	})
	if !ok || !d.rowScopeMissing(err, self.orm.config.RowScopeSetting) {
		return err
	}
	return &RowScopeMissingError{Setting: self.orm.config.RowScopeSetting, Err: err}
}

// auditRowScope 在一条语句发出之前判断要不要报事件。
func (self *TSession) auditRowScope(query string) {
	if self.privileged || self.orm == nil {
		return
	}
	cfg := self.orm.config
	if cfg.OnRowScopeEvent == nil || self.orm.RowScopeMode() == RowScopeOff {
		return
	}
	inTx := !self.IsAutoCommit && self.tx != nil
	if isDDL(query) {
		cfg.OnRowScopeEvent(RowScopeEvent{Kind: RowScopeDDL, SQL: query, InTx: inTx})
		return
	}
	if _, ok := self.RowScope(); !ok {
		cfg.OnRowScopeEvent(RowScopeEvent{Kind: RowScopeUnscoped, SQL: query, InTx: inTx})
	}
}
