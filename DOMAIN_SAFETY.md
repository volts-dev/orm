# 条件（domain）安全语义

本文记录 2026-09 这一轮针对 vectors 使用反馈做的几条**条件层**加固。每一条都对应
vectors 仓里反复出现的注释或工作坊代码，改完之后那些绕法可以删掉。

## 1. 条件解析失败是中断，不是降级

`Where()` / `Domain()` / `And()` / `Or()` 为了链式调用没有返回错误的位置。原来解析失败只
log 一句，条件整个不进树，语句变成"少一个条件的合法查询"——读回全表、按域写/删越界，
且一切静默。vectors 里至少七处注释记着这个坑（"domain 解析失败是降级不是中断"）。

现在三种坏形态都会被拒绝，错误可用 `errors.Is(err, ormerr.ErrInvalidDomain)` 判别：

| 形态 | 例子 | 原来 | 现在 |
|---|---|---|---|
| 参数类型不支持 | `Domain([]string{...})` / `Domain(map)` | log 后继续 | `ErrInvalidDomain` |
| 非空字符串解析成空树 | `Where("((")`、被截断的半截字符串 | 变成"无条件" | `ErrInvalidDomain` |
| 逻辑操作符元数不平 | `['|', leaf]`、`["&", leaf]` | log 后照跑（多出的 `|` 被吞） | `ErrInvalidDomain` |

错误记在 `TStatement` 上（`Statement.Err()`），由 `Read` / `Search` / `Count` / `Sum` /
`Write` / `Delete` / `ReadGroup` / `SoftDelete` 在执行前统一交回；执行后随 `Init()` 复位，
不会污染同一会话的下一条语句。

**不是错误的情况**：`Domain(nil)`、`Domain("")`、`Domain("[]")`、`Domain("()")` 及其配对
嵌套（`[ ( ) ]`）都是合法的空条件，无操作——请求体里 domain 缺省就是这些形状。

**仍然无法在 ORM 层拦住的**：`Where("state != some_var")`。字符串 DSL 里裸标识符就是字符串
字面量，`some_var` 会被当成 `'some_var'`。这是 DSL 的语义而不是 bug，需要变量求值的
调用方（记录规则、报表）应在自己那层编译成对象树再交给 ORM（vectors core/recordrule 即是）。

## 2. `x IN ()` 恒假，不是"没有这个条件"

vectors 权限界面为此专门造过一条 `["id","in",["0"]]` 的假条件（api_user_admin.go
`idListDomain`、platform_user_ops.go `TenantIdsOfUser`）。现在：

| 写法 | 语义 |
|---|---|
| `.Ids()` / `.Ids(empty...)` / `.Ids(nil, nil)` | 这零条记录：读回空集；`Write` 报 `ErrNotFound`；`Delete` 影响 0 行 |
| `.In("id")` / `domain.IN("id")` / `domain.New("id","in")` | 合法叶子 `(id, 'IN', <空>)`，渲染为 `FALSE` |
| `[('id','in',[])]`（字符串或 `[]any`） | 同上，`FALSE` |
| `.NotIn("id")` / `[('id','not in',[])]` | 恒真 = AND 的单位元，**无操作**；有意不落 TRUE 叶子，以免让 `hasCondition()` 误判"有条件"而绕开 `Write`/`Delete` 的 `ErrUnsafe` 守卫 |

与 Odoo `browse([])` / `search([('id','in',[])])` 一致。注意 `.Ids(a...).Ids(b...)` 里若 `a`
为空，结果恒空——那正是"这零条 AND 这几条"的含义。

## 3. 按条件写、一行没匹配到：带类型的 `ErrNotFound`

`Where(...).Write(...)` 一行都没命中时原来返回一条裸 `fmt.Errorf`。vectors 侧只能按原文
字符串匹配（invite_flow_test / api_user_actions）。现在是
`ormerr.New(ormerr.ErrNotFound, <原文>)`：`errors.Is(err, ormerr.ErrNotFound)` 可判别，
`err.Error()` 仍包含 `Not records found`，存量字符串匹配不受影响。

按 id 写（`Ids(...).Write`）在目标行全部不可见时仍是 `(0, nil)`——那是可见范围语义
（见 `scopeIdsByDomain`），与本条不同。

## 4. `TDomainNode` 可以过 JSON

默认编码只输出导出的 `Value` 字段，一棵条件树过一次 JSON 只剩 `{"Value":null}`。vectors
跨进程读记录因此只敢传字符串 domain。现在 `MarshalJSON` 输出 Odoo 列表形态
（叶子 `["field","op",value]`，列表 `[...]`），`UnmarshalJSON` 解回；数字经 `json.Number`
还原为 `int64`，19 位雪花 id 不会被 `float64` 削掉末位。接收方即便还是老代码，按 `[]any`
走 `Any2Domain` 也能吃下同一份 JSON。

## 5. 字符串条件有解析缓存

vectors 的 `BeforeSession` 钩子给**每条**语句追加一次 `Where("tenant_id=?", id)`——条件
字符串恒定、值走参数。`String2Domain` 一次约 9.8µs / 42 allocs，全是重复劳动。现在
`TStatement.Op()` 对 ≤256 字节的字符串条件按原文缓存解析结果（上限 2048 条，满了就不再
收新条目），命中后 `Clone()` 一棵再交给下游——下游（normalize_leaf 等）会就地改写节点，
缓存里的那棵永远不能直接外流。带字面量值的条件（`name='foo'`）同样按原文缓存，上限
保证不会无界增长。

## 6. 会话级 Set（`SetMustFieldValue`）

- `Statement.Init()` 重挂会话级 Queryable Set 时直建叶子，不再走字符串解析。
- `SetMustFieldValue` 也**立刻**施加到当前语句。原来它只在下一次 `Init()` 生效：
  `NewSession()` 之后、第一条语句之前调用，那条语句什么都没带上（读回全表、Create
  不盖戳），从第二条起才对——"配了但第一次没生效"。
