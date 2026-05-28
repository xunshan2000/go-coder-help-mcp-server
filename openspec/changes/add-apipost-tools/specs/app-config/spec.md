## ADDED Requirements

### Requirement: apipost 顶层段可选性
配置文件 SHALL 支持可选的 `apipost` 顶层段。若该段完全缺失，进程 MUST 启动成功，仅在 stderr 打印一行 `apipost not configured; skipping apipost tools`，并跳过所有 `apipost_*` 工具的注册；若该段存在，则 MUST 通过所有 apipost 字段校验（见后续 Requirement），任一字段非法 MUST 立即使进程启动失败。

#### Scenario: 完全省略 apipost 段
- **WHEN** `config.yaml` 中不含 `apipost` 顶层键
- **THEN** 进程 MUST 启动成功
- **AND** stderr MUST 包含一行 `apipost not configured; skipping apipost tools`

#### Scenario: apipost 段字段不全
- **WHEN** `config.yaml` 含 `apipost: {base_url: https://x}`（缺 api_token / project_name）
- **THEN** 进程 MUST 启动失败，退出码非 0
- **AND** 错误信息 MUST 指明缺失的具体字段名

### Requirement: apipost.base_url 必填且格式合法
`apipost.base_url` 字段 MUST 为非空字符串，MUST 以 `http://` 或 `https://` 开头，且 MUST 能被 `net/url` 解析为合法 URL。

#### Scenario: 合法 base_url
- **WHEN** `apipost.base_url` 为 `https://v2-openapi.apipost.net`
- **THEN** 启动成功，后续 HTTP 请求以该 URL 为基础拼接 path

#### Scenario: base_url 缺失协议前缀
- **WHEN** `apipost.base_url` 为 `v2-openapi.apipost.net`
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明 `apipost.base_url` 必须以 `http://` 或 `https://` 开头

### Requirement: apipost.api_token 必填与脱敏
`apipost.api_token` 字段 MUST 为非空字符串。Token 属于敏感信息，系统 MUST NOT 在任何日志 / 启动信息 / 错误输出 / MCP 响应中回显 Token 明文；启动日志打印 apipost 就绪信息时 MUST 以 `***` 或等价掩码代替 Token 字段。

#### Scenario: 合法 token 成功启动
- **WHEN** `apipost.api_token` 为 `apt_realsecret1234567890abcdef`
- **THEN** 启动成功
- **AND** stderr 启动日志 MUST NOT 包含字符串 `apt_realsecret1234567890abcdef`

#### Scenario: token 为空
- **WHEN** `apipost.api_token` 为空字符串或字段缺失
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明 `apipost.api_token` 必填

### Requirement: apipost.project_name 必填
`apipost.project_name` 字段 MUST 为非空字符串。Apipost 开放接口 V2 的全局 Header 集合中 `project_name` 为必填项，所有端点调用时都会注入；缺失或空 MUST 使进程启动失败。

#### Scenario: project_name 为空
- **WHEN** `apipost` 段存在但 `project_name` 字段缺失或为空字符串
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明 `apipost.project_name` 必填

### Requirement: 默认 team_id 与 project_id
`apipost` 段 SHALL 支持可选字段 `team_id` 与 `project_id`；若提供则作为工具调用的默认上下文（工具参数未显式指定时使用）。两字段取值若非空 MUST 匹配正则 `^[A-Za-z0-9_-]+$`；不匹配 MUST 立即使进程启动失败。

#### Scenario: 省略默认上下文
- **WHEN** `apipost` 段不含 `team_id` 与 `project_id`
- **THEN** 启动成功
- **AND** 调用需要 `project_id` 的工具时 LLM MUST 显式传入

#### Scenario: 合法默认 project_id 生效
- **WHEN** `apipost.project_id` 为 `prj_abc_123`，LLM 调用 `apipost_list_apis` 未传 `project_id`
- **THEN** 工具 MUST 使用 `prj_abc_123` 作为请求参数

#### Scenario: 非法默认值
- **WHEN** `apipost.project_id` 为 `"../bad"` 或含空格
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明 `apipost.project_id` 格式非法

### Requirement: apipost.request_timeout 默认值与合法性
`apipost.request_timeout` SHALL 支持可选 Go duration 字符串。缺省值为 `30s`。若显式配置值非合法 duration 或小于等于 `0`，进程 MUST 启动失败。

#### Scenario: 使用默认 timeout
- **WHEN** `apipost.request_timeout` 字段缺失
- **THEN** 启动成功，运行时使用 `30s` 作为单次 HTTP 超时

#### Scenario: 非法 timeout
- **WHEN** `apipost.request_timeout: "0s"` 或 `"-5s"` 或 `"abc"`
- **THEN** 进程 MUST 启动失败

### Requirement: apipost.max_response_bytes 默认值与合法性
`apipost.max_response_bytes` SHALL 支持可选正整数。缺省值为 `10485760`（10 MiB）。若显式配置非正整数或 `0`，进程 MUST 启动失败。

#### Scenario: 使用默认上限
- **WHEN** `apipost.max_response_bytes` 字段缺失
- **THEN** 启动成功，运行时单次响应体读取上限为 10485760 字节

#### Scenario: 非法值
- **WHEN** `apipost.max_response_bytes: 0` 或 `-1`
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明 `apipost.max_response_bytes` 必须是正整数
