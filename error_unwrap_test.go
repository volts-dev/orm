package orm

import (
	stderrors "errors"
	"testing"

	"github.com/volts-dev/volts/errors"
)

// 事务里的业务错误必须保住自己的身份。
//
// CRUD 端点的写法是 `codec.WriteError(ctx, tx.Rollback(err))`，而上层用 errors.As
// 找链上的 *errors.Error 来判断"这是写给用户看的话"还是"未预期的内部错误"。
// sessionError 不实现 Unwrap 时链就断在这里，所有业务提示都会被换成
// "服务器内部错误（错误编号 ERR-xxxxxxxx）"——用户看不到本可以照做的那句话。
func TestSessionErrorUnwrapsToCodedError(t *testing.T) {
	coded := errors.New("", 400, "作业类型上没有配默认库位")

	wrapped := newSessionError("", coded)
	if wrapped == nil {
		t.Fatal("newSessionError 吞掉了错误")
	}

	var api *errors.Error
	if !stderrors.As(wrapped, &api) {
		t.Fatal("errors.As 穿不透 sessionError —— 带码业务错误会被当成内部错误脱敏")
	}
	if api.Code != 400 || api.Detail != "作业类型上没有配默认库位" {
		t.Fatalf("取回的不是原来那条错误: code=%d detail=%q", api.Code, api.Detail)
	}
}

// 回滚失败时这一层会同时装着业务错误与回滚错误，两条都要能被找到。
func TestSessionErrorUnwrapsAllBranches(t *testing.T) {
	coded := errors.New("", 409, "记录已被他人修改")
	rollbackErr := stderrors.New("rollback failed: connection reset")

	wrapped := newSessionError("", coded, rollbackErr)

	var api *errors.Error
	if !stderrors.As(wrapped, &api) || api.Code != 409 {
		t.Fatal("回滚也失败时，业务错误的码丢了")
	}
	if !stderrors.Is(wrapped, rollbackErr) {
		t.Fatal("回滚错误本身也该在链上")
	}
}
