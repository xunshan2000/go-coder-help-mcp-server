package redis

import "example.com/mcp-server/internal/tools"

func Register(r *tools.Registry, pool *Pool) {
	r.Add(newHelpTool(pool), handleHelp(pool))
	r.Add(newScanKeysTool(pool), handleScanKeys(pool))
	r.Add(newGetTool(pool), handleGet(pool))
}
