package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// --- apipost_list_api_cases ---

func newListAPICasesTool() mcp.Tool {
	return mcp.NewTool("apipost_list_api_cases",
		mcp.WithDescription("列出接口用例（批量详情）。GET /open/apis/sample；可选过滤 target_ids / sample_ids"),
		mcp.WithString("project_id",
			mcp.Description("项目 id；不传则使用 config.apipost.project_id"),
		),
		mcp.WithArray("target_ids",
			mcp.Description("按接口 target_id 过滤；可选"),
			mcp.Items(map[string]any{"type": "string"}),
		),
		mcp.WithArray("sample_ids",
			mcp.Description("按用例 sample_id 过滤；可选"),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func handleListAPICases(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_list_api_cases"
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		pid, err := resolveProjectID(args, deps)
		if err != nil {
			return errorResponse(tool, nil, err), nil
		}
		q := url.Values{}
		q.Set("project_id", pid)
		for _, id := range readStringArray(args, "target_ids") {
			q.Add("target_ids[]", id)
		}
		for _, id := range readStringArray(args, "sample_ids") {
			q.Add("sample_ids[]", id)
		}
		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodGet, "/open/apis/sample", q, nil, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// --- apipost_create_api_case ---

func newCreateAPICaseTool() mcp.Tool {
	return mcp.NewTool("apipost_create_api_case",
		mcp.WithDescription("为某个接口创建用例。POST /open/apis/sample/create；body 需含 target_id（所属接口）与用例定义（name / method / url / request 等，字段参见 Apipost 文档 '创建接口用例'）"),
		mcp.WithString("project_id",
			mcp.Description("项目 id；不传则使用 config.apipost.project_id"),
		),
		mcp.WithObject("body",
			mcp.Description("用例完整定义。工具会把 project_id 合入后 POST；未指定 type 时自动补 'sample'。"),
			mcp.Required(),
		),
	)
}

func handleCreateAPICase(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_create_api_case"
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		pid, err := resolveProjectID(args, deps)
		if err != nil {
			return errorResponse(tool, nil, err), nil
		}
		body := readObject(args, "body")
		if body == nil {
			return errorResponse(tool, nil, fmt.Errorf("body 必填且必须是对象")), nil
		}
		if _, ok := body["target_id"]; !ok {
			return errorResponse(tool, nil, fmt.Errorf("body.target_id 必填（用例所属的接口 id）")), nil
		}
		merged := make(map[string]any, len(body)+2)
		for k, v := range body {
			merged[k] = v
		}
		merged["project_id"] = pid
		if _, ok := merged["type"]; !ok {
			merged["type"] = "sample"
		}
		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodPost, "/open/apis/sample/create", nil, merged, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// RegisterCases 注册接口用例类工具。
func RegisterCases(add func(mcp.Tool, server.ToolHandlerFunc), deps Deps) {
	add(newListAPICasesTool(), handleListAPICases(deps))
	add(newCreateAPICaseTool(), handleCreateAPICase(deps))
}
