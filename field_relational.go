package orm

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/domain"
	"github.com/volts-dev/utils"
)

type (
	TRelational struct {
		TField
	}

	TRelationalMultiField struct {
		TRelational
	}

	TOne2OneField struct {
		TRelational
	}

	// 主表[字段所在的表]字段值是关联表其中多条记录的集,关联表记录可以赋值给主表多条记录
	// 特性：不存储,外键在关联表,关联表有XX_id以表示记录归主表那条记录绑定
	// 例子：订单系统的主从表
	TOne2ManyField struct {
		TRelational
	}

	// 主表[字段所在的表]字段值是关联表其中之一条记录,关联表记录可以赋值给主表多条记录
	// 特性：存储,外键在主表,值只有一个,many child -> one parent 用于指定ParentID 表示本表的多条记录是关联表的某条记录的Child
	// 例子：订单系统的主从表 从表下拉选择菜单,性别
	TMany2OneField struct {
		TRelational
	}

	TMany2ManyField struct {
		TRelationalMultiField
	}
)

func init() {
	RegisterField("one2one", newOne2OneField)
	RegisterField("one2many", newOne2ManyField)
	RegisterField("many2one", newMany2OneField)
	RegisterField("many2many", newMany2ManyField)
}

func newOne2OneField() IField {
	return new(TOne2OneField)
}

func newMany2OneField() IField {
	return new(TMany2OneField)
}

// difine many2many(relate.model,ref.model,base_id,relate_id)
func newMany2ManyField() IField {
	return new(TMany2ManyField)
}

func newOne2ManyField() IField {
	return new(TOne2ManyField)
}

// defaultIdSqlType returns the SQL column type used for FK columns whose
// target model id type isn't known at Phase 1 (e.g. forward references,
// remote models). Matches the "all model ids are int64" convention;
// Phase 2 verifies this against the actual local model when present.
func defaultIdSqlType() SQLType {
	return GoType2SQLType(reflect.TypeOf(int64(0)))
}

func (self *TRelational) Attributes(ctx *TTagContext) map[string]any {
	attrs := self.Base().Attributes(ctx)
	attrs["relation"] = self.relatedModelName
	return attrs
}

func (self *TOne2OneField) Init(ctx *TTagContext) { //related_model_name string, inverse_name string
	field_Value := ctx.FieldTypeValue
	field := ctx.Field.Base()
	field.isRelated = true

	field.store = true
	field.typeName = TYPE_O2O
	params := ctx.Params

	var modelName string
	if len(params) > 0 {
		modelName = fmtModelName(utils.TitleCasedName(params[0]))
		field.relatedModelName = params[0]
		field.relationModel = params[0]
	}

	// 现在成员名是关联的Model名,Tag 为关联的字段
	model := ctx.Model
	model.Obj().SetRelationByName(modelName, field.Name())

	// Phase 1: defer parent model resolution to osv.Freeze.
	// FK SqlType defaults to int64 per the "all model ids are int64" convention
	// when the Go field type isn't directly available; Phase 2 verifies/refines this.
	if field_Value.IsValid() {
		field.SqlType = GoType2SQLType(field_Value.Type())
	} else {
		field.SqlType = defaultIdSqlType()
	}

	ctx.Orm.osv.markPending(fieldRef{
		fromModel: model.String(),
		fieldName: field.Name(),
		toModel:   modelName,
		fieldType: TYPE_O2O,
	})

	// One2One field inheritance (copying parent fields into the owning model)
	// is now done by osv.linkLocal during Freeze Phase 2 — kept out of Init so
	// registration order between modules no longer matters and so that we can
	// reject One2One→remote in Phase 3.
}

func (self *TOne2OneField) OnRead(ctx *TFieldContext) error {
	field := ctx.Field
	if !field.IsRelated() {
		return fmt.Errorf("the field %s must related field, but not %s!", field.Name(), field.TypeName())
	}

	ds, err := ctx.Model.OneToOne(ctx)
	if err != nil {
		return err
	}

	if ds != nil && ds.Count() > 0 {
		field := ctx.Field

		relateModel, err := ctx.Model.Orm().GetModel(field.RelatedModelName())
		if err != nil {
			// # Should not happen, unless the foreign key is missing.
			return err
		}

		//group := ds.GroupBy(field.RelatedKeyName())
		// 键按字符串归一,理由同 OneToMany:分组键取自对端主键列、查找键取自本表 FK 列,
		// 两列的 Go 类型不保证一致,对不上就整批读成空——不报错的错数据。
		group := groupByString(ds, relateModel.IdField())
		ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
			// 获取关联表主键
			//fieldValue := record.GetByField(field.Name())
			grp := group[utils.ToString(record.GetByField(field.RelatedKeyName()))]

			// 关联行没读回来：**跳过这一条，不能崩**。
			//
			// 下面直接 grp.Count()/grp.Record() 会对 nil 解引用，整个请求变成 panic。
			// 而"读不回来"在真实数据里很常见，且都不是本记录的错：
			//   - 悬空外键（对方被删/数据导入残留）
			//   - 空/哨兵值（本仓空 many2one 落库是 -1，不是 NULL）
			//   - 子读取被上层的行级权限或租户过滤挡掉
			// 真机 2026-08-08：res.company 的 classic 读（partner_id 是 one2one）
			// 必崩，表现为"公司菜单打不开"，日志里只有一行 recover 加一段栈。
			// 一条读不出来的关联行应当让那个字段留空，而不是让整页数据消失。
			if grp == nil || grp.Count() == 0 {
				return nil
			}

			if grp.Count() > 1 {
				return fmt.Errorf(
					"model %s's has more than 1 record for %s@%s OneToOne Id %v",
					field.RelatedModelName(), field.Name(), field.ModelName(), grp.Keys())
			}

			//record.SetByField(field.Name(), grp.Record().AsItfMap())
			for _, f := range grp.Record().Fields() {
				if !record.FieldByName(f).IsValid {
					record.SetByField(f, grp.Record().GetByField(f))
				}
			}

			return nil
		})
	}

	return nil
}

func (self *TOne2ManyField) Init(ctx *TTagContext) { //related_model_name string, inverse_name string
	field := ctx.Field
	params := ctx.Params

	log.Assert(len(params) < 2, "One2Many(%s) of model %s must including at least 2 args!", field.Name(), self.modelName)
	// self.Base()._column_type = ""
	// Field.Base()._classic_read = false
	// Field.Base()._classic_write = false
	field.Base().isRelated = true
	field.Base().store = false
	field.Base().relatedModelName = fmtModelName(utils.TitleCasedName(params[0])) //目标表
	field.Base().relatedKeyName = fmtFieldName(params[1])                         //目标表关键字段
	field.Base().relationModel = field.Base().relatedModelName
	field.Base().typeName = TYPE_O2M
}

func (self *TOne2ManyField) OnRead(ctx *TFieldContext) error {
	// 字段计算步获取任何关系值
	ctx.UseNameGet = false
	ctx.ClassicRead = false

	if self.hasGetter {
		self.getterFunc(ctx)
	} else {
		field := ctx.Field
		if !field.IsRelated() {
			return fmt.Errorf("the field %s must related field, but not %s!", field.Name(), field.TypeName())
		}

		ds, err := ctx.Model.OneToMany(ctx)
		if err != nil {
			return err
		}

		// 获得关系Model 以提供idfield。放在 ds.Count()>0 判断之外获取,因为下面
		// SetByField 现在对每条记录都无条件调用(空关联也要落一个空切片)。
		relateModel, err := ctx.Model.Orm().GetModel(field.RelatedModelName())
		if err != nil {
			return err
		}

		// group 在 ds 为空(nil 或 0 行)时也是 nil——GroupBy/Range/Count 对 nil
		// *TDataSet 均安全,故无需额外判空。
		//
		// 键按**字符串**归一后再匹配,理由同 ManyToOne:分组键取自子表反向 FK 列、
		// 查找键取自本表锚定列,两列的 Go 类型不保证一致(声明宽度不同、
		// BigNumberToString 只覆盖其中一列……)。用 any 直接做 map 键时 int64(7) 与
		// "7" 是两个键,对不上就整批 o2m 悄悄读成空数组——一个不报错的空列表。
		group := groupByString(ds, field.RelatedKeyName())
		// x2many 读出口的统一规则（m2m 的 OnRead 逐字相同，那里有完整说明）：
		//
		//	没给子规格 → 对端 id 列表
		//	给了子规格 → 对端记录列表，列范围由 ctx.Fields 限定
		//
		// 判据是 ctx.Fields 而不是 ClassicRead——经典读只说明"这是给界面看的"，
		// 说明不了要 id 还是要整条记录，而后者的 payload 差几个数量级。
		//
		// 与 m2m 的唯一差别：这里的子记录还会多带一列反向 FK(relFieldName)。它是
		// 分组回填的必需列(ensureFields 强行塞进 SELECT)，而 m2m 的对端根本没有这
		// 样一列。多一个指回父记录的键不碍事，也就不再多写一段裁剪。
		embedRecords := len(ctx.Fields) > 0
		idField := relateModel.IdField()
		// BigNumberToString 打开时,雪花 id 必须以字符串形态回填。裸 int64 经
		// JSON 传给前端会丢精度(> 2^53),x2many 的 loadSeeded 按舍入后的错 id
		// 查询会得到空结果,o2m 列表显示不出数据。这与 AsMap 对 id/外键的
		// id-as-string 边界保持一致(m2o/m2m 走 AsMap 天然已是字符串)。
		idAsStr := ctx.Model.Orm().config.BigNumberToString && isBigNumberField(relateModel.GetFieldByName(idField))
		ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
			// 继承字段用委托 FK(partner_id)的值匹配子表的反向键，否则用本模型主键。
			grp := group[utils.ToString(record.GetByField(relAnchorKey(ctx)))]
			// 无论有无关联行都调用 SetByField:此前只在 grp.Count()>0 时才设置字段值,
			// 一条关联行都没有时(如刚创建、还没有任何子行的订单/用户)整个字段 key 会从
			// 输出里彻底消失(不是空数组,是键都不存在)——前端/调用方误判为"关系字段
			// 没有返回"。空切片而非 nil,确保 AsMap/JSON 序列化为 [] 而不是 null。
			if embedRecords {
				records := make([]map[string]any, 0, grp.Count())
				grp.Range(func(pos int, sub *dataset.TRecordSet) error {
					records = append(records, sub.AsMap())
					return nil
				})
				record.SetByField(field.Name(), records)
			} else {
				records := make([]any, 0, grp.Count()) // 只保存ID
				grp.Range(func(pos int, sub *dataset.TRecordSet) error {
					idv := sub.GetByField(idField)
					if idAsStr {
						idv = utils.ToString(idv)
					}
					records = append(records, idv)
					return nil
				})
				record.SetByField(field.Name(), records)
			}

			return nil
		})
	}

	return nil
}

// OnWrite 把 Odoo 风格的 x2many 命令元组落到 comodel 上。
//
// 在此之前 one2many **完全没有写入实现**：TOne2ManyField 只重载了 OnRead，写入落到
// TField.OnWrite 的默认分支(把值原样放进 ctx.values)，而 o2m 是 store=false 字段，
// _todoCompute 对非存储字段只调 OnWrite、不收集返回值——于是整份命令被静默丢弃。
// 表现是"表单里内嵌 list 加了几行、保存提示成功、重新读出来一行都没有"，且日志里
// 没有任何指向这里的线索(product 的属性行、订单行都撞过)。
//
// 为什么走 IModel 而不是像 m2m 那样直接拼 SQL：o2m 的子行是**真实记录**，各模型对
// Create/Update/Delete 的覆写承载着业务(pro.tmpl.attr.item.Create 要据 value_ids
// 生成 pro.tmpl.attr.value 并重建变体)。绕过模型层写库，行是进去了，业务后果一个
// 都不会发生。comodel 经 ctx.Session._getModel 取得，继承调用方的事务与 schema——
// 父记录此刻往往还没提交，另起会话读不到它(本仓既有的 "NewSession 读不到未提交行")。
func (self *TOne2ManyField) OnWrite(ctx *TFieldContext) error {
	// 自定义 setter 优先：字段作者接管了写入语义。
	if self.hasSetter {
		return self.TRelational.OnWrite(ctx)
	}

	if ctx.Value == nil || len(ctx.Ids) == 0 {
		return nil
	}

	vSlice, ok := ctx.Value.([]any)
	if !ok || len(vSlice) == 0 {
		return nil
	}

	field := ctx.Field
	commands, isAllCommands := parseX2MCommands(vSlice)
	if !isAllCommands {
		// 裸 id 列表([1,2,3])等旧形态没有明确语义:当"设置为这批"会把不在列表里的子行
		// 全部解绑/删除,当"追加"又和 Odoo 不一致。历史行为是无操作,保持不变——但不再
		// 无声无息,否则调用方又要靠猜。
		log.Warnf("one2many field <%s@%s> write value is not a command list, ignored: %v",
			field.Name(), field.ModelName(), ctx.Value)
		return nil
	}

	comodel, err := ctx.Session._getModel(field.RelatedModelName())
	if err != nil {
		return err
	}

	inverse := field.RelatedKeyName()
	inverseField := comodel.GetFieldByName(inverse)
	if inverseField == nil {
		// 反向字段根本不存在 —— o2m 声明本身是错的。**必须降级成警告,不能报错。**
		//
		// 真实例子:`sys.action.view_ids` 声明成 one2many(sys.view, action_id),而
		// sys.view 上从来没有 action_id 列(Odoo 那边 view_ids 指向的是
		// ir.actions.act_window.view 这个小模型,不是 ir.ui.view;vectors 移植时接错了
		// 模型)。registry 模块的 sys_actions_views.xml 等 19 个文件都在给它写
		// `[(5,0,0),(0,0,{...})]`。
		//
		// 这类写入在本方法存在之前一直是**静默无操作**,所以模型声明错了也没人发现。
		// 一旦在这里返回 error,整条 `<record model="sys.action">` 的创建就失败,registry
		// 模块装不上 —— 实测:租户 setup 停在 "Installing modules registry..." 再无下文。
		// 把「模型声明缺陷」升级成「装不上系统」，比原来的无操作坏得多。
		//
		// 警告点名了缺陷,数据仍然不写(与历史行为一致)。真正的修法是把 view_ids 指向
		// 一个每动作一行的视图模型,那是移植层的事,不该由写入路径代劳。
		log.Warnf("one2many field <%s@%s> declares inverse <%s> which model <%s> does not have — write ignored (fix the field declaration)",
			field.Name(), field.ModelName(), inverse, field.RelatedModelName())
		return nil
	}
	// 反向键存在但不是指回本表的外键,同样是声明错误,同样只降级不报错。判据与读取侧
	// (TModel.OneToMany)保持一致:o2o 本质是带唯一约束的 m2o,列是真实存在的,可以接受。
	if t := inverseField.TypeName(); t != TYPE_M2O && t != TYPE_O2O {
		log.Warnf("one2many field <%s@%s> inverse <%s@%s> is a %s, not a many2one/one2one back-reference — write ignored",
			field.Name(), field.ModelName(), inverse, field.RelatedModelName(), t)
		return nil
	}
	// 解绑(命令 3/5/6)的落地方式取决于反向键能否为空:必填时置空这一行就是一条永远
	// 写不进去的 UPDATE(且真写进去了就是一条谁也认领不了的孤儿),此时按 Odoo 对
	// ondelete=cascade 的处理直接删除。
	detachByDelete := inverseField.Required()

	for _, cmd := range commands {
		code := utils.ToInt64(cmd[0])
		// 点名某一行的命令(1 改 / 2 删 / 3 解绑 / 4 链接)必须真的带着 id。
		//
		// 前端把一条**没有 id** 的行（列表里那种所有字段都空、只剩一个「—」的幽灵行）
		// 交给删除时，发来的就是 [2, null]。往下传的后果不是"删不掉"，而是 id 列表变空、
		// TSession.Delete 退化成"按当前域删"——而当前域只有租户/公司记录规则，于是该租户
		// 可见的**每一行**都被删掉。真机 2026-08-04 就这么丢过两批价格规则。
		if code >= 1 && code <= 4 {
			if len(cmd) < 2 || cmd[1] == nil || utils.IsBlank(cmd[1]) {
				return fmt.Errorf(
					"one2many <%s@%s>: command %d carries no record id (%v) — refusing, an id-less delete/unlink wipes every visible row",
					field.Name(), field.ModelName(), code, cmd)
			}
		}
		switch code {
		case 0: // Create (0, 0, vals)
			vals, err := x2mCommandVals(cmd, field)
			if err != nil {
				return err
			}
			for _, parentId := range ctx.Ids {
				row := make(map[string]any, len(vals)+1)
				for k, v := range vals {
					row[k] = v
				}
				row[inverse] = parentId
				if _, err := comodel.Create(&CreateRequest{Data: []any{row}}); err != nil {
					return err
				}
			}

		case 1: // Update (1, id, vals)
			vals, err := x2mCommandVals(cmd, field)
			if err != nil {
				return err
			}
			if _, err := comodel.Update(&UpdateRequest{Ids: []any{cmd[1]}, Data: []any{vals}}); err != nil {
				return err
			}

		case 2: // Delete (2, id)
			if _, err := comodel.Delete(&DeleteRequest{Ids: []any{cmd[1]}}); err != nil {
				return err
			}

		case 3: // Unlink (3, id)
			if err := o2mDetach(comodel, inverse, detachByDelete, []any{cmd[1]}); err != nil {
				return err
			}

		case 4: // Link (4, id)
			for _, parentId := range ctx.Ids {
				if _, err := comodel.Update(&UpdateRequest{
					Ids:  []any{cmd[1]},
					Data: []any{map[string]any{inverse: parentId}},
				}); err != nil {
					return err
				}
			}

		case 5: // Clear (5)
			for _, parentId := range ctx.Ids {
				current, err := o2mChildIds(comodel, inverse, parentId)
				if err != nil {
					return err
				}
				if err := o2mDetach(comodel, inverse, detachByDelete, current); err != nil {
					return err
				}
			}

		case 6: // Set (6, 0, ids)
			var wanted []any
			if len(cmd) > 2 {
				if v, ok := cmd[2].([]any); ok {
					wanted = v
				} else if cmd[2] != nil {
					wanted = []any{cmd[2]}
				}
			}
			wantedKeys := make(map[string]bool, len(wanted))
			for _, id := range wanted {
				wantedKeys[utils.ToString(id)] = true
			}
			for _, parentId := range ctx.Ids {
				current, err := o2mChildIds(comodel, inverse, parentId)
				if err != nil {
					return err
				}
				currentKeys := make(map[string]bool, len(current))
				stale := make([]any, 0, len(current))
				for _, id := range current {
					key := utils.ToString(id)
					currentKeys[key] = true
					if !wantedKeys[key] {
						stale = append(stale, id)
					}
				}
				if err := o2mDetach(comodel, inverse, detachByDelete, stale); err != nil {
					return err
				}
				for _, id := range wanted {
					if currentKeys[utils.ToString(id)] {
						continue
					}
					if _, err := comodel.Update(&UpdateRequest{
						Ids:  []any{id},
						Data: []any{map[string]any{inverse: parentId}},
					}); err != nil {
						return err
					}
				}
			}

		default:
			log.Warnf("one2many command %d not supported on field <%s@%s>", code, field.Name(), field.ModelName())
		}
	}

	return nil
}

// x2mCommandVals 取出命令元组第三位的写入值。缺失时返回空 map 而不是报错:
// (1, id) 这种"只有 id 没有值"的短写法在链路上出现过,它等价于"什么都不改"。
func x2mCommandVals(cmd []any, field IField) (map[string]any, error) {
	if len(cmd) < 3 || cmd[2] == nil {
		return map[string]any{}, nil
	}
	vals, err := toStringMap(cmd[2])
	if err != nil {
		return nil, fmt.Errorf("one2many field <%s@%s> command %v carries an unusable value: %w",
			field.Name(), field.ModelName(), cmd[0], err)
	}
	return vals, nil
}

// toStringMap 把命令携带的值归一成 map[string]any。JSON 解出来就是 map[string]any,
// 但内部调用方可能直接塞 map[string]string 或结构体。
func toStringMap(v any) (map[string]any, error) {
	switch m := v.(type) {
	case map[string]any:
		return m, nil
	case map[string]string:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[k] = val
		}
		return out, nil
	}
	rv := reflect.Indirect(reflect.ValueOf(v))
	if rv.Kind() == reflect.Struct {
		return utils.Struct2ItfMap(v), nil
	}
	return nil, fmt.Errorf("expect a map or struct, got %T", v)
}

// o2mChildIds 读出当前挂在 parentId 名下的全部子行 id。
//
// 走 comodel.Read 而不是 comodel.Records():前者经 TModel.Read→Clone 继承事务,
// 后者是一条全新连接上的会话——父记录和刚写进去的子行此刻都还没提交,读不到。
// o2mChildIds 列出挂在 parentId 名下的子行 id。
//
// **调用方拿这批 id 去删除/解绑，所以这里多返回一个都是数据损毁。** 因此本函数不
// 信任"域一定落到了 SQL 上"，而是把反向键一起读回来，在 Go 侧逐行核对归属。
//
// 真机 2026-08-04（kylin，产品模板 Prices 页）：删掉列表里的一行，四行全没了。日志：
//
//	SELECT "pro_pricelist_item"."id" FROM system.pro_pricelist_item
//	  WHERE tenant_id = $1 AND (公司可见性规则)        ← 父记录条件不见了
//	DELETE FROM "system"."pro_pricelist_item" WHERE "id" in ($1,$2,$3,$4)
//
// 那次恰好四行都属于同一个产品，所以看着像"这张列表被清空"；换个产品也有价格规则，
// 一样会被删——是**跨记录的销毁**。父记录条件为什么会丢还没查清（纯 orm 模型、
// 以及子模型带记录规则两种情形都复现不出来，见 field_o2m_scope_test.go /
// field_o2m_recordrule_test.go），但无论原因是什么，这一步都不该把整张表交出去。
func o2mChildIds(comodel IModel, inverse string, parentId any) ([]any, error) {
	// 父 id 为空时读出来的必然是整张表。宁可报错也不能让调用方拿去删。
	if parentId == nil || utils.IsBlank(parentId) {
		return nil, fmt.Errorf(
			"one2many: refusing to list children of %s with a blank parent id — the caller deletes/detaches what this returns",
			comodel.String())
	}

	idField := comodel.IdField()
	ds, err := comodel.Read(&ReadRequest{
		Domain: domain.New(inverse, "=", parentId),
		// 反向键一起读回来，下面自己核对——只读 id 的话，域丢了也看不出来。
		Fields: []string{idField, inverse},
		Limit:  -1,
	})
	if err != nil {
		return nil, err
	}
	if ds == nil || ds.Count() == 0 {
		return nil, nil
	}

	want := utils.ToString(parentId)
	ids := make([]any, 0, ds.Count())
	foreign := 0
	ds.Range(func(_ int, rec *dataset.TRecordSet) error {
		if utils.ToString(rec.GetByField(inverse)) != want {
			foreign++
			return nil
		}
		ids = append(ids, rec.GetByField(idField))
		return nil
	})
	if foreign > 0 {
		// 走到这里说明域没落到 SQL 上。已经挡住了，但必须留声——否则下次只会以
		// 另一种形式再炸一遍。
		log.Warnf("one2many: read of %s children ignored the parent filter (%s = %v); "+
			"dropped %d row(s) belonging to other parents before the caller could delete them",
			comodel.String(), inverse, parentId, foreign)
	}
	return ids, nil
}

// o2mDetach 把子行从父记录上摘下来:反向键必填时删除,否则置空外键留下记录本身。
//
// 置空这一支必须用会话的 Nullable():_separateValues 判定"调用方碰过这个字段"的依据
// 是值非 nil,所以 {inverse: nil} 走普通 Update 会被整条丢掉——解绑变成静默无操作,
// 正是本函数所在的这次修复要根除的那类故障。代价是模型层的 Update 覆写不会触发;
// 单纯摘外键没有业务语义可言,而"看着执行了其实没写"要坏得多。
func o2mDetach(comodel IModel, inverse string, byDelete bool, ids []any) error {
	if len(ids) == 0 {
		return nil
	}
	if byDelete {
		_, err := comodel.Delete(&DeleteRequest{Ids: ids})
		return err
	}
	_, err := comodel.Tx().
		Nullable(inverse).
		Ids(ids...).
		Write(map[string]any{inverse: nil})
	return err
}

func (self *TMany2OneField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	params := ctx.Params
	fieldValue := ctx.FieldTypeValue

	if fieldValue.IsValid() {
		field.SqlType = GoType2SQLType(fieldValue.Type())
	} else {
		// Defer: FK column type defaults to int64 per id convention.
		field.SqlType = defaultIdSqlType()
	}

	// 不直接指定 采用以下tag写法
	// field:"many2one() int()"
	//lField.initMany2One(lTag[1:]...)	fld._classic_read = true // 预先设计是false
	//fld.Base()._classic_write = true
	log.Assert(len(params) < 1, "Many2One(%s) of model %s must including at least 1 args!", field.Name(), self.modelName)
	field.isRelated = true
	field.relatedModelName = fmtModelName(utils.TitleCasedName(params[0])) //目标表
	field.relationModel = field.Base().relatedModelName
	field.typeName = TYPE_M2O
	field.store = true

	ctx.Orm.osv.markPending(fieldRef{
		fromModel: ctx.Model.String(),
		fieldName: field.Name(),
		toModel:   field.relatedModelName,
		fieldType: TYPE_M2O,
	})
}

// groupByString 按 field 列分组,并把分组键归一成字符串。
//
// dataset.GroupBy 的键是列的**原始值**,类型随驱动、随列声明、随输出格式化器而变。
// 关系读取每一处都是"拿 A 表某列的值去 B 表某列的分组里查",两列的 Go 类型只要不
// 一致就永远查不中,而查不中的表现是空关系——不报错的错数据。
func groupByString(ds *dataset.TDataSet, field string) map[string]*dataset.TDataSet {
	group := ds.GroupBy(field)
	out := make(map[string]*dataset.TDataSet, len(group))
	for key, grp := range group {
		out[utils.ToString(key)] = grp
	}
	return out
}

// isBlankRelationId 判断一个 many2one 外键值是否表示「没有关联」。
//
// 除 NULL 与零值外还必须认**非正数哨兵**:省略 many2one 的 create/导入会把列落成 -1
// 而不是 NULL(调用方常见写法 `SetDefaultByName("company_id", -1)`),记录规则那边同样
// 按 `company_id < 1` 判定「无归属」。id 在本 ORM 恒为正,故数值 <1 一律视为未设置。
// 认不出来的后果是每读一批就为这些行走一遍「找不到 comodel 记录」的分支并打警告。
//
// 非数值字符串(m2o 声明在 string 列上时的真实键)不在此列,原样交给匹配逻辑。
func isBlankRelationId(v any) bool {
	if utils.IsBlank(v) {
		return true
	}

	switch n := v.(type) {
	case int:
		return n < 1
	case int8:
		return n < 1
	case int16:
		return n < 1
	case int32:
		return n < 1
	case int64:
		return n < 1
	case float32:
		return n < 1
	case float64:
		return n < 1
	case string:
		if id, err := utils.IsNumeric(n); err == nil {
			return id < 1
		}
	}

	return false
}

// OnRead 把 many2one 读成**经典形态**。
//
// # 输出契约(仅经典读/NameGet;plain 读回的是存储值,见下)
//
//	有关联       → map{id, name, …}   对端记录
//	没有关联     → false               对齐 Odoo 的空关系
//	悬空/不可见  → map{id}             有 id、取不到名字
//
// 关键是**只有两种形态**:false 或 map。此前是三种,且第三种随数据出现:空 FK 原值
// 不动,于是同一个字段同一条读取路径,有值时给 map、没值时给裸 int64(0)(或 -1,或
// BigNumberToString 打开时的 "")。调用方每一处都得先认标量再认 map 才能不崩,
// 认漏一处就是表单上一个字面量的 "0"、列表里一个点不开的链接。
//
// 为什么是 false 而不是 nil:nil 与「这个字段没被读」不可区分——JSON 里都是缺键/
// null。false 是 Odoo 的既有约定,domain 侧本仓也已经按它对齐(`('x','=',False)`
// 落 IS NULL,见 expr_m2o_false_test.go),前端 toM2OTuple 早就认它。
//
// 悬空 FK 给 map{id} 而不是裸 id:形态守恒。渲染结果与从前一致(前端对裸 id 与
// 只有 id 的 map 都退化成用 id 当标签),但调用方不用再为它多写一个标量分支。
func (self *TMany2OneField) OnRead(ctx *TFieldContext) error {
	field := ctx.Field
	if !field.IsRelated() {
		return fmt.Errorf("the field %s must related field, but not %s!", field.Name(), field.TypeName())
	}

	// 形态归一**只在经典读里做**。plain 读回的是存储值:裸外键。写回要用它、内部
	// 按 id 匹配也要用它,把它换成 false/map 会让"读出来再写回去"这条最常见的用法
	// 直接坏掉。ManyToOne() 本身也只在这两种模式下才真去查对端。
	if !ctx.ClassicRead && !ctx.UseNameGet {
		return nil
	}

	ds, err := ctx.Model.ManyToOne(ctx)
	if err != nil {
		return err
	}

	relateModel, err := ctx.Model.Orm().GetModel(field.RelatedModelName())
	if err != nil {
		return err
	}
	idField := relateModel.IdField()

	// 按**字符串**归一后再匹配。GroupBy 的键是 comodel 主键列的原始值,而这里拿来
	// 匹配的是主表 FK 列的原始值——两列的 Go 类型不保证一致(FK 声明成 string 的
	// many2one、驱动把 BigInt 给成不同宽度的整型等)。用 any 直接做 map 键时,
	// int64(7) 与 "7" 是两个不同的键,匹配不上就悄悄退化成裸 id。
	//
	// ds 为空(一批记录的 FK 全为空,或对端一条都不可见)时 GroupBy 回 nil,range
	// nil map 安全——**不能**因此跳过下面的 Range:空 FK 的归一正是在那里做的,
	// 提前返回就等于"整批都没值时反而退回旧形态"。
	group := ds.GroupBy(idField)
	byId := make(map[string]*dataset.TDataSet, len(group))
	for key, grp := range group {
		byId[utils.ToString(key)] = grp
	}

	var missing []string
	ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
		fieldValue := record.GetByField(field.Name())
		// 空 FK 是 many2one 的常态(可空字段、还没选值的新记录)。
		// isBlankRelationId 认得 false 本身,重复读取幂等。
		if isBlankRelationId(fieldValue) {
			record.SetByField(field.Name(), false)
			return nil
		}

		grp := byId[utils.ToString(fieldValue)]
		if grp.Count() == 0 {
			// 悬空 FK(目标行已删)/该行当前会话不可见(租户、记录规则)。这条记录
			// 只回 id 是可接受的降级,但**绝不能返回 error**:Range 一遇 error 就
			// 整个中断,同一批里它之后的记录会全部丢掉内嵌子记录,只剩裸 id,而调用
			// 方(_read)只把错误记进日志、请求照常 200 返回。表现就是"列表里前几行
			// 的 many2one 显示名称,后面全变成一串数字 id"。
			missing = append(missing, utils.ToString(fieldValue))
			record.SetByField(field.Name(), map[string]any{idField: fieldValue})
			return nil
		}

		record.SetByField(field.Name(), grp.Record().AsMap())
		return nil
	})

	if len(missing) > 0 {
		log.Warnf("%s@%s ManyToOne: %d record(s) reference missing/invisible %s row(s) %v, left as id-only",
			field.Name(), field.ModelName(), len(missing), field.RelatedModelName(), missing)
	}
	/*
		model, err := ctx.Session.Orm().osv.GetModel(self.RelatedModelName())
		if err != nil {
			// # Should not happen, unless the foreign key is missing.
			return err
		}

		ds := ctx.Dataset
		if ctx.Session.IsClassic && ds != nil {
			//# evaluate name_get() as superuser, because the visibility of a
			//# many2one field value (id and name) depends on the current record's
			//# access rights, and not the value's access rights.
			//   value_sudo = value.sudo()
			//# performance trick: make sure that all records of the same
			//# model as value in value.env will be prefetched in value_sudo.env
			// value_sudo.env.prefetch[value._name].update(value.env.prefetch[value._name])
			ds.First()
			for !ds.Eof() {
				// 获取关联表主键
				rel_id := ds.FieldByName(self.Name()).AsInterface()

				// igonre blank value
				if utils.IsBlank(rel_id) {
					if self._attr_required {
						return fmt.Errorf("the Many2One field(%s@%s) value is required!", self.modelName, self.Name())
					}

					ds.Next()
					continue
				}

				rel_ds, err := model.NameGet([]interface{}{rel_id})
				if err != nil {
					return err
				}

				id_field := model.IdField() // get the id field name
				ds.FieldByName(self.Name()).AsInterface([]interface{}{rel_ds.FieldByName(id_field).AsInterface(), rel_ds.FieldByName("name").AsInterface()})

				ds.Next()
			}
		}*/

	return nil
}

func (self *TMany2OneField) OnWrite(ctx *TFieldContext) error {
	value := ctx.Value

	/* 读取默认 */
	if utils.IsBlank(value) {
		value = ctx.Field.Default()
		if utils.IsBlank(value) {
			if fn := ctx.Field.DefaultFunc(); fn != nil {
				if err := fn(ctx); err != nil {
					return err
				}
				value = ctx.Value
			}
		}
	}

	switch v := value.(type) {
	case string:
		/* 空字符串不进行Name搜索 */
		if v == "" {
			return nil
		}

		// 处理值为名称转为ID
		model, err := ctx.Model.Orm().GetModel(self.RelatedModelName(), WithContext(ctx.Model.Options().Context))
		if err != nil {
			return err
		}

		ds, err := model.NameSearch(v, nil, "", 1, "", nil)
		if err != nil {
			return err
		}

		if ds.Count() > 0 {
			ctx.SetValue(ds.Record().GetByField(model.IdField()))
		} else {
			if id, has := ctx.Session.CacheNameIds[v]; has {
				ctx.SetValue(id)
				break
			}

			/* 如果是命名者 有权根据名称创建记录 且关联模型支持RecordName */
			if ctx.Field.IsNameField() {
				if recName := model.GetRecordName(); recName != "" {
					model.Tx(ctx.Session.Clone())
					ids, err := model.Create(&CreateRequest{
						Context: ctx.Context,
						Data: []any{map[string]any{
							recName: v,
						}},
					})
					if err != nil {
						return err
					}

					ctx.SetValue(ids[0])

					if ctx.Session.CacheNameIds == nil {
						ctx.Session.CacheNameIds = make(map[string]any)
					}
					ctx.Session.CacheNameIds[v] = ids[0]
				}
			}

		}
	case []any:
		if len(v) > 0 {
			ctx.SetValue(v[0])
		}
	default:
		// 不修改
		ctx.SetValue(ctx.Value)
		//return fmt.Errorf("%s@%s OnWrite many2one failed with value:%v", field.Name(), ctx.Model.String(), v)
	}

	return nil
}

func (self *TMany2ManyField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	params := ctx.Params

	//	field._column_type = "" //* not a store field
	field.isRelated = true
	field.store = false
	model_name := fmtModelName(utils.TitleCasedName(field.ModelName())) // 字段归属的Model

	cnt := len(params)
	if cnt == 0 {
		log.Panicf("Many2Many(%s) of model %s must including at least 1 args!", field.Name(), field.ModelName())
	}

	related_model := fmtModelName(utils.TitleCasedName(params[0])) // 字段链接的Model
	field.relatedModelName = related_model                         //目标表
	field.relationModel = related_model
	field.typeName = TYPE_M2M

	switch cnt {
	case 1:
		// many2many(关联表)
		middle_model := fmt.Sprintf("%s.%s.rel", model_name, related_model)      // 表字段关系的Model
		field.joinModelName = middle_model                                       //提供目标表格关系的表
		field.joinSourceKey = fmtFieldName(fmt.Sprintf("%s_id", model_name))     // 关系表关键字段
		field.relatedKeyName = fmtFieldName(fmt.Sprintf("%s_id", related_model)) //目标表关键字段

	case 2:
		// many2many(关联表,关系表)
		middle_model := fmtModelName(utils.TitleCasedName(params[1]))            // 表字段关系的Model
		field.joinModelName = middle_model                                       //提供目标表格关系的表
		field.joinSourceKey = fmtFieldName(fmt.Sprintf("%s_id", model_name))     // 关系表关键字段
		field.relatedKeyName = fmtFieldName(fmt.Sprintf("%s_id", related_model)) //目标表关键字段

	case 3:
		// many2many(关联表,字段1,字段2)
		middle_model := fmt.Sprintf("%s.%s.rel", model_name, related_model) // 表字段关系的Model
		field.joinModelName = middle_model                                  //提供目标表格关系的表
		field.joinSourceKey = fmtFieldName(params[1])                       // 关系表关键字段
		field.relatedKeyName = fmtFieldName(params[2])                      //目标表关键字段

	case 4:
		// many2many(关联表,关系表,字段1,字段2)
		middle_model := fmtModelName(utils.TitleCasedName(params[1])) // 表字段关系的Model
		field.joinModelName = middle_model                            //提供目标表格关系的表
		field.joinSourceKey = fmtFieldName(params[2])                 // 关系表关键字段
		field.relatedKeyName = fmtFieldName(params[3])                //目标表关键字段

	default:
		log.Panicf("field %s of model %s must format like 'Many2Many(relate_model)' or 'Many2Many(relate_model,model_id,relate_model_id)'!", field.Name(), self.modelName)
	}

	ctx.Orm.osv.middleModel.Store(field.joinModelName, true)
}

// 创建关联表
// model, columns
//
// DDL 与模型注册分两段、各自幂等：
//   - DDL 按 (schema, 关联表) 去重执行——m2m 关联表必须在**每个**用到它的 schema 里
//     各建一份（schema 隔离租户如 VectorsSystem 的 system schema 有自己的一套 T 表，
//     关联表也不例外）。此前用「关系模型是否已注册」做全局守卫，进程内第一个（几乎
//     必然是 public 的）同步建完表后，其他 schema 的同步全部被跳过——system 的
//     res_user_group_rel 之类关联表从未建出来，而读写又因不带 schema 落到 public，
//     两个 bug 互相掩盖成"看似能跑但数据串库"。
//   - 模型注册仍全局一次（模型元数据与 schema 无关）。
func (self *TMany2ManyField) UpdateDb(ctx *TTagContext) {
	orm := ctx.Orm
	field := ctx.Field

	// 同步会话携带目标 schema；注册期(RegisterModel)无会话 → 默认 schema。
	schema := ""
	if ctx.Session != nil {
		schema = ctx.Session.Schema
	}

	middle_model := strings.Replace(field.JoinModelName(), ".", "_", -1)

	// DDL：按 (schema, 表) 每进程一次；CREATE ... IF NOT EXISTS 本身幂等，重启安全。
	ddlKey := schema + "|" + middle_model
	if _, done := orm.osv.middleModelDDL.LoadOrStore(ddlKey, true); !done {
		idField := ctx.Model.GetFieldByName(ctx.Model.IdField())
		sqlType := orm.dialect.GetSqlType(idField)
		id1 := field.RelatedKeyName()
		id2 := field.JoinSourceKey()

		// 关联表引用必须带 schema 前缀（qualifiedMiddle），否则落到 search_path
		// 默认 schema。索引名不可加前缀（PG 索引天然归属其表所在 schema）。
		qualifiedMiddle := `"` + middle_model + `"`
		if schema != "" {
			qualifiedMiddle = `"` + schema + `"."` + middle_model + `"`
		}

		// Run each DDL statement independently — SQLite/MySQL don't support
		// multi-statement Exec, unnamed CREATE INDEX, or PG's COMMENT ON TABLE.
		stmts := []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s ("%s" %s NOT NULL, "%s" %s NOT NULL, UNIQUE("%s","%s"))`,
				qualifiedMiddle, id1, sqlType, id2, sqlType, id1, id2),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s_%s_idx" ON %s ("%s")`,
				middle_model, id1, qualifiedMiddle, id1),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s_%s_idx" ON %s ("%s")`,
				middle_model, id2, qualifiedMiddle, id2),
		}
		if strings.EqualFold(orm.dialect.DBType(), "postgres") {
			stmts = append(stmts, fmt.Sprintf(`COMMENT ON TABLE %s IS '%s'`,
				qualifiedMiddle, fmt.Sprintf("RELATION BETWEEN %s AND %s", self.modelName, middle_model)))
		}
		// DDL 必须走**调用方那条会话**，不能另开连接。
		//
		// TOrm.SyncModel 把整批模型的建表放在一个事务里（orm.go: session.Begin /
		// Commit），而 orm.Exec 是另一条连接、自动提交。只要关联表同时也是一张
		// **模型表**——Odoo 那种 mailing.list.contact_ids ↔ mailing.subscription 的
		// 形状，m2m 的中间表本身就是个有主键有字段的真模型——同步事务刚在这张表上
		// 建过索引、持着锁不放，这里的 CREATE INDEX 就会永远等下去：
		// 没有超时、没有报错，日志停在上一条 SQL，进程活着但端口永不监听。
		// （2026-08-25 mass_mailing 首次真栈安装踩到，pg_blocking_pids 实锤。）
		//
		// 走同一条会话就没有跨连接的锁等待，关联表也随同步事务一起提交/回滚；
		// 顺带还让 schema 与模型建表一致（此前 RegisterModel 路径无会话，
		// 关联表一律落在默认 schema）。会话为 nil 时（非同步期调用）回落 orm.Exec。
		exec := func(q string) error {
			if ctx.Session != nil {
				_, err := ctx.Session.Exec(q)
				return err
			}
			_, err := orm.Exec(q)
			return err
		}
		for _, q := range stmts {
			if err := exec(q); err != nil {
				log.Errf("m2m create table '%s' failure : SQL:%s,\nError:%s", ctx.Field.RelatedModelName(), q, err.Error())
			}
		}

		self.update_db_foreign_keys(ctx)
	}

	// 模型注册：全局一次。
	if _, has := orm.osv.models.Load(field.JoinModelName()); has {
		return
	}

	// 新建模型
	relModel := new(TModel)
	model_val := reflect.Indirect(reflect.ValueOf(relModel))
	model_type := model_val.Type()
	model, err := orm._modelMetas(ctx.Model.Transaction(), newModel(field.JoinModelName(), middle_model, model_val, model_type, nil))
	if err != nil {
		log.Err(err)
	}
	// 注册model —— 带上本会话，让它自己的衍生 DDL 也留在同一个事务里（见上）。
	if err = orm.osv.RegisterModel("", model.GetBase(), ctx.Session); err != nil {
		log.Err(err)
	}
}

// 设置字段获得的值
// TODO :未完成
func (self *TMany2ManyField) OnRead(ctx *TFieldContext) error {
	// 自定义 getter 优先，与 TOne2ManyField.OnRead 对齐。
	//
	// ★ 少了这一段的表现是**静默返回空数组**：字段声明成
	// `ManyToManyField(...).Store(false).Getter(fn)`，getter **一次也不会被调**，
	// 因为分类阶段 `field.IsRelated()` 先命中，读取就直接走关系表查询——而那张
	// 关系表对非存储字段永远是空的。于是"算出来的关联集"恒空，请求成功、无日志。
	// o2m 一直有这条分支，m2m 没有，两者的差别没有任何理由。
	// 2026-08-26 由 crm.lead.duplicate_lead_ids 撞出来（角标显示"1 条疑似重复"，
	// 点开永远是空的）；calendar.event 的 invalid_email_partner_ids /
	// unavailable_partner_ids 也一直死在这里。
	if self.hasGetter {
		ctx.UseNameGet = false
		ctx.ClassicRead = false
		if err := self.getterFunc(ctx); err != nil {
			return err
		}
		// getter 只管算出 id 列表，**id 的 JSON 形态由这里统一收口**——与下面
		// 非 getter 那条路的输出契约必须一样。少了这一步，getter 里那句
		// `SetByField(name, []int64{...})` 会让 19 位雪花 id 以裸 number 下发，
		// 浏览器 JSON.parse 的那一瞬间末几位就没了（判别指纹：id 以多个 0 结尾），
		// 之后拿它去查一律查不到、界面表现为"点开什么都没有"，不报错不打日志。
		normalizeX2mIds(ctx)
		return nil
	}

	field := ctx.Field
	if !field.IsRelated() {
		return fmt.Errorf("the field %s must related field, but not %s!", field.Name(), field.TypeName())
	}

	ds, err := ctx.Model.ManyToMany(ctx)
	if err != nil {
		return err
	}

	// ManyToMany 回的是**关系表**的查询结果，不是对端记录：
	//
	//   ClassicRead  → `SELECT mid.*, rel.*`，一行里混着关系表的列(main_id/tag_id)
	//                  与对端的列(id/name)，两张表的字段拼在同一个 map 里；
	//   非 ClassicRead → `SELECT mid.tag_id, mid.main_id`，**完全没有对端数据**，
	//                  调用方拿到的是 [{main_id:1, tag_id:2}]，连名字都没有。
	//
	// 两种都不是能交给调用方的形状，这里重新构造。
	//
	// # 输出契约（与 OneToMany 逐字相同）
	//
	//	没给子规格 → 对端 **id 列表**（[]any，空关联是空切片不是 nil）
	//	给了子规格 → 对端**记录**列表（[]map），列范围由 ctx.Fields 限定
	//
	// 判据是 ctx.Fields 而**不是** ctx.ClassicRead。此前 m2m 用的是 ClassicRead：
	// 经典读一律内嵌整条对端记录，而同一次经典读里 o2m 回的却是 id 列表——同一类
	// 字段、同一次请求，两种形状。ReadRequest.SubFields 的文档写的一直是这条
	// （"many2many标签 -> 子记录列表，仅含 Fields 指定列"），实现没跟上。
	//
	// 为什么默认是 id 而不是记录：一次经典列表读会把每行每个 m2m 的对端整条塞进
	// 结果，行数一多就是几个数量级的 payload（Odoo 的 read() 对 x2many 一律回 ids，
	// 正是这个理由）。要名字就给子规格，一句话的事，而且那时列范围也由调用方定。
	//
	// 只改输出构造、不动 SQL：ManyToMany 还有若干直接消费其数据集的调用方
	// （vectors 的 recordrule / user_groups / config_settings_group 都按
	// srcKey/relKey 读 junction 行），改列名会连带改掉那个契约。代价是经典读、
	// 无子规格时仍会 JOIN 出一批用不上的对端列——那次 JOIN 同时也是字段 domain
	// 唯一生效的地方（见 ManyToMany 的两个分支），不能顺手砍掉。
	relateModel, err := ctx.Model.Orm().GetModel(field.RelatedModelName())
	if err != nil {
		return err
	}
	srcKey := field.JoinSourceKey()  // 关系表里指向本表的列，如 main_id
	relKey := field.RelatedKeyName() // 关系表里指向对端的列，如 tag_id
	// 对端**自己**若真有同名列，就不能摘——那是它的数据。
	dropSrc := relateModel.GetFieldByName(srcKey) == nil
	dropRel := relateModel.GetFieldByName(relKey) == nil

	// 子规格给了就内嵌记录，没给就回 id 列表。同 OneToMany 的 embedRecords。
	embedRecords := len(ctx.Fields) > 0
	// 子规格点名的列（外加对端主键——没有它调用方拿到的记录无从回指）。
	// SQL 那边取的是 rel.*，这里按名字裁到子规格的范围，契约与 o2m 一致。
	var wanted map[string]bool
	if embedRecords {
		wanted = make(map[string]bool, len(ctx.Fields)+1)
		for _, name := range ctx.Fields {
			wanted[name] = true
		}
		wanted[relateModel.IdField()] = true
	}
	// BigNumberToString 打开时雪花 id 必须以字符串回填，理由同 OneToMany：
	// 裸 int64 经 JSON 传给前端会丢精度(> 2^53)，之后按舍入后的错 id 查恒空。
	idAsStr := ctx.Model.Orm().config.BigNumberToString &&
		isBigNumberField(relateModel.GetFieldByName(relateModel.IdField()))

	// 按 source 侧(本表)的 join key 分组，才能用本记录 id 命中。
	// RelatedKeyName 是 comodel 侧的键(如 res_company_id)，用它分组会与下方
	// 本表 id 的查找键不在同一键空间，导致永远查空。
	// group 在 ds 为空(nil 或 0 行)时也是 nil——GroupBy/Range/Count 对 nil *TDataSet
	// 均安全,故下面无需额外判空。键按字符串归一,理由见 OneToMany。
	group := groupByString(ds, srcKey)
	ctx.Dataset.Range(func(pos int, record *dataset.TRecordSet) error {
		// 继承字段用委托 FK(partner_id)的值匹配 junction 的 source 键，否则用本模型主键。
		fieldRecord := group[utils.ToString(record.GetByField(relAnchorKey(ctx)))]

		// 无论有无关联行都调用 SetByField:此前只在 fieldRecord.Count()>0 时才设置字段值,
		// 一条关联行都没有时(如用户未分配任何 company/group)整个字段 key 会从输出里
		// 彻底消失(不是空数组,是键都不存在)——前端/调用方误判为"关系字段没有返回"。
		// 空切片而非 nil,确保 AsMap/JSON 序列化为 [] 而不是 null。
		if embedRecords {
			records := make([]map[string]any, 0, fieldRecord.Count())
			fieldRecord.Range(func(_ int, row *dataset.TRecordSet) error {
				m := row.AsMap()
				if dropSrc {
					delete(m, srcKey)
				}
				if dropRel {
					delete(m, relKey)
				}
				for name := range m {
					if !wanted[name] {
						delete(m, name)
					}
				}
				records = append(records, m)
				return nil
			})
			record.SetByField(field.Name(), records)
		} else {
			records := make([]any, 0, fieldRecord.Count())
			fieldRecord.Range(func(_ int, row *dataset.TRecordSet) error {
				idv := row.GetByField(relKey)
				if idAsStr {
					idv = utils.ToString(idv)
				}
				records = append(records, idv)
				return nil
			})
			record.SetByField(field.Name(), records)
		}

		return nil
	})

	return nil
}

// write relate data to the reference table
// isM2MCommandTuple 判断单个值是否为 Odoo 风格的命令元组, 兼容长短两种写法:
//
//	完整三元组 (code, id, value)  如 (4, id, 0)
//	精简二元组 (code, id)         如 (4, id)  —— 前端常用
//
// 返回原元组及是否匹配。
func isM2MCommandTuple(val any) ([]any, bool) {
	if s, ok := val.([]any); ok && (len(s) == 2 || len(s) == 3) {
		code := utils.ToInt64(s[0])
		if code >= 0 && code <= 6 {
			return s, true
		}
	}
	return nil, false
}

// parseM2MCommands 尝试把切片整体解析成命令元组列表。
// 仅当所有元素都是合法命令元组时 isAllCommands 才为 true;
// 任一元素不是命令(如普通 id 列表 [1,2,3])则返回 false, 交由旧版逻辑处理。
func parseM2MCommands(vSlice []any) (commands [][]any, isAllCommands bool) {
	isAllCommands = len(vSlice) > 0
	for _, v := range vSlice {
		if cmd, ok := isM2MCommandTuple(v); ok {
			commands = append(commands, cmd)
		} else {
			isAllCommands = false
			return nil, false
		}
	}
	return commands, isAllCommands
}

// parseX2MCommands 与 parseM2MCommands 同义，但额外接受单元素的 (5) —— m2m 侧的
// 解析器是按"前端只发二/三元组"写死的，o2m 的 Clear 没有 id 也没有值，写成 [5] 是
// 合法的 Odoo 形态。仍然是"全部元素都是命令才算命令列表"，任一元素不是(如裸 id 列表
// [1,2,3])就整体返回 false，交由调用方决定。
func parseX2MCommands(vSlice []any) (commands [][]any, isAllCommands bool) {
	if len(vSlice) == 0 {
		return nil, false
	}
	for _, v := range vSlice {
		s, ok := v.([]any)
		if !ok || len(s) < 1 || len(s) > 3 {
			return nil, false
		}
		if code := utils.ToInt64(s[0]); code < 0 || code > 6 {
			return nil, false
		}
		commands = append(commands, s)
	}
	return commands, true
}

func (self *TMany2ManyField) OnWrite(ctx *TFieldContext) error {
	/* 空值不处理 */
	if ctx.Value == nil {
		return nil
	}

	// 支持 Odoo 风格的 Command Tuple
	if vSlice, ok := ctx.Value.([]any); ok {
		// 尝试作为 Command 列表解析
		if commands, isAllCommands := parseM2MCommands(vSlice); isAllCommands {
			for _, cmd := range commands {
				code := utils.ToInt64(cmd[0])
				switch code {
				case 3: // Unlink (3, id, 0)
					if err := self.unlink_all(ctx, []any{cmd[1]}); err != nil {
						return err
					}
				case 4: // Link (4, id, 0)
					if err := self.link(ctx, []any{cmd[1]}); err != nil {
						return err
					}
				case 5: // Clear (5, 0, 0)
					if err := self.unlink_all(ctx, nil); err != nil {
						return err
					}
				case 6: // Set (6, 0, ids)
					var ids []any
					if len(cmd) < 3 {
						// 缺少第三元素的 Set 等价于 Clear
					} else if v, ok := cmd[2].([]any); ok {
						ids = v
					} else {
						ids = []any{cmd[2]} // Fallback if it's a single element
					}

					if err := self.unlink_all(ctx, nil); err != nil { // 先清除所有关联
						return err
					}
					if len(ids) > 0 {
						if err := self.link(ctx, ids); err != nil {
							return err
						}
					}
				default:
					log.Warnf("M2M command %d not fully supported yet", code)
				}
			}
			return nil
		}
	}

	// TODO　更多类型
	// 支持一下几种M2M数据类型 (旧版逻辑兼容)
	var ids []any
	switch v := ctx.Value.(type) {
	case []int:
		ids = utils.ToAnySlice(v...)
	case []int64:
		ids = utils.ToAnySlice(v...)
	case []any:
		ids = v
	default:
		return log.Errf("M2M field name <%s> could not support this type of value %v", ctx.Field.Name(), ctx.Value)
	}

	if len(ids) > 0 {
		// TODO 读比写快 不删除原有数据 直接读取并对比再添加
		// unlink all the relate record on the ref. table
		err := self.unlink_all(ctx, ids)
		if err != nil {
			return err
		}

		// relink record from new data
		err = self.link(ctx, ids)
		if err != nil {
			return err
		}
	}

	return nil
}

// # beware of duplicates when inserting
func (self *TMany2ManyField) link(ctx *TFieldContext, ids []any) error {
	quoter := ctx.Session.Orm().dialect.Quoter()
	field := ctx.Field

	// 关联表写入必须带会话 schema：schema 隔离租户(如 VectorsSystem 的 system)的
	// link/unlink 若用裸表名，会写进 search_path 默认 schema(public)的同名表——
	// 数据跨 schema 串库(写不进自己租户的表，还污染共享表)。
	middle_table_name := quoter.QuoteTable(ctx.Session.Schema, strings.Replace(field.JoinModelName(), ".", "_", -1))
	for _, rec_id := range ctx.Ids {
		for _, relate_id := range ids {
			query := fmt.Sprintf(
				`INSERT INTO %v (%s, %s) VALUES (?,?) ON CONFLICT DO NOTHING`,
				middle_table_name, quoter.Quote(field.JoinSourceKey()), quoter.Quote(field.RelatedKeyName()),
			)

			/*
			   	query := fmt.Sprintf(`INSERT INTO %s (%s, %s)
			                           (SELECT a, b FROM unnest(array[%s]) AS a, unnest(array[%s]) AS b)
			                           EXCEPT (SELECT %s, %s FROM %s WHERE %s IN (%s))`,
			   		middle_table_name, field.RelatedKeyName(), field.JoinSourceKey(),
			   		rec_id, strings.Join(ids, ","),
			   		field.RelatedKeyName(), field.JoinSourceKey(), middle_table_name, field.RelatedKeyName(), rec_id,
			   	)
			*/

			// 必须用内部的 _exec，不能用公开的 Exec：Exec 在 IsAutoClose 的会话上
			// `defer self.Close()`，而 Close 会把 db/tx 置 nil。这里是循环，第一次
			// 调用就把会话关了，第二次直接空指针崩溃——`orm.Model(x).Create(带 m2m 值)`
			// 因此必然 panic（OnWrite 先 unlink_all 再 link，两次就够）。
			// vectors 侧一直用 model.Records()/Tx()(IsAutoClose=false)，才没撞上。
			_, err := ctx.Session._exec(query, rec_id, relate_id)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// TODO 错误将IDS删除基数
// # remove all records for which user has access rights
func (self *TMany2ManyField) unlink_all(ctx *TFieldContext, ids []any) error {
	quoter := ctx.Session.Orm().dialect.Quoter()
	quote := quoter.Quote
	field := ctx.Field
	// 同 link：删除也必须限定会话 schema，否则删的是 public 同名表的行。
	middle_table_name := quoter.QuoteTable(ctx.Session.Schema, strings.Replace(field.JoinModelName(), ".", "_", -1))

	ctxIdsSql := strings.Repeat("?,", len(ctx.Ids)-1) + "?"

	var b strings.Builder
	fmt.Fprintf(&b, "DELETE FROM %s WHERE %s.%s IN (%s) ",
		middle_table_name, middle_table_name, quote(field.JoinSourceKey()), ctxIdsSql)

	args := append([]any{}, ctx.Ids...)
	if len(ids) > 0 {
		relIdsSql := strings.Repeat("?,", len(ids)-1) + "?"
		fmt.Fprintf(&b, " AND %s.%s IN (%s)", middle_table_name, quote(field.RelatedKeyName()), relIdsSql)
		args = append(args, ids...)
	}

	// 提交修改
	session := ctx.Session // orm.NewSession()
	// 同 link：走内部 _exec，公开的 Exec 会在 IsAutoClose 会话上把会话关掉，
	// 后续任何一次使用都是空指针。
	_, err := session._exec(b.String(), args...)
	if err != nil {
		return err
	}

	return nil
}

// Add the foreign keys corresponding to the field's relation table.
func (self *TMany2ManyField) update_db_foreign_keys(ctx *TTagContext) {
	/*        cr = model._cr
	          comodel = model.env[self.related_model_name]
	          reflect = model.env['ir.model.constraint']._reflect_constraint
	          # create foreign key references with ondelete=cascade, unless the targets are SQL views
	          if sql.table_kind(cr, model._table) != 'v':
	              sql.add_foreign_key(cr, self.relation, self.column1, model._table, 'id', 'cascade')
	              reflect(model, '%s_%s_fkey' % (self.relation, self.column1), 'f', None, self._module)
	          if sql.table_kind(cr, comodel._table) != 'v':
	              sql.add_foreign_key(cr, self.relation, self.column2, comodel._table, 'id', 'cascade')
	              reflect(model, '%s_%s_fkey' % (self.relation, self.column2), 'f', None, self._module)
	*/
}

// normalizeX2mIds 把 x2many 自定义 getter 写回的 id 列表统一成读出口的形态：
// BigNumberToString 打开且对端主键是大整数时，一律转成字符串。
//
// 只动**数字**元素：getter 也可能按"给了子规格就回记录"的契约写回 []map，那种原样保留。
func normalizeX2mIds(ctx *TFieldContext) {
	if ctx == nil || ctx.Dataset == nil || ctx.Model == nil || ctx.Field == nil {
		return
	}
	if !ctx.Model.Orm().config.BigNumberToString {
		return
	}
	relateModel, err := ctx.Model.Orm().GetModel(ctx.Field.RelatedModelName())
	if err != nil || relateModel == nil {
		return
	}
	if !isBigNumberField(relateModel.GetFieldByName(relateModel.IdField())) {
		return
	}

	name := ctx.Field.Name()
	ctx.Dataset.Range(func(_ int, record *dataset.TRecordSet) error {
		switch list := record.GetByField(name).(type) {
		case []int64:
			out := make([]any, 0, len(list))
			for _, v := range list {
				out = append(out, utils.ToString(v))
			}
			record.SetByField(name, out)
		case []any:
			out := make([]any, 0, len(list))
			for _, v := range list {
				switch v.(type) {
				case int, int32, int64, uint, uint32, uint64, float32, float64:
					out = append(out, utils.ToString(v))
				default:
					out = append(out, v)
				}
			}
			record.SetByField(name, out)
		}
		return nil
	})
	ctx.Dataset.First()
}
