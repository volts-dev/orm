package orm

import (
	"errors"
	"fmt"
)

type (
	sessionError struct {
		title  string
		errors []error
	}
)

var (
	ErrNoMapPointer          = errors.New("mp should be a map's pointer")
	ErrNoStructPointer       = errors.New("mp should be a struct's pointer")
	ErrParamsType      error = errors.New("Params type error")
	ErrTableNotFound   error = errors.New("Not found table")
	ErrUnSupportedType error = errors.New("Unsupported type error")
	ErrNotExist        error = errors.New("Not exist error")
	ErrCacheFailed     error = errors.New("Cache failed")
	ErrNeedDeletedCond error = errors.New("Delete need at least one condition")
	ErrNotImplemented  error = errors.New("Not implemented.")
	ErrDeleteFailed    error = errors.New("Delete Failed.")
	ErrInvalidSession  error = errors.New("The session of query is invalid!")
)

// 接受多个错误 如果0错误返回nil
func newSessionError(title string, errs ...error) error {
	e := &sessionError{
		title:  title,
		errors: make([]error, 0),
	}

	for _, err := range errs {
		if err != nil {
			e.errors = append(e.errors, err)
		}
	}

	if len(e.errors) == 0 {
		return nil
	}

	return e
}

func (self sessionError) Error() string {
	return fmt.Sprintf("%s:%v ", self.title, self.errors)
}

// Unwrap 让 errors.Is / errors.As 穿透这一层。
//
// **没有它，事务里的每一条业务错误都会丢掉自己的身份。** `TSession.Rollback(err)`
// 把调用方的错误装进 sessionError 再返回，而 CRUD 端点是
// `codec.WriteError(ctx, tx.Rollback(err))` —— 上层用 `errors.As` 找链上的
// `*errors.Error` 来决定"这是开发者写给用户看的话（原样下发）还是未预期的内部错误
// （换成通用文案 + 引用码）"。链断在这里，于是**所有**在 Create/Update/Delete 覆写里
// 返回的带码提示，用户看到的都是"服务器内部错误，请稍后再试（错误编号 ERR-xxxxxxxx）"，
// 原文只留在服务端日志里。2026-08-20 真栈实测：stock 的"作业类型上没有配默认库位"
// 就是这么被糊掉的。
//
// 返回 []error 而不是单个：这一层本来就聚合了业务错误与回滚失败两条，Go 1.20 起
// errors.Is/As 支持多分支遍历。
func (self *sessionError) Unwrap() []error {
	return self.errors
}
