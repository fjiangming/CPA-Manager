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

---

## 部署指南

### 前置条件

- Docker 和 Docker Compose 已安装
- CPA（CLI Proxy API）服务已运行，且已启用 Management

### 1. Docker Compose 部署

创建部署目录并编写配置：

```bash
mkdir -p /opt/cpa-manager && cd /opt/cpa-manager
```

创建 `docker-compose.yml`：

```yaml
services:
  cpa-manager:
    image: ghcr.io/fjiangming/cpa-manager:latest
    container_name: cpa-manager
    restart: unless-stopped
    ports:
      - "18317:18317"
    volumes:
      - cpa-data:/data
    environment:
      - HTTP_ADDR=0.0.0.0:18317
      - USAGE_DATA_DIR=/data
      - USAGE_DB_PATH=/data/usage.sqlite
      # - CPA_UPSTREAM_URL=http://cpa-host:8317       # CPA 上游地址（也可在 Web UI 登录时填写）
      # - CPA_MANAGEMENT_KEY=your-key                  # Management Key（也可在 Web UI 登录时填写）
      # - USAGE_COLLECTOR_MODE=auto                    # 采集模式：auto / http / resp
      # - USAGE_RESP_QUEUE=usage                       # RESP 队列名称
      # - USAGE_RESP_POP_SIDE=right                    # RESP 弹出方向：left / right
      # - USAGE_BATCH_SIZE=100                         # 每次批量拉取条数
      # - USAGE_POLL_INTERVAL_MS=500                   # 轮询间隔（毫秒）
      # - USAGE_QUERY_LIMIT=50000                      # 查询结果上限
      # - USAGE_CORS_ORIGINS=*                         # CORS 允许来源（逗号分隔）
      # - USAGE_RESP_TLS_SKIP_VERIFY=false             # 跳过 TLS 验证（RESP 模式）

volumes:
  cpa-data:
```

### 环境变量参考

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `HTTP_ADDR` | `0.0.0.0:18317` | 服务监听地址和端口 |
| `USAGE_DATA_DIR` | `/data` | 数据存储目录（SQLite 等） |
| `USAGE_DB_PATH` | `/data/usage.sqlite` | SQLite 数据库文件路径 |
| `CPA_UPSTREAM_URL` | _(空)_ | CPA 上游服务地址，如 `http://cpa-host:8317`。为空时需在 Web UI 登录页填写 |
| `CPA_MANAGEMENT_KEY` | _(空)_ | CPA Management Key。为空时需在 Web UI 登录页填写 |
| `CPA_MANAGEMENT_KEY_FILE` | `/run/secrets/cpa_management_key` | Management Key 文件路径（Docker Secrets 场景） |
| `CPA_MANAGER_CONFIG` | _(空)_ | 指定外部 JSON 配置文件路径，覆盖默认的 `config.json` |
| `USAGE_COLLECTOR_MODE` | `auto` | 采集模式：`auto`（自动选择）/ `http`（仅 HTTP 队列）/ `resp`（仅 RESP 协议） |
| `USAGE_RESP_QUEUE` | `usage` | RESP 协议队列名称 |
| `USAGE_RESP_POP_SIDE` | `right` | RESP 协议弹出方向：`left` / `right` |
| `USAGE_BATCH_SIZE` | `100` | 每次从队列批量拉取的条数 |
| `USAGE_POLL_INTERVAL_MS` | `500` | 队列轮询间隔（毫秒） |
| `USAGE_QUERY_LIMIT` | `50000` | 用量查询结果上限 |
| `PANEL_PATH` | _(空)_ | 自定义管理面板 HTML 文件路径（覆盖内置面板） |
| `USAGE_CORS_ORIGINS` | `*` | CORS 允许的来源列表（逗号分隔，如 `http://a.com,http://b.com`） |
| `USAGE_RESP_TLS_SKIP_VERIFY` | `false` | 是否跳过 RESP 连接的 TLS 证书验证 |

> **提示**：大多数场景下只需配置前 3 个变量（`HTTP_ADDR`、`USAGE_DATA_DIR`、`USAGE_DB_PATH`），其余在 Web UI 登录时或通过默认值即可工作。

启动服务：

```bash
docker compose up -d
```

访问管理面板：

```
http://<你的服务器IP>:18317/management.html
```

首次登录时填写你的 CPA 上游地址（如 `http://cpa-host:8317`）和 Management Key。

### 2. 自定义端口

如需修改对外端口，只需调整 `ports` 映射：

```yaml
ports:
  - "9090:18317"   # 对外 9090，容器内仍为 18317
```

### 3. 升级更新

代码推送到 `main` 分支后，GitHub Actions 会自动构建新镜像。服务器上执行：

```bash
cd /opt/cpa-manager

# 拉取最新镜像
docker compose pull

# 用新镜像重建容器（数据卷不受影响）
docker compose up -d
```

> **数据安全**：SQLite 数据库存储在 Docker Volume `cpa-data` 中，升级不会丢失数据。

如需确认当前运行的镜像版本：

```bash
docker inspect cpa-manager --format '{{.Image}}' | cut -c1-12
docker images ghcr.io/fjiangming/cpa-manager --format '{{.ID}} {{.Tag}} {{.CreatedAt}}'
```

### 4. 完全卸载

```bash
cd /opt/cpa-manager

# 停止并删除容器
docker compose down

# 删除数据卷（⚠️ 这将永久删除所有巡检历史和用量统计数据）
docker volume rm cpa-manager_cpa-data

# 删除本地镜像
docker rmi ghcr.io/fjiangming/cpa-manager:latest

# 删除部署目录
cd / && rm -rf /opt/cpa-manager
```

> ⚠️ `docker volume rm` 会永久删除数据库，操作前请确认不再需要历史数据。如需备份：
> ```bash
> docker cp cpa-manager:/data/usage.sqlite ./usage.sqlite.bak
> ```

### 5. 查看日志

```bash
# 实时日志
docker compose logs -f cpa-manager

# 最近 100 行
docker compose logs --tail 100 cpa-manager
```
