// Package errors defines ORM 框架的统一错误体系。
// 支持 errors.Is/As，所有 ORM 错误都从 8 个 sentinel 之一 unwrap。
package errors

import (
	"errors"
	"fmt"
	"strings"
)

// sentinel errors —— 调用方用 errors.Is(err, ErrXxx) 判断
var (
	// ErrNotFound 查询无结果
	ErrNotFound = errors.New("orm: record not found")
	// ErrDuplicate 唯一约束冲突
	ErrDuplicate = errors.New("orm: duplicate record")
	// ErrConflict 并发冲突（死锁 / 锁超时 / 乐观锁版本冲突）
	ErrConflict = errors.New("orm: concurrent conflict")
	// ErrValidation 客户端校验失败 / 外键约束
	ErrValidation = errors.New("orm: validation failed")
	// ErrTimeout 操作超时（ctx.DeadlineExceeded 触发）
	ErrTimeout = errors.New("orm: operation timeout")
	// ErrConnection 连接失败 / 断开
	ErrConnection = errors.New("orm: connection failed")
	// ErrTransaction 事务相关错误
	ErrTransaction = errors.New("orm: transaction error")
	// ErrUnsafe 危险操作被默认守护拒绝（Phase 2 启用）
	// 无 WHERE 的 DELETE/UPDATE、DROP TABLE、TRUNCATE 需要 AllowUnsafe() 明确 opt-in
	ErrUnsafe = errors.New("orm: unsafe operation denied; use AllowUnsafe() to opt-in")
	// ErrNoSoftDelete 软删除相关：模型无 deleted tag 字段
	ErrNoSoftDelete = errors.New("orm: model has no 'deleted' tag field")
	// ErrSoftDeleteMisconfigured 软删除相关：模型有多个 deleted tag 字段
	ErrSoftDeleteMisconfigured = errors.New("orm: model has multiple 'deleted' tag fields")
	// ErrLockNotSupported 当前方言没有行级锁（sqlite）。
	// session 收到它时降级为一条警告并继续执行，不当作失败——SQLite 的写事务
	// 本身即全库互斥，行锁无从谈起。
	ErrLockNotSupported = errors.New("orm: dialect does not support row-level locking")
	// ErrLockOutsideTransaction 不在事务中请求行锁。
	// 单语句事务会在语句结束的瞬间释放锁，等于没加锁，却让调用方以为拿到了互斥；
	// 因此这里直接拒绝而不是发一条没用的 SQL。先 Begin() 再取锁。
	ErrLockOutsideTransaction = errors.New("orm: row lock requires an explicit transaction; call Begin() first")
	// ErrLockNotApplicable 该查询形态不能加行锁（GROUP BY / Count 等聚合）。
	ErrLockNotApplicable = errors.New("orm: row lock cannot be applied to this query")

	// ErrInvalidDomain：Where()/Domain()/And()/Or() 收到的条件解析失败或类型不支持。
	// 链式方法没有返回错误的位置，于是记在 Statement 上，由随后的
	// Read/Search/Count/Sum/Write/Delete/ReadGroup 统一交回——绝不静默丢弃条件。
	// 原来只 log 不报错：条件被整个丢掉，读回全表、按域写/删越界，都是"看着正常的
	// 错结果"，也是 vectors 侧反复记录的"domain 解析失败是降级不是中断"。
	ErrInvalidDomain = errors.New("orm: invalid domain/condition")

	// ErrIndexUnsupported：当前方言/版本表达不了这条索引（如 MySQL < 8.0.13 的
	// 表达式索引、部分唯一索引）。拒绝而不是退化——退化会静默改变唯一性语义。
	ErrIndexUnsupported = errors.New("orm: index definition not supported by this database")
	// ErrNotTransient：对没有声明 transient 的模型调用 VacuumTransient。
	ErrNotTransient = errors.New("orm: model is not transient")
	// ErrNoCreatedField：transient 模型没有 `created` 标签字段，无从判断记录年龄。
	ErrNoCreatedField = errors.New("orm: model has no 'created' tag field")
)

// ORMError 携带上下文的 ORM 错误，支持 errors.Is/As
type ORMError struct {
	Kind  error  // 一个 sentinel（ErrNotFound / ErrDuplicate / ... ）
	Field string // 可选：关联字段名
	SQL   string // 可选：脱敏后的 SQL（参数字面量替换为占位符）
	Cause error  // 可选：底层 driver 错误
}

// Error 实现 error 接口
func (e *ORMError) Error() string {
	parts := []string{e.Kind.Error()}
	if e.Field != "" {
		parts = append(parts, fmt.Sprintf("field=%s", e.Field))
	}
	if e.SQL != "" {
		parts = append(parts, fmt.Sprintf("sql=%s", e.SQL))
	}
	if e.Cause != nil {
		parts = append(parts, fmt.Sprintf("cause=%v", e.Cause))
	}
	return strings.Join(parts, "; ")
}

// Unwrap 让 errors.Is(err, ErrNotFound) 工作
func (e *ORMError) Unwrap() error {
	return e.Kind
}

// New 构造 ORMError；kind 必须是 8 个 sentinel 之一
func New(kind error, cause error) *ORMError {
	return &ORMError{Kind: kind, Cause: cause}
}

// WithField 链式设置字段名
func (e *ORMError) WithField(name string) *ORMError {
	e.Field = name
	return e
}

// WithSQL 链式设置 SQL（自动脱敏）
func (e *ORMError) WithSQL(sql string) *ORMError {
	e.SQL = sanitizeSQL(sql)
	return e
}

// sanitizeSQL 脱敏：把字符串字面量、数字字面量替换为占位符 ?
// 用于错误日志避免泄露用户数据
func sanitizeSQL(sql string) string {
	var sb strings.Builder
	sb.Grow(len(sql))

	inSingleQuote := false
	inDoubleQuote := false
	i := 0
	for i < len(sql) {
		ch := sql[i]
		switch {
		case ch == '\'' && !inDoubleQuote:
			if inSingleQuote {
				sb.WriteByte('?')
				inSingleQuote = false
			} else {
				inSingleQuote = true
			}
		case ch == '"' && !inSingleQuote:
			if inDoubleQuote {
				sb.WriteByte('?')
				inDoubleQuote = false
			} else {
				inDoubleQuote = true
			}
		case inSingleQuote || inDoubleQuote:
			// 跳过引号内字符
		case ch >= '0' && ch <= '9' && !inSingleQuote && !inDoubleQuote:
			for i < len(sql) && (sql[i] >= '0' && sql[i] <= '9' || sql[i] == '.') {
				i++
			}
			sb.WriteByte('?')
			continue
		default:
			sb.WriteByte(ch)
		}
		i++
	}
	return sb.String()
}
