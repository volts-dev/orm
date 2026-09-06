package orm

import (
	"time"

	"github.com/volts-dev/orm/errors"
)

// softDeleteMode controls how Read filters soft-deleted records.
type softDeleteMode int

const (
	// softDeleteFilterActive is the zero-value default: Read automatically
	// adds "deleted_at IS NULL" to exclude soft-deleted records.
	softDeleteFilterActive softDeleteMode = iota

	// softDeleteIncludeAll disables filtering: Read returns all records.
	softDeleteIncludeAll

	// softDeleteOnlyDeleted inverts the filter: Read returns only
	// soft-deleted records (e.g. trash/recycle-bin views).
	softDeleteOnlyDeleted
)

// softDeleteClause 返回本次会话应追加到 WHERE 上的软删除过滤条件（标识符已引用），
// 模型没有 `deleted` 标签字段、或调用方 IncludeDeleted() 时返回空串。
//
// 读路径（_readFromDatabase）与聚合路径（ReadGroup）**必须**共用这一份实现：两边
// 一旦漂移，列表视图里看不见的记录就会出现在报表合计里。
func (self *TSession) softDeleteClause() string {
	deletedField := self.Statement.Model.Obj().DeletedField
	if deletedField == "" {
		return ""
	}

	quoted := self.orm.dialect.Quoter().QuoteIdentMust(deletedField)
	switch self.softDeleteMode {
	case softDeleteFilterActive:
		return quoted + " IS NULL"
	case softDeleteOnlyDeleted:
		return quoted + " IS NOT NULL"
	}
	return "" // softDeleteIncludeAll：不过滤
}

// IncludeDeleted disables the soft-delete auto-filter for this session.
// Subsequent Read calls return both active and soft-deleted records.
func (self *TSession) IncludeDeleted() *TSession {
	self.softDeleteMode = softDeleteIncludeAll
	return self
}

// OnlyDeleted sets the soft-delete filter to return only soft-deleted records.
// Useful for recycle-bin / restore-list views.
func (self *TSession) OnlyDeleted() *TSession {
	self.softDeleteMode = softDeleteOnlyDeleted
	return self
}

// SoftDelete marks the matched records as deleted by writing the current
// timestamp to the model's `deleted` tag field.
//
// Requires a row-locating condition (Ids / Where / Domain) unless AllowUnsafe()
// is set. Returns ErrNoSoftDelete if the model has no `deleted` tag field.
func (self *TSession) SoftDelete(ids ...any) (int64, error) {
	obj := self.Statement.Model.Obj()
	if obj.DeletedField == "" {
		return 0, errors.ErrNoSoftDelete
	}
	if err := self.Statement.Err(); err != nil {
		return 0, err
	}

	if len(ids) > 0 {
		self.Statement.IdParam = append(self.Statement.IdParam, ids...)
	}

	if !self.allowUnsafe && !self.hasCondition() {
		return 0, errors.ErrUnsafe
	}

	// For mass soft-delete (AllowUnsafe + no condition), fetch all IDs first
	// so _write's internal condition requirement is satisfied.
	if self.allowUnsafe && len(self.Statement.IdParam) == 0 && self.Statement.domain.Count() == 0 {
		allIds, _, err := self._search("", nil)
		if err != nil {
			return 0, err
		}
		if len(allIds) == 0 {
			return 0, nil
		}
		self.Statement.IdParam = allIds
	}

	return self.Write(map[string]any{
		obj.DeletedField: time.Now(),
	})
}
