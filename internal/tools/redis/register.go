package redis

import "example.com/mcp-server/internal/tools"

func Register(r *tools.Registry, pool *Pool) {
	r.Add(newHelpTool(pool), handleHelp(pool))
	r.Add(newScanKeysTool(pool), handleScanKeys(pool))
	r.Add(newGetTool(pool), handleGet(pool))
	if !pool.HasWritableSources() {
		return
	}
	r.Add(newSetTool(pool), handleSet(pool))
	r.Add(newDeleteTool(pool), handleDelete(pool))
	r.Add(newHSetTool(pool), handleHSet(pool))
	r.Add(newHDelTool(pool), handleHDel(pool))
	r.Add(newLPushTool(pool), handleLPush(pool))
	r.Add(newRPushTool(pool), handleRPush(pool))
	r.Add(newSAddTool(pool), handleSAdd(pool))
	r.Add(newSRemTool(pool), handleSRem(pool))
	r.Add(newZAddTool(pool), handleZAdd(pool))
	r.Add(newZRemTool(pool), handleZRem(pool))
}
