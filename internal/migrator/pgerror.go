package migrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// TranslatePosition converts a 1-based character (rune) offset into 1-based line
// and column coordinates, returning the line text (without trailing newline) and
// a caret string pointing to the column.
func TranslatePosition(sqlText string, charOffset int) (line int, col int, lineText string, caret string) {
	runes := []rune(sqlText)
	if charOffset < 1 || charOffset > len(runes) {
		return 0, 0, "", ""
	}

	curLine := 1
	curCol := 1
	lineStartIdx := 0

	for i := 0; i < charOffset-1; i++ {
		if runes[i] == '\n' {
			curLine++
			curCol = 1
			lineStartIdx = i + 1
		} else {
			curCol++
		}
	}

	lineEndIdx := lineStartIdx
	for lineEndIdx < len(runes) && runes[lineEndIdx] != '\n' {
		lineEndIdx++
	}

	lineRunes := runes[lineStartIdx:lineEndIdx]
	if len(lineRunes) > 0 && lineRunes[len(lineRunes)-1] == '\r' {
		lineRunes = lineRunes[:len(lineRunes)-1]
	}

	lineText = string(lineRunes)
	caret = strings.Repeat(" ", curCol-1) + "^"
	return curLine, curCol, lineText, caret
}

// FormatPostgresError formats a PostgreSQL error, translating character-offset
// position information into line and column numbers and rendering a code snippet
// with a caret pointer. If prefixLen is > 0, the position offset is adjusted
// to account for any preamble prepended before sqlText.
func FormatPostgresError(err error, sqlText string, prefixLen int) error {
	if err == nil {
		return nil
	}

	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr == nil {
		return err
	}

	var line, col int
	var lineText, caret string

	if pqErr.Position != "" {
		if pos, convErr := strconv.Atoi(pqErr.Position); convErr == nil {
			adjustedOffset := pos - prefixLen
			if adjustedOffset > 0 {
				line, col, lineText, caret = TranslatePosition(sqlText, adjustedOffset)
			}
		}
	}

	var b strings.Builder
	if line > 0 {
		fmt.Fprintf(&b, "migration error at line %d, column %d:\n", line, col)
		lineNumStr := strconv.Itoa(line)
		fmt.Fprintf(&b, "  %s | %s\n", lineNumStr, lineText)
		fmt.Fprintf(&b, "  %s | %s\n", strings.Repeat(" ", len(lineNumStr)), caret)
	}

	if pqErr.Code != "" {
		fmt.Fprintf(&b, "pq: %s (SQLSTATE %s)", pqErr.Message, pqErr.Code)
	} else {
		fmt.Fprintf(&b, "pq: %s", pqErr.Message)
	}

	if pqErr.Detail != "" {
		fmt.Fprintf(&b, "\nDetail: %s", pqErr.Detail)
	}
	if pqErr.Hint != "" {
		fmt.Fprintf(&b, "\nHint: %s", pqErr.Hint)
	}
	if pqErr.Where != "" {
		fmt.Fprintf(&b, "\nWhere: %s", pqErr.Where)
	}

	return errors.New(b.String())
}

// DiagnosePostgresError is the single entry point callers use to turn a
// raw Postgres error into the fullest diagnostic available: line/column
// context via FormatPostgresError always, plus — for SQLSTATE 42501
// (insufficient_privilege), the one error class where a live connection
// can add actionable context — a permission report from
// RunPermissionDiagnostic. Callers that just need line/column formatting
// with no live connection to probe further should call
// FormatPostgresError directly instead; this exists so a caller like
// pgResetDriver.Run doesn't need to know pq.Error internals or SQLSTATE
// codes itself, only that a migration failed.
func DiagnosePostgresError(ctx context.Context, db *sql.DB, err error, sqlText string, prefixLen int) error {
	formatted := FormatPostgresError(err, sqlText, prefixLen)

	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr == nil || pqErr.Code != "42501" {
		return formatted
	}
	diag := RunPermissionDiagnostic(ctx, db, pqErr)
	if diag == "" {
		return formatted
	}
	return fmt.Errorf("%w\n\n%s", formatted, diag)
}
