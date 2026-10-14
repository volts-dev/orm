package orm

// 删除观察者：宿主挂一个函数，orm 每真删掉一批记录就同步告诉它「模型 X 的这些 id 没了」。
//
// # 为什么要有它
//
// 有一类东西挂在记录上却不经外键：Odoo 用 (res_model, res_id) 指过去的那几张表——讨论串的
// 消息、关注者、活动、计划消息。ondelete 管不了它们（那是 many2one 的事），模型自己的
// Delete 覆写也管不全：
//
//   - 级联（session_ondelete.go）**绕过子模型的 Delete 覆写直接发 SQL**，删项目时被级联
//     删掉的任务，任务自己的覆写一次都不会被调；
//   - 模块代码里大量 `tx.Model("x").Delete(id)` 直接走会话，同样不经覆写。
//
// 上游 Odoo 是在 BaseModel.unlink / mail.thread.unlink 里做这件事，覆盖一切删除路径。本仓与
// 之等价的唯一汇合点就是这里的 TSession.Delete。
//
// # 契约
//
//   - 在主 DELETE（以及级联删掉的每一批子行）**之后**、同一个会话（同一个事务）里调。
//     返回 error 则这次 Delete 返回 error，调用方回滚。
//   - 交给观察者的会话是 Clone 出来的（共享事务、schema、行级范围），观察者在上面
//     Query / Exec / Model(...) 都不会把调用方的会话指到别的表上；用完由这里 Close。
//   - ids 是**这次真删掉的**（按 id 删时经过了会话条件筛选，级联时是真正接手的那批），
//     每批不超过 cascadeBatchSize 个。
//   - 观察者每次删除都会被调，便宜的判断（"这个模型我管不管"）放在最前面。
type DeleteObserver func(s *TSession, model, table string, ids []any) error

// deletedGroup 是一批被删掉的记录。
type deletedGroup struct {
	model string
	table string
	ids   []any
}

// SetDeleteObserver 装上删除观察者（nil 卸下）。整个进程一个；要挂多个由宿主自己分发。
func (self *TOrm) SetDeleteObserver(fn DeleteObserver) {
	if fn == nil {
		self.osv.deleteObserver.Store(nil)
		return
	}
	self.osv.deleteObserver.Store(&fn)
}

// notifyDeleted 把这次删除（级联的子行在前、主记录在后）逐批报给观察者。
func (self *TSession) notifyDeleted(groups []deletedGroup) error {
	if self.orm == nil || self.orm.osv == nil {
		return nil
	}
	p := self.orm.osv.deleteObserver.Load()
	if p == nil {
		return nil
	}
	for _, g := range groups {
		if g.model == "" || len(g.ids) == 0 {
			continue
		}
		for _, batch := range batchIds(g.ids) {
			s := self.Clone()
			s.IsAutoClose = false
			err := (*p)(s, g.model, g.table, batch)
			s.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
