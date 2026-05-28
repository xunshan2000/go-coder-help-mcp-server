// Package tools 实现 Apipost MCP 工具的 handler 与 tool 定义。
// 共享逻辑（参数解析、默认 project_id / team_id 回落、响应包装）在 common.go。
package tools

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/mark3labs/mcp-go/mcp"

	"example.com/mcp-server/internal/tools/apipost/client"
)

// idPattern 与 config 中一致：资源 id 必须是字母 / 数字 / 下划线 / 连字符。
var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Deps 聚合 handler 闭包需要的依赖。
type Deps struct {
	Client *client.Client
}

// toolResponse 是每个 apipost_* 工具 MCP 响应 text 的 JSON 结构。
type toolResponse struct {
	Tool       string `json:"tool"`
	HTTPStatus int    `json:"http_status"`
	Truncated  bool   `json:"truncated,omitempty"`
	Data       any    `json:"data,omitempty"`
	Error      string `json:"error,omitempty"`
}

func successResponse(tool string, meta *client.ResponseMeta, data any) *mcp.CallToolResult {
	tr := toolResponse{Tool: tool, Data: data}
	if meta != nil {
		tr.HTTPStatus = meta.StatusCode
		tr.Truncated = meta.Truncated
	}
	buf, err := json.Marshal(tr)
	if err != nil {
		return mcp.NewToolResultErrorf("%s: marshal response: %v", tool, err)
	}
	return mcp.NewToolResultText(string(buf))
}

func errorResponse(tool string, meta *client.ResponseMeta, err error) *mcp.CallToolResult {
	tr := toolResponse{Tool: tool, Error: err.Error()}
	if meta != nil {
		tr.HTTPStatus = meta.StatusCode
		tr.Truncated = meta.Truncated
	}
	buf, mErr := json.Marshal(tr)
	if mErr != nil {
		return mcp.NewToolResultErrorf("%s: %v", tool, err)
	}
	return mcp.NewToolResultError(string(buf))
}

// readString 返回字符串参数；未传 / 类型不符返回空串。
func readString(args map[string]any, name string) string {
	v, ok := args[name]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// readStringDefault 从 args 中取字符串；若空，回落到 fallback。
func readStringDefault(args map[string]any, name, fallback string) string {
	if s := readString(args, name); s != "" {
		return s
	}
	return fallback
}

// readStringArray 按 JSON array(string) 读；非数组时返回 nil。
func readStringArray(args map[string]any, name string) []string {
	v, ok := args[name]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// readObject 读取一个 object 参数；非 object 返回 nil。
func readObject(args map[string]any, name string) map[string]any {
	v, ok := args[name]
	if !ok || v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// resolveProjectID 从参数或默认值取 project_id，并做格式白名单。
func resolveProjectID(args map[string]any, deps Deps) (string, error) {
	pid := readStringDefault(args, "project_id", deps.Client.DefaultProjectID())
	if pid == "" {
		return "", fmt.Errorf("project_id 必填（参数未传且 config.apipost.project_id 未设置）")
	}
	if !idPattern.MatchString(pid) {
		return "", fmt.Errorf("project_id 格式非法: %q", pid)
	}
	return pid, nil
}

// resolveTeamID 从参数或默认值取 team_id；空值被视为可选场景并返回空串。
func resolveTeamID(args map[string]any, deps Deps) (string, error) {
	tid := readStringDefault(args, "team_id", deps.Client.DefaultTeamID())
	if tid == "" {
		return "", nil
	}
	if !idPattern.MatchString(tid) {
		return "", fmt.Errorf("team_id 格式非法: %q", tid)
	}
	return tid, nil
}
