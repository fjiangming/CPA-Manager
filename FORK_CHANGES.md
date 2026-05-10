# Fork 差异说明 — 冲突解决指南

> 本文件记录 `fjiangming/CPA-Manager` 相对上游 `seakee/CPA-Manager` 的所有代码改动。
> 当合并上游更新产生冲突时，按本文件的指引决定保留哪些改动。

## 功能概述

本 fork 新增了 **Codex 定时巡检** 功能：
- 后台自动按计划执行 Codex 账号巡检（复用前端的探测决策逻辑）
- 巡检结果持久化到 SQLite，保留最近 100 条历史
- 前端新增定时巡检管理页面（策略配置 + 历史 + 详情）
- 支持自动执行建议操作或手动审查后执行

---

## 新增文件（无冲突风险）

这些文件是本 fork 独有的，上游不存在对应文件，合并时**不会冲突**：

### 后端 Go

| 文件 | 说明 |
|------|------|
| `usage-service/internal/inspection/types.go` | 巡检类型定义（Schedule, HistoryRecord, AccountResult） |
| `usage-service/internal/inspection/inspector.go` | 巡检执行器（API 探测 + 决策逻辑，从前端 TS 移植） |
| `usage-service/internal/inspection/scheduler.go` | 定时调度器（time.Timer 循环，自动恢复配置） |
| `usage-service/internal/httpapi/inspection_handler.go` | HTTP API 处理器（策略 CRUD、历史查询、手动触发） |

### 前端 React

| 文件 | 说明 |
|------|------|
| `src/services/api/scheduledInspection.ts` | API 客户端 |
| `src/pages/ScheduledInspectionPage.tsx` | 定时巡检管理页面 |
| `src/pages/ScheduledInspectionPage.module.scss` | 页面样式 |

### CI/CD

| 文件 | 说明 |
|------|------|
| `.github/workflows/docker-build.yml` | push-to-main 自动构建 Docker 镜像到 GHCR |
| `FORK_CHANGES.md` | 本文件 |

---

## 修改的原有文件（可能冲突）

### 1. `usage-service/internal/store/store.go`

**改动位置**：
- `init()` 函数末尾（~行 131）：新增 `inspection_schedule` 和 `inspection_history` 两张表的 CREATE TABLE 语句
- 文件末尾（~行 569 之后）：新增 ~180 行 CRUD 方法

**冲突解决策略**：
- ✅ **保留本 fork 的改动** — 新增的表和方法是独立附加的，不修改任何现有表或方法
- 如果上游在 `init()` 中新增了其他表，只需确保我们的 CREATE TABLE 语句也被保留
- 如果上游修改了 `nullString` 或 `nullInt` 辅助函数，确保我们的 CRUD 方法仍然兼容

```
冲突热点：init() 函数的 statements 数组末尾
解决方法：保留上游新增的表 + 保留我们的 inspection_schedule 和 inspection_history
```

---

### 2. `usage-service/internal/httpapi/server.go`

**改动位置**：
- import 区：新增 `inspection` 包引用
- `Server` 结构体：新增 `scheduler *inspection.Scheduler` 字段
- `New()` 构造函数：新增 `scheduler` 参数
- `handleRoot()` 路由分发：新增 `/v0/management/inspection` 前缀判断

**冲突解决策略**：
- ✅ **保留本 fork 的改动**
- 如果上游修改了 `New()` 签名或新增参数，需要同时保留上游的新参数和我们的 `scheduler`
- 如果上游在 `handleRoot()` 中新增了其他路由前缀，确保我们的 `inspection` 路由也被保留

```
冲突热点：New() 函数签名、handleRoot() 路由链
解决方法：合并双方的参数/路由，都保留
```

---

### 3. `usage-service/cmd/cpa-manager/main.go`

**改动位置**：
- import 区：新增 `inspection` 包
- `main()` 中：创建 `inspection.NewScheduler(db)`，在两个启动分支中调用 `scheduler.Start()`
- HTTP 服务器创建：`httpapi.New()` 调用增加 `scheduler` 参数
- 关闭流程：增加 `scheduler.Stop()`

**冲突解决策略**：
- ✅ **保留本 fork 的改动**
- 如果上游修改了 `main()` 的启动逻辑或 `httpapi.New()` 签名，需同步调整
- 关键是确保 `scheduler` 在 `collector` 之后创建，在 `server.Shutdown` 之前 Stop

```
冲突热点：main() 函数的 httpapi.New() 调用和启动/关闭流程
解决方法：在上游的 New() 参数列表末尾追加 scheduler，在 Stop 流程中追加 scheduler.Stop()
```

---

### 4. `src/pages/CodexInspectionPage.tsx`

**改动位置**：
- import 区：新增 `IconTimer`（1 行）
- `statusActions` 区域（~行 864）：新增定时巡检入口 `<Link>`（4 行）

**冲突解决策略**：
- ✅ **保留本 fork 的改动** — 仅新增了一个按钮，不影响任何现有逻辑
- 如果上游重构了 `statusActions` 区域的 JSX 结构，在新结构中重新添加这个 Link 即可

```
冲突热点：statusActions 区域的 JSX
解决方法：在"返回监控"按钮之后、设置按钮之前加入定时巡检 Link
```

---

### 5. `src/router/MainRoutes.tsx`

**改动位置**：
- import 区：新增 `ScheduledInspectionPage` 引入（1 行）
- 路由数组：新增 `/monitoring/codex-inspection/scheduled` 路由（1 行）

**冲突解决策略**：
- ✅ **保留本 fork 的改动** — 纯追加
- 如果上游重组了路由，在对应位置重新添加即可

---

### 6. `src/i18n/locales/en.json`

**改动位置**：
- `monitoring` 命名空间末尾：新增 `codex_inspection_scheduled` + `scheduled_inspection_*` 系列约 40 条翻译

**冲突解决策略**：
- ✅ **保留本 fork 的改动** — 纯追加新 key
- 如果上游也在 `monitoring` 末尾新增了 key，合并时保留双方的 key 即可
- 注意 JSON 格式：确保倒数第二个 key 末尾有逗号

---

## 决策逻辑一致性提醒

> ⚠️ **重要**：如果上游更新了 `src/features/monitoring/codexInspection.ts` 中的巡检决策逻辑
> （如 `resolveWindowAwareProbeAction`、`resolveLegacyProbeAction`），需要同步更新
> `usage-service/internal/inspection/inspector.go` 中对应的 Go 实现。

检查方法：
```bash
git diff upstream/main -- src/features/monitoring/codexInspection.ts
```
关注以下函数的变更：
- `resolveWindowAwareProbeAction` → Go: `resolveWindowAwareAction()`
- `resolveLegacyProbeAction` → Go: `resolveLegacyAction()`
- `inspectSingleAccount` → Go: `probeAccount()`

---

## 合并上游更新的标准流程

```bash
# 1. 拉取上游最新代码
git fetch upstream

# 2. 合并到本地 main
git merge upstream/main

# 3. 如果有冲突，按本文件指引解决
#    - 新增文件：不会冲突
#    - 修改文件：按上述每个文件的策略解决

# 4. 编译验证
cd usage-service && go build ./...
cd .. && npm run build

# 5. 推送触发自动构建
git push origin main
```
