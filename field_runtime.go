package orm

import (
	"context"
	"fmt"
	"strings"

	"github.com/volts-dev/utils"
)

// parseFieldTag 把一条字段 tag（如 `many2one(res.partner) title('Pricelist') index`）
// 拆成「属性名 → 参数」与属性的处理顺序，并认出其中的字段类型。
//
// 结构体映射（_mapping）与运行期补字段（AddField）共用这一份解析 —— 两条路认的
// tag 语法必须一模一样，否则同一个字段写在结构体上和从声明里补上来会是两个样子。
//
// rawTag 只用于报错信息（结构体上的完整 tag 原文）。
func parseFieldTag(modelName, fieldName, rawTag, tagStr string, isTableTag bool) (map[string][]string, []string, string, error) {
	tagMaps := make(map[string][]string)
	order := make([]string, 0)
	var fieldTypeName string

	for _, key := range splitTag(tagStr) {
		//NOTE 以下代码是为了避免解析不规则字符串为字段名提醒使用者规范Tag数据格式应该注意不用空格
		attrs := parseTag(key)
		if len(attrs) == 0 {
			return nil, nil, "", fmt.Errorf("Tag parse failed: Model:%s Field:%s Tag:%s Key:%s Result:%v", modelName, fieldName, rawTag, key, attrs)
		}
		typeName := strings.ToLower(attrs[0])
		s := strings.Replace(key, typeName, "", 1) // 去掉Tag Item
		s = strings.TrimLeft(s, "(")
		s = strings.TrimRight(s, ")")
		if len(s) > 0 && strings.Contains(s, " ") {
			if !strings.HasPrefix(s, "'") && !strings.HasSuffix(s, "'") {
				return nil, nil, "", fmt.Errorf("Model %s's %s tags could no including space ' ' in brackets value whicth it not 'String' type.", modelName, strings.ToUpper(fieldName))
			}
		}
		tagMaps[typeName] = attrs[1:]
		order = append(order, typeName)

		if !isTableTag && IsFieldType(typeName) {
			fieldTypeName = typeName
		}
	}
	return tagMaps, order, fieldTypeName, nil
}

// AddField 给一个**已注册**的模型补一个字段 —— 模型集合冻结（Freeze）之后也可以。
//
// 用途：字段的定义不在属主的 Go 代码里，而是运行期才知道。vectors 的扩展字段就是这样：
// 别的模块声明、存在属主表上的列（sale 给 res.partner 的 sale_warn_msg）。tag 语法与
// 结构体上的 `field:"..."` 完全相同。
//
// # 顺序是承重的：先建列、后挂字段
//
// 字段对象存在**共享的** TModelObject 上，挂上去的那一刻起，所有会话读这个模型都会把它
// 放进 SELECT。列还没建好就挂，这张表从那一刻起每一次读都报 column does not exist。
// 所以这里先在库里把列（以及它的索引、many2many 关联表）建好，最后一步才 SetField。
//
// # 每一份表都要补
//
// 同一个模型的表可能不止一份：schema 隔离的租户（vectors 的 VectorsSystem）把模型物化进
// 自己的 schema。只补默认 schema 的话，那个租户读这个模型就报列不存在。所以 postgres 上
// 补到**每一个有这张表的 schema**。
//
// # 幂等与并发
//
//   - 同名同类型的字段已在模型上：返回 (false, nil)，什么也不做。
//   - 同名字段只是从库里反查出来的（上一次运行补过这一列，这次重启反查到了）：
//     按声明重建并替换它，返回 (true, nil)。列已存在，不发 DDL。
//   - 同名不同类型：返回错误，不替换。替换一个正在被读写的字段对象不安全，改类型要走迁移。
//   - 多个进程同时补同一列：先查后加，加失败时再查一次，列已在就按成功处理。
//
// DisableSchemaSync 时不发任何 DDL（契约是库结构已由部署期迁移就位），只挂字段。
//
// # 一次性回填（WithBackfill）
//
// 字段搬家时（值原来存在别处，现在改成这张表上的一列），列建出来的**那一刻**要把旧值
// 拷过来。WithBackfill 给的函数与建列在**同一个事务**里执行：拷贝失败，列也不建，下次
// 再补时重来；列已经在了（上一次建过、或别的进程抢先建了）就不再执行 —— 于是它在每个
// schema 里恰好成功执行一次，不需要另记「迁过没有」。many2many 同理，时机是关联表建出来
// 的那一刻。
func (self *TOrm) AddField(modelName, fieldName, tag string, opts ...AddFieldOption) (added bool, err error) {
	var o addFieldOptions
	for _, opt := range opts {
		opt(&o)
	}

	m, err := self.osv.GetModel(modelName)
	if err != nil {
		return false, err
	}
	model := m.GetBase()
	if model == nil || model.obj == nil {
		return false, fmt.Errorf("orm: AddField: model %q is not a local model", modelName)
	}

	tagMaps, order, typeName, err := parseFieldTag(modelName, fieldName, tag, tag, false)
	if err != nil {
		return false, err
	}
	if !IsFieldType(typeName) {
		return false, fmt.Errorf("orm: AddField %s.%s: tag %q names no field type", modelName, fieldName, tag)
	}

	// 反查得来、没有声明认领的同名字段不算「已声明」：那是上一次运行补的列，重启后被反查
	// 成了只有 SQL 类型的样子（见 TField.introspected），这里按声明重建它。
	if existing := model.GetFieldByName(fieldName); existing != nil && !existing.Base().introspected {
		// 比的是 Init 之后的类型名：数值、布尔、json 这几类在 Init 里会改写成 SQL 类型名
		// （bool → BOOL、json → jsonb），拿 tag 上的原文去比，同一个声明补第二次会被当成换类型。
		// 这里只跑 Init，不跑其余 tag —— index / default 这类 tag 会往共享模型上登记东西。
		probe, err := NewField(fieldName, WithFieldType(typeName), WithModel(model))
		if err != nil {
			return false, err
		}
		probe.Init(&TTagContext{Orm: self, Model: model, Field: probe, ModelValue: model.modelValue, Params: tagMaps[typeName]})
		if strings.EqualFold(existing.TypeName(), probe.TypeName()) {
			return false, nil
		}
		return false, fmt.Errorf("orm: AddField %s.%s: already declared as %s, refusing to replace it with %s",
			modelName, fieldName, existing.TypeName(), probe.TypeName())
	}

	field, err := self.buildField(model, fieldName, typeName, tagMaps, order)
	if err != nil {
		return false, err
	}

	if !self.config.DisableSchemaSync {
		for _, schema := range self.schemasWithTable(model.Table()) {
			if err := self.materializeField(schema, model, field, &o); err != nil {
				return false, fmt.Errorf("orm: AddField %s.%s (schema %q): %w", modelName, fieldName, schema, err)
			}
		}
	}

	if err := self.resolveFieldRef(model, field); err != nil {
		return false, err
	}
	model.obj.SetField(field)
	// 删除时「谁指向我」的两份索引是按模型集合建的缓存：不作废的话，运行期补上来的
	// many2one / many2many 带着 ondelete 声明也没人执行（目标记录删了，这一列还指着它）。
	self.osv.invalidateM2ORefIndex()
	self.osv.invalidateM2MRefIndex()
	return true, nil
}

// AddFieldOption 是 AddField 的可选项。
type AddFieldOption func(*addFieldOptions)

type addFieldOptions struct {
	backfill func(s *TSession) error
}

// WithBackfill 在这一列（many2many 是关联表）**第一次建出来的那个事务里**执行 fn，见 AddField。
//
// fn 拿到的会话已经在事务里、Schema 是这一次建列所在的 schema，裸 SQL 的表名按
// search_path 落在这个 schema（再回落 public）。fn 返回错误则整个事务回滚、列不建，
// AddField 返回这个错误。
func WithBackfill(fn func(s *TSession) error) AddFieldOption {
	return func(o *addFieldOptions) { o.backfill = fn }
}

// buildField 按 tag 建一个**游离**的字段对象：初始化与 tag 处理都做完，但还没挂到模型上。
// 与 _mapping 里结构体字段那一段同序（Init → SyncToSqlType → 其余 tag → 默认长度与标题），
// 只是没有 Go 类型可推 SQL 类型 —— 类型完全由 tag 决定。
func (self *TOrm) buildField(model *TModel, fieldName, typeName string, tagMaps map[string][]string, order []string) (IField, error) {
	field, err := NewField(fieldName, WithFieldType(typeName), WithModel(model))
	if err != nil {
		return nil, err
	}
	field.Base().modelName = model.String()

	ctx := &TTagContext{
		Orm:        self,
		Model:      model,
		Field:      field,
		ModelValue: model.modelValue,
		Params:     tagMaps[typeName],
	}
	field.Init(ctx)
	self.dialect.SyncToSqlType(ctx)
	if err := self._handleTags(ctx, tagMaps, order, ""); err != nil {
		return nil, err
	}

	if field.Store() && !field.IsRelated() && field.SQLType().Name == "" {
		return nil, fmt.Errorf("orm: AddField %s.%s: type %q has no SQL type — give it one in the tag (e.g. varchar(64))",
			model.String(), fieldName, typeName)
	}
	if field.Base().size == 0 {
		field.Base().size = field.SQLType().DefaultLength
	}
	if field.Label() == "" {
		field.Base().label = utils.TitleCasedNameWithSpace(field.Name())
	}
	if field.Base().description == "" && field.Label() != "" {
		field.Base().description = field.Label()
	}
	return field, nil
}

// schemasWithTable 返回库里有这张表的所有 schema。查不了（非 postgres、或查询失败）
// 时退回 [""]，即会话默认 schema —— 与此前"只补默认那一份"的行为一致。
func (self *TOrm) schemasWithTable(table string) []string {
	if !strings.Contains(strings.ToLower(self.dialect.String()), "postgres") {
		return []string{""}
	}
	ds, err := self.Query(`SELECT table_schema FROM information_schema.tables WHERE table_name = ? ORDER BY table_schema`, table)
	if err != nil || ds == nil || ds.Count() == 0 {
		return []string{""}
	}
	out := make([]string, 0, ds.Count())
	for ds.First(); !ds.Eof(); ds.Next() {
		out = append(out, ds.FieldByName("table_schema").AsString())
	}
	return out
}

// materializeField 在一个 schema 里把字段落到库上：列、它的索引、many2many 关联表。
func (self *TOrm) materializeField(schema string, model *TModel, field IField, o *addFieldOptions) error {
	ctx := self.context
	if ctx == nil {
		ctx = context.Background()
	}
	table := model.Table()

	if field.Store() && !field.IsInherited() && !isX2Many(field) {
		exists, err := self.dialect.IsColumnExist(ctx, schema, table, field.Name())
		if err != nil {
			return err
		}
		if !exists {
			if err := self.addColumn(ctx, schema, table, field, o); err != nil {
				return err
			}
		}
	}

	session := self.NewSession()
	defer session.Close()
	session.Schema = schema
	session.Model(model.String())

	for name, index := range model.GetIndexes() {
		if utils.IndexOf(field.Name(), index.Cols...) == -1 {
			continue
		}
		if err := session._addIndex(table, name); err != nil {
			return err
		}
	}

	if field.IsRelated() {
		if field.TypeName() == TYPE_M2M && o != nil && o.backfill != nil {
			return self.addRelationTable(schema, model, field, o)
		}
		field.UpdateDb(&TTagContext{Orm: self, Field: field, Model: model, Session: session})
	}
	return nil
}

// addColumn 建列；带回填时建列与回填在同一个事务里（见 WithBackfill）。
func (self *TOrm) addColumn(ctx context.Context, schema, table string, field IField, o *addFieldOptions) error {
	ddl := self.dialect.GenAddColumnSQL(schema, table, field)
	if o == nil || o.backfill == nil {
		if _, err := self.Exec(ddl); err != nil {
			// 另一个进程抢先补上了：列在就算成功。
			if again, _ := self.dialect.IsColumnExist(ctx, schema, table, field.Name()); !again {
				return err
			}
		}
		return nil
	}

	s := self.NewSession()
	defer s.Close()
	s.Schema = schema
	if err := s.Begin(); err != nil {
		return err
	}
	if _, err := s.Exec(ddl); err != nil {
		s.Rollback(err)
		// 别的进程抢先建了（连同它的回填一起提交了）：列在就算成功，回填不再做。
		if again, _ := self.dialect.IsColumnExist(ctx, schema, table, field.Name()); again {
			return nil
		}
		return err
	}
	if err := o.backfill(s); err != nil {
		s.Rollback(err)
		return fmt.Errorf("backfill: %w", err)
	}
	return s.Commit()
}

// addRelationTable 建 many2many 关联表；关联表是这一次才建出来的，就在同一个事务里回填。
func (self *TOrm) addRelationTable(schema string, model *TModel, field IField, o *addFieldOptions) error {
	rel := strings.Replace(field.JoinModelName(), ".", "_", -1)
	s := self.NewSession()
	defer s.Close()
	s.Schema = schema
	if err := s.Begin(); err != nil {
		return err
	}
	sql, args := self.dialect.TableCheckSql(schema, rel)
	ds, err := s._query(sql, args...)
	if err != nil {
		s.Rollback(err)
		return err
	}
	existed := ds.Count() > 0

	s.Model(model.String())
	field.UpdateDb(&TTagContext{Orm: self, Field: field, Model: model, Session: s})
	if !existed {
		if err := o.backfill(s); err != nil {
			s.Rollback(err)
			// UpdateDb 按 (schema, 表) 记了「这个进程建过」；事务回滚了，要让下一次补字段重建。
			self.osv.middleModelDDL.Delete(schema + "|" + rel)
			return fmt.Errorf("backfill: %w", err)
		}
	}
	// UpdateDb 的 DDL 出错只记日志不返回；在事务里出过错，提交这一步才报出来。
	if err := s.Commit(); err != nil {
		self.osv.middleModelDDL.Delete(schema + "|" + rel)
		return err
	}
	return nil
}

func isX2Many(field IField) bool {
	switch field.TypeName() {
	case TYPE_O2M, TYPE_M2M:
		return true
	}
	return false
}

// resolveFieldRef 让运行期补上来的关系字段立刻认得它的目标模型。
//
// 启动期这件事由 Freeze 统一做（field.Init 里 markPending 登记、Freeze 逐条解析）。冻结之后
// 没有人再处理 pendingRefs，所以这里就地做：目标在本进程 → 什么都不用做（运行期按名字
// GetModel）；目标不在本进程 → 向 resolver 要 schema、登记成远程模型，与 Freeze 第 3 步同一做法。
func (self *TOrm) resolveFieldRef(model *TModel, field IField) error {
	if !field.IsRelated() {
		return nil
	}
	to := field.Base().relatedModelName
	if to == "" {
		return nil
	}
	if _, ok := self.osv.models.Load(to); ok {
		return nil
	}
	if field.TypeName() == TYPE_O2O {
		return fmt.Errorf("orm: AddField %s.%s: one2one to a model this process does not have (%s) is not supported",
			model.String(), field.Name(), to)
	}
	if self.osv.resolver == nil {
		return nil // 与 Freeze 在非严格模式下对待未解析引用的方式一致：留到运行期再说
	}
	ctx := self.context
	if ctx == nil {
		ctx = context.Background()
	}
	schema, err := self.osv.resolver.LookupSchema(ctx, to)
	if err != nil {
		return nil
	}
	remote := newRemoteModelObject(schema, self.osv.resolver)
	remote.orm = self
	remote.osv = self.osv
	self.osv.models.LoadOrStore(to, remote)
	return nil
}
