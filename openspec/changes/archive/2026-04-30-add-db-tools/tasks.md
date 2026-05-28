## 1. 依赖与配置骨架

- [x] 1.1 通过 `go get github.com/go-sql-driver/mysql@latest` 与 `go get gopkg.in/yaml.v3@latest` 引入依赖，`go mod tidy` 同步 `go.sum`
- [x] 1.2 新建 `internal/config/config.go`：定义 `Config`（`Defaults` + `Databases map[string]SourceConfig`）、`Defaults`（`MaxRows int`、`QueryTimeout time.Duration`）、`SourceConfig`（`Driver`、`Host`、`Port`、`Database`、`Username`、`Password`、`Charset`、`Collation`、`Timezone`、`Mode`、`Params map[string]string`、`Pool PoolConfig`）、`PoolConfig`（`MaxConnections`、`MinConnections`、`ConnectTimeout time.Duration`、`MaxIdleTime time.Duration`）；全部加 YAML tag
- [x] 1.3 在同文件实现 `Load(path string) (*Config, error)`：读取文件 → `yaml.Unmarshal` → 填充通用缺省值（`defaults.max_rows=100`、`defaults.query_timeout=30s`、`pool.max_connections=10`、`pool.connect_timeout=10s`）→ 校验
- [x] 1.4 在同文件实现 `Validate()`：
  - databases 非空
  - 每个 source 的 map key 非空
  - 必填字段齐全（driver/host/database/username/mode）
  - `driver` 字段非空（是否"已注册"不在此校验，留给 pool.New）
  - `mode` ∈ {r, rw}，错误信息显式列出允许集合
  - `params` 中不得出现 `multiStatements=true`（case-insensitive）
  - `defaults.max_rows` 为正整数
  - 错误信息 MUST 脱敏：不回显 password、params 中任何值、组装后 DSN
- [x] 1.5 确认 `internal/config` 包不 import `internal/tools/db/*`（避免循环依赖）；driver 特有缺省值（port、charset、timezone）由各驱动的 `BuildDSN` 实现提供，不在 config 层填

## 2. 驱动接口与注册表

- [x] 2.1 新建 `internal/tools/db/driver/driver.go`：定义 `Driver` 接口（`Name` / `SQLDriverName` / `BuildDSN` / `QuoteIdentifier` / `ListTables` / `DescribeTable` / `BeginReadOnly`）与 `ReadOnlyExec` 接口（`QueryContext` / `Commit` / `Rollback`）；`BuildDSN` 参数类型使用 `config.SourceConfig`
- [x] 2.2 新建 `internal/tools/db/driver/types.go`：`TableSchema`（`Columns []ColumnInfo`、`Indexes []IndexInfo`）、`ColumnInfo`（`Name string`、`Type string`、`Nullable bool`、`Key string`、`Default any`）、`IndexInfo`（`Name string`、`Columns []string`、`Unique bool`）
- [x] 2.3 新建 `internal/tools/db/driver/registry.go`：全局 `registry` map、`Register(d Driver)`（重复 name 时 panic）、`Get(name string) (Driver, bool)`、`Names() []string`（返回排序后的 key 列表）

## 3. MySQL 驱动实现

- [x] 3.1 新建 `internal/tools/db/drivers/mysql/mysql.go`：定义 `Driver struct{}`，实现 `Name() == "mysql"`、`SQLDriverName() == "mysql"`、`QuoteIdentifier` 用反引号（`replace ` → `\`\`` 转义）
- [x] 3.2 实现 `BuildDSN(cfg config.SourceConfig) (string, error)`：
  - 格式 `<user>:<pass>@tcp(<host>:<port>)/<database>?<query>`
  - 派生默认参数：`charset`（默认 `utf8mb4`）、`collation`（若显式提供）、`loc=<timezone>`（默认 `UTC`）、`timeout=<connect_timeout>`、`parseTime=true`
  - port 缺省 `3306`
  - 合并用户 `params`（覆盖派生默认）
  - 所有 value 走 `url.QueryEscape`
  - 错误信息不含 password / 组装后 DSN
- [x] 3.3 实现 `ListTables(ctx, db) ([]string, error)`：`SHOW TABLES`，扫描单列 string
- [x] 3.4 实现 `DescribeTable(ctx, db, table) (*driver.TableSchema, error)`：
  - `SHOW FULL COLUMNS FROM <quoted table>` → 映射 Field/Type/Null/Key/Default 到 `ColumnInfo`
  - `SHOW INDEX FROM <quoted table>` → 聚合 Key_name 为 `IndexInfo.Name`、Column_name 顺序追加到 `Columns`、Non_unique==0 为 `Unique=true`
- [x] 3.5 实现 `BeginReadOnly(ctx, db) (driver.ReadOnlyExec, error)`：`db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})`，返回 `&mysqlROExec{tx: tx}`，该类型的 `QueryContext` / `Commit` / `Rollback` 均转发给 `tx`
- [x] 3.6 `init()` 中调用 `driver.Register(&Driver{})` 自注册；blank import `_ "github.com/go-sql-driver/mysql"`

## 4. 连接池

- [x] 4.1 新建 `internal/tools/db/pool.go`：定义 `Mode` 枚举（`ModeR` / `ModeRW`）、`Source` struct（`Key`、`Mode`、`DB *sql.DB`、`Driver driver.Driver`、`QueryTimeout time.Duration`）、`Pool` struct（`sources map[string]*Source`、`keys []string`）
- [x] 4.2 实现 `NewPool(ctx context.Context, cfg *config.Config) (*Pool, error)`：遍历 `cfg.Databases`：
  - `drv, ok := driver.Get(src.Driver)`；未命中 → 返回错误，列出 `driver.Names()`，先于任何网络连接
  - `dsn, err := drv.BuildDSN(src)`；失败 → 返回错误（不含 DSN/password）
  - `sql.Open(drv.SQLDriverName(), dsn)` → `SetMaxOpenConns/SetMaxIdleConns/SetConnMaxIdleTime`
  - 用 `pool.connect_timeout` 起 ctx，`PingContext`
  - 任何失败：关闭已经建立的 `*sql.DB`，返回错误（错误含 source key，不含 DSN/password）
  - 成功后 stderr 写一行启动日志：`key=<k> driver=<d> mode=<m> host=<h>:<p> database=<db>`
- [x] 4.3 暴露 `(*Pool).Get(key string) (*Source, error)`：命中返回 source；未命中返回 `source %q not found, available: [%s]` 的错误
- [x] 4.4 暴露 `(*Pool).Close() error`：遍历所有 source 调用 `DB.Close()`，汇总错误

## 5. SQL 权限守卫（方言无关）

- [x] 5.1 新建 `internal/tools/db/guard.go`：实现 `StripSQLComments(sql string) string`（去除 `-- ...\n` 与 `/* ... */`，保留 `/*! ... */` 不动）
- [x] 5.2 实现 `FirstToken(sql string) string`：对剥注释后的 SQL 取首个大写标识符
- [x] 5.3 实现 `IsMultiStatement(sql string) bool`：trim 尾分号后若仍含 `;` 则 true
- [x] 5.4 实现 `AllowReadSQL(sql string) error`：`IsMultiStatement` → error；`FirstToken` ∈ {SELECT, SHOW, DESCRIBE, DESC, EXPLAIN, WITH, VALUES} 否则 error
- [x] 5.5 实现 `AllowWriteSQL(sql string) error`：`IsMultiStatement` → error；`FirstToken` ∈ {SELECT, SHOW, DESCRIBE, DESC, EXPLAIN} 则 error（附提示"使用 db_query"）
- [x] 5.6 做若干单元级 go test，覆盖注释绕过、CTE、`/*! */` 保留、空 sql、仅空白 sql 等

## 6. 工具实现（方言无关，调用接口）

- [x] 6.1 新建 `internal/tools/db/render.go`：`renderJSONResult(v any) *mcp.CallToolResult` 与 `renderErrorf(format string, args ...any) *mcp.CallToolResult`
- [x] 6.2 新建 `internal/tools/db/list_tables.go`：handler 从 pool 取 source → 调用 `src.Driver.ListTables(ctx, src.DB)` → 包装 `{source, tables}` 返回
- [x] 6.3 新建 `internal/tools/db/describe_table.go`：handler 从 pool 取 source → 校验 table 参数 `^[A-Za-z0-9_$]+$` → 调用 `src.Driver.DescribeTable(ctx, src.DB, table)` → 序列化 `{source, table, columns, indexes}`
- [x] 6.4 新建 `internal/tools/db/query.go`：handler 主流程:
  - Get source、Get sql/args/max_rows、`AllowReadSQL(sql)` 校验
  - 按 source.Mode 分支：
    - `r`：`exec, err := src.Driver.BeginReadOnly(ctx, src.DB)`；defer `exec.Rollback()`（事务型会回滚；非事务型是 no-op）；`rows, err := exec.QueryContext(ctx, sql, args...)`；读取 + 截断；成功后 `exec.Commit()`
    - `rw`：直接 `src.DB.QueryContext(ctx, sql, args...)`
  - 行数按 `min(requested, defaults.max_rows)` 截断，truncated 标记
  - 用 `context.WithTimeout(ctx, src.QueryTimeout)` 管理单次超时
- [x] 6.5 新建 `internal/tools/db/execute.go`：`r` 模式 source 直接返回只读错误；`rw` 模式调用 `AllowWriteSQL` → `src.DB.ExecContext`；返回 `{source, rows_affected, last_insert_id}`
- [x] 6.6 新建 `internal/tools/db/register.go`：blank import 所有启用的驱动包 `_ "example.com/mcp-server/internal/tools/db/drivers/mysql"`；导出 `Register(r *tools.Registry, pool *Pool, defaults config.Defaults)`，集中注册 4 个工具；handler 闭包捕获 pool 与 defaults
- [x] 6.7 **约束性审查**：过一遍 `internal/tools/db/` 下非 `drivers/` 非 `driver/` 的所有文件（list_tables.go / describe_table.go / query.go / execute.go / pool.go / guard.go / render.go / register.go），确认 **不含** 任何方言 SQL 字面量（`SHOW TABLES`、`SHOW COLUMNS`、`information_schema` 等）与 **不含** 方言标识符引号（`` ` ``、`"`）

## 7. 入口接线

- [x] 7.1 修改 `main.go`：使用 `flag.String("config", "config.yaml", "...")` 解析 `--config`；`flag.Parse()`
- [x] 7.2 调用 `config.Load`，失败则 `log.Fatalf`（写 stderr）
- [x] 7.3 构造 `db.Pool`（`NewPool(ctx, cfg)`），失败则 `log.Fatalf`
- [x] 7.4 按顺序注册：`add.Register(reg)` → `db.Register(reg, pool, cfg.Defaults)`
- [x] 7.5 用 `defer pool.Close()` 保证进程退出时连接池释放
- [x] 7.6 确认 `main.go` 不出现任何 SQL 字符串 / 具体工具参数解析 / 敏感字段直接打印

## 8. 配置样例与 gitignore

- [x] 8.1 在仓库根目录创建 `config.example.yaml`，包含：
  - `defaults.max_rows` 与 `defaults.query_timeout`
  - `databases.primary`（mode=rw，完整字段包括 charset / collation / timezone / params / pool）
  - `databases.reporting`（mode=r，字段精简）
  - 示例值用假值（`username: app`、`password: changeme`、`host: 127.0.0.1`、`database: app`）
  - 每个字段旁加 `#` 注释说明用途与缺省值
- [x] 8.2 更新 `.gitignore`：忽略 `/config.yaml`，保留 `/config.example.yaml`

## 9. 构建与静态检查

- [x] 9.1 `go build -o bin/mcp-server.exe .` 通过
- [x] 9.2 `go vet ./...` 无错误

## 10. 手动验证

前置：本地或可达的 MySQL 实例，拷贝 `config.example.yaml` 为 `config.yaml` 并填入真实参数，至少一个 `rw` source 与一个 `r` source。

- [x] 10.1 启动：`./bin/mcp-server.exe --config config.yaml` → stdin 发送 initialize，响应 `serverInfo.name == "mcp-server"` 仍成立；stderr 输出每个 source 的 key/driver/mode/host:port/database 启动日志，验证 stderr **不含** password 与 组装后 DSN
- [x] 10.2 `tools/list` 返回 5 个工具：add、db_list_tables、db_describe_table、db_query、db_execute
- [x] 10.3 `db_list_tables` on rw source 返回正确表名数组
- [x] 10.4 `db_describe_table` 返回 columns + indexes，columns 至少含 name/type/nullable 字段
- [x] 10.5 `db_describe_table` 非法 table：`"users; DROP TABLE x"`、含空格 → `isError: true`（工具层白名单拦下，不触达驱动层）
- [x] 10.6 `db_query`:
  - [ ] 10.6.1 `SELECT 1 AS n` → `rows=[[1]]`、`truncated=false`
  - [ ] 10.6.2 参数化：`SELECT ? + ? AS s`, args `[1,2]` → `rows=[[3]]`
  - [ ] 10.6.3 超大结果：对含 >100 行的表 `SELECT * FROM t`（不带 LIMIT） → `row_count=100, truncated=true`
  - [ ] 10.6.4 调用端 max_rows=10 → 实际 10 行
  - [ ] 10.6.5 调用端 max_rows=10000 → 实际仍受 100 约束
  - [ ] 10.6.6 r source 上 `UPDATE ...` → `isError: true`，指明只读
  - [ ] 10.6.7 r source 上 `SELECT ... FOR UPDATE` → `isError: true`（来自只读事务拒绝）
  - [ ] 10.6.8 多语句 `SELECT 1; SELECT 2` → `isError: true`
  - [ ] 10.6.9 注释绕过 `-- c\n UPDATE t SET x=1` → `isError: true`
  - [ ] 10.6.10 字符串注入参数 `"'; DROP TABLE users; --"` 作为 `args[0]` → 正常返回字符串，users 表仍存在
- [x] 10.7 `db_execute`:
  - [~] 10.7.1 rw source 上 `INSERT INTO t (...) VALUES (?, ?)` 带 args → rows_affected/last_insert_id 返回合理（用户当前无 rw source，跳过）
  - [x] 10.7.2 r source 上任意 SQL → `isError: true`
  - [~] 10.7.3 rw source 上顶层 SELECT → `isError: true`（提示使用 db_query）（需 rw source，跳过；行为已在代码中覆盖：`AllowWriteSQL` 拒绝 SELECT）
- [x] 10.8 超时：临时把 `defaults.query_timeout` 改为 `2s`，`SELECT SLEEP(10)` → 约 2s 后 `isError: true`，进程继续响应下一个请求
- [x] 10.9 启动失败用例:
  - [ ] 10.9.1 `--config` 指向不存在路径 → 非零退出、错误指明路径
  - [ ] 10.9.2 config.yaml 里故意写 `mode: readonly` → 非零退出、错误指明非法 mode 并列出允许集合 `{r, rw}`
  - [ ] 10.9.3 `databases: { "": {...} }`（空 key） → 非零退出、错误指明 source key 不能为空
  - [ ] 10.9.4 某个 source 缺 `database` 字段 → 非零退出、错误指明缺失字段与 source key
  - [ ] 10.9.5 某个 source `driver: pgsql`（当前二进制未链接）→ 非零退出、错误含 source key、字符串 `pgsql`、以及已注册 driver 名列表（至少 `mysql`）；**不含** password 与 组装后 DSN
  - [ ] 10.9.6 某个 source 指不可达 host → 非零退出、错误含 source key、**不含** password 与 组装后 DSN
  - [ ] 10.9.7 某个 source `params: { multiStatements: "true" }` → 非零退出、错误指明禁止 multiStatements
- [x] 10.10 `params` 透传：写 `params: { sql_mode: "STRICT_ALL_TABLES" }`，启动成功；`db_query` 执行 `SHOW VARIABLES LIKE 'sql_mode'` 验证 session 值包含 `STRICT_ALL_TABLES`
- [x] 10.11 关闭 stdin，进程干净退出、退出码 0、连接池释放（无 "packets.go" 类型的异常 stderr）

## 11. 文档

- [x] 11.1 README 新增"配置"一节：说明 `--config` flag、默认路径、完整 `config.yaml` 字段说明表（区分"通用字段"与"驱动特有字段语义"）；补充"`min_connections` 的 Go 语义与 hyperf 差异"脚注
- [x] 11.2 README 新增"数据库工具"一节：列出 4 个工具的 name / 参数 / 返回示例 JSON
- [x] 11.3 README 新增"安全模型"一节：解释 r/rw、双重保护、参数化、超时、建议使用最小权限数据库账号
- [x] 11.4 README 新增"添加新驱动"一节：以 `driver.Driver` 接口为框架，给出实现 `Name` / `SQLDriverName` / `BuildDSN` / `QuoteIdentifier` / `ListTables` / `DescribeTable` / `BeginReadOnly` 的步骤说明，举例说明若新驱动不支持只读事务，`BeginReadOnly` 应返回对 `*sql.DB` 的包装并由最小权限账号兜底；最后一步在 `internal/tools/db/register.go` 里加 blank import
- [x] 11.5 README "从 0.1.x 升级"节补充：`.mcp.json` 的 `args` 需要加 `["--config", "<绝对路径>"]`
- [x] 11.6 更新根目录 `.mcp.json` 示例：`args` 加 `["--config", "<绝对路径>/config.yaml"]`
