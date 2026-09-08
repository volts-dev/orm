package orm

import (
	stderrors "errors"
	"os"
	"strings"
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

// ondelete=restrict 挡住删除时，那句话必须**到得了用户眼前**。
//
// 它是专门写给人看的（"先把那些记录删掉或改指向别处"，还点了名是哪个模型的哪一行），
// 但不带状态码的错误在出口会被整句换成一枚 ERR-xxxxxxxx —— 用户只知道失败、不知道
// 是谁挡着，也就无从照做。2026-09-08 真栈：卸载 loyalty 被三张礼品卡挡住，
// 界面上只有 ERR-38fd2b4b。
func TestOnDeleteRestrictErrorIsUserFacing(t *testing.T) {
	src, err := os.ReadFile("session_ondelete.go")
	if err != nil {
		t.Fatalf("读不到 session_ondelete.go: %v", err)
	}
	at := strings.Index(string(src), "still references it")
	if at < 0 {
		t.Fatal("找不到 restrict 的报错——它被改名或删了，这个测试要跟着走")
	}
	// 往前找这一句是怎么构造的：必须是带码的 errors.New，不能是裸 fmt.Errorf。
	head := string(src)[max(0, at-400) : at]
	if !strings.Contains(head, "errors.New(\"\", 400") {
		t.Error("restrict 的报错没带 400 —— 它会在出口被脱敏成 ERR-xxxxxxxx，" +
			"而这正是用户唯一能照着做的那句话")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
