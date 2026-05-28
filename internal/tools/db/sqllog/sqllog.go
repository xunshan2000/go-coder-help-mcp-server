// Package sqllog 实现 SQL 审计日志的写入：
// 按本地日期分文件（sql-YYYY-MM-DD.log）、JSONL 格式、并发安全、懒打开、懒切换。
// 写入失败 MUST NOT 影响工具层响应，仅降级到 stderr 一行告警（不含 sql / args 内容）。
package sqllog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry 是单条审计记录。字段按 struct 定义顺序序列化到 JSON，便于 grep 固定前缀。
// 工具特定字段（rows / truncated / rows_affected / last_insert_id）用 *T 便于通过
// omitempty 区分"未参与该工具"与"零值"。
type Entry struct {
	TS           string `json:"ts"`
	Source       string `json:"source"`
	Mode         string `json:"mode"`
	Tool         string `json:"tool"`
	SQL          string `json:"sql"`
	SQLRendered  string `json:"sql_rendered"`
	Args         []any  `json:"args"`
	DurationMs   int64  `json:"duration_ms"`
	Rows         *int   `json:"rows,omitempty"`
	Truncated    *bool  `json:"truncated,omitempty"`
	RowsAffected *int64 `json:"rows_affected,omitempty"`
	LastInsertID *int64 `json:"last_insert_id,omitempty"`
	OK           bool   `json:"ok"`
	Err          string `json:"err,omitempty"`
}

// Logger 持有当前打开的日期文件句柄与互斥锁。
// 生命周期：进程启动 New → 每次 handler defer 调 Write → 进程退出 Close。
type Logger struct {
	dir  string
	now  func() time.Time
	mu   sync.Mutex
	file *os.File
	date string // YYYY-MM-DD，与当前打开文件对应
}

// New 创建 Logger，仅建好 dir（不 Open 任何文件 —— 首次 Write 再懒打开）。
func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("sqllog: mkdir %s: %w", dir, err)
	}
	return &Logger{dir: dir, now: time.Now}, nil
}

// Write 序列化一条 entry + "\n" 到对应日期的文件；跨日懒切换。
// 任何内部错误都 fallthrough 到 stderr 一行告警，MUST NOT 上抛。
func (l *Logger) Write(_ context.Context, e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	today := l.now().Format("2006-01-02")
	if l.file == nil || l.date != today {
		if l.file != nil {
			_ = l.file.Close()
			l.file = nil
		}
		path := filepath.Join(l.dir, "sql-"+today+".log")
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[sqllog] open failed: %v (tool=%s source=%s)\n", err, e.Tool, e.Source)
			return
		}
		l.file = f
		l.date = today
	}

	buf, err := json.Marshal(e)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[sqllog] marshal failed: %v (tool=%s source=%s)\n", err, e.Tool, e.Source)
		return
	}
	buf = append(buf, '\n')
	if _, err := l.file.Write(buf); err != nil {
		fmt.Fprintf(os.Stderr, "[sqllog] write failed: %v (tool=%s source=%s)\n", err, e.Tool, e.Source)
		return
	}
}

// Close 关闭当前打开的文件（若有）；幂等。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	l.date = ""
	return err
}

// NowTS 返回本地时区下 MySQL DATETIME 风格的时间戳字符串。
// 作为 Entry.TS 的统一入口（保持与 sql_rendered 中 time.Time 渲染一致）。
func NowTS() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
