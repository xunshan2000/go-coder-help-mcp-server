## Context

`db_query` / `db_execute` 目前执行完即返回 MCP 响应，不留下任何本地痕迹。用户希望留一份本地 SQL 审计日志，用于事后排查、安全审计、慢查询分析。日志格式用 JSONL（文件名扩展 `.log`）—— 一行一条记录，方便 `jq` / DuckDB / Loki / ELK 消费，同时 `tail -f` 也勉强可读。

关键约束：
- 日志写失败不能影响工具响应（LLM 的调用不可因本地磁盘问题而失败）
- 多个 MCP 请求是 mcp-go SDK 侧并发调度的，日志写盘必须并发安全
- 审计日志内含真实 SQL + 参数值，**敏感**，必须 gitignore
- 未来新增 PG / ClickHouse 驱动时，`sql_rendered` 字段需要各方言自己的值转义规则 —— 通过驱动接口扩展来承载
- v1 不配置化（不进 config.yaml）：固定 `./logs/` 下、按本地日期滚动、始终启用；复杂度留给后续 change

## Goals / Non-Goals

**Goals:**
- 定义一个 SQL 审计日志器 `internal/tools/db/sqllog/`：JSON Lines 格式、按日滚动、并发安全、写失败不破坏调用
- 扩展 `Driver` 接口加一个 `RenderSQL(sql string, args []any) string` 方法
- MySQL 驱动实现 `RenderSQL`：按 MySQL 转义规则把 args 内联到 `?` 占位符
- `db_query` / `db_execute` handler 在结束时（无论成功 / 失败）调用 logger 写一条记录
- `list_tables` / `describe_table` 不记录（它们没有用户提交的 SQL）
- 日志文件相对进程 cwd 放在 `./logs/sql-YYYY-MM-DD.log`
- 进程关闭时 flush 并 Close 日志文件

**Non-Goals:**
- 不实现配置化（启停开关 / 自定义路径 / 大小滚动 / 压缩归档 / 保留天数）—— 后续 change
- 不实现异步日志（不引 channel / 后台 goroutine）—— 单进程 stdio + LLM pace 下 sync 写足够
- 不做字段脱敏（args 原样下盘）—— 前置决策
- 不做 `list_tables` / `describe_table` 的调用日志 —— 前置决策
- 不提供 `.jsonl → .sql` 转换 CLI —— 用户可自己用 `jq -r '.sql_rendered + ";"' < xxx.log` 取得
- 不实现日志的结构校验 / schema 文档输出 —— README 里列字段就够

## Decisions

### 1. 文件命名与滚动

格式：`./logs/sql-YYYY-MM-DD.log`（相对进程启动时的 cwd）

- 目录自动 `os.MkdirAll(0755)`
- 日期按**本地时区**计算（`time.Now().Format("2006-01-02")`）
- 每次写入前检查"今日日期字符串"与当前打开文件对应的日期字符串是否一致；不一致 → Close 当前文件、Open 新日期的文件（append 模式，权限 0640）
- "跨日"切换点是**下一次写入时**（懒切换），不起 cron goroutine
- 文件每条 JSON 后追加 `\n`

**Rationale**：
- 按日期切文件是最符合直觉的审计归档方式
- 懒切换避免后台 goroutine 的复杂度（无需 cancel / wait-done）
- cwd 相对路径简单；MCP 客户端拉起进程时 cwd 由客户端决定 —— 写进 README 提醒

**Alternatives considered**：
- UTC 滚动：跨日期边界对亚太用户不直觉，拒
- 按小时 / 按大小滚动：v1 scope 外
- 打日志到 server stderr：stderr 会污染 mcp-go 的某些诊断；且不是日志文件语义

### 2. JSONL 记录结构

每条日志一行 JSON，键按下列 **稳定顺序**（方便 grep 固定前缀）：

```json
{"ts":"2026-04-30 17:59:06","source":"default","mode":"r","tool":"db_query","sql":"SELECT * FROM users WHERE id = ?","sql_rendered":"SELECT * FROM users WHERE id = 1","args":[1],"duration_ms":42,"rows":15,"truncated":false,"ok":true}
```

字段列表（成功路径）：

| 字段 | 类型 | 说明 |
|---|---|---|
| `ts` | string | 本地时区下的 `YYYY-MM-DD HH:MM:SS`（零填充、秒级精度；与 MySQL DATETIME 字面量一致，便于 `sql_rendered` 与 `ts` 视觉对齐；无时区后缀） |
| `source` | string | map key |
| `mode` | string | `r` / `rw` |
| `tool` | string | `db_query` / `db_execute` |
| `sql` | string | 实际发给 DB 的参数化 SQL（含 `?`） |
| `sql_rendered` | string | args 内联后的可读形式；**仅供人工阅读** |
| `args` | array | 绑定值；JSON 编码（nil → null、数字 → number、字符串 → string、[]byte → base64） |
| `duration_ms` | integer | handler 总耗时 |
| `rows` | integer | `db_query` 独有：返回行数（不是 row_count 而是实际记录数） |
| `truncated` | bool | `db_query` 独有 |
| `rows_affected` | integer | `db_execute` 独有 |
| `last_insert_id` | integer | `db_execute` 独有 |
| `ok` | bool | 始终存在；true = handler 返回非 isError |

失败路径（`ok: false`）：
- 保留 `ts/source/mode/tool/sql/sql_rendered/args/duration_ms/ok:false/err`
- `err` 为 handler 将返回给 MCP 的错误文本摘要（单行，无换行）
- 可能缺 `sql_rendered`（驱动 RenderSQL 失败时用 `""`）、缺 `sql`（当请求连 sql 参数都缺失时日志标记 `sql:""`）
- 对于连 source 都没取到的失败（比如 source 不存在），`mode` 为 `""`

"字段稳定顺序"通过显式 `map[string]any` 序列化难以保证 —— 我们用一个专用 struct 字段 tag 顺序 + `encoding/json` 默认按 struct 字段顺序输出的性质来达成。

### 3. 驱动接口扩展：RenderSQL

`Driver` 接口新增方法：

```go
// RenderSQL 把参数化 SQL 的 ? 占位符替换为各驱动方言下的值字面量，
// 供审计日志 / 人工分析使用。返回值 MUST NOT 作为 SQL 发送给数据库。
// 若 args 个数与占位符不匹配 / 驱动无法内联某个值类型，MAY 返回空字符串或
// 带标记的字符串（如 "/* unable to render: ... */"）；调用方 MUST 容忍空。
RenderSQL(sql string, args []any) string
```

所有现有实现必须补齐（仅 MySQL 一个）。将来新驱动同时继承此责任。

MySQL 实现的语义：
- 按顺序扫描 `?`（跳过出现在 `'...'` / `"..."` / `` `...` `` 内的 `?`）
- 对应的 args[i] 按类型生成字面量：
  - `nil` → `NULL`
  - `bool` → `1` / `0`（MySQL 习惯）
  - 数值类型（`int*`、`uint*`、`float*`）→ `%v` 格式
  - `string` → 单引号包裹，内部字符按 MySQL 客户端转义表处理（`\0` / `\'` / `\"` / `\\` / `\n` / `\r` / `\t` / `\Z` / `\b`；非 ASCII 直接保留）
  - `[]byte` → hex 字面量 `X'48656c6c6f'`
  - `time.Time` → `'2006-01-02 15:04:05'` UTC 格式（与 `parseTime=true` 的回显格式一致）
  - 其它类型 → `fmt.Sprintf("%v", v)` 包单引号（降级处理）
- args 个数不匹配占位符时，仅替换前 `min(占位符数, len(args))` 个；多出的占位符保留 `?` 原样
- **注意**：这个实现是**最佳努力**，目的是"让 DBA 看懂"而非"完全等价于驱动的参数化行为"。

**Rationale**：
- 把方言相关的事留给驱动实现，logger 包保持驱动无关
- 失败容忍（返回空字符串或原样 SQL）让 logger 健壮

**Alternatives considered**：
- Logger 包内自己做 render：会硬编码 MySQL 转义到通用包，违反驱动抽象
- 加 `QuoteLiteral(v any) string` + logger 内部循环：API 更原子，但 logger 要做 `?` 扫描 + 引号状态机，重复劳动；把整个 `RenderSQL` 交给驱动更内聚
- 用 go-sql-driver 的 `InterpolateParams=true` 然后在 server 端拦截 —— 需要写自定义 driver wrapper，改动面巨大且不可跨驱动，拒

### 4. 日志器 API 与生命周期

```go
package sqllog

type Entry struct {
    TS           string  `json:"ts"`
    Source       string  `json:"source"`
    Mode         string  `json:"mode"`
    Tool         string  `json:"tool"`
    SQL          string  `json:"sql"`
    SQLRendered  string  `json:"sql_rendered"`
    Args         []any   `json:"args"`
    DurationMs   int64   `json:"duration_ms"`
    Rows         *int    `json:"rows,omitempty"`
    Truncated    *bool   `json:"truncated,omitempty"`
    RowsAffected *int64  `json:"rows_affected,omitempty"`
    LastInsertID *int64  `json:"last_insert_id,omitempty"`
    OK           bool    `json:"ok"`
    Err          string  `json:"err,omitempty"`
}

type Logger struct {
    dir   string
    mu    sync.Mutex
    file  *os.File
    date  string  // yyyy-mm-dd of currently open file
}

// New creates a Logger rooted at `dir`. The directory is created if missing.
// Does NOT open any file yet — first Write lazily opens today's.
func New(dir string) (*Logger, error) { ... }

// Write serializes one entry + newline, rotating the underlying file if the
// local-date has changed. Any internal failure is logged to stderr and
// swallowed — this function MUST NOT return an error.
func (l *Logger) Write(ctx context.Context, e Entry) { ... }

// Close flushes and closes the underlying file (if any). Idempotent.
func (l *Logger) Close() error { ... }
```

- `Write` 的错误不上抛，只打一行 stderr 带 source + tool 标记（无 SQL / args 内容，避免重复污染）
- `mu` 串行化所有 Write；高并发下性能下降但保证写入完整（无交错行）
- `Close` 在 main.go `defer pool.Close()` 之后 `defer logger.Close()`

### 5. Handler 接入点

`db_query` / `db_execute` 的 handler 入口记下 `start := time.Now()`；在所有返回点（包括 early-return 错误路径）前 `defer` 一个写日志的函数。

具体形态：
```go
func handleQuery(pool *Pool, defaults config.Defaults, logger *sqllog.Logger) func(...) {
    return func(ctx, req) (*mcp.CallToolResult, error) {
        start := time.Now()
        args := req.GetArguments()
        sourceName, _ := readString(args, "source")  // 即使失败也尽量填
        sqlText, _ := readString(args, "sql")
        bindings := readAnySlice(args, "args")

        // 声明一个 entry 变量，defer 里根据最终 result 填字段
        var (
            mode        string
            rowsPtr     *int
            truncPtr    *bool
            ok          bool
            errMsg      string
            renderedSQL string
        )
        defer func() {
            logger.Write(ctx, sqllog.Entry{
                TS: nowLocalTS(),
                Source: sourceName, Mode: mode,
                Tool: "db_query",
                SQL: sqlText, SQLRendered: renderedSQL, Args: bindings,
                DurationMs: time.Since(start).Milliseconds(),
                Rows: rowsPtr, Truncated: truncPtr,
                OK: ok, Err: errMsg,
            })
        }()

        // 原有 handler 逻辑；在各个 return 点前赋值 ok / errMsg / rowsPtr / truncPtr / renderedSQL
        ...
    }
}
```

`renderedSQL` 在取到 source 后调用 `src.Driver.RenderSQL(sqlText, bindings)`；如果 source 都没取到，用空字符串。

**权衡**：handler 代码重复度变高（两个 handler 各有一套 defer 构造）。为可读性接受这个成本，不硬抽出一个 "wrap handler with audit" 的 meta-handler（否则 `*Source` / 工具特定字段流转变复杂，反而更难读）。

### 6. 日志的"失败路径"含义

几种失败：
- **Source 不存在**：`mode=""`, `ok:false`, `err="source \"xxx\" not found..."`, `sql_rendered=""`
- **r 源上写语句**：`mode="r"`, `ok:false`, `err="source=... mode=r: ..."`，**有** `sql_rendered`（驱动能 render，只是不执行）
- **驱动层错误（connection refused / timeout）**：`ok:false`, `err="query 失败 [src]: ..."`, 有 `sql_rendered`
- **logger 自己写盘失败**：回落到 stderr 一行 `[sqllog] write failed: ...`，MCP 响应完全不受影响

全部失败场景都有 `ts` / `source`（尽最大努力）/ `tool` / `ok:false` / `err`。

## Risks / Trade-offs

- **[风险] 日志文件敏感**（真实 SQL + args 包含业务数据）→ **缓解**：`.gitignore` 加 `/logs/`；README 在"安全模型"一节补充"审计日志的处理"段落，建议运维定期 rotate / encrypt at rest
- **[风险] cwd 不可预期**：MCP 客户端拉起进程时 cwd 可能是客户端目录 → **缓解**：README 明示"日志位置 = 进程 cwd 下 `./logs/`；若通过 `.mcp.json` 拉起且位置不稳，建议在 MCP 客户端侧设置项目 cwd，或后续 change 加配置路径"
- **[风险] 盘写满后 logger 每次调用都失败 stderr 刷屏** → **缓解**：Logger 内部用原子 bool 记录"上一次 Write 已经失败"，连续失败时 stderr 只在每 N 次打一次告警（实现简单，不精确）—— 设为 N=100，或者记一次时间戳按 1 分钟节流。先从最简单的"每次失败都打"开始，观察再收敛
- **[权衡] 同步写入 vs 异步队列**：单进程 stdio + LLM 请求 rate < 10 QPS 场景下同步写入每条多 0.1ms 级别，完全无感。引入 channel + 后台 goroutine 会带来优雅退出的复杂度（drain-on-close、中途 crash 丢失）。本 change 选同步
- **[权衡] `sql_rendered` 与驱动真实执行不等价**：我们的 MySQL 渲染规则是 best-effort，不包含 `sql_mode` 等会话变量的影响，也不处理二进制字段的边界 case。日志应该在 README 中明示"`sql_rendered` 是分析辅助，不可作为安全证据"
- **[风险] 日志 Go 标准库 JSON 对 `[]byte` 默认 base64 编码，DBA 看 args 不直观** → **接受**：`sql_rendered` 里的 `[]byte` 用 `X'hex'` 已经人类可读；args 数组里的 base64 主要是字节级完整性兜底。需要时 jq 一行 `@base64d` 解码即可
- **[权衡] 跨日懒切换可能漏掉"零调用日"**：如果某天 0 次 SQL 调用，那天不会产生 `sql-YYYY-MM-DD.log` 文件。这是正确行为（没数据不该凭空建文件），但分析工具如果按目录 walk 要容忍日期跳跃

## Migration Plan

1. 新增 `internal/tools/db/sqllog/sqllog.go`（含 `Logger` + `Entry` 类型 + 单元测试 —— mock 时间、验证跨日切换、并发写不交错）
2. 扩展 `driver.Driver` 接口加 `RenderSQL`；更新 MySQL 驱动实现并加单元测试（cover 各种类型 + 引号嵌入 `?` 的 skip 行为）
3. 改 `query.go` / `execute.go` 增加 defer 日志写入；`register.go` 的签名加 logger 参数
4. 改 `main.go`：启动时 `sqllog.New("./logs")`，`defer logger.Close()`，把 logger 传给 `db.Register`
5. 更新 `.gitignore` 加 `/logs/`
6. 更新 README：新增"SQL 审计日志"小节（位置 / 字段 / 示例 jq 查询）；"安全模型"一节补充日志敏感性提示
7. 手动验证：跑真实查询 + 错误查询，检查 `./logs/sql-YYYY-MM-DD.log` 的内容结构、字段完整性、并发场景

**Rollback**：新增代码可逆 —— 删 `internal/tools/db/sqllog/`、还原 `query.go` / `execute.go` 的 defer 块、从 Driver 接口移除 `RenderSQL` 即可。旧 `./logs/` 目录不动。

## Open Questions

- **stderr 刷屏节流的具体策略**：简单每次都打 vs 每 100 次 / 每 60s 节流一次。倾向每次都打（v1 不搞节流），观察实际运行再收敛
- **args 包含 `time.Time` 时 JSON 编码**：Go 默认 ISO 8601 字符串 —— OK；但 MySQL `RenderSQL` 下应该输出 `'YYYY-MM-DD HH:MM:SS'` MySQL 格式。两个地方不一致不算 bug，一个是审计视图一个是复现视图
- **将来是否把 Logger 抽象化（扩到其它工具）**：目前只 DB 工具需要，放在 `internal/tools/db/sqllog/`；将来真有跨工具需求时再搬到 `internal/audit/` 这种位置
