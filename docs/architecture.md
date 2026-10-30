# 架构规划

## 1. 目标与取舍

首版采用模块化单体，而不是微服务。项目、环境、用例、TAPD 连接和执行记录都在一个 Go 进程中，通过明确接口隔离。这样能保持部署与代码简单，也不会把未来的远程执行器和 MCP 连接写死在业务层。

```text
React Web
   │ JSON/HTTP
Go API
   ├─ Project module
   ├─ Environment module ── SecretVault
   ├─ TestCase module
   ├─ TAPD connector ─────── MCP transport
   └─ Run orchestrator ───── Linux agent / SSH (next)
         │
      Repository
      SQLite → PostgreSQL (when multi-user)
```

## 2. 技术栈

| 层 | 选择 | 原因 |
|---|---|---|
| Web | React 19、TypeScript、Vite | 组件边界清晰，构建快，适合工具型界面 |
| API | Go 1.24、`net/http` | 依赖少，部署为单二进制，接口可读 |
| 数据库 | SQLite（下一阶段） | 单机起步成本低；Repository 接口便于迁移 PostgreSQL |
| 密钥 | AES-GCM + 外部主密钥 | 数据库只保存密文；主密钥不能写进数据库或 Git |
| TAPD | 内置 Connector + MCP Transport | 业务层只依赖统一的 TAPD 能力，不依赖具体传输实现 |
| Linux 执行 | SSH 引导 + 轻量 Agent（下一阶段） | 先覆盖常见场景，后续再做队列与并发调度 |

## 3. 领域模型

- `Project`：项目名称、来源（本地目录 / TAPD）、来源配置、状态。
- `Environment`：独立的测试资源，包含操作系统、地址、端口、账号、认证方式和密钥引用；通过多对多关系分配给零个或多个项目。
- `TestCase`：标题、步骤、预期结果、优先级、来源及外部对象 ID。
- `ExternalItem`：TAPD 中的 Bug / 需求的统一快照，用于同步与生成用例。
- `TestRun`：一次执行的快照；后续包含提交版本、环境、用例集、日志和产物。

环境只保存 `secret_ref`，API 永不返回密码。当前演示版使用进程内 AES-GCM 密钥；生产版必须通过环境变量或密钥服务注入稳定主密钥。

## 4. 模块边界

### MCP 内核层

`internal/mcp` 是内置的通用 MCP 客户端内核（仅标准库）：

1. JSON-RPC 2.0 封装：`initialize` 握手、`tools/list`、`tools/call`。
2. 三种传输：Streamable HTTP（POST JSON-RPC）、旧版 HTTP+SSE（GET 事件流 + POST endpoint）、stdio 子进程。
3. http(s) 地址自动在 Streamable HTTP 与 SSE 间降级；stdio 由后端启动子进程并注入环境变量凭据。

### TAPD Connector

`tapd.Manager` 是业务入口，能力包括：

1. 连接配置（界面保存，AES-GCM 加密密钥）与真实探测；`Status` 区分"未配置 / 凭据有效 / 连接失败"。
2. OpenAPI 直连：Basic（API 账号）与 Bearer（个人访问令牌）认证；分页同步缺陷 / 需求（每类上限 500 条）。
3. MCP 传输：工具按名称启发式匹配，结果 JSON 宽容解析，与直连产出同一 `ExternalItem` 模型。
4. 工作区发现：API 账号或已填公司 ID 的个人令牌可列全部；个人令牌缺公司 ID 时返回 `ErrCompanyIDRequired`，API 层降级为枚举已绑定项目的工作区。
5. 把选中的外部对象交给用例生成器；生成结果为草稿，由用户确认后入库。

Transport 负责 MCP 握手、工具发现、超时与重试；Connector 负责 TAPD 语义。二者分开，未来可替换传输而不改业务页面。

### 持久化

当前使用 JSON 文件仓库（`AI_TEST_TOOL_DATA`，默认 `server/data/state.json`）+ 独立密钥文件；每次变更原子落盘。SQLite migration 与外部主密钥属于 M1，Repository 接口已预留。

### Linux 执行层

第一阶段只建环境并做连通性检查；第二阶段增加执行器：

```text
TestRun queued → prepare workspace → execute command → stream logs → collect artifacts → finish
```

执行命令必须使用模板白名单和参数数组，禁止把用户文本直接拼接到 shell。每次执行使用独立工作目录、超时和最小权限账号。

## 5. API 草案

```text
GET    /api/health
GET    /api/projects
POST   /api/projects
GET    /api/environments
POST   /api/environments
PATCH  /api/environments/{id}/projects
GET    /api/test-cases
POST   /api/test-cases
POST   /api/test-cases/generate
GET    /api/external-items?projectId=
GET    /api/tapd/status
GET    /api/tapd/settings
PUT    /api/tapd/settings
POST   /api/tapd/test
GET    /api/tapd/workspaces
GET    /api/tapd/local-mcp
POST   /api/tapd/sync
POST   /api/runs             # 执行器完成后开放
```

## 6. 迭代顺序

1. **M0 · 当前骨架**：产品界面、内存 Repository、项目 / 环境 / 用例 API、TAPD Connector 边界。
2. **M1 · 可持久使用**：SQLite migration、登录、稳定主密钥、操作审计、目录授权记录。
3. **M2 · TAPD 闭环**：MCP Transport、连接配置、增量同步、Bug / 需求映射、人工确认生成用例。
4. **M3 · Linux 执行**：连通性检查、任务队列、Agent/SSH 执行、实时日志、产物下载。
5. **M4 · 团队化**：PostgreSQL、RBAC、多租户、并发配额与通知。

## 7. 关键约束

- 浏览器不能把任意本地绝对路径交给服务器直接访问。桌面浏览器支持时使用 File System Access API；远程部署时改为上传源码、Git 仓库或本地 Agent。
- TAPD 同步和用例生成是两个动作；同步不能自动覆盖人工编辑过的用例。
- 所有外部调用带超时、幂等键和审计记录。
- 密码、Token、私钥不进入日志、响应和前端持久化。

