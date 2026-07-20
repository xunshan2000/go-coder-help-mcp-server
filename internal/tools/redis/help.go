package redis

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func newHelpTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_help",
		mcp.WithDescription(fmt.Sprintf("Redis usage guide. Call this first when you do not know which source to use or how to inspect or write keys. Returns available sources, db index, write permission, and the recommended workflow. %s Recommended workflow: redis_help -> redis_scan_keys -> redis_get; write tools are available only for sources configured with write=true.", sourceSummary(pool))),
	)
}

func handleHelp(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sources := make([]map[string]any, 0, len(pool.ordered))
		for _, src := range pool.ordered {
			sources = append(sources, map[string]any{
				"environment":   src.Environment,
				"source":        src.Key,
				"addr":          src.Addr,
				"db":            src.Index,
				"write_allowed": src.Write,
				"dial_timeout":  src.DialTimeout.String(),
				"read_timeout":  src.ReadTimeout.String(),
			})
		}
		workflow := []string{
			"Choose an environment and source from sources",
			"Call redis_scan_keys if the exact key is unknown; keep scanning until next_cursor is 0",
			"Call redis_get to inspect one key and decode its type-specific value",
			"Use max_items to cap large collections such as hash, list, set, and zset",
		}
		toolGuide := map[string]any{
			"redis_scan_keys": "Input: environment, source, optional pattern/cursor/count. Output: keys and next_cursor.",
			"redis_get":       "Input: environment, source, key, optional max_items. Output: exists, type, ttl_seconds, size, and value.",
		}
		if pool.HasWritableSources() {
			workflow = append(workflow,
				"Use Redis write tools only when the selected source has write_allowed=true",
				"Call redis_set for strings, redis_hset/redis_hdel for hashes, redis_lpush/redis_rpush for lists",
				"Call redis_sadd/redis_srem for sets, redis_zadd/redis_zrem for sorted sets, and redis_delete to remove a key",
			)
			toolGuide["redis_set"] = "Input: environment, source, key, value, optional ttl_seconds."
			toolGuide["redis_delete"] = "Input: environment, source, key."
			toolGuide["redis_hset"] = "Input: environment, source, key, fields object."
			toolGuide["redis_hdel"] = "Input: environment, source, key, fields array."
			toolGuide["redis_lpush"] = "Input: environment, source, key, values array."
			toolGuide["redis_rpush"] = "Input: environment, source, key, values array."
			toolGuide["redis_sadd"] = "Input: environment, source, key, members array."
			toolGuide["redis_srem"] = "Input: environment, source, key, members array."
			toolGuide["redis_zadd"] = "Input: environment, source, key, members object."
			toolGuide["redis_zrem"] = "Input: environment, source, key, members array."
		}

		return renderJSONResult(map[string]any{
			"summary":    "Use redis_help first to inspect available sources. Call redis_scan_keys to search keys and redis_get to inspect values. Use redis_* write tools only on sources with write_allowed=true.",
			"sources":    sources,
			"workflow":   workflow,
			"tool_guide": toolGuide,
		}), nil
	}
}

func sourceSummary(pool *Pool) string {
	if len(pool.ordered) == 0 {
		return "No redis sources are currently available."
	}

	parts := make([]string, 0, len(pool.ordered))
	for _, src := range pool.ordered {
		parts = append(parts, fmt.Sprintf("%s/%s(db=%d,write=%t)", src.Environment, src.Key, src.Index, src.Write))
	}
	return "Available redis sources: " + strings.Join(parts, ", ") + "."
}
