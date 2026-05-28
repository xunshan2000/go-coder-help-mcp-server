package tools

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type Registry struct {
	srv *server.MCPServer
}

func NewRegistry(srv *server.MCPServer) *Registry {
	return &Registry{srv: srv}
}

func (r *Registry) Add(tool mcp.Tool, handler server.ToolHandlerFunc) {
	r.srv.AddTool(tool, handler)
}
