package add

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"

	"example.com/mcp-server/internal/tools"
)

func Register(r *tools.Registry) {
	r.Add(
		mcp.NewTool("add",
			mcp.WithDescription("返回两个数值的和。适用于 ±2^53 以内的数值，超出范围可能丢失精度。"),
			mcp.WithNumber("a",
				mcp.Description("第一个加数"),
				mcp.Required(),
			),
			mcp.WithNumber("b",
				mcp.Description("第二个加数"),
				mcp.Required(),
			),
		),
		handleAdd,
	)
}

func handleAdd(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	a, err := readNumber(args, "a")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	b, err := readNumber(args, "b")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	sum := a + b
	return mcp.NewToolResultText(strconv.FormatFloat(sum, 'f', -1, 64)), nil
}

func readNumber(args map[string]any, name string) (float64, error) {
	raw, ok := args[name]
	if !ok || raw == nil {
		return 0, fmt.Errorf("参数 %q 缺失或无效", name)
	}
	v, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("参数 %q 不是合法的数值", name)
	}
	return v, nil
}
