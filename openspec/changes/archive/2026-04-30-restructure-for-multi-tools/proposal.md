## Why

当前项目只有单文件 `main.go`，`module` 名、服务名、可执行文件名都绑在 `add` 工具上。为了承载后续多种 MCP 能力（数据库访问工具、其它业务工具），需要先把代码结构规范化，建立可扩展的工具注册骨架，之后每新增一个工具只需新增包并注册，不再需要修改主流程。本次改动只做结构重构，不引入任何新的业务工具或新的运行时依赖。

## What Changes

- **BREAKING** 将 Go module 名从 `example.com/mcp-add` 更名为 `example.com/mcp-server`
- **BREAKING** 将 MCP `initialize` 响应中的服务名从 `mcp-add` 改为 `mcp-server`；版本号从 `0.1.0` 升到 `0.2.0`
- **BREAKING** 构建产物可执行文件从 `mcp-add.exe` / `mcp-add` 改名为 `mcp-server.exe` / `mcp-server`，并统一输出到仓库根目录的 `bin/` 子目录（即 `bin/mcp-server.exe` / `bin/mcp-server`）；客户端 `.mcp.json` 中的 `command` 路径需要相应更新
- 在仓库根目录引入 `bin/` 目录作为所有构建产物的统一输出位置，并通过 `.gitignore` 避免将产物提交入库
- 引入 `internal/` 分层：
  - `internal/server`：MCP server 的构造与 stdio 启动逻辑
  - `internal/tools`：工具注册中心（`Registry`）以及各工具子包
  - `internal/tools/add`：把现有 `add` 工具的定义、handler、参数解析搬到此包，行为不变
- 提供一个统一的工具注册入口：`main.go` 只负责构造 server、调用各工具子包的 `Register(registry)`、启动 stdio 服务，不再直接声明任何具体工具
- 更新 `README.md` 的构建 / 运行 / 客户端接入说明以反映新名称
- 更新根目录 `.mcp.json` 示例以反映新的可执行文件名

## Capabilities

### New Capabilities
- `server-core`: MCP 服务端入口、整体服务身份（服务名 / 版本）、工具注册机制、stdio 生命周期

### Modified Capabilities
（无。`mcp-add` 现有需求行为保持不变，只是实现位置搬迁。服务名不再是 `mcp-add` 但原 `mcp-add` spec 并未规定具体名称字符串，不构成需求冲突。）

## Impact

- **代码**：`main.go` 拆分为 `internal/server`、`internal/tools/add` 等包；新增 `internal/tools` 注册中心；module 名修改导致所有 `import` 路径变化
- **构建产物**：可执行文件名变化（`mcp-add.exe` → `bin/mcp-server.exe`），且统一输出到 `bin/` 目录，`bin/` 需加入 `.gitignore`
- **客户端**：使用本服务的 MCP 客户端（如 Claude Code 的 `.mcp.json`）需要同步修改 `command` 指向新的 `bin/mcp-server.exe` / `bin/mcp-server`
- **依赖**：不新增任何依赖（继续使用 `github.com/mark3labs/mcp-go`）
- **后续 change**：为 `add-db-tools`（数据库访问 MCP）提供接入点；数据库工具将作为一个新的 `internal/tools/db` 子包挂到同一个 `Registry`
