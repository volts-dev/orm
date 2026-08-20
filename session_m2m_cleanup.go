package orm

import (
	"fmt"
	"strings"
)

// m2m 关联表清理：删掉一条记录时，把它在所有 m2m 关联表里的行一并删掉。
//
// **这不是 `ondelete` tag 的语义。** Odoo 的 ondelete 是 many2one 上的策略
// (cascade/restrict/set null)；m2m 关联行的清除在 Odoo 里由关联表两列上的
// `ON DELETE CASCADE` 外键完成，与 ondelete tag 无关。本仓的
// TMany2ManyField.update_db_foreign_keys 至今是空函数体——关联表根本没有外键
// 约束，所以这件事必须由 orm 显式做，否则孤儿行**持续积累**（不是某次事故的残留）。
//
// **两个方向都要清，且第二个才是会真出事的那个：**
//
//   - 被删模型自己声明的 m2m 字段 → 清关联表的 JoinSourceKey 列；
//   - 别的模型指向被删模型的 m2m 字段 → 清关联表的 RelatedKeyName 列。
//     删掉一个 res.group，sys_menu_group_rel 里"菜单还在、组没了"的行会让那个菜单
//     被限制到一个没人持有的组上，**对所有人永久隐身**；只清第一个方向等于没修。
//     2026-08-20 权限普查实测：vectors 五张权限关系表 10~13% 是孤儿行。
//
// 清理失败一律只告警不报错：主记录已经删掉了，把"垃圾没清干净"升级成"删除失败"
// 会让调用方以为记录还在。唯一的例外是事务——见 relationTableExists 上的说明。

type (
	// m2mRelationRef 描述「某张 m2m 关联表里指向某个模型的那一列」。
	m2mRelationRef struct {
		joinModel string // 关联表的模型名（表名 = 点换下划线）
		column    string // 关联表里指向目标模型的列
	}

)

// m2mRefsOf 返回所有「有一列指向 modelName」的 m2m 关联表列。
//
// 名字先按原样精确查，查不到再退化到 fmtModelName 规整后重查——与 osv.GetModel
// 的查找口径一致：字段声明侧的 RelatedModelName 已被 fmtModelName 规整过，
// 而注册名可能是 snake_case 的表名式写法。
func (self *TOsv) m2mRefsOf(modelName string) []m2mRelationRef {
	if modelName == "" {
		return nil
	}

	index := self.m2mRefIndex.Load()
	if index == nil {
		index = self.buildM2MRefIndex()
	}
	if index == nil {
		return nil
	}

	if refs := (*index)[modelName]; len(refs) > 0 {
		return refs
	}
	if formatted := fmtModelName(modelName); formatted != modelName {
		return (*index)[formatted]
	}
	return nil
}

// buildM2MRefIndex 扫一遍已注册模型，建立「模型名 → 指向它的关联表列」索引。
// 结果缓存在 osv 上，模型集合一变（RegisterModel / RemoveModel）就作废重建。
func (self *TOsv) buildM2MRefIndex() *map[string][]m2mRelationRef {
	index := make(map[string][]m2mRelationRef)

	add := func(model string, ref m2mRelationRef) {
		if model == "" || ref.joinModel == "" || ref.column == "" {
			return
		}
		for _, exist := range index[model] {
			if exist == ref {
				return // 两侧各声明一次同一张关联表是常态，别发两条一样的语句
			}
		}
		index[model] = append(index[model], ref)
	}

	self.models.Range(func(_, value any) bool {
		obj, ok := value.(*TModelObject)
		if !ok || obj == nil {
			return true
		}
		for _, field := range obj.GetFields() {
			if field == nil || field.TypeName() != TYPE_M2M {
				continue
			}
			// 源端按**声明该字段的模型**记（field.ModelName() 就是注册名），不按
			// 遍历到的 obj 记：模型对象在 inherits/合并下是共享的，拿宿主模型的名字
			// 配上别人的关联列，会用 B 的 id 去删 A 的关系行。
			add(field.ModelName(), m2mRelationRef{joinModel: field.JoinModelName(), column: field.JoinSourceKey()})
			add(field.RelatedModelName(), m2mRelationRef{joinModel: field.JoinModelName(), column: field.RelatedKeyName()})
		}
		return true
	})

	self.m2mRefIndex.Store(&index)
	return &index
}

// invalidateM2MRefIndex 在模型集合变化时作废索引（RegisterModel / RemoveModel）。
func (self *TOsv) invalidateM2MRefIndex() {
	self.m2mRefIndex.Store(nil)
}

// cleanupM2MRelations 删除 ids 在各 m2m 关联表里留下的行。
// modelName 必须在主删除之前取好：_exec 会复位 Statement。
func (self *TSession) cleanupM2MRelations(modelName string, ids []any) {
	if self.orm == nil || modelName == "" || len(ids) == 0 {
		return
	}

	refs := self.orm.osv.m2mRefsOf(modelName)
	if len(refs) == 0 {
		return
	}

	quoter := self.orm.dialect.Quoter()
	holder := idsToSqlHolder(ids...)
	for _, ref := range refs {
		table := strings.Replace(ref.joinModel, ".", "_", -1)
		if !self.relationColumnUsable(table, ref.column) {
			continue
		}

		// 关联表写入必须带会话 schema，否则落到 search_path 的默认库(public)——
		// schema 隔离租户(如 VectorsSystem 的 system)会删错表。同 link/unlink_all。
		sql := fmt.Sprintf(`DELETE FROM %s WHERE %s IN (%s)`,
			quoter.QuoteTable(self.Schema, table),
			quoter.QuoteIdentMust(ref.column),
			holder)
		if _, err := self._exec(sql, ids...); err != nil {
			log.Warnf("delete %s: clean up relation table %s.%s failed: %v", modelName, table, ref.column, err)
		}
	}
}

// relationColumnUsable 判断关联表**和那一列**在当前 schema 里都真实存在。
//
// 为什么先探测而不是直接发语句、失败了再告警：在事务里，一条报错的语句会把整个事务
// 判死（postgres: current transaction is aborted），连带调用方那次删除一起回滚——
// 清垃圾不该有这种代价。
//
// 为什么连列一起查：同一张关联表两侧各有一份声明，列名是从**声明方的模型名**推出来的
// （TMany2ManyField.Init）。两侧只要有一侧用了四参形式自定义列名而另一侧没有，推出的
// 列名就与库里对不上，DELETE 直接 42703。这类不一致在运行期完全无声——m2m 的读写只走
// 声明它的那一侧，从来不会两边对账。
//
// 用 dbMetas 的快照而不是自己拼 information_schema：它按会话 schema 取、按 metaEpoch
// 缓存（任何 DDL 都会让它失效，于是"升级时新建的关联表"下一次就看得见），而且是方言
// 无关的——sqlite 上根本没有 information_schema。
func (self *TSession) relationColumnUsable(table, column string) bool {
	_, snapshot, err := self.orm.dbMetas(self)
	if err != nil {
		// 探测不出来就不清：留下孤儿只是回到修之前的状态，而在事务里发一条会报错的
		// 语句会把调用方那次删除一起搭进去。
		log.Warnf("m2m cleanup: introspect schema %q failed, skip cleaning %s.%s: %v", self.Schema, table, column, err)
		return false
	}
	return snapshot.Columns(table)[column]
}
