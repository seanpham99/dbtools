package mssqlengine

import "testing"

// TestExtractRenamedObjects_SpRenameIsNotParsed pins the documented gap:
// SQL Server renames through sp_rename, which takes string-literal
// arguments. dbtools does not guess at it, so a rebuild-in-place migration
// on MSSQL still requires --force on repair, and a rename migration's
// temporary name stays a drift report until that gap is closed.
func TestExtractRenamedObjects_SpRenameIsNotParsed(t *testing.T) {
	sql := "EXEC sp_rename 'widget_new', 'widget';"
	if got := (mssqlDDL{}).ExtractRenamedObjects(sql); len(got) != 0 {
		t.Errorf("ExtractRenamedObjects() = %+v, want 0 renames", got)
	}
}
