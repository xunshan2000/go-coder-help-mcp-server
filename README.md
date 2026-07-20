# mcp-server

一个用 Go 实现的可扩展 MCP（Model Context Protocol）服务端，通过 stdio 与 MCP 客户端（Claude Code、Claude Desktop 等）通信。项目采用工具注册中心模式 + 驱动接口抽象，每个 MCP 工具以独立子包形式接入，数据库相关工具通过 `Driver` 接口支持多种方言。

当前版本：`0.5.0`。内置工具：

- `db_list_tables` — 列出数据源下的用户表
- `db_describe_table` — 返回表的列与索引信息
- `db_query` — 执行只读 SQL（支持参数化、max_rows 限制、r 模式双重保护）
- `db_execute` — 执行写 SQL（仅 `write: true` 的源可用）
- `redis_scan_keys` — 使用 SCAN 搜索 Redis key
- `redis_get` — 查看 Redis key 的类型、TTL、大小和值
- `redis_set` — 写入 Redis string key（支持可选 TTL）
- `redis_delete` — 删除一个 Redis key
- `redis_hset` / `redis_hdel` — 写入或删除 Redis hash 字段
- `redis_lpush` / `redis_rpush` — 向 Redis list 左侧或右侧写入值
- `redis_sadd` / `redis_srem` — 写入或删除 Redis set 成员
- `redis_zadd` / `redis_zrem` — 写入或删除 Redis zset 成员

## 前置条件

- Go 1.21 或更高版本
- 可访问 Go 模块代理（国内网络环境下可使用 `https://goproxy.cn`）
- 至少配置并启用数据库、Redis 或 Apipost 中的一项

## 目录结构

```
go-mcp-server/
├── main.go                                # 入口：flag → config → pool → server → tools → stdio
├── config.yaml                            # 真实配置（.gitignore，不提交）
├── config.example.yaml                    # 配置模板，提交
├── bin/                                   # 所有构建产物（已 gitignore）
├── internal/
│   ├── config/
│   │   └── config.go                      # YAML 配置 + 校验
│   ├── server/
│   │   └── server.go                      # MCP server 的薄封装
│   └── tools/
│       ├── registry.go                    # 工具注册中心
│       ├── db/
│       │   ├── driver/                    # 驱动接口层（方言无关）
│       │   │   ├── driver.go              #   Driver / ReadOnlyExec 接口
│       │   │   ├── types.go               #   TableSchema / ColumnInfo / IndexInfo
│       │   │   └── registry.go            #   Register / Get / Names
│       │   ├── drivers/                   # 驱动实现（一驱动一子包）
│       │   │   └── mysql/
│       │   │       └── mysql.go           #   MySQL 驱动实现 + init() 自注册
│       │   ├── pool.go                    # 连接池
│       │   ├── sshtunnel/                 # SSH 隧道与 TCP 转发
│       │   ├── guard.go                   # SQL 守卫（首 token 白名单 + 多语句拒绝）
│       │   ├── list_tables.go             # db_list_tables
│       │   ├── describe_table.go          # db_describe_table
│       │   ├── query.go                   # db_query
│       │   ├── execute.go                 # db_execute
│       │   ├── render.go                  # JSON 结果 / 错误 helper
│       │   └── register.go                # blank import drivers/** + Register
│       └── redis/
│           ├── scan.go                    # redis_scan_keys
│           ├── get.go                     # redis_get
│           ├── write.go                   # redis_set / redis_delete
│           └── register.go
```

## 构建

```bash
go mod tidy

# Windows
go build -o bin/mcp-server.exe .

# macOS / Linux
go build -o bin/mcp-server .
```

产物统一输出到 `bin/`。如果拉取依赖超时，加国内代理：

```bash
GOPROXY=https://goproxy.cn,direct go mod tidy
GOPROXY=https://goproxy.cn,direct go build -o bin/mcp-server.exe .
```

## 配置

服务启动必须有一份 YAML 配置。通过 `--config <path>` 指定，缺省路径 `./config.yaml`。从 `config.example.yaml` 复制一份开始：

```bash
cp config.example.yaml config.yaml
# 编辑 config.yaml 填入真实连接参数
./bin/mcp-server.exe --config ./config.yaml
```

### 字段说明（通用）

| 字段 | 必填 | 缺省 | 说明 |
| --- | --- | --- | --- |
| `defaults.max_rows` | 否 | `100` | `db_query` 的行数上限；调用端 `max_rows` 参数只能降低不能抬高 |
| `defaults.query_timeout` | 否 | `30s` | 单次 SQL 超时（Go `time.Duration` 格式） |
| `features.database` | 否 | `true` | 是否初始化数据库连接并注册 `db_*` 工具 |
| `features.redis` | 否 | `true` | 是否初始化 Redis 连接并注册 `redis_*` 工具 |
| `features.apipost` | 否 | `true` | 是否初始化 Apipost 客户端并注册 `apipost_*` 工具 |
| `features.sql_audit` | 否 | `true` | 是否写 SQL 审计日志；不影响 `db_*` 工具注册 |
| `environments.<env>` | 是 | — | 环境名，自由字符串，例如 `pro`、`local`、`test1`、`test2` |
| `environments.<env>.databases.<key>` | 否 | — | 数据库逻辑名；同名 key 可在不同环境重复 |
| `environments.<env>.databases.<key>.driver` | 是 | — | 驱动名；目前支持 `mysql`，将来可扩展 |
| `environments.<env>.databases.<key>.host` | 是 | — | 数据库主机 |
| `environments.<env>.databases.<key>.database` | 是 | — | 实际库名 |
| `environments.<env>.databases.<key>.username` | 是 | — | |
| `environments.<env>.databases.<key>.password` | 否 | `""` | |
| `environments.<env>.databases.<key>.write` | 否 | `false` | 是否允许该数据源执行写操作；默认只读 |
| `environments.<env>.databases.<key>.params` | 否 | `{}` | `map[string]string`，透传驱动特定 DSN 参数；禁止 `multiStatements: "true"` |
| `environments.<env>.redis.<key>` | 否 | — | Redis 逻辑名；同名 key 可在不同环境重复 |
| `environments.<env>.redis.<key>.host` | 是 | — | Redis 主机 |
| `environments.<env>.redis.<key>.port` | 否 | `6379` | Redis 端口 |
| `environments.<env>.redis.<key>.auth` | 否 | `""` | Redis AUTH 密码 |
| `environments.<env>.redis.<key>.index` | 否 | `0` | Redis DB index |
| `environments.<env>.redis.<key>.write` | 否 | `false` | 是否允许该 Redis 数据源执行写操作；默认只读 |
| `environments.<env>.redis.<key>.dial_timeout` | 否 | `5s` | 建连超时 |
| `environments.<env>.redis.<key>.read_timeout` | 否 | `5s` | 单次命令读写 deadline |
| `environments.<env>.redis.<key>.ssh` | 否 | — | SSH 隧道；字段与 MySQL 的 `ssh` 配置一致 |

模块开关未配置时默认开启。显式设为 `false` 后，该模块的配置校验、网络初始化和工具注册都会跳过，便于保留暂时停用的连接配置。

### 环境维度

环境名完全由配置决定，不内置固定枚举。所有数据库和 Redis 工具都要求同时传入 `environment` 与 `source`；例如 `{"environment":"pro","source":"platform"}` 与 `{"environment":"local","source":"platform"}` 会定位到两个独立连接。`db_help` / `redis_help` 会向模型列出所有可用组合及各自的 `write_allowed`。

从 `0.4.x` 升级时，需要把原顶层 `databases` / `redis` 移入某个 `environments.<env>`，并把数据库的 `mode: rw` 改为 `write: true`、`mode: r` 改为 `write: false`（也可直接省略）。旧字段会被严格 YAML 校验拒绝，避免误以为写权限仍然生效。

### 驱动特有字段（MySQL）

| 字段 | 必填 | 缺省 | 映射目标 |
| --- | --- | --- | --- |
| `port` | 否 | `3306` | DSN TCP 端口 |
| `charset` | 否 | `utf8mb4` | DSN `charset` |
| `collation` | 否 | 服务端默认 | DSN `collation` |
| `timezone` | 否 | `UTC` | DSN `loc` 参数（**必须**是 Go `time.Location` 名，例如 `UTC` / `Local` / `Asia/Shanghai` / `Etc/GMT+5`；`+08:00` 等 offset 字符串不支持） |
| `params.parseTime` | 否 | `"true"` | 将 DATETIME 列 parse 为 `time.Time` |

### MySQL / Redis SSH 隧道

每个 MySQL 或 Redis 数据源可配置独立的 `ssh` 段。启用后，服务先连接 SSH 跳板机，在本机创建仅监听 `127.0.0.1` 的临时端口，再通过该端口访问原始 `host:port`。

```yaml
environments:
  pro:
    databases:
      platform:
        driver: mysql
        host: mysql.internal
        port: 3306
        database: platform
        username: app_ro
        password: db_password
        write: false
        ssh:
          host: bastion.example.com
          port: 22
          username: deploy
          private_key: .ssh/id_ed25519
          private_key_passphrase: key_passphrase
          known_hosts: .ssh/known_hosts
          connect_timeout: 10s
```

`password` 与 `private_key` 至少配置一个，也可以同时配置作为多个认证候选。`private_key` 和 `known_hosts` 的相对路径以配置文件所在目录为基准；未配置 `known_hosts` 时默认使用当前用户的 `~/.ssh/known_hosts`。生产环境应保持主机密钥校验，仅在明确接受中间人攻击风险时设置 `insecure_skip_host_key: true`。SSH 密码、私钥口令、数据库密码和完整 DSN 均不会写入启动日志。

### 连接池

| 字段 | 缺省 | 映射到 Go `*sql.DB` |
| --- | --- | --- |
| `pool.max_connections` | `10` | `SetMaxOpenConns` |
| `pool.min_connections` | `0` | `SetMaxIdleConns`（见下方脚注） |
| `pool.connect_timeout` | `10s` | 启动 Ping 超时 + MySQL DSN `timeout` |
| `pool.max_idle_time` | `0`（不限） | `SetConnMaxIdleTime` |

> **`min_connections` 与 hyperf 差异脚注**：hyperf swoole 下 `min_connections` 是启动时预建的连接数；Go `database/sql` 是懒连接，没有"预热"语义，我们把它映射到 `SetMaxIdleConns`（空闲连接上限）。两者稳态 QPS 相同，冷启动第一批请求在 Go 侧略慢。

### 多源与读写分离示例

```yaml
defaults:
  max_rows: 100
  query_timeout: 30s

environments:
  pro:
    databases:
      platform:
        driver: mysql
        host: pro.db.example
        database: platform
        username: app_rw
        password: <填你的>
        write: true

  local:
    databases:
      platform:
        driver: mysql
        host: 127.0.0.1
        database: platform
        username: app_ro
        password: <填你的>
        write: false
```

LLM 调用时用 `environment: "pro", source: "platform"` / `environment: "local", source: "platform"` 切换。

## 数据库工具

### `db_list_tables`

参数：
```json
{ "environment": "pro", "source": "platform" }
```
返回（text 中的 JSON）：
```json
{ "environment": "pro", "source": "platform", "tables": ["users", "orders", "..."] }
```

### `db_describe_table`

参数：
```json
{ "environment": "pro", "source": "platform", "table": "users" }
```
`table` 必须匹配 `^[A-Za-z0-9_$]+$`，否则被工具层白名单拦下（不触达数据库）。

返回：
```json
{
  "environment": "pro",
  "source": "platform",
  "table": "users",
  "columns": [
    {"name": "id", "type": "bigint(20)", "nullable": false, "key": "PRI", "default": null},
    ...
  ],
  "indexes": [
    {"name": "PRIMARY", "columns": ["id"], "unique": true},
    ...
  ]
}
```

### `db_query`

参数：
```json
{ "environment": "pro", "source": "platform", "sql": "SELECT * FROM users WHERE id = ?", "args": [1], "max_rows": 50 }
```
- `args` 通过 `?` 占位符参数化绑定（MySQL），永不拼入 SQL 字符串
- `max_rows` 可选；不能抬高全局上限 `defaults.max_rows`，只能调低

返回：
```json
{
  "environment": "pro",
  "source": "platform",
  "columns": ["id", "email"],
  "rows": [[1, "a@x.com"], [2, "b@x.com"]],
  "row_count": 2,
  "truncated": false
}
```

### `db_execute`

参数：
```json
{ "environment": "pro", "source": "platform", "sql": "INSERT INTO users (email) VALUES (?)", "args": ["a@x.com"] }
```
- 仅 `write: true` 的 source 可用；没有任何可写数据库源时，`db_execute` 不会注册
- 首关键字不能是 `SELECT`/`SHOW`/`DESCRIBE`/`DESC`/`EXPLAIN`（用 `db_query` 代替）

返回：
```json
{ "environment": "pro", "source": "platform", "rows_affected": 1, "last_insert_id": 42 }
```

### `redis_set`

Redis 写工具仅在至少一个 Redis 数据源配置 `write: true` 时注册，并且调用时仍会校验所选 `source` 的写权限。

参数：
```json
{ "environment": "pro", "source": "cache", "key": "user:1:name", "value": "Alice", "ttl_seconds": 3600 }
```

返回：
```json
{ "environment": "pro", "source": "cache", "key": "user:1:name", "ok": true, "ttl_seconds": 3600 }
```

### `redis_delete`

参数：
```json
{ "environment": "pro", "source": "cache", "key": "user:1:name" }
```

返回：
```json
{ "environment": "pro", "source": "cache", "key": "user:1:name", "deleted": 1 }
```

### Redis 结构写入

Hash：
```json
{ "environment": "pro", "source": "cache", "key": "user:1", "fields": { "name": "Alice", "role": "admin" } }
```
工具：`redis_hset`，返回 `fields_changed`。删除字段用 `redis_hdel`：
```json
{ "environment": "pro", "source": "cache", "key": "user:1", "fields": ["role"] }
```

List：
```json
{ "environment": "pro", "source": "cache", "key": "queue:jobs", "values": ["job-1", "job-2"] }
```
工具：`redis_lpush` 或 `redis_rpush`，返回写入后的 `length`。

Set：
```json
{ "environment": "pro", "source": "cache", "key": "user:1:tags", "members": ["vip", "beta"] }
```
工具：`redis_sadd`，返回 `members_added`。删除成员用 `redis_srem`。

ZSet：
```json
{ "environment": "pro", "source": "cache", "key": "rank:daily", "members": { "alice": 99.5, "bob": 88 } }
```
工具：`redis_zadd`，返回 `members_changed`。删除成员用 `redis_zrem`。

## SQL 审计日志

每次 `db_query` / `db_execute` 调用结束后（无论成功或失败），会被追加一条 JSON 记录到本地审计日志。用于事后排查、安全审计、慢查询分析。

- **位置**：配置文件所在目录下 `logs/sql-YYYY-MM-DD.log`（例如 `--config E:\code\go-mcp-server\config.yaml` 时写入 `E:\code\go-mcp-server\logs\`）
- **格式**：[JSONL](https://jsonlines.org/)（一行一条 JSON，`\n` 分隔）
- **滚动**：按**本地时区**日期滚动；跨日后下一次写入自动切到新文件
- **并发安全**：多 MCP 请求并发调用时每行原子完整
- **日志写失败不影响 MCP 响应**：写盘失败只在 stderr 打一行告警（不含 `sql` / `args`），MCP 调用正常返回

### 字段表

| 字段 | 类型 | 出现时机 | 说明 |
| --- | --- | --- | --- |
| `ts` | string | 始终 | `YYYY-MM-DD HH:MM:SS`（本地时区、秒级、无时区后缀） |
| `environment` | string | 始终 | 环境 key（环境未识别时仍会记原样输入） |
| `source` | string | 始终 | 数据源 key（source 未识别时仍会记原样输入） |
| `mode` | string | 始终 | 兼容字段，由 `write` 派生：`false` 为 `r`、`true` 为 `rw`；source 未命中时为 `""` |
| `tool` | string | 始终 | `db_query` / `db_execute` |
| `sql` | string | 始终 | 用户原始 SQL（含 `?` 占位符，**未内联**） |
| `sql_rendered` | string | 始终 | 驱动方言下 args 内联后的可读形式；**仅供人工阅读，不可执行** |
| `args` | array | 始终 | 占位符绑定值数组（字节按 base64） |
| `duration_ms` | integer | 始终 | handler 总耗时 |
| `ok` | boolean | 始终 | handler 是否未标记 isError |
| `rows` | integer | `db_query` 成功时 | 实际返回行数 |
| `truncated` | boolean | `db_query` 成功时 | 是否因 `max_rows` 被截断 |
| `rows_affected` | integer | `db_execute` 成功时 | |
| `last_insert_id` | integer | `db_execute` 成功时 | |
| `err` | string | `ok=false` 时 | 单行错误摘要（与 MCP 错误响应文本一致） |

### 示例行

```json
{"ts":"2026-05-06 10:44:24","environment":"pro","source":"platform","mode":"r","tool":"db_query","sql":"SELECT 1 AS one, ? AS two","sql_rendered":"SELECT 1 AS one, 42 AS two","args":[42],"duration_ms":266,"rows":1,"truncated":false,"ok":true}
{"ts":"2026-05-06 10:44:24","environment":"pro","source":"platform","mode":"r","tool":"db_query","sql":"UPDATE t SET x = ? WHERE id = ?","sql_rendered":"UPDATE t SET x = 'v' WHERE id = 1","args":["v",1],"duration_ms":0,"ok":false,"err":"source=pro/platform: 只允许只读语句..."}
```

### jq 分析示例

```bash
# 某天总调用次数
jq -s 'length' logs/sql-2026-05-06.log

# 失败调用
jq 'select(.ok==false)' logs/sql-2026-05-06.log

# 按耗时降序 top 10
jq -s 'sort_by(-.duration_ms) | .[0:10]' logs/sql-2026-05-06.log

# 某 source 的 SELECT 语句频次
jq 'select(.source=="default" and .tool=="db_query") | .sql_rendered' logs/sql-2026-05-06.log
```

> ⚠ **日志含真实 SQL 与 args 值，视同敏感文件**：`.gitignore` 已包含 `/logs/`；建议运维定期轮转归档 / 加密静态存储 / 访问权限按需最小化。详见"安全模型 > 审计日志"。

## Apipost 工具

可选接入 [Apipost 开放接口 V2](docs/apipost%20%E5%BC%80%E6%94%BE%E6%8E%A5%E5%8F%A3%E6%96%87%E6%A1%A3%20V2%E7%89%88%E6%9C%AC%EF%BC%88saas%E7%89%88%EF%BC%89.md)，让 LLM 在写 handler / 改接口定义时能直接读写 Apipost 中的 API 条目。当 `config.yaml` 含合法 `apipost` 段时注册 9 个工具；否则启动时 stderr 打印一行 `apipost not configured; skipping apipost tools`，不阻止进程启动。

### 配置

```yaml
apipost:
  base_url: https://v2-openapi.apipost.net      # 官方云 / 自托管域名
  api_token: apt_xxxxxxxxxxxxxxxxxxxxxxxx        # 个人 / 团队 Token
  project_name: 我的项目                          # Apipost 全局 Header 必填
  team_id: 1bfd2a3823426001                     # 可选，作为 list_projects 的默认 team
  project_id: 1bfd2a779bc26001                  # 可选，作为其他工具的默认 project
  request_timeout: 30s                          # 单次 HTTP 超时
  max_response_bytes: 10485760                  # 响应体字节上限（10 MiB）
```

| 字段 | 必填 | 缺省 | 说明 |
| --- | --- | --- | --- |
| `base_url` | 是 | — | 必须以 `http://` 或 `https://` 开头 |
| `api_token` | 是 | — | 注入为 `api-token` Header；**不会**出现在任何日志 / 错误 / 响应中 |
| `project_name` | 是 | — | 注入为 `project_name` Header；Apipost 所有开放接口都要求 |
| `team_id` | 否 | `""` | 可选默认 team，工具未传时使用 |
| `project_id` | 否 | `""` | 可选默认 project，工具未传时使用 |
| `request_timeout` | 否 | `30s` | |
| `max_response_bytes` | 否 | `10485760` | 超限响应截断并在工具响应标 `truncated: true` |

### 工具清单

| Tool | Method | Endpoint | 关键参数 |
| --- | --- | --- | --- |
| `apipost_list_projects` | GET | `/open/project/list` | `team_id?` / `action?`（默认 `0`） |
| `apipost_get_project` | GET | `/open/project/info` | `project_id?` |
| `apipost_list_apis` | GET | `/open/apis/list` | `project_id?` |
| `apipost_get_apis` | POST | `/open/apis/details` | `target_ids[]`（单个资源传 `[id]`） |
| `apipost_create_http_api` | POST | `/open/apis/create` | `body` = HTTP API 完整定义（`target_type` 自动补 `api`） |
| `apipost_update_api` | POST | `/open/apis/update` | `body` 含 `target_id` 的**完整**定义 |
| `apipost_delete_apis` | POST | `/open/apis/delete` | `target_ids[]` |
| `apipost_list_api_cases` | GET | `/open/apis/sample` | 可选 `target_ids[]` / `sample_ids[]` |
| `apipost_create_api_case` | POST | `/open/apis/sample/create` | `body` 含 `target_id`（所属接口）/ `name` / `method` / `url` / `request` |

### ⚠ 修改接口的陷阱

`apipost_update_api` 的 Apipost 端口设计是"非必填字段不传会被置默认值"。也就是说，直接传 patch 的字段（例如只改 `name`）会**把 `request` / `response` 等整块字段清空**。

正确姿势：**先 `apipost_get_apis` 取完整定义 → 在返回的 data 上做局部修改 → 整体作为 `body` 传给 `apipost_update_api`**。

工具 description 已明示此点；LLM 会被提示按这个流程走。

### 响应形态

成功：
```json
{
  "tool": "apipost_get_apis",
  "http_status": 200,
  "data": { "code": 0, "msg": "成功", "data": [ ... Apipost 原始数据 ... ] }
}
```

失败（MCP `isError: true`）：
```json
{
  "tool": "apipost_update_api",
  "http_status": 200,
  "error": "apipost: code=10020 msg=target_id 不存在"
}
```

`data` 字段**原样透传** Apipost 响应体（含 `{code, msg, data}` 结构），不重组。

### Token 最小权限

Apipost Token 是唯一授权凭据 —— LLM 误调 `apipost_delete_apis` 会直接生效。生产环境建议给一个**只读或项目范围内的最小权限 Token**，避免跨项目 / 跨团队的意外影响。

## 安全模型

本项目默认假设 LLM 产生的 SQL 不可完全信任，设计了多道防线：

1. **账号权限（用户自保障）**：生产环境建议每个 source 配**最小权限账号**；只读 source 用数据库层面的 read-only user，不依赖工具层独家防护
2. **读工具双重保护**：`db_query` 对所有数据源都执行以下保护，无论 `write` 是否开启：
   - **工具层首 token 白名单**：SQL 剥注释后取首关键字，必须 ∈ `{SELECT, SHOW, DESCRIBE, DESC, EXPLAIN, WITH, VALUES}`
   - **驱动层只读上下文**：MySQL 驱动的 `BeginReadOnly` 返回 `BeginTx(ReadOnly=true)`；即使白名单被绕过，`FOR UPDATE` / DML / DDL 等会被数据库直接拒绝
3. **多语句禁止**：DSN 不启用 `multiStatements`，且工具层字符串检查也拒绝；`params` 中禁止 `multiStatements: "true"`
4. **参数化**：`args` 一律作为占位符绑定，驱动负责 escape，永不字符串拼接
5. **超时**：所有 SQL 调用受 `defaults.query_timeout` 约束，超时会取消底层查询
6. **行数上限**：`db_query` 返回的 `rows` 最多 `defaults.max_rows`，LLM 无法绕过
7. **脱敏**：数据库密码、SSH 密码、私钥口令与组装后 DSN 不出现在任何日志或错误输出中
8. **SSH 主机校验**：SSH 隧道默认通过 `known_hosts` 校验跳板机；跳过校验必须显式配置

### 建议的数据库账号配置

- `write: false` 源：只授 `SELECT`，不给 `INSERT/UPDATE/DELETE/DROP/CREATE` 等
- `write: true` 源：按业务所需授予最小权限；避免给 `SUPER`、`FILE`（`INTO OUTFILE` 是 SELECT 但会写文件）等敏感权限
- 密码用强口令；`params.tls` 配置 TLS 连接（生产建议）

### 审计日志

参见上文"SQL 审计日志"一节。关键点：

- 日志默认启用；设置 `features.sql_audit: false` 可关闭，数据库工具仍正常工作
- 文件路径 `<配置文件所在目录>/logs/sql-*.log` 已被 `.gitignore` 默认忽略，防止业务 SQL / 参数值被意外 commit
- 视同敏感数据：建议**定期 rotate**、**离线归档**、**加密静态存储**；文件系统权限 `0640`（属主可读写、同组只读）
- 日志写失败**不会**让 MCP 请求失败：磁盘满 / 目录只读等降级到 stderr 一行告警，不包含 SQL 或 args 内容

### Apipost Token 处理

- `api_token` 注入为 `api-token` HTTP Header；**不出现**在 stderr 启动日志、MCP 响应、工具错误文本、进程任何输出
- 所有 HTTP 错误路径在返回前经过 `sanitize()` 把 Token 明文替换为 `***`
- `project_name` 作为 Apipost 全局必填 Header，也从 config 统一注入；项目名一般不算密，但同样不主动回显
- 响应体大小上限（默认 10 MiB）防止导出整项目或误查大数据集时撑爆 LLM 上下文

## 添加新驱动

以加入 PostgreSQL 为例，三步：

**1. 新建实现包** `internal/tools/db/drivers/pgsql/pgsql.go`，实现 `driver.Driver` 接口：

```go
package pgsql

import (
    "context"
    "database/sql"
    _ "github.com/jackc/pgx/v5/stdlib"  // blank import 底层 database/sql 驱动

    "example.com/mcp-server/internal/config"
    "example.com/mcp-server/internal/tools/db/driver"
)

func init() { driver.Register(&Driver{}) }

type Driver struct{}

func (Driver) Name() string          { return "pgsql" }
func (Driver) SQLDriverName() string { return "pgx" }
func (Driver) BuildDSN(cfg config.SourceConfig) (string, error) {
    // 组装 PG DSN: "postgres://user:pass@host:port/db?..."
}
func (Driver) QuoteIdentifier(name string) string {
    return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
func (Driver) ListTables(ctx context.Context, db *sql.DB) ([]string, error) {
    // 例如 SELECT tablename FROM pg_tables WHERE schemaname = current_schema()
}
func (Driver) DescribeTable(ctx context.Context, db *sql.DB, table string) (*driver.TableSchema, error) {
    // 查 information_schema.columns / pg_indexes，映射为 TableSchema
}
func (Driver) BeginReadOnly(ctx context.Context, db *sql.DB) (driver.ReadOnlyExec, error) {
    tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
    // PG 原生支持只读事务
}

// RenderSQL 把 ? 占位符按方言内联为字面量，供审计日志 sql_rendered 字段 / 人工阅读；
// 不可作为 SQL 执行。最简实现可直接 return sql（此时审计中 sql_rendered 保持占位符形式）。
func (Driver) RenderSQL(sql string, args []any) string {
    // 例如 PG 用 $1/$2 占位：本项目工具层统一用 ?，PG 驱动可实现 ? → 字面量的方言渲染
    return sql
}
```

**不支持只读事务的驱动**（例如 ClickHouse）的 `BeginReadOnly`：

```go
func (Driver) BeginReadOnly(ctx context.Context, db *sql.DB) (driver.ReadOnlyExec, error) {
    return &fakeROExec{db: db}, nil  // 返回对 *sql.DB 的包装，Commit/Rollback 为 no-op
}
// ⚠ 此时只读保证完全依赖数据库账号权限；在驱动包 README 中必须明示
```

**2. 激活驱动**：在 `internal/tools/db/register.go` 加一行 blank import：

```go
import (
    _ "example.com/mcp-server/internal/tools/db/drivers/mysql"
    _ "example.com/mcp-server/internal/tools/db/drivers/pgsql"  // ← 新增
)
```

**3. 用户配置**：`config.yaml` 中写 `driver: pgsql` 即可使用。

工具层（`list_tables.go` / `describe_table.go` / `query.go` / `execute.go` / `pool.go` / `guard.go`）**无需任何修改**，它们只依赖 `driver.Driver` 接口。

## 运行

服务监听 stdio，通常由 MCP 客户端作为子进程拉起；直接在终端运行它会进入等待输入状态。

手动冒烟测试：

```bash
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
  | ./bin/mcp-server.exe --config ./config.yaml
```

期望看到已启用模块的工具列表，以及 `serverInfo.name == "mcp-server"` / `"version":"0.5.0"` 的 `initialize` 响应。

## 在 MCP 客户端中注册

以 Claude Code 的 `.mcp.json` 为例：

### Windows

```json
{
  "mcpServers": {
    "mcp-server": {
      "command": "D:\\absolute\\path\\to\\go-mcp-server\\bin\\mcp-server.exe",
      "args": ["--config", "D:\\absolute\\path\\to\\go-mcp-server\\config.yaml"]
    }
  }
}
```

### macOS / Linux

```json
{
  "mcpServers": {
    "mcp-server": {
      "command": "/absolute/path/to/go-mcp-server/bin/mcp-server",
      "args": ["--config", "/absolute/path/to/go-mcp-server/config.yaml"]
    }
  }
}
```

⚠ **一定要显式传 `--config` 的绝对路径**：MCP 客户端拉起进程时 cwd 可能不是项目根目录，`./config.yaml` 的默认路径不一定能解析到。

## 如何新增一个普通工具（非数据库）

以新增 `health` 工具为例：

1. **新建子包**：`internal/tools/health/health.go`，包名 `health`
2. **实现 `Register`**：在 `health.go` 中定义 `func Register(r *tools.Registry)`，内部用 `mcp.NewTool("health", ...)` 构造工具并通过 `r.Add(tool, handler)` 注册
3. **在 `main.go` 调用**：加一行 `health.Register(reg)`，并 import `"example.com/mcp-server/internal/tools/health"`

数据库工具因涉及方言差异走驱动接口层，流程不同（见"添加新驱动"一节）。
