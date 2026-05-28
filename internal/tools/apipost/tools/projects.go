package tools

import (
	"context"
	"net/http"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// --- apipost_list_projects ---

func newListProjectsTool() mcp.Tool {
	return mcp.NewTool("apipost_list_projects",
		mcp.WithDescription("列出当前 Token 可见的 Apipost 项目。GET /open/project/list；team_id / action 可选（action 默认 0=全部）"),
		mcp.WithString("team_id",
			mcp.Description("团队 id；不传则回落到 config.apipost.team_id；两者都未设置时需显式传"),
		),
		mcp.WithString("action",
			mcp.Description("列表分类：0全部 1我管理的 2我参与的 3回收站；默认 0"),
		),
	)
}

func handleListProjects(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_list_projects"
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		team, err := resolveTeamID(args, deps)
		if err != nil {
			return errorResponse(tool, nil, err), nil
		}

		q := url.Values{}
		if team != "" {
			q.Set("team_id", team)
		}
		if a := readString(args, "action"); a != "" {
			q.Set("action", a)
		} else {
			q.Set("action", "0")
		}

		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodGet, "/open/project/list", q, nil, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// --- apipost_get_project ---

func newGetProjectTool() mcp.Tool {
	return mcp.NewTool("apipost_get_project",
		mcp.WithDescription("获取项目详情。GET /open/project/info；project_id 不传则回落到 config.apipost.project_id"),
		mcp.WithString("project_id",
			mcp.Description("项目 id；不传则使用 config.apipost.project_id"),
		),
	)
}

func handleGetProject(deps Deps) server.ToolHandlerFunc {
	const tool = "apipost_get_project"
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		pid, err := resolveProjectID(args, deps)
		if err != nil {
			return errorResponse(tool, nil, err), nil
		}

		q := url.Values{}
		q.Set("project_id", pid)

		var out any
		meta, cErr := deps.Client.Do(ctx, http.MethodGet, "/open/project/info", q, nil, &out)
		if cErr != nil {
			return errorResponse(tool, meta, cErr), nil
		}
		return successResponse(tool, meta, out), nil
	}
}

// RegisterProjects 注册项目读取类工具。
func RegisterProjects(add func(mcp.Tool, server.ToolHandlerFunc), deps Deps) {
	add(newListProjectsTool(), handleListProjects(deps))
	add(newGetProjectTool(), handleGetProject(deps))
}
