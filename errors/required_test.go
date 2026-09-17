package errors

import (
	stderrors "errors"
	"fmt"
	"testing"
)

// 必填错误必须**穿得过包装**：业务代码随手 %w 包一层是常态，靠类型断言取字段名
// 的写法会在第一次包装时静默失效，用户那边的表现就是提示又变回 500。
func TestRequiredFields_SurvivesWrapping(t *testing.T) {
	err := New(ErrRequired, fmt.Errorf("Field name is required")).WithFields("name", "partner_id")
	wrapped := fmt.Errorf("create sys.attachment: %w", err)

	got := RequiredFields(wrapped)
	if len(got) != 2 || got[0] != "name" || got[1] != "partner_id" {
		t.Fatalf("RequiredFields = %v, want [name partner_id]", got)
	}
}

// ErrRequired 从 ErrValidation 底下分出来，但**不能把它分丢**：既有调用方判的是
// ErrValidation，那些代码一个字都不改。
func TestErrRequired_IsStillErrValidation(t *testing.T) {
	err := New(ErrRequired, nil).WithFields("name")
	if !stderrors.Is(err, ErrValidation) {
		t.Fatal("ErrRequired 必须 unwrap 到 ErrValidation，否则既有的 errors.Is 全部失效")
	}
	if !stderrors.Is(err, ErrRequired) {
		t.Fatal("errors.Is(err, ErrRequired) 认不出来")
	}
}

// 别的校验失败（外键、类型解析）底下盖着驱动原文，不能被当成"必填"放行到用户面前。
func TestRequiredFields_OtherValidationErrorsAreNotRequired(t *testing.T) {
	err := New(ErrValidation, fmt.Errorf(`pq: insert or update on table "sale_order" violates foreign key constraint`))
	if got := RequiredFields(err); got != nil {
		t.Fatalf("RequiredFields = %v, want nil —— 普通校验错误不得走非脱敏通道", got)
	}
	if got := RequiredFields(stderrors.New("boom")); got != nil {
		t.Fatalf("RequiredFields = %v, want nil", got)
	}
}
