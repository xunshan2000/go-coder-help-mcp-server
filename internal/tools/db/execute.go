package db

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"example.com/mcp-server/internal/tools/db/sqllog"
)

func newExecuteTool() mcp.Tool {
	return mcp.NewTool("db_execute",
		mcp.WithDescription("Run write SQL on a source configured with write=true, such as INSERT, UPDATE, or DELETE. If you only need to read data, use db_query instead. Call db_help first if you are not sure whether the source allows writes."),
		mcp.WithString("environment",
			mcp.Description("Environment key, such as pro, local, test1, or another value listed by db_help."),
			mcp.Required(),
		),
		mcp.WithString("source",
			mcp.Description("Database source key. The source must be configured with write=true. Call db_help first to see available sources."),
			mcp.Required(),
		),
		mcp.WithString("sql",
			mcp.Description("Write SQL statement. The first keyword cannot be SELECT, SHOW, DESCRIBE, DESC, or EXPLAIN. Multi-statement SQL is not allowed."),
			mcp.Required(),
		),
		mcp.WithArray("args",
			mcp.Description("Bound values for ? placeholders, in order."),
			mcp.Items(map[string]any{}),
		),
	)
}

func handleExecute(pool *Pool, logger *sqllog.Logger) func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		args := req.GetArguments()

		environmentName, environmentErr := readString(args, "environment")
		sourceName, sourceErr := readString(args, "source")
		sqlText, sqlErr := readString(args, "sql")
		bindings := readAnySlice(args, "args")

		var (
			mode         string
			renderedSQL  string
			errMsg       string
			rowsAffPtr   *int64
			lastInsIDPtr *int64
			okFlag       bool
		)
		defer func() {
			if logger == nil {
				return
			}
			logger.Write(ctx, sqllog.Entry{
				TS:           sqllog.NowTS(),
				Environment:  environmentName,
				Source:       sourceName,
				Mode:         mode,
				Tool:         "db_execute",
				SQL:          sqlText,
				SQLRendered:  renderedSQL,
				Args:         bindings,
				DurationMs:   time.Since(start).Milliseconds(),
				RowsAffected: rowsAffPtr,
				LastInsertID: lastInsIDPtr,
				OK:           okFlag,
				Err:          errMsg,
			})
		}()

		if environmentErr != nil {
			errMsg = environmentErr.Error()
			return renderErrorf("%s", errMsg), nil
		}
		if sourceErr != nil {
			errMsg = sourceErr.Error()
			return renderErrorf("%s", errMsg), nil
		}
		if sqlErr != nil {
			errMsg = sqlErr.Error()
			return renderErrorf("%s", errMsg), nil
		}

		src, err := pool.Get(environmentName, sourceName)
		if err != nil {
			errMsg = err.Error()
			return renderErrorf("%s", errMsg), nil
		}
		mode = src.auditMode()
		renderedSQL = src.Driver.RenderSQL(sqlText, bindings)

		if !src.Write {
			errMsg = fmt.Sprintf("source=%s/%s is read-only; set environments.%s.databases.%s.write: true to enable db_execute", src.Environment, src.Key, src.Environment, src.Key)
			return renderErrorf("%s", errMsg), nil
		}

		if err := AllowWriteSQL(sqlText); err != nil {
			errMsg = fmt.Sprintf("source=%s/%s: %v", src.Environment, src.Key, err)
			return renderErrorf("%s", errMsg), nil
		}

		queryCtx, cancel := context.WithTimeout(ctx, src.QueryTimeout)
		defer cancel()

		res, err := src.DB.ExecContext(queryCtx, sqlText, bindings...)
		if err != nil {
			errMsg = fmt.Sprintf("execute failed [%s/%s]: %v", src.Environment, src.Key, err)
			return renderErrorf("%s", errMsg), nil
		}

		var rowsAffected int64
		if n, ierr := res.RowsAffected(); ierr == nil {
			rowsAffected = n
		}
		var lastInsertID int64
		if n, ierr := res.LastInsertId(); ierr == nil {
			lastInsertID = n
		}
		rowsAffPtr = &rowsAffected
		lastInsIDPtr = &lastInsertID
		okFlag = true

		return renderJSONResult(map[string]any{
			"environment":    src.Environment,
			"source":         src.Key,
			"rows_affected":  rowsAffected,
			"last_insert_id": lastInsertID,
		}), nil
	}
}
