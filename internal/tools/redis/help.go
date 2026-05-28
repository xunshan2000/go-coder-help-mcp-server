package redis

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func newHelpTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_help",
		mcp.WithDescription(fmt.Sprintf("Redis usage guide. Call this first when you do not know which source to use or how to inspect keys. Returns available sources, db index, and the recommended workflow. %s Recommended workflow: redis_help -> redis_scan_keys -> redis_get.", sourceSummary(pool))),
	)
}

func handleHelp(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sources := make([]map[string]any, 0, len(pool.keys))
		for _, key := range pool.keys {
			src := pool.sources[key]
			sources = append(sources, map[string]any{
				"name":         src.Key,
				"addr":         src.Addr,
				"db":           src.Index,
				"dial_timeout": src.DialTimeout.String(),
				"read_timeout": src.ReadTimeout.String(),
			})
		}

		return renderJSONResult(map[string]any{
			"summary": "Use redis_help first to inspect available sources. Call redis_scan_keys to search keys, then redis_get to inspect a key's value by type. All redis tools are read-only.",
			"sources": sources,
			"workflow": []string{
				"Choose a source from sources",
				"Call redis_scan_keys if the exact key is unknown; keep scanning until next_cursor is 0",
				"Call redis_get to inspect one key and decode its type-specific value",
				"Use max_items to cap large collections such as hash, list, set, and zset",
			},
			"tool_guide": map[string]any{
				"redis_scan_keys": "Input: source, optional pattern/cursor/count. Output: keys and next_cursor.",
				"redis_get":       "Input: source, key, optional max_items. Output: exists, type, ttl_seconds, size, and value.",
			},
		}), nil
	}
}

func sourceSummary(pool *Pool) string {
	if len(pool.keys) == 0 {
		return "No redis sources are currently available."
	}

	parts := make([]string, 0, len(pool.keys))
	for _, key := range pool.keys {
		src := pool.sources[key]
		parts = append(parts, fmt.Sprintf("%s(db=%d)", src.Key, src.Index))
	}
	return "Available redis sources: " + strings.Join(parts, ", ") + "."
}
