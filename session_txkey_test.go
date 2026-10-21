package orm

import "testing"

// 不在事务里（含 nil 会话）回 nil：调用方据此退回「不借外层」的行为。
func TestTxKey_NilOutsideTransaction(t *testing.T) {
	var s *TSession
	if s.TxKey() != nil {
		t.Fatal("nil session must have no tx key")
	}
	if (&TSession{IsAutoCommit: true}).TxKey() != nil {
		t.Fatal("autocommit session must have no tx key")
	}
}
