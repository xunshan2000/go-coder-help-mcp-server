## Context

现状：整个项目就一个文件 `main.go`（67 行），直接在 `main` 函数里 `NewMCPServer` → `AddTool(add)` → `ServeStdio`。Module 名 `example.com/mcp-add`、服务名 `mcp-add`、二进制名 `mcp-add.exe` 都把"add"绑进了项目身份。

后续规划要加多种 MCP 工具，第一个就是数据库访问工具（另开 `add-db-tools` change），后面可能还有文件系统、HTTP 等。如果继续堆到 `main.go`，很快会出现：工具定义、参数校验、业务逻辑、错误处理全部混在一起；每个工具的测试无法单独跑；module 名/服务名不再反映项目意图。

约束：
- 保持 add 工具对外行为不变（已写进 `openspec/specs/mcp-add/spec.md`）
- 不引入 MCP SDK 之外的新依赖
- 不改变通信方式（继续 stdio）
- Windows / 类 Unix 双平台都要可构建

## Goals / Non-Goals

**Goals:**
- 建立一个"多工具友好"的 Go 项目骨架：新增一个工具 ≈ 新增一个 `internal/tools/<name>/` 包 + 在一处登记
- 把 `main.go` 瘦身到只承担 "组装 server、注册工具、启动 stdio" 三件事
- 让每个工具子包对外只暴露一个 `Register(r *tools.Registry)` 入口，把该工具的定义、schema、handler 都封装在包内
- 把项目身份从"add 专用"升级为"通用 MCP server"：module 改名、服务名改名、二进制改名

**Non-Goals:**
- 不引入 YAML 配置（没东西要配，留给 `add-db-tools`）
- 不引入任何新的工具业务（add 之外的工具一律留给后续 change）
- 不引入插件化 / 动态加载（所有工具仍在编译期静态注册）
- 不做自动化测试框架改造（不引入 `go test` 单测，除非实现过程中确有需要）
- 不改变 MCP 协议层的任何行为（initialize 响应、tools/list、tools/call 语义保持）

## Decisions

### 1. 采用 `internal/` 分层而非平铺多个顶层包

目录布局：

```
go-mcp-server/
├── main.go                          // 仅入口：构造 server → 注册工具 → ServeStdio
├── go.mod                           // module example.com/mcp-server
├── bin/                             // 所有构建产物（mcp-server / mcp-server.exe），不入 git
├── internal/
│   ├── server/
│   │   └── server.go                // NewServer(name, version) *server.MCPServer 的薄封装
│   └── tools/
│       ├── registry.go              // 工具注册中心：Registry 类型 + Register 方法
│       └── add/
│           └── add.go               // add 工具：定义 + handler + 参数解析全部在此
```

**Rationale**：
- `internal/` 防止其它 module 误引用（明确这是应用层私有实现）
- `server/` 子包只做 MCP server 构造的薄封装，便于测试与未来加入共享中间件（如日志、统一错误包装）
- `tools/<name>/` 一个工具一个包，彼此零依赖，删除 / 禁用只需不调用 `Register`

**Alternatives considered**：
- 平铺 `add.go` / `server.go` 到根目录：短期简单，但数据库工具进来后 `db.go` + 连接池 + 多个 handler 会再次膨胀。拒绝。
- `cmd/mcp-server/main.go` 的 Go 标准布局：适合将来要发 library + 多个 binary；目前只有一个 binary，过度设计。拒绝。

### 2. 工具注册中心 (`tools.Registry`) 做薄包装

```go
// internal/tools/registry.go
package tools

import "github.com/mark3labs/mcp-go/server"

type Registry struct{ srv *server.MCPServer }

func NewRegistry(srv *server.MCPServer) *Registry { return &Registry{srv: srv} }

func (r *Registry) Add(tool mcp.Tool, handler server.ToolHandlerFunc) {
    r.srv.AddTool(tool, handler)
}
```

每个工具子包对外暴露：
```go
// internal/tools/add/add.go
func Register(r *tools.Registry) {
    r.Add(mcp.NewTool("add", ...), handleAdd)
}
```

`main.go` 最终形态：
```go
srv := server.New("mcp-server", "0.2.0")
reg := tools.NewRegistry(srv)
add.Register(reg)
// 将来：db.Register(reg, cfg)
server.Serve(srv)
```

**Rationale**：
- 工具子包不需要 import `mark3labs/mcp-go/server` 的内部细节，只需 `*tools.Registry`，测试替身（mock）也简单
- 未来若要统一加"每次工具调用前记录日志"这样的横切逻辑，只要改 `Registry.Add` 一处
- 保持"薄"，不引入 middleware / 插件接口这种过早抽象

**Alternatives considered**：
- 每个工具自己调用 `srv.AddTool`，main 只是调用各工具的 `Init(srv)`。功能上等价，但 `Registry` 类型给未来横切留了位置，成本几乎为零，保留。
- 使用 Go init() 隐式注册：反模式，删除一个工具需要改 `import _` 语句，不够显式。拒绝。

### 3. Module 改名 → 同步改服务名、二进制名、统一输出到 `bin/`

- `go.mod`: `module example.com/mcp-server`
- `server.NewMCPServer("mcp-server", "0.2.0", ...)`
- 构建命令：`go build -o bin/mcp-server.exe .`（Windows）/ `go build -o bin/mcp-server .`（类 Unix）
- 仓库根目录新增 `bin/` 子目录，所有构建产物统一放此处，不再散落在根目录
- `.gitignore` 忽略整个 `bin/` 目录（而不是单独列出每个平台的产物名）

**Rationale**：
- module 名、服务名、二进制名三者保持一致，避免未来工具扩展时出现"module 叫 mcp-server，服务名还叫 mcp-add"这种错配
- 版本号从 `0.1.0` 升到 `0.2.0` 标记此次结构性变更，MCP 客户端可用版本号判断接入兼容性
- 产物统一到 `bin/` 子目录：(a) 仓库根目录保持整洁；(b) 多平台 / 多 target 构建（将来 `go build -o bin/mcp-server-linux-amd64 ...`）有自然落点；(c) `.gitignore` 只需忽略一个目录

**Alternatives considered**：
- 只改 module 不改服务名：减少客户端迁移成本，但留下命名不一致的技术债。拒绝。
- 升到 `1.0.0`：表示稳定 API，但当前还在快速演进，保留 `0.x`。拒绝。
- 继续把产物放仓库根目录：短期简单，但 `.gitignore` 要列具体文件名，后续加平台 target 会越来越乱。拒绝。
- 采用 `cmd/<name>/main.go` 多 binary 布局：为将来"一个服务一个进程"做准备。目前已明确只做单 binary（模式 A），过度设计。拒绝。

### 4. Go 版本保持 1.21+，不升级

`go.mod` 当前声明 `go 1.25.5`，已远高于原始需求的 1.21。本次重构不动 Go 版本声明，避免引入与本次目标无关的差异。

## Risks / Trade-offs

- **[风险] MCP 客户端升级滞后**：`.mcp.json` 中 `command` 还指向 `mcp-add.exe`，用户升级本项目后如果没同步修改 `.mcp.json` 会拉起失败 → **缓解**：在 README 中单独开一节"从 0.1.x 升级到 0.2.0"给出 `.mcp.json` 改动示例；`proposal.md` 已把它列为 BREAKING

- **[风险] 旧二进制残留**：仓库根目录当前有一个已构建的 `mcp-add.exe`。新骨架下应构建到 `bin/mcp-server.exe`，但若不清理 `mcp-add.exe` 会造成残留、误用，或者某些构建命令仍然把产物写到根目录 → **缓解**：实现 task 中明确删除根目录下的 `mcp-add.exe`；在 `.gitignore` 中加入整个 `bin/` 目录，并同时加入历史遗留 `mcp-*.exe` / `mcp-*`（根目录）以防再次污染

- **[权衡] `internal/server` 薄包装层**：现在看确实只是把一行 `server.NewMCPServer(...)` 再包一层，看起来"过度"。但有两个落点值得占位：(a) 将来接入配置加载时，这里是 "根据 config 组装 server" 的自然位置；(b) 将来要对所有工具做统一日志 / 追踪时，这里是注入 hook 的点。保留这层一个文件的成本可接受

- **[权衡] 不做单元测试**：理论上重构之后应该加单测保障行为不变。但 add 工具已有 `openspec/specs/mcp-add/` 描述的手动验证场景，且单测引入需要另一层设计（mock `*server.MCPServer`）。本 change 明确非目标，若重构后手动验证失败再补测

## Migration Plan

1. 先改 `go.mod` module 名 → `go mod tidy` → `go build` 确认编译（此时 `main.go` 还没拆，只是改 import 路径，没有的话直接 build）
2. 新建 `internal/tools/registry.go`、`internal/tools/add/add.go`、`internal/server/server.go`
3. 把 `main.go` 里的 add 工具逻辑搬到 `internal/tools/add/add.go`，`readNumber` 作为该包私有函数
4. `main.go` 改为调用 `server.New` + `tools.NewRegistry` + `add.Register` + `server.Serve`
5. `go build -o bin/mcp-server.exe .`，确认产物出现在 `bin/` 目录下
6. 手动运行 `bin/mcp-server.exe`，用 JSON-RPC 重跑 `openspec/specs/mcp-add/spec.md` 中的全部 scenario（initialize、tools/list、4 个 add 成功用例、3 个错误用例）
7. 删除旧 `mcp-add.exe`（根目录），新增 / 更新 `.gitignore` 忽略 `bin/`，更新 `README.md` 和 `.mcp.json`

**Rollback**：全部改动都在单一分支 / 未发布，`git checkout` 即回滚。MCP 客户端侧只要把 `.mcp.json` 的 `command` 改回旧二进制（若保留）即可恢复。

## Open Questions

- 是否要保留一个兼容 shim（让旧的 `mcp-add` 服务名也能被识别）？当前决定 **不保留**：0.x 语义下 breaking 可以接受，且 MCP 客户端通过 `command` 拉起进程，客户端看不到旧服务名是否存在。若实际迁移中出现阻塞可再开新 change。
