package db

import (
	"context"
	"fmt"
	"regexp"

	"github.com/mark3labs/mcp-go/mcp"
)

var tableNamePattern = regexp.MustCompile(`^[A-Za-z0-9_$]+$`)

func newDescribeTableTool() mcp.Tool {
	return mcp.NewTool("db_describe_table",
		mcp.WithDescription("Describe a table schema and indexes. If you do not know the table name, call db_list_tables first. If you do not know the source, call db_help first."),
		mcp.WithString("environment",
			mcp.Description("Environment key, such as pro, local, test1, or another value listed by db_help."),
			mcp.Required(),
		),
		mcp.WithString("source",
			mcp.Description("Database source key. Call db_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("table",
			mcp.Description("Table name. Only letters, numbers, underscore, and dollar sign are allowed."),
			mcp.Required(),
		),
	)
}

func handleDescribeTable(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		table, err := readString(args, "table")
		if err != nil {
			return renderErrorf("%s", err.Error()), nil
		}
		if !tableNamePattern.MatchString(table) {
			return renderErrorf("invalid table name %q: only letters, numbers, underscore, and dollar sign are allowed", table), nil
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
		schema, err := src.Driver.DescribeTable(queryCtx, database, table)
		if err != nil {
			return renderErrorf("describe failed [%s/%s.%s]: %v", src.Environment, src.Key, table, err), nil
		}

		return renderJSONResult(map[string]any{
			"environment": src.Environment,
			"source":      src.Key,
			"table":       table,
			"columns":     schema.Columns,
			"indexes":     schema.Indexes,
		}), nil
	}
}

func readString(args map[string]any, name string) (string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return "", fmt.Errorf("missing argument %q", name)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	if s == "" {
		return "", fmt.Errorf("argument %q cannot be empty", name)
	}
	return s, nil
}
