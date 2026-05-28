package mysql

import (
	"strings"
	"testing"
	"time"
)

func TestRenderSQL_Basics(t *testing.T) {
	d := Driver{}
	cases := []struct {
		name string
		sql  string
		args []any
		want string
	}{
		{
			name: "no placeholders empty args",
			sql:  "SELECT 1",
			args: nil,
			want: "SELECT 1",
		},
		{
			name: "no placeholders with args ignored",
			sql:  "SELECT 1",
			args: []any{1, 2, 3},
			want: "SELECT 1",
		},
		{
			name: "single int",
			sql:  "SELECT * FROM users WHERE id = ?",
			args: []any{42},
			want: "SELECT * FROM users WHERE id = 42",
		},
		{
			name: "string with single quote escape",
			sql:  "SELECT * FROM users WHERE name = ?",
			args: []any{"O'Reilly"},
			want: "SELECT * FROM users WHERE name = 'O\\'Reilly'",
		},
		{
			name: "nil -> NULL, bool -> 1/0",
			sql:  "INSERT INTO t VALUES (?,?,?)",
			args: []any{nil, true, false},
			want: "INSERT INTO t VALUES (NULL,1,0)",
		},
		{
			name: "float",
			sql:  "SELECT ?",
			args: []any{3.14},
			want: "SELECT 3.14",
		},
		{
			name: "bytes as hex literal",
			sql:  "INSERT INTO bin VALUES (?)",
			args: []any{[]byte{0x48, 0x65, 0x6c, 0x6c, 0x6f}},
			want: "INSERT INTO bin VALUES (X'48656c6c6f')",
		},
		{
			name: "time.Time UTC",
			sql:  "UPDATE t SET created_at = ?",
			args: []any{time.Date(2026, 4, 30, 17, 59, 6, 0, time.UTC)},
			want: "UPDATE t SET created_at = '2026-04-30 17:59:06'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := d.RenderSQL(tc.sql, tc.args)
			if got != tc.want {
				t.Fatalf("RenderSQL(%q, %v) = %q, want %q", tc.sql, tc.args, got, tc.want)
			}
		})
	}
}

func TestRenderSQL_SkipsQuestionInsideQuotes(t *testing.T) {
	d := Driver{}

	// 单引号内 ? 不应被替换；后面 WHERE 的 ? 才替换
	sql := "SELECT 'a?b', col FROM t WHERE id = ?"
	got := d.RenderSQL(sql, []any{7})
	want := "SELECT 'a?b', col FROM t WHERE id = 7"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	// 双引号内 ? 不应被替换
	sql2 := `SELECT "q?m" FROM t WHERE k = ?`
	got2 := d.RenderSQL(sql2, []any{1})
	want2 := `SELECT "q?m" FROM t WHERE k = 1`
	if got2 != want2 {
		t.Fatalf("got %q want %q", got2, want2)
	}

	// 反引号内 ? 不应被替换
	sql3 := "SELECT `col?name` FROM t WHERE id = ?"
	got3 := d.RenderSQL(sql3, []any{1})
	want3 := "SELECT `col?name` FROM t WHERE id = 1"
	if got3 != want3 {
		t.Fatalf("got %q want %q", got3, want3)
	}
}

func TestRenderSQL_UnboundQuestionRemains(t *testing.T) {
	d := Driver{}
	sql := "SELECT * FROM t WHERE a = ? AND b = ? AND c = ?"
	got := d.RenderSQL(sql, []any{1, 2})
	want := "SELECT * FROM t WHERE a = 1 AND b = 2 AND c = ?"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderSQL_ExtraArgsIgnored(t *testing.T) {
	d := Driver{}
	sql := "SELECT ?"
	got := d.RenderSQL(sql, []any{1, 2, 3})
	want := "SELECT 1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderSQL_EscapeControlChars(t *testing.T) {
	d := Driver{}
	got := d.RenderSQL("INSERT INTO t VALUES (?)", []any{"a\nb\tc\r\\d\"e"})
	// Expect backslash escapes for each control / quote character
	wantPieces := []string{"\\n", "\\t", "\\r", "\\\\", "\\\""}
	for _, p := range wantPieces {
		if !strings.Contains(got, p) {
			t.Fatalf("expected piece %q in %q", p, got)
		}
	}
}

func TestRenderSQL_CTE(t *testing.T) {
	d := Driver{}
	sql := "WITH recent AS (SELECT * FROM orders WHERE created_at > ?) SELECT * FROM recent WHERE user_id = ?"
	got := d.RenderSQL(sql, []any{"2026-01-01", 42})
	want := "WITH recent AS (SELECT * FROM orders WHERE created_at > '2026-01-01') SELECT * FROM recent WHERE user_id = 42"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderSQL_TypeMix(t *testing.T) {
	d := Driver{}
	sql := "INSERT INTO t (i, s, b, n, bt, tm) VALUES (?, ?, ?, ?, ?, ?)"
	got := d.RenderSQL(sql, []any{
		int64(99),
		"hi",
		true,
		nil,
		[]byte{0xFF},
		time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC),
	})
	want := "INSERT INTO t (i, s, b, n, bt, tm) VALUES (99, 'hi', 1, NULL, X'ff', '2026-05-06 10:00:00')"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestRenderSQL_DoubledSingleQuoteInside(t *testing.T) {
	d := Driver{}
	// SQL 标准中 '' 表示字面量单引号；我们应当把内部 '' 视为仍在引号内
	sql := "SELECT 'it''s ok? here', ?"
	got := d.RenderSQL(sql, []any{42})
	want := "SELECT 'it''s ok? here', 42"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
