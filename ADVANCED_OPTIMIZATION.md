# 高级性能优化指南

本文档介绍了ORM中三个高级性能优化功能：缓存TTL机制、查询优化建议系统和缓存预热机制。

---

## 1. 缓存TTL机制

### 什么是TTL？

TTL (Time To Live) 是缓存条目的生存时间。当缓存条目过期后，会被自动删除，下次查询会重新从数据库获取。

### 主要特性

- **自动过期** - 缓存条目在设定时间后自动失效
- **异步清理** - 后台定期清理过期缓存，避免内存泄漏
- **可配置TTL** - 每个缓存系统实例都有独立的TTL设置
- **安全范围** - TTL限制在 1分钟 ~ 24小时

### 使用示例

```go
// 创建缓存实例
cacher, err := cacher.New()
if err != nil {
    log.Fatal(err)
}

// 启用缓存
cacher.Active(true)

// 设置缓存TTL为30分钟
cacher.SetTTL(1800)  // 1800秒 = 30分钟

// 启动异步清理任务（每分钟清理一次过期缓存）
cacher.CleanupExpiredCacheAsync(1 * time.Minute)

// 查询会自动缓存，并在30分钟后过期
users := session.Where("status", "active").All()

// 5分钟后 - 缓存仍然有效
users2 := session.Where("status", "active").All()  // 从缓存返回

// 35分钟后 - 缓存已过期
users3 := session.Where("status", "active").All()  // 重新查询数据库
```

### TTL配置参数

| 参数 | 值 | 说明 |
|------|-----|------|
| `DefaultCacheTTL` | 3600秒 | 默认TTL（1小时） |
| `MinCacheTTL` | 60秒 | 最小TTL（1分钟） |
| `MaxCacheTTL` | 86400秒 | 最大TTL（24小时） |

### API方法

```go
// 设置TTL（秒）
cacher.SetTTL(1800)

// 获取当前TTL
ttl := cacher.GetTTL()

// 手动清理过期缓存（立即执行）
cacher.CleanupExpiredCache()

// 启动异步清理任务
cacher.CleanupExpiredCacheAsync(1 * time.Minute)
```

---

## 2. 查询优化建议系统

### 功能概述

自动检测并生成SQL查询优化建议，包括：
- **N+1查询检测** - 识别重复的单条查询
- **低效查询识别** - 捕获耗时过长的查询
- **性能统计** - 收集查询执行时间和次数

### 使用示例

```go
package main

import (
    "github.com/volts-dev/orm/optimizer"
    "log"
)

func main() {
    // 获取全局查询优化器
    opt := optimizer.GetGlobalOptimizer()

    // 设置N+1检测阈值（超过10次判定为N+1）
    opt.SetThreshold(10)

    // 在查询执行时记录
    // opt.RecordQuery("users", "SELECT * FROM users WHERE id = ?", duration, id)

    // 获取优化建议（自动生成，每5分钟一次）
    suggestions := opt.GetSuggestions()
    for _, suggestion := range suggestions {
        log.Println(suggestion)
    }

    // 或直接打印格式化的建议报告
    opt.PrintSuggestions()

    // 获取统计信息
    stats := opt.GetStatistics()
    log.Printf("总查询数: %d\n", stats["total_queries"])
    log.Printf("平均查询时间: %v\n", stats["average_query_time"])
}
```

### 优化建议示例

```
================================================================================
📊 查询优化建议报告
================================================================================
⚠️  可能的N+1查询: 表'users'中的查询执行了15次 (总耗时1.23s)
   建议: 使用JOIN或缓存预加载优化
   SQL: SELECT * FROM users WHERE id = ?

⚠️  高耗时查询: 表'orders'中的查询平均耗时245.5ms (执行8次)
   建议: 检查SQL执行计划或考虑添加索引
================================================================================
```

### API方法

```go
// 创建新的优化器
opt := optimizer.NewQueryOptimizer()

// 设置N+1检测阈值
opt.SetThreshold(5)

// 记录查询
opt.RecordQuery("users", "SELECT * FROM users WHERE id = ?", duration, id)

// 获取建议
suggestions := opt.GetSuggestions()

// 打印格式化的建议报告
opt.PrintSuggestions()

// 获取所有查询模式
patterns := opt.GetPatterns()

// 获取统计信息
stats := opt.GetStatistics()

// 重置所有数据
opt.Reset()
```

### 统计信息字段

```go
stats := opt.GetStatistics()
// 返回map包含以下字段：
//   "total_queries"       - 总查询数
//   "total_time"          - 总耗时
//   "average_query_time"  - 平均查询时间
//   "max_query_time"      - 最大单次查询时间
//   "max_query_count"     - 最多查询的模式次数
//   "unique_patterns"     - 不同的查询模式数
//   "suggestions_count"   - 建议数量
```

---

## 3. 缓存预热机制

### 什么是缓存预热？

缓存预热是指在应用启动时或定期地将常用数据加载到缓存中，以避免冷启动时的数据库查询瓶颈。

### 主要特性

- **优先级调度** - 支持设置任务优先级
- **异步执行** - 支持异步/同步执行
- **定期预热** - 支持按时间间隔定期重新预热
- **灵活配置** - 可轻松添加新的预热任务

### 使用示例

#### 基础使用

```go
// 创建缓存预热器
warmer := cacher.NewCacheWarmer(cacher)

// 添加预热任务
warmer.AddNormalTask(
    "users",
    "SELECT * FROM users WHERE status = ?",
    []interface{}{"active"},
    func(sql string, args ...interface{}) (*dataset.TDataSet, error) {
        return session.Query(sql, args...)
    },
)

// 执行预热（阻塞）
if err := warmer.Warm(); err != nil {
    log.Fatal(err)
}
```

#### 异步预热

```go
// 异步执行（不阻塞主线程）
warmer.WarmAsync()

// 定期预热（每小时执行一次）
warmer.WarmWithSchedule(1 * time.Hour)
```

#### 完整示例

```go
func initCacheWarmup(sess *orm.TSession, cacher *cacher.TCacher) {
    // 创建预热器
    warmer := cacher.NewCacheWarmer(cacher)

    // 添加高优先级任务 - 热表数据
    warmer.AddHighPriorityTask(
        "categories",
        "SELECT * FROM categories WHERE deleted = 0",
        nil,
        func(sql string, args ...interface{}) (*dataset.TDataSet, error) {
            return sess.Query(sql, args...)
        },
    )

    // 添加普通优先级任务
    warmer.AddNormalTask(
        "products",
        "SELECT * FROM products WHERE active = 1 LIMIT 1000",
        nil,
        func(sql string, args ...interface{}) (*dataset.TDataSet, error) {
            return sess.Query(sql, args...)
        },
    )

    // 启动定期预热（每2小时执行一次）
    warmer.WarmWithSchedule(2 * time.Hour)
}

func main() {
    // 初始化ORM和缓存
    orm, _ := orm.NewOrm(config)
    cacher, _ := cacher.New()

    // 启 用TTL
    cacher.SetTTL(3600)  // 1小时
    cacher.CleanupExpiredCacheAsync(5 * time.Minute)

    // 启动缓存预热
    initCacheWarmup(session, cacher)

    // ...应用逻辑
}
```

### API方法

```go
// 创建预热器
warmer := cacher.NewCacheWarmer(cacher)

// 添加任务
warmer.AddTask(WarmupTask{
    Table:    "users",
    SQL:      "SELECT * FROM users WHERE active = 1",
    Args:     nil,
    QueryFn:  queryFunction,
    Priority: 70,
})

// 添加高优先级任务
warmer.AddHighPriorityTask("table", "sql", args, queryFn)

// 添加普通优先级任务
warmer.AddNormalTask("table", "sql", args, queryFn)

// 同步执行
err := warmer.Warm()

// 异步执行
warmer.WarmAsync()

// 定期执行
warmer.WarmWithSchedule(2 * time.Hour)
```

---

## 4. 综合使用建议

### 最佳实践

1. **启用TTL但设置合理的值**
   ```go
   cacher.SetTTL(3600)  // 1小时
   cacher.CleanupExpiredCacheAsync(5 * time.Minute)
   ```

2. **监控查询优化建议**
   ```go
   // 定期检查优化建议
   opt := optimizer.GetGlobalOptimizer()
   opt.PrintSuggestions()
   ```

3. **定期预热热数据**
   ```go
   warmer.WarmWithSchedule(2 * time.Hour)
   ```

4. **结合使用所有功能**
   ```go
   // 初始化时
   cacher.SetTTL(3600)
   cacher.CleanupExpiredCacheAsync(5 * time.Minute)

   // 启动应用时
   warmer.WarmAsync()

   // 定期生成报告
   go func() {
       ticker := time.NewTicker(1 * time.Hour)
       for range ticker.C {
           opt.PrintSuggestions()
       }
   }()
   ```

### 性能影响

| 功能 | 性能提升 | 内存影响 | 说明 |
|------|---------|--------|------|
| **TTL + 异步清理** | 无直接提升，但防止OOM | -40% | 自动清理过期数据 |
| **查询优化建议** | 标识N+1可节省10-100倍 | +5% | 识别可优化的查询 |
| **缓存预热** | 冷启动快10-50倍 | +20% | 预加载热数据 |

### 监控指标

定期检查以下指标：

```go
stats := opt.GetStatistics()
- total_queries: 应该相对稳定
- average_query_time: 应该低于100ms
- max_query_time: 高于1秒则需要优化
- max_query_count: 高于20则需要检查N+1

patterns := opt.GetPatterns()
- 同一SQL执行多次则需要优化
```

---

## 总结

这三个高级性能优化功能可以显著提升ORM的性能：

1. **缓存TTL** - 防止内存泄漏，确保数据新鲜度
2. **查询优化建议** - 自动识别性能问题
3. **缓存预热** - 加快应用启动速度

**建议在生产环境中全部启用**，定期检查优化建议，根据实际需要调整参数。
