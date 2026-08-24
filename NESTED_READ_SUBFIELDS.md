# 单次嵌套 Read：关系字段子记录内嵌（SubFields）

## 目标

form view 的 m2m/o2m/m2o 组件的初始化值，**不再各自独立调用 API** 获取，而是和普通
标量字段一样，由 form 的一次 `read` 返回的数据集中获得。组件自动从数据集的字段值里取自己
需要的子字段，例如：

- **many2one**：捕获 `display_name`；
- **many2many_tags**：自带 `display_name`、`color`；
- **many2many（内嵌 list）/ one2many**：内嵌 list 视图中所有可见列。

实现方式：单次 `read` 携带一棵"字段树"，关系字段带上其 comodel 所需的子字段规格；ORM 据此
把子记录**内嵌**进返回数据集。

本仓库改动覆盖 **ORM 后端** 与 **前端 BasicModel**；中间的 HTTP 桥接层（`call_kw`
控制器）不在本仓库，其改法在下方"③ HTTP 桥接层契约"给出。

---

## ① ORM 后端（本仓库，已实现 + 测试）

### 数据结构

`ReadRequest` 新增递归子规格（[model_request.go](model_request.go)）：

```go
// 键为本模型上的关系字段名；值描述其 comodel 的读取（Fields/Domain/SubFields）。
SubFields map[string]*ReadRequest
```

`TSession.subReads`（[session.go](session.go)）承载该规格；`TFieldContext.SubFields`
（[field.go](field.go)）用于把下一层规格透传给关系字段的子读取，支持多层内嵌。

### 派发逻辑（[session_crwd.go](session_crwd.go) `_read`）

- 派发门控：`UseNameGet || IsClassic || len(subReads) > 0 || 有非存储标量计算字段 ||
  有 post-read 字段 || len(namedRelated) > 0`。
- **纯嵌套规格模式**（未全局 Classic/NameGet）下只处理两种关系字段：带子规格的，以及
  调用方 `Select()` 点了名的。其余一律跳过——一次 `Read()` 会遍历模型上全部字段，给每个
  x2many 都补一次子查询等于把每次读放大成 N+1。
- 带子规格的字段：`ctx.Fields = sub.Fields`、`ctx.SubFields = sub.SubFields`、
  `ctx.Domain = sub.Domain(string)`，并置 `ctx.ClassicRead = true`（列范围由 `ctx.Fields`
  限定；递归深度由 `ctx.SubFields` 决定）。

### 读出口形态契约（关键）

同一个字段、同一条读取路径，形态**不随数据变**。

| 字段类型 | 没给子规格 | 给了子规格 |
| --- | --- | --- |
| many2one | 经典读/NameGet：有值 `map{id,name,…}`、没值 `false`、悬空 `map{id}`；<br>plain 读：裸外键（存储值） | `map[string]any`，仅含请求列 + id |
| one2many | `[]any`（对端 id 列表，空关联是空切片） | `[]map[string]any`，仅含请求列 + id + 反向 FK |
| many2many | `[]any`（对端 id 列表，空关联是空切片） | `[]map[string]any`，仅含请求列 + id |

代码：[model_request.go `ManyToOne`](model_request.go)、
[field_relational.go](field_relational.go) 的 `TMany2OneField/TOne2ManyField/TMany2ManyField.OnRead`。
契约用例：[test/read_shape_contract_test.go](test/read_shape_contract_test.go)、
[m2m_read_shape_test.go](m2m_read_shape_test.go)。

要点：

- **x2many 是不是内嵌记录，由子规格决定，不由 `ClassicRead` 决定**。`ClassicRead` 只说明
  "这是给界面看的"，说明不了要 id 还是要整条记录，而两者的 payload 差几个数量级——一次经典
  列表读会把每行每个 x2many 的对端整条塞进结果。Odoo 的 `read()` 对 x2many 一律回 ids，
  正是这个理由。（m2m 从前看的是 `ClassicRead`，于是同一次经典读里 o2m 回 id 列表、m2m 回
  整条记录，同一类字段两种形状。）
- **空 many2one 是 `false`，不是 `0`/`""`/`-1`**。`nil` 与"这个字段没被读"在 JSON 里
  不可区分；`false` 是 Odoo 的既有约定，domain 侧也已按它对齐（`('x','=',False)` 落
  IS NULL，见 [expr_falsy_types_test.go](expr_falsy_types_test.go)）。
- **点了名的 x2many 一定出键，且是真值**。plain 读里 `Select("id","lines")` 回来的记录
  必然带 `lines`，空就是真空。不给空数组顶替"没读"——`[]` 是"没有关联行"这个具体断言，
  拿它冒充"没读"就是造一份看着正常的错数据。没点名则维持不出键。
- **列范围受 `Fields` 限定**：未请求的列不会出现。
- **连接键自动补齐**：子读取始终带上 comodel 的 `id`、o2m 的反向 FK（`ensureFields`），
  否则无法按键分组回填到父记录。m2m 的对端没有这样一列，所以它的内嵌记录只多一个 `id`。
- **`BigNumberToString` 对所有取值路径一致**：格式化在取数当场就地套用
  （[session_query.go](session_query.go)），不是挂在数据集上等 `AsMap()` 才生效。
  `GetByField` / `GetByIndex` / `AsMap` / `AsJson` / `AsStruct` / `GroupBy` 的分组键
  给出同一个值。
- **递归**：`ManyToOne`/`OneToMany` 在构造子读取会话时设置 `session.subReads = ctx.SubFields`，
  从而 o2m 行内的 m2o 列也能再内嵌（多层）。

### 测试

[test/field_nested_read_test.go](test/field_nested_read_test.go) `TestNestedReadSubFields`：
建立 `nr_order --(m2o partner_id)--> nr_partner`、`nr_order <--(o2m lines)-- nr_line`，
单次 `Read` 带 `SubFields{partner_id:{Fields:[name,color]}, lines:{Fields:[name,qty]}}`，
断言 `partner_id` 内嵌为 `{name,color,id}` map、`lines` 内嵌为子记录列表，且未请求列被排除。

```bash
cd orm/test && go test -run TestNestedReadSubFields -v
```

---

## ② 前端 BasicModel（[vertex～/src/views/model.js]，已实现）

`model.js` 是 Odoo BasicModel 的移植。原先 `_fetchRecord` 先平铺 `read`，再用
`_fetchX2Manys`/`_fetchNameGet` **逐字段另发 RPC**——正是要消除的"独立调用 API"。改动：

1. **`_relatedFieldNames(element, viewType, name)`**：算出某关系字段需要的子字段名
   （m2o → `display_name`；o2m/m2m → 内嵌视图可见列；恒带 `id`）。
2. **`_buildNestedReadSpec(element, viewType)`**：由 `fieldsInfo` 构建"字段树"——标量
   字段为叶子 `{}`，关系字段为 `{fields:[...]}`。
3. **`_fetchRecord`**：`read` 的第二个实参改为该字段树（而非平铺字段名）。
4. **`_parseServerData`（m2o 分支）**：兼容两种形态——经典二元组 `[id, display_name]`
   与内嵌子记录对象 `{id, display_name, ...}`；后者保留全部列供组件取用。
5. **`_fetchX2Manys`**：当 `record.data[field]` 是子记录对象列表（内嵌形态）时，直接用内嵌
   数据预热 `list._cache`，使随后的 `_readUngroupedList → _readMissingFields` 判定无缺失
   字段而**不再发 read**。组件从 `record.data` 的行 datapoint 直接渲染。

### 各 widget 的 relatedFields 约定

桥接到 `_relatedFieldNames` 的来源（视图 arch 解析后的 `fieldsInfo[field]`）：

| widget | 需要的子字段 |
| --- | --- |
| many2one | `display_name`（+ 视图/options 声明的额外列） |
| many2many_tags | `display_name`、`color` |
| one2many / many2many（内嵌 list） | 内嵌 list 视图的全部可见列 |

> ⚠️ 本仓库无法构建/运行该 SPA，故前端改动需在真实环境验证。校验语法：
> `node --check`（以 ESM 模式）已通过。

---

## ③ HTTP 桥接层契约（不在本仓库，待接）

前端 `read` 的第二个实参现在是**字段树**，桥接层（`/dataset/call_kw/<model>/read`）需把它映射成
`orm.ReadRequest`：叶子 `{}` → 普通 `Fields`；带 `fields` 的子树 → `SubFields`（可递归）。

```go
// args[1] 形如:
//   {"name":{}, "partner_id":{"fields":["id","display_name"]},
//    "tag_ids":{"fields":["id","display_name","color"]},
//    "line_ids":{"fields":["id","product","qty","price"]}}
func buildReadRequest(model string, ids []any, spec map[string]any) *orm.ReadRequest {
	req := &orm.ReadRequest{Model: model, Ids: ids}
	for name, raw := range spec {
		node, _ := raw.(map[string]any)
		req.Fields = append(req.Fields, name)
		if sub, ok := node["fields"]; ok { // 关系字段子规格
			if req.SubFields == nil {
				req.SubFields = map[string]*orm.ReadRequest{}
			}
			req.SubFields[name] = subSpecFromFields(sub) // 递归: 列表名→Fields, 嵌套对象→SubFields
		}
	}
	return req
}
```

要点：`SubFields[name].Fields` 即该关系字段请求的列；如需更深层，`fields` 元素也可是
`{name:{fields:[...]}}` 形式，递归填入 `SubFields[name].SubFields`。`Domain` 可选透传为字符串。

---

## ④ 仍待完成：把 form view 挂到 BasicModel（需 SPA 运行时验证）

当前 [vertex～/src/views/view-form/view-form.js] 用的是**简单 datasource**（`show()` 里直接
`datasource.read(0)` + `onDataChanged` 仅取 `[id,name]`），并未接入 BasicModel。要让 form
真正走上面 ②，还需：

1. **arch → fieldsInfo**：解析 form 视图 arch，为每个字段生成 `fieldsInfo[viewType][name]`，
   关系字段带上内嵌视图（o2m/m2m 的 list、tags 的 relatedFields）。
2. **widget 注册表**：每个 widget 声明 `relatedFields`（见 ② 的约定表），供 `_relatedFieldNames`
   读取。
3. **载入点**：`view-form.js` 的 `show()` 改为 `new BasicModel(...).load({modelName, res_id,
   fieldsInfo, viewType})`，拿到 record 句柄后 `model.get(handle)`，把 `record.data` 分发给
   各 input（替换现有 `onDataChanged` 的 `[id,name]` 逻辑）。
4. **组件渲染**：m2o/m2m_tags/o2m 组件从 `record.data[field]`（已内嵌）渲染初始值，仅在用户
   交互（如下拉搜索 `name_search`）时才发请求。

完成后端到端验证：打开一个含 m2o/m2m_tags/内嵌 o2m list 的 form，确认初始渲染过程中**只有
一次** `read`（无 per-widget 的 `name_get`/`read`），且各组件显示出 `display_name`/`color`/
list 可见列。
