// Package errors defines ORM 框架的统一错误体系。
// 支持 errors.Is/As，所有 ORM 错误都从 8 个 sentinel 之一 unwrap。
package errors

import (
	"errors"
	"fmt"
	"strings"
)

// 11 个 sentinel errors —— 调用方用 errors.Is(err, ErrXxx) 判断
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
