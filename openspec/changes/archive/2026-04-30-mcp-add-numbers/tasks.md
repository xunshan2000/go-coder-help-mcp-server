## 1. 工程初始化

- [x] 1.1 在仓库根目录执行 `go mod init <module-path>`，生成 `go.mod`（建议 module 名为 `example.com/mcp-add` 或根据用户偏好）
- [x] 1.2 通过 `go get github.com/mark3labs/mcp-go@latest` 引入 MCP Go SDK，确认 `go.sum` 同步生成
- [x] 1.3 在 `go.mod` 中确认最低 Go 版本为 1.21

## 2. 核心实现

- [x] 2.1 创建 `main.go`，在 `main` 函数中初始化 MCP Server 实例（包含服务名 `mcp-add` 与版本号 `0.1.0`）
- [x] 2.2 定义 `add` 工具的输入 schema：两个必填 `number` 类型参数 `a` 与 `b`，并附上工具描述
- [x] 2.3 实现 `add` 工具的 handler：从请求中读取 `a`、`b`，做类型校验，合法时返回 `a + b` 的文本内容
- [x] 2.4 在 handler 中处理非法输入（缺参、类型错误），返回 `isError: true` 的 MCP 响应，不让进程崩溃
- [x] 2.5 使用 SDK 提供的 stdio 传输方式启动服务，确保主函数在收到客户端关闭后干净退出（退出码 0）

## 3. 验证

- [x] 3.1 `go build ./...` 编译通过，产出二进制（Windows 下为 `mcp-add.exe`）
- [x] 3.2 手动启动二进制，向 stdin 注入 `initialize` JSON-RPC 请求，确认返回合法 `initialize` 响应
- [x] 3.3 通过 `tools/list` 请求，确认返回列表中包含 `name` 为 `add` 的条目
- [x] 3.4 通过 `tools/call` 分别验证：`{a:2,b:3}` → `"5"`；`{a:1.5,b:2.25}` → `"3.75"`；`{a:-10,b:4}` → `"-6"`；`{a:0,b:0}` → `"0"`
- [x] 3.5 验证非法输入：缺少 `a`、`a` 为字符串、空对象 `{}` 三种情况下响应被标记为错误且进程继续运行

## 4. 文档与客户端集成

- [x] 4.1 在 `README.md`（或 `openspec/changes/mcp-add-numbers/` 下的说明中）记录构建与运行命令
- [x] 4.2 提供一份 MCP 客户端（如 Claude Code 的 `.mcp.json`）注册示例，覆盖 Windows 与类 Unix 平台的可执行路径差异
- [x] 4.3 在文档中提示：`add` 工具适用于 ±2^53 以内的数值，超出范围可能丢失精度
