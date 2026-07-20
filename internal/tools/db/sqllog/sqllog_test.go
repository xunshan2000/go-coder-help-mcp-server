package sqllog

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogger_BasicWriteAndCloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rows := 3
	trunc := false
	l.Write(context.Background(), Entry{
		TS:          "2026-05-06 10:00:00",
		Environment: "pro",
		Source:      "default",
		Mode:        "r",
		Tool:        "db_query",
		SQL:         "SELECT * FROM t WHERE id = ?",
		SQLRendered: "SELECT * FROM t WHERE id = 1",
		Args:        []any{1},
		DurationMs:  12,
		Rows:        &rows,
		Truncated:   &trunc,
		OK:          true,
	})
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// idempotent
	if err := l.Close(); err != nil {
		t.Fatalf("Close (second): %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "sql-*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected 1 sql-*.log, got %v err=%v", files, err)
	}
	buf, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	var e Entry
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.Environment != "pro" || e.Tool != "db_query" || e.OK != true || e.Rows == nil || *e.Rows != 3 {
		t.Fatalf("parsed entry unexpected: %+v", e)
	}

	// stable field ordering: ts first, ok before err-absence
	if !strings.HasPrefix(lines[0], `{"ts":"`) {
		t.Fatalf("expected leading ts field, got: %s", lines[0])
	}
}

func TestLogger_Concurrency(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	var wg sync.WaitGroup
	const N = 50
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.Write(context.Background(), Entry{
				TS:         "2026-05-06 10:00:00",
				Source:     "default",
				Mode:       "r",
				Tool:       "db_query",
				SQL:        "SELECT ?",
				Args:       []any{i},
				DurationMs: 1,
				OK:         true,
			})
		}(i)
	}
	wg.Wait()
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "sql-*.log"))
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	f, err := os.Open(files[0])
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	count := 0
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line %d unmarshal fail: %v (%q)", count, err, sc.Text())
		}
		count++
	}
	if count != N {
		t.Fatalf("expected %d lines, got %d", N, count)
	}
}

func TestLogger_DailyRotation(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// inject fake now
	day1 := time.Date(2026, 4, 30, 23, 59, 59, 0, time.Local)
	day2 := day1.Add(2 * time.Second) // next local day
	current := day1
	l.now = func() time.Time { return current }

	l.Write(context.Background(), Entry{TS: "x", Tool: "db_query", OK: true})
	current = day2
	l.Write(context.Background(), Entry{TS: "y", Tool: "db_query", OK: true})
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "sql-*.log"))
	if len(files) != 2 {
		t.Fatalf("expected 2 files (two dates), got %d: %v", len(files), files)
	}
	wantSet := map[string]bool{
		"sql-" + day1.Format("2006-01-02") + ".log": false,
		"sql-" + day2.Format("2006-01-02") + ".log": false,
	}
	for _, f := range files {
		wantSet[filepath.Base(f)] = true
	}
	for name, found := range wantSet {
		if !found {
			t.Fatalf("missing expected file %s in %v", name, files)
		}
	}
}

func TestLogger_WriteDoesNotPanicOnClosedFile(t *testing.T) {
	// 模拟 "l.file 已被外部关闭" 的极端情况：Write 不应 panic，stderr 告警后静默返回
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// 先写一条正常建立文件句柄
	l.Write(context.Background(), Entry{TS: "x", Tool: "db_query", OK: true})
	// 直接关掉底层 file（不走 Close，制造异常）
	l.mu.Lock()
	if l.file != nil {
		_ = l.file.Close()
	}
	l.mu.Unlock()
	// 此时 l.file 非 nil 但底层 fd 已关闭；Write 应捕获写错误并 stderr 告警，不 panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Write panicked after file force-close: %v", r)
		}
	}()
	l.Write(context.Background(), Entry{TS: "y", Tool: "db_query", OK: true})
}

func TestLogger_ReadOnlyDirFallsBackToStderr(t *testing.T) {
	// Windows 下很难让 dir 只读；跳过
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory semantics differ on Windows")
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "logs")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.Chmod(dir, 0o700)

	l := &Logger{dir: dir, now: time.Now}
	// 不 panic，不抛错
	l.Write(context.Background(), Entry{TS: "x", Tool: "db_query", OK: true})
}
