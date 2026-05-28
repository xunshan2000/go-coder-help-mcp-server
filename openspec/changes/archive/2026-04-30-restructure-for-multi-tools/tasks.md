## 1. Module 与项目骨架

- [x] 1.1 将 `go.mod` 中的 `module example.com/mcp-add` 改为 `module example.com/mcp-server`
- [x] 1.2 运行 `go mod tidy`，确认 `go.sum` 正常更新、无多余依赖变动
- [x] 1.3 新建目录结构 `internal/server/`、`internal/tools/`、`internal/tools/add/`

## 2. 工具注册中心

- [x] 2.1 在 `internal/tools/registry.go` 定义 `Registry` 类型，封装 `*server.MCPServer`；对外暴露 `NewRegistry(srv *server.MCPServer) *Registry` 与 `Add(tool mcp.Tool, handler server.ToolHandlerFunc)`
- [x] 2.2 保证 `tools` 包仅依赖 `github.com/mark3labs/mcp-go/mcp` 与 `.../server`，不依赖具体工具子包

## 3. 搬迁 add 工具

- [x] 3.1 在 `internal/tools/add/add.go` 创建 `add` 工具的完整实现：`mcp.NewTool("add", ...)` 定义、`handleAdd` handler、私有 `readNumber` 辅助函数；描述文本与原 `main.go` 保持一致
- [x] 3.2 对外暴露 `Register(r *tools.Registry)`：内部调用 `r.Add(...)` 完成注册；不在包内直接依赖 `*server.MCPServer`
- [x] 3.3 保证 `add` 工具对外契约与 `openspec/specs/mcp-add/spec.md` 全部需求一致（名称 `add`、`a`/`b` 必填 number、四个数值场景、三个错误场景）

## 4. Server 薄封装

- [x] 4.1 在 `internal/server/server.go` 实现 `New(name, version string) *server.MCPServer`，内部调用 `server.NewMCPServer(name, version, server.WithToolCapabilities(true))`
- [x] 4.2 在同文件实现 `Serve(srv *server.MCPServer) error`，内部调用 `server.ServeStdio(srv)`（保留后续加入日志 / 优雅退出 hook 的位置）

## 5. 瘦身 main.go

- [x] 5.1 重写 `main.go` 使其仅包含：`server.New("mcp-server", "0.2.0")` → `tools.NewRegistry(srv)` → `add.Register(reg)` → `server.Serve(srv)` + 错误处理
- [x] 5.2 确认 `main.go` 不再出现任何 `a`/`b` 参数解析、数值运算代码，也不再直接 import `mcp-go/mcp`（只 import 项目内 `internal/server`、`internal/tools` 与 `internal/tools/add`）

## 6. 构建产物与旧产物清理

- [x] 6.1 在仓库根目录创建空的 `bin/` 目录（构建产物输出位置）
- [x] 6.2 在 Windows 上执行 `go build -o bin/mcp-server.exe .`，确认编译通过、产物生成在 `bin/` 目录下
- [x] 6.3 删除仓库根目录中旧的 `mcp-add.exe`（确保根目录不再残留任何 `mcp-*.exe` / `mcp-*` 构建产物）
- [x] 6.4 新增或更新仓库根目录的 `.gitignore`，至少忽略整个 `bin/` 目录；若当前没有 `.gitignore`，新建一个

## 7. 手动验证

- [x] 7.1 运行 `bin/mcp-server.exe`，向 stdin 注入 `initialize` JSON-RPC 请求，确认返回的 `serverInfo.name == "mcp-server"`、`serverInfo.version == "0.2.0"`
- [x] 7.2 调用 `tools/list`，确认响应包含且仅包含 `name == "add"` 的一个条目，其 `inputSchema` 声明 `a`、`b` 为必填 number
- [x] 7.3 依次调用 `tools/call` 验证 `{a:2,b:3}` → `"5"`、`{a:1.5,b:2.25}` → `"3.75"`、`{a:-10,b:4}` → `"-6"`、`{a:0,b:0}` → `"0"`
- [x] 7.4 依次验证三个错误场景：`{b:3}`、`{a:"hello",b:3}`、`{}`，响应均被标记为错误且进程继续运行
- [x] 7.5 关闭 stdin，确认进程干净退出且退出码为 0

## 8. 文档与客户端配置

- [x] 8.1 更新 `README.md`：构建命令统一写作 `go build -o bin/mcp-server.exe .` / `go build -o bin/mcp-server .`；运行命令统一写作 `bin/mcp-server(.exe)`；服务名/版本描述均改为 `mcp-server` / `0.2.0`；新增"从 0.1.x 升级到 0.2.0"一节，说明 `.mcp.json` 中 `command` 需要改名并改为 `bin/` 路径
- [x] 8.2 更新根目录 `.mcp.json`：将 `command` 指向新的 `bin/mcp-server.exe`（Windows）或 `bin/mcp-server`（类 Unix）
- [x] 8.3 在 README 中明确列出当前目录结构（`bin/`、`internal/server`、`internal/tools/<name>/`）以及"如何新增一个工具"的三步说明（新建子包、实现 `Register`、在 `main.go` 调用 `Register`）
