package orm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/volts-dev/dataset"
	"github.com/volts-dev/utils"
)

// 「自定义规格」一对字段，对齐 Odoo 19 的 Properties / PropertiesDefinition
// （odoo/orm/fields_properties.py）。
//
// 分工是整个设计的要点，别混：
//
//	容器记录（如 pro.cat）  properties_definition 列：**有哪些规格项**
//	  [{"name":"3adf37f3","string":"材质","type":"char","default":"棉"}, ...]
//	明细记录（如 pro.tmpl） properties 列：**这条记录填了什么**
//	  {"3adf37f3":"丝"}
//
// 库里只存后者那个瘦字典——一万个产品共用一份定义，字符串"材质"不该复制一万遍。
// 读出去给前端的是**两者合并后的完整列表**（定义 ∪ 值），前端不必再取一次定义：
//
//	[{"name":"3adf37f3","string":"材质","type":"char","default":"棉","value":"丝"}]
//
// 写回来的也是这个完整列表。列表里带 `definition_changed` / `definition_deleted`
// 标记的项表示"用户顺手改了定义"（+ Add Property、改名、删项），这部分要写回**容器**。
// Odoo 用这个标记而不是每次比对定义，为的是省掉一次 SQL；本仓照抄，语义一致。
//
// ★ 与 pro.tmpl 的「属性和变体」（EAV，pro.tmpl.attr.item/value）没有任何共用代码：
// 那套是变体轴，会做笛卡尔积生成 SKU；这套是规格参数，只描述，不产生变体。

type (
	// TPropertiesDefinitionField 容器上的定义列（JSON 数组）。
	TPropertiesDefinitionField struct {
		TField
	}

	// TPropertiesField 明细上的值列（JSON 字典），定义取自 definition 指向的容器字段。
	TPropertiesField struct {
		TField
		// definitionRecord 本模型上指向容器的 many2one 字段名，如 "categ_id"
		definitionRecord string
		// definitionRecordField 容器模型上存定义的字段名，如 "product_properties_definition"
		definitionRecordField string
	}
)

// propertyAllowedTypes 当前支持的规格类型。
//
// Odoo 还支持 many2one / many2many / selection / tags / html，本仓分阶段落地——
// 未支持的类型在写入时**报错**而不是静默存下：存进去就会有人填值，等到真支持时
// 那些值的语义已经无从考证了。
var propertyAllowedTypes = map[string]bool{
	"boolean":   true,
	"integer":   true,
	"float":     true,
	"char":      true,
	"text":      true,
	"date":      true,
	"datetime":  true,
	"monetary":  true,
	"separator": true, // 纯 UI 分隔标题，没有值
}

func init() {
	RegisterField(TYPE_PROPERTIES, newPropertiesField)
	RegisterField(TYPE_PROPERTIES_DEFINITION, newPropertiesDefinitionField)
}

func newPropertiesField() IField {
	return new(TPropertiesField)
}

func newPropertiesDefinitionField() IField {
	return new(TPropertiesDefinitionField)
}

// IPostReadField 是「存储字段读完还要再加工一遍」的可选接口。
//
// 为什么需要它：_read() 把字段分成"从库里取"和"内存里算"两类，OnRead 只对后者触发。
// properties 两头都占——列要真读，读完还要跟容器的定义合并。不认这个接口的话，
// 前端拿到的是库里那个 {name:value} 瘦字典，没有 string/type，控件渲染不出任何东西。
type IPostReadField interface {
	IsPostRead() bool
}

func isPostReadField(field IField) bool {
	pr, ok := field.(IPostReadField)
	return ok && pr.IsPostRead()
}

// isPropertiesWriteField 报告该字段的写入是否必须走 OnWrite（而不是 onConvertToWrite）。
// 两者都要整条记录的上下文：值字段要知道容器是谁，定义字段要能把校验失败**报出去**
// （onConvertToWrite 的签名没有 error 位，校验失败只能吞掉）。
func isPropertiesWriteField(field IField) bool {
	switch field.(type) {
	case *TPropertiesField, *TPropertiesDefinitionField:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// 定义字段（容器侧）
// ---------------------------------------------------------------------------

func (self *TPropertiesDefinitionField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.SqlType = SQLType{Jsonb, 0, 0}
	field.typeName = TYPE_PROPERTIES_DEFINITION
	field.store = true
	field.IsJSON = true
	field.size = 0
}

func (self *TPropertiesDefinitionField) onConvertToRead(session *TSession, cols []string, record []any, colIndex int) any {
	value := *record[colIndex].(*any)
	return decodeJsonValue(value)
}

func (self *TPropertiesDefinitionField) onConvertToWrite(session *TSession, value any) any {
	return encodeJsonValue(value)
}

// OnWrite 校验定义并补全缺失的 name。
func (self *TPropertiesDefinitionField) OnWrite(ctx *TFieldContext) error {
	list, err := toPropertyList(ctx.Value)
	if err != nil {
		return fmt.Errorf("%s@%s: %s", ctx.Field.Name(), ctx.Field.ModelName(), err.Error())
	}

	if list == nil {
		ctx.values = nil
		return nil
	}

	addMissingPropertyNames(list)
	if err := validatePropertiesDefinition(list); err != nil {
		return fmt.Errorf("%s@%s: %s", ctx.Field.Name(), ctx.Field.ModelName(), err.Error())
	}

	// 定义里不该带 value：那是明细记录的东西。前端从产品表单改定义时会把整行原样发上来。
	for _, item := range list {
		delete(item, "value")
		delete(item, "definition_changed")
		delete(item, "definition_deleted")
	}

	ctx.values = encodeJsonValue(list)
	return nil
}

// ---------------------------------------------------------------------------
// 值字段（明细侧）
// ---------------------------------------------------------------------------

func (self *TPropertiesField) Init(ctx *TTagContext) {
	field := ctx.Field.Base()
	field.SqlType = SQLType{Jsonb, 0, 0}
	field.typeName = TYPE_PROPERTIES
	field.store = true
	field.IsJSON = true
	field.size = 0

	if len(ctx.Params) == 0 {
		log.Fatalf("the properties field %s@%s must declare its definition source, e.g. properties('categ_id.product_properties_definition')",
			field.Name(), field.ModelName())
		return
	}

	path := strings.Trim(ctx.Params[0], "'")
	rec, fld, found := strings.Cut(path, ".")
	if !found || rec == "" || fld == "" {
		log.Fatalf("the properties field %s@%s has a malformed definition path %q, want '<many2one field>.<definition field>'",
			field.Name(), field.ModelName(), path)
		return
	}
	self.definitionRecord = rec
	self.definitionRecordField = fld
}

func (self *TPropertiesField) Attributes(ctx *TTagContext) map[string]any {
	attrs := self.Base().Attributes(ctx)
	// 前端要知道定义存在哪，才能在用户改类别时重新拉定义、并把改定义的写入指向容器。
	attrs["definition_record"] = self.definitionRecord
	attrs["definition_record_field"] = self.definitionRecordField
	return attrs
}

func (self *TPropertiesField) IsPostRead() bool { return true }

func (self *TPropertiesField) onConvertToRead(session *TSession, cols []string, record []any, colIndex int) any {
	value := *record[colIndex].(*any)
	return decodeJsonValue(value)
}

func (self *TPropertiesField) onConvertToWrite(session *TSession, value any) any {
	return encodeJsonValue(value)
}

// OnRead 把库里的 {name: value} 与容器上的定义合并成完整列表。
//
// 三个批量步骤，全程 2 条 SQL 封顶：
//  1. 拿到每条记录的容器 id（数据集里有 definitionRecord 列就直接用，没有才补一次查询——
//     前端按需读列，只读 properties 不读 categ_id 是常态）
//  2. 一次读回所有涉及的容器定义
//  3. 逐记录合并
//
// 没有容器（类别为空）或容器没定义时给**空列表**，不是报错也不是原样返回瘦字典：
// 前端拿到空列表就是"这条记录没有规格项"，语义清楚。
func (self *TPropertiesField) OnRead(ctx *TFieldContext) error {
	ds := ctx.Dataset
	if ds == nil || ds.Count() == 0 {
		return nil
	}

	model := ctx.Model
	fieldName := ctx.Field.Name()
	idField := model.IdField()

	containerField := model.GetFieldByName(self.definitionRecord)
	if containerField == nil {
		return fmt.Errorf("the properties field %s@%s points at an unknown definition record field %q",
			fieldName, model.String(), self.definitionRecord)
	}
	containerModelName := containerField.RelatedModelName()
	if containerModelName == "" {
		return fmt.Errorf("the properties field %s@%s: %q is not a relational field",
			fieldName, model.String(), self.definitionRecord)
	}

	// 1. 每条记录的容器 id
	containerByRecord, err := self.readContainerIds(ctx, ds, idField)
	if err != nil {
		return err
	}
	if len(containerByRecord) == 0 {
		return nil
	}

	// 2. 容器定义
	containerIds := make([]any, 0, len(containerByRecord))
	seen := make(map[string]bool, len(containerByRecord))
	for _, cid := range containerByRecord {
		key := utils.ToString(cid)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		containerIds = append(containerIds, cid)
	}
	if len(containerIds) == 0 {
		return nil
	}

	containerModel, err := model.Orm().GetModel(containerModelName, WithContext(model.Options().Context))
	if err != nil {
		return err
	}
	sub := containerModel.Records()
	if ctx.Session != nil && ctx.Session.Schema != "" {
		// 子读取起的是全新会话，不继承调用方的 schema——非默认 schema 的租户下
		// 定义会去 public 里查，查不到就悄悄退化成"没有规格项"。
		sub.SetSchema(ctx.Session.Schema)
	}
	containerDs, err := sub.Select(containerModel.IdField(), self.definitionRecordField).Ids(containerIds...).Read()
	if err != nil {
		return err
	}

	definitions := make(map[string][]map[string]any, containerDs.Count())
	containerDs.Range(func(pos int, rec *dataset.TRecordSet) error {
		key := utils.ToString(rec.GetByField(containerModel.IdField()))
		list, err := toPropertyList(decodeJsonValue(rec.GetByField(self.definitionRecordField)))
		if err != nil {
			log.Warnf("%s@%s holds a malformed properties definition: %s", self.definitionRecordField, containerModelName, err.Error())
			return nil
		}
		definitions[key] = list
		return nil
	})

	// 3. 合并
	ds.Range(func(pos int, rec *dataset.TRecordSet) error {
		recKey := utils.ToString(rec.GetByField(idField))
		cid, has := containerByRecord[recKey]
		if !has {
			rec.SetByField(fieldName, []map[string]any{})
			return nil
		}
		definition := definitions[utils.ToString(cid)]
		values := toPropertyDict(decodeJsonValue(rec.GetByField(fieldName)))
		rec.SetByField(fieldName, dictToPropertyList(values, definition))
		return nil
	})

	return nil
}

// readContainerIds 返回 记录id(字符串) -> 容器id。
func (self *TPropertiesField) readContainerIds(ctx *TFieldContext, ds *dataset.TDataSet, idField string) (map[string]any, error) {
	out := make(map[string]any, ds.Count())

	missing := make([]any, 0)
	ds.Range(func(pos int, rec *dataset.TRecordSet) error {
		recId := rec.GetByField(idField)
		if utils.IsBlank(recId) {
			return nil
		}
		cid := rec.GetByField(self.definitionRecord)
		if cid == nil {
			missing = append(missing, recId)
			return nil
		}
		if !utils.IsBlank(cid) {
			out[utils.ToString(recId)] = m2oRawId(cid)
		}
		return nil
	})

	if len(missing) == 0 {
		return out, nil
	}

	// 数据集里没有容器列（调用方没读它）——补一次最小查询，只取 id + 容器外键。
	sub := ctx.Model.Records()
	if ctx.Session != nil && ctx.Session.Schema != "" {
		sub.SetSchema(ctx.Session.Schema)
	}
	extra, err := sub.Select(idField, self.definitionRecord).Ids(missing...).Read()
	if err != nil {
		return nil, err
	}
	extra.Range(func(pos int, rec *dataset.TRecordSet) error {
		cid := rec.GetByField(self.definitionRecord)
		if !utils.IsBlank(cid) {
			out[utils.ToString(rec.GetByField(idField))] = m2oRawId(cid)
		}
		return nil
	})

	return out, nil
}

// OnWrite 把前端发上来的完整列表拆成两部分：定义写回容器，值裁剪后落本记录的列。
func (self *TPropertiesField) OnWrite(ctx *TFieldContext) error {
	value := ctx.Value

	// dict 形态（内部调用、或只改值不改定义）直接落库，不碰容器定义。
	if dict, ok := asPropertyDict(value); ok {
		ctx.values = encodeJsonValue(dict)
		return nil
	}

	list, err := toPropertyList(value)
	if err != nil {
		return fmt.Errorf("%s@%s: %s", ctx.Field.Name(), ctx.Field.ModelName(), err.Error())
	}
	if list == nil {
		// 调用方**没提供**这个字段。更新时那就是"别动"；新建时不是。
		//
		// Odoo 的 Properties 是个 compute 字段（`_depends = (definition_record,)`、
		// `precompute = True`、`store = True`），新建时会按容器的定义把默认值算出来存下
		// （fields_properties.py 的 `_compute` → `_add_default_values`）。少了这一步，
		// "在类别上给材质设了默认值棉"的新产品打开是**空的**，而同类别里改过一次规格
		// 的产品有值——同一个类别下两条产品显示不一样，且没有任何报错。
		if len(ctx.Ids) > 0 {
			ctx.values = nil
			return nil
		}
		defaults, err := self.defaultsFromContainer(ctx)
		if err != nil {
			return err
		}
		if len(defaults) == 0 {
			ctx.values = nil
			return nil
		}
		// 往下走统一的"补默认值 → 落瘦字典"。这份列表里没有 definition_changed，
		// 所以不会反过来重写容器的定义。
		list = defaults
	}

	addMissingPropertyNames(list)

	definitionChanged := false
	kept := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if utils.ToBool(item["definition_deleted"]) {
			// 删项：整项从定义里去掉，本记录上那个值也随之失去意义。
			definitionChanged = true
			continue
		}
		if utils.ToBool(item["definition_changed"]) {
			definitionChanged = true
		}
		delete(item, "definition_changed")
		delete(item, "definition_deleted")
		kept = append(kept, item)
	}

	if err := validatePropertiesDefinition(kept); err != nil {
		return fmt.Errorf("%s@%s: %s", ctx.Field.Name(), ctx.Field.ModelName(), err.Error())
	}

	if definitionChanged {
		if err := self.writeDefinition(ctx, kept); err != nil {
			return err
		}
	}

	// 没填值的项落默认值——Odoo 在容器变化时用 compute 做同一件事。
	for _, item := range kept {
		if item["value"] == nil {
			if def, has := item["default"]; has && !utils.IsBlank(def) {
				item["value"] = def
			}
		}
	}

	ctx.values = encodeJsonValue(propertyListToDict(kept))
	return nil
}

// writeDefinition 把改动后的定义写回容器记录。
func (self *TPropertiesField) writeDefinition(ctx *TFieldContext, list []map[string]any) error {
	model := ctx.Model
	containerId, err := self.resolveContainerId(ctx)
	if err != nil {
		return err
	}
	if utils.IsBlank(containerId) {
		// 没有容器就无处存定义。这不该静默——用户刚在界面上加了一个规格项，
		// 保存后它会凭空消失，而且没有任何提示。
		return fmt.Errorf("%s@%s: cannot change the properties definition, %s is empty",
			ctx.Field.Name(), model.String(), self.definitionRecord)
	}

	containerField := model.GetFieldByName(self.definitionRecord)
	if containerField == nil {
		return fmt.Errorf("%s@%s points at an unknown definition record field %q",
			ctx.Field.Name(), model.String(), self.definitionRecord)
	}
	containerModel, err := model.Orm().GetModel(containerField.RelatedModelName(), WithContext(model.Options().Context))
	if err != nil {
		return err
	}

	// 定义里不留 value：值是明细的东西，留着会让"每个产品各存一份定义"重新发生。
	clean := make([]map[string]any, 0, len(list))
	for _, item := range list {
		cp := make(map[string]any, len(item))
		for k, v := range item {
			if k == "value" {
				continue
			}
			cp[k] = v
		}
		clean = append(clean, cp)
	}

	sub := containerModel.Records()
	if ctx.Session != nil && ctx.Session.Schema != "" {
		sub.SetSchema(ctx.Session.Schema)
	}
	_, err = sub.Ids(containerId).Write(map[string]any{
		self.definitionRecordField: clean,
	})
	if err != nil {
		return err
	}

	log.Infof("properties: definition of %s#%v changed via %s@%s (%d items)",
		containerModel.String(), containerId, ctx.Field.Name(), model.String(), len(clean))
	return nil
}

// defaultsFromContainer 按容器上的定义造一份"只有默认值"的列表，用于新建记录时补值。
//
// 返回的每一项都是副本：定义那份 map 来自容器的读取结果，下游会往里塞 value，
// 就地改写的话同一次请求里建的多条记录会互相串味。
func (self *TPropertiesField) defaultsFromContainer(ctx *TFieldContext) ([]map[string]any, error) {
	containerId, err := self.resolveContainerId(ctx)
	if err != nil {
		return nil, err
	}
	if utils.IsBlank(containerId) {
		return nil, nil
	}

	model := ctx.Model
	containerField := model.GetFieldByName(self.definitionRecord)
	if containerField == nil {
		return nil, fmt.Errorf("%s@%s points at an unknown definition record field %q",
			ctx.Field.Name(), model.String(), self.definitionRecord)
	}
	containerModel, err := model.Orm().GetModel(containerField.RelatedModelName(), WithContext(model.Options().Context))
	if err != nil {
		return nil, err
	}

	sub := containerModel.Records()
	if ctx.Session != nil && ctx.Session.Schema != "" {
		// 子读取起的是全新会话，不继承调用方的 schema——非默认 schema 的租户下
		// 会去 public 里查，查不到就悄悄退化成"没有默认值"。
		sub.SetSchema(ctx.Session.Schema)
	}
	ds, err := sub.Select(containerModel.IdField(), self.definitionRecordField).Ids(containerId).Read()
	if err != nil {
		return nil, err
	}
	if ds.Count() == 0 {
		return nil, nil
	}

	definition, err := toPropertyList(decodeJsonValue(ds.Record().GetByField(self.definitionRecordField)))
	if err != nil {
		log.Warnf("%s@%s holds a malformed properties definition: %s",
			self.definitionRecordField, containerModel.String(), err.Error())
		return nil, nil
	}

	out := make([]map[string]any, 0, len(definition))
	for _, item := range definition {
		cp := make(map[string]any, len(item)+1)
		for k, v := range item {
			cp[k] = v
		}
		out = append(out, cp)
	}
	return out, nil
}

// resolveContainerId 找出本次写入涉及的容器 id：优先取本次载荷里的值（新建、或同时改了
// 类别），否则回读记录。
func (self *TPropertiesField) resolveContainerId(ctx *TFieldContext) (any, error) {
	if ctx.Dataset != nil && ctx.Dataset.Count() > 0 {
		if v := ctx.Dataset.Record().GetByField(self.definitionRecord); !utils.IsBlank(v) {
			return m2oRawId(v), nil
		}
	}

	if len(ctx.Ids) == 0 {
		return nil, nil
	}

	sub := ctx.Model.Records()
	if ctx.Session != nil && ctx.Session.Schema != "" {
		sub.SetSchema(ctx.Session.Schema)
	}
	ds, err := sub.Select(ctx.Model.IdField(), self.definitionRecord).Ids(ctx.Ids...).Read()
	if err != nil {
		return nil, err
	}
	if ds.Count() == 0 {
		return nil, nil
	}
	// 多条记录同时改定义是有歧义的（它们可能挂在不同容器上），Odoo 直接拒绝。
	ids := make(map[string]bool, ds.Count())
	var first any
	ds.Range(func(pos int, rec *dataset.TRecordSet) error {
		v := rec.GetByField(self.definitionRecord)
		if utils.IsBlank(v) {
			return nil
		}
		raw := m2oRawId(v)
		if len(ids) == 0 {
			first = raw
		}
		ids[utils.ToString(raw)] = true
		return nil
	})
	if len(ids) > 1 {
		return nil, fmt.Errorf("updating records with different properties definitions is not supported, update them by definition instead")
	}
	return first, nil
}

// ---------------------------------------------------------------------------
// 纯函数区：列表/字典互转、校验、命名
// ---------------------------------------------------------------------------

// toPropertyList 把任意形态的定义/值列表归一成 []map[string]any。
func toPropertyList(value any) ([]map[string]any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case []map[string]any:
		return v, nil
	case string:
		if v == "" {
			return nil, nil
		}
		decoded := decodeJsonValue(v)
		if _, still := decoded.(string); still {
			return nil, fmt.Errorf("malformed properties json %q", v)
		}
		return toPropertyList(decoded)
	case []byte:
		return toPropertyList(string(v))
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("each property must be an object, got %T", item)
			}
			out = append(out, m)
		}
		return out, nil
	}
	return nil, fmt.Errorf("wrong properties value type %T", value)
}

// asPropertyDict 判断值是否已是 {name: value} 瘦字典形态。
//
// 注意顺序：调用方要先试 list 再试 dict 是不行的——JSON 里 `{}` 和 `[]` 泾渭分明，
// 这里按 Go 类型判定，不会误判。
func asPropertyDict(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case string:
		if v == "" {
			return nil, false
		}
		if decoded, ok := decodeJsonValue(v).(map[string]any); ok {
			return decoded, true
		}
	case []byte:
		if decoded, ok := decodeJsonValue(string(v)).(map[string]any); ok {
			return decoded, true
		}
	}
	return nil, false
}

// toPropertyDict 把库里读回来的值归一成 {name: value}。
func toPropertyDict(value any) map[string]any {
	if v, ok := asPropertyDict(value); ok {
		return v
	}
	return map[string]any{}
}

// dictToPropertyList 定义 ∪ 值 → 完整列表。定义里没有的键会被丢掉（容器上删过的项）。
func dictToPropertyList(values map[string]any, definition []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(definition))
	for _, item := range definition {
		cp := make(map[string]any, len(item)+1)
		for k, v := range item {
			cp[k] = v
		}
		name := utils.ToString(cp["name"])
		if v, has := values[name]; has {
			cp["value"] = v
		} else {
			delete(cp, "value")
		}
		out = append(out, cp)
	}
	return out
}

// propertyListToDict 完整列表 → {name: value} 瘦字典。
func propertyListToDict(list []map[string]any) map[string]any {
	out := make(map[string]any, len(list))
	for _, item := range list {
		name := utils.ToString(item["name"])
		if name == "" {
			continue
		}
		value, has := item["value"]
		if !has || value == nil {
			// 键都不留：读的时候会从定义补 default，存一个 null 反而会盖掉默认值。
			continue
		}

		switch utils.ToString(item["type"]) {
		case "integer", "float", "monetary":
			// 数值的 0 是**合法值**，不能跟"空"混为一谈——这正是 Odoo 在
			// _list_to_dict 里给数值类型开的那个口子。
		case "separator":
			// 分隔符没有值。
			continue
		default:
			if utils.IsBlank(value) {
				value = false
			}
		}
		out[name] = value
	}
	return out
}

// addMissingPropertyNames 给新增项生成 name（16 位十六进制，同 Odoo 取 uuid4 的前 64 位）。
//
// name 是定义与值之间**唯一**的关联键，所以它必须在服务端生成：让前端造 id，
// 两个人同时加规格项就可能撞名，撞上的那一刻两项的值会互相覆盖。
func addMissingPropertyNames(list []map[string]any) {
	for _, item := range list {
		if name := utils.ToString(item["name"]); name != "" {
			continue
		}
		item["name"] = newPropertyName()
	}
}

func newPropertyName() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 在 Linux/macOS 上失败意味着系统级故障。这里返回空名字，
		// 让紧随其后的 validatePropertiesDefinition 把它当"没有 name"顶回去——
		// 编一个非随机的名字更糟：它迟早会跟另一条记录撞上，而撞上的表现是
		// 两个规格项的值互相覆盖，届时没人会想到根因在这。
		log.Errf("properties: failed to generate a property name: %s", err.Error())
		return ""
	}
	return hex.EncodeToString(buf[:])
}

// validatePropertiesDefinition 校验定义列表。
func validatePropertiesDefinition(list []map[string]any) error {
	seen := make(map[string]bool, len(list))
	for _, item := range list {
		name := utils.ToString(item["name"])
		if name == "" {
			return fmt.Errorf("a property definition must have a name")
		}
		if seen[name] {
			return fmt.Errorf("duplicated property name %q", name)
		}
		seen[name] = true

		typ := utils.ToString(item["type"])
		if typ == "" {
			return fmt.Errorf("the property %q must have a type", name)
		}
		if !propertyAllowedTypes[typ] {
			return fmt.Errorf("the property type %q is not supported yet (property %q)", typ, name)
		}
	}
	return nil
}

// m2oRawId 把 many2one 的各种形态（裸 id / [id,name] 经典元组 / {id:...} 记录）归一成裸 id。
func m2oRawId(value any) any {
	switch v := value.(type) {
	case []any:
		if len(v) > 0 {
			return v[0]
		}
		return nil
	case map[string]any:
		if id, has := v["id"]; has {
			return id
		}
		return nil
	}
	return value
}
