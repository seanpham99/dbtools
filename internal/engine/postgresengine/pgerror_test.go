package postgresengine

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/lib/pq"
)

func TestTranslatePosition(t *testing.T) {
	tests := []struct {
		name        string
		sqlText     string
		charOffset  int
		wantLine    int
		wantCol     int
		wantLineTxt string
		wantCaret   string
	}{
		{
			name:        "empty string",
			sqlText:     "",
			charOffset:  1,
			wantLine:    0,
			wantCol:     0,
			wantLineTxt: "",
			wantCaret:   "",
		},
		{
			name:        "offset zero",
			sqlText:     "SELECT 1;",
			charOffset:  0,
			wantLine:    0,
			wantCol:     0,
			wantLineTxt: "",
			wantCaret:   "",
		},
		{
			name:        "negative offset",
			sqlText:     "SELECT 1;",
			charOffset:  -5,
			wantLine:    0,
			wantCol:     0,
			wantLineTxt: "",
			wantCaret:   "",
		},
		{
			name:        "out of range offset",
			sqlText:     "SELECT 1;",
			charOffset:  20,
			wantLine:    0,
			wantCol:     0,
			wantLineTxt: "",
			wantCaret:   "",
		},
		{
			name:        "single line first char",
			sqlText:     "SELECT 1;",
			charOffset:  1,
			wantLine:    1,
			wantCol:     1,
			wantLineTxt: "SELECT 1;",
			wantCaret:   "^",
		},
		{
			name:        "single line middle char",
			sqlText:     "SELECT * FROM users;",
			charOffset:  8,
			wantLine:    1,
			wantCol:     8,
			wantLineTxt: "SELECT * FROM users;",
			wantCaret:   "       ^",
		},
		{
			name:        "single line last char",
			sqlText:     "SELECT 1;",
			charOffset:  9,
			wantLine:    1,
			wantCol:     9,
			wantLineTxt: "SELECT 1;",
			wantCaret:   "        ^",
		},
		{
			name: "multiline line 2 start",
			sqlText: "CREATE TABLE users (\n" +
				"    id INT PRIMARY KEY\n" +
				");",
			charOffset:  22, // '    id' starts at offset 22 ('C'...'\n' is 21 runes)
			wantLine:    2,
			wantCol:     1,
			wantLineTxt: "    id INT PRIMARY KEY",
			wantCaret:   "^",
		},
		{
			name: "multiline line 2 column 5",
			sqlText: "CREATE TABLE users (\n" +
				"    id INT PRIMARY KEY\n" +
				");",
			charOffset:  26, // 'i' is col 5
			wantLine:    2,
			wantCol:     5,
			wantLineTxt: "    id INT PRIMARY KEY",
			wantCaret:   "    ^",
		},
		{
			name: "multiline line 3 col 2",
			sqlText: "LINE 1\n" +
				"LINE 2\n" +
				"LINE 3",
			charOffset:  16, // 'I' in 'LINE 3' (line 1 is 7 runes, line 2 is 7 runes, 'L' is 15, 'I' is 16)
			wantLine:    3,
			wantCol:     2,
			wantLineTxt: "LINE 3",
			wantCaret:   " ^",
		},
		{
			name: "crlf line endings",
			sqlText: "SELECT 1;\r\n" +
				"SELECT 2;\r\n",
			charOffset:  12, // 'S' of second line (SELECT 1;\r\n is 11 runes)
			wantLine:    2,
			wantCol:     1,
			wantLineTxt: "SELECT 2;",
			wantCaret:   "^",
		},
		{
			name: "unicode multibyte characters in comments",
			sqlText: "-- 🚀 Initial migration with unicode 数据库\n" +
				"CREATE TABLE test (id INT);",
			// Rune count on line 1: 3 + 1 + 32 + 3 + 1 = 40 runes
			charOffset:  41, // 'C' on line 2
			wantLine:    2,
			wantCol:     1,
			wantLineTxt: "CREATE TABLE test (id INT);",
			wantCaret:   "^",
		},
		{
			name: "unicode position pointing to multibyte char",
			sqlText: "-- 🚀\n" +
				"SELECT '你好世界';",
			// Line 1: "-- 🚀\n" -> 5 runes
			// Line 2: "SELECT '" -> 8 runes -> total 13 runes before '你'
			// CharOffset 14 -> '好' (col 9 on line 2: S(1)E(2)L(3)E(4)C(5)T(6) (7)'(8)你(9) -> wait, '你' is col 9, '好' is col 10!)
			// At offset 14: col is 9 ('你')
			charOffset:  14,
			wantLine:    2,
			wantCol:     9,
			wantLineTxt: "SELECT '你好世界';",
			wantCaret:   "        ^",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, col, lineTxt, caret := TranslatePosition(tt.sqlText, tt.charOffset)
			if line != tt.wantLine || col != tt.wantCol {
				t.Errorf("TranslatePosition() line=%d, col=%d; want line=%d, col=%d", line, col, tt.wantLine, tt.wantCol)
			}
			if lineTxt != tt.wantLineTxt {
				t.Errorf("TranslatePosition() lineTxt=%q; want %q", lineTxt, tt.wantLineTxt)
			}
			if caret != tt.wantCaret {
				t.Errorf("TranslatePosition() caret=%q; want %q", caret, tt.wantCaret)
			}
		})
	}
}

func TestFormatPostgresError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		got := FormatPostgresError(nil, "SELECT 1;", 0)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("non-pq error", func(t *testing.T) {
		orig := errors.New("some io error")
		got := FormatPostgresError(orig, "SELECT 1;", 0)
		if got != orig {
			t.Errorf("expected original error %v, got %v", orig, got)
		}
	})

	t.Run("pq error without position", func(t *testing.T) {
		pqErr := &pq.Error{
			Severity: "ERROR",
			Code:     "42P01",
			Message:  `relation "nonexistent" does not exist`,
		}
		got := FormatPostgresError(pqErr, "SELECT * FROM nonexistent;", 0)
		if got == nil {
			t.Fatal("expected error, got nil")
		}
		want := `pq: relation "nonexistent" does not exist (SQLSTATE 42P01)`
		if got.Error() != want {
			t.Errorf("got %q, want %q", got.Error(), want)
		}
	})

	t.Run("pq error with position and snippet", func(t *testing.T) {
		sqlText := "CREATE TABLE users (\n    id INT,\n    invalid syntax\n);"
		// "CREATE TABLE users (\n    id INT,\n    " is 21 + 12 + 4 = 37 runes.
		// "i" of "invalid" is rune 38.
		pqErr := &pq.Error{
			Severity: "ERROR",
			Code:     "42601",
			Message:  `syntax error at or near "invalid"`,
			Position: "38",
		}
		got := FormatPostgresError(pqErr, sqlText, 0)
		if got == nil {
			t.Fatal("expected error, got nil")
		}

		errMsg := got.Error()
		if !strings.Contains(errMsg, "migration error at line 3, column 5:") {
			t.Errorf("missing header in: %s", errMsg)
		}
		if !strings.Contains(errMsg, "  3 |     invalid syntax") {
			t.Errorf("missing code line in: %s", errMsg)
		}
		if !strings.Contains(errMsg, "    |     ^") {
			t.Errorf("missing caret in: %s", errMsg)
		}
		if !strings.Contains(errMsg, `pq: syntax error at or near "invalid" (SQLSTATE 42601)`) {
			t.Errorf("missing pq summary in: %s", errMsg)
		}
	})

	t.Run("pq error with prefix length adjustment", func(t *testing.T) {
		prefix := "SET search_path TO public; RESET client_min_messages;\n"
		prefixLen := len([]rune(prefix)) // 54 runes
		sqlText := "SELECT * FROM nonexistent_table;"

		pqErr := &pq.Error{
			Severity: "ERROR",
			Code:     "42P01",
			Message:  `relation "nonexistent_table" does not exist`,
			Position: "69", // 54 prefix + 15 ("SELECT * FROM n")
		}

		got := FormatPostgresError(pqErr, sqlText, prefixLen)
		if got == nil {
			t.Fatal("expected error, got nil")
		}

		errMsg := got.Error()
		if !strings.Contains(errMsg, "migration error at line 1, column 15:") {
			t.Errorf("expected line 1, column 15 in: %s", errMsg)
		}
		if !strings.Contains(errMsg, "  1 | SELECT * FROM nonexistent_table;") {
			t.Errorf("expected code line in: %s", errMsg)
		}
		if !strings.Contains(errMsg, "    |               ^") {
			t.Errorf("expected caret at col 15 in: %s", errMsg)
		}
	})

	t.Run("pq error with detail hint and where", func(t *testing.T) {
		pqErr := &pq.Error{
			Severity: "ERROR",
			Code:     "42703",
			Message:  `column "foo" does not exist`,
			Detail:   `Column "foo" was not found in table "bar".`,
			Hint:     `Perhaps you meant to reference the column "bar.baz".`,
			Where:    `PL/pgSQL function update_bar() line 4 at SQL statement`,
		}
		got := FormatPostgresError(pqErr, "SELECT foo FROM bar;", 0)
		if got == nil {
			t.Fatal("expected error, got nil")
		}

		errMsg := got.Error()
		if !strings.Contains(errMsg, `Detail: Column "foo" was not found in table "bar".`) {
			t.Errorf("missing Detail in: %s", errMsg)
		}
		if !strings.Contains(errMsg, `Hint: Perhaps you meant to reference the column "bar.baz".`) {
			t.Errorf("missing Hint in: %s", errMsg)
		}
		if !strings.Contains(errMsg, `Where: PL/pgSQL function update_bar() line 4 at SQL statement`) {
			t.Errorf("missing Where in: %s", errMsg)
		}
	})
}

func TestDiagnosePostgresError(t *testing.T) {
	t.Run("non-42501 error: no diagnostic appended", func(t *testing.T) {
		pqErr := &pq.Error{Code: "42P01", Message: `relation "x" does not exist`}
		got := DiagnosePostgresError(context.Background(), nil, pqErr, "SELECT * FROM x;", 0)
		if got == nil {
			t.Fatal("expected error, got nil")
		}
		if strings.Contains(got.Error(), "permission diagnostic") {
			t.Errorf("expected no permission diagnostic for a non-42501 error, got: %s", got.Error())
		}
	})

	t.Run("42501 error with nil db: formats without a diagnostic instead of panicking", func(t *testing.T) {
		pqErr := &pq.Error{Code: "42501", Message: "permission denied for schema x"}
		got := DiagnosePostgresError(context.Background(), nil, pqErr, "CREATE TABLE x (id INT);", 0)
		if got == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(got.Error(), "permission denied for schema x") {
			t.Errorf("expected the original message preserved, got: %s", got.Error())
		}
		if strings.Contains(got.Error(), "permission diagnostic") {
			t.Errorf("expected no permission diagnostic with a nil db, got: %s", got.Error())
		}
	})

	t.Run("42501 error with a live db: diagnostic appended after the formatted error", func(t *testing.T) {
		db := getMockDB(t)
		mockDriverInstance.mu.Lock()
		mockDriverInstance.handlers = map[string]func(args []driver.Value) (driver.Rows, error){
			"SELECT current_user": func(args []driver.Value) (driver.Rows, error) {
				return &mockRows{
					cols:   []string{"current_user", "session_user", "current_database", "current_schema"},
					values: [][]driver.Value{{"app_user", "app_user", "testdb", "public"}},
				}, nil
			},
			"nspowner": func(args []driver.Value) (driver.Rows, error) {
				return &mockRows{cols: []string{"coalesce"}, values: [][]driver.Value{{"postgres"}}}, nil
			},
			"has_schema_privilege": func(args []driver.Value) (driver.Rows, error) {
				return &mockRows{cols: []string{"has_usage", "has_create"}, values: [][]driver.Value{{false, false}}}, nil
			},
			"azure_pg_admin": func(args []driver.Value) (driver.Rows, error) {
				return &mockRows{cols: []string{"exists"}, values: [][]driver.Value{{false}}}, nil
			},
		}
		mockDriverInstance.mu.Unlock()

		pqErr := &pq.Error{Code: "42501", Message: "permission denied for schema public"}
		got := DiagnosePostgresError(context.Background(), db, pqErr, "CREATE TABLE x (id INT);", 0)
		if got == nil {
			t.Fatal("expected error, got nil")
		}
		errMsg := got.Error()
		if !strings.Contains(errMsg, "permission denied for schema public") {
			t.Errorf("expected the formatted error first, got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "[permission diagnostic (SQLSTATE 42501)]") {
			t.Errorf("expected the permission diagnostic appended, got: %s", errMsg)
		}
	})
}
