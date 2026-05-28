package server

import (
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func New(name, version string) *mcpserver.MCPServer {
	return mcpserver.NewMCPServer(
		name,
		version,
		mcpserver.WithToolCapabilities(true),
	)
}

func Serve(srv *mcpserver.MCPServer) error {
	return mcpserver.ServeStdio(srv)
}
