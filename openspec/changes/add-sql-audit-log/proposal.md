## Why

目前 `db_query` / `db_execute` 执行完即销毁上下文，**没有本地审计轨迹**。当出现以下场景时缺乏依据：
- 事后排查 LLM 曾执行过哪些 SQL 导致数据变化
- 安全审计需要证明某条危险查询发生的时间 / 是否被阻断
- 分析慢查询（要知道哪条语句耗时多少）
- 统计 LLM 调用模式（哪些表访问最多、错误率）

加一份**本地 SQL 审计日志**，每条 `db_query` / `db_execute` 调用都写一条 JSON 记录到 `./logs/sql-YYYY-MM-DD.log`，字段结构化、可用 `jq` / DuckDB / ELK / Loki 等直接消费。

## What Changes

- 新增 `internal/tools/db/sqllog/` 子包：SQL 审计日志器（line-writer + 按日期分文件 + 并发安全）
- `db_query` 与 `db_execute` 的 handler 在每次调用结束（无论成功 / 失败）MUST 写入一条 JSONL 记录
- 日志字段：
  - `ts` — 调用完成时间，格式 `YYYY-MM-DD HH:MM:SS`（本地时区、零填充、秒级精度，例如 `"2026-04-30 17:59:06"`；与 MySQL DATETIME 字面量格式一致）
  - `source` — 数据源 key
  - `mode` — `r` / `rw`
  - `tool` — `db_query` / `db_execute`
  - `sql` — 用户原始 SQL（含 `?` 占位符，**未绑定**；就是实际发给 DB 的那条）
  - `sql_rendered` — 合成 SQL（args 按驱动规则转义后内联到 `?` 位置；**仅供人类阅读 / 复现查询**，不作执行依据）
  - `args` — 参数化占位符的绑定值数组（原样，不脱敏）
  - `duration_ms` — 从 handler 开始到结束的毫秒数（整数）
  - `ok` — 布尔，是否成功返回（未标记 isError）
  - `err` — 失败时填充的一行错误摘要（成功时省略）
  - 成功时的工具特定字段：
    - `db_query`: `rows` (返回行数), `truncated` (布尔)
    - `db_execute`: `rows_affected`, `last_insert_id`

  三字段并存的意义：
  - `sql` 还原出 DB 实际收到的参数化语句（用于证明"我们没有拼接 SQL"）
  - `args` 审计绑定值真实内容（PII 审计、注入测试复盘）
  - `sql_rendered` 让 DBA / 分析者能**直接复制粘贴**到 MySQL Workbench 等客户端重放（无须手动替换占位符）
- 日志文件：
  - 位置：进程启动 cwd 下 `./logs/sql-YYYY-MM-DD.log`
  - 日期按**本地时区**滚动；新的一天第一条日志触发新文件创建
  - 目录不存在时自动创建
  - 多次调用串行 append（互斥锁保护），同一文件内不交错
- 日志写入失败（磁盘满、权限拒绝等）MUST NOT 导致工具调用失败；失败事件回落到 stderr 一行告警（同样受脱敏约束，不回显 args / SQL 内容）
- `list_tables` / `describe_table` **不**写日志（它们只调用驱动的元信息查询，用户未直接提交 SQL）
- 进程退出时 MUST flush 并关闭当前日志文件

## Capabilities

### New Capabilities
- `sql-audit`: SQL 审计日志行为规范（文件位置 / 命名 / JSONL 字段 / 滚动 / 并发安全 / 失败处理）

### Modified Capabilities
- `db-tools`: 两处变更：
  - `db_query` 与 `db_execute` Requirement 补充约束：每次调用结束 MUST 触发一次 `sql-audit` 日志写入，调用结果不受日志写入成功与否影响
  - `Driver` 接口新增一个 `RenderSQL(sql string, args []any) string` 方法，用于把 `?` 参数化 SQL 转成内联绑定值的人类可读形式；MySQL 驱动给出实现；将来新驱动必须实现（这个扩展是为了让审计日志的 `sql_rendered` 字段方言正确）

## Impact

- **代码**：新增 `internal/tools/db/sqllog/sqllog.go` + 单元测试；`db_query` / `db_execute` handler 注入 logger 调用；`register.go` 负责日志器生命周期（启动创建 / defer 关闭）
- **文件系统**：运行目录下多出 `./logs/` 目录和每日 `.log` 文件；长期运行需要用户周期性清理（本 change 不实现自动清理）
- **依赖**：不引入新的外部依赖（纯标准库 `os` / `encoding/json` / `sync` / `time`）
- **性能**：每条 SQL 额外一次 `json.Marshal` + 一次文件写入；对单进程 stdio + LLM pace（每秒最多个位数请求）无感
- **安全**：日志含真实 SQL 与参数值，**属于敏感文件**；`.gitignore` 需加入 `/logs/`；README 需告知用户 `./logs/` 的处理建议
- **跨平台**：路径用 `filepath.Join` 兼容 Windows / 类 Unix
- **不改动的**：MCP 协议层、工具对外响应结构、`config.yaml` 字段（v1 审计日志默认启用，不可配；将来需要开关 / 自定义路径 / 大小轮转时再开新 change）
