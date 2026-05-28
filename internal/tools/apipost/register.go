// Package apipost 聚合 Apipost MCP 工具的注册入口。
// 由 main.go 在 config.apipost 段存在时调用 Register。
package apipost

import (
	"example.com/mcp-server/internal/tools"
	"example.com/mcp-server/internal/tools/apipost/client"
	apiposttools "example.com/mcp-server/internal/tools/apipost/tools"
)

// Register 注册全部 9 个 apipost_* 工具到 registry。
func Register(r *tools.Registry, c *client.Client) {
	deps := apiposttools.Deps{Client: c}
	apiposttools.RegisterProjects(r.Add, deps)
	apiposttools.RegisterApis(r.Add, deps)
	apiposttools.RegisterCases(r.Add, deps)
}
