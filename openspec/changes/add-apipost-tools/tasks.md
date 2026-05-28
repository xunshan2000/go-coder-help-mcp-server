## 1. 配置层扩展

- [x] 1.1 在 `internal/config/config.go` 新增 `ApipostConfig` struct（字段：`BaseURL`、`APIToken`、`ProjectName`、`ProjectID`、`TeamID`、`RequestTimeout time.Duration`、`MaxResponseBytes int64`），指针类型嵌入 `Config.Apipost`（`nil` 表示未配置）
- [x] 1.2 扩展 `Load`：apipost 段缺失时 `cfg.Apipost = nil`；存在时填默认（`request_timeout=30s` / `max_response_bytes=10485760`）
- [x] 1.3 扩展 `Validate`：apipost 段存在时校验 `base_url` 格式（`http(s)://` + `url.Parse`）、`api_token` 非空、`project_name` 非空、`team_id`/`project_id` 匹配 `^[A-Za-z0-9_-]+$`、`request_timeout > 0`、`max_response_bytes > 0`
- [x] 1.4 给 apipost 校验写单元测试（缺段 / 合法 / 各字段非法 / 默认值生效）

## 2. HTTP 客户端

- [x] 2.1 新建 `internal/tools/apipost/client/client.go`：定义 `Client`、`ResponseMeta`、`New(cfg)`
- [x] 2.2 实现 `Do(ctx, method, path, query, body, out) (*ResponseMeta, error)`:
  - `baseURL.JoinPath(path)` 拼 URL，query 用 `url.Values`
  - 注入 `api-token` / `project_name` 两个 Header；body 非 nil 时加 `Content-Type: application/json`
  - `io.LimitReader(resp.Body, maxResponseBytes+1)`，超限丢尾部并标 `Truncated=true`
  - HTTP 非 2xx / Apipost `code != 0` / ctx 超时 / JSON 解析失败各自给明确错误文本
  - 错误路径统一 `sanitizeToken(msg, token)` 去明文
- [x] 2.3 实现 `sanitizeToken(s, token)`（返回把所有 token 子串替换为 `***` 的字符串）
- [x] 2.4 client 单元测试：用 `httptest.Server` mock Apipost，覆盖
  - Header 注入（校验 request 里 `api-token` 与 `project_name` 都存在且值正确）
  - 大小截断（返回 20 MiB body，`max_response_bytes=1 MiB`）
  - `code != 0` 翻译为错误
  - HTTP 401 翻译为含 `401` 的错误
  - 错误文本不含 Token 明文

## 3. 工具 handler

- [x] 3.1 新建 `internal/tools/apipost/tools/common.go`：`successResponse(tool, meta, data) *mcp.CallToolResult`、`errorResponse(tool, meta, err) *mcp.CallToolResult`、`resolveProjectID(args, cfg) (string, error)` / `resolveTeamID` 回落逻辑
- [x] 3.2 新建 `projects.go`：实现 `apipost_list_projects`（GET `/open/project/list`，query `team_id` + `action`）、`apipost_get_project`（GET `/open/project/info`，query `project_id`）
- [x] 3.3 新建 `apis.go`：
  - `apipost_list_apis` — GET `/open/apis/list`，query `project_id`
  - `apipost_get_apis` — POST `/open/apis/details`，body `{project_id, target_ids[]}`
  - `apipost_create_http_api` — POST `/open/apis/create`，body 透传 LLM 传入（含 `project_id`、`target_type:"api"`、`method`、`url` 等）；description 里注明只负责 HTTP 类型
  - `apipost_update_api` — POST `/open/apis/update`，body 透传；description 明示"非必填字段不传会被置默认值，调用前先 get details"
  - `apipost_delete_apis` — POST `/open/apis/delete`，body `{project_id, target_ids[]}`
- [x] 3.4 新建 `cases.go`：
  - `apipost_list_api_cases` — GET `/open/apis/sample`，query `project_id` + 可选 `target_ids` / `sample_ids`
  - `apipost_create_api_case` — POST `/open/apis/sample/create`

## 4. 注册与主程序接入

- [x] 4.1 新建 `internal/tools/apipost/register.go`：`Register(r *tools.Registry, client *client.Client, cfg *config.ApipostConfig)`，依次注册 9 个工具
- [x] 4.2 修改 `main.go`：`cfg.Apipost == nil` → stderr `apipost not configured; skipping apipost tools`；否则 `client := apipostclient.New(*cfg.Apipost)` + `apipost.Register(reg, client, cfg.Apipost)`
- [x] 4.3 启动日志打印 `apipost enabled: base_url=<url> project_name=<name> timeout=<d>`；MUST NOT 回显 `api_token`

## 5. 构建与静态检查

- [x] 5.1 `go build -o bin/mcp-server.exe .` 通过
- [x] 5.2 `go vet ./...` 无错误
- [x] 5.3 `go test ./internal/tools/apipost/... -count=1` 全通过

## 6. 手动验证（需要真 Token）

- [x] 6.1 填真实 Token 到 `config.yaml`（参考 `config.example.yaml` 新增 apipost 段注释）；启动 server
- [x] 6.2 `apipost_list_projects` 取到项目列表；记录至少一条 `project_id` / `project_code`
- [x] 6.3 `apipost_list_apis` 用上一步的 project_id 取接口简约列表
- [x] 6.4 `apipost_get_apis` 取一条接口完整详情
- [x] 6.5 `apipost_create_http_api` 建一条测试接口；用 `apipost_list_apis` 验证出现
- [x] 6.6 `apipost_update_api` 改名（需先 get 详情再基于它改）
- [x] 6.7 `apipost_list_api_cases` 看接口用例清单（使用上面创建的接口的 target_id）
- [x] 6.8 `apipost_delete_apis` 清掉测试接口
- [x] 6.9 错误路径：故意传错 `project_id` 触发 `code != 0`；断言 MCP 响应 `isError: true`、错误文本含 code/msg、不含 Token 明文

## 7. 文档

- [x] 7.1 `config.example.yaml` 新增 `apipost` 段示例（base_url 给官方云 + 自托管两个注释示例；api_token 用 `apt_xxxxx` 占位；project_name 用 `我的项目` 占位）
- [x] 7.2 README 新增"Apipost 工具"章节（与"数据库工具"平级）：
  - 配置字段表
  - 9 个工具清单（表格：name / method / endpoint / 关键参数）
  - "调用前先 get details 再 update" 的陷阱说明
  - Token 最小权限原则提示
- [x] 7.3 README "安全模型"章节补一段：Apipost Token 不进日志 / 错误；`project_name` 作用；响应大小上限
