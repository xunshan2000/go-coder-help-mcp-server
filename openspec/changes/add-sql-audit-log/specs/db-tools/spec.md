## MODIFIED Requirements

### Requirement: Driver 接口约束
系统 SHALL 定义一个 `Driver` 接口，所有数据库驱动（MySQL、PostgreSQL、ClickHouse 等）MUST 通过实现该接口接入。工具层（`db_list_tables` / `db_describe_table` / `db_query` / `db_execute` 的 handler）MUST NOT 直接拼接任何特定方言的 SQL（如 `SHOW TABLES`、`SHOW FULL COLUMNS`）或硬编码任何方言特有的标识符转义字符；方言相关的所有行为 MUST 通过 Driver 接口委托给具体驱动实现。

接口至少包含以下方法：
- `Name() string`：驱动标识（匹配配置中 `driver` 字段）
- `SQLDriverName() string`：`sql.Open` 用的驱动名
- `BuildDSN(cfg) (string, error)`：字段化配置 → DSN 字符串
- `QuoteIdentifier(name string) string`：标识符转义
- `ListTables(ctx, *sql.DB) ([]string, error)`：列表用户表
- `DescribeTable(ctx, *sql.DB, table) (*TableSchema, error)`：返回结构信息
- `BeginReadOnly(ctx, *sql.DB) (ReadOnlyExec, error)`：提供只读执行上下文
- `RenderSQL(sql string, args []any) string`：把参数化 SQL 的 `?` 占位符替换为该驱动方言下的值字面量，供审计日志 / 人工分析使用；返回值 MUST NOT 作为 SQL 发送给数据库；在无法可靠渲染某些值 / 占位符与 args 个数不匹配等边界情况下 MAY 返回空字符串或包含说明注释的字符串，调用方 MUST 容忍这些情况

驱动实现 MUST 通过包级 `init()` 调用全局 `driver.Register(d)` 自注册。

#### Scenario: 工具层不含方言特定 SQL
- **WHEN** 审阅 `internal/tools/db/list_tables.go`、`describe_table.go`、`query.go`、`execute.go` 与 `register.go` 的源码
- **THEN** 这些文件 MUST NOT 出现 `SHOW TABLES`、`SHOW COLUMNS`、`SHOW INDEX`、`information_schema`、`pg_tables` 等任何具体方言的 SQL 字符串字面量
- **AND** 这些文件 MUST NOT 出现 `` "`" `` / `"\""` 等方言特有的标识符引号字面量

#### Scenario: 方言 SQL 仅存在于驱动实现包
- **WHEN** 审阅 `internal/tools/db/drivers/mysql/` 下的源码
- **THEN** 该包 MAY 出现 MySQL 方言 SQL（`SHOW TABLES`、`SHOW FULL COLUMNS FROM ...` 等）
- **AND** 其它任何 `internal/tools/db/` 下的非 driver 子包文件 MUST NOT 出现 MySQL 方言 SQL

#### Scenario: 添加新驱动不改动工具层
- **WHEN** 将来向仓库新增 `internal/tools/db/drivers/<newdriver>/` 并在入口 blank import
- **THEN** 工具层源文件（`list_tables.go` / `describe_table.go` / `query.go` / `execute.go` / `pool.go` / `guard.go`）MUST NOT 需要修改

#### Scenario: 所有已注册驱动实现 RenderSQL
- **WHEN** 审阅驱动实现包（当前至少 `drivers/mysql/`）
- **THEN** 每个 `driver.Driver` 的实现 MUST 提供 `RenderSQL(sql string, args []any) string` 方法
- **AND** 给定 SQL `"SELECT ? + ? AS s"` 与 args `[1, 2]`，MySQL 驱动的 `RenderSQL` MUST 返回一个人类可读字符串，其中两个 `?` 被替换为 `1` 与 `2`（具体字面量格式由驱动决定，但必须自洽）
- **AND** 返回值 MUST NOT 被用于向数据库发送 SQL

### Requirement: db_query 工具
系统 SHALL 注册名为 `db_query` 的 MCP 工具，接收必填参数 `source`（字符串）与 `sql`（字符串），可选参数 `args`（任意类型数组，用于参数化占位符）与 `max_rows`（正整数）。执行成功时 MUST 返回包含 `columns`、`rows`（二维数组）、`row_count` 与 `truncated` 字段的 JSON 文本。执行 SQL MUST 使用参数化占位符（`?`）传入 `args`，MUST NOT 使用字符串拼接。该工具对 `mode` 为 `r` 或 `rw` 的数据源均可用；`r` 源的执行路径 MUST 通过 `Driver.BeginReadOnly` 提供的 `ReadOnlyExec.QueryContext`，工具层负责在读取完成后调用 `Commit`，在出错时调用 `Rollback`；`rw` 源可直接通过 `*sql.DB.QueryContext` 执行。

每次 `db_query` 调用结束（无论成功或失败）MUST 按 `sql-audit` 能力约定写一条审计日志记录；审计日志写入失败 MUST NOT 影响本次调用返回给 MCP 客户端的响应内容与 isError 标记。

#### Scenario: 简单 SELECT
- **WHEN** 在 mode 为 `r` 或 `rw` 的 source 上调用 `db_query`，参数 `{"source": "primary", "sql": "SELECT 1 AS n"}`
- **THEN** 响应 MUST 为 `isError: false`
- **AND** 文本 JSON 的 `columns` MUST 等于 `["n"]`
- **AND** `rows` MUST 等于 `[[1]]`
- **AND** `row_count` MUST 等于 `1`
- **AND** `truncated` MUST 等于 `false`

#### Scenario: 参数化查询
- **WHEN** 调用 `db_query`，参数 `{"source": "primary", "sql": "SELECT ? + ? AS s", "args": [1, 2]}`
- **THEN** 响应文本 JSON 的 `rows` MUST 等于 `[[3]]`
- **AND** 实际发给数据库的语句 MUST 使用占位符绑定，MUST NOT 由工具端直接将字面值拼入 SQL 字符串

#### Scenario: 行数上限截断
- **WHEN** 全局 `max_rows` 为 `100`，调用 `db_query` 查询一张有 300 行的表且未指定 `max_rows` 参数
- **THEN** 返回的 `rows` MUST 有 `100` 行
- **AND** `row_count` MUST 等于 `100`
- **AND** `truncated` MUST 等于 `true`

#### Scenario: 调用者降低 max_rows
- **WHEN** 客户端调用 `db_query` 时显式指定 `max_rows: 10`，全局默认是 `100`
- **THEN** 返回的 `rows` 最多 `10` 行

#### Scenario: 调用者尝试抬高 max_rows
- **WHEN** 客户端调用 `db_query` 时显式指定 `max_rows: 10000`，全局默认是 `100`
- **THEN** 实际生效的上限 MUST 是全局 `max_rows`（`100`），不允许调用端抬高

#### Scenario: r 源上执行非只读语句
- **WHEN** 在 mode 为 `r` 的 source 上调用 `db_query`，参数 `{"source": "reporting", "sql": "UPDATE users SET email='x' WHERE id=1"}`
- **THEN** 响应 MUST 标记为错误
- **AND** 错误文本 MUST 指明该 source 仅允许只读语句
- **AND** 对底层数据库 MUST NOT 发出任何形式的 UPDATE 请求

#### Scenario: 多语句拒绝
- **WHEN** 调用 `db_query`，参数 `sql` 为 `"SELECT 1; SELECT 2"`
- **THEN** 响应 MUST 标记为错误
- **AND** 错误文本 MUST 指明禁止多语句

#### Scenario: 注释绕过 SELECT 白名单
- **WHEN** 调用 `db_query` 到 `r` 源，参数 `sql` 为 `"-- comment\n UPDATE users SET x=1"`
- **THEN** 响应 MUST 标记为错误（首 token 判定应先剥离前导 `--` / `/* */` 注释再取首 token，此处首 token 为 `UPDATE`）

#### Scenario: 调用写出一条审计日志
- **WHEN** 任意成功或失败的 `db_query` 调用完成
- **THEN** 系统 MUST 在 `./logs/sql-<今日>.log` 中追加一条对应的 JSONL 记录
- **AND** 记录 MUST 遵循 `sql-audit` 能力的字段结构

#### Scenario: 日志写失败不破坏响应
- **GIVEN** 日志文件所在目录不可写
- **WHEN** 客户端发起一次能正常到达数据库的 `db_query`
- **THEN** `db_query` 的 MCP 响应 MUST 为 `isError: false` 并包含正确的 `rows`

### Requirement: db_execute 工具
系统 SHALL 注册名为 `db_execute` 的 MCP 工具，接收必填参数 `source`（字符串）与 `sql`（字符串），可选参数 `args`（任意类型数组）。仅对 `mode` 为 `rw` 的数据源可用；`r` 模式的 source 上调用 MUST 被拒绝。成功时 MUST 返回包含 `rows_affected` 与 `last_insert_id` 字段的 JSON 文本。SQL MUST 通过参数化占位符传入 `args`，MUST NOT 使用字符串拼接。

每次 `db_execute` 调用结束（无论成功或失败）MUST 按 `sql-audit` 能力约定写一条审计日志记录；审计日志写入失败 MUST NOT 影响本次调用返回给 MCP 客户端的响应内容与 isError 标记。

#### Scenario: 在 rw 源上执行 INSERT
- **WHEN** 在 mode 为 `rw` 的 source 上调用 `db_execute`，参数为合法的 `INSERT ...` 语句（含 `args`）
- **THEN** 响应 MUST 为 `isError: false`
- **AND** 文本 JSON MUST 包含 `rows_affected`（非负整数）与 `last_insert_id`（非负整数）

#### Scenario: 在 r 源上调用 db_execute
- **WHEN** 在 mode 为 `r` 的 source 上调用 `db_execute` 携带任意 SQL
- **THEN** 响应 MUST 标记为错误
- **AND** 错误文本 MUST 指明该 source 为只读
- **AND** MUST NOT 对底层数据库发出任何修改请求

#### Scenario: db_execute 拒绝 SELECT 作为顶层语句
- **WHEN** 在 mode 为 `rw` 的 source 上调用 `db_execute`，`sql` 的首 token 为 `SELECT`、`SHOW`、`DESCRIBE`、`DESC` 或 `EXPLAIN`
- **THEN** 响应 MUST 标记为错误
- **AND** 错误文本 MUST 提示使用 `db_query` 代替 `db_execute` 来执行只读语句

#### Scenario: 调用写出一条审计日志
- **WHEN** 任意成功或失败的 `db_execute` 调用完成
- **THEN** 系统 MUST 在 `./logs/sql-<今日>.log` 中追加一条对应的 JSONL 记录
- **AND** 记录 MUST 遵循 `sql-audit` 能力的字段结构
