package db

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

func newListTablesTool() mcp.Tool {
	return mcp.NewTool("db_list_tables",
		mcp.WithDescription("List all tables under a source. If you do not know which source to use, call db_help first."),
		mcp.WithString("environment",
			mcp.Description("Environment key, such as pro, local, test1, or another value listed by db_help."),
			mcp.Required(),
		),
		mcp.WithString("source",
			mcp.Description("Database source key. Call db_help first to see available sources."),
			mcp.Required(),
		),
	)
}

func handleListTables(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		environment, err := readString(args, "environment")
		if err != nil {
			return renderErrorf("%s", err.Error()), nil
		}
		sourceName, err := readString(args, "source")
		if err != nil {
			return renderErrorf("%s", err.Error()), nil
		}
		src, err := pool.Get(environment, sourceName)
		if err != nil {
			return renderErrorf("%s", err.Error()), nil
		}

		queryCtx, cancel := context.WithTimeout(ctx, src.QueryTimeout)
		defer cancel()

		database, err := src.database(queryCtx)
		if err != nil {
			return renderErrorf("connect failed [%s/%s]: %v", src.Environment, src.Key, err), nil
		}
		tables, err := src.Driver.ListTables(queryCtx, database)
		if err != nil {
			return renderErrorf("list tables failed [%s/%s]: %v", src.Environment, src.Key, err), nil
		}
		return renderJSONResult(map[string]any{
			"environment": src.Environment,
			"source":      src.Key,
			"tables":      tables,
		}), nil
	}
}
