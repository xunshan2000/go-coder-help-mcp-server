package db

import "testing"

func TestFirstToken(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"SELECT 1", "SELECT"},
		{"  select * from t", "SELECT"},
		{"\n\tSHOW TABLES", "SHOW"},
		{"-- comment\nUPDATE t SET x=1", "UPDATE"},
		{"/* block */ WITH cte AS (SELECT 1) SELECT * FROM cte", "WITH"},
		{"", ""},
		{"   \t", ""},
		{"/*! SELECT 1 */", ""},
	}
	for _, c := range cases {
		got := FirstToken(c.in)
		if got != c.want {
			t.Errorf("FirstToken(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsMultiStatement(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"SELECT 1", false},
		{"SELECT 1;", false},
		{"SELECT 1 ; ", false},
		{"SELECT 1; SELECT 2", true},
		// 注意：IsMultiStatement 是保守策略。字符串字面量里的分号也会被判定为多语句。
		// 这是刻意的——DSN 层 multiStatements 已关闭，这里是 defense-in-depth；
		// 需要在字符串里放分号时，使用 CONCAT(..., CHAR(59), ...) 代替。
		{"SELECT 'a;b'", true},
		{"-- hi;\nSELECT 1", false},
		{"/* ; */ SELECT 1", false},
	}
	for _, c := range cases {
		got := IsMultiStatement(c.in)
		if got != c.want {
			t.Errorf("IsMultiStatement(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestAllowReadSQL(t *testing.T) {
	okCases := []string{
		"SELECT 1",
		"select * from t",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"SHOW TABLES",
		"DESCRIBE users",
		"DESC users",
		"EXPLAIN SELECT 1",
		"VALUES (1)",
	}
	badCases := []string{
		"UPDATE t SET x=1",
		"INSERT INTO t VALUES (1)",
		"DELETE FROM t",
		"-- comment\nUPDATE t SET x=1",
		"/* block */ DROP TABLE t",
		"SELECT 1; SELECT 2",
		"",
		"   ",
	}
	for _, c := range okCases {
		if err := AllowReadSQL(c); err != nil {
			t.Errorf("AllowReadSQL(%q) unexpected err: %v", c, err)
		}
	}
	for _, c := range badCases {
		if err := AllowReadSQL(c); err == nil {
			t.Errorf("AllowReadSQL(%q) expected err, got nil", c)
		}
	}
}

func TestAllowWriteSQL(t *testing.T) {
	okCases := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET x=1",
		"DELETE FROM t",
		"REPLACE INTO t VALUES (1)",
	}
	badCases := []string{
		"SELECT 1",
		"SHOW TABLES",
		"DESCRIBE users",
		"EXPLAIN SELECT 1",
		"INSERT INTO t VALUES (1); DELETE FROM t",
		"",
	}
	for _, c := range okCases {
		if err := AllowWriteSQL(c); err != nil {
			t.Errorf("AllowWriteSQL(%q) unexpected err: %v", c, err)
		}
	}
	for _, c := range badCases {
		if err := AllowWriteSQL(c); err == nil {
			t.Errorf("AllowWriteSQL(%q) expected err, got nil", c)
		}
	}
}

func TestStripSQLComments(t *testing.T) {
	// /*! ... */ should be preserved
	in := "/*! UPDATE t SET x=1 */"
	got := StripSQLComments(in)
	if got != in {
		t.Errorf("StripSQLComments should preserve /*! */, got %q", got)
	}

	// regular block & line removed (line comment 保留末尾换行)
	in2 := "SELECT /* foo */ 1 -- trailing\n"
	got2 := StripSQLComments(in2)
	if got2 != "SELECT  1 \n" {
		t.Errorf("StripSQLComments(%q) = %q", in2, got2)
	}
}
