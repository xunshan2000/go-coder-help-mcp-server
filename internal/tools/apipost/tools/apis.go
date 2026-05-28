package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// --- apipost_list_apis ---

func newListApisTool() mcp.Tool {
	return mcp.NewTool("apipost_list_apis",
		mcp.WithDescription("列出项目下接口（简约结构，含目录节点）。GET /open/apis/list"),
		mcp.WithString("project_id",
			mcp.Description("项目 id；不传则使用 config.apipost.project_id"),
		),
	)
}

func handleListApis(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_list_apis"
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		pid, err := resolveProjectID(args, deps)
		if err != nil {
			return errorResponse(tool, nil, err), nil
		}
		q := url.Values{}
		q.Set("project_id", pid)
		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodGet, "/open/apis/list", q, nil, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// --- apipost_get_apis（批量详情） ---

func newGetApisTool() mcp.Tool {
	return mcp.NewTool("apipost_get_apis",
		mcp.WithDescription("批量获取接口完整定义。POST /open/apis/details；单个资源传 target_ids:[id]"),
		mcp.WithString("project_id",
			mcp.Description("项目 id；不传则使用 config.apipost.project_id"),
		),
		mcp.WithArray("target_ids",
			mcp.Description("要查询的接口 target_id 数组；至少一个"),
			mcp.Items(map[string]any{"type": "string"}),
			mcp.Required(),
		),
	)
}

func handleGetApis(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_get_apis"
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		pid, err := resolveProjectID(args, deps)
		if err != nil {
			return errorResponse(tool, nil, err), nil
		}
		ids := readStringArray(args, "target_ids")
		if len(ids) == 0 {
			return errorResponse(tool, nil, fmt.Errorf("target_ids 必填且至少包含一个 id")), nil
		}
		body := map[string]any{
			"project_id": pid,
			"target_ids": ids,
		}
		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodPost, "/open/apis/details", nil, body, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// --- apipost_create_http_api ---

func newCreateHTTPAPITool() mcp.Tool {
	return mcp.NewTool("apipost_create_http_api",
		mcp.WithDescription("新建 HTTP 类型接口。POST /open/apis/create；body 为完整 HTTP API 定义（target_type 固定 'api'；需含 parent_id / name / method / url / protocol / request 等字段；字段集参见 Apipost 文档 '创建接口（HTTP类型）'）"),
		mcp.WithString("project_id",
			mcp.Description("项目 id；不传则使用 config.apipost.project_id"),
		),
		mcp.WithObject("body",
			mcp.Description("HTTP API 完整定义。工具会把 project_id 合入该对象后 POST。未指定 target_type 时自动补 'api'。"),
			mcp.Required(),
		),
	)
}

func handleCreateHTTPAPI(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_create_http_api"
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
		// shallow clone + 注入 project_id / target_type
		merged := make(map[string]any, len(body)+2)
		for k, v := range body {
			merged[k] = v
		}
		merged["project_id"] = pid
		if _, ok := merged["target_type"]; !ok {
			merged["target_type"] = "api"
		}
		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodPost, "/open/apis/create", nil, merged, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// --- apipost_update_api ---

func newUpdateAPITool() mcp.Tool {
	return mcp.NewTool("apipost_update_api",
		mcp.WithDescription("修改接口。POST /open/apis/update；body 必须含 target_id 与完整定义。⚠ Apipost 文档明示：非必填字段不传会被置默认值 —— 调用前应先用 apipost_get_apis 取完整详情，在其上改动后整体回传。"),
		mcp.WithString("project_id",
			mcp.Description("项目 id；不传则使用 config.apipost.project_id"),
		),
		mcp.WithObject("body",
			mcp.Description("含 target_id 的完整 API 定义。工具会把 project_id 合入该对象后 POST。"),
			mcp.Required(),
		),
	)
}

func handleUpdateAPI(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_update_api"
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
			return errorResponse(tool, nil, fmt.Errorf("body.target_id 必填")), nil
		}
		merged := make(map[string]any, len(body)+1)
		for k, v := range body {
			merged[k] = v
		}
		merged["project_id"] = pid
		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodPost, "/open/apis/update", nil, merged, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// --- apipost_delete_apis ---

func newDeleteApisTool() mcp.Tool {
	return mcp.NewTool("apipost_delete_apis",
		mcp.WithDescription("批量删除接口 / 目录。POST /open/apis/delete；单个资源传 target_ids:[id]"),
		mcp.WithString("project_id",
			mcp.Description("项目 id；不传则使用 config.apipost.project_id"),
		),
		mcp.WithArray("target_ids",
			mcp.Description("要删除的 target_id 数组；至少一个"),
			mcp.Items(map[string]any{"type": "string"}),
			mcp.Required(),
		),
	)
}

func handleDeleteApis(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_delete_apis"
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		pid, err := resolveProjectID(args, deps)
		if err != nil {
			return errorResponse(tool, nil, err), nil
		}
		ids := readStringArray(args, "target_ids")
		if len(ids) == 0 {
			return errorResponse(tool, nil, fmt.Errorf("target_ids 必填且至少包含一个 id")), nil
		}
		body := map[string]any{
			"project_id": pid,
			"target_ids": ids,
		}
		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodPost, "/open/apis/delete", nil, body, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// RegisterApis 注册 API CRUD 类工具。
func RegisterApis(add func(mcp.Tool, server.ToolHandlerFunc), deps Deps) {
	add(newListApisTool(), handleListApis(deps))
	add(newGetApisTool(), handleGetApis(deps))
	add(newCreateHTTPAPITool(), handleCreateHTTPAPI(deps))
	add(newUpdateAPITool(), handleUpdateAPI(deps))
	add(newDeleteApisTool(), handleDeleteApis(deps))
}
