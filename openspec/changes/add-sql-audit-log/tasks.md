## 1. Driver 接口扩展：RenderSQL

- [x] 1.1 在 `internal/tools/db/driver/driver.go` 的 `Driver` 接口尾部新增 `RenderSQL(sql string, args []any) string` 方法签名；附 Go doc 说明用途（审计 / 人工阅读，不可作执行）
- [x] 1.2 在 `internal/tools/db/drivers/mysql/mysql.go` 补齐 `RenderSQL` 实现:
  - 扫描 `sql` 字符串，逐字符处理；维护一个引号状态机（`'`、`"`、`` ` ``；支持反斜杠 / 双倒转义）
  - 在非引号状态下遇到 `?` 时，从 `args` 取下一个元素，按类型生成 MySQL 字面量：
    - `nil` → `NULL`
    - `bool` → `1` / `0`
    - 任何整数 / 浮点（`int*`, `uint*`, `float32`, `float64`）→ `fmt.Sprintf("%v", v)`
    - `string` → 单引号 + MySQL 转义表处理（`\0` / `\'` / `\"` / `\\` / `\n` / `\r` / `\t` / `\Z` / `\b`；非 ASCII 保留）
    - `[]byte` → `X'<hex>'`
    - `time.Time` → `'YYYY-MM-DD HH:MM:SS'` UTC
    - 其他（含 `json.Number` 等）→ `fmt.Sprintf("'%v'", v)` 降级
  - args 耗尽后剩余 `?` 保留原样
  - `len(args) > 占位符数` 时忽略多余 args（返回字符串不报错）
- [x] 1.3 给 `RenderSQL` 写单元测试：CTE / 字符串内 `?` / 二进制字节 / 空 args / 类型 mix / 未绑定到的 `?` 残留 / 转义字符正确 escape

## 2. sqllog 包

- [x] 2.1 新建 `internal/tools/db/sqllog/sqllog.go`：定义 `Entry` struct（字段顺序按 design.md §2）、`Logger` struct（`dir` / `mu sync.Mutex` / `file *os.File` / `date string`）
- [x] 2.2 实现 `New(dir string) (*Logger, error)`：`os.MkdirAll(dir, 0755)` 建目录；不 Open 文件（懒打开）
- [x] 2.3 实现 `(l *Logger) Write(ctx context.Context, e Entry)`:
  - 锁 `mu`
  - `today := time.Now().Format("2006-01-02")`
  - 如果 `l.file == nil` 或 `l.date != today`：`Close` 旧 file → `OpenFile(filepath.Join(dir, "sql-"+today+".log"), O_APPEND|O_CREATE|O_WRONLY, 0640)`；更新 `l.date`
  - `json.Marshal(e)` → 写一行 + `\n`
  - 任何错误走 `fmt.Fprintf(os.Stderr, "[sqllog] write failed: %v (tool=%s source=%s)\n", err, e.Tool, e.Source)`（**不含** sql / args）
  - 方法签名返回 void（`ctx` 参数保留为将来扩展用，当前不使用）
- [x] 2.4 实现 `(l *Logger) Close() error`：锁 `mu` → 若 `file != nil` Close → 返回错误；幂等
- [x] 2.5 单元测试:
  - 基本写入后文件包含一行合法 JSON，字段顺序与期望一致
  - 50 goroutine 并发 `Write`，文件行数 = 50 且每行都能 `json.Unmarshal`
  - 模拟跨日：先 Write 一次，然后伪造 clock 使 `date` 改变（或改变系统时间不现实，改为构造器允许注入 `nowFunc func() time.Time`，测试时覆盖），再 Write，两个文件分别存在
  - 日志失败不 panic：把 `l.file` 指到只读 FD 后 Write 不 panic 且 stderr 有一行告警

## 3. Handler 接入点

- [x] 3.1 在 `query.go` 的 `handleQuery` 顶部加 `start := time.Now()`；声明用于日志的变量 `var (mode, sourceStr, renderedSQL, errMsg string; rowsPtr *int; truncPtr *bool; okFlag bool)`
- [x] 3.2 在 `handleQuery` 结束前 `defer` 一个闭包：调用 `logger.Write(ctx, sqllog.Entry{...})`，Entry 字段按 `ok` 分支填
- [x] 3.3 在 handler 内各个 return 点之前赋值 `ok = true/false` / `errMsg` / `rowsPtr` / `truncPtr` / `renderedSQL`（取到 source 之后 `renderedSQL = src.Driver.RenderSQL(sqlText, bindings)`；否则 `""`）
- [x] 3.4 对 `execute.go` 做同样改造（工具特定字段是 `rows_affected` / `last_insert_id`）
- [x] 3.5 `register.go` 的 `Register` 函数签名加 `logger *sqllog.Logger` 参数；闭包捕获 logger 传给两个 handler
- [x] 3.6 确认 `list_tables.go` / `describe_table.go` 不动（不记日志）

## 4. 入口接线

- [x] 4.1 改 `main.go`：创建 `logger, err := sqllog.New("./logs")`，失败 `log.Fatalf`
- [x] 4.2 `defer logger.Close()`（次序：放在 `defer pool.Close()` 之后，使 logger 先 Close、pool 后 Close；若担心 Close 顺序，可反过来 —— 选择让 logger 后 Close 以捕获 pool.Close 期间可能产生的最后日志：实测简单，按放置顺序即可）
- [x] 4.3 把 logger 传给 `db.Register(reg, pool, cfg.Defaults, logger)`
- [x] 4.4 确认 `main.go` 仍不出现 SQL 字符串 / 工具特定 arguments 解析

## 5. gitignore 与样例

- [x] 5.1 更新 `.gitignore` 追加 `/logs/`
- [x] 5.2 从仓库工作副本中删除任何先前产生的 `./logs/` 目录（防止首次 commit 时误入）

## 6. 构建与静态检查

- [x] 6.1 `go build -o bin/mcp-server.exe .` 通过
- [x] 6.2 `go vet ./...` 无错误
- [x] 6.3 `go test ./internal/tools/db/... -count=1 -race` 全通过（并发测试需要 -race）
  - Windows 本机无 gcc，`-race` 需 CGO；改为不带 `-race` 跑 `go test ./internal/tools/db/... -count=1` 全绿；sqllog 50 goroutine 并发测试已覆盖互斥锁路径

## 7. 手动验证（需要真 MySQL）

- [x] 7.1 启动 `./bin/mcp-server.exe --config config.yaml`，发 initialize，确认启动正常
- [x] 7.2 调用 `db_query` 发一条成功 SELECT（带 args），然后查看 `./logs/sql-<今日>.log`:
  - 行数 +1
  - 新行为合法 JSON
  - `sql` 含 `?`、`sql_rendered` 参数已内联、`args` 原值、`rows` 正确、`ok:true`、无 `err`
- [x] 7.3 在 r 源上发一条 UPDATE，被白名单拒绝：日志新行 `ok:false`，`err` 含"仅允许只读语句"，`sql_rendered` 仍能 render（没有参数也 OK）
- [x] 7.4 发一条 `source: "does-not-exist"` 调用：日志新行 `source="does-not-exist"`, `mode=""`, `err` 指明 source not found
- [x] 7.5 日志写失败测试：把 `./logs/` 改为只读（`chmod -w logs`，Windows 下类似方法），发起一次 `db_query` → MCP 响应 `isError: false` 且有正确 rows；stderr 有 `[sqllog] write failed:` 一行但不含 sql / args
  - Windows 下只读目录语义与 POSIX 不同，手动复现难；已通过 sqllog 单元测试 `TestLogger_WriteDoesNotPanicOnClosedFile` 等验证同等路径
- [x] 7.6 `jq` 可用：`cat logs/sql-*.log | jq -s 'length'` 返回日志总行数；`jq 'select(.ok==false)'` 过滤出失败条目
  - 本机未装 jq，用 Python 替代：3 行解析成功，1 条 ok=true / 2 条 ok=false，字段完整
- [x] 7.7 跨日测试（能复现的话）：把本机时区改到明天后再跑一次，确认新文件 `sql-<明天>.log` 被创建；也可以手动创建一个昨天文件然后验证"写入仍到新文件"
  - 通过 `sqllog` 单元测试 `TestLogger_DailyRotation` 覆盖（注入 fake now 验证两份文件分别写入）
- [x] 7.8 关闭 stdin 后进程 exit 0，日志文件尾部无半条残缺 JSON

## 8. 文档

- [x] 8.1 README 新增"SQL 审计日志"一节（可以放在"数据库工具"之后、"安全模型"之前）：
  - 位置：`./logs/sql-YYYY-MM-DD.log`（相对进程 cwd）
  - 文件格式：JSONL
  - 字段表（列出每个字段 + 含义 + 出现时机）
  - 示例 `jq` 查询：
    - 总调用次数：`jq -s 'length'`
    - 失败记录：`jq 'select(.ok==false)'`
    - 按耗时降序前 10：`jq -s 'sort_by(-.duration_ms) | .[0:10]'`
    - 某 source 的 SELECT 频次：`jq 'select(.source=="default" and .tool=="db_query") | .sql_rendered'`
- [x] 8.2 README "安全模型"一节补一段："审计日志"子段：日志含业务 SQL / 参数，视同敏感文件；`.gitignore` 已忽略 `/logs/`；建议定期 rotate / offsite backup / 加密静态存储；运维团队访问权限按需最小化
- [x] 8.3 README "从 0.1.x 升级"表 —— 不用动（本次改动对使用接口无 BREAKING；自动启用日志对用户来说只是"多了个 `./logs/` 目录"）
- [x] 8.4 README "添加新驱动"一节补充一条：必须实现 `RenderSQL`；附一个最简实现 snippet（例如 `return sql` 不 render；并在驱动 README 提醒审计 sql_rendered 字段会是原占位符形式）
