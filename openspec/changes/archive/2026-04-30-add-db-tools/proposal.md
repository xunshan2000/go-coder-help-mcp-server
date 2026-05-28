## Why

上一轮 `restructure-for-multi-tools` 已把项目升级为可扩展的 `mcp-server`，当前只有 `add` 一个工具。真正的业务价值在把外部数据接进 LLM；最常见的诉求是"让 LLM 能够查我的数据库"。本 change 在新骨架下加入一组数据库访问工具，支持多数据源配置与读 / 读写权限切片，覆盖只读探索、读写业务操作、读写分离等常见场景。

架构上引入一个**驱动接口层**：不同 SQL 方言（MySQL、PostgreSQL、ClickHouse 等）在 DSN 组装、元信息查询、标识符转义、只读执行策略等方面差异显著，这些差异被 `driver.Driver` 接口统一约束。配置里的 `driver` 字段就是一个选择器，指向哪个已注册的实现。本 change 交付 MySQL 作为第一个实现，架构支持将来平滑加入 PostgreSQL / ClickHouse / SQLite，而工具层代码无需改动。

## What Changes

- 新增 `internal/tools/db/driver/` 子包：定义 `Driver` 接口、`ReadOnlyExec` 接口与表元信息结构体（`TableSchema` / `ColumnInfo` / `IndexInfo`），提供全局 `Register`/`Get`/`Names` 注册表
- 新增 `internal/tools/db/drivers/mysql/` 子包：MySQL 版 `Driver` 实现，`init()` 中自注册；blank import `github.com/go-sql-driver/mysql`
- 新增 `internal/tools/db/` 本身：`Pool`（按 driver 接口建立 `*sql.DB`）、SQL 守卫、4 个工具的 handler、render 辅助
- 引入 YAML 配置文件作为启动依赖：通过 `--config <path>` CLI flag 指定（默认 `./config.yaml`）
- 新增 `internal/config/` 子包负责解析 YAML、校验必填字段、与 `--config` flag 集成（不负责校验 "driver 是否受支持"，这部分在 Pool 构建时查注册表判定）
- 配置采用 **map + 拆分字段**（hyperf 风格）的结构，而非原始 DSN：
  - `databases` 顶层为 map，key 即数据源标识（LLM 调用时用 `source: "<key>"` 引用），value 为字段化配置
  - 字段包括 `driver` / `host` / `port` / `database` / `username` / `password` / `charset` / `collation` / `timezone` / `mode`，加一个可选 `params`（map，透传任何其它驱动特定参数）
  - 连接池参数在 `pool` 子段：`max_connections` / `min_connections` / `connect_timeout` / `max_idle_time`
  - DSN 由对应 driver 实现在内部组装，不对用户暴露
- 每个数据源有明确的 `mode`（`r` / `rw`）；权限校验在工具层 **+** 驱动提供的只读执行上下文（`BeginReadOnly`）双道执行
- 暴露 4 个 MCP 工具（都在 `db_` 前缀下）：
  - `db_list_tables`：通过 `driver.ListTables` 列出指定数据源下的全部表
  - `db_describe_table`：通过 `driver.DescribeTable` 返回结构信息
  - `db_query`：执行只读 SQL，`r` 源使用 `driver.BeginReadOnly` 提供的只读上下文
  - `db_execute`：执行写 SQL，仅对 `rw` 源开放
- 查询结果统一使用"列名数组 + 行值二维数组 + 是否截断标记"的结构返回，默认 max_rows = 100（全局可配）
- `main.go` 新增 `--config` flag 解析、加载配置、把配置与连接池传给 `db.Register`

## Capabilities

### New Capabilities
- `db-tools`: 数据库访问工具组（`db_list_tables` / `db_describe_table` / `db_query` / `db_execute`）、r/rw 权限模型、基于 `driver.Driver` 接口的驱动抽象层
- `app-config`: 应用级 YAML 配置文件加载、`--config` flag、多数据源字段化定义与校验

### Modified Capabilities
- `server-core`: `main.go` 的入口职责从"三步组装"扩展为"解析 --config → 加载配置 → 组装 server → 构建连接池 → 注册工具（传入配置与连接池）→ 启动 stdio"；`Requirement: 入口瘦身` 的禁止项需要相应放宽，允许 `main.go` 做 flag 解析与配置加载，但仍禁止出现具体工具业务逻辑

## Impact

- **代码**：新增 `internal/config/config.go`、`internal/tools/db/driver/`（接口 + 注册表）、`internal/tools/db/drivers/mysql/`（首个实现）、`internal/tools/db/`（pool / guard / tools / render）；`main.go` 增加 flag 解析与配置加载；`go.mod` 新增两项依赖（`gopkg.in/yaml.v3`、`github.com/go-sql-driver/mysql`）
- **运行时依赖**：启动需要一份 YAML 配置；配置缺失 / 解析失败 / 校验失败 / driver 未注册 / 任一 source Ping 不通 → 启动失败并打印清晰错误
- **安全**：`r` 模式使用首 token 白名单 + `driver.BeginReadOnly` 提供的只读上下文（MySQL 下即 `sql.TxOptions{ReadOnly: true}`）双重限制；`args` 用参数化占位符（`?`），禁止字符串拼接 SQL
- **文档**：README 增加"配置 config.yaml"、"数据库工具速览"、"安全模型"、"添加新驱动"四节
- **客户端**：`.mcp.json` 的 `args` 需加上 `["--config", "<绝对路径>"]`；升级时需同步
- **后续可扩展**：添加 PostgreSQL / ClickHouse / SQLite 驱动 = 新建 `drivers/<name>/` 子包实现 `Driver` 接口 + 在入口 blank import；不需要改工具层代码，也不需要 spec 层变更
