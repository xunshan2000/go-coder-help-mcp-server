package db

import (
	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/tools"
	"example.com/mcp-server/internal/tools/db/sqllog"

	_ "example.com/mcp-server/internal/tools/db/drivers/mysql"
)

func Register(r *tools.Registry, pool *Pool, defaults config.Defaults, logger *sqllog.Logger) {
	r.Add(newHelpTool(pool, defaults), handleHelp(pool, defaults))
	r.Add(newListTablesTool(), handleListTables(pool))
	r.Add(newDescribeTableTool(), handleDescribeTable(pool))
	r.Add(newQueryTool(), handleQuery(pool, defaults, logger))
	r.Add(newExecuteTool(), handleExecute(pool, logger))
}
