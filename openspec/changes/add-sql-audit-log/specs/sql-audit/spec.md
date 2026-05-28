## ADDED Requirements

### Requirement: 审计日志文件位置与命名
系统 SHALL 在每次 `db_query` 或 `db_execute` 调用完成后，写一条审计记录到 `./logs/sql-YYYY-MM-DD.log` 文件，其中 `YYYY-MM-DD` 为调用完成时本地时区下的日期。目录 `./logs/` 不存在时 MUST 在首次写入前自动创建（权限 `0755`）。文件以 append 模式打开，权限 `0640`。相对路径解析基准为进程启动时的工作目录（cwd）。

#### Scenario: 首次写入自动创建目录与文件
- **WHEN** `./logs/` 目录不存在，MCP 客户端首次触发 `db_query` 调用
- **THEN** 系统 MUST 创建 `./logs/` 目录
- **AND** MUST 在其中创建 `sql-<今日>.log` 文件
- **AND** 该文件 MUST 包含一条与该 `db_query` 对应的 JSONL 记录

#### Scenario: 同日多次调用 append
- **WHEN** 同一本地日期内产生 N 条 `db_query` / `db_execute` 调用
- **THEN** `./logs/sql-<今日>.log` MUST 包含 N 行记录
- **AND** 每行 MUST 为合法 JSON
- **AND** 每条记录 MUST 以单个 `\n` 结尾

### Requirement: 按本地日期滚动
系统 SHALL 在调用的本地日期与当前打开文件对应日期不一致时，自动关闭当前文件并打开新日期对应的新文件。切换 MUST 在写入该调用对应的记录**之前**完成，使得这条记录写入正确的新文件。

#### Scenario: 跨日第一条日志落入新文件
- **GIVEN** 当前打开文件是 `sql-2026-04-30.log`
- **WHEN** 系统时钟走到本地日期 `2026-05-01` 后发生第一次 `db_query`
- **THEN** 系统 MUST 关闭 `sql-2026-04-30.log`
- **AND** MUST 创建 / 打开 `sql-2026-05-01.log`
- **AND** 这次调用的 JSONL 记录 MUST 出现在 `sql-2026-05-01.log` 而不是前一天的文件

### Requirement: JSONL 记录结构
每条记录 SHALL 为一行合法 UTF-8 JSON 对象，紧跟单个 `\n`。字段按以下约定：

**始终存在**：
- `ts`: string, 本地时区下的 `YYYY-MM-DD HH:MM:SS`（零填充、秒级精度、无时区后缀；例如 `"2026-04-30 17:59:06"`）
- `source`: string, 数据源 key（获取不到时为 `""`）
- `mode`: string, `"r"` / `"rw"` / `""`（source 未命中时）
- `tool`: string, `"db_query"` 或 `"db_execute"`
- `sql`: string, 用户原始 SQL（含 `?` 占位符；缺失时为 `""`）
- `sql_rendered`: string, args 内联后的可读形式（驱动 `RenderSQL` 返回值；失败或不可得时 `""`）
- `args`: JSON 数组, 占位符绑定值（`null` / 数字 / 字符串 / 数组 / 对象；`[]byte` 按 Go 默认 base64 编码）；缺失时为 `[]` 或 `null`
- `duration_ms`: integer, handler 总耗时
- `ok`: boolean, 是否未标记 isError 成功返回

**成功时附加**（根据 tool）：
- 当 `tool == "db_query"`: `rows` (integer, 实际返回行数), `truncated` (boolean)
- 当 `tool == "db_execute"`: `rows_affected` (integer), `last_insert_id` (integer)

**失败时附加**：
- `err`: string, 单行错误摘要（MCP 响应中返回给客户端的错误文本截取；无换行）

#### Scenario: 成功 db_query 的记录形态
- **WHEN** 客户端调用 `db_query` 成功，`source="default"`、`mode="r"`、返回 5 行、未截断、耗时 42ms
- **THEN** 对应 JSONL 行 MUST 同时包含字段 `ts`, `source="default"`, `mode="r"`, `tool="db_query"`, `sql`, `sql_rendered`, `args`, `duration_ms=42`, `rows=5`, `truncated=false`, `ok=true`
- **AND** MUST NOT 包含 `err` 字段
- **AND** MUST NOT 包含 `rows_affected` / `last_insert_id` 字段

#### Scenario: 失败 db_query 的记录形态
- **WHEN** 客户端在 `r` source 上调用 `db_query` 传入 UPDATE 语句，被工具层首 token 白名单拒绝
- **THEN** 对应 JSONL 行 MUST 包含 `ok=false` 与 `err` 字段
- **AND** `sql` MUST 为用户输入的 UPDATE 语句原文
- **AND** MUST NOT 包含 `rows` / `truncated` 字段

#### Scenario: 成功 db_execute 的记录形态
- **WHEN** 客户端在 `rw` source 上调用 `db_execute` 插入 1 行成功
- **THEN** 对应 JSONL 行 MUST 包含 `tool="db_execute"`, `rows_affected=1`, `last_insert_id`（为 driver 返回的非负整数）, `ok=true`
- **AND** MUST NOT 包含 `rows` / `truncated`

### Requirement: 并发写入安全
系统 SHALL 保证多个 MCP 请求并发触发 `db_query` / `db_execute` 时，写入日志文件的 JSON 行 MUST 不出现交错（即每条完整 JSON + `\n` 作为原子追加）。实现 MAY 使用互斥锁 / 单 writer goroutine 等任何机制。

#### Scenario: 并发请求下每行完整
- **WHEN** 10 个并发 goroutine 同时通过 mcp-go 调度 `db_query`
- **THEN** 对应日志文件中每一行 MUST 是合法完整的 JSON
- **AND** 文件中每一行 MUST 可被 `json.Unmarshal` 成功解析

### Requirement: 日志写入失败不破坏调用
当日志文件创建 / 写入因任何原因失败（磁盘满、权限拒绝、文件系统只读等）时，`db_query` / `db_execute` 的 MCP 响应 MUST NOT 被影响 —— 工具层 MUST 仍返回正常的执行结果或错误响应。系统 MAY 将日志失败信息输出到 stderr 一行告警，但 stderr 告警 MUST NOT 包含本次调用的 `sql` / `args` 内容，避免重复泄漏。

#### Scenario: logs 目录只读
- **GIVEN** `./logs/` 目录存在但对进程不可写
- **WHEN** 客户端发起一次 `db_query` 且查询能正常到达数据库并返回结果
- **THEN** `db_query` 的 MCP 响应 MUST 为 `isError: false` 且包含正确的 `rows`
- **AND** stderr MAY 输出一行日志写入失败告警
- **AND** 该告警 MUST NOT 包含查询 SQL 文本 / args 值

### Requirement: 进程退出时 flush
进程收到 SIGINT / SIGTERM / stdin 关闭等正常退出信号时，系统 SHALL 在退出前 flush 并关闭当前打开的日志文件。非异常退出（panic / kill -9）不在本需求约束内。

#### Scenario: 正常退出
- **WHEN** MCP 客户端关闭 stdin 导致进程正常退出
- **THEN** 日志文件内所有已 `Write` 的记录 MUST 落盘
- **AND** 文件 MUST 被关闭（OS 句柄释放）

### Requirement: 日志文件的版本控制忽略
仓库的 `.gitignore` MUST 包含 `/logs/` 目录忽略规则，防止审计日志（含敏感业务 SQL 与参数值）被意外 commit。

#### Scenario: gitignore 规则
- **WHEN** 审阅 `.gitignore`
- **THEN** 文件 MUST 包含忽略 `/logs/` 目录的条目（可为 `/logs/` 或等价写法）
