## Why

本 MCP server 已具备数据库访问能力；Apipost 是团队另一个日常开发工具（接口设计、Mock、测试）。LLM 在写 handler、改接口定义时，需要能**读**项目下的 API 列表、**看**接口完整定义、**改 / 新建**接口定义来形成闭环。

Apipost 开放接口 V2（saas 版）提供了项目 / API / 接口用例等 HTTP JSON 端点，用 `api-token` Header 鉴权。本 change 把其中**项目内**的核心资源按分类暴露为一组 MCP 工具。**不包含**团队 / 项目生命周期（创建团队、创建项目）—— 这些低频动作保留给人工在 Apipost 控制台做；**也不包含**环境（environment）的 CRUD —— 环境多为个人私有配置，在 Apipost 客户端直接维护更自然。

## What Changes

- 新增 `internal/tools/apipost/` 子包：HTTP 客户端（`api-token` + `project_name` 全局 Header 注入）、URL 参数化构造、错误翻译
- `config.yaml` 顶层新增 `apipost` 段，**单实例**配置：
  - `base_url`（默认 Apipost 云 / 自托管均可）
  - `api_token`（个人 / 团队 Token）
  - `project_name`（Apipost 全局 Header；所有端点都需要）
  - 可选 `project_id` / `team_id`（作为默认上下文）
  - `request_timeout`（默认 30s）
- 暴露 **9 个 MCP 工具**，分三组：

  **项目读取**
  - `apipost_list_projects` — GET `/open/project/list`
  - `apipost_get_project` — GET `/open/project/info`

  **API 资源 CRUD**
  - `apipost_list_apis` — GET `/open/apis/list`（简约结构，含目录）
  - `apipost_get_apis` — POST `/open/apis/details`（批量详情；单个传一个 id 即可）
  - `apipost_create_http_api` — POST `/open/apis/create`（HTTP 类型接口；SSE / WebSocket / GraphQL 等 v1 外）
  - `apipost_update_api` — POST `/open/apis/update`
  - `apipost_delete_apis` — POST `/open/apis/delete`

  **接口用例**
  - `apipost_list_api_cases` — GET `/open/apis/sample`
  - `apipost_create_api_case` — POST `/open/apis/sample/create`

- 所有工具的响应 content 为一段 text，内容是 UTF-8 JSON：`{tool, http_status, data}` 或失败时 `{tool, http_status, error}`；失败 MCP 标 `isError: true`
- **不写审计日志**（本 change 不引入 apilog 包 / 不新开 `./logs/apipost-*.log`）；失败和慢调用的排查依赖 LLM 会话上下文与 Apipost 控制台
- **不对外暴露**：创建团队、创建项目、删除团队、删除项目、自动化测试、数据模型、导入 / 导出 OpenAPI、接口状态管理、全局管理
- 依赖：不引入第三方 HTTP 库，用 Go 标准库 `net/http`

## Capabilities

### New Capabilities
- `apipost-tools`: Apipost 资源的 MCP 工具集（9 个工具）；HTTP 客户端约束：`api-token` 与 `project_name` 全局 Header 注入、Token 不泄漏、参数化构造 URL 防 SSRF、响应超时

### Modified Capabilities
- `app-config`: 顶层配置新增 `apipost` 段（`base_url` / `api_token` / `project_name` / `request_timeout` 等），定义必填 / 可选字段；Token 在日志 / 错误中 MUST 脱敏

## Impact

- **代码**：新增 `internal/tools/apipost/client/`（HTTP 客户端、全局 Header 注入、错误翻译）、`internal/tools/apipost/tools/`（3 个资源域文件：`projects.go` / `apis.go` / `cases.go`）；`main.go` 条件注册（`apipost` 段存在时才注册）
- **依赖**：不新增
- **运行时**：启动时 `apipost` 段存在则校验字段 + 简单 HTTP 连通性可选探测（v1 不 probe，失败让首次调用返回）；段缺失则**软失败** —— stderr 提示未启用、不阻止启动
- **安全**：
  - `api_token` 绝不出现在日志 / 错误信息 / stderr 启动日志
  - URL 构造用 `net/url` 包安全拼接，Query 参数用 `url.Values`
  - path 不带用户参数（Apipost 全部用 query / body 传 id，天然避免 path injection）
  - 响应体大小上限 10 MiB，超出截断
- **文档**：README 新增 "Apipost 工具" 一节（与"数据库工具"平级），列工具目录 + `config.yaml` 示例 + Token 处理说明

## Scope Non-Goals（本 change 不做）

- 审计日志（`./logs/apipost-*.log`）
- 团队 / 项目的创建、更新、删除
- 自动化测试、数据模型、导入 / 导出 OpenAPI
- 全局参数 / 接口属性 / 接口状态 / 接口服务器管理
- 非 HTTP 接口类型（SSE / WebSocket / GraphQL / SocketIO / TCP 等）—— 可在后续 change 追加
- 多 Apipost 实例 / 多 Token（单实例配置）
- 客户端 rate-limit、重试策略（命中限流 / 5xx 原样透传）
- Apipost 文件附件的直接上传 / 下载
