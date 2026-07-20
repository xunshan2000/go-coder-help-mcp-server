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
		mcp.WithDescription(fmt.Sprintf("Database usage guide. Call this first when you do not know which source to use, which tools are available, or what the recommended workflow is. %s Recommended workflow: db_help -> db_list_tables -> db_describe_table -> db_query. Only sources configured with write=true may use db_execute.", sourceSummary(pool))),
	)
}

func handleHelp(pool *Pool, defaults config.Defaults) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sources := make([]map[string]any, 0, len(pool.ordered))
		for _, src := range pool.ordered {
			sources = append(sources, map[string]any{
				"environment":   src.Environment,
				"source":        src.Key,
				"query_timeout": src.QueryTimeout.String(),
				"write_allowed": src.Write,
			})
		}
		workflow := []string{
			"Choose an environment and source from sources",
			"Call db_list_tables if table names are unknown",
			"Call db_describe_table if columns are unknown",
			"Call db_query for read-only SQL",
			"Use args for placeholders instead of string concatenation",
		}
		toolGuide := map[string]any{
			"db_list_tables":    "Input: environment and source. List all tables.",
			"db_describe_table": "Input: environment, source, and table. Describe schema and indexes.",
			"db_query":          "Input: environment, source, and read-only SQL.",
		}
		if pool.HasWritableSources() {
			workflow = append(workflow, "Call db_execute only when the selected source has write_allowed=true")
			toolGuide["db_execute"] = "Input: environment, source, and write SQL; requires write_allowed=true."
		}

		return renderJSONResult(map[string]any{
			"summary": "Use db_help first to inspect available sources and limits. If you do not know the table name, call db_list_tables. If you do not know the columns, call db_describe_table. Use db_query for reads. Use db_execute only on sources with write_allowed=true.",
			"sources": sources,
			"defaults": map[string]any{
				"max_rows":      defaults.MaxRows,
				"query_timeout": defaults.QueryTimeout.String(),
			},
			"workflow":   workflow,
			"tool_guide": toolGuide,
		}), nil
	}
}

func sourceSummary(pool *Pool) string {
	if len(pool.ordered) == 0 {
		return "No sources are currently available."
	}

	parts := make([]string, 0, len(pool.ordered))
	for _, src := range pool.ordered {
		parts = append(parts, fmt.Sprintf("%s/%s(write=%t)", src.Environment, src.Key, src.Write))
	}
	return "Available sources: " + strings.Join(parts, ", ") + "."
}
