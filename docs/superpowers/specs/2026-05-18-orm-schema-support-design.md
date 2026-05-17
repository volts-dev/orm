# volts-dev/orm Schema 支持设计规格书

**日期**：2026-05-18
**状态**：草案，等待用户审核通过
**范围**：在 `volts-dev/orm` 中，以 Session 级别的动态多 Schema 隔离设计方案提供多数据库 Schema 的支持（以 PostgreSQL 为核心，兼容 MySQL 和 SQLite）。

## 目标

实现数据库级别和会话级别的 `Schema` 支持，使开发者能够轻松执行 Schema 级别的多租户数据隔离。

使用例：
```go
// 1. DDL 同步支持指定 Schema
session.Schema("tenant_a").SyncModel(...) 

// 2. DML 数据查询支持指定 Schema
session.Schema("tenant_a").Find(&users) // 生成 SQL: SELECT * FROM "tenant_a"."sys_user"
```

## 架构方案

### 1. 会话级数据传递设计

在 `TSession` 上持有 `Schema` 信息，并通过链式 API 提供设置手段。

在 [/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/session.go](/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/session.go) 中：

```go
type TSession struct {
    // ...
    Schema string
}

// 链式调用设置 Schema 空间
func (self *TSession) SetSchema(schema string) *TSession {
    self.Schema = schema
    return self
}
```

在 `TOrm` 上存储全局默认配置并自动传递给 `TSession`：
在 [/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/orm.go](/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/orm.go) 中：
```go
type TOrm struct {
    // ...
    Schema string
}

func (self *TOrm) NewSession() *TSession {
    session := &TSession{
        // ...
        Schema: self.Schema, // 继承默认 Schema 空间
    }
    return session
}
```

### 2. DML 表名拼装支持 —— Statement 与 QuoteTable

为 `TStatement` 添加获取当前表名的助手函数，该函数会结合当前 Session 的 Schema 和 dialect 引号规范。

在 [/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/statement.go](/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/statement.go) 中，`TStatement` 的表名解析：

```go
// 返回符合当前 Schema 空间并包裹好引号的表名，如 "tenant_a"."sys_user"
func (self *TStatement) QuoteTable() string {
    tableName := fmtTableName(self.Model.String())
    schema := self.session.Schema
    return self.session.orm.dialect.Quoter().QuoteTable(schema, tableName)
}
```

这要求我们重构 `/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/dialect/` 包中的 `Quoter` 接口和实现。

在 [/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/dialect/quoter.go](/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/dialect/quoter.go) 中：
```go
type Quoter interface {
    Quote(string) string
    QuoteTo(io.Writer, string)
    Join([]string, string) string
    // 新增：生成带 Schema 的全表名包裹
    QuoteTable(schema, table string) string
}
```

各方言实现 `QuoteTable`：
- **Postgres / MySQL**：返回 `"schema"."table"` / `` `schema`.`table` ``。
- **SQLite**：直接忽略 `schema`，返回 `"table"`。

修改 `session_crwd.go`、`session_query.go` 等 DML 操作中直接使用 `self.Statement.Model.Table()` 来组装 SQL 的部分，统一改用 `self.Statement.QuoteTable()`。

### 3. DDL 元数据同步与 DSN 连接串支持

#### DSN 解析恢复：
在 [/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/driver_postgres.go](/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/driver_postgres.go) 中恢复被注释的 DSN 默认 Schema 解析：
```go
db.Schema = o.Get("schema")
if len(db.Schema) == 0 {
    db.Schema = "public"
}
```

#### DDL 物理迁移元数据检测修正：
在 [/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/dialect_postgres.go](/Users/shadow/SectionZero/MyProject/Go/src/volts-dev/orm/dialect_postgres.go) 中：
1. 重构 `getSchema` 为 `getSchema(session *TSession)` 形式，优先从 `session` 动态获取：
```go
func (db *postgres) getSchema(session *TSession) string {
    if session != nil && session.Schema != "" {
        return session.Schema
    }
    if db.Schema != "" {
        return db.Schema
    }
    return DefaultPostgresSchema
}
```
2. 修改 `GetFields`、`GetIndexes`、`IsTableExist` 等检查物理库结构的元数据语句，传入当前 Session 运行所在的 Schema。如：
```go
func (db *postgres) GetFields(ctx context.Context, session *TSession, tableName string) ([]string, map[string]IField, error) {
    args := []interface{}{tableName, db.getSchema(session)}
    // ...
}
```
这样物理库检查就不会因为只看 `public` 而报错在特定 schema 空间表重复创建。

## 详细设计要点

- **兼容性**：不改变现有无 Schema 指定时的运行表现，默认仍走 `public`。
- **安全性**：不改变原有底层 Go 的 `sql.DB` 连接池的物理连接隔离，使用纯动态 DML 表名拼接方式，避免 `SET search_path` 污染连接池导致的多租户数据越权灾难。
- **元数据同步完美衔接**：通过 DDL 元数据查询与 `session.Schema` 的贯通，完美防止已存在的表因检测范围限制而不断在 `_alterTable` 中重复运行建表引发的死锁/报错。

---

如果这个设计方案完全符合你的期望，请告诉我，我将准备向 git 提交规格书并编写具体的 implementation plan！
