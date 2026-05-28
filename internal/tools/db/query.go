package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/tools/db/driver"
	"example.com/mcp-server/internal/tools/db/sqllog"
)

func newQueryTool() mcp.Tool {
	return mcp.NewTool("db_query",
		mcp.WithDescription("Run read-only SQL on a source. Use this for SELECT, SHOW, DESCRIBE, DESC, EXPLAIN, or WITH. If you do not know the source, call db_help first. If you do not know the table, call db_list_tables first. If you do not know the columns, call db_describe_table first. Multi-statement SQL is not allowed."),
		mcp.WithString("source",
			mcp.Description("Database source key. Call db_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("sql",
			mcp.Description("Read-only SQL statement. Prefer adding LIMIT when practical. Multi-statement SQL is not allowed."),
			mcp.Required(),
		),
		mcp.WithArray("args",
			mcp.Description("Bound values for ? placeholders, in order. Use args instead of building SQL with string concatenation."),
			mcp.Items(map[string]any{}),
		),
		mcp.WithNumber("max_rows",
			mcp.Description("Requested row cap for this call. Can lower the global limit, but cannot raise it."),
		),
	)
}

func handleQuery(pool *Pool, defaults config.Defaults, logger *sqllog.Logger) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		args := req.GetArguments()

		sourceName, sourceErr := readString(args, "source")
		sqlText, sqlErr := readString(args, "sql")
		bindings := readAnySlice(args, "args")

		var (
			mode        string
			renderedSQL string
			errMsg      string
			rowsPtr     *int
			truncPtr    *bool
			okFlag      bool
		)
		defer func() {
			if logger == nil {
				return
			}
			logger.Write(ctx, sqllog.Entry{
				TS:          sqllog.NowTS(),
				Source:      sourceName,
				Mode:        mode,
				Tool:        "db_query",
				SQL:         sqlText,
				SQLRendered: renderedSQL,
				Args:        bindings,
				DurationMs:  time.Since(start).Milliseconds(),
				Rows:        rowsPtr,
				Truncated:   truncPtr,
				OK:          okFlag,
				Err:         errMsg,
			})
		}()

		if sourceErr != nil {
			errMsg = sourceErr.Error()
			return renderErrorf("%s", errMsg), nil
		}
		if sqlErr != nil {
			errMsg = sqlErr.Error()
			return renderErrorf("%s", errMsg), nil
		}

		src, err := pool.Get(sourceName)
		if err != nil {
			errMsg = err.Error()
			return renderErrorf("%s", errMsg), nil
		}
		mode = string(src.Mode)
		renderedSQL = src.Driver.RenderSQL(sqlText, bindings)

		if src.Mode == ModeR {
			if err := AllowReadSQL(sqlText); err != nil {
				errMsg = fmt.Sprintf("source=%s mode=r: %v", src.Key, err)
				return renderErrorf("%s", errMsg), nil
			}
		}

		maxRows := defaults.MaxRows
		if raw, ok := args["max_rows"]; ok && raw != nil {
			if n, ok := asInt(raw); ok && n > 0 && n < maxRows {
				maxRows = n
			}
		}

		queryCtx, cancel := context.WithTimeout(ctx, src.QueryTimeout)
		defer cancel()

		columns, rows, rowCount, truncated, execErr := runQuery(queryCtx, src, sqlText, bindings, maxRows)
		if execErr != nil {
			errMsg = fmt.Sprintf("query failed [%s]: %v", src.Key, execErr)
			return renderErrorf("%s", errMsg), nil
		}

		rc := rowCount
		tr := truncated
		rowsPtr = &rc
		truncPtr = &tr
		okFlag = true

		return renderJSONResult(map[string]any{
			"source":    src.Key,
			"columns":   columns,
			"rows":      rows,
			"row_count": rowCount,
			"truncated": truncated,
		}), nil
	}
}

func runQuery(ctx context.Context, src *Source, sqlText string, bindings []any, maxRows int) (columns []string, rows [][]any, rowCount int, truncated bool, err error) {
	var sqlRows *sql.Rows
	var roExec driver.ReadOnlyExec

	if src.Mode == ModeR {
		roExec, err = src.Driver.BeginReadOnly(ctx, src.DB)
		if err != nil {
			return nil, nil, 0, false, fmt.Errorf("begin read-only context failed: %w", err)
		}
		defer func() {
			if roExec != nil {
				_ = roExec.Rollback()
			}
		}()
		sqlRows, err = roExec.QueryContext(ctx, sqlText, bindings...)
	} else {
		sqlRows, err = src.DB.QueryContext(ctx, sqlText, bindings...)
	}
	if err != nil {
		return nil, nil, 0, false, err
	}
	defer sqlRows.Close()

	columns, err = sqlRows.Columns()
	if err != nil {
		return nil, nil, 0, false, err
	}

	rows = [][]any{}
	for sqlRows.Next() {
		if len(rows) >= maxRows {
			truncated = true
			break
		}
		raw := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := sqlRows.Scan(dest...); err != nil {
			return nil, nil, 0, false, err
		}
		rows = append(rows, convertRowValues(raw))
	}
	if err := sqlRows.Err(); err != nil {
		return nil, nil, 0, false, err
	}

	rowCount = len(rows)

	if src.Mode == ModeR && roExec != nil {
		if cerr := roExec.Commit(); cerr != nil {
			return nil, nil, 0, false, fmt.Errorf("read-only commit failed: %w", cerr)
		}
	}
	return columns, rows, rowCount, truncated, nil
}

func convertRowValues(raw []any) []any {
	out := make([]any, len(raw))
	for i, v := range raw {
		switch x := v.(type) {
		case []byte:
			out[i] = string(x)
		default:
			out[i] = v
		}
	}
	return out
}

func readAnySlice(args map[string]any, name string) []any {
	raw, ok := args[name]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []any:
		return v
	default:
		return nil
	}
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	}
	return 0, false
}
