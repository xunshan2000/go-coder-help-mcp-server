## ADDED Requirements

### Requirement: Apipost 工具命名与注册条件
系统 SHALL 以 `apipost_` 为统一前缀注册 Apipost 相关 MCP 工具；每个工具与一个 Apipost 开放接口 V2 端点一一对应。Apipost 顶层配置段（`apipost`）整段存在且通过校验时 SHALL 注册全部 9 个工具；整段缺失时 MUST NOT 注册任何 `apipost_` 前缀的工具，且进程 MUST 在 stderr 打印一行 `apipost not configured; skipping apipost tools`，但 MUST NOT 因此启动失败。

#### Scenario: 配置存在时注册工具
- **WHEN** `config.yaml` 含合法 `apipost` 段且校验通过
- **THEN** MCP `ListTools` 响应 MUST 至少包含全部 9 个 `apipost_*` 工具
- **AND** 所有 `apipost_*` 工具的 name 字段 MUST 以 `apipost_` 开头

#### Scenario: 配置缺失时跳过注册
- **WHEN** `config.yaml` 完全不含 `apipost` 段
- **THEN** 进程 MUST 启动成功
- **AND** MCP `ListTools` 响应 MUST NOT 包含任何 `apipost_` 前缀的工具
- **AND** stderr MUST 出现一行 `apipost not configured; skipping apipost tools`

### Requirement: 全局 Header 注入
HTTP 客户端 SHALL 在每一次向 Apipost 发起请求时，自动注入以下两个 HTTP Header：
- `api-token: <config.apipost.api_token>`
- `project_name: <config.apipost.project_name>`

两个 Header 对所有 `apipost_*` 工具统一生效，不作为工具参数暴露给 LLM。请求 body 非 nil 时 MUST 附加 `Content-Type: application/json`。

#### Scenario: 所有端点都注入全局 Header
- **WHEN** 任意 `apipost_*` 工具发起请求
- **THEN** 实际 HTTP 请求 MUST 包含 `api-token` 与 `project_name` 两个 Header
- **AND** 两个 Header 的取值 MUST 分别等于 config 的 `apipost.api_token` 与 `apipost.project_name`

### Requirement: Token 脱敏
系统 MUST NOT 在任何日志、启动信息、MCP 响应、错误文本或 stderr 输出中回显 `api_token` 明文。Token 在内部数据结构中 MAY 保留明文用于 HTTP Header，但任何对外输出路径 MUST 先经统一脱敏（将 Token 字符串替换为 `***`）。

#### Scenario: 启动日志不泄漏 Token
- **WHEN** `apipost.api_token` 为 `apt_secret123456789`，进程成功启动
- **THEN** stderr 全部输出 MUST NOT 包含字符串 `apt_secret123456789`

#### Scenario: HTTP 错误不泄漏 Token
- **WHEN** 某次 Apipost 工具调用失败，错误信息被 wrap 并作为 MCP 响应的 error 字段返回
- **THEN** MCP 响应 text 与 error 字段 MUST NOT 包含 Token 明文

### Requirement: URL 与参数安全构造
HTTP 客户端 SHALL 使用 `net/url` 构造完整 URL —— `baseURL.JoinPath(path)` 拼接 path，`url.Values` 序列化 query 参数。Apipost 开放接口 V2 的所有资源 id（project_id / target_id / env_id / target_ids / sample_ids 等）通过 query 或 JSON body 传递，MUST NOT 作为 URL path 段拼接。

#### Scenario: project_id 以 query 传递
- **WHEN** LLM 调用 `apipost_list_apis` 传入 `project_id: "1bfd2a779bc26001"`
- **THEN** 实际发起的请求 URL query MUST 包含 `project_id=1bfd2a779bc26001`
- **AND** URL path MUST 为 `/open/apis/list`（不含 id）

#### Scenario: target_ids 以 body 数组传递
- **WHEN** LLM 调用 `apipost_delete_apis` 传入 `target_ids: ["id1","id2"]`
- **THEN** 实际请求的 JSON body MUST 含 `"target_ids":["id1","id2"]`

### Requirement: 请求超时
每次 Apipost HTTP 请求 SHALL 同时受 `apipost.request_timeout` 与调用方 `context.Context` 约束。超时触发时 MUST 返回 `isError: true`，错误信息 MUST 指明"请求超时"，MUST NOT 包含 Token。

#### Scenario: 请求超时返回明确错误
- **WHEN** Apipost 响应慢于 `request_timeout`
- **THEN** 工具调用 MUST 返回 `isError: true`
- **AND** 错误信息 MUST 包含"请求超时"文本（或等价语义）

### Requirement: 响应体大小上限与截断标记
HTTP 客户端 SHALL 对 Apipost 响应体施加字节数上限（默认 `10485760` = 10 MiB，可由 `apipost.max_response_bytes` 覆盖）。读取超过上限时 MUST 截断并丢弃剩余内容（不阻塞 socket）；此时 MCP 工具响应 MUST 标记 `truncated: true`，且响应 JSON 可能不完整。

#### Scenario: 正常大小响应不截断
- **WHEN** Apipost 返回 1 KiB 响应体
- **THEN** MCP 响应 `truncated` 字段 MUST 为 `false` 或缺省
- **AND** `data` 字段 MUST 为解析后的完整 JSON 对象

#### Scenario: 超限响应被截断
- **WHEN** `apipost_list_apis` 触发 Apipost 返回 20 MiB 响应，`max_response_bytes` 为默认 10 MiB
- **THEN** MCP 响应 MUST 含 `truncated: true`
- **AND** 进程 MUST NOT 因响应过大而 OOM

### Requirement: 错误翻译
HTTP 客户端 SHALL 把以下几类结果翻译成统一的工具错误：
- `ctx.Err()` 取消或超时 → `isError: true`，文本"请求超时"
- HTTP status 非 2xx → `isError: true`，文本含 `http <status>: <Apipost 响应体前 500 字节>`
- HTTP 2xx 但响应体 JSON 解析失败 → `isError: true`，文本"响应 JSON 解析失败"
- HTTP 2xx 且 `data.code != 0`（Apipost 业务失败） → `isError: true`，文本含 `code=<N> msg=<原 msg>`
- 所有错误文本 MUST NOT 包含 Token 明文

#### Scenario: Apipost 业务失败透传
- **WHEN** Apipost 返回 HTTP 200 + `{"code":10020,"msg":"target_id 不存在","data":null}`
- **THEN** 工具 MCP 响应 MUST 为 `isError: true`
- **AND** 错误文本 MUST 包含 `10020` 与 `target_id 不存在` 字样

#### Scenario: HTTP 4xx 透传
- **WHEN** Apipost 返回 HTTP 401 + 非 JSON 响应
- **THEN** 工具 MCP 响应 MUST 为 `isError: true`
- **AND** 错误文本 MUST 包含 `401` 数字

### Requirement: 工具响应 JSON 形态
所有 `apipost_*` 工具的 MCP 响应 content 中 SHALL 包含一段 text，内容为 UTF-8 JSON 对象，字段约束：
- `tool`: string，工具 name（如 `apipost_get_apis`）
- `http_status`: integer，Apipost 返回的 HTTP status code（未发起请求时 MAY 为 `0`）
- `truncated`: boolean，响应体是否被截断
- 成功时附加 `data`: 原样透传 Apipost 响应体 JSON（包含 Apipost 统一结构 `{code, msg, data}`，不重组）
- 失败时附加 `error`: 单行错误摘要

失败响应 MUST 同时标记 MCP `isError: true`；成功响应 `isError` MUST 缺省或为 `false`。

#### Scenario: 成功响应形态
- **WHEN** `apipost_list_projects` 对 Apipost 发起请求并收到 HTTP 200 + `{"code":0,"msg":"成功","data":[...]}`
- **THEN** MCP 响应 text MUST 为合法 JSON
- **AND** 该 JSON MUST 含 `tool: "apipost_list_projects"`, `http_status: 200`, `data.code: 0`, `data.data: [...]`
- **AND** MCP `isError` MUST 为 `false` 或缺省

#### Scenario: 失败响应形态
- **WHEN** `apipost_get_apis` 发起请求收到 HTTP 200 + `{"code":403,"msg":"无权限"}`
- **THEN** MCP 响应 MUST 标记 `isError: true`
- **AND** 响应 text JSON MUST 含 `tool: "apipost_get_apis"`, `http_status: 200`, `error`（含 `403` 与 `无权限` 字样）

### Requirement: 默认 project_id / team_id 回落
当工具需要 `project_id` / `team_id` 但 LLM 未在参数中显式传入时，系统 SHALL 回落使用 `config.apipost.project_id` / `config.apipost.team_id` 的值；若配置中也未设置且工具必需该字段，则工具 MUST 以 `isError: true` 返回，错误信息指明缺失字段。

#### Scenario: 使用配置默认 project_id
- **WHEN** `apipost.project_id` 配置为 `prj_abc`，LLM 调用 `apipost_list_apis` 未传 `project_id`
- **THEN** 工具 MUST 使用 `prj_abc` 作为请求参数

#### Scenario: 必填字段缺失且无默认
- **WHEN** 配置未设置 `project_id`，LLM 调用 `apipost_list_apis` 未传 `project_id`
- **THEN** 工具 MUST 返回 `isError: true`
- **AND** 错误信息 MUST 指明 `project_id` 必填

### Requirement: 项目读取工具
系统 SHALL 注册以下只读工具：
- `apipost_list_projects`：`GET /open/project/list`，query 参数 `team_id`（可选，回落到 config 默认）、`action`（可选，默认 `0` 表示"全部"）
- `apipost_get_project`：`GET /open/project/info`，query 参数 `project_id`（可选，回落到 config 默认）

#### Scenario: list_projects 使用默认 team_id
- **WHEN** `apipost.team_id` 配置为 `tm_abc`，LLM 调用 `apipost_list_projects` 未传 `team_id`
- **THEN** 实际请求 URL query MUST 含 `team_id=tm_abc`

### Requirement: API 资源 CRUD 工具
系统 SHALL 注册以下 5 个 API 资源操作工具：
- `apipost_list_apis`：`GET /open/apis/list`（简约结构，含目录节点）
- `apipost_get_apis`：`POST /open/apis/details`，body 参数 `target_ids` 数组（单个资源时传 `[id]`）
- `apipost_create_http_api`：`POST /open/apis/create`，body 为 HTTP 类型 API 完整定义
- `apipost_update_api`：`POST /open/apis/update`，body 含 `target_id` 的完整定义；工具描述 MUST 明示"非必填字段不传会被置默认值，调用前应先取 details"
- `apipost_delete_apis`：`POST /open/apis/delete`，body 参数 `target_ids` 数组

#### Scenario: get_apis 单个 id
- **WHEN** LLM 调用 `apipost_get_apis` 传入 `target_ids: ["abc123"]`
- **THEN** 实际 POST body MUST 含 `"target_ids":["abc123"]`
- **AND** Apipost 返回的 `data` 数组透传到工具响应

#### Scenario: delete_apis 批量
- **WHEN** LLM 调用 `apipost_delete_apis` 传入 `target_ids: ["id1","id2","id3"]`
- **THEN** 实际请求 MUST 为单次 POST，body 含三个 id
- **AND** 工具不再分批发送

### Requirement: 接口用例工具
系统 SHALL 注册以下 2 个接口用例工具：
- `apipost_list_api_cases`：`GET /open/apis/sample`，可选 query `target_ids[]` / `sample_ids[]`
- `apipost_create_api_case`：`POST /open/apis/sample/create`，body 含 `target_id`（所属接口 id）、`type:"sample"`、`name`、`method`、`url`、`request`

#### Scenario: create_api_case 最小字段
- **WHEN** LLM 调用 `apipost_create_api_case` 传入必填字段
- **THEN** 工具 MUST 发起 POST 请求到 `/open/apis/sample/create`
- **AND** 非 200 或 `code != 0` 的响应按"错误翻译"转成 `isError: true`
