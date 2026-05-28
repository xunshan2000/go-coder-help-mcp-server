## ADDED Requirements

### Requirement: 统一的服务身份
系统 SHALL 在 MCP `initialize` 响应中声明自身的服务名为字符串 `mcp-server`，版本号为字符串 `0.2.0`；服务名与版本号 MUST NOT 与某个具体工具的名称绑定。

#### Scenario: initialize 返回的服务名
- **WHEN** 客户端通过 stdio 向可执行文件发送合法的 `initialize` 请求
- **THEN** 响应的服务信息 MUST 声明服务名 `mcp-server`
- **AND** 响应的服务信息 MUST 声明版本号 `0.2.0`

#### Scenario: 服务名不绑定具体工具
- **WHEN** `initialize` 响应被任意一个兼容的 MCP 客户端解析
- **THEN** 返回的服务名 MUST NOT 为 `mcp-add` 或任何具体工具的名称

### Requirement: 多工具可扩展注册
系统 SHALL 支持在同一个运行实例中注册任意数量（>=1）的 MCP 工具，且 `tools/list` 的响应 MUST 正确反映当前所有已注册工具；新增工具 MUST NOT 要求修改与工具无关的其它代码路径。

#### Scenario: 当前仅有 add 工具
- **WHEN** 客户端在 `initialize` 之后调用 `tools/list`
- **THEN** 响应列表 MUST 至少包含一个条目
- **AND** 响应列表 MUST 包含 `name` 为 `add` 的条目，其行为与 `mcp-add` 能力下定义的 `add 工具注册` 需求一致

#### Scenario: 未来新增工具不影响 add
- **WHEN** 代码库中新增任意其它工具并注册
- **THEN** `tools/list` MUST 同时返回 `add` 条目与新工具条目，且 `add` 工具的 `name`、`description`、`inputSchema` MUST 与新增前保持一致

### Requirement: 可执行文件命名与输出位置
项目构建产物 SHALL 统一输出到仓库根目录下的 `bin/` 子目录，并统一命名：Windows 平台为 `bin/mcp-server.exe`，其它平台为 `bin/mcp-server`；构建产物 MUST NOT 以任何单一工具的名称（例如 `mcp-add`）命名，MUST NOT 直接写入仓库根目录，且 `bin/` 目录 MUST 被纳入版本控制忽略列表。

#### Scenario: Windows 构建产物
- **WHEN** 在 Windows 环境下执行 `go build -o bin/mcp-server.exe .`
- **THEN** 构建 MUST 成功并在仓库根目录下的 `bin/` 子目录中产出名为 `mcp-server.exe` 的可执行文件
- **AND** 执行 `bin/mcp-server.exe` MUST 正常启动 MCP 服务（`initialize` 可返回合法响应）

#### Scenario: 类 Unix 构建产物
- **WHEN** 在 Linux 或 macOS 环境下执行 `go build -o bin/mcp-server .`
- **THEN** 构建 MUST 成功并在仓库根目录下的 `bin/` 子目录中产出名为 `mcp-server` 的可执行文件
- **AND** 执行 `bin/mcp-server` MUST 正常启动 MCP 服务

#### Scenario: 构建产物不污染仓库
- **WHEN** 审阅 `.gitignore` 与仓库跟踪文件
- **THEN** `.gitignore` MUST 忽略整个 `bin/` 目录
- **AND** 仓库的跟踪文件列表 MUST NOT 包含 `bin/` 下的任何可执行文件
- **AND** 仓库根目录（`bin/` 以外的位置）MUST NOT 存在 `mcp-*.exe` 或 `mcp-*`（无扩展名）构建产物

### Requirement: 入口瘦身
`main.go` SHALL 仅承担三项职责：构造 MCP server 实例、向服务注册所有工具、启动 stdio 传输；`main.go` MUST NOT 包含任何具体工具的参数 schema 定义、参数解析逻辑或业务计算逻辑。

#### Scenario: main.go 不包含工具实现细节
- **WHEN** 审阅 `main.go` 的源码
- **THEN** `main.go` MUST NOT 出现任何工具参数（如 `a`、`b`）的解析或数值运算代码
- **AND** 每个具体工具的注册 MUST 通过调用该工具所在子包导出的 `Register` 函数完成
