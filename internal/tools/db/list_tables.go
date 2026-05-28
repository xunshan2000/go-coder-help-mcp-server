package db

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

func newListTablesTool() mcp.Tool {
	return mcp.NewTool("db_list_tables",
		mcp.WithDescription("List all tables under a source. If you do not know which source to use, call db_help first."),
		mcp.WithString("source",
			mcp.Description("Database source key. Call db_help first to see available sources."),
			mcp.Required(),
		),
	)
}

func handleListTables(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		sourceName, err := readString(args, "source")
		if err != nil {
			return renderErrorf("%s", err.Error()), nil
		}
		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err.Error()), nil
		}

		queryCtx, cancel := context.WithTimeout(ctx, src.QueryTimeout)
		defer cancel()

		tables, err := src.Driver.ListTables(queryCtx, src.DB)
		if err != nil {
			return renderErrorf("list tables failed [%s]: %v", src.Key, err), nil
		}
		return renderJSONResult(map[string]any{
			"source": src.Key,
			"tables": tables,
		}), nil
	}
}
