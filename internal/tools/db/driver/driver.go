package driver

import (
	"context"
	"database/sql"

	"example.com/mcp-server/internal/config"
)

type Driver interface {
	Name() string
	SQLDriverName() string
	BuildDSN(cfg config.SourceConfig) (string, error)
	QuoteIdentifier(name string) string

	ListTables(ctx context.Context, db *sql.DB) ([]string, error)
	DescribeTable(ctx context.Context, db *sql.DB, table string) (*TableSchema, error)

	BeginReadOnly(ctx context.Context, db *sql.DB) (ReadOnlyExec, error)

	// RenderSQL 把参数化 SQL 的 ? 占位符按驱动方言内联为可读字面量，
	// 仅供审计日志 / 人工阅读；返回值 MUST NOT 作为 SQL 发送给数据库。
	// 实现 MAY 在无法渲染某个值时返回空字符串或原样 SQL；调用方 MUST 容忍空结果。
	RenderSQL(sql string, args []any) string
}

type ReadOnlyExec interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	Commit() error
	Rollback() error
}
