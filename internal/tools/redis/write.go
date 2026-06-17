package redis

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
)

func newSetTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_set",
		mcp.WithDescription(fmt.Sprintf("Set a Redis string key. Optionally set an expiration with ttl_seconds. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Redis key to write. Must not be empty."),
			mcp.Required(),
		),
		mcp.WithString("value",
			mcp.Description("String value to store. Empty string is allowed."),
			mcp.Required(),
		),
		mcp.WithNumber("ttl_seconds",
			mcp.Description("Optional expiration in seconds. Must be a positive integer when provided."),
		),
	)
}

func newDeleteTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_delete",
		mcp.WithDescription(fmt.Sprintf("Delete one Redis key. Returns the number of keys removed, 0 or 1. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Exact Redis key to delete. Must not be empty."),
			mcp.Required(),
		),
	)
}

func newHSetTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_hset",
		mcp.WithDescription(fmt.Sprintf("Set one or more Redis hash fields. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Redis hash key to write. Must not be empty."),
			mcp.Required(),
		),
		mcp.WithObject("fields",
			mcp.Description("Object of field names to string values. Must contain at least one field."),
			mcp.Required(),
		),
	)
}

func newHDelTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_hdel",
		mcp.WithDescription(fmt.Sprintf("Delete one or more fields from a Redis hash. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Redis hash key. Must not be empty."),
			mcp.Required(),
		),
		mcp.WithArray("fields",
			mcp.Description("Hash fields to delete. Must contain at least one string."),
			mcp.Required(),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func newLPushTool(pool *Pool) mcp.Tool {
	return newListPushTool(pool, "redis_lpush", "Push one or more string values to the left side of a Redis list.", "left")
}

func newRPushTool(pool *Pool) mcp.Tool {
	return newListPushTool(pool, "redis_rpush", "Push one or more string values to the right side of a Redis list.", "right")
}

func newListPushTool(pool *Pool, name, description, side string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(fmt.Sprintf("%s %s", description, sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description(fmt.Sprintf("Redis list key to push to the %s. Must not be empty.", side)),
			mcp.Required(),
		),
		mcp.WithArray("values",
			mcp.Description("String values to push. Must contain at least one string."),
			mcp.Required(),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func newSAddTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_sadd",
		mcp.WithDescription(fmt.Sprintf("Add one or more members to a Redis set. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Redis set key. Must not be empty."),
			mcp.Required(),
		),
		mcp.WithArray("members",
			mcp.Description("Set members to add. Must contain at least one string."),
			mcp.Required(),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func newSRemTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_srem",
		mcp.WithDescription(fmt.Sprintf("Remove one or more members from a Redis set. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Redis set key. Must not be empty."),
			mcp.Required(),
		),
		mcp.WithArray("members",
			mcp.Description("Set members to remove. Must contain at least one string."),
			mcp.Required(),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func newZAddTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_zadd",
		mcp.WithDescription(fmt.Sprintf("Add or update one or more members in a Redis sorted set. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Redis sorted set key. Must not be empty."),
			mcp.Required(),
		),
		mcp.WithObject("members",
			mcp.Description("Object of member names to numeric scores. Must contain at least one member."),
			mcp.Required(),
		),
	)
}

func newZRemTool(pool *Pool) mcp.Tool {
	return mcp.NewTool("redis_zrem",
		mcp.WithDescription(fmt.Sprintf("Remove one or more members from a Redis sorted set. %s", sourceSummary(pool))),
		mcp.WithString("source",
			mcp.Description("Redis source key. Required. Call redis_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("key",
			mcp.Description("Redis sorted set key. Must not be empty."),
			mcp.Required(),
		),
		mcp.WithArray("members",
			mcp.Description("Sorted set members to remove. Must contain at least one string."),
			mcp.Required(),
			mcp.Items(map[string]any{"type": "string"}),
		),
	)
}

func handleSet(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		value, err := readRequiredValue(args, "value")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		ttlSeconds, err := readOptionalPositiveInt(args, "ttl_seconds")
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		cmd := []string{"SET", key, value}
		if ttlSeconds > 0 {
			cmd = append(cmd, "EX", strconv.Itoa(ttlSeconds))
		}
		reply, err := src.Do(ctx, cmd...)
		if err != nil {
			return renderErrorf("redis set failed [%s.%s]: %v", src.Key, key, err), nil
		}
		status, err := asString(reply)
		if err != nil {
			return renderErrorf("redis set reply decode failed [%s.%s]: %v", src.Key, key, err), nil
		}
		if status != "OK" {
			return renderErrorf("redis set failed [%s.%s]: unexpected reply %q", src.Key, key, status), nil
		}

		return renderJSONResult(map[string]any{
			"source":      src.Key,
			"key":         key,
			"ok":          true,
			"ttl_seconds": ttlSeconds,
		}), nil
	}
}

func handleDelete(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		reply, err := src.Do(ctx, "DEL", key)
		if err != nil {
			return renderErrorf("redis delete failed [%s.%s]: %v", src.Key, key, err), nil
		}
		deleted, err := asInt64(reply)
		if err != nil {
			return renderErrorf("redis delete reply decode failed [%s.%s]: %v", src.Key, key, err), nil
		}

		return renderJSONResult(map[string]any{
			"source":  src.Key,
			"key":     key,
			"deleted": deleted,
		}), nil
	}
}

func handleHSet(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourceName, key, args, err := readSourceKeyArgs(req)
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		fields, err := readStringMap(args, "fields")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		cmd := []string{"HSET", key}
		for _, field := range sortedKeys(fields) {
			cmd = append(cmd, field, fields[field])
		}
		changed, err := doIntWrite(ctx, src, key, cmd, "redis hset")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		return renderJSONResult(map[string]any{"source": src.Key, "key": key, "fields_changed": changed}), nil
	}
}

func handleHDel(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return handleMemberWrite(pool, "HDEL", "fields", "fields_deleted", "redis hdel")
}

func handleLPush(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return handleListPush(pool, "LPUSH", "redis lpush")
}

func handleRPush(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return handleListPush(pool, "RPUSH", "redis rpush")
}

func handleListPush(pool *Pool, command, op string) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourceName, key, args, err := readSourceKeyArgs(req)
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		values, err := readStringSlice(args, "values")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		cmd := append([]string{command, key}, values...)
		length, err := doIntWrite(ctx, src, key, cmd, op)
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		return renderJSONResult(map[string]any{"source": src.Key, "key": key, "length": length}), nil
	}
}

func handleSAdd(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return handleMemberWrite(pool, "SADD", "members", "members_added", "redis sadd")
}

func handleSRem(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return handleMemberWrite(pool, "SREM", "members", "members_removed", "redis srem")
}

func handleZAdd(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourceName, key, args, err := readSourceKeyArgs(req)
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		members, err := readScoreMap(args, "members")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		cmd := []string{"ZADD", key}
		for _, member := range sortedKeys(members) {
			cmd = append(cmd, strconv.FormatFloat(members[member], 'f', -1, 64), member)
		}
		changed, err := doIntWrite(ctx, src, key, cmd, "redis zadd")
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		return renderJSONResult(map[string]any{"source": src.Key, "key": key, "members_changed": changed}), nil
	}
}

func handleZRem(pool *Pool) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return handleMemberWrite(pool, "ZREM", "members", "members_removed", "redis zrem")
}

func handleMemberWrite(pool *Pool, command, argName, resultName, op string) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		sourceName, key, args, err := readSourceKeyArgs(req)
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		values, err := readStringSlice(args, argName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		src, err := pool.Get(sourceName)
		if err != nil {
			return renderErrorf("%s", err), nil
		}

		cmd := append([]string{command, key}, values...)
		changed, err := doIntWrite(ctx, src, key, cmd, op)
		if err != nil {
			return renderErrorf("%s", err), nil
		}
		return renderJSONResult(map[string]any{"source": src.Key, "key": key, resultName: changed}), nil
	}
}

func readRequiredValue(args map[string]any, name string) (string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return "", fmt.Errorf("missing argument %q", name)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", name)
	}
	return s, nil
}

func readOptionalPositiveInt(args map[string]any, name string) (int, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return 0, nil
	}
	switch v := raw.(type) {
	case float64:
		if v > 0 && math.Trunc(v) == v && v <= float64(maxInt()) {
			return int(v), nil
		}
	case int:
		if v > 0 {
			return v, nil
		}
	case int64:
		if v > 0 && v <= int64(maxInt()) {
			return int(v), nil
		}
	}
	return 0, fmt.Errorf("argument %q must be a positive integer", name)
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func readSourceKeyArgs(req mcp.CallToolRequest) (string, string, map[string]any, error) {
	args := req.GetArguments()
	sourceName, err := readRequiredString(args, "source")
	if err != nil {
		return "", "", nil, err
	}
	key, err := readRequiredString(args, "key")
	if err != nil {
		return "", "", nil, err
	}
	return sourceName, key, args, nil
}

func readStringSlice(args map[string]any, name string) ([]string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return nil, fmt.Errorf("missing argument %q", name)
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("argument %q must be an array", name)
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("argument %q must contain at least one item", name)
	}
	out := make([]string, 0, len(arr))
	for i, rawItem := range arr {
		s, ok := rawItem.(string)
		if !ok {
			return nil, fmt.Errorf("argument %q item %d must be a string", name, i)
		}
		out = append(out, s)
	}
	return out, nil
}

func readStringMap(args map[string]any, name string) (map[string]string, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return nil, fmt.Errorf("missing argument %q", name)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("argument %q must be an object", name)
	}
	if len(obj) == 0 {
		return nil, fmt.Errorf("argument %q must contain at least one field", name)
	}
	out := make(map[string]string, len(obj))
	for k, rawValue := range obj {
		if k == "" {
			return nil, fmt.Errorf("argument %q field name cannot be empty", name)
		}
		s, ok := rawValue.(string)
		if !ok {
			return nil, fmt.Errorf("argument %q field %q must be a string", name, k)
		}
		out[k] = s
	}
	return out, nil
}

func readScoreMap(args map[string]any, name string) (map[string]float64, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return nil, fmt.Errorf("missing argument %q", name)
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("argument %q must be an object", name)
	}
	if len(obj) == 0 {
		return nil, fmt.Errorf("argument %q must contain at least one member", name)
	}
	out := make(map[string]float64, len(obj))
	for member, rawScore := range obj {
		if member == "" {
			return nil, fmt.Errorf("argument %q member name cannot be empty", name)
		}
		score, ok := rawScore.(float64)
		if !ok {
			return nil, fmt.Errorf("argument %q member %q score must be a number", name, member)
		}
		out[member] = score
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func doIntWrite(ctx context.Context, src *Source, key string, cmd []string, op string) (int64, error) {
	reply, err := src.Do(ctx, cmd...)
	if err != nil {
		return 0, fmt.Errorf("%s failed [%s.%s]: %v", op, src.Key, key, err)
	}
	n, err := asInt64(reply)
	if err != nil {
		return 0, fmt.Errorf("%s reply decode failed [%s.%s]: %v", op, src.Key, key, err)
	}
	return n, nil
}
