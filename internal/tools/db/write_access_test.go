package db

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/tools"
	"example.com/mcp-server/internal/tools/db/drivers/mysql"
)

func TestRegisterExecuteOnlyWhenWritableSourceExists(t *testing.T) {
	readOnly := dbAccessTestPool(false)
	readServer := mcpserver.NewMCPServer("test", "test")
	Register(tools.NewRegistry(readServer), readOnly, config.Defaults{}, nil)
	if _, ok := readServer.ListTools()["db_execute"]; ok {
		t.Fatal("db_execute must not be registered for read-only sources")
	}
	if _, ok := readServer.ListTools()["db_query"]; !ok {
		t.Fatal("db_query must be registered for read-only sources")
	}

	writable := dbAccessTestPool(true)
	writeServer := mcpserver.NewMCPServer("test", "test")
	Register(tools.NewRegistry(writeServer), writable, config.Defaults{}, nil)
	if _, ok := writeServer.ListTools()["db_execute"]; !ok {
		t.Fatal("db_execute must be registered when a writable source exists")
	}
	for name, serverTool := range writeServer.ListTools() {
		if name == "db_help" {
			continue
		}
		if _, ok := serverTool.Tool.InputSchema.Properties["environment"]; !ok || !slices.Contains(serverTool.Tool.InputSchema.Required, "environment") {
			t.Fatalf("tool %s must require environment", name)
		}
	}
}

func TestExecuteRejectsReadOnlySource(t *testing.T) {
	res, err := handleExecute(dbAccessTestPool(false), nil)(context.Background(), dbTestCall(map[string]any{
		"source": "primary",
		"sql":    "UPDATE users SET active = 1 WHERE id = ?",
		"args":   []any{1},
	}))
	if err != nil {
		t.Fatalf("handleExecute returned protocol error: %v", err)
	}
	if !res.IsError || !strings.Contains(dbResultText(t, res), "environments.test1.databases.primary.write: true") {
		t.Fatalf("expected read-only error, got: %s", dbResultText(t, res))
	}
}

func TestQueryRejectsWriteSQLEvenOnWritableSource(t *testing.T) {
	res, err := handleQuery(dbAccessTestPool(true), config.Defaults{MaxRows: 100}, nil)(context.Background(), dbTestCall(map[string]any{
		"source": "primary",
		"sql":    "UPDATE users SET active = 1",
	}))
	if err != nil {
		t.Fatalf("handleQuery returned protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("db_query accepted write SQL: %s", dbResultText(t, res))
	}
}

func TestPoolSeparatesSameSourceNameByEnvironment(t *testing.T) {
	pro := &Source{Environment: "pro", Key: "platform"}
	local := &Source{Environment: "local", Key: "platform"}
	pool := &Pool{
		sources: map[string]map[string]*Source{
			"pro":   {"platform": pro},
			"local": {"platform": local},
		},
		ordered: []*Source{local, pro},
	}
	if got, err := pool.Get("pro", "platform"); err != nil || got != pro {
		t.Fatalf("Get(pro, platform) = %v, %v", got, err)
	}
	if got, err := pool.Get("local", "platform"); err != nil || got != local {
		t.Fatalf("Get(local, platform) = %v, %v", got, err)
	}
}

func dbAccessTestPool(write bool) *Pool {
	src := &Source{
		Environment: "test1", Key: "primary", Write: write, Driver: mysql.Driver{}, QueryTimeout: time.Second,
	}
	return &Pool{
		sources: map[string]map[string]*Source{"test1": {"primary": src}},
		ordered: []*Source{src},
	}
}

func dbTestCall(args map[string]any) mcp.CallToolRequest {
	if _, ok := args["environment"]; !ok {
		args["environment"] = "test1"
	}
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
}

func dbResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("missing tool result content")
	}
	content, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return content.Text
}
