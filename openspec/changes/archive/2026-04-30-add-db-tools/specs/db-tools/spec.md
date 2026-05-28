## ADDED Requirements

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

### Requirement: BeginReadOnly 契约
`Driver.BeginReadOnly` MUST 返回一个 `ReadOnlyExec`，该接口至少包含 `QueryContext`、`Commit`、`Rollback` 三个方法。支持只读事务的驱动（例如 MySQL、PostgreSQL）MUST 在该方法内开启数据库事务（以 `ReadOnly: true` 或等价机制），事务 MUST 阻止任何数据变更；不支持只读事务的驱动（例如 ClickHouse、SQLite）MAY 返回对 `*sql.DB` 的直接包装，此时 `Commit` / `Rollback` MUST 为空操作（返回 `nil`），且驱动实现 MUST 在其 README / 文档中声明"该驱动的只读保证完全依赖最小权限账号"。

#### Scenario: MySQL 驱动的只读事务阻止写入
- **WHEN** 在 mode 为 `r` 的 MySQL source 上调用 `db_query`，参数 `sql` 为 `"SELECT * FROM users WHERE id=1 FOR UPDATE"`
- **THEN** 响应 MUST 标记为错误（MySQL 只读事务会拒绝 `FOR UPDATE`）
- **AND** 错误文本 MUST 指明底层数据库拒绝原因或"只读上下文下禁止加锁"

### Requirement: db_list_tables 工具
系统 SHALL 注册名为 `db_list_tables` 的 MCP 工具，接收必填参数 `source`（字符串，对应配置中 `databases` map 的 key），返回该数据源下所有用户表的名称列表。该工具对任意 `mode`（`r` / `rw`）的数据源均可用。实现 MUST 通过 `Driver.ListTables` 完成，工具层 MUST NOT 直接构造方言 SQL。

#### Scenario: 列出表
- **WHEN** 客户端调用 `tools/call` 工具 `db_list_tables`，参数 `{"source": "primary"}`，配置中 `primary` 指向一个含若干表的数据库
- **THEN** 响应 MUST 为 `isError: false` 的文本结果
- **AND** 文本内容 MUST 为合法 JSON 对象，包含键 `source`（值等于 `primary`）与键 `tables`（值为字符串数组，包含数据库中所有用户表的名称）

#### Scenario: source 不存在
- **WHEN** 客户端调用 `db_list_tables`，参数 `{"source": "does-not-exist"}`
- **THEN** 响应 MUST 标记为错误
- **AND** 文本内容 MUST 包含该 source 名称与"可用 source"的列表

### Requirement: db_describe_table 工具
系统 SHALL 注册名为 `db_describe_table` 的 MCP 工具，接收必填参数 `source`（字符串）与 `table`（字符串），返回该表的列定义与索引信息。该工具对任意 `mode` 的数据源均可用。实现 MUST 通过 `Driver.DescribeTable` 完成，工具层 MUST NOT 直接构造方言 SQL，也 MUST NOT 拼接 `table` 到任何 SQL 字符串（拼接由各驱动实现在其 `QuoteIdentifier` 保护下进行）。`table` 参数 MUST 先由工具层通过字符集白名单（至少允许 `[A-Za-z0-9_$]+`）预筛，非法字符立即返回错误。

#### Scenario: 描述表结构
- **WHEN** 客户端调用 `db_describe_table`，参数 `{"source": "primary", "table": "users"}`，`users` 表存在
- **THEN** 响应文本 MUST 为合法 JSON 对象
- **AND** 对象 MUST 包含键 `columns`，其值为对象数组，每个对象至少包含 `name`、`type`、`nullable` 字段
- **AND** 对象 MUST 包含键 `indexes`（可为空数组）

#### Scenario: 表不存在
- **WHEN** 客户端调用 `db_describe_table`，参数中的 `table` 在该 source 下不存在
- **THEN** 响应 MUST 标记为错误
- **AND** 错误文本 MUST 指明表不存在

#### Scenario: 非法 table 名
- **WHEN** 客户端调用 `db_describe_table`，参数 `table` 为 `"users; DROP TABLE x"` 或含空格、反引号、引号等字符
- **THEN** 响应 MUST 标记为错误（由工具层字符集白名单拦下，不触达驱动层）
- **AND** 数据库 MUST NOT 收到任何相关 SQL

### Requirement: db_query 工具
系统 SHALL 注册名为 `db_query` 的 MCP 工具，接收必填参数 `source`（字符串）与 `sql`（字符串），可选参数 `args`（任意类型数组，用于参数化占位符）与 `max_rows`（正整数）。执行成功时 MUST 返回包含 `columns`、`rows`（二维数组）、`row_count` 与 `truncated` 字段的 JSON 文本。执行 SQL MUST 使用参数化占位符（`?`）传入 `args`，MUST NOT 使用字符串拼接。该工具对 `mode` 为 `r` 或 `rw` 的数据源均可用；`r` 源的执行路径 MUST 通过 `Driver.BeginReadOnly` 提供的 `ReadOnlyExec.QueryContext`，工具层负责在读取完成后调用 `Commit`，在出错时调用 `Rollback`；`rw` 源可直接通过 `*sql.DB.QueryContext` 执行。

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

### Requirement: db_execute 工具
系统 SHALL 注册名为 `db_execute` 的 MCP 工具，接收必填参数 `source`（字符串）与 `sql`（字符串），可选参数 `args`（任意类型数组）。仅对 `mode` 为 `rw` 的数据源可用；`r` 模式的 source 上调用 MUST 被拒绝。成功时 MUST 返回包含 `rows_affected` 与 `last_insert_id` 字段的 JSON 文本。SQL MUST 通过参数化占位符传入 `args`，MUST NOT 使用字符串拼接。

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

### Requirement: 只读双重保护
对 `mode` 为 `r` 的数据源执行 `db_query` 时，系统 SHALL 同时做到两件事：(1) 工具层首 token 白名单校验（允许集合为 `SELECT` / `SHOW` / `DESCRIBE` / `DESC` / `EXPLAIN` / `WITH` / `VALUES`）；(2) 通过 `Driver.BeginReadOnly` 获取只读执行上下文，在该上下文中执行查询并在完成后 Commit 或 Rollback。即使白名单由于未知原因被绕过，驱动层的只读上下文（或最小权限账号兜底）也 MUST 阻止数据变更。

#### Scenario: 只读校验失败不会建立连接外的副作用
- **WHEN** 首 token 白名单判定失败（例如传入 `UPDATE ...`）
- **THEN** 系统 MUST 在将 SQL 发送到数据库之前返回错误
- **AND** 数据库服务器 MUST NOT 收到任何对应的请求

### Requirement: SQL 参数化传递
任何以 `sql` + `args` 形式传入的调用，`args` 中的每个元素 SHALL 作为参数化占位符的值由底层驱动绑定，系统 MUST NOT 将 `args` 中的值格式化 / 拼接入 SQL 字符串。

#### Scenario: 字符串参数不被拼接
- **WHEN** 调用 `db_query`，参数 `{"source": "primary", "sql": "SELECT ? AS v", "args": ["'; DROP TABLE users; --"]}`
- **THEN** 响应 MUST 为 `isError: false`
- **AND** 返回的 `rows` MUST 等于 `[["'; DROP TABLE users; --"]]`
- **AND** `users` 表 MUST NOT 因此被删除

### Requirement: 查询超时
系统 SHALL 对每次 `db_query` / `db_execute` 的底层 SQL 调用施加由配置 `defaults.query_timeout` 指定的超时（缺省 30 秒）。超时发生时 MUST 取消底层查询并返回标记为错误的响应；进程 MUST NOT 崩溃。

#### Scenario: 超时触发
- **WHEN** `query_timeout` 配置为 `2s`，调用 `db_query` 参数 `sql` 为一个将执行 10 秒的查询（例如 MySQL 下的 `SELECT SLEEP(10)`）
- **THEN** 约 2 秒后响应 MUST 标记为错误
- **AND** 错误文本 MUST 指明超时
- **AND** 进程 MUST 继续存活并能响应下一个请求
