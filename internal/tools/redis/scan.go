package redis

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
)

func newScanKeysTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_scan_keys",
		mcp.WithDescription(fmt.Sprintf("Scan keys in a Redis source using SCAN in read-only mode. Prefer this over KEYS for discovery. Returns keys plus next_cursor; continue scanning until next_cursor is \"0\". %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("pattern",
			mcp.Description("Optional MATCH pattern, for example user:* or order:2026:*. Defaults to *."),
		),
		mcp.WithString("cursor",
			mcp.Description("SCAN cursor. Use \"0\" for the first call, then pass the returned next_cursor to continue."),
		),
		mcp.WithNumber("count",
			mcp.Description("Requested SCAN COUNT hint. This is not a hard limit. Defaults to 100."),
		),
	)
}

func handleScanKeys(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		sourceName, err := readRequiredString(args, "source")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		pattern := readOptionalString(args, "pattern", "*")
		cursor := readOptionalString(args, "cursor", "0")
		count := readPositiveInt(args, "count", 100)

		reply, err := src.Do(ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", strconv.Itoa(count))
		if err != nil {
			return renderErrorf("redis scan failed [%s]: %v", src.Key, err), nil
		}

		arr, err := asArray(reply)
		if err != nil {
			return renderErrorf("redis scan reply decode failed [%s]: %v", src.Key, err), nil
		}
		if len(arr) != 2 {
			return renderErrorf("redis scan reply decode failed [%s]: expected 2 items, got %d", src.Key, len(arr)), nil
		}

		nextCursor, err := asString(arr[0])
		if err != nil {
			return renderErrorf("redis scan reply decode failed [%s]: %v", src.Key, err), nil
		}
		keys, err := toStringSlice(arr[1])
		if err != nil {
			return renderErrorf("redis scan keys decode failed [%s]: %v", src.Key, err), nil
		}

		return renderJSONResult(map[string]any{
			"source":      src.Key,
			"pattern":     pattern,
			"cursor":      cursor,
			"next_cursor": nextCursor,
			"count":       count,
			"keys":        keys,
		}), nil
	}
}

func readRequiredString(args map[string]any, name string) (string, error) {
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

func readOptionalString(args map[string]any, name, fallback string) string {
	raw, ok := args[name]
	if !ok || raw == nil {
		return fallback
	}
	s, ok := raw.(string)
	if !ok || s == "" {
		return fallback
	}
	return s
}

func readPositiveInt(args map[string]any, name string, fallback int) int {
	raw, ok := args[name]
	if !ok || raw == nil {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	}
	return fallback
}
