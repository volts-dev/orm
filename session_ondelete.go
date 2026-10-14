package orm

import (
	"context"
	"fmt"
	"strings"

	"github.com/volts-dev/volts/errors"
)

// many2one 的 `ondelete` 策略：删掉一条记录时，怎么处置**指向它的**那些行。
//
// 在 Odoo 里这件事由关联列上的外键 `ON DELETE ...` 完成；本仓的关系字段从来不建外键
// 约束，所以必须由 orm 显式做。在此之前 `onDelete` 只有两处赋值、**读它的一处都没有**
// （tag.go 那行注释里还留着 `TAG_ON_DELETE = "ondelete" // TODO`），于是全仓 100 多处
// 声明是死配置：删掉一张销售单，它的订单行原样留在库里，谁也不指向它了——
// 2026-08-20 真栈清点：`sale_order_line` 30 行、`account_move_line` 5 行这样的孤儿。
//
// # 三条策略，与 Odoo 同义
//
//	cascade   删掉所有指向它的行（并递归处理那些行自己的子行）
//	restrict  只要还有行指向它，就**拒绝删除**并报错
//	set null  把指向它的那一列置空
//
// # 三条刻意的边界
//
//   - **只认显式声明。** 没写 ondelete 的 many2one 一概不动（Odoo 的默认是 set null，
//     本仓 900 多个 m2o 绝大多数没声明——套上默认等于每次删除都要扫全库改列，
//     既是巨大的行为变更也是没必要的开销）。要级联就把 ondelete 写出来。
//   - **绕过子模型的 Delete 覆写，直接发 SQL。** 与 Odoo 一致（那边是数据库在做级联，
//     Python 的 unlink 同样不触发）。这也是唯一安全的做法：子模型的业务守卫
//     （"已过账的凭证不许删"之类）若在级联中途拒绝，父记录已经删了一半，留下的
//     是比孤儿更糟的半截状态。
//   - **跨进程管不了。** 索引只包含**本进程注册过**的模型。三进程拓扑下，别的进程里
//     指向本模型的 cascade 声明在这里看不见，那些子行仍会变成孤儿——这是分布式的
//     固有限制，不是这段代码能补的。
type m2oRef struct {
	model  string // 声明该 m2o 字段的模型名（即"子"模型）
	table  string // 子表名
	column string // 指向被删模型的那一列
	idCol  string // 子表主键列
	policy string // 规范化后的策略：cascade / restrict / set null
	// required 为真时 set null 会撞 NOT NULL，执行前降级为告警跳过。
	required bool
	// foreign：这一列在**别的进程**的模型上（宿主经 SetForeignRefs 告诉我们的），不在本进程
	// 的模型集合里 —— 列存不存在要直接问库，不能看启动时的库结构快照（它可能是运行期才补的）。
	foreign bool
	// companyDependent：这一列是按公司区分的 jsonb（{"<公司>": id, ...}），每一格都可能
	// 指向被删的记录。「指向」按每一格的文本值比（数字与字符串两种写法都有：前端的雪花号
	// 走字符串），set null 是删掉那几格而不是整列置空（别的公司的值不相干）。
	companyDependent bool
}

const (
	onDeleteCascade  = "cascade"
	onDeleteRestrict = "restrict"
	onDeleteSetNull  = "set null"
)

// cascadeMaxDepth 级联递归的层数上限。自引用 cascade（分类树、菜单树、移动链）是
// 合法用法，但数据成环时递归不会自己停——visited 集合已经挡住了环，这层上限是
// 兜底，超过就报错而不是继续删：宁可删不掉，也不要删到一半停在中间。
const cascadeMaxDepth = 32

// cascadeBatchSize 单条语句里 IN 列表的最大 id 数。
const cascadeBatchSize = 500

// normalizeOnDelete 把声明里的写法规整成三个常量之一；认不出来的一律返回空
// （＝不介入，保持修改之前的行为）。**认不出就不动**是这里唯一安全的失败方向：
// 把拼错的 "cascad" 当成 cascade 会删掉真实数据。
func normalizeOnDelete(raw string) string {
	s := strings.ToLower(strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "'\"")))
	s = strings.Join(strings.Fields(s), " ") // 折叠中间的多余空白
	switch s {
	case "cascade":
		return onDeleteCascade
	case "restrict":
		return onDeleteRestrict
	case "set null", "set_null", "setnull", "null":
		return onDeleteSetNull
	}
	return ""
}

// m2oRefsOf 返回所有「声明了 ondelete 且指向 modelName」的 many2one/one2one 列。
// 名字先按原样查，再按 fmtModelName 规整后重查——与 osv.GetModel 的查找口径一致。
func (self *TOsv) m2oRefsOf(modelName string) []m2oRef {
	if modelName == "" {
		return nil
	}

	index := self.m2oRefIndex.Load()
	if index == nil {
		index = self.buildM2ORefIndex()
	}
	if index == nil {
		return nil
	}

	refs := (*index)[modelName]
	if len(refs) == 0 {
		if formatted := fmtModelName(modelName); formatted != modelName {
			refs = (*index)[formatted]
		}
	}
	return append(refs[:len(refs):len(refs)], self.foreignRefsOf(modelName)...)
}

// ForeignRef 是一条「别的进程的模型上、指向本进程某个模型的多对一列」。
//
// 三进程拓扑下，指向本模型的列可能在别的进程的模型上（vectors 的扩展字段：sale 给
// res.partner 加的列指向 pro.pricelist，res.partner 在 kylin、pro.pricelist 在 salesvc）。
// 本进程的索引只看得见自己注册的模型，于是删一张价格表，指向它的那一列没人管。
// 而数据按租户分片、同一租户的表都在同一个库里，这些列的 ondelete 本进程照样能直接执行。
type ForeignRef struct {
	Model            string // 声明这一列的模型（它的表名按 fmtTableName 推）
	Column           string
	OnDelete         string // "set null" / "restrict" / "cascade"
	Required         bool
	CompanyDependent bool

	// RelationTable 非空时这是一个 many2many：Column 是这张关联表里指向 target 的列。
	// 删掉 target 的记录就删掉关联表里指向它们的行（同本进程 m2m 的清理，见
	// session_m2m_cleanup.go），与 OnDelete 无关。
	RelationTable string
}

// SetForeignRefs 装上「别的进程的模型上、指向 target 的多对一列」的来源，删除时与本进程
// 的索引一并执行 ondelete。fn 每次删除都会被调，由宿主自己缓存。
func (self *TOrm) SetForeignRefs(fn func(target string) []ForeignRef) {
	if fn == nil {
		self.osv.foreignRefs.Store(nil)
		return
	}
	self.osv.foreignRefs.Store(&fn)
}

func (self *TOsv) foreignRefsOf(target string) []m2oRef {
	p := self.foreignRefs.Load()
	if p == nil {
		return nil
	}
	var out []m2oRef
	for _, r := range (*p)(target) {
		if r.RelationTable != "" {
			continue // many2many，见 foreignRelationRefsOf
		}
		// 本进程自己**声明**了这一列：它已经在本进程的索引里了，别重复执行。只看模型在不在
		// 不够 —— New() 会把库里已有的表反查成模型，那种模型上的列是反查来的（没有
		// ondelete），跳过它就谁也不管了。
		if declaredLocally(self, r.Model, r.Column) {
			continue
		}
		policy := normalizeOnDelete(r.OnDelete)
		if policy == "" || r.Model == "" || r.Column == "" {
			continue
		}
		if r.CompanyDependent && policy == onDeleteCascade {
			policy = onDeleteSetNull
		}
		out = append(out, m2oRef{
			model: r.Model, table: fmtTableName(r.Model), column: r.Column, idCol: "id",
			policy: policy, required: r.Required, companyDependent: r.CompanyDependent, foreign: true,
		})
	}
	return out
}

// refUsable 判断这条引用的列（与子表主键列）此刻在库里可用。
func (self *TSession) refUsable(ref m2oRef, column string) bool {
	if !ref.foreign {
		return self.relationColumnUsable(ref.table, column)
	}
	ctx := self.context
	if ctx == nil {
		ctx = context.Background()
	}
	ok, err := self.orm.dialect.IsColumnExist(ctx, self.Schema, ref.table, column)
	return err == nil && ok
}

// buildM2ORefIndex 扫一遍已注册模型，建立「被指向的模型名 → 指向它的列」索引。
// 与 m2m 那份同构：结果缓存在 osv 上，模型集合一变就作废重建。
func (self *TOsv) buildM2ORefIndex() *map[string][]m2oRef {
	index := make(map[string][]m2oRef)

	self.models.Range(func(_, value any) bool {
		obj, ok := value.(*TModelObject)
		if !ok || obj == nil {
			return true
		}
		idCol := obj.uidFieldName
		if idCol == "" {
			idCol = "id"
		}
		for _, field := range obj.GetFields() {
			if field == nil {
				continue
			}
			switch field.TypeName() {
			case TYPE_M2O, TYPE_O2O:
			default:
				continue
			}
			policy := normalizeOnDelete(field.Base().onDelete)
			if policy == "" {
				continue
			}
			// 子模型按**声明该字段的模型**记（field.ModelName() 是注册名），不按遍历到
			// 的 obj 记：模型对象在 inherits/合并下是共享的，拿宿主模型的表名配上别人
			// 的列，会去删无辜的表。同 m2m 索引里的那条判断。
			owner := field.ModelName()
			target := field.RelatedModelName()
			if owner == "" || target == "" || field.Name() == "" {
				continue
			}
			ref := m2oRef{
				model:            owner,
				table:            fmtTableName(owner),
				column:           field.Name(),
				idCol:            idCol,
				policy:           policy,
				required:         field.Required(),
				companyDependent: IsCompanyDependent(field),
			}
			if ref.companyDependent && ref.policy == onDeleteCascade {
				// 某家公司的价格表被删了，就把整个客户删掉？不可能是本意。按 set null 处理
				// （只删掉指向它的那几格），并点名让声明改掉。
				log.Warnf("ondelete: %s.%s is company_dependent; 'cascade' makes no sense there, treating it as 'set null'", owner, field.Name())
				ref.policy = onDeleteSetNull
			}
			dup := false
			for _, exist := range index[target] {
				if exist == ref {
					dup = true
					break
				}
			}
			if !dup {
				index[target] = append(index[target], ref)
			}
		}
		return true
	})

	self.m2oRefIndex.Store(&index)
	return &index
}

// invalidateM2ORefIndex 在模型集合变化时作废索引（RegisterModel / RemoveModel）。
func (self *TOsv) invalidateM2ORefIndex() {
	self.m2oRefIndex.Store(nil)
}

// applyOnDelete 在主记录被删除**之前**执行 ondelete 策略。
//
// 顺序是刻意的：**先把整棵树的 restrict 都检查完，再动手改任何一行**。restrict 的语义
// 是"拒绝这次删除"，若边删边查，等发现拦路的那一行时前面的级联已经删掉了。
//
// modelName / ids 必须由调用方在主删除之前取好：_exec / _query 都会复位 Statement。
//
// 返回级联真正删掉的那几批（模型 → id），给删除观察者（见 session_delete_observer.go）：
// 级联绕过子模型的 Delete 覆写直接发 SQL，观察者是它们唯一的"被删了"通知。
func (self *TSession) applyOnDelete(modelName string, ids []any) ([]deletedGroup, error) {
	if self.orm == nil || modelName == "" || len(ids) == 0 {
		return nil, nil
	}
	visited := map[string]bool{}
	if err := self.checkOnDeleteRestrict(modelName, ids, 0, visited); err != nil {
		return nil, err
	}
	var cascaded []deletedGroup
	_, err := self.applyOnDeleteWrites(modelName, ids, 0, map[string]bool{}, &cascaded)
	return cascaded, err
}

// checkOnDeleteRestrict 递归检查整棵级联树上的 restrict：任何一处还有行指向要被删掉的
// 记录，就报错返回。走 cascade 边是因为级联下去的那些行同样会被删掉，它们身上的
// restrict 一样要认。
func (self *TSession) checkOnDeleteRestrict(modelName string, ids []any, depth int, visited map[string]bool) error {
	if len(ids) == 0 {
		return nil
	}
	if depth > cascadeMaxDepth {
		return fmt.Errorf("ondelete: cascade depth limit (%d) exceeded at model %s — refusing to delete", cascadeMaxDepth, modelName)
	}

	fresh := markVisited(modelName, ids, visited)
	if len(fresh) == 0 {
		return nil
	}

	for _, ref := range self.orm.osv.m2oRefsOf(modelName) {
		if !self.refUsable(ref, ref.column) {
			continue
		}
		switch ref.policy {
		case onDeleteRestrict:
			blocking, err := self.referencingIds(ref, fresh, 1)
			if err != nil {
				return err
			}
			if len(blocking) > 0 {
				// ★ 带 400：这句话是**写给人看的**（"先把那些记录删掉或改指向别处"），
				// 不带码的话它在出口会被当成"未预期的内部错误"整句换成一枚
				// ERR-xxxxxxxx，用户只知道失败、不知道是谁挡着。2026-09-08 真栈：
				// 卸载 loyalty 被三张礼品卡挡住，界面上只有 ERR-38fd2b4b。
				return errors.New("", 400, fmt.Sprintf(
					"cannot delete %s: %s.%s still references it (ondelete='restrict'; e.g. %s id %v) — remove or reassign those records first",
					modelName, ref.model, ref.column, ref.model, blocking[0]))
			}
		case onDeleteCascade:
			childIds, err := self.referencingIds(ref, fresh, 0)
			if err != nil {
				return err
			}
			if err := self.checkOnDeleteRestrict(ref.model, childIds, depth+1, visited); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyOnDeleteWrites 执行 set null 与 cascade。**先递归到叶子再往回删**，这样每一层
// 删除时它自己的子行已经不在了。
//
// 返回的是这一层**真正接手处理**的那批 id（去掉了上层已经在处理的）。调用方只删这些：
// 自引用成环时（A.parent=B、B.parent=A）查出来的"子行"可能正是上层要删的那一行，
// 照单全删会把主删除的目标先删掉，主 DELETE 随后影响 0 行、打一条莫名其妙的警告。
func (self *TSession) applyOnDeleteWrites(modelName string, ids []any, depth int, visited map[string]bool, cascaded *[]deletedGroup) ([]any, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if depth > cascadeMaxDepth {
		return nil, fmt.Errorf("ondelete: cascade depth limit (%d) exceeded at model %s — refusing to delete", cascadeMaxDepth, modelName)
	}

	fresh := markVisited(modelName, ids, visited)
	if len(fresh) == 0 {
		return nil, nil
	}

	quoter := self.orm.dialect.Quoter()
	for _, ref := range self.orm.osv.m2oRefsOf(modelName) {
		if !self.refUsable(ref, ref.column) {
			continue
		}
		switch ref.policy {
		case onDeleteSetNull:
			if ref.required {
				// 声明冲突：必填列置空必然撞 NOT NULL。跳过并点名，别把"删除"整个搞失败。
				log.Warnf("delete %s: %s.%s declares ondelete('set null') but is required — skipped (fix the declaration)",
					modelName, ref.model, ref.column)
				continue
			}
			for _, batch := range batchIds(fresh) {
				where, args := self.refPredicate(ref, batch)
				set := quoter.QuoteIdentMust(ref.column) + " = NULL"
				if ref.companyDependent {
					// 只删掉值是这批 id 的那几格；jsonb - text[] 删多个键。
					col := quoter.QuoteIdentMust(ref.column)
					set = fmt.Sprintf(`%s = %s - ARRAY(SELECT e.key FROM jsonb_each(%s) AS e WHERE (e.value #>> '{}') IN (%s))`,
						col, col, col, idsToSqlHolder(args...))
					args = append(append([]any{}, args...), args...)
				}
				sql := fmt.Sprintf(`UPDATE %s SET %s WHERE %s`,
					quoter.QuoteTable(self.Schema, ref.table), set, where)
				if _, err := self._exec(sql, args...); err != nil {
					return nil, fmt.Errorf("delete %s: ondelete('set null') on %s.%s failed: %w", modelName, ref.model, ref.column, err)
				}
			}
		case onDeleteCascade:
			childIds, err := self.referencingIds(ref, fresh, 0)
			if err != nil {
				return nil, err
			}
			if len(childIds) == 0 {
				continue
			}
			// 先处理孙辈，再删这一层；只删递归真正接手的那批（见函数头注释）
			doomed, err := self.applyOnDeleteWrites(ref.model, childIds, depth+1, visited, cascaded)
			if err != nil {
				return nil, err
			}
			if len(doomed) == 0 {
				continue
			}
			// 子行自己的 m2m 关系行也要清，否则级联删完换一种孤儿继续留着
			self.cleanupM2MRelations(ref.model, doomed)
			for _, batch := range batchIds(doomed) {
				sql := fmt.Sprintf(`DELETE FROM %s WHERE %s IN (%s)`,
					quoter.QuoteTable(self.Schema, ref.table),
					quoter.QuoteIdentMust(ref.idCol),
					idsToSqlHolder(batch...))
				if _, err := self._exec(sql, batch...); err != nil {
					return nil, fmt.Errorf("delete %s: ondelete('cascade') on %s.%s failed: %w", modelName, ref.model, ref.column, err)
				}
			}
			if cascaded != nil {
				*cascaded = append(*cascaded, deletedGroup{model: ref.model, table: ref.table, ids: doomed})
			}
			// 级联删除要留痕：从界面上看只删了一条，实际动了别的表。
			log.Infof("delete %s: ondelete('cascade') removed %d row(s) from %s (via %s)",
				modelName, len(doomed), ref.table, ref.column)
		}
	}
	return fresh, nil
}

// referencingIds 取出「指向这批 id」的子行主键。limit>0 时只取那么多（restrict 只需要
// 知道有没有）。
func (self *TSession) referencingIds(ref m2oRef, ids []any, limit int) ([]any, error) {
	if !self.refUsable(ref, ref.idCol) {
		// 主键列名对不上（inherits 共享 obj 时可能推错），宁可什么都不做
		log.Warnf("ondelete: %s has no usable id column %q, skip %s", ref.table, ref.idCol, ref.policy)
		return nil, nil
	}

	quoter := self.orm.dialect.Quoter()
	var out []any
	for _, batch := range batchIds(ids) {
		where, args := self.refPredicate(ref, batch)
		sql := fmt.Sprintf(`SELECT %s FROM %s WHERE %s`,
			quoter.QuoteIdentMust(ref.idCol),
			quoter.QuoteTable(self.Schema, ref.table),
			where)
		if limit > 0 {
			sql += fmt.Sprintf(" LIMIT %d", limit)
		}
		ds, err := self._query(sql, args...)
		if err != nil {
			return nil, fmt.Errorf("ondelete: scan %s.%s failed: %w", ref.table, ref.column, err)
		}
		if ds == nil {
			continue
		}
		ds.First()
		for !ds.Eof() {
			if v := ds.FieldByName(ref.idCol).AsInterface(); v != nil {
				out = append(out, v)
			}
			ds.Next()
		}
		if limit > 0 && len(out) >= limit {
			return out[:limit], nil
		}
	}
	return out, nil
}

// markVisited 记下「这个模型的这些 id 已经处理过」，返回其中还没处理过的那些。
// 自引用 cascade（分类树、菜单树、移动链）在数据成环时会无限递归，这是唯一能停住它的
// 东西——深度上限只是兜底。
func markVisited(modelName string, ids []any, visited map[string]bool) []any {
	fresh := make([]any, 0, len(ids))
	for _, id := range ids {
		key := modelName + "|" + fmt.Sprint(id)
		if visited[key] {
			continue
		}
		visited[key] = true
		fresh = append(fresh, id)
	}
	return fresh
}

// batchIds 把 id 列表切成不超过 cascadeBatchSize 的批次：一次 IN 塞进上万个占位符
// 会撞上驱动的参数上限。
func batchIds(ids []any) [][]any {
	if len(ids) <= cascadeBatchSize {
		return [][]any{ids}
	}
	var out [][]any
	for start := 0; start < len(ids); start += cascadeBatchSize {
		end := start + cascadeBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		out = append(out, ids[start:end])
	}
	return out
}

// refPredicate 是「ref 这一列指向 batch 里某个 id」的 WHERE 条件与它的参数。
// 按公司区分的列是 jsonb：任何一格指向它就算。
func (self *TSession) refPredicate(ref m2oRef, batch []any) (string, []any) {
	col := self.orm.dialect.Quoter().QuoteIdentMust(ref.column)
	if !ref.companyDependent {
		return fmt.Sprintf(`%s IN (%s)`, col, idsToSqlHolder(batch...)), batch
	}
	strs := make([]any, len(batch))
	for i, id := range batch {
		strs[i] = fmt.Sprint(id)
	}
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM jsonb_each(%s) AS e WHERE (e.value #>> '{}') IN (%s))`,
		col, idsToSqlHolder(strs...)), strs
}

// foreignRelationRefsOf 是别的进程的 many2many 关联表里指向 target 的列（ForeignRef.RelationTable）。
func (self *TOsv) foreignRelationRefsOf(target string) []ForeignRef {
	p := self.foreignRefs.Load()
	if p == nil {
		return nil
	}
	var out []ForeignRef
	for _, r := range (*p)(target) {
		if r.RelationTable == "" || r.Column == "" {
			continue
		}
		// 不去重：本进程的 m2m 索引若也管着这张关联表，同一条 DELETE 执行两遍，无害。
		out = append(out, r)
	}
	return out
}

// declaredLocally 报告本进程的模型 model 上是否有**声明**（不是从库里反查来）的字段 column。
func declaredLocally(osv *TOsv, model, column string) bool {
	v, ok := osv.models.Load(model)
	if !ok {
		return false
	}
	obj, ok := v.(*TModelObject)
	if !ok {
		return false
	}
	f := obj.GetFieldByName(column)
	return f != nil && f.Base() != nil && !f.Base().introspected
}
