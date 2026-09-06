# 部分索引、表达式索引与临时模型

2026-09 按 vectors 的三条缺失能力反馈补齐：`sys_module_job` 靠"锁内 check-then-write"
代替 partial unique；`phone_validation` 的号码搜索因为"本仓 ORM 没有表达式索引接口"落不到
索引上；base_import / 修改口令向导等四五处因为"本仓 orm 没有 transient 机制"只能手工清或者
干脆留着。

## 1. 声明方式

索引通过 `ModelBuilder`（通常写在模型的 `OnBuildFields` 里）：

```go
func (self *SysModuleJob) OnBuildFields() error {
    b := self.Builder()
    b.SetPartialUniqueIndex("state = 'pending'", "tenant_id", "job_key") // 只在 pending 行之间唯一
    b.SetUniqueExprIndex("lower(email)")                                 // 大小写不敏感的唯一
    b.SetExprIndex("regexp_replace(number, '[^0-9]', '', 'g')")          // 表达式索引
    b.SetPartialIndex("active", "name")                                  // 只给活动行建索引
    b.SetIndexSpec(IndexSpec{Unique: true, Cols: []string{"a"}, Exprs: []string{"lower(b)"}, Where: "c > 0"})
    return b.Err()
}
```

- `Exprs` / `Where` 是模型作者写的 SQL 片段，与字段名同一信任级别，原样进 DDL；只做最基本的
  拒绝（`;`、注释、不配对的括号/引号）。`Cols` 必须是模型上存在的字段。
- **声明错误不会被静默跳过**：Builder 方法为链式调用不返回错误，但会记到模型对象上，
  `RegisterModel` / `SyncModel` 取出后报错。`b.Err()` 可提前取。`SetIndex` 对不存在的字段
  原来是空指针崩溃，现在同样报声明错误。

临时模型用 table 标签或 builder：

```go
type ChangePasswordWizard struct {
    orm.TModel `table:"name('change.password.wizard') transient(12)"` // 保留 12 小时
    CreateTime time.Time `field:"datetime() created"`                  // 判旧依据，必须有
}
// 或
func (self *X) OnBuildFields() error { return self.Builder().TableTransient(0.5).Err() }
```

不带参数的 `transient` 取 `DefaultTransientMaxHours`（1 小时，与 Odoo 同值）；非正数或非数字
直接报错，不静默落回默认。

## 2. 命名与同步

带表达式/谓词的索引叫**定义性索引**：名字 = 常规前缀名 + `_p<8 位定义哈希>`，例如
`UQE_pjob_tijkey_p4a5d4828`。定义（列、表达式、谓词，空白与大小写规整后）一变，哈希就变。

SyncModel 对账时**同名即同定义**——不去比 PG 规整后的文本（`lower((name)::text)`、
`(state = 'pending'::text)`），那种比法每次启动都会 DROP 再 CREATE。改了声明时：库里的
旧版本（同逻辑名、不同哈希、来自反查）被识别为过期定义并 DROP，新定义 CREATE。第二次同步
什么都不做。

自定义名（`IndexSpec.Name`）同样追加哈希：否则改了谓词名字不变，库里的旧定义永远不会被重建。
名字超过 63 字节时截断主体、保留哈希。

## 3. 各方言的表达能力

| 能力 | PostgreSQL | SQLite | MySQL ≥ 8.0.13 | MySQL < 8.0.13 / MariaDB / TiDB |
|---|---|---|---|---|
| 部分索引（非唯一） | 原生 `WHERE` | 原生 `WHERE` | **退化为全表索引**（结果集一致，只多占空间；告警一次） | 同左 |
| 部分唯一索引 | 原生 | 原生 | functional key parts 模拟：每个键包 `CASE WHEN (谓词) THEN 键 ELSE NULL END`，不满足谓词的行全键 NULL、不参与唯一冲突 | **拒绝**（`ErrIndexUnsupported`） |
| 表达式索引 | 原生 | 原生（≥3.9） | functional key parts `((expr))` | **拒绝**（`ErrIndexUnsupported`） |
| 反查识别表达式/谓词 | 解析 `pg_indexes.indexdef` | `PRAGMA index_list.partial` + `sqlite_master.sql` | `STATISTICS.EXPRESSION` | 只有列 |

为什么拒绝而不是退化：部分唯一索引退化成全表唯一会把合法行拒掉，直接丢掉会放进重复行，
两个方向都是错的；表达式索引在没有 functional key parts 的版本上需要一个带类型的生成列，
类型从任意表达式推不出来。MySQL 5.7 已 EOL（2023-10），MariaDB 至今没有 functional key parts。
版本经 `SELECT @@VERSION` 惰性探测一次并缓存；探测失败按不支持处理。

## 测试覆盖

- **PostgreSQL 13：真库端到端**（index_pg_it_test.go）——约束真的生效、`pg_indexes.indexdef`
  反查出的谓词/表达式被认成同一条声明（幂等不重建）、改谓词后旧索引 DROP 新索引 CREATE。
  连接 `postgres/postgres@localhost:5432/test_orm`，连不上自动 Skip。
- **SQLite：真库端到端**（index_partial_sqlite_test.go）——同上一套，`:memory:`/临时文件。
- **MySQL：SQL 生成 + 版本矩阵单测**（index_spec_test.go）确定性覆盖两个版本分支的 DDL；
  **真库端到端**（index_mysql_it_test.go）走仓库既有的环境变量约定，默认 Skip：

  ```sh
  MYSQL_TEST_HOST=127.0.0.1 MYSQL_TEST_USER=root MYSQL_TEST_PASS=xxx \
    go test -run 'MySQLIndex_' .
  ```

`IDialect` 新增 `ValidateIndex(*TIndex) error`：建表、加索引、SyncModel 三条路径都先校验再
生成 DDL，拒绝的是"这条索引"而不是整张表。

## 4. 临时模型的清理

```go
n, err := orm.Model("change.password.wizard").VacuumTransient()          // 清本模型；now 可传
report, err := orm.VacuumTransients(ctx)                                  // 清全部临时模型
```

- 条件是 `created < now - TransientMaxHours`，走**普通 Delete 路径**：BeforeSession 钩子
  （多租户过滤）、ondelete 级联、m2m 关联表清理全部照常；会话上已有的 Where/Domain 与之 AND 叠加。
- 模型未声明 transient → `ErrNotTransient`；没有 `created` 标签字段 → `ErrNoCreatedField`
  —— **绝不会**因为判不出年龄而清空整表。
- `VacuumTransients` 一个模型失败不影响其余，错误经 `errors.Join` 汇总，`errors.Is` 可判别。
- orm 不含调度器：什么时候跑由应用层的定时任务决定（vectors 有自己的 cron 基础设施）。

## 5. 顺带修的既有问题

- SQLite 反查表达式索引时 `PRAGMA index_info` 的 name 是 NULL，原来扫进 `string` 直接报错——
  库里只要有一个表达式索引（哪怕是 DBA 手建的），整张表的内省就挂。
- PG 的 `indexdef` 原来按第一个 `(` 切分列名，遇到表达式或 `WHERE` 会切出碎片。
- `ModelBuilder.SetIndex` 对不存在的字段名是空指针崩溃。
