# AI Test Tool

一个面向研发与 QA 的自动化测试工作台。当前版本先收敛到三条主线：

- 从本地目录或 TAPD 建立项目
- 管理 Linux 测试环境，并隔离敏感凭据
- 从 TAPD 缺陷 / 需求同步素材，生成并维护测试用例

## 目录

```text
ai-test-tool/
├─ web/       React + TypeScript + Vite
├─ server/    Go HTTP API（仅标准库）
│  └─ cmd/mock-tapd  本地 TAPD OpenAPI 模拟服务（联调用）
└─ docs/      架构与界面规范
```

## 本地运行

后端（默认持久化到 `server/data/state.json`，可用 `AI_TEST_TOOL_DATA` 覆盖）：

```powershell
cd server
go run ./cmd/api
```

前端（另开一个终端）：

```powershell
cd web
npm install
npm run dev
```

访问 `http://localhost:4173`。Vite 会把 `/api` 代理到 `http://localhost:8080`。

没有 TAPD 凭据时，可另起模拟服务联调：

```powershell
cd server
go run ./cmd/mock-tapd   # 账号 qa-user / qa-pass，工作区 8888
```

## 连接 TAPD

在「连接设置 → TAPD」中选择传输方式：

1. **直连 OpenAPI**（推荐）
   - 认证方式二选一：API 账号（`api_user` + `api_password`）或个人访问令牌。
   - 个人访问令牌查看全部工作区需填写「公司 ID」（TAPD 网页地址栏中的数字）；也可填写「工作区 ID 清单」，平台会对清单逐个向 TAPD 实时验证并显示真实项目列表。两者都不填时，新建项目可手动输入工作区 ID，或仅显示已绑定项目的工作区。
2. **MCP 服务**
   - 支持官方 `mcp-server-tapd`（stdio，如 `uvx mcp-server-tapd`）与 http(s) 远程服务；http(s) 自动识别 Streamable HTTP 与旧版 SSE 两种传输。
   - 可点击「从本机 Claude / Cursor 配置导入」自动发现已有 TAPD MCP 地址。

凭据经 AES-GCM 加密后保存在服务端，API 响应永不回传明文密钥。

## 测试设计与一键执行

1. 在「测试用例」页生成用例：同步 TAPD 素材后勾选生成，或手动创建；来源区分为 TAPD / AI 智能体 / 手动。
2. 在「连接设置 → AI 智能体」配置执行与设计的智能体：本机 CLI（Claude Code、Codex，自动读取其配置的默认模型并显式指定）或 OpenAI 兼容接口。
3. 在「执行记录 → 一键测试」选择项目、测试环境（本机目录或已录入的 Linux 环境，经 SSH 执行）、用例、智能体与模型，开始执行；每条用例的通过状态、结论与执行证据实时入库。

## 验证

```powershell
cd server
go test ./...

cd ..\web
npm run build
```

领域模型、接口边界与后续迭代计划见[架构文档](docs/architecture.md)。
