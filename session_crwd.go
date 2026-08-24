package orm

import (
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/orm/errors"
	"github.com/volts-dev/utils"
)

// Create 在指定 model 上插入一条或多条记录，支持传入多个 src（变参/数组）。
// 返回值：单条（或不传 src 走 Sets）时返回该记录的 id；传入多条时返回 []any 形式的 id 列表。
// 如需 classic 模式，先链式调 session.Classic()；
// 如需自定义 ctx，先链式调 session.WithContext(ctx)。
func (self *TSession) Create(src ...any) (uid []any, err error) {
	model := self.Statement.Model
	self.Op = OpCreate
	if _, err := model.BeforeSession(self); err != nil {
		return nil, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, ErrInvalidSession
	}

	return self._create(src...)
}

// Read 在 Statement 配置的条件下读取记录集。
// 如需 classic 模式，先链式调 session.Classic()；
// 如需自定义 ctx，先链式调 session.WithContext(ctx)。
func (self *TSession) Read() (*TDataset, error) {
	model := self.Statement.Model
	self.Op = OpRead
	if _, err := model.BeforeSession(self); err != nil {
		return nil, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return nil, ErrInvalidSession
	}

	// 守卫放在 BeforeSession **之后**：多租户/行级权限是靠那个钩子往会话上追加条件
	// 实现的，放在前面会把它们追加的条件当成不存在，把正常读取一律拦下。
	if err := self.guardUnscopedRead(); err != nil {
		return nil, err
	}

	return self._read()
}

// TODO 接受多值 dataset
// TODO 当只有M2M被更新时不更新主数据倒数据库
// Write 在 Statement 配置的条件下更新记录。
// 如需 classic 模式，先链式调 session.Classic()；
// 如需自定义 ctx，先链式调 session.WithContext(ctx)。
func (self *TSession) Write(data any) (effect int64, err error) {
	model := self.Statement.Model
	self.Op = OpWrite
	if _, err := model.BeforeSession(self); err != nil {
		return 0, err
	}
	defer func() {
		model.AfterSession(self)
		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	return self._write(data)
}

// TODO 根据条件删除
// delete records
func (self *TSession) Delete(ids ...any) (res_effect int64, err error) {
	model := self.Statement.Model
	self.Op = OpDelete
	if _, err := model.BeforeSession(self); err != nil {
		return 0, err
	}
	defer func() {
		model.AfterSession(self)

		self._resetStatement()
	}()

	if self.IsAutoClose {
		defer self.Close()
	}

	if self.IsDeprecated {
		return -1, ErrInvalidSession
	}

	// TODO 为什么用len
	if len(self.Statement.Model.String()) < 1 {
		return 0, ErrTableNotFound
	}

	// get id list — merge explicit args with any ids already set via Ids()
	// flattenIds 使 Create 返回的 []any 可直接回喂给 Delete()
	//
	// **调用方点没点名，必须在 flatten 之前记下来。** 点了名却一个都没剩（传进来的
	// 全是 nil/空），语义是"删这几条"而不是"删所有能看见的"——后者会清空整张表。
	namedIds := len(ids) > 0
	if namedIds {
		self.Statement.IdParam = append(self.Statement.IdParam, flattenIds(ids)...)
	}
	ids = self.Statement.IdParam

	// 空值 id 一律剔除。它们一行都删不掉，却足以把"点名删除"伪装成"按域删除"——
	// 下面那条 len(ids)==0 的回退会把当前域下的全部行选出来删掉。
	if len(ids) > 0 {
		kept := make([]any, 0, len(ids))
		for _, id := range ids {
			if id != nil && !utils.IsBlank(id) {
				kept = append(kept, id)
			}
		}
		if len(kept) != len(ids) {
			namedIds = true
			ids = kept
			self.Statement.IdParam = kept
		}
	}

	// Phase 2: safety guard — block no-condition deletes unless explicitly opted-in
	//
	// ⚠ hasCondition() **挡不住租户场景**：上层(vectors core/model 的 withSession)会在
	// BeforeSession 里往会话追加 tenant_id/company_id 记录规则，于是"有条件"恒成立，
	// 这道保险形同虚设。真机 2026-08-04：前端删一行发来的 id 是空的，这里就顺着下面
	// 那条"没有 id 就按域全选"把该租户可见的**每一行**都删了：
	//     SELECT id FROM system.pro_pricelist_item WHERE tenant_id=$1 AND (公司规则)
	//     DELETE FROM system.pro_pricelist_item WHERE id in ($1,$2)
	// 记录规则是**可见性**，不是调用方给的删除范围，不能当作"有意为之"的证据。
	if !self.allowUnsafe && !self.hasCondition() {
		return 0, errors.ErrUnsafe
	}

	// 点了名却全是空值:拒绝。绝不能退化成"按域删"。
	if namedIds && len(ids) == 0 {
		return 0, fmt.Errorf(
			"%w: delete on %s was called with ids that are all blank — refusing to fall back to deleting everything the current filter matches",
			errors.ErrUnsafe, self.Statement.Model.String())
	}

	if len(ids) == 0 {
		var err error
		ids, _, err = self._search("", nil)
		if err != nil {
			return 0, err
		}
	} else {
		// 点名删除同样要过条件：下面的 DELETE 只带 `WHERE id in (...)`，
		// 会话上累积的 tenant_id / 行级权限条件一条都不进 SQL。
		// 按域删走 _search 天然带条件，按 id 删必须在这里补。见 scopeIdsByDomain。
		scoped, err := self.scopeIdsByDomain(ids)
		if err != nil {
			return 0, err
		}
		ids = scoped
	}
	expectRowCount := int64(len(ids))

	if len(ids) == 0 {
		// Nothing to delete, prevent SQL syntax error on empty IN clause
		return 0, nil
	}

	// get the model id field name
	id_field := self.Statement.Model.IdField()
	quoter := self.orm.dialect.Quoter()
	// 模型名要在主删除**之前**取：_exec 里 defer 了 _resetStatement。
	model_name := self.Statement.Model.String()

	// ondelete 策略要在主删除**之前**跑：restrict 得来得及拦住这一次删除，cascade 得
	// 趁父行还在时按外键找到子行。失败一律返回错误（主记录还没删，回滚是干净的）——
	// 与下面 m2m 清理的"只告警"相反，那时记录已经删掉了。见 session_ondelete.go。
	if err := self.applyOnDelete(model_name, ids); err != nil {
		return 0, err
	}

	//#1 删除目标Model记录（表名按会话 schema 限定）
	sql := fmt.Sprintf(`DELETE FROM %s WHERE %s in (%s); `,
		quoter.QuoteTable(self.Schema, self.Statement.Model.Table()),
		quoter.QuoteIdentMust(id_field),
		idsToSqlHolder(ids...))
	res, err := self._exec(sql, ids...)
	if err != nil {
		return 0, err
	}

	cnt, err := res.RowsAffected()
	if err != nil {
		// autocommit 模式下 DELETE 已提交，RowsAffected 出错不应 Rollback（会造成"已回滚"的错觉）；
		// 仅在确处于事务时才回滚。
		if self.tx != nil {
			return 0, self.Rollback(err)
		}
		return 0, err
	}

	// 主记录已经删掉了，把它在各 m2m 关联表里的行一并清掉——两个方向都清。
	// 关联表没有外键约束(update_db_foreign_keys 是空实现)，不显式清就会永远留着：
	// "菜单还在、组没了"的孤儿行会让那个菜单对所有人永久隐身。
	// 放在行数校验之前：部分行本就不存在时，它们的关系行更是该清的孤儿。
	self.cleanupM2MRelations(model_name, ids)

	/* check the row count */
	if cnt != expectRowCount {
		log.Warnf("expect delete %d rows, but %d rows affected", expectRowCount, cnt)
		return expectRowCount, nil
	}
	/*
		table_name := self.Statement.Model.Table()
		//lCacher := self.orm.Cacher.RecCacher(self.Statement.Model.GetName()) // for del
		//if lCacher != nil {
		for _, id := range ids {
			//lCacher.Remove(id)
			self.orm.Cacher.RemoveById(table_name, id)
		}
		//}
		// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
		//lCacher = self.orm.Cacher.SqlCacher(self.Statement.Model.GetName()) // for del
		//lCacher.Clear()
		self.orm.Cacher.ClearByTable(self.Statement.Model.Table())
	*/
	return res.RowsAffected()
}

// _create 支持传入多个 src（变参/数组），一次插入多条记录。
// 优化点：整批共享的不变量（表名校验、主键字段解析）在循环外只处理一次，
// 循环内仅保留与每条记录数据相关的处理；其余逻辑与 _create 完全一致。
// 返回每条记录的 id（顺序与传入一致）；中途出错则返回已成功的 id 列表 + error。
func (self *TSession) _create(src ...any) ([]any, error) {
	// —— 整批不变量：循环外只处理一次 ——
	if len(self.Statement.Model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	// 不传 src 时回退为单条（Sets）创建
	if len(src) == 0 {
		src = []any{nil}
	}

	idField := self.Statement.Model.IdField()
	// 主键字段对象与 TIdField 断言仅解析一次；OnCreate 仍按记录调用
	var idCreator *TIdField
	if field := self.Statement.Model.GetFieldByName(idField); field != nil {
		idCreator, _ = field.(*TIdField)
	}

	ids := make([]any, 0, len(src))

	// 批量插入期间禁止底层 _exec/_query 复位 Statement。本循环逐行执行 INSERT，
	// 而 _exec/_query 各自 `defer self._resetStatement()`——第一行插完就把整个
	// Statement（Sets / Fields / NullableFields / OmitFields 这些「整批不变量」）
	// 清空，后续行读到的全是空。典型症状：vectors withSession 盖的 tenant_id/
	// create_id 只落在第一行，其余行 tenant_id=0，被租户过滤 `WHERE tenant_id=?`
	// 全部挡掉——按 id 读得到、按条件读不到（2026-07-24 真栈撞出，回归见
	// create_multirow_sets_test.go）。
	// 循环结束后由上层 Create 的 defer 统一复位一次；此处保存并恢复原值（而非
	// 硬编码 true），以尊重批量调用方可能已经关掉自动复位的意图。
	prevAutoReset := self.AutoResetStatement
	self.AutoResetStatement = false
	defer func() { self.AutoResetStatement = prevAutoReset }()

	// —— 每条记录处理 ——
	for _, one := range src {
		// If src is nil but Sets are present, use Sets as the data source.
		// If src is provided, _validateValues converts it; Sets are applied afterward.
		if one == nil && len(self.Statement.Sets) == 0 {
			return ids, fmt.Errorf("must submit the values for create")
		}
		srcWasSets := false
		if one == nil {
			one = self.Statement.Sets
			srcWasSets = true
		}

		/* 解析数据 */
		data, res_err := self._validateValues(one)
		if res_err != nil {
			return ids, res_err
		}

		/* 应用 Sets（覆盖已有值） */
		if !srcWasSets && len(self.Statement.Sets) > 0 {
			rec := data.Record()
			for k, v := range self.Statement.Sets {
				rec.SetByField(k, v)
			}
		}

		/* 拆分数据 */
		explicitKeys := explicitKeysOf(one)
		if !srcWasSets {
			explicitKeys = mergeExplicitKeys(explicitKeys, self.Statement.Sets)
		}
		newValues, refValues, newTodo, err := self._separateValues(data, self.Statement.Fields, self.Statement.NullableFields, true, nil, explicitKeys)
		if err != nil {
			return ids, err
		}

		if idCreator != nil {
			newValues[idField] = idCreator.OnCreate(&TFieldContext{
				Session: self,
				Model:   self.Statement.Model,
				Dataset: data,
				Field:   idCreator,
			})
		}

		// 根据字段计算数据值
		datas, multiSql, err := self._todoCompute(data, nil, newTodo)
		if err != nil {
			return ids, err
		}

		/* 创建关联数据 */
		var relModel IModel
		for tbl, rel_vals := range refValues {
			fieldName := self.Statement.Model.Obj().GetRelationByName(tbl)
			hasExplicitValue := false
			if v := newValues[fieldName]; v != nil && !utils.IsBlank(v) {
				hasExplicitValue = true
			}
			if v := datas[fieldName]; len(v) > 0 && !utils.IsBlank(v[0]) {
				hasExplicitValue = true
			}

			// 跳过条件：(a) 关系表没有任何待写字段；或 (b) 用户已经显式提供了关联外键值。
			// 任一成立都说明无需自动创建/更新关联记录。
			if len(rel_vals) == 0 || hasExplicitValue {
				continue
			}

			/* 使用原事物会话进行创建或者更新关联表记录 */
			relModel, err = self._getModel(tbl) // NOTE 这里沿用了self的Tx
			if err != nil {
				return ids, err
			}

			// 获取管理表UID
			record_id := rel_vals[relModel.IdField()]
			if record_id == nil || utils.IsBlank(record_id) {
				/* 复制 OnConflict */
				tx := relModel.Tx()
				if oc := self.Statement.OnConflict; oc != nil {
					tx.OnConflict(oc)
				}

				rids, err := tx.Create(rel_vals)
				if err != nil {
					return ids, err
				}
				record_id = rids[0]
			} else {
				relModel.Tx().Ids(record_id).Write(rel_vals)
			}

			newValues[self.Statement.Model.Obj().GetRelationByName(tbl)] = record_id
		}

		// 被设置默认值的字段赋值给Val
		self.Statement.Model.Obj().GetDefault().Range(func(key, value any) bool {
			k := key.(string)
			if newValues[k] == nil {
				newValues[k] = value //fmt. lFld._symbol_c
			}
			return true
		})

		// #验证数据类型
		//TODO 需要更准确安全
		self.Statement.Model.GetBase()._validate(newValues)

		var field IField
		var id any
		recIds := make([]any, 0, multiSql)
		for idx := 0; idx < multiSql; idx++ {
			fields := make([]string, 0)
			params := make([]any, 0)
			uniqueFields := make([]string, 0)

			// 字段,值
			for k, v := range newValues {
				if v == nil {
					continue
				}

				// 避免计算字段重复
				if _, has := datas[k]; has {
					continue
				}

				if field = self.Statement.Model.GetFieldByName(k); field != nil && field.IsUnique() && !field.IsPrimaryKey() {
					uniqueFields = append(uniqueFields, field.Name())
					if multiSql > 1 {
						return ids, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
					}
				}

				if k == idField {
					id = v
				}

				fields = append(fields, k)
				params = append(params, v)
			}

			for k, vs := range datas {
				if vs == nil {
					continue
				}

				if field = self.Statement.Model.GetFieldByName(k); field != nil && field.IsUnique() && !field.IsPrimaryKey() {
					uniqueFields = append(uniqueFields, field.Name())
					if multiSql > 1 {
						return ids, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
					}
				}

				fields = append(fields, k)
				if len(vs) > 1 {
					params = append(params, vs[idx])
				} else {
					params = append(params, vs[0])
				}
			}

			OnConflictValues := make([]any, 0)
			if self.Statement.OnConflict != nil {
				if self.Statement.OnConflict.UpdateAll {
					self.Statement.OnConflict.DoUpdates = make([]string, 0)
					for field_name, v := range newValues {
						/* id 字段不参与更新 */
						if field_name == self.Statement.Model.IdField() {
							continue
						}

						if utils.IndexOf(field_name, self.Statement.OnConflict.Fields...) == -1 {
							self.Statement.OnConflict.DoUpdates = append(self.Statement.OnConflict.DoUpdates, field_name)
							OnConflictValues = append(OnConflictValues, v)
						}
					}
				} else if len(self.Statement.OnConflict.DoUpdates) > 0 {
					for _, field_name := range self.Statement.OnConflict.DoUpdates {
						if v, ok := newValues[field_name]; ok {
							OnConflictValues = append(OnConflictValues, v)
						}
					}
				} else {
					self.Statement.OnConflict.DoNothing = true
				}
			}

			params = append(params, OnConflictValues...)
			// 冲突目标必须是**一个完整的唯一索引**。上面按字段标志收集出来的
			// uniqueFields 是扁平的（丢了索引归属），且来自 map 遍历（顺序随机），
			// 直接拿去用会生成 `ON CONFLICT ("其中随便一列")` —— 复合唯一索引下
			// 永远匹配不到任何约束，Postgres 报 42P10。
			uniqueFields = expandToUniqueIndex(self.Statement.Model.GetIndexes(), fields, uniqueFields)
			sqlExpr, isQuery := self.Statement.generate_insert(fields, uniqueFields)
			if isQuery {
				ds, err := self._query(sqlExpr, params...)
				if err != nil {
					return ids, err
				}

				id = ds.Record().GetByIndex(0)
			} else {
				var res sql.Result
				res, err = self.Exec(sqlExpr, params...)
				if err != nil {
					return ids, err
				}

				// 支持递增字段返回ID
				if len(self.Statement.Model.IdField()) > 0 {
					id, err = res.LastInsertId()
					if err != nil {
						return ids, err
					}
				}
			}

			recIds = append(recIds, id)
		}

		/*  根据 Ids 创建 M2M 关联记录 */
		if _, _, err = self._todoCompute(data, recIds, newTodo); err != nil {
			return ids, err
		}

		if id != nil {
			//更新缓存
			table_name := self.Statement.Model.Table()
			lRec := dataset.NewRecordSet(nil, newValues)
			self.orm.Cacher.PutById(table_name, utils.ToString(id), lRec) //for create

			// #由于表数据有所变动 所以清除所有有关于该表的SQL缓存结果
			self.orm.Cacher.ClearByTable(table_name) //for create
		}

		ids = append(ids, id)
	}

	return ids, nil
}

// Low-level implementation of write()
// TODO 只更新变更字段值
// #fix:由于更新可能只对少数字段更新,更新不适合使用缓存
func (self *TSession) _write(src any) (int64, error) {
	model := self.Statement.Model
	if len(model.String()) < 1 {
		return 0, ErrTableNotFound
	}

	// If src is nil but Sets are present, use Sets as the data source.
	if src == nil && len(self.Statement.Sets) == 0 {
		return 0, fmt.Errorf("must submit the values for update")
	}
	srcWasSets := false
	if src == nil {
		src = self.Statement.Sets
		srcWasSets = true
	}

	data, err := self._validateValues(src)
	if err != nil {
		return 0, err
	}

	/* 应用 Sets（覆盖已有值） */
	if !srcWasSets && len(self.Statement.Sets) > 0 {
		rec := data.Record()
		for k, v := range self.Statement.Sets {
			rec.SetByField(k, v)
		}
	}

	// #获取Ids
	var ids []any
	if len(self.Statement.IdParam) > 0 {
		ids = self.Statement.IdParam
	} else {
		idField := model.IdField()
		if id := data.Record().GetByField(idField); id != nil {
			//  必须不是 Set 语句值
			if _, has := self.Statement.Sets[idField]; !has {
				// flattenIds 处理 data 中 id 值本身为 []any 的情况（如直接回喂 Create 返回值）
				ids = flattenIds([]any{id})
			}
		}
	}
	includePkey := len(ids) > 0

	// Phase 2: safety guard — block no-condition writes unless explicitly opted-in.
	// Guard runs here (after ID extraction from data) so Write(map_with_id) is allowed.
	if !self.allowUnsafe && !includePkey && self.Statement.domain.Count() == 0 {
		return 0, errors.ErrUnsafe
	}

	// 组合查询语句
	var (
		from_clause, where_clause string
		where_clause_params       []any
	)

	if includePkey {
		from_clause = self.Statement.qualifiedTable(model.Table())

		// 点名 id 不等于绕过条件：下面拼的 SQL 只有 `WHERE id IN (...)`，
		// Where()/Domain() 累积的限制一条都不进去。多租户的 tenant_id 过滤与
		// 行级权限都是"往会话追加条件"实现的，不在这里收窄 ids 就等于按 id
		// 写可以改到本会话根本看不见的行。详见 scopeIdsByDomain。
		scoped, err := self.scopeIdsByDomain(ids)
		if err != nil {
			return 0, err
		}
		if len(scoped) == 0 {
			// 目标行全部在可见范围之外：什么也不改。
			return 0, nil
		}
		ids = scoped
		self.Statement.IdParam = scoped

	} else if self.Statement.domain.Count() > 0 {
		query, err := self.Statement.where_calc(self.Statement.domain, false, nil)
		if err != nil {
			return 0, err
		}

		// # determine the actual query to execute
		from_clause, where_clause, where_clause_params = query.getSql()
		// the PK condition status
		if where_clause == "" {
			return 0, fmt.Errorf("must have ids or qury clause")
		}

		// 条件更新是"先 SELECT 出 id，再逐条 UPDATE"两步走，两步之间存在窗口：
		// 别的事务可以在这中间把行改成不再满足条件的样子，UPDATE 照样落库。
		// 调用方加 .ForUpdate() 即在这里锁住选中的行，让整段变成真正的原子 CAS
		// （必须在事务里，否则 lockClause 直接报错）。
		lockClause, err := self.lockClause(false)
		if err != nil {
			return 0, err
		}

		sql := JoinClause(
			"SELECT",
			self.Statement.IdKey,
			"FROM",
			from_clause,
			"WHERE",
			where_clause,
			lockClause,
		)
		// 获得Id占位符索引
		ds, err := self._query(sql, where_clause_params...) // use internal _query to avoid premature AutoClose
		if err != nil {
			return 0, err
		}

		len := ds.Count()
		if len == 0 {
			return 0, fmt.Errorf("Not records found from database matching for writing update!")
		}

		ids = make([]any, len)
		ds.Range(func(pos int, record *dataset.TRecordSet) error {
			ids[pos] = record.GetByField(self.Statement.IdKey)
			return nil
		})
	} else {
		return 0, fmt.Errorf("At least have one of Where()|Domain()|Ids() condition to locate for writing update")
	}

	explicitKeys := explicitKeysOf(src)
	if !srcWasSets {
		explicitKeys = mergeExplicitKeys(explicitKeys, self.Statement.Sets)
	}
	newVals, refVals, newTodo, err := self._separateValues(data, self.Statement.Fields, self.Statement.NullableFields, false, ids, explicitKeys)
	if err != nil {
		return 0, err
	}

	// 根据字段计算数据值
	datas, multiSql, err := self._todoCompute(data, ids, newTodo)
	if err != nil {
		return 0, err
	}

	var field IField
	var effectedRows int64 = 0
	// newVals holds plain scalar fields; datas holds relational (m2o/o2m/m2m) fields
	// routed through _todoCompute (see _separateValues: field.IsRelated() always
	// diverts to upd_todo/newTodo, never touches newVals). Gating solely on
	// len(newVals) silently no-oped any Write() whose changed fields were ALL
	// relational: e.g. reassigning only location_dest_id or lot_id executed with
	// effect=0, no error, no SQL emitted. Both loops below already handle datas
	// independently, so widening the gate is sufficient.
	if len(newVals) > 0 || len(datas) > 0 {
		quoter := self.orm.dialect.Quoter().Quote
		for idx, id := range ids {
			//self.check_access_rule(cr, user, ids, 'write', context=context)

			params := make([]any, 0, len(newVals)+len(datas)+1)
			//set_clause := ""

			// TODO 验证数据类型
			//self._validate(lNewVals)

			// 拼SQL
			var sql strings.Builder
			sql.WriteString("UPDATE ")
			sql.WriteString(from_clause)
			sql.WriteString(" SET ")

			sqlLen := sql.Len()
			comma := false
			for k, v := range newVals {
				if comma {
					sql.WriteString(",")
				}

				// 避免计算字段重复
				if _, has := datas[k]; has {
					continue
				}

				// 更新里不予许多条唯一记录
				if field = self.Statement.Model.GetFieldByName(k); field != nil && field.IsUnique() && multiSql > 1 {
					return 0, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
				}

				sql.WriteString(quoter(k))
				sql.WriteString("=?")
				params = append(params, v)

				comma = true
			}

			// NOTE: previously reset via `comma = len(datas) != 0`, which ignored
			// whether the newVals loop above had already written a clause — when
			// newVals was empty (all-relational Write(), see gate fix above) this
			// unconditionally forced a leading comma before the first datas key,
			// producing invalid SQL like "SET ,\"col\"=?". comma must simply carry
			// over from the newVals loop so the first clause overall never gets one.
			for k, vs := range datas {
				if comma {
					sql.WriteString(",")
				}

				if field = self.Statement.Model.GetFieldByName(k); field != nil && field.IsUnique() && multiSql > 1 {
					return 0, fmt.Errorf("Create record over than exspect %d rows!", multiSql)
				}

				sql.WriteString(quoter(k))
				sql.WriteString("=?")
				if len(vs) > 1 {
					params = append(params, vs[idx])
				} else {
					params = append(params, vs[0])
				}

				comma = true
			}

			if sql.Len() == sqlLen {
				return 0, fmt.Errorf("must have values")
			}

			sql.WriteString(" WHERE ")
			if multiSql > 1 {
				sql.WriteString(fmt.Sprintf(`%s = ?`, quoter(self.Statement.IdKey)))
				params = append(params, id) // add in ids data

			} else {
				sql.WriteString(fmt.Sprintf(`%s IN (%s)`,
					quoter(self.Statement.IdKey),
					strings.Repeat("?,", len(ids)-1)+"?"),
				)
				params = append(params, ids...) // add in ids data
			}

			res, err := self._exec(sql.String(), params...)
			if err != nil {
				return 0, err
			}

			res_effect, err := res.RowsAffected()
			if err != nil {
				return 0, err
			}

			/*table_name := self.Statement.Model.GetName()
			//lCacher := self.orm.Cacher.RecCacher(self.Statement.Model.GetName()) // for write
			//if lCacher != nil {
			for _, id := range ids {
				if id != "" {
					//更新缓存
					//lKey := self.generate_caches_key(self.Statement.Model.GetName(), id)
					lRec := NewRecordSet(nil, lNewVals)
					self.orm.Cacher.PutById(table_name, id, lRec)
				}
			}*/
			//}
			/* 统计 */
			effectedRows += res_effect

			// 退出多SQL更新
			if multiSql == 1 {
				break
			}
		}
	}

	// 更新关联表
	var refIds []any
	var refModel IModel
	var ds *TDataset
	var in_vals, fieldName, sql string
	for tbl, ref_vals := range refVals {
		if len(ref_vals) == 0 {
			continue
		}

		fieldName = model.Obj().GetRelationByName(tbl)

		quoter := self.orm.dialect.Quoter()
		// add in ids data
		in_vals = strings.Repeat("?,", len(ids)-1) + "?"
		sql = fmt.Sprintf("SELECT distinct %s FROM %s WHERE %s IN(%s)",
			quoter.Quote(fieldName), quoter.QuoteTable(self.Schema, model.Table()), quoter.Quote(self.Statement.IdKey), in_vals)
		ds, err = self._query(sql, ids...)
		if err != nil {
			return 0, err
		}

		if ds.Count() != 0 {
			refIds = make([]any, ds.Count())
			ds.Range(func(pos int, record *dataset.TRecordSet) error {
				// 用 Range 回调的 record 取每行的值；旧实现误用 ds.Record()（始终是游标位置 0），
				// 导致多记录时所有 refIds 都取到第一行，写错关联记录。
				refIds[pos] = record.GetByField(fieldName)
				return nil
			})

			//# 重新写入关联数据
			refModel, err = self._getModel(tbl) // NOTE 这里沿用了self的Tx
			if err != nil {
				return 0, err
			}
			refModel.Tx().Ids(refIds...).Write(ref_vals) //TODO 检查是否真确使用
		}
	}

	return effectedRows, nil
}

func (self *TSession) _read() (*dataset.TDataSet, error) {
	model := self.Statement.Model

	if len(model.String()) < 1 {
		return nil, ErrTableNotFound
	}

	// TODO: check access rights 检查权限
	//	self.check_access_rights("read")
	//	fields = self._check_field_access_rights("read", fields, nil)

	//# split fields into stored and computed fields
	storeFields := make([]string, 0, 16) // 可存于数据的字段
	relateFields := make([]string, 0, 8)
	computedFields := make([]string, 0, 8) // 数据库没有的字段
	postReadFields := make([]string, 0, 2) // 既要读库、读完还要再加工的字段(properties)
	hasScalarCompute := false              // 存在「非存储标量计算字段」(走 getter，不读 DB)

	// 字段分类。指定 Select 与「Select * From」两条路径只差「字段从哪来」，
	// 归类规则必须完全一致——此前是两份逐字复制的分支，改一处漏一处就会让
	// 显式指定字段和读全表得到不同的字段集。
	classify := func(field IField) {
		name := field.Name()
		// 排除被 Omit 标记的字段
		if self.Statement.IsOmit(name) {
			return
		}

		switch {
		case field.IsRelated():
			computedFields = append(computedFields, name)
			relateFields = append(relateFields, name)
		case !field.Store() && field.HasGetter():
			// 非存储标量计算字段(如 display_name):走 getter 计算，不读 DB
			computedFields = append(computedFields, name)
			hasScalarCompute = true
		case isPostReadField(field):
			// 两头都占的字段(properties):列要真读，读完还要跟别处的数据合并。
			// 只进 storeFields 会让前端拿到库里那个瘦字典，只进 computedFields
			// 则整列压根不查。
			storeFields = append(storeFields, name)
			postReadFields = append(postReadFields, name)
		default: //本Model存于数据库的字段
			storeFields = append(storeFields, name)
		}
	}

	if len(self.Statement.Fields) > 0 {
		for _, name := range self.Statement.Fields {
			// Omit 的字段这里不早退：交给 classify 统一判断，规则只有一处。
			field := model.Obj().GetFieldByName(name)
			if field == nil {
				log.Warnf(`%s.read() with unknown field '%s'`, model.String(), name)
				continue
			}
			classify(field)
		}
	} else {
		for _, field := range model.GetFields() {
			classify(field)
		}
	}

	// 获取数据库数据
	//# fetch stored fields from the database to the cache
	dataset, _, err := self._readFromDatabase(storeFields, relateFields)
	if err != nil {
		return nil, err
	}

	// TODO 优化循环代码
	// 处理经典字段数据
	// postReadFields 不在这个门槛的可选项里而是**并列的触发条件**：properties 的合并
	// 与 Classic/NameGet 无关，普通一次 read 也必须做，否则前端拿到的是无从渲染的瘦字典。
	if (self.UseNameGet || self.IsClassic || len(self.subReads) > 0 || hasScalarCompute || len(postReadFields) > 0) && dataset.Count() > 0 {
		// 处理那些数据库不存在的字段：company_ids...
		//# retrieve results from records; this takes values from the cache and
		// # computes remaining fields
		nameFields := make([]IField, 0)
		/*
			for _, name := range storeFields {
				fld := self.Statement.Model.Obj().GetFieldByName(name)
				if fld != nil {
					nameFields = append(nameFields, fld)
				}
			}
		*/
		for _, name := range computedFields {
			fld := model.Obj().GetFieldByName(name)
			if fld != nil {
				nameFields = append(nameFields, fld)
			}
		}
		for _, name := range postReadFields {
			fld := model.Obj().GetFieldByName(name)
			if fld != nil {
				nameFields = append(nameFields, fld)
			}
		}

		//FIXME　执行太多SQL
		for _, field := range nameFields {
			sub, hasSub := self.subReads[field.Name()]
			// 纯嵌套规格模式(非全局 Classic/NameGet)下只内嵌带子规格的关系字段，
			// 避免对未声明的 o2m/m2m 计算字段做多余查询；非存储标量计算字段
			// (如 display_name)是纯内存计算，不在此跳过。
			if !self.IsClassic && !self.UseNameGet && !hasSub && field.IsRelated() {
				continue
			}

			ctx := &TFieldContext{
				Session: self,
				Model:   model,
				Field:   field,
				//Id:      rec_id,
				//Value:   val,
				Dataset:    dataset,
				UseNameGet: self.UseNameGet,
				// 下钻只有一层，靠的是 ManyToOne/OneToMany 的子读取**不**把 Classic
				// 传给子会话（model_request.go 的 `sub.Ids(ids...).Read()`）：子会话不满足
				// 本段的派发条件，comodel 自己的关系字段就不再展开。
				// 别顺手给那行补 `.Classic()`——模型间的关系环(A.m2o→B、B.m2o→A)会让
				// 它无限递归爆栈。回归用例 classic_read_cycle_test.go。
				ClassicRead: self.IsClassic,
			}
			if hasSub {
				ctx.Fields = sub.Fields
				ctx.SubFields = sub.SubFields
				if s, ok := sub.Domain.(string); ok {
					ctx.Domain = s
				}
				// 带子规格即按经典内嵌:返回子记录(map)而非仅 [id,name]/ids，
				// 列范围由 ctx.Fields 限定；递归深度由 ctx.SubFields 决定(有限)。
				ctx.ClassicRead = true
			}

			if err := field.OnRead(ctx); err != nil {
				log.Errf("%s@%s.OnRead:%s", field.ModelName(), field.Name(), err.Error())
			}
		}
	}

	dataset.First()
	dataset.Classic(self.IsClassic)
	return dataset, nil
}

/*
   """ Read the given fields of the records in ``self`` from the database,
       and store them in cache. Access errors are also stored in cache.

       :param field_names: list of column names of model ``self``; all those
           fields are guaranteed to be read
       :param inherited_field_names: list of column names from parent
           models; some of those fields may not be read
   """
*/
// 从数据库读取记录并保存到缓存中
// :param field_names: Model的所有字段
// :param inherited_field_names:关联父表的所有字段
func (self *TSession) _readFromDatabase(storeFields, relateFields []string) (res_ds *dataset.TDataSet, res_sql string, err error) {
	var (
		query *TQuery
		select_clause, from_clause, where_clause,
		order_clause, limit_clause, offset_clause, groupby_clause string
		where_clause_params []any
	)
	// 生成查询条件：点名的 id 与既有条件**AND 叠加**，不是取代。
	//
	// 这里原本先 domain.Clear() 再放 id 条件（注释写着"当指定了主键其他查询条件将失效"）。
	// 那等于：只要调用方给了 id，会话上累积的一切限制统统作废——多租户的
	// `tenant_id = ?`、公司可见性、行级权限的规则条件，全部不进 SQL。
	// 后果是**按 id 就能读到任何一行**：读列表被正确过滤，按 id 精确读却全都读得到。
	// 真栈 2026-08-08：商家 A 按 Domain 读只看见自己的 stock.quant（正确），
	// 同一会话按 Ids 读商家 B 的行照样返回（越权）。写/删侧的同类问题见 scopeIdsByDomain。
	//
	// IN 本身就是 AND 追加（domain.TDomainNode.IN → OP(AND_OPERATOR, ...)），
	// 所以去掉 Clear 即可，语义变成"这些 id 里我看得见的那些"。
	if len(self.Statement.IdParam) != 0 {
		self.Statement.domain.IN(self.Statement.Model.IdField(), self.Statement.IdParam...)
	}

	query, err = self.Statement.where_calc(self.Statement.domain, false, nil)
	if err != nil {
		return nil, "", err
	}

	/* Join fields and function clause */
	qual_names := make([]string, 0)
	//if self.IsClassic {
	//对可迭代函数'iterable'中的每一个元素应用‘function’方法，将结果作为list返回
	//# determine the fields that are stored as columns in tables;
	fields := make([]IField, 0)
	fields_pre := make([]IField, 0)
	for _, name := range storeFields {
		if f := self.Statement.Model.Obj().GetFieldByName(name); f != nil {
			fields = append(fields, f)
		}
	}

	for _, name := range relateFields {
		if f := self.Statement.Model.Obj().GetFieldByName(name); f != nil {
			fields = append(fields, f)
		}
	}

	//	当字段为field.base_field.column.translate可调用即是translate为回调函数而非Bool值时不加入Join
	hasInherited := false
	for _, fld := range fields {
		//if fld.IsClassicRead() && !(fld.IsRelatedField() && false) { //用false代替callable(field.base_field.column.translate)
		if fld.Store() && fld.SQLType().Name != "" { //用false代替callable(field.base_field.column.translate) — IsRelated check pending
			fields_pre = append(fields_pre, fld)
		} else if fld.IsInherited() && fld.SQLType().Name != "" && !fld.HasGetter() &&
			fld.TypeName() != TYPE_O2M && fld.TypeName() != TYPE_M2M {
			// _inherits 委托字段：本表无列(store=false)，靠 qualify→inherits_join_calc
			// JOIN 父表(o2o FK)取值，故仍须进入 SELECT 并带模型限定。
			// 仅纳入父表上有真实列者：标量字段与 m2o(FK 列)。
			// o2m/m2m 是虚拟关系字段、永远没有物理列(注意它们仍可能被赋了 SQLType，
			// 故必须按 TypeName 排除，不能只看 SQLType)；getter 函数字段同样无列。
			// 这些由各自的 OnRead 单独取值，不进 JOIN-SELECT。
			fields_pre = append(fields_pre, fld)
			hasInherited = true
		}
	}

	// 多表(域条件引入 JOIN)或存在继承字段时，所有字段都带表限定，
	// 以触发 qualify→inherits_join_calc 为继承字段补上父表 JOIN。
	if len(query.tables) > 1 || hasInherited {
		for _, f := range fields_pre {
			qual_names = append(qual_names, query.qualify(f, self.Statement.Model))
		}
	} else {
		for _, f := range fields_pre {
			qual_names = append(qual_names, query.qualify(f, nil))
		}
	}

	//} else {
	//	qual_names = self.Statement.generate_fields()
	//}
	select_clause = strings.Join(append(qual_names, self.Statement.FuncsClause...), ",")

	// # determine the actual query to execute
	from_clause, where_clause, where_clause_params = query.getSql()

	// Phase 2: soft-delete auto-filter
	where_clause = andClause(where_clause, self.softDeleteClause())

	if where_clause != "" {
		where_clause = "WHERE " + where_clause
	}

	// orderby clause
	order_clause = self.Statement.generate_order_by(query, nil) // TODO 未完成

	// GroupBy clause — 每个字段必须命中模型字段并经标识符校验/引用，防止注入
	if len(self.Statement.GroupByClause) > 0 {
		quoter := self.orm.dialect.Quoter()
		groupCols := make([]string, 0, len(self.Statement.GroupByClause))
		for _, name := range self.Statement.GroupByClause {
			if field := self.Statement.Model.GetFieldByName(name); field == nil {
				log.Warnf("GroupBy field %s not found on model %s, ignored", name, self.Statement.Model.String())
				continue
			}
			q, err := quoter.QuoteIdent(name)
			if err != nil {
				log.Warnf("GroupBy field %s is not a valid identifier, ignored: %v", name, err)
				continue
			}
			groupCols = append(groupCols, q)
		}
		if len(groupCols) > 0 {
			groupby_clause = "GROUP BY " + strings.Join(groupCols, ",")
		}
	}

	// limit clause
	limit := self.Statement.LimitClause
	// implicitLimit 记住"这个上限不是调用方要的，是我们替他加的"。
	// 只有隐式上限截断才算数据丢失——调用方自己写 Limit(20) 拿到 20 条是他要的结果。
	implicitLimit := false
	if limit != -1 {
		if limit == 0 {
			limit = DefaultLimit
			implicitLimit = true
		}
		limit_clause = "LIMIT " + utils.ToString(limit)
	}

	// offset clause
	if self.Statement.OffsetClause > 0 {
		offset_clause = "OFFSET " + utils.ToString(self.Statement.OffsetClause)
	}

	// 行锁子句（FOR UPDATE / FOR SHARE ...），语法上必须排在 LIMIT/OFFSET 之后。
	lock_clause, err := self.lockClause(groupby_clause != "")
	if err != nil {
		return nil, "", err
	}

	// 子句顺序即 SQL 语法顺序：GROUP BY 必须在 ORDER BY **之前**。此前两者写反，
	// 只要同时出现（模型有默认 _order + 调用方 .GroupBy(...)）就是语法错误的 SQL，
	// 整条查询直接报错——GroupBy 因此从来只在无排序时能用。
	res_sql = JoinClause(
		"SELECT",
		select_clause,
		"FROM",
		from_clause,
		where_clause,
		groupby_clause,
		order_clause,
		limit_clause,
		offset_clause,
		lock_clause,
	)

	// 加锁读一律绕开 SQL 结果缓存：命中缓存就等于这条 SELECT 根本没发给数据库，
	// 锁自然也没加上——那就又回到了"以为锁住了其实没有"。同理不回填缓存：
	// 加锁读之后紧接着就是修改，缓存这一份马上会过期。
	locking := self.Statement.Lock.IsLocking()

	if !locking {
		// 从缓存里获得数据
		res_ds = self.orm.Cacher.GetBySql(self.Statement.Model.Table(), res_sql, where_clause_params)
		if res_ds != nil {
			res_ds.First()
			return res_ds, res_sql, nil
		}
	}

	// 获得Id占位符索引
	res_ds, err = self.Query(res_sql, where_clause_params...) //cr.execute(res_sql, params)
	if err != nil {
		return nil, "", err
	}

	self.warnIfTruncated(res_ds, limit, implicitLimit)

	if !locking {
		//# 添加进入缓存
		self.orm.Cacher.PutBySql(self.Statement.Model.Table(), res_sql, where_clause_params, res_ds)
	}

	//# 必须是合法位置上
	res_ds.First()
	return res_ds, res_sql, nil
}

// TODO
// _validateValues converts any supported value into a *dataset.TDataSet.
// It does NOT apply Statement.Sets — callers handle Sets inline after this call.
// Supported inputs: *dataset.TDataSet (returned as-is), map[string]any,
// map[string]string, or a struct pointer/value.
// explicitKeysOf 取出调用方**逐个写下**的键；非 map 源返回 nil。
//
// map 源(Write(map[string]any{...})/Sets)只含调用方写下的键——键在即"碰过",
// 这时零值 false/0/"" 是**合法值**不是"没提供"，nil 则是"清空"。
// struct 源不行:StructToMap 无条件导出结构体上每个模型字段,未赋值的字段也在
// 里面且是零值,与"显式设成零值"无法区分——那里必须保持旧语义(零值=没提供,
// 该填默认值就填、该跳过就跳过),否则建记录时字段默认值会被结构体零值顶掉。
// struct 源要精确表达"就写这几列"，用 Select()（见 writeScopeSet）。
//
// ★ 为什么返回键集合而不是一个 bool：键在不在**不能**回头问 dataset。
//
//	dataset.AppendRecord 会把"所有值都是 nil"的记录整条丢掉(isBlankRec)，
//	于是 Write(map{"expire": nil}) 这种单键清空请求进到 _separateValues 时
//	数据集里空空如也，无从分辨"没给这一列"和"给了 nil"——影响行数恒 0、
//	不报错，正是"清空一个日期字段做不到"的真正来源。键集合在入口就截下来，
//	不经过那个容器。
func explicitKeysOf(src any) map[string]bool {
	var keys map[string]bool
	switch v := src.(type) {
	case map[string]any:
		keys = make(map[string]bool, len(v))
		for k := range v {
			keys[k] = true
		}
	case map[string]string:
		keys = make(map[string]bool, len(v))
		for k := range v {
			keys[k] = true
		}
	}
	return keys
}

// mergeExplicitKeys 把 Sets 的键并进显式键集合——Sets 的值同样是调用方明写的。
func mergeExplicitKeys(keys map[string]bool, sets map[string]any) map[string]bool {
	if len(sets) == 0 {
		return keys
	}
	if keys == nil {
		return keys // struct/dataset 源：Sets 不改变它的整体语义
	}
	for k := range sets {
		keys[k] = true
	}
	return keys
}

func (self *TSession) _validateValues(values any) (*dataset.TDataSet, error) {
	// Session-specific concern: auto-detect Model from struct type name
	// when no model is set yet on the Statement. Must happen BEFORE
	// NormalizeValues so the struct path has a non-nil model.
	if values != nil && self.Statement.Model == nil {
		rv := reflect.ValueOf(values)
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if rv.Kind() == reflect.Struct {
			if name := fmtModelName(utils.Obj2Name(values)); name != "" {
				self.Model(name)
			}
		}
	}
	return NormalizeValues(values, self.Statement.Model)
}

func (self *TSession) _todoCompute(data *dataset.TDataSet, ids []any, newTodo []IField) (map[string][]any, int, error) {
	// 根据字段计算数据值
	var multiSql = 1
	var name string
	var value any
	var ctx *TFieldContext
	datas := make(map[string][]any)
	for _, field := range newTodo {
		name = field.Name()
		value = data.Record().GetByField(name)
		ctx = &TFieldContext{
			Session: self,
			Model:   self.Statement.Model,
			Dataset: data,
			Field:   field,
			Value:   value,
			Ids:     ids,
		}

		if utils.IsBlank(value) {
			if defaultFunc := field.DefaultFunc(); defaultFunc != nil {
				//if utils.IsBlank(value) && !field.IsDefaultEmpty() {
				if err := defaultFunc(ctx); err != nil {
					return nil, multiSql, err
				}

				datas[name] = []any{ctx.values}
				continue
			}
		}

		if field.Store() {
			switch field.TypeName() {
			case TYPE_M2O:
				// 必须无条件走 OnWrite(TMany2OneField.OnWrite 已处理 string/tuple/裸id
				// 等全部形态):此前只在 value 是纯字符串(输入名称查找/新建)时才调用
				// OnWrite,其余形态(如前端 Many2OneField 提交的 [id,name] 经典元组)会
				// 落到下面 default 分支,在没有自定义 getter/setter 时直接把整个元组扔进
				// field.onConvertToWrite→value2SqlTypeValue,BigInt 列对 []any 调用
				// utils.ToInt64 解析失败退回 0,写成 0 后配合 id-as-string 的"0→''"
				// 兜底格式化,读出来就是空——表现为"更新提交成功但读不到值"。
				if err := field.OnWrite(ctx); err != nil {
					return nil, multiSql, err
				}
				datas[name] = []any{ctx.values}
			default:
				if field.HasGetter() || field.HasSetter() {
					//if err := field.ComputeFunc(ctx); err != nil {
					//	return nil, multiSql, err
					//}
					if err := field.OnWrite(ctx); err != nil {
						return nil, multiSql, err
					}

					switch v := ctx.values.(type) {
					case map[string]any:
						datas[name] = []any{v}
					case []any:
						count := len(v)
						if count > 1 {
							//if count == len(ids) {
							datas[name] = v // 记录计算数据值
							multiSql = count
						} else if count == 1 {
							datas[name] = v
						} else {
							return nil, multiSql, fmt.Errorf("the %s ComputeFunc return values is not matching records count!", field.Name())
						}
					default:
						//if len(ids) == 1 {
						datas[name] = []any{ctx.values}
						//}
						//return nil, multiSql, fmt.Errorf("the %s ComputeFunc return values is not matching records count!", field.Name())
					}

				} else {
					datas[name] = []any{field.onConvertToWrite(self, value)} // field.SymbolFunc()(utils.Itf2Str(val))
				}
			}
		} else {
			/* for M2M 字段*/
			/* ids 可能由于 onConflict 导致无法获取到值，此时需要重新获取 */
			if ids != nil {
				if err := field.OnWrite(ctx); err != nil {
					return nil, multiSql, err
				}
			}
		}
	}

	return datas, multiSql, nil
}

// TODO FN
// 分配值并补全ID,Update,Create字段值
// separate data for difference type of update
// , includeVersion bool, includeUpdated bool, includeNil bool,
//
//	includeAutoIncr bool, allUseBool bool, useAllCols bool,
//	mustColumnMap map[string]bool, nullableMap map[string]bool,
//	columnMap map[string]bool, update, unscoped bool
//
// needID is the values inclduing key
func (self *TSession) _separateValues(data *dataset.TDataSet, mustFields []string, nullableFields map[string]bool, includeNil bool, ids []any, explicitKeys map[string]bool) (map[string]any, map[string]map[string]any, []IField, error) {
	/* 用于更新本Model的实际数据 */
	new_vals := make(map[string]any)
	rel_vals := make(map[string]map[string]any)
	ext_todo := make([]IField, 0) // 最后处理的字段 Created Updated
	upd_todo := make([]IField, 0) // function 字段组 采用其他存储方式

	/* 初始化保存关联表用于更新创建关联表数据 */
	record := data.Record()

	// 未知键必须在遍历字段**之前**报出来：下面这个循环是按模型字段走的，
	// 输入里没被任何字段认领的键根本不会被访问到——那正是它此前静默消失的原因。
	if err := self.checkUnknownFields(explicitKeys); err != nil {
		return nil, nil, nil, err
	}

	// 写入范围：Select()/Fields() 点名过就只写那几列，且它们按"明确给了值"处理。
	// 只在**更新**时生效——新建的语义本来就是"给什么写什么 + 默认值补齐"，
	// 点名反而会把该补的默认值挡掉，建出一堆半残的行。见 writeScopeSet。
	var writeScope map[string]bool
	if len(ids) != 0 {
		writeScope = self.writeScopeSet()
	}

	self.Statement.Model.Obj().GetRelations().Range(func(key, value any) bool {
		tbl := utils.ToString(key)
		field_name := utils.ToString(value)
		rel_vals[tbl] = make(map[string]any) //NOTE 新建空Map以防Nil导致内存出错

		/* 添加非空值到关系表数据集里*/
		if val := record.GetByField(field_name); !utils.IsBlank(val) {
			//if val, has := data[field_name]; has && utils.IsBlank(val) {
			//if val, has := vals[self.Statement.Model.Obj().GetRelationByName(tbl)]; has && val != nil {
			rel_id := val                                          //新建新的并存入已经知道的ID
			rel_vals[tbl][self.Statement.Model.IdField()] = rel_id //utils.Itf2Str(vals[self.model._relations[tbl]])
		}
		return true
	})

	// 格式化IdField数据生成唯一ID
	idKeyName := self.Statement.IdKey

	/* 处理常规字段 */
	var errs []string
	var name string
	var field IField
	var fieldValue any
	var isBlank, setted bool
	// present：键**在不在**输入里。setted 判的是"值不是 nil"，两者此前被压成一个，
	// 于是「没给这一列」与「给了 nil」不可区分——后者因此永远写不进 NULL。
	// explicit：调用方是否明确表达了这一列的值（map 里写了键，或 Select() 点了名）。
	var present, explicit, explicitNil bool
	isIncludedIds := len(ids) != 0
	for _, field = range self.Statement.Model.GetFields() {
		// ignore AutoIncrement field
		if field == nil || field.IsAutoIncrement() {
			// do no use any AutoIncrement field's value
			continue
		}

		if !field.IsInherited() {
			if field.Base().isCreatedAt && isIncludedIds {
				// 包含主键的数据,说明已经是被创建过了,则不补全该字段
				continue
			}

			if field.Base().isCreatedAt || field.Base().isUpdatedAt {
				ext_todo = append(ext_todo, field)
				continue
			}
		}

		name = field.Name()
		if name == idKeyName {
			continue
		}

		// 排除被 Omit 标记的字段，使其不参与写入
		if self.Statement.IsOmit(name) {
			continue
		}

		// Select()/Fields() 点名之后，范围外的列一概不碰。
		if writeScope != nil && !writeScope[name] {
			continue
		}

		fieldValue = record.GetByField(name)
		// present 来自入口截下的键集合，不问 dataset——见 explicitKeysOf 的说明。
		present = explicitKeys[name]
		setted = fieldValue != nil
		isBlank = !setted || utils.IsBlank(fieldValue)

		// 调用方明确表达了这一列：map 里写了这个键，或 Select() 点了名。
		explicit = present || (writeScope != nil && writeScope[name])
		// 显式的"清空"。只在更新时成立——新建时"没值"等同于 NULL，该走默认值。
		explicitNil = isIncludedIds && explicit && isNullishWrite(field, fieldValue)

		// int64 有时候传进来的数字是string类型 需要转换成数字类型
		if field.SQLType().IsNumeric() {
			if v, ok := fieldValue.(string); ok {
				if vv, err := utils.IsNumeric(v); err == nil {
					fieldValue = vv
				} else {
					// 如果解析成数字成功则判定为数字成功 M2O 值可能是id或者Name值
					fieldValue = field.onConvertToWrite(self, fieldValue)
				}
				record.SetByField(name, fieldValue)
			}
		}

		/* #相同名称的字段分配给对应表 */
		if comm_models := self.Statement.Model.Obj().GetCommonFieldByName(name); setted && comm_models != nil { // 获得拥有该字段的所有表
			// 为各表预存值
			/*
				modelName := self.Statement.Model.String()
				fieldValue = field.onConvertToWrite(self, fieldValue) // 为当前表添加共同字段值
				for tbl := range comm_models {
					if tbl == modelName {
						new_vals[name] = fieldValue
						data.Record().SetByField(name, fieldValue)
					} else {
						rel_vals[tbl][name] = fieldValue
					}
				}*/
			modelName := self.Statement.Model.String()
			for tbl := range comm_models {
				if tbl != modelName {
					rel_vals[tbl][name] = fieldValue
				}
			}
		}

		// 关系字段不自动转换类型！将由字段独自处理
		if field.IsRelated() {
			if setted {
				upd_todo = append(upd_todo, field)
			} else if isIncludedIds && field.Store() && (explicitNil || (nullableFields != nil && nullableFields[name])) {
				// 显式写了 nil（explicitNil），或事先 Nullable() 声明过的关系字段，
				// 允许写 NULL。前者是后加的：map 里明写 {"partner_id": nil} 的意思
				// 只可能是"解绑"，再要求调用方多写一次 Nullable() 纯属仪式，而且
				// 这个 API 基本没人知道（vectors 全仓无一处使用）。
				//
				// 这里此前无条件 continue,于是**没有任何办法把一个 m2o 外键清空**:
				// 值传 nil 时 setted 为 false(判定依据就是值非 nil),这一支直接跳过,
				// 下面那段处理显式空值的 nullableFields 分支永远够不着——写请求发出去、
				// 不报错、库里纹丝不动。one2many 的解绑(命令 3/5/6)正是靠摘掉子行的
				// 反向外键实现的,没有这一条它就是个静默无操作。
				//
				// 两道闸保证不影响既有行为:必须调用方显式 Nullable(该字段),且字段
				// 得有真实的列(o2m/m2m 是虚拟关系字段,置空无从谈起)。
				new_vals[name] = nil
			}

			continue
		}

		// 字段有值处理函数无论如何都要调用
		//
		// properties 一对字段也走这里：值要按容器上的定义裁剪、定义改动要写回容器，
		// 两件事都需要整条记录的上下文（容器外键的值、记录 id），而 onConvertToWrite
		// 只拿得到孤零零一个值、且签名里没有 error 位（校验失败只能吞掉）。
		// 借用同一段的时机语义也正好对：新建时总跑（补默认值），更新时仅当显式给了值。
		if field.HasSetter() || isPropertiesWriteField(field) {
			// 创建时（无 ids）总是运行 Setter（无值时补算、有值时转换）；
			// 更新时（有 ids）仅当显式提供了值时才运行。仅 (update && 无值) 不运行。
			if !isIncludedIds || setted {
				ctx := &TFieldContext{
					Session: self,
					Model:   self.Statement.Model,
					Dataset: data,
					Field:   field,
					Value:   fieldValue,
					Ids:     ids,
				}
				if err := field.OnWrite(ctx); err != nil {
					return nil, nil, nil, err
				}

				if ctx.values != nil {
					fieldValue = ctx.values
					isBlank = false
				} else if isPropertiesWriteField(field) {
					// properties 的"算出来是空"是**有意义的空**：用户把规格值全清了，
					// 列该落 NULL。不接管的话 fieldValue 还是前端发来的那份**完整
					// 定义列表**，会被原样写进明细列——每条产品各存一份定义，正是
					// 这套设计要避免的事，而且下次读出来还能正常显示，不易察觉。
					fieldValue = nil
					isBlank = true
				}
			}
		}

		/* 过滤可以为空的字段空字段 */
		if isBlank && !isIncludedIds {
			/* 填补默认值——仅当调用方**根本没提供**该字段时(setted==false)。
			   isBlank 只看值是不是该类型的零值(IsBlank(false)/IsBlank(0)/IsBlank("")
			   全是 true),区分不了"没传"和"显式传了零值";而 setted 来自
			   record.GetByField(name)!=nil,只有键真的在数据集里才为真,是精确的
			   "调用方碰过这个字段"信号。
			   不加这个门槛,显式传入的零值会被字段默认值顶掉——实例:XML 导入
			   <template active="False"> 生成 active=false,却被 active 的默认值
			   true 覆盖,库里全是 active=true(12 个 footer 模板变体因此同时生效)。
			   显式零值走到下面 includeNil 分支照常落库。 */
			// 判据用 setted 而非 present：新建时显式写 nil 仍应由默认值来填
			// （"没值"与 NULL 在新建语义上是一回事），只有显式的**零值**才越过默认值。
			// Select() 点名的字段一律不填默认——点名即"就写我给的这个值"。
			if !(present && setted) && !(writeScope != nil && writeScope[name]) && !field.IsDefaultEmpty() {
				if field.DefaultFunc() != nil {
					ctx := &TFieldContext{
						Session: self,
						Model:   self.Statement.Model,
						Dataset: data,
						Field:   field,
						Value:   fieldValue,
						Ids:     ids,
					}
					if err := field.DefaultFunc()(ctx); err != nil {
						return nil, nil, nil, err
					}

					if ctx.values != nil {
						fieldValue = ctx.values
					}
					// isBlank is unconditionally reset to false after this if/else chain
				} else if fieldValue = field.Default(); fieldValue != nil {
					/* 关系字段不自动转换类型！将由字段独自处理 */
					fieldValue = value2FieldTypeValue(field, fieldValue)
				} else {
					/* 计算默认值 */
					// For inherited fields with setter/getter, default values must still land in rel_vals.
					// Let the inherited-field handler below run (it can invoke OnWrite/DefaultFunc).
					if !field.IsInherited() {
						upd_todo = append(upd_todo, field)
						continue
					}
				}
				isBlank = false
			}

			/* 再次确认空值 */
			if isBlank && !setted {
				/* 处理空值 */
				//if setted && (includeNil || isIncludedIds) {
				//if  (includeNil || isIncludedIds) {
				/* 分离关系表字段 */
				/*if field.IsInherited() {
					// 如果是继承字段移动到 rel_vals 里创建记录，因本Model对应的数据没有该字段
					tableName := field.ModelName() // rel_fld.RelateTableName
					rel_vals[tableName][name] = field.onConvertToWrite(self, fieldValue)
				} else {
					new_vals[name] = field.onConvertToWrite(self, fieldValue)
					record.SetByField(name, fieldValue)
				}*/

				/* 更新不需要检测字段 */
				if isIncludedIds {
					// If field is explicitly marked nullable, write nil as SQL NULL
					if nullableFields != nil {
						if isNullable, ok := nullableFields[name]; ok && isNullable {
							new_vals[name] = nil
						}
					}
					continue
				} else {
					// 未包含主键的数据,需要检测是否为必须字段
					isMustField := utils.IndexOf(name, mustFields...) != -1

					// nullableFields: treat "absent/nil map" as "no constraint" (i.e. nullable).
					// If present, the value means "is nullable".
					notNullable := false
					if nullableFields != nil {
						if isNullable, ok := nullableFields[name]; ok {
							notNullable = !isNullable
						}
					}

					if isMustField || field.Required() || notNullable {
						// Fields with setter/getter/default-func may compute their values from other inputs;
						// don't force the caller to provide an explicit value in that case.
						if field.HasSetter() || field.HasGetter() || field.DefaultFunc() != nil {
							continue
						}
						errs = append(errs, fmt.Sprintf("Field %s is required", field.Name()))
					}
				}
				//}
			}
		}

		/* 接下来fieldValue为什么值都要赋值包含空值也不例外！ */
		/*
			if field.SQLType().IsNumeric() {
				if v, ok := fieldValue.(string); ok {
					// 过滤0值字符串
					if v == "0" {
						fieldValue = 0
					} else {
						// 如果解析成数字成功则判定为数字成功 M2O 值可能是id或者Name值
						fieldValue=field.onConvertToWrite(self, fieldValue)
						if v := utils.ToInt(v); v != 0 {
							fieldValue = v
						}
					}
				}
			}
		*/
		// TODO 优化确认代码位置  !NOTE! 转换值为数据库类型
		//val = field.onConvertToWrite(self, val)

		//#*** 非Model固有字段归为关联表字段 2个判断缺一不可
		//#1 判断是否是关联表可能性
		//#2 判断是否Model和关联Model都有该字段
		if field.IsInherited() {
			tableName := field.ModelName()
			if (setted && includeNil) || !utils.IsBlank(fieldValue) {
				rel_vals[tableName][name] = fieldValue // 其他表的值无需格式化  field.onConvertToWrite(self, fieldValue)
			}

			continue
		}

		if field.Store() && field.SQLType().Name != "" {
			isExplicitlyNullable := nullableFields != nil && nullableFields[name]
			// explicit:调用方在 map 里明写了这个键(或 Select() 点了名),零值也要落库。
			// 没有这一条,Write(map[...]{"active": false}) 返回成功却什么都没改
			// (isBlank 把合法零值当"没提供"),是本仓最容易误判成业务 bug 的坑。
			if includeNil || !isBlank || isExplicitlyNullable || explicit {
				// explicitNil:更新时显式给的空值落成 SQL NULL。此前只有事先
				// Nullable() 声明过才走这条,否则 nil 被当"没提供"整列跳过、
				// 零时刻则被原样写成 0001-01-01——一个既非 NULL 又不报错的假日期。
				if (isExplicitlyNullable || explicitNil) && isBlank {
					new_vals[name] = nil // write SQL NULL for explicitly nullable blank field
				} else {
					fieldValue = field.onConvertToWrite(self, fieldValue)
					new_vals[name] = fieldValue
				}
				record.SetByField(name, fieldValue)
			}
		} else {
			//# 大型复杂计算字段
			upd_todo = append(upd_todo, field)
		}

		/*
			if field.IsClassicWrite() && field.Base().Fnct_inv() == nil {
				if !field.Translatable() { //TODO totranslate &&

					new_vals[name] = field.SymbolFunc()(utils.Itf2Str(val))

					//direct = append(direct, name)
				} else {
					upd_todo = append(upd_todo, name)
				}
			}
		*/
		/* check selection */
		if !field.IsInherited() && field.TypeName() == "selection" && fieldValue != nil {
			self._check_selection_field_value(field, fieldValue) //context
		}
	}

	for _, field = range ext_todo {
		name = field.Name()
		fieldValue, _ = self.orm._nowTime(field.TypeName()) //TODO 优化预先生成日期

		if len(new_vals) != 0 {
			new_vals[name] = fieldValue // 为当前表添加共同字段值
			record.SetByField(name, fieldValue)
		}

		for tbl := range self.Statement.Model.Obj().GetCommonFieldByName(name) {
			if data := rel_vals[tbl]; len(data) != 0 {
				rel_vals[tbl][name] = fieldValue // 为关联表添加共同字段值
			}
		}
	}

	// 如果出现错误
	if len(errs) != 0 {
		return nil, nil, nil, errors.New(errors.ErrValidation, fmt.Errorf("%s", strings.Join(errs, "\n")))
	}

	return new_vals, rel_vals, upd_todo, nil
}

// _structToMap is a thin session-bound wrapper around StructToMap that wires
// in Statement.Model and the Statement.OmitFields filter. Kept for backward
// compatibility with callers (including tests) that use the session method.
func (self *TSession) _structToMap(src any) map[string]any {
	return StructToMap(src, self.Statement.Model, self.Statement.OmitFields)
}

// Check whether value is among the valid values for the given
//
//	selection/reference field, and raise an exception if not.
func (self *TSession) _check_selection_field_value(field IField, value any) {
	//   field = self._fields[field]
	// field.convert_to_cache(value, self)
}

// expandToUniqueIndex 把「零散的唯一字段」补全成**一个完整的唯一索引的全部列**。
//
// 为什么必须这么做：INSERT ... ON CONFLICT (列…) 的冲突目标必须**恰好**对应一个
// 已存在的唯一约束/唯一索引。调用方按字段的 IsUnique() 标志收集出来的列表有两个
// 缺陷——① 丢了「这些列同属哪个唯一索引」的归属；② 来自 map 遍历，顺序随机。
// 于是复合唯一索引（如 pro.attr.value 的 (name, attribute_id)）会生成
// `ON CONFLICT ("name")` 这种只含其中一列、且每次运行还可能不同的目标，Postgres
// 必然报 42P10 "there is no unique or exclusion constraint matching..."。
//
// 选取规则：在模型声明的唯一索引里，挑第一个「所有列都出现在本次 INSERT 列表中」
// 的索引，按索引自身的列序返回。按索引名排序保证同一模型每次结果一致。
// 找不到合适的索引就原样返回入参，让上层沿用既有行为（通常回落到主键）。
func expandToUniqueIndex(indexes map[string]*TIndex, insertFields, uniqueFields []string) []string {
	if len(uniqueFields) == 0 || len(indexes) == 0 {
		return uniqueFields
	}

	inInsert := make(map[string]bool, len(insertFields))
	for _, f := range insertFields {
		inInsert[f] = true
	}
	picked := make(map[string]bool, len(uniqueFields))
	for _, f := range uniqueFields {
		picked[f] = true
	}

	names := make([]string, 0, len(indexes))
	for name := range indexes {
		names = append(names, name)
	}
	sort.Strings(names) // map 遍历顺序随机，排序后同一模型每次选中同一个索引

	for _, name := range names {
		idx := indexes[name]
		if idx == nil || idx.Type != UniqueType || len(idx.Cols) == 0 {
			continue
		}
		// 必须整组列都在本次 INSERT 里，缺一列这个索引就用不了；
		// 同时要求它确实覆盖了调用方识别出的某个唯一字段，避免选到无关索引。
		complete, relevant := true, false
		for _, col := range idx.Cols {
			if !inInsert[col] {
				complete = false
				break
			}
			if picked[col] {
				relevant = true
			}
		}
		if complete && relevant {
			return append([]string(nil), idx.Cols...)
		}
	}

	return uniqueFields
}
