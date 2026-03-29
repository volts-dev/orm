package errors

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
)

type (
	Errors struct {
		errors []error
	}
)

func New(errs ...error) *Errors {
	return &Errors{
		errors: errs,
	}

}

func (self Errors) Error() string {
	buf := bytes.NewBufferString("")
	for _, err := range self.errors {
		buf.WriteString(err.Error() + "\n")
	}
	return buf.String()
}

// ErrorType 错误类型枚举
type ErrorType int

const (
	ErrorTypeValidation ErrorType = iota + 1
	ErrorTypeNotFound
	ErrorTypeDuplicate
	ErrorTypeConnection
	ErrorTypeTransaction
	ErrorTypeQuery
	ErrorTypeInternalServer
	ErrorTypeConcurrency
	ErrorTypeTimeout
)

// ORMError ORM框架的统一错误类型
type ORMError struct {
	Type      ErrorType
	Message   string
	FieldName string   // 关联的字段名
	Details   string   // 详细错误信息
	Stack     []string // 调用栈
	Cause     error    // 原始错误
}

// NewORMError 创建新的ORM错误
func NewORMError(errorType ErrorType, message string) *ORMError {
	return &ORMError{
		Type:    errorType,
		Message: message,
		Stack:   captureStack(),
	}
}

// Wrap 包裹一个现有的错误
func Wrap(errorType ErrorType, message string, cause error) *ORMError {
	return &ORMError{
		Type:    errorType,
		Message: message,
		Cause:   cause,
		Stack:   captureStack(),
	}
}

// WithField 添加关联的字段名
func (e *ORMError) WithField(fieldName string) *ORMError {
	e.FieldName = fieldName
	return e
}

// WithDetails 添加详细信息
func (e *ORMError) WithDetails(details string) *ORMError {
	e.Details = details
	return e
}

// Error 实现error接口
func (e *ORMError) Error() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("[%s] %s", e.getTypeString(), e.Message))

	if e.FieldName != "" {
		sb.WriteString(fmt.Sprintf(" (字段: %s)", e.FieldName))
	}

	if e.Details != "" {
		sb.WriteString(fmt.Sprintf("\n详情: %s", e.Details))
	}

	if e.Cause != nil {
		sb.WriteString(fmt.Sprintf("\n原因: %v", e.Cause))
	}

	return sb.String()
}

// GetStackTrace 获取格式化的调用栈
func (e *ORMError) GetStackTrace() string {
	if len(e.Stack) == 0 {
		return "无调用栈信息"
	}

	var sb strings.Builder
	for i, frame := range e.Stack {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, frame))
	}
	return sb.String()
}

// String 返回完整的错误信息（包括调用栈）
func (e *ORMError) String() string {
	var sb strings.Builder

	sb.WriteString(e.Error())
	sb.WriteString("\n\n调用栈:\n")
	sb.WriteString(e.GetStackTrace())

	return sb.String()
}

// getTypeString 获取错误类型的字符串表示
func (e *ORMError) getTypeString() string {
	switch e.Type {
	case ErrorTypeValidation:
		return "验证错误"
	case ErrorTypeNotFound:
		return "未找到"
	case ErrorTypeDuplicate:
		return "重复记录"
	case ErrorTypeConnection:
		return "连接错误"
	case ErrorTypeTransaction:
		return "事务错误"
	case ErrorTypeQuery:
		return "查询错误"
	case ErrorTypeInternalServer:
		return "内部错误"
	case ErrorTypeConcurrency:
		return "并发错误"
	case ErrorTypeTimeout:
		return "超时错误"
	default:
		return "未知错误"
	}
}

// captureStack 捕获调用栈
func captureStack() []string {
	var stack []string

	pc := make([]uintptr, 10)
	n := runtime.Callers(3, pc)

	for i := 0; i < n; i++ {
		f := runtime.FuncForPC(pc[i])
		file, line := f.FileLine(pc[i])

		// 简化文件路径（只保留包名和文件名）
		fileParts := strings.Split(file, "/")
		var shortFile string
		if len(fileParts) >= 2 {
			shortFile = strings.Join(fileParts[len(fileParts)-2:], "/")
		} else {
			shortFile = file
		}

		stack = append(stack, fmt.Sprintf("%s() at %s:%d", f.Name(), shortFile, line))
	}

	return stack
}

// IsValidationError 判断是否是验证错误
func IsValidationError(err error) bool {
	if ormErr, ok := err.(*ORMError); ok {
		return ormErr.Type == ErrorTypeValidation
	}
	return false
}

// IsNotFoundError 判断是否是未找到错误
func IsNotFoundError(err error) bool {
	if ormErr, ok := err.(*ORMError); ok {
		return ormErr.Type == ErrorTypeNotFound
	}
	return false
}

// IsDuplicateError 判断是否是重复错误
func IsDuplicateError(err error) bool {
	if ormErr, ok := err.(*ORMError); ok {
		return ormErr.Type == ErrorTypeDuplicate
	}
	return false
}

// IsConnectionError 判断是否是连接错误
func IsConnectionError(err error) bool {
	if ormErr, ok := err.(*ORMError); ok {
		return ormErr.Type == ErrorTypeConnection
	}
	return false
}

// IsTransactionError 判断是否是事务错误
func IsTransactionError(err error) bool {
	if ormErr, ok := err.(*ORMError); ok {
		return ormErr.Type == ErrorTypeTransaction
	}
	return false
}

// IsTimeoutError 判断是否是超时错误
func IsTimeoutError(err error) bool {
	if ormErr, ok := err.(*ORMError); ok {
		return ormErr.Type == ErrorTypeTimeout
	}
	return false
}

// 常见错误的预定义
var (
	// 验证错误
	ErrEmptyTableName   = NewORMError(ErrorTypeValidation, "表名不能为空")
	ErrInvalidTableName = NewORMError(ErrorTypeValidation, "表名格式不正确")
	ErrEmptySQL         = NewORMError(ErrorTypeValidation, "SQL语句不能为空")
	ErrInvalidSQL       = NewORMError(ErrorTypeValidation, "SQL语句格式不正确")

	// 查询错误
	ErrNoRows          = NewORMError(ErrorTypeNotFound, "未找到任何记录")
	ErrDuplicateRecord = NewORMError(ErrorTypeDuplicate, "记录已存在")
	ErrTooManyRows     = NewORMError(ErrorTypeQuery, "返回了过多的行数")

	// 连接错误
	ErrConnectionFailed = NewORMError(ErrorTypeConnection, "连接失败")
	ErrConnectionClosed = NewORMError(ErrorTypeConnection, "连接已关闭")

	// 事务错误
	ErrTransactionNotStarted     = NewORMError(ErrorTypeTransaction, "事务未开始")
	ErrTransactionAlreadyStarted = NewORMError(ErrorTypeTransaction, "事务已开始")
	ErrTransactionCommitFailed   = NewORMError(ErrorTypeTransaction, "事务提交失败")
	ErrTransactionRollbackFailed = NewORMError(ErrorTypeTransaction, "事务回滚失败")

	// 并发错误
	ErrDeadlock    = NewORMError(ErrorTypeConcurrency, "检测到死锁")
	ErrLockTimeout = NewORMError(ErrorTypeConcurrency, "获取锁超时")

	// 超时错误
	ErrQueryTimeout       = NewORMError(ErrorTypeTimeout, "查询超时")
	ErrTransactionTimeout = NewORMError(ErrorTypeTimeout, "事务超时")
)
