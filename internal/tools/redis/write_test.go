package redis

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"example.com/mcp-server/internal/tools"
)

func TestHandleSet_WithTTL(t *testing.T) {
	addr, gotCommand, stop := startRedisStub(t, "+OK\r\n")
	defer stop()

	pool := redisTestPool(addr)
	res, err := handleSet(pool)(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"environment": "test1",
				"source":      "cache",
				"key":         "demo",
				"value":       "hello",
				"ttl_seconds": float64(30),
			},
		},
	})
	if err != nil {
		t.Fatalf("handleSet returned protocol error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleSet returned tool error: %s", resultText(t, res))
	}
	if want := []string{"SET", "demo", "hello", "EX", "30"}; !reflect.DeepEqual(*gotCommand, want) {
		t.Fatalf("redis command mismatch\nwant: %#v\ngot:  %#v", want, *gotCommand)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(resultText(t, res)), &payload); err != nil {
		t.Fatalf("decode result JSON: %v", err)
	}
	if payload["ok"] != true || payload["ttl_seconds"].(float64) != 30 {
		t.Fatalf("unexpected result payload: %#v", payload)
	}
}

func TestHandleDelete(t *testing.T) {
	addr, gotCommand, stop := startRedisStub(t, ":1\r\n")
	defer stop()

	pool := redisTestPool(addr)
	res, err := handleDelete(pool)(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"environment": "test1",
				"source":      "cache",
				"key":         "demo",
			},
		},
	})
	if err != nil {
		t.Fatalf("handleDelete returned protocol error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleDelete returned tool error: %s", resultText(t, res))
	}
	if want := []string{"DEL", "demo"}; !reflect.DeepEqual(*gotCommand, want) {
		t.Fatalf("redis command mismatch\nwant: %#v\ngot:  %#v", want, *gotCommand)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(resultText(t, res)), &payload); err != nil {
		t.Fatalf("decode result JSON: %v", err)
	}
	if payload["deleted"].(float64) != 1 {
		t.Fatalf("unexpected result payload: %#v", payload)
	}
}

func TestHandleHSet(t *testing.T) {
	addr, gotCommand, stop := startRedisStub(t, ":2\r\n")
	defer stop()

	pool := redisTestPool(addr)
	res, err := handleHSet(pool)(context.Background(), testCall(map[string]any{
		"source": "cache",
		"key":    "demo:hash",
		"fields": map[string]any{"b": "two", "a": "one"},
	}))
	if err != nil {
		t.Fatalf("handleHSet returned protocol error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleHSet returned tool error: %s", resultText(t, res))
	}
	if want := []string{"HSET", "demo:hash", "a", "one", "b", "two"}; !reflect.DeepEqual(*gotCommand, want) {
		t.Fatalf("redis command mismatch\nwant: %#v\ngot:  %#v", want, *gotCommand)
	}
}

func TestHandleRPush(t *testing.T) {
	addr, gotCommand, stop := startRedisStub(t, ":3\r\n")
	defer stop()

	pool := redisTestPool(addr)
	res, err := handleRPush(pool)(context.Background(), testCall(map[string]any{
		"source": "cache",
		"key":    "demo:list",
		"values": []any{"a", "b", "c"},
	}))
	if err != nil {
		t.Fatalf("handleRPush returned protocol error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleRPush returned tool error: %s", resultText(t, res))
	}
	if want := []string{"RPUSH", "demo:list", "a", "b", "c"}; !reflect.DeepEqual(*gotCommand, want) {
		t.Fatalf("redis command mismatch\nwant: %#v\ngot:  %#v", want, *gotCommand)
	}
}

func TestHandleSAdd(t *testing.T) {
	addr, gotCommand, stop := startRedisStub(t, ":2\r\n")
	defer stop()

	pool := redisTestPool(addr)
	res, err := handleSAdd(pool)(context.Background(), testCall(map[string]any{
		"source":  "cache",
		"key":     "demo:set",
		"members": []any{"a", "b"},
	}))
	if err != nil {
		t.Fatalf("handleSAdd returned protocol error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleSAdd returned tool error: %s", resultText(t, res))
	}
	if want := []string{"SADD", "demo:set", "a", "b"}; !reflect.DeepEqual(*gotCommand, want) {
		t.Fatalf("redis command mismatch\nwant: %#v\ngot:  %#v", want, *gotCommand)
	}
}

func TestHandleZAdd(t *testing.T) {
	addr, gotCommand, stop := startRedisStub(t, ":2\r\n")
	defer stop()

	pool := redisTestPool(addr)
	res, err := handleZAdd(pool)(context.Background(), testCall(map[string]any{
		"source":  "cache",
		"key":     "demo:zset",
		"members": map[string]any{"b": float64(2), "a": float64(1.5)},
	}))
	if err != nil {
		t.Fatalf("handleZAdd returned protocol error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleZAdd returned tool error: %s", resultText(t, res))
	}
	if want := []string{"ZADD", "demo:zset", "1.5", "a", "2", "b"}; !reflect.DeepEqual(*gotCommand, want) {
		t.Fatalf("redis command mismatch\nwant: %#v\ngot:  %#v", want, *gotCommand)
	}
}

func TestReadOptionalPositiveIntRejectsFractionalTTL(t *testing.T) {
	_, err := readOptionalPositiveInt(map[string]any{"ttl_seconds": 1.5}, "ttl_seconds")
	if err == nil {
		t.Fatalf("expected fractional ttl_seconds to be rejected")
	}
}

func TestWriteHandlerRejectsReadOnlySource(t *testing.T) {
	src := &Source{Environment: "test1", Key: "cache"}
	pool := &Pool{
		sources: map[string]map[string]*Source{"test1": {"cache": src}},
		ordered: []*Source{src},
	}
	res, err := handleSet(pool)(context.Background(), testCall(map[string]any{
		"source": "cache",
		"key":    "demo",
		"value":  "blocked",
	}))
	if err != nil {
		t.Fatalf("handleSet returned protocol error: %v", err)
	}
	if !res.IsError || !strings.Contains(resultText(t, res), "environments.test1.redis.cache.write: true") {
		t.Fatalf("expected read-only error, got: %s", resultText(t, res))
	}
}

func TestRegisterWriteToolsOnlyWhenWritableSourceExists(t *testing.T) {
	readSource := &Source{Environment: "test1", Key: "cache"}
	readOnly := &Pool{sources: map[string]map[string]*Source{"test1": {"cache": readSource}}, ordered: []*Source{readSource}}
	readServer := mcpserver.NewMCPServer("test", "test")
	Register(tools.NewRegistry(readServer), readOnly)
	if _, ok := readServer.ListTools()["redis_set"]; ok {
		t.Fatal("redis_set must not be registered for read-only sources")
	}
	if _, ok := readServer.ListTools()["redis_get"]; !ok {
		t.Fatal("redis_get must be registered for read-only sources")
	}

	writeSource := &Source{Environment: "test1", Key: "cache", Write: true}
	writable := &Pool{sources: map[string]map[string]*Source{"test1": {"cache": writeSource}}, ordered: []*Source{writeSource}}
	writeServer := mcpserver.NewMCPServer("test", "test")
	Register(tools.NewRegistry(writeServer), writable)
	if _, ok := writeServer.ListTools()["redis_set"]; !ok {
		t.Fatal("redis_set must be registered when a writable source exists")
	}
	for name, serverTool := range writeServer.ListTools() {
		if name == "redis_help" {
			continue
		}
		if _, ok := serverTool.Tool.InputSchema.Properties["environment"]; !ok || !slices.Contains(serverTool.Tool.InputSchema.Required, "environment") {
			t.Fatalf("tool %s must require environment", name)
		}
	}
}

func TestPoolSeparatesSameSourceNameByEnvironment(t *testing.T) {
	pro := &Source{Environment: "pro", Key: "cache"}
	local := &Source{Environment: "local", Key: "cache"}
	pool := &Pool{
		sources: map[string]map[string]*Source{
			"pro":   {"cache": pro},
			"local": {"cache": local},
		},
		ordered: []*Source{local, pro},
	}
	if got, err := pool.Get("pro", "cache"); err != nil || got != pro {
		t.Fatalf("Get(pro, cache) = %v, %v", got, err)
	}
	if got, err := pool.Get("local", "cache"); err != nil || got != local {
		t.Fatalf("Get(local, cache) = %v, %v", got, err)
	}
}

func redisTestPool(addr string) *Pool {
	src := &Source{
		Environment: "test1",
		Key:         "cache",
		Addr:        addr,
		Write:       true,
		DialTimeout: time.Second,
		ReadTimeout: time.Second,
	}
	return &Pool{
		sources: map[string]map[string]*Source{"test1": {"cache": src}},
		ordered: []*Source{src},
	}
}

func testCall(args map[string]any) mcp.CallToolRequest {
	if _, ok := args["environment"]; !ok {
		args["environment"] = "test1"
	}
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
}

func startRedisStub(t *testing.T, response string) (string, *[]string, func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	gotCommand := []string{}
	done := make(chan error, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		reply, err := readReply(bufio.NewReader(conn))
		if err != nil {
			done <- err
			return
		}
		items, err := asArray(reply)
		if err != nil {
			done <- err
			return
		}
		for _, item := range items {
			s, err := asString(item)
			if err != nil {
				done <- err
				return
			}
			gotCommand = append(gotCommand, s)
		}
		if _, err := fmt.Fprint(conn, response); err != nil {
			done <- err
			return
		}
		done <- nil
	}()

	stop := func() {
		ln.Close()
		select {
		case err := <-done:
			if err != nil && !isClosedNetworkError(err) {
				t.Fatalf("redis stub error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatalf("redis stub did not finish")
		}
	}
	return ln.Addr().String(), &gotCommand, stop
}

func isClosedNetworkError(err error) bool {
	return err != nil && (err == net.ErrClosed || err.Error() == "use of closed network connection")
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("missing tool result content")
	}
	text, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return text.Text
}
