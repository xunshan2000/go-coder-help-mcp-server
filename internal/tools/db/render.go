package db

import (
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

func renderJSONResult(v any) *mcp.CallToolResult {
	buf, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultErrorf("JSON 序列化失败: %v", err)
	}
	return mcp.NewToolResultText(string(buf))
}

func renderErrorf(format string, args ...any) *mcp.CallToolResult {
	return mcp.NewToolResultErrorf(format, args...)
}
