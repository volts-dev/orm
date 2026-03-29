# N+1 查询优化指南

## 什么是 N+1 查询问题

N+1 查询问题是一个常见的性能问题，特别是在 ORM 中：

```go
// ❌ 问题：N+1 查询
orders := session.All()  // 1 次查询获取所有订单
for _, order := range orders {
    user := session.Where("id", order.UserId).One()  // N 次查询获取每个订单对应的用户
}
// 总共 N+1 次查询！
```

---

## 解决方案

### 方案 1: 使用 JOIN（推荐）

```go
// ✅ 优化：只需 1 次查询
orders := session.
    Join("INNER", "users", "users.id = orders.user_id").
    Field("orders.*", "users.name as user_name").
    All()
// 只需 1 次查询！
```

**优势**：
- 最高性能（单次数据库查询）
- 数据库可以优化执行计划
- 减少网络往返

**用途**：
- 一对一关联（Orders ↔ Users）
- 一对多关联的列表（Posts ↔ Categories）

---

### 方案 2: 使用缓存预加载

```go
// ✅ 优化：通过缓存批量加载
session.Cacher.Active(true)  // 启用缓存

orders := session.All()  // 查询所有订单

// 批量获取用户（利用缓存）
userIds := extractIds(orders, "UserId")
users := session.Model("users").Where("id", "IN", userIds).All()

// 建立关联
for _, order := range orders {
    order.User = findUser(users, order.UserId)
}
```

**优势**：
- 只需 2 次查询（1 次获取订单，1 次批量获取用户）
- 缓存会加速后续相同查询
- 适合复杂的关联关系

**用途**：
- 一对多关联
- 多对多关联
- 复杂的关联链

---

### 方案 3: 使用预加载（Preload）

```go
// ⏳ 计划中的功能
orders := session.
    Preload("User").       // 自动预加载关联User
    Preload("Items").      // 自动预加载Items
    All()
```

**状态**：该功能正在计划中

---

## 缓存策略优化

### 1. 启用缓存

```go
// 全局启用缓存
orm.Cacher.Active(true)

// 为特定表启用缓存
orm.Cacher.SetStatus(true, "users")
orm.Cacher.SetStatus(true, "orders")
```

### 2. 充分利用缓存

```go
// 首次查询：执行 SQL
users1 := session.Where("department", "sales").Field("id", "name").All()

// 后续相同查询：从缓存获取（无 SQL）
users2 := session.Where("department", "sales").Field("id", "name").All()
```

### 3. 缓存清理

```go
// 更新后清理缓存
user.Update()
session.Cacher.RemoveById("users", user.Id)

// 删除后清理缓存
session.Cacher.RemoveById("users", user.Id)

// 批量操作后清理缓存
session.Cacher.ClearByTable("users")
```

---

## 性能对比

| 方案 | 查询次数 | 性能 | 复杂度 | 用途 |
|------|---------|------|--------|------|
| N+1（问题） | N+1 | 最差 | 最低 | ❌ 应避免 |
| JOIN | 1 | 最好 | 低 | ✅ 简单关联 |
| 缓存预加载 | 2 | 很好 | 中 | ✅ 复杂关联 |
| Preload | 1-2 | 很好 | 中 | ⏳ 待实现 |

---

## 最佳实践

### 1. 优先使用 JOIN

```go
// ✅ 好
session.Join("LEFT", "departments", "departments.id = users.department_id").
    Where("users.status", "active").
    All()
```

### 2. 批量查询而非逐条查询

```go
// ❌ 不好
for _, id := range ids {
    user := getUser(id)  // 多次查询
}

// ✅ 好
users := session.Where("id", "IN", ids).All()  // 1 次查询
```

### 3. 启用缓存且定期清理

```go
// ✅ 好的实践
orm.Cacher.Active(true)
orm.Cacher.SetStatus(true, "hot_table")     // 为热表启用缓存

// 更新后清理缓存
record.Update()
orm.Cacher.RemoveById("hot_table", record.Id)
```

### 4. 使用字段过滤减少数据传输

```go
// ✅ 只查询需要的字段
session.Field("id", "name", "email").All()

// ❌ 不必要加载所有字段
session.All()
```

---

## 监控和调试

### 1. 启用 SQL 日志

```go
orm.Logger.SetShowSQL(true)
```

### 2. 检查是否出现 N+1 问题

如果日志显示重复的相似 SQL：
```sql
SELECT * FROM users WHERE id = 1;
SELECT * FROM users WHERE id = 2;
SELECT * FROM users WHERE id = 3;
```

这就是 N+1 问题的信号。应改用 JOIN 或缓存。

### 3. 检查缓存是否有效

```go
orm.Cacher.Active(true)
// 观察日志，第二次相同查询时应该看到 "Cache hit" 消息
```

---

## 总结

| 问题 | 原因 | 解决方案 | 性能 |
|------|------|--------|------|
| N+1 查询 | 逐条查询关联数据 | JOIN 或缓存预加载 | 提升 10-100 倍 |
| 缓存未被利用 | 缓存未启用或键不匹配 | 启用缓存，使用 Cacher.Active() | 提升 5-10 倍 |
| 缓存过期数据 | 缓存不清理 | 更新时调用 RemoveById/ClearByTable | 确保数据一致性 |

---

## 相关问题修复

本 ORM 最近修复的相关问题：
- ✅ 缓存无限增长（容量限制已添加）
- ✅ 缓存数据污染（警告和保护已添加）
- ✅ 缓存锁竞态（死锁风险已消除）
- ✅ 事务 context 处理（超时控制已修复）

详见 `FIXES_SUMMARY.md`
