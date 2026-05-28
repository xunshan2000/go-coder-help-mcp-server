package db

import (
	"fmt"
	"strings"
)

var readOnlyFirstTokens = map[string]struct{}{
	"SELECT":   {},
	"SHOW":     {},
	"DESCRIBE": {},
	"DESC":     {},
	"EXPLAIN":  {},
	"WITH":     {},
	"VALUES":   {},
}

var writeExcludedFirstTokens = map[string]struct{}{
	"SELECT":   {},
	"SHOW":     {},
	"DESCRIBE": {},
	"DESC":     {},
	"EXPLAIN":  {},
}

// StripSQLComments 去除 SQL 中的行注释 (-- ... 到换行) 与块注释 (/* ... */)。
// MySQL 特定的可执行块注释 /*! ... */ 被保留，因为它们是可执行 SQL，不是注释。
func StripSQLComments(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))

	i := 0
	n := len(sql)
	for i < n {
		// -- line comment (must be followed by space / tab or EOL per SQL-92; allow anything here)
		if i+1 < n && sql[i] == '-' && sql[i+1] == '-' {
			// skip until newline
			for i < n && sql[i] != '\n' {
				i++
			}
			continue
		}
		// block comment
		if i+1 < n && sql[i] == '/' && sql[i+1] == '*' {
			// /*! ... */ is MySQL executable comment — keep as-is.
			if i+2 < n && sql[i+2] == '!' {
				// copy until closing */ inclusive
				out.WriteByte(sql[i])
				out.WriteByte(sql[i+1])
				i += 2
				for i < n {
					if i+1 < n && sql[i] == '*' && sql[i+1] == '/' {
						out.WriteByte(sql[i])
						out.WriteByte(sql[i+1])
						i += 2
						break
					}
					out.WriteByte(sql[i])
					i++
				}
				continue
			}
			// regular block comment: skip until */
			i += 2
			for i < n {
				if i+1 < n && sql[i] == '*' && sql[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		out.WriteByte(sql[i])
		i++
	}
	return out.String()
}

// FirstToken 返回 SQL 首个大写的 ASCII 标识符 token。
// 遇到非字母立即停止；空或全空白返回 ""。
func FirstToken(sql string) string {
	stripped := StripSQLComments(sql)
	trimmed := strings.TrimLeft(stripped, " \t\r\n\f\v")
	var b strings.Builder
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			if c >= 'a' && c <= 'z' {
				c = c - 'a' + 'A'
			}
			b.WriteByte(c)
			continue
		}
		break
	}
	return b.String()
}

// IsMultiStatement 判定 SQL 是否包含多语句。
// 策略：剥注释后的 SQL trim 掉尾部空白与单个尾分号，
// 剩余字符串中若仍出现 `;` 则视为多语句。
func IsMultiStatement(sql string) bool {
	stripped := StripSQLComments(sql)
	trimmed := strings.TrimRight(stripped, " \t\r\n;")
	return strings.Contains(trimmed, ";")
}

func AllowReadSQL(sql string) error {
	if IsMultiStatement(sql) {
		return fmt.Errorf("禁止多语句 SQL")
	}
	tok := FirstToken(sql)
	if tok == "" {
		return fmt.Errorf("SQL 为空")
	}
	if _, ok := readOnlyFirstTokens[tok]; !ok {
		return fmt.Errorf("该 source 仅允许只读语句，但首关键字为 %s (允许集合: SELECT / SHOW / DESCRIBE / DESC / EXPLAIN / WITH / VALUES)", tok)
	}
	return nil
}

func AllowWriteSQL(sql string) error {
	if IsMultiStatement(sql) {
		return fmt.Errorf("禁止多语句 SQL")
	}
	tok := FirstToken(sql)
	if tok == "" {
		return fmt.Errorf("SQL 为空")
	}
	if _, ok := writeExcludedFirstTokens[tok]; ok {
		return fmt.Errorf("db_execute 不支持只读语句 (首关键字 %s)，请改用 db_query", tok)
	}
	return nil
}
