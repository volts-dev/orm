package orm

import (
	"context"
	stdErrors "errors"
	"fmt"
	"time"

	ormerr "github.com/volts-dev/orm/errors"
)

// 临时模型（transient）。
//
// Odoo 的 TransientModel 是"向导/导入任务这类用完就丢的行，超过 _transient_max_hours
// 由 vacuum 定期清掉"。本仓此前没有这个概念，vectors 里 base_import、change_password
// 向导、phone_blacklist 等四五处只能各自手工清或者干脆留着（"本仓 orm 没有 transient
// 机制，这张表必须有人清"）。
//
// 职责切分：orm 只负责**声明**（哪些模型是临时的、保留多久）和**清理原语**
//（VacuumTransient / VacuumTransients）；什么时候跑交给应用层的调度器——orm 没有、
// 也不该有后台任务运行器。
//
// 判旧依据是模型上带 `created` 标签的字段（Create 时由 orm 自动写入）。没有这列的
// 临时模型无从判断年龄，VacuumTransient 直接报 ErrNoCreatedField，而不是清空整表。

// DefaultTransientMaxHours 临时记录的默认保留时长（小时）。与 Odoo 的 _transient_max_hours 同值。
const DefaultTransientMaxHours = 1.0

// transientCreatedField 找出模型上带 created 标签的字段名；没有返回空串。
func transientCreatedField(model IModel) string {
	for _, f := range model.GetFields() {
		if f != nil && f.IsCreatedAt() {
			return f.Name()
		}
	}
	return ""
}

// VacuumTransient 删除本会话所绑定模型里已过期的临时记录：
//
//	created < now - TransientMaxHours
//
// now 缺省取 time.Now()，显式传入便于测试与"按某个基准时刻清理"。返回删除的行数。
// 模型未声明 transient → ErrNotTransient；没有 created 字段 → ErrNoCreatedField。
//
// 走的是普通 Delete 路径：BeforeSession 钩子（多租户过滤）、ondelete 级联、m2m 关联
// 表清理全部照常。会话上的其他条件（Where/Domain）与年龄条件 AND 叠加。
func (self *TSession) VacuumTransient(now ...time.Time) (int64, error) {
	model := self.Statement.Model
	if model == nil {
		return 0, ErrTableNotFound
	}
	if !model.IsTransient() {
		return 0, ormerr.New(ormerr.ErrNotTransient,
			fmt.Errorf("model %s is not declared transient (table tag `transient` or Builder().TableTransient())", model.String()))
	}
	createdField := transientCreatedField(model)
	if createdField == "" {
		return 0, ormerr.New(ormerr.ErrNoCreatedField,
			fmt.Errorf("transient model %s has no `created` tag field; cannot tell how old a record is", model.String()))
	}

	base := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		base = now[0]
	}
	hours := model.TransientMaxHours()
	if hours <= 0 {
		hours = DefaultTransientMaxHours
	}
	cutoff := base.Add(-time.Duration(hours * float64(time.Hour)))

	return self.Where(createdField+"<?", cutoff).Delete()
}

// VacuumTransients 对已注册的每个临时模型各跑一次 VacuumTransient。
//
// 返回 模型名 → 删除行数。一个模型失败不影响其余模型；所有失败经 errors.Join 汇总返回，
// 调用方可用 errors.Is 判别具体类型。每个模型用独立会话，用完即关。
func (self *TOrm) VacuumTransients(ctx context.Context) (map[string]int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := make(map[string]int64)
	var errs []error
	for _, name := range self.osv.GetModels() {
		model, err := self.GetModel(name)
		if err != nil || model == nil {
			continue // 取不到的模型（远程壳、未挂模块）不是临时模型的候选
		}
		if !model.IsTransient() {
			continue
		}
		sess := self.NewSession().WithContext(ctx)
		n, err := sess.Model(name).VacuumTransient()
		sess.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		result[name] = n
	}
	return result, stdErrors.Join(errs...)
}
