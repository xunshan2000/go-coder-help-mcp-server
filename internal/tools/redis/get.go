package redis

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
)

func newGetTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_get",
		mcp.WithDescription(fmt.Sprintf("Inspect one Redis key in read-only mode. The tool auto-detects key type, returns exists/type/ttl_seconds/size, and reads common value types: string, hash, list, set, zset. Large collections are capped by max_items and may return truncated=true or next_cursor. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Exact Redis key to inspect."),
			mcp.Required(),
		),
		mcp.WithNumber("max_items",
			mcp.Description("Maximum number of collection items to return for hash/list/set/zset values. Defaults to 100."),
		),
	)
}

func handleGet(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		sourceName, err := readRequiredString(args, "source")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		key, err := readRequiredString(args, "key")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		maxItems := readPositiveInt(args, "max_items", 100)

		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		payload, err := loadKey(ctx, src, key, maxItems)
		if err != nil {
			return renderErrorf("redis get failed [%s.%s]: %v", src.Key, key, err), nil
		}
		return renderJSONResult(payload), nil
	}
}

func loadKey(ctx context.Context, src *Source, key string, maxItems int) (map[string]any, error) {
	typReply, err := src.Do(ctx, "TYPE", key)
	if err != nil {
		return nil, err
	}
	keyType, err := asString(typReply)
	if err != nil {
		return nil, fmt.Errorf("decode TYPE reply: %w", err)
	}

	payload := map[string]any{
		"source": src.Key,
		"key":    key,
		"type":   keyType,
	}
	if keyType == "none" {
		payload["exists"] = false
		return payload, nil
	}
	payload["exists"] = true

	ttlReply, err := src.Do(ctx, "TTL", key)
	if err != nil {
		return nil, err
	}
	ttl, err := asInt64(ttlReply)
	if err != nil {
		return nil, fmt.Errorf("decode TTL reply: %w", err)
	}
	payload["ttl_seconds"] = ttl

	switch keyType {
	case "string":
		value, err := src.Do(ctx, "GET", key)
		if err != nil {
			return nil, err
		}
		length, err := src.Do(ctx, "STRLEN", key)
		if err != nil {
			return nil, err
		}
		size, err := asInt64(length)
		if err != nil {
			return nil, fmt.Errorf("decode STRLEN reply: %w", err)
		}
		payload["size"] = size
		payload["value"] = value
	case "hash":
		total, fields, nextCursor, err := readHash(ctx, src, key, maxItems)
		if err != nil {
			return nil, err
		}
		payload["size"] = total
		payload["next_cursor"] = nextCursor
		payload["truncated"] = nextCursor != "0"
		payload["value"] = fields
	case "list":
		total, values, err := readList(ctx, src, key, maxItems)
		if err != nil {
			return nil, err
		}
		payload["size"] = total
		payload["truncated"] = total > int64(len(values))
		payload["value"] = values
	case "set":
		total, members, nextCursor, err := readSet(ctx, src, key, maxItems)
		if err != nil {
			return nil, err
		}
		payload["size"] = total
		payload["next_cursor"] = nextCursor
		payload["truncated"] = nextCursor != "0"
		payload["value"] = members
	case "zset":
		total, members, nextCursor, err := readZSet(ctx, src, key, maxItems)
		if err != nil {
			return nil, err
		}
		payload["size"] = total
		payload["next_cursor"] = nextCursor
		payload["truncated"] = nextCursor != "0"
		payload["value"] = members
	default:
		payload["warning"] = "unsupported redis type"
	}

	return payload, nil
}

func readHash(ctx context.Context, src *Source, key string, maxItems int) (int64, map[string]string, string, error) {
	sizeReply, err := src.Do(ctx, "HLEN", key)
	if err != nil {
		return 0, nil, "", err
	}
	size, err := asInt64(sizeReply)
	if err != nil {
		return 0, nil, "", err
	}

	reply, err := src.Do(ctx, "HSCAN", key, "0", "COUNT", strconv.Itoa(maxItems))
	if err != nil {
		return 0, nil, "", err
	}
	arr, err := asArray(reply)
	if err != nil {
		return 0, nil, "", err
	}
	if len(arr) != 2 {
		return 0, nil, "", fmt.Errorf("expected HSCAN reply with 2 items, got %d", len(arr))
	}
	nextCursor, err := asString(arr[0])
	if err != nil {
		return 0, nil, "", err
	}
	flat, err := toStringSlice(arr[1])
	if err != nil {
		return 0, nil, "", err
	}
	fields := make(map[string]string, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		fields[flat[i]] = flat[i+1]
	}
	return size, fields, nextCursor, nil
}

func readList(ctx context.Context, src *Source, key string, maxItems int) (int64, []string, error) {
	sizeReply, err := src.Do(ctx, "LLEN", key)
	if err != nil {
		return 0, nil, err
	}
	size, err := asInt64(sizeReply)
	if err != nil {
		return 0, nil, err
	}
	reply, err := src.Do(ctx, "LRANGE", key, "0", strconv.Itoa(maxItems-1))
	if err != nil {
		return 0, nil, err
	}
	values, err := toStringSlice(reply)
	if err != nil {
		return 0, nil, err
	}
	return size, values, nil
}

func readSet(ctx context.Context, src *Source, key string, maxItems int) (int64, []string, string, error) {
	sizeReply, err := src.Do(ctx, "SCARD", key)
	if err != nil {
		return 0, nil, "", err
	}
	size, err := asInt64(sizeReply)
	if err != nil {
		return 0, nil, "", err
	}

	reply, err := src.Do(ctx, "SSCAN", key, "0", "COUNT", strconv.Itoa(maxItems))
	if err != nil {
		return 0, nil, "", err
	}
	arr, err := asArray(reply)
	if err != nil {
		return 0, nil, "", err
	}
	if len(arr) != 2 {
		return 0, nil, "", fmt.Errorf("expected SSCAN reply with 2 items, got %d", len(arr))
	}
	nextCursor, err := asString(arr[0])
	if err != nil {
		return 0, nil, "", err
	}
	members, err := toStringSlice(arr[1])
	if err != nil {
		return 0, nil, "", err
	}
	return size, members, nextCursor, nil
}

func readZSet(ctx context.Context, src *Source, key string, maxItems int) (int64, []map[string]any, string, error) {
	sizeReply, err := src.Do(ctx, "ZCARD", key)
	if err != nil {
		return 0, nil, "", err
	}
	size, err := asInt64(sizeReply)
	if err != nil {
		return 0, nil, "", err
	}

	reply, err := src.Do(ctx, "ZSCAN", key, "0", "COUNT", strconv.Itoa(maxItems))
	if err != nil {
		return 0, nil, "", err
	}
	arr, err := asArray(reply)
	if err != nil {
		return 0, nil, "", err
	}
	if len(arr) != 2 {
		return 0, nil, "", fmt.Errorf("expected ZSCAN reply with 2 items, got %d", len(arr))
	}
	nextCursor, err := asString(arr[0])
	if err != nil {
		return 0, nil, "", err
	}
	flat, err := toStringSlice(arr[1])
	if err != nil {
		return 0, nil, "", err
	}
	out := make([]map[string]any, 0, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		out = append(out, map[string]any{
			"member": flat[i],
			"score":  flat[i+1],
		})
	}
	return size, out, nextCursor, nil
}

func asString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("expected string reply, got %T", v)
	}
}

func asArray(v any) ([]any, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected array reply, got %T", v)
	}
	return arr, nil
}

func asInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	default:
		return 0, fmt.Errorf("expected integer reply, got %T", v)
	}
}

func toStringSlice(v any) ([]string, error) {
	arr, err := asArray(v)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, err := asString(item)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
