package mysql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"example.com/mcp-server/internal/config"
	"example.com/mcp-server/internal/tools/db/driver"
)

func init() {
	driver.Register(&Driver{})
}

type Driver struct{}

func (Driver) Name() string          { return "mysql" }
func (Driver) SQLDriverName() string { return "mysql" }

func (Driver) QuoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func (Driver) BuildDSN(cfg config.SourceConfig) (string, error) {
	port := cfg.Port
	if port == 0 {
		port = 3306
	}

	params := map[string]string{
		"parseTime": "true",
	}

	charset := cfg.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	params["charset"] = charset

	if cfg.Collation != "" {
		params["collation"] = cfg.Collation
	}

	timezone := cfg.Timezone
	if timezone == "" {
		timezone = "UTC"
	}
	params["loc"] = timezone

	if cfg.Pool.ConnectTimeout > 0 {
		params["timeout"] = cfg.Pool.ConnectTimeout.String()
	}

	for k, v := range cfg.Params {
		params[k] = v
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var query strings.Builder
	for i, k := range keys {
		if i > 0 {
			query.WriteByte('&')
		}
		query.WriteString(url.QueryEscape(k))
		query.WriteByte('=')
		query.WriteString(url.QueryEscape(params[k]))
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s",
		cfg.Username,
		cfg.Password,
		cfg.Host,
		port,
		cfg.Database,
		query.String(),
	)
	return dsn, nil
}

func (Driver) ListTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SHOW TABLES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tables == nil {
		tables = []string{}
	}
	return tables, nil
}

func (d Driver) DescribeTable(ctx context.Context, db *sql.DB, table string) (*driver.TableSchema, error) {
	quoted := d.QuoteIdentifier(table)

	colRows, err := db.QueryContext(ctx, "SHOW FULL COLUMNS FROM "+quoted)
	if err != nil {
		return nil, err
	}
	defer colRows.Close()

	colNames, err := colRows.Columns()
	if err != nil {
		return nil, err
	}
	idx := func(name string) int {
		for i, c := range colNames {
			if strings.EqualFold(c, name) {
				return i
			}
		}
		return -1
	}
	iField := idx("Field")
	iType := idx("Type")
	iNull := idx("Null")
	iKey := idx("Key")
	iDefault := idx("Default")

	var columns []driver.ColumnInfo
	for colRows.Next() {
		raw := make([]sql.NullString, len(colNames))
		dest := make([]any, len(colNames))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := colRows.Scan(dest...); err != nil {
			return nil, err
		}
		info := driver.ColumnInfo{
			Name:     nsVal(raw, iField),
			Type:     nsVal(raw, iType),
			Nullable: strings.EqualFold(nsVal(raw, iNull), "YES"),
			Key:      nsVal(raw, iKey),
		}
		if iDefault >= 0 && raw[iDefault].Valid {
			info.Default = raw[iDefault].String
		} else {
			info.Default = nil
		}
		columns = append(columns, info)
	}
	if err := colRows.Err(); err != nil {
		return nil, err
	}

	idxRows, err := db.QueryContext(ctx, "SHOW INDEX FROM "+quoted)
	if err != nil {
		return nil, err
	}
	defer idxRows.Close()

	idxCols, err := idxRows.Columns()
	if err != nil {
		return nil, err
	}
	idxOf := func(name string) int {
		for i, c := range idxCols {
			if strings.EqualFold(c, name) {
				return i
			}
		}
		return -1
	}
	iKeyName := idxOf("Key_name")
	iColName := idxOf("Column_name")
	iNonUniq := idxOf("Non_unique")
	iSeqInIdx := idxOf("Seq_in_index")

	type pendingIdx struct {
		name   string
		cols   map[int]string
		unique bool
	}
	idxMap := map[string]*pendingIdx{}
	var idxOrder []string

	for idxRows.Next() {
		raw := make([]sql.NullString, len(idxCols))
		dest := make([]any, len(idxCols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := idxRows.Scan(dest...); err != nil {
			return nil, err
		}
		name := nsVal(raw, iKeyName)
		col := nsVal(raw, iColName)
		nonUniq := nsVal(raw, iNonUniq)
		seq := 0
		if iSeqInIdx >= 0 && raw[iSeqInIdx].Valid {
			fmt.Sscanf(raw[iSeqInIdx].String, "%d", &seq)
		}
		p, ok := idxMap[name]
		if !ok {
			p = &pendingIdx{
				name:   name,
				cols:   map[int]string{},
				unique: nonUniq == "0",
			}
			idxMap[name] = p
			idxOrder = append(idxOrder, name)
		}
		p.cols[seq] = col
	}
	if err := idxRows.Err(); err != nil {
		return nil, err
	}

	var indexes []driver.IndexInfo
	for _, name := range idxOrder {
		p := idxMap[name]
		seqs := make([]int, 0, len(p.cols))
		for k := range p.cols {
			seqs = append(seqs, k)
		}
		sort.Ints(seqs)
		cols := make([]string, 0, len(seqs))
		for _, s := range seqs {
			cols = append(cols, p.cols[s])
		}
		indexes = append(indexes, driver.IndexInfo{
			Name:    p.name,
			Columns: cols,
			Unique:  p.unique,
		})
	}
	if indexes == nil {
		indexes = []driver.IndexInfo{}
	}
	if columns == nil {
		columns = []driver.ColumnInfo{}
	}

	return &driver.TableSchema{
		Columns: columns,
		Indexes: indexes,
	}, nil
}

func nsVal(raw []sql.NullString, idx int) string {
	if idx < 0 || idx >= len(raw) {
		return ""
	}
	if !raw[idx].Valid {
		return ""
	}
	return raw[idx].String
}

func (Driver) BeginReadOnly(ctx context.Context, db *sql.DB) (driver.ReadOnlyExec, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return &roExec{tx: tx}, nil
}

type roExec struct {
	tx *sql.Tx
}

func (e *roExec) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return e.tx.QueryContext(ctx, query, args...)
}

func (e *roExec) Commit() error   { return e.tx.Commit() }
func (e *roExec) Rollback() error { return e.tx.Rollback() }

// RenderSQL 在非引号状态下扫描 ? 并按 MySQL 方言内联对应 arg 的字面量；
// 仅供审计 / 人工阅读，不可作为 SQL 执行。
func (Driver) RenderSQL(sqlText string, args []any) string {
	if len(args) == 0 || !strings.ContainsRune(sqlText, '?') {
		return sqlText
	}
	var out strings.Builder
	out.Grow(len(sqlText) + 16*len(args))

	argIdx := 0
	quote := byte(0) // 0 = none; otherwise '\'', '"', '`'
	i := 0
	for i < len(sqlText) {
		c := sqlText[i]
		if quote != 0 {
			out.WriteByte(c)
			if c == '\\' && quote != '`' && i+1 < len(sqlText) {
				out.WriteByte(sqlText[i+1])
				i += 2
				continue
			}
			if c == quote {
				if i+1 < len(sqlText) && sqlText[i+1] == quote {
					out.WriteByte(sqlText[i+1])
					i += 2
					continue
				}
				quote = 0
			}
			i++
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
			out.WriteByte(c)
			i++
		case '?':
			if argIdx < len(args) {
				out.WriteString(mysqlLiteral(args[argIdx]))
				argIdx++
			} else {
				out.WriteByte('?')
			}
			i++
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func mysqlLiteral(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case bool:
		if x {
			return "1"
		}
		return "0"
	case int:
		return fmt.Sprintf("%d", x)
	case int8:
		return fmt.Sprintf("%d", x)
	case int16:
		return fmt.Sprintf("%d", x)
	case int32:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	case uint:
		return fmt.Sprintf("%d", x)
	case uint8:
		return fmt.Sprintf("%d", x)
	case uint16:
		return fmt.Sprintf("%d", x)
	case uint32:
		return fmt.Sprintf("%d", x)
	case uint64:
		return fmt.Sprintf("%d", x)
	case float32:
		return fmt.Sprintf("%v", x)
	case float64:
		return fmt.Sprintf("%v", x)
	case string:
		return "'" + mysqlEscapeString(x) + "'"
	case []byte:
		return "X'" + hex.EncodeToString(x) + "'"
	case time.Time:
		return "'" + x.UTC().Format("2006-01-02 15:04:05") + "'"
	default:
		return "'" + mysqlEscapeString(fmt.Sprintf("%v", v)) + "'"
	}
}

func mysqlEscapeString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case 0:
			b.WriteString("\\0")
		case '\'':
			b.WriteString("\\'")
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		case 0x1A:
			b.WriteString("\\Z")
		case '\b':
			b.WriteString("\\b")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
