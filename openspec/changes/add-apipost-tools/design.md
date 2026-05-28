## Context

`add-db-tools` 已建好"外部系统 → MCP 工具"的骨架（配置加载、Registry）。Apipost 作为又一个外部系统接入 —— HTTP JSON 开放接口，`api-token` Header 鉴权。

与 DB 差异点：
- DB 有**方言差异**（需 Driver 接口抽象）；Apipost 只此一家、HTTP JSON，不抽象
- DB 是**核心依赖**（启动必须可达）；Apipost 是**可选**（没配置就不注册）
- DB 的风险面是 SQL 注入 / 写语句；Apipost 的风险面是 **Token 泄漏**（path 层 Apipost 不用 id 拼 URL，天然没 SSRF 风险面）

**本 change 与前版差异**（用户 2026-05-06 反馈）：
- **删去**审计日志（原计划的 `apipost-audit` capability 不纳入 v1）
- **砍掉**团队 / 项目生命周期操作、导入导出、自动化测试、数据模型、非 HTTP 接口类型
- **保留**项目内核心资源（项目读取 / API CRUD / 接口用例），共 9 个工具
- **砍掉**环境（environment）的 CRUD —— 环境多为个人私有配置（`is_private=1`），直接在 Apipost 客户端维护更顺手；公共环境也是少量、低频的管理动作，不值得走 MCP

## Goals / Non-Goals

**Goals:**
- 提供项目内核心资源的 MCP 工具（9 个）
- `api-token` 与 `project_name` 两个全局 Header 通过 `config.yaml` 统一注入；绝不出现在日志 / 错误 / 响应回显中
- URL 构造走 `net/url`，Query 参数用 `url.Values`
- 请求有超时、响应体大小有上限（10 MiB）
- 未配置 `apipost` 段时，启动日志提示"未启用"，但不失败

**Non-Goals:**
- 不引入第三方 HTTP 客户端库（`net/http` 足够）
- 不做"API 平台 Driver 接口"抽象
- 不做客户端侧 rate-limit；命中 Apipost 限流直接把响应 code 原样返回
- 不做 WebSocket / SSE 订阅（超出 MCP stdio 工具边界）
- 不做复杂 retry 策略
- 不缓存响应
- 不做多 workspace / 多 token
- 不做审计日志（前置决策）
- 不做 Apipost 文件附件上传 / 下载
- 不做环境（environment）管理（list / get / create / update / delete）—— 环境是个人私有配置为主，Apipost 客户端操作更自然
- 不覆盖 Apipost 全部资源模型 —— 剩余的留给后续 change

## Decisions

### 1. 配置结构：`apipost` 顶层段

```yaml
apipost:
  base_url: https://v2-openapi.apipost.net       # Apipost 官方云 / 自托管均可
  api_token: apt_xxxxxxxxxxxxxxxxxxxxxxxx          # 个人 / 团队 Token（注入为 api-token Header）
  project_name: 我的项目                            # 注入为 project_name Header（全局必填）
  project_id: 1bfd2a779bc26001                    # 可选：工具未传 project_id 时使用
  team_id: 1bfd2a3823426001                       # 可选：list_projects 未传 team_id 时使用
  request_timeout: 30s
  max_response_bytes: 10485760                     # 10 MiB
```

校验规则：
- `apipost` 段 **整段可选**。存在时：
  - `base_url` 必填，必须以 `http://` 或 `https://` 开头，URL 合法
  - `api_token` 必填，非空字符串
  - `project_name` 必填，非空字符串（Apipost 所有接口都需要此 Header）
  - `team_id` / `project_id` 可选，非空字符串，匹配 `^[A-Za-z0-9_-]+$`
  - `request_timeout` 合法 Go duration；缺省 `30s`
  - `max_response_bytes` 正整数；缺省 `10485760`
- 整段缺失 → `apipost` 工具不注册，stderr 打一行 `apipost not configured; skipping apipost tools`

**Rationale**：单实例配置最简单；Token 一个字段集中管理便于轮换。`project_name` 是 Apipost 特殊的全局 Header，必须预配（不是任何端点的 path / query 参数）。

### 2. 不引入 Platform Driver 抽象

Apipost / Apifox / Postman 资源模型差异大。DB Driver 抽象的共享流程（守卫 / 只读 tx / 截断）在 Apipost 工具层不存在；每个工具就是"请求 → 解析响应 → 返回"。YAGNI。

### 3. HTTP 客户端封装

```go
// internal/tools/apipost/client/client.go
package client

type Client struct {
    baseURL          *url.URL
    apiToken         string
    projectName      string
    defaultProjectID string
    defaultTeamID    string
    httpClient       *http.Client
    maxResponseBytes int64
}

func New(cfg config.ApipostConfig) (*Client, error) { ... }

// Do 发起一次请求到 {baseURL}{path}，query 与 body 按需传递；响应 JSON decode 进 out。
func (c *Client) Do(ctx context.Context, method, path string,
    query url.Values,
    body any,
    out any,
) (*ResponseMeta, error) { ... }

type ResponseMeta struct {
    StatusCode int
    Truncated  bool
    BodyBytes  int64
}
```

关键实现点：
- **URL 构造**：`baseURL.JoinPath(path)` 拼接；query 用 `url.Values` 序列化；path 里**不允许**拼用户参数（Apipost 所有资源 id 都通过 query / body 传，天然避免 path injection）
- **Header 注入**：每次请求加 `api-token: <api_token>` + `project_name: <project_name>` 两个 Header；`Content-Type: application/json` 只在 body 非 nil 时加
- **超时**：`http.Client.Timeout = request_timeout` + `ctx` 超时
- **响应大小限制**：`io.LimitReader(resp.Body, maxResponseBytes+1)`；超出 → `truncated: true`，尾部丢弃不阻塞连接
- **错误翻译**：
  - `ctx.Err()` → "请求超时"
  - HTTP 非 2xx → 从响应体提取 `code` / `msg`（Apipost 统一响应格式 `{code, msg, data}`）；`code != 0` 视为业务失败
  - JSON decode 失败 → "响应 JSON 解析失败"
  - 所有错误路径的输出文本 MUST NOT 包含 `c.apiToken` 明文（`strings.ReplaceAll` 兜底）
- **重试**：v1 不做任何重试（避免写接口副作用 + 简化）

### 4. Apipost 统一响应格式

Apipost 所有端点的成功响应形如：
```json
{ "code": 0, "msg": "成功", "data": { ... 具体载荷 ... } }
```

失败时 `code != 0`，`msg` 带错误描述。HTTP status 可能仍为 200（业务层错误），也可能为 4xx / 5xx。

**工具层策略**：
- `http_status == 200 && code == 0` → 成功；`data` 字段透传
- `http_status != 200` 或 `code != 0` → 工具 `isError: true`；错误信息含 `msg` 原文 + `code` 数字

### 5. 工具粒度与命名

- 所有工具 `name` 以 `apipost_` 前缀
- 每个工具对应一个 Apipost 端点，不做超级工具
- 参数用 string / number / object / array，避免奇怪嵌套
- **`project_id` 默认值**：`create / update / delete` 类工具若未传 `project_id`，fallback 到 config 的 `apipost.project_id`；仍为空 → `isError` 拒绝
- **`target_ids` / `sample_ids` 等数组**：作为 JSON array 参数传；LLM 传单个资源时用 `[id]`

### 6. 端点映射表

| Tool | Method | Path | 关键参数 |
|---|---|---|---|
| `apipost_list_projects` | GET | `/open/project/list` | query: `team_id`, `action` (默认 `0`) |
| `apipost_get_project` | GET | `/open/project/info` | query: `project_id` |
| `apipost_list_apis` | GET | `/open/apis/list` | query: `project_id` |
| `apipost_get_apis` | POST | `/open/apis/details` | body: `{project_id, target_ids[]}` |
| `apipost_create_http_api` | POST | `/open/apis/create` | body: HTTP API 定义（含 `project_id`、`target_type:"api"`、`parent_id`、`method`、`url`、`request`、`response` 等） |
| `apipost_update_api` | POST | `/open/apis/update` | body: 含 `target_id` 的完整 API 定义（Apipost 要求非必填字段不传会被重置为默认值 —— 调用方需先 get 详情再改） |
| `apipost_delete_apis` | POST | `/open/apis/delete` | body: `{project_id, target_ids[]}` |
| `apipost_list_api_cases` | GET | `/open/apis/sample` | query: `project_id`, 可选 `target_ids[]` / `sample_ids[]` |
| `apipost_create_api_case` | POST | `/open/apis/sample/create` | body: `{project_id, target_id, type:"sample", name, method, url, request}` 等 |

所有端点继承"全局 Header：`api-token` + `project_name`"约束。

### 7. 工具响应形态

成功：
```json
{
  "tool": "apipost_get_apis",
  "http_status": 200,
  "data": { "code": 0, "msg": "成功", "data": [...] }
}
```
这里 `data` 字段 = Apipost 响应体整体（含 code / msg / data 嵌套），透传不重组，避免与 schema 耦合。

失败：
```json
{
  "tool": "apipost_update_api",
  "http_status": 200,
  "error": "code=10020 msg=target_id 不存在"
}
```
或 HTTP 4xx / 5xx：
```json
{
  "tool": "apipost_get_api",
  "http_status": 401,
  "error": "http 401: unauthorized"
}
```
错误响应 MCP `isError: true`。

## Risks / Trade-offs

- **[风险] Token 泄漏**：`api_token` 脱敏是第一风险。所有错误输出 MUST 经 `sanitize()` 去 token；启动日志、HTTP 错误 wrap、JSON 解码错误片段全覆盖 → **缓解**：client 层集中 `strings.ReplaceAll`，单元测试构造含 token 的错误路径断言无泄漏
- **[风险] `project_name` 配错导致所有调用 401 / 403**：用户如果把 Token 和错误的 project_name 配到一起，所有调用都失败 → **缓解**：README 明示"`project_name` 必须与 Token 所属的项目或团队匹配"
- **[风险] 巨型响应**：列表类端点在大项目里可能返回几百 KB 级 → **缓解**：`max_response_bytes` 默认 10 MiB 截断；README 明示不建议用 `list_apis` 过度拉 大项目的全量
- **[风险] 误改 / 误删**：`delete_apis` / `update_api` 可造成破坏 → **缓解**：依赖 Apipost 账号权限（只读 Token 就不给写）；README 强调"Token 最小权限原则"；v1 工具层不加二次确认逻辑
- **[风险] `update_api` 字段丢失陷阱**：Apipost 文档明示"修改接口非必填字段不传会被置默认值"，LLM 直接构造 patch 会丢字段 → **缓解**：工具描述里明示"调用前应先用 `apipost_get_apis` 取完整定义，在其上改动后整体回传"；不做自动 merge（字段结构嵌套太深，自动 merge 容易掩盖 bug）
- **[风险] Apipost API 版本漂移**：Apipost 升级接口字段名 / 路径变化 → **缓解**：`data` 透传、不强耦合 schema；本 change spec 描述工具**行为**而非具体响应字段
- **[权衡] 不做 rate-limit**：限流响应直接传给 LLM，让 LLM 自己退让
- **[权衡] 不缓存 / 不重试**：简化；写接口幂等性无法保证，重试会副作用加倍

## Migration Plan

1. 扩展 `internal/config/config.go`：新增 `ApipostConfig` struct（指针嵌入 `Config.Apipost`）+ `Load` 填默认 + `Validate` 校验可选段
2. 新建 `internal/tools/apipost/client/client.go`：`Client` 类型、`New` / `Do`、全局 Header 注入、大小限制、Token 脱敏的错误翻译
3. 新建 `internal/tools/apipost/tools/`：三个资源域文件（`projects.go` / `apis.go` / `cases.go`）各自定义工具 + handler
4. 新建 `internal/tools/apipost/register.go`：注册全部 9 个工具
5. 改 `main.go`：`cfg.Apipost == nil` 打印 skip 日志；否则构造 client + register
6. 更新 README：新增"Apipost 工具"一节、配置示例、Token 处理
7. 手动验证：用真 Token 跑每个工具，记录响应形态

**Rollback**：新增代码完全可逆。

## Open Questions

- **Apipost `base_url` 的准确值**：文档中用 `{{host}}` 占位。Apipost 官方云 API V2 最可能是 `https://v2-openapi.apipost.net` 或类似；implementation 首步要查 Apipost 控制台确认并写进 README 的 `config.example.yaml` 注释
- **`update_api` body 的完整字段集**：Apipost 文档在 HTTP API 创建 / 修改处列了几百字段（auth、request、response、body mode 等嵌套）；工具层用 `map[string]any` 透传 LLM 传的 body，不做字段校验 —— 让 Apipost 服务端校验。implementation 阶段确认这点可行
- **批量接口的单条 vs 多条**：`get_apis` / `delete_apis` 参数都是数组，LLM 传单个 id 的场景用 `[id]`，README 给明示例
