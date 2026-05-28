package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"example.com/mcp-server/internal/config"
)

func newHelpTool(pool *Pool, defaults config.Defaults) mcp.Tool {
	return mcp.NewTool("db_help",
		mcp.WithDescription(fmt.Sprintf("Database usage guide. Call this first when you do not know which source to use, which tools are available, or what the recommended workflow is. %s Recommended workflow: db_help -> db_list_tables -> db_describe_table -> db_query. Only rw sources may use db_execute.", sourceSummary(pool))),
	)
}

func handleHelp(pool *Pool, defaults config.Defaults) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sources := make([]map[string]any, 0, len(pool.keys))
		for _, key := range pool.keys {
			src := pool.sources[key]
			sources = append(sources, map[string]any{
				"name":          src.Key,
				"mode":          string(src.Mode),
				"query_timeout": src.QueryTimeout.String(),
				"write_allowed": src.Mode == ModeRW,
			})
		}

		return renderJSONResult(map[string]any{
			"summary": "Use db_help first to inspect available sources and limits. If you do not know the table name, call db_list_tables. If you do not know the columns, call db_describe_table. Use db_query for reads. Use db_execute only on rw sources.",
			"sources": sources,
			"defaults": map[string]any{
				"max_rows":      defaults.MaxRows,
				"query_timeout": defaults.QueryTimeout.String(),
			},
			"workflow": []string{
				"Choose a source from sources",
				"Call db_list_tables if table names are unknown",
				"Call db_describe_table if columns are unknown",
				"Call db_query for read-only SQL",
				"Use args for placeholders instead of string concatenation",
			},
			"tool_guide": map[string]any{
				"db_list_tables":    "List all tables in a source",
				"db_describe_table": "Describe one table schema and indexes",
				"db_query":          "Run read-only SQL on a source",
				"db_execute":        "Run write SQL on an rw source only",
			},
		}), nil
	}
}

func sourceSummary(pool *Pool) string {
	if len(pool.keys) == 0 {
		return "No sources are currently available."
	}

	parts := make([]string, 0, len(pool.keys))
	for _, key := range pool.keys {
		src := pool.sources[key]
		parts = append(parts, fmt.Sprintf("%s(%s)", src.Key, src.Mode))
	}
	return "Available sources: " + strings.Join(parts, ", ") + "."
}
