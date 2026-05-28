## ADDED Requirements

### Requirement: YAML 配置文件作为启动依赖
系统 SHALL 在启动时加载一份 YAML 格式的配置文件；配置文件路径 SHALL 由 `--config <path>` 命令行 flag 指定，缺省值为当前工作目录下的 `./config.yaml`。若配置文件不存在、无法解析、或校验未通过，进程 MUST 以非零退出码立即退出，并在 stderr 打印人类可读的错误原因；MUST NOT 以任何"空配置"形式继续运行。

#### Scenario: 显式指定配置路径
- **WHEN** 使用 `./mcp-server --config /abs/path/config.yaml` 启动，且该文件存在、可读、内容合法
- **THEN** 进程 MUST 成功启动，后续的 MCP 请求能访问配置中声明的数据源

#### Scenario: 省略 --config 使用默认路径
- **WHEN** 不传 `--config`，进程启动时当前工作目录存在合法的 `./config.yaml`
- **THEN** 进程 MUST 成功启动

#### Scenario: 配置缺失
- **WHEN** 进程启动时指定的 `--config` 路径不存在，或默认 `./config.yaml` 不存在且未指定 `--config`
- **THEN** 进程 MUST 以非零退出码退出
- **AND** stderr MUST 输出指明"配置文件不存在"的错误信息，并包含尝试加载的路径

#### Scenario: 配置解析失败
- **WHEN** 配置文件存在但不是合法的 YAML，或字段类型与 schema 不符
- **THEN** 进程 MUST 以非零退出码退出
- **AND** stderr MUST 输出定位到行号或字段名的错误信息

### Requirement: 多数据源字段化配置
配置文件 `databases` 顶层 SHALL 为 YAML map，map 的 key 即数据源标识符（后续工具调用通过 `source: "<key>"` 引用）。每个数据源 value MUST 包含字段 `driver`、`host`、`database`、`username`、`mode`；字段 `port`、`password`、`charset`、`collation`、`timezone`、`params`、`pool` 为可选。`mode` 的取值 MUST 为 `r` 或 `rw` 之一；`driver` 字段 MUST 非空（运行时是否受支持由"驱动注册校验"需求决定）；map key（source 标识符）MUST 非空；校验失败 MUST 立即使进程启动失败。

#### Scenario: 合法的多源字段化配置
- **WHEN** 配置 `databases` 下含 `primary: {driver: mysql, host: ..., database: app, username: app, password: ..., mode: rw, ...}` 与 `reporting: {driver: mysql, host: ..., database: app, username: ro, password: ..., mode: r, ...}`
- **THEN** 启动成功
- **AND** 工具调用时传入 `source: "primary"` 或 `source: "reporting"` 可分别访问对应数据源

#### Scenario: 缺失必填字段
- **WHEN** 配置中某数据源缺少 `host`（或 `database` / `username` / `mode` / `driver` 中的任一项）
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明出错的 source key 与缺失的字段名

#### Scenario: map key 为空字符串
- **WHEN** 配置中出现 `databases: { "": {driver: mysql, ...} }`
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明"source key 不能为空"

#### Scenario: 非法的 mode 值
- **WHEN** 某数据源的 `mode` 被写成 `readonly`、`w`、空字符串等非 `r/rw` 值
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明非法值与所在 source key，且 MUST 明示允许取值集合为 `{r, rw}`

#### Scenario: 空 databases
- **WHEN** 配置中 `databases` 为空 map 或完全省略
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明"至少需要一个数据源"

#### Scenario: 省略 port 使用驱动缺省值（MySQL）
- **WHEN** 数据源 `driver: mysql` 配置省略 `port` 字段
- **THEN** 进程 MUST 成功启动
- **AND** 实际连接使用的端口 MUST 为 `3306`（MySQL 驱动的缺省端口）

#### Scenario: 省略 timezone 使用驱动缺省值（MySQL）
- **WHEN** 数据源 `driver: mysql` 配置省略 `timezone` 字段
- **THEN** 进程 MUST 成功启动
- **AND** MySQL 驱动组装的 DSN MUST 包含 `loc=UTC`

### Requirement: 驱动注册校验
系统 SHALL 在启动构建连接池阶段，对每个数据源的 `driver` 字段查询驱动注册表。若该 `driver` 未在注册表中登记（包括未编译进当前二进制的驱动），进程 MUST 以非零退出码立即退出，错误信息 MUST 同时包含：所在 source key、实际填入的 driver 名称、当前已注册的全部驱动名列表（排序后）。此校验 MUST 发生在任何网络连接尝试之前。

#### Scenario: 使用已注册驱动
- **WHEN** 某数据源 `driver: mysql`，且当前二进制已链接 MySQL 驱动实现
- **THEN** 驱动注册校验 MUST 通过，进入后续 DSN 组装与 Ping 流程

#### Scenario: 使用未注册驱动
- **WHEN** 某数据源 `driver: pgsql`，但当前二进制未链接 PostgreSQL 驱动实现
- **THEN** 进程 MUST 在任何网络连接尝试之前以非零退出码退出
- **AND** 错误信息 MUST 包含所在 source key、字符串 `pgsql`、以及当前已注册驱动名的列表
- **AND** 错误信息 MUST NOT 包含该 source 的 `password` 或组装后 DSN

### Requirement: 透传参数 params
每个数据源 SHALL 支持可选的 `params` 字段（`map[string]string`），其中的键值对由对应驱动的 `BuildDSN` 实现决定如何翻译到 DSN 中（MUST 原样保留用户填入的值）；`params` 中的键 MUST NOT 允许 `multiStatements=true`（不区分大小写）。校验失败 MUST 立即使进程启动失败。

#### Scenario: 透传正常键值（MySQL 驱动下）
- **WHEN** 数据源 `driver: mysql`，`params` 含 `{tls: "skip-verify", sql_mode: "STRICT_ALL_TABLES"}`
- **THEN** 启动成功
- **AND** 该 source 建立的 MySQL 连接的 session `@@sql_mode` MUST 包含 `STRICT_ALL_TABLES`

#### Scenario: 禁止开启 multiStatements
- **WHEN** 数据源的 `params` 含 `multiStatements: "true"` 或 `MULTISTATEMENTS: "true"`
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明"禁止在 params 中开启 multiStatements"

#### Scenario: params 与字段派生键冲突（驱动决定语义）
- **WHEN** 数据源同时设置了 `charset: utf8mb4` 与 `params: { charset: "latin1" }`
- **THEN** 该驱动的 `BuildDSN` 实现 MUST 遵循"用户 params 优先于字段派生默认值"的约定，最终生效 `charset=latin1`
- **AND** 进程不得因此失败

### Requirement: 敏感信息脱敏
系统 MUST NOT 在任何日志、启动信息或错误输出中打印 `password` 字段的内容、`params` 中的键值、或完整组装后的 DSN 字符串。启动成功后打印的每个 source 信息 SHALL 至多包含 `key`、`driver`、`mode`、`host:port`、`database`。

#### Scenario: 启动日志不泄漏密码
- **WHEN** 某数据源配置 `password: "hunter2"` 并成功启动
- **THEN** 进程启动过程中 stderr 输出 MUST NOT 包含字符串 `hunter2`
- **AND** stderr 输出 MUST NOT 包含组装后的 DSN（形如 `user:pass@tcp(...)/...`）

#### Scenario: Ping 失败的错误信息不泄漏密码
- **WHEN** 某数据源字段组合指向不可达地址、启动 Ping 失败
- **THEN** 返回 / 打印的错误信息 MUST 包含 source key
- **AND** 错误信息 MUST NOT 包含 `password` 字段的值或组装后的 DSN 字符串

### Requirement: 连接池配置
每个数据源 SHALL 支持可选的 `pool` 子段，其字段包括 `max_connections`、`min_connections`、`connect_timeout`、`max_idle_time`。缺省值：`max_connections=10`、`min_connections=0`、`connect_timeout=10s`、`max_idle_time=0`（不限）。系统 MUST 将这些字段按以下映射应用到 `*sql.DB`：`max_connections → SetMaxOpenConns`、`min_connections → SetMaxIdleConns`、`max_idle_time → SetConnMaxIdleTime`；`connect_timeout` MUST 同时作为启动 Ping 的 ctx 超时，并由驱动 `BuildDSN` 按需翻译进 DSN（MySQL 驱动下映射为 `timeout=<dur>` 参数）。

#### Scenario: 使用默认连接池参数
- **WHEN** 数据源完全省略 `pool` 子段
- **THEN** 进程启动成功
- **AND** 对应 `*sql.DB` 的 MaxOpenConns MUST 为 `10`、MaxIdleConns MUST 为 `0`

#### Scenario: 显式 pool 字段（MySQL 驱动下）
- **WHEN** 数据源 `driver: mysql` 配置 `pool: {max_connections: 20, min_connections: 5, connect_timeout: 5s, max_idle_time: 2m}`
- **THEN** 对应 `*sql.DB` 的 MaxOpenConns MUST 为 `20`
- **AND** MaxIdleConns MUST 为 `5`
- **AND** ConnMaxIdleTime MUST 为 `2` 分钟
- **AND** 启动 Ping 的超时 MUST 为 `5s`
- **AND** MySQL 驱动组装的 DSN MUST 包含 `timeout=5s`

### Requirement: 全局默认值
配置文件 SHALL 支持顶层 `defaults` 段，其中至少包括 `max_rows`（查询行数上限）与 `query_timeout`（单次 SQL 超时时长）。缺省 `max_rows` SHALL 为 `100`；缺省 `query_timeout` SHALL 为 `30s`。若字段被显式写入且为合法正整数 / 时长字符串，则以配置值生效。

#### Scenario: 未提供 defaults
- **WHEN** 配置中完全省略 `defaults` 段
- **THEN** 进程 MUST 启动成功，且后续 `db_query` 使用 `max_rows=100`、`query_timeout=30s` 的缺省值

#### Scenario: 覆盖 max_rows
- **WHEN** 配置中 `defaults.max_rows` 为 `500`
- **THEN** 后续 `db_query` 在未显式指定 `max_rows` 参数时使用上限 `500`

#### Scenario: 非法 max_rows
- **WHEN** 配置中 `defaults.max_rows` 为 `0`、负数或非整数
- **THEN** 进程 MUST 启动失败
- **AND** 错误信息 MUST 指明 `max_rows` 必须是正整数

### Requirement: 启动时连通性验证
对每个配置中通过驱动注册校验的数据源，系统 SHALL 在启动阶段执行一次 `sql.Open` + `PingContext`（Ping 超时使用该源的 `pool.connect_timeout`）。任一数据源 Ping 失败 MUST 导致进程以非零退出码退出；错误信息 MUST 指明失败的 source key，且 MUST 遵循"敏感信息脱敏"要求。

#### Scenario: 所有数据源可达
- **WHEN** 所有已配置 source 的 Ping 均成功
- **THEN** 进程启动成功，stderr 输出每个 source 的 key / driver / mode / host:port / database 的启动日志（不含 DSN / password）

#### Scenario: 某数据源 Ping 失败
- **WHEN** 配置中某个 source 的字段组合指向一个不可达的数据库实例
- **THEN** 进程 MUST 启动失败，退出码非 0
- **AND** stderr 错误信息 MUST 包含失败的 source key
