# 贡献指南

感谢参与 `volts-dev/orm` 的开发。本文档说明本地开发、测试、benchmark、lint 的操作规范。

---

## 1. 本地开发环境

### Go 版本
- 最低 **Go 1.25.0**（见 `go.mod`）

### 多仓协作（可选）
若同时改 `volts-dev/orm` 和它的调用方（如 `vs/modules/*`），用 Go workspace：

```bash
# 在共同父目录下
go work init ./orm ./modules/product ./modules/website ./modules/storage ./modules/stock
```

`go.work` **不要提交到任何仓库**（已在各仓 `.gitignore` 处理；本仓也已忽略）。

### 启动测试用 MySQL + PostgreSQL

```bash
docker compose -f test/docker-compose.test.yml up -d
source test/docker-compose.test.env
```

SQLite 走文件模式，不需要容器。

---

## 2. 跑测试

### 主包单元测试（无 DB 依赖）

```bash
go test -race -count=1 .
```

### 集成测试（依赖 DB）

```bash
# 确保 docker 已起 + env 已 source
rm -f test/test.db test/test.db-shm test/test.db-wal
go test -race -count=1 ./test/...
```

### 跑全部测试

```bash
docker compose -f test/docker-compose.test.yml up -d
source test/docker-compose.test.env
rm -f test/test.db test/test.db-shm test/test.db-wal
go test -race -count=1 ./...
```

### 覆盖率检查

```bash
go test -count=1 -coverprofile=coverage.out ./...
go tool cover -func=coverage.out > /tmp/coverage-now.txt
diff test/coverage-baseline.txt /tmp/coverage-now.txt
```

---

## 3. 跑 benchmark + 对比基线

```bash
# 跑 benchmark（5 次取均值，降低噪音）
go test -bench=. -benchmem -count=5 ./... > /tmp/baseline-now.txt

# 对比基线（需要安装 benchstat：go install golang.org/x/perf/cmd/benchstat@latest）
benchstat test/baseline.txt /tmp/baseline-now.txt
```

输出说明：`delta` 列正值表示**变慢**，负值表示**变快**；`p` 列表示统计显著性（`p < 0.05` 才有说服力）。

---

## 4. 跑 lint

```bash
# 安装 golangci-lint：https://golangci-lint.run/usage/install/
golangci-lint run ./...
```

何时用 `//nolint:xxx`：
- 测试代码里的 false positive（如 unused test helper）：加 `//nolint:unused // 描述原因`
- 框架/特殊场景必须违反 lint（如有意 unused field 占位）：加 `//nolint:xxx // 描述原因`
- 不要用 `//nolint`（不指定 linter 名）—— 会屏蔽未来新增的检查

---

## 5. 基线调整流程

`test/baseline.txt`（benchmark 基线）和 `test/coverage-baseline.txt`（覆盖率基线）是 Phase 完成时的快照。**默认情况下，PR 不应修改基线文件**。

允许更新基线的情况：
1. **完成一个 Phase**：Phase 内的所有 PR 合并后，最后一个 PR 重新生成基线
2. **新设计本身改变了行为/路径**（如 Phase 1 加 ctx 全链路带来轻微开销）：PR 描述里**论证**为什么基线必须重设，包括：
   - 性能回退的具体原因（如「ctx 链路增加 X ns/op，是必要功能开销」）
   - 是否在合理范围（默认阈值：< 5% 不需论证；5-15% 需论证；> 15% 需审批）
   - 覆盖率变化的具体原因

**禁止**：为了让 CI 过、为了让基线对比看起来好看，注释测试 / 砍 benchmark / 调阈值。

---

## 6. PR 标题约定

参考 `git log` 现有风格：

```
<type>(<scope>): <subject>
```

`type`：`feat` / `fix` / `refactor` / `docs` / `test` / `chore` / `style`
`scope`：包名或主题（如 `orm`、`postgres`、`session`、`ci`）
`subject`：祈使句，小写起头，不加句号

示例：
```
feat(postgres): dynamically resolve active session schema in postgres metadata DDL queries
refactor(orm): rename TField private fields to Go conventions
chore(ci): add GitHub Actions workflow for test + vet + coverage
```

PR 描述里要包含：
- 改动概述
- 测试情况（手动测了什么、CI 是否绿）
- 如果改了基线：基线调整论证（见 §5）
- 关联 issue（如有）
