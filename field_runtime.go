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
//   - 同名不同类型：返回错误，不替换。替换一个正在被读写的字段对象不安全，改类型要走迁移。
//   - 多个进程同时补同一列：先查后加，加失败时再查一次，列已在就按成功处理。
//
// DisableSchemaSync 时不发任何 DDL（契约是库结构已由部署期迁移就位），只挂字段。
func (self *TOrm) AddField(modelName, fieldName, tag string) (added bool, err error) {
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

	if existing := model.GetFieldByName(fieldName); existing != nil {
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
			if err := self.materializeField(schema, model, field); err != nil {
				return false, fmt.Errorf("orm: AddField %s.%s (schema %q): %w", modelName, fieldName, schema, err)
			}
		}
	}

	if err := self.resolveFieldRef(model, field); err != nil {
		return false, err
	}
	model.obj.SetField(field)
	return true, nil
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
func (self *TOrm) materializeField(schema string, model *TModel, field IField) error {
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
			if _, err := self.Exec(self.dialect.GenAddColumnSQL(schema, table, field)); err != nil {
				// 另一个进程抢先补上了：列在就算成功。
				if again, _ := self.dialect.IsColumnExist(ctx, schema, table, field.Name()); !again {
					return err
				}
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
		field.UpdateDb(&TTagContext{Orm: self, Field: field, Model: model, Session: session})
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
