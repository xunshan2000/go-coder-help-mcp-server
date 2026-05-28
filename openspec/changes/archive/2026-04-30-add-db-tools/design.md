## Context

`restructure-for-multi-tools` 已交付一个支持多工具的 `mcp-server` 骨架。本 change 在此骨架上加入数据库访问能力：同时暴露多个数据源（读写分离场景下一个 master + 多个 replica），按源的 `mode` 决定 LLM 能做什么，保证任何 SQL 都通过参数化占位符执行。

不同 SQL 方言在连接字符串、元信息查询、标识符转义、只读执行方式等方面差异显著（例如 ClickHouse 没有传统事务，MySQL 用反引号转义，PostgreSQL 用双引号）。本 change 用一个小而干净的 `Driver` 接口覆盖这些差异点，把工具层（handler）与驱动实现彻底解耦。第一版只落地 MySQL 实现，但接口设计需要允许将来无改工具层地接入 PostgreSQL / ClickHouse / SQLite。

关键约束：
- LLM 产生的 SQL 不可完全信任，`r` 模式必须**双重**防线（工具层白名单 + 驱动层只读上下文）
- 只读事务这个假设不能写死到工具层 —— ClickHouse 不支持事务；接口要让驱动自己决定怎么"开只读上下文"
- MCP 客户端（Claude Code）响应面是上下文窗口，行数大量返回会撑爆
- 当前已有 `openspec/specs/server-core/spec.md` 里 `Requirement: 入口瘦身` 明确禁止 `main.go` 出现"工具参数解析、业务计算"；本 change 需要扩展其外延（允许 flag 解析 + 配置加载）
- 用户明确要求配置风格参考 hyperf：字段粒度（host / port / database / username / password / charset / collation / pool 子段）而非原始 DSN，便于运维

## Goals / Non-Goals

**Goals:**
- 定义 `driver.Driver` 接口约束所有驱动必须提供的能力（DSN 组装、标识符转义、列出表、描述表、只读执行上下文）；工具层只依赖接口，不出现任何数据库方言的字符串
- 用 `driver.Register` 全局注册表 + 各驱动 `init()` 自注册的方式装配驱动；添加新驱动 = 新建实现包 + 入口 blank import
- 交付 MySQL 驱动作为第一个实现，覆盖接口全部方法
- 新增 4 个数据库相关 MCP 工具（list_tables / describe_table / query / execute）
- 引入 YAML 配置文件 + `--config` flag，配置结构为 hyperf 风格（`databases` 顶层 map、每源字段拆分、pool 子段）
- 数据源权限 `r` 与 `rw` 两档；`r` 拒绝非只读语句并通过驱动的只读上下文兜底；`rw` 放行全部
- 所有 SQL 参数通过 `?` 占位符传入，禁止字符串拼接
- 查询结果以"列名数组 + 二维行数组 + truncated 标记"结构返回，max_rows 限制（默认 100，调用端只能降不能升）
- 启动时严格校验配置；driver 未注册时在 Pool 启动阶段失败

**Non-Goals:**
- 不实现 PostgreSQL / ClickHouse / SQLite 驱动（架构已留位，实现留给后续 change）
- 不实现连接池的动态重配置 / 热重载
- 不实现查询执行计划可视化 / SQL 格式化
- 不做 SQL 防注入的语法级解析（白名单是"首 token 级"粗筛，真正防护靠参数化占位符 + 驱动只读上下文）
- 不做 multi-statement 支持（内部 DSN 不开启 `multiStatements=true`）
- 不做数据变更审计日志（stdio 传输下 `log.Printf` 易污染 JSON-RPC；留给专门 change）
- 不做事务跨多次工具调用
- 不保留 `dsn` 原始字段直填（简化：单一配置路径，避免字段交叉）
- 不实现 `w`（只写）模式；第一版简化为 `r` / `rw` 二分
- 不包含 `prefix`（表前缀）与 ORM 相关字段；本项目工具是原生 SQL 转发，无表名改写
- 不实现 hyperf swoole 专有的 `wait_timeout` / `heartbeat`（Go `database/sql` 无原生对应，由 ctx 超时与 `SetConnMaxLifetime` 替代）

## Decisions

### 1. 配置模型：YAML + `--config` flag + 字段拆分 + map 结构

配置文件示例：

```yaml
# config.yaml
defaults:
  max_rows: 100          # db_query 默认与上限，调用端不得抬高
  query_timeout: 30s     # 单次 SQL 超时

databases:
  # map key 即数据源标识。LLM 调用 db_query 时 source="primary" 引用它。
  primary:
    driver: mysql
    host: 127.0.0.1
    port: 3306
    database: app           # 库名
    username: app
    password: secret
    charset: utf8mb4
    collation: utf8mb4_unicode_ci
    timezone: UTC           # 驱动层按需翻译（MySQL 下映射为 loc 参数）
    mode: rw                # r | rw
    params:                 # 可选：透传其它驱动特定参数，由各驱动的 BuildDSN 处理
      parseTime: "true"
      tls: skip-verify
    pool:
      max_connections: 10   # → SetMaxOpenConns
      min_connections: 2    # → SetMaxIdleConns
      connect_timeout: 10s  # → 启动 Ping 超时 + 由驱动转译入 DSN（MySQL 下为 timeout 参数）
      max_idle_time: 60s    # → SetConnMaxIdleTime

  reporting:
    driver: mysql
    host: replica.example.com
    port: 3306
    database: app
    username: ro
    password: ro_secret
    mode: r
    pool:
      max_connections: 5
      min_connections: 1
```

字段必填 / 可选（所有驱动通用，驱动特有语义下面讨论）：

| 字段 | 必填 | 缺省值 | 说明 |
|---|---|---|---|
| `driver` | 是 | — | 运行时由 `driver.Names()` 返回的已注册驱动集合决定 |
| `host` | 是 | — | |
| `port` | 否 | 驱动提供 | MySQL 驱动缺省 `3306` |
| `database` | 是 | — | 库名 |
| `username` | 是 | — | |
| `password` | 否 | `""` | |
| `charset` | 否 | 驱动提供 | MySQL 驱动缺省 `utf8mb4` |
| `collation` | 否 | 驱动按 charset 默认 | |
| `timezone` | 否 | 驱动提供 | MySQL 驱动缺省 `UTC` |
| `mode` | 是 | — | `r` 或 `rw` |
| `params` | 否 | `{}` | |
| `pool.max_connections` | 否 | `10` | |
| `pool.min_connections` | 否 | `0` | |
| `pool.connect_timeout` | 否 | `10s` | |
| `pool.max_idle_time` | 否 | `0` | |

启动：`./bin/mcp-server.exe --config ./config.yaml`

- 配置缺失 / 解析失败 / 校验失败 → 立即退出
- 启动日志只打印 source key + driver + mode + host:port + database，MUST NOT 打印 password / params / 组装后 DSN

**Alternatives considered**：
- 保留 `dsn` 字段作为逃生口：`params` 已能覆盖；两条路径心智成本大。拒绝
- `databases` 仍用 list：与 hyperf 风格不一致，且 `name` 字段易与 `database`（库名）混淆。拒绝

### 2. 驱动接口抽象（本次设计最核心的点）

所有驱动实现一个最小接口：

```go
// internal/tools/db/driver/driver.go
package driver

import (
    "context"
    "database/sql"

    "example.com/mcp-server/internal/config"
)

type Driver interface {
    // Name 返回驱动标识，与配置中 driver 字段匹配，如 "mysql" / "pgsql" / "clickhouse"
    Name() string

    // SQLDriverName 返回 sql.Open 用的 driverName
    // （通常等于 Name()，但某些驱动注册名不同，如 pgx 驱动注册为 "pgx"）
    SQLDriverName() string

    // BuildDSN 把字段化的 SourceConfig 组装成驱动识别的 DSN 字符串
    // 实现 MUST NOT 把 password / 组装后 DSN 写到任何共享日志里
    BuildDSN(cfg config.SourceConfig) (string, error)

    // QuoteIdentifier 按该驱动的语法转义标识符（表名 / 列名）
    // MySQL: 反引号；PostgreSQL: 双引号
    QuoteIdentifier(name string) string

    // ListTables 列出 database 下所有用户表
    ListTables(ctx context.Context, db *sql.DB) ([]string, error)

    // DescribeTable 返回表结构（列 + 索引）
    DescribeTable(ctx context.Context, db *sql.DB, table string) (*TableSchema, error)

    // BeginReadOnly 提供只读执行上下文。支持事务的驱动返回只读 tx 包装；
    // 不支持事务的驱动（如 ClickHouse）返回普通 *sql.DB 包装（由账号权限兜底）。
    BeginReadOnly(ctx context.Context, db *sql.DB) (ReadOnlyExec, error)
}

type ReadOnlyExec interface {
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    Commit() error    // 事务型：Commit；非事务型：no-op 返回 nil
    Rollback() error  // 事务型：Rollback；非事务型：no-op 返回 nil
}

type TableSchema struct {
    Columns []ColumnInfo
    Indexes []IndexInfo
}

type ColumnInfo struct {
    Name     string
    Type     string
    Nullable bool
    Key      string // "PRI" / "UNI" / "MUL" / ""
    Default  any    // null 用 nil 表示
}

type IndexInfo struct {
    Name    string
    Columns []string
    Unique  bool
}
```

注册表：

```go
// internal/tools/db/driver/registry.go
package driver

var registry = map[string]Driver{}

func Register(d Driver) {
    if _, dup := registry[d.Name()]; dup {
        panic("driver already registered: " + d.Name())
    }
    registry[d.Name()] = d
}

func Get(name string) (Driver, bool) { d, ok := registry[name]; return d, ok }

func Names() []string { /* 返回 sorted 的 key 列表 */ }
```

MySQL 实现（第一版交付）：

```go
// internal/tools/db/drivers/mysql/mysql.go
package mysql

import (
    "context"
    "database/sql"
    _ "github.com/go-sql-driver/mysql"  // 注册 "mysql" sql driver

    "example.com/mcp-server/internal/config"
    "example.com/mcp-server/internal/tools/db/driver"
)

func init() { driver.Register(&Driver{}) }

type Driver struct{}

func (Driver) Name() string          { return "mysql" }
func (Driver) SQLDriverName() string { return "mysql" }
func (Driver) BuildDSN(cfg config.SourceConfig) (string, error) { /* ... */ }
func (Driver) QuoteIdentifier(name string) string              { /* backtick */ }
func (Driver) ListTables(ctx, db) ([]string, error)           { /* SHOW TABLES */ }
func (Driver) DescribeTable(ctx, db, table) (*driver.TableSchema, error) { /* SHOW FULL COLUMNS + SHOW INDEX */ }
func (Driver) BeginReadOnly(ctx, db) (driver.ReadOnlyExec, error) {
    tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
    if err != nil { return nil, err }
    return &mysqlROExec{tx: tx}, nil
}

type mysqlROExec struct{ tx *sql.Tx }
func (e *mysqlROExec) QueryContext(ctx, q, args...) (*sql.Rows, error) { return e.tx.QueryContext(ctx, q, args...) }
func (e *mysqlROExec) Commit() error   { return e.tx.Commit() }
func (e *mysqlROExec) Rollback() error { return e.tx.Rollback() }
```

驱动激活：`internal/tools/db/register.go`（或 main.go）中 blank import：

```go
import (
    _ "example.com/mcp-server/internal/tools/db/drivers/mysql"
    // _ "example.com/mcp-server/internal/tools/db/drivers/pgsql"      // 将来
    // _ "example.com/mcp-server/internal/tools/db/drivers/clickhouse" // 将来
)
```

**添加新驱动的完整步骤**（写进 README 一节）：
1. 新建 `internal/tools/db/drivers/<name>/`，实现 `driver.Driver` 接口所有方法
2. 在包的 `init()` 中 `driver.Register(&Driver{})` 并 blank import 底层 `database/sql` 驱动包
3. 在 `internal/tools/db/register.go` 里加一行 `_ "example.com/mcp-server/internal/tools/db/drivers/<name>"`
4. 用户即可在 `config.yaml` 中 `driver: <name>` 使用

**Rationale**：
- 接口只有 7 个方法，覆盖工具层所有方言差异；最小化约束，避免过度设计
- `BeginReadOnly` 返回接口而非具体类型，让 ClickHouse 这种无事务的驱动也能实现"只读执行上下文"，工具层只关心 `QueryContext` + `Commit`/`Rollback`
- `BuildDSN` 作为方法而非独立函数，让每个驱动完全掌控自己 DSN 字段的解释（例如 MySQL 把 `timezone` 翻译成 `loc`，PG 翻译成 `TimeZone` session 参数，ClickHouse 可能是 `time_zone` URL 参数）
- 注册表 + `init()` 自注册让"添加驱动 = 纯新增"；blank import 是 Go 社区惯用模式，心智成本低
- `panic("driver already registered")` 是防御，正常路径下触发不到

**Alternatives considered**：
- 接口里塞 `CountTables` / `ListColumns` / `ListIndexes` 等细颗粒方法：更"全能"但无直接使用者；增加新驱动的实现成本。拒绝
- 把 `ReadOnlyExec` 拉平为 `ExecuteReadOnlyQuery(ctx, db, sql, args) (rows, error)`：无法表达事务 lifecycle（Commit 什么时候发生？），在循环读取 rows 时难以管理事务。拒绝
- 用 `plugin` 包做运行时加载：跨平台限制大（Windows 不友好），过度设计。拒绝

### 3. 权限模型

每个 source 有 `mode ∈ {r, rw}`。工具调用时按下表决策：

| 工具 | `r` 源 | `rw` 源 |
|---|---|---|
| `db_list_tables` | ✅ | ✅ |
| `db_describe_table` | ✅ | ✅ |
| `db_query` (只读 SQL) | ✅（走 `BeginReadOnly`） | ✅（直接 `QueryContext`） |
| `db_execute` (写 SQL) | ❌ | ✅ |

`r` 源的"只读 SQL" 判定：
1. **首 token 白名单**：去除 `/* */` 块注释与 `-- ` 行注释后，取首个大写标识符，必须 ∈ {`SELECT`, `SHOW`, `DESCRIBE`, `DESC`, `EXPLAIN`, `WITH`, `VALUES`}
2. **多语句禁止**：trim 尾分号后若仍有 `;` 则拒绝
3. **驱动提供的只读上下文**：`r` 源 `db_query` 通过 `driver.BeginReadOnly` 执行；MySQL 驱动返回的是只读事务，对 DML / DDL / `FOR UPDATE` 自动拒绝；将来接 ClickHouse 时其实现仅做"普通 `*sql.DB` 包装"，由最小权限账号兜底（文档中点出）

`db_execute` 在 `rw` 源上的"非只读 SQL" 判定（对称）：
- 首 token 在 {`SELECT`, `SHOW`, `DESCRIBE`, `DESC`, `EXPLAIN`} 内 → 拒绝，提示"请改用 db_query"
- 多语句同样禁止

所有首 token 判定都在工具层用纯字符串分析完成（`db/guard.go`），不绑定任何驱动方言。驱动层只管"只读上下文"的语义。

**Alternatives considered**：
- 用 `vitess.io/vitess/go/vt/sqlparser` 做真正的 AST 级校验：最准，但编译体积激增、不跨 PG/SQLite/ClickHouse 方言。拒绝
- 只靠 DB 只读事务：ClickHouse 无事务无法兜底。拒绝
- 只靠白名单：`SELECT ... INTO OUTFILE` 在 MySQL 上算 SELECT 但会写文件。拒绝

### 4. 工具 schema 与返回结构

**`db_list_tables`** — 参数：`{ source: string }`。返回 text，JSON 对象：
```json
{ "source": "primary", "tables": ["users", "orders", "..."] }
```
实现：`src.Driver.ListTables(ctx, src.DB)`。

**`db_describe_table`** — 参数：`{ source: string, table: string }`。返回：
```json
{ "source": "primary", "table": "users",
  "columns": [ {"name": "id", "type": "BIGINT", "nullable": false, "key": "PRI", "default": null}, ... ],
  "indexes": [ {"name": "idx_email", "columns": ["email"], "unique": true}, ... ] }
```
实现：`src.Driver.DescribeTable(ctx, src.DB, table)`，工具层只负责序列化 `TableSchema`。`table` 名在调用前用白名单 `^[\w$]+$` 校验，避免被塞入非法字符；合法的 `table` 传给驱动层时由 `QuoteIdentifier` 转义。

**`db_query`** — 参数：`{ source: string, sql: string, args?: any[], max_rows?: number }`。
- `max_rows` 缺省用全局 `defaults.max_rows`，不能超过全局上限（超过则 clip 到全局值）
- 返回：
  ```json
  { "source": "primary", "columns": ["id", "email"],
    "rows": [[1, "a@x.com"], [2, "b@x.com"]],
    "row_count": 2, "truncated": false }
  ```
- `args` 通过 `?` 占位符由底层 `database/sql` 驱动做参数绑定，工具层永不拼接
- 执行路径：
  - `r` 源：`exec, err := src.Driver.BeginReadOnly(ctx, src.DB)` → `exec.QueryContext(...)` → `exec.Commit()`
  - `rw` 源：直接 `src.DB.QueryContext(...)`

**`db_execute`** — 参数：`{ source: string, sql: string, args?: any[] }`。
- 返回：`{ "source": "primary", "rows_affected": 3, "last_insert_id": 42 }`
- 实现：直接 `src.DB.ExecContext(...)`

失败一律以 `isError: true` 的 text 返回，内容含 source / 错误原因，禁止把密码 / 组装 DSN 写入。

### 5. 连接池：启动时建立 + 按接口驱动

每个 source 在启动时：

1. `drv, ok := driver.Get(cfg.Driver)`；未命中 → 启动失败，错误列出已注册 driver 名称
2. `dsn, err := drv.BuildDSN(cfg)`；失败 → 启动失败
3. `db, err := sql.Open(drv.SQLDriverName(), dsn)`
4. `SetMaxOpenConns(pool.max_connections)`、`SetMaxIdleConns(pool.min_connections)`、`SetConnMaxIdleTime(pool.max_idle_time)`
5. `PingContext`（ctx 超时 = `pool.connect_timeout`）；失败 → 启动失败
6. 存入 `Pool`：`Source{ Key, Mode, DB: db, Driver: drv, QueryTimeout: defaults.query_timeout }`

`Source` 把 driver 实例一并保存，工具 handler 拿到 source 后直接 `src.Driver.XXX(...)`。

连接池字段映射（配置 → Go `*sql.DB` 方法）：

| 配置字段 | Go 方法 | 行为 |
|---|---|---|
| `pool.max_connections` | `SetMaxOpenConns(n)` | 并发打开连接上限 |
| `pool.min_connections` | `SetMaxIdleConns(n)` | 空闲连接上限（Go 没"最小活跃"概念，用 max_idle 近似） |
| `pool.connect_timeout` | 启动 Ping 的 ctx 超时 + 由驱动 BuildDSN 翻译入 DSN | MySQL 下翻译为 `timeout=<dur>` |
| `pool.max_idle_time` | `SetConnMaxIdleTime(d)` | 空闲连接寿命 |

**[权衡] 启动依赖所有 source 可达**：第一版保持"全成功才启动"的硬性契约，简化逻辑；若开发阻力显现再加 `required: false`。

### 6. 修改 `server-core` 规范

`server-core` 的 `Requirement: 入口瘦身` 目前禁止 `main.go` 出现"具体工具参数的解析或数值运算"。本 change 让 `main.go` 增加：
- 解析 `--config <path>` flag
- 调用 `config.Load(path)`
- 构造 `db.Pool`（通过驱动注册表找实现 + DSN 组装 + Ping）
- 注册工具时传入 pool 与 defaults

MODIFIED `Requirement: 入口瘦身`，把 "参数解析" 明确限定为"具体工具的参数 schema / arguments 解析"，并显式允许 CLI flag、配置加载、pool 构造。

## Risks / Trade-offs

- **[风险] LLM 通过 `SELECT ... FOR UPDATE` 或 `LOCK TABLES` 在只读场景下意外加锁** → **缓解**：MySQL 驱动 `BeginReadOnly` 返回只读事务，MySQL 会拒绝 `FOR UPDATE` / `LOCK TABLES`；其它驱动接入时其 `BeginReadOnly` 实现也应保证写操作被拒绝（事务或账号权限），在 Driver 接口文档中明确这一责任
- **[风险] 配置中 password 泄漏到日志或错误信息** → **缓解**：启动日志只打印 source key + driver + mode + host:port + database；错误信息由 config/pool 自己格式化，绝不拼 password 或组装 DSN；`BuildDSN` 返回的错误消息也不包含 DSN
- **[风险] `db_query` 被用于 DDL 绕过（如 `SELECT ... INTO OUTFILE '/tmp/x'`）** → **缓解**：生产账号应使用最小权限（用户自保障）；文档中明示；只读事务 + 只读用户是兜底
- **[风险] 查询返回巨量数据撑爆 LLM 上下文** → **缓解**：`max_rows` 硬切 + `truncated: true` 标记；调用端只能降不能升
- **[风险] `params` 透传允许用户传 `multiStatements=true`** → **缓解**：`config.Validate` 显式拒绝 `params` 中出现 `multiStatements=true`（case-insensitive）
- **[权衡] 接口抽象成本**：7 个方法 + 3 个辅助 struct 是必然引入的代码量，换来添加新驱动的 near-zero 工具层改动
- **[权衡] `min_connections` → `SetMaxIdleConns` 语义不完全对应 hyperf**：hyperf 的 `min_connections` 是 swoole 启动预热数；Go `database/sql` 是懒连接，冷启动第一批请求略慢但稳态 QPS 一致。README 里加脚注
- **[权衡] 不同驱动的 `ColumnInfo.Key` / `Default` 语义可能略有差异**（MySQL 的 `"PRI"` 对应 PostgreSQL 的某种主键判定）：第一版 MySQL-only 先不考虑；将来接 PG 时在该驱动的 `DescribeTable` 实现里做映射，保持 `TableSchema` struct 不变

## Migration Plan

1. `go get github.com/go-sql-driver/mysql gopkg.in/yaml.v3`；`go mod tidy`
2. 创建 `internal/config/config.go`：Config / Defaults / SourceConfig / PoolConfig + YAML tag + Load + Validate（不校验 driver 受支持）
3. 创建 `internal/tools/db/driver/driver.go`（接口）+ `registry.go`（注册表）+ `types.go`（TableSchema 等）
4. 创建 `internal/tools/db/drivers/mysql/mysql.go`：MySQL 驱动实现，init 自注册
5. 创建 `internal/tools/db/pool.go`（依赖 driver 接口）、`guard.go`、`render.go`、`list_tables.go` / `describe_table.go` / `query.go` / `execute.go`、`register.go`（含 blank import drivers/mysql）
6. 改 `main.go`：flag 解析、config.Load、db.NewPool、add.Register、db.Register、defer Close
7. 写 `config.example.yaml`；更新 `.gitignore`
8. 手动验证（详见 tasks §8）
9. 更新 README：新增"配置字段表"、"数据库工具"、"安全模型"、"添加新驱动（以实现 Driver 接口为例）" 四节

**Rollback**：工具包全为新增，删除 `internal/tools/db/`、`internal/config/` 与 `main.go` 里对应几行即可回滚；旧 `add` 工具不受影响。

## Open Questions

- **`config.yaml` 的默认路径对 MCP 客户端拉起场景**：cwd 不一定是项目根目录，README 中建议 `.mcp.json` 中传绝对路径给 `--config`
- **`ColumnInfo` / `IndexInfo` 的字段最小集**：`Key` / `Default` 这两个 MySQL-shaped 字段是否将来会对 PG / CH 造成割裂？可以先实现 MySQL 版，第二个驱动接入时再回头调整接口（届时可能加 `ExtraAttrs map[string]any` 逃生口）
- **`min_connections` → `SetMaxIdleConns` 的语义差异**：是否需要在 README 的配置表里加脚注？倾向加
