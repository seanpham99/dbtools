package localenv

import (
	"os"
	"testing"
)

func chdirTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir() returned error: %v", err)
	}
}

func TestWriteAndLoad(t *testing.T) {
	chdirTemp(t)

	if err := Write(map[string]string{"DBTOOLS_LOCAL_URL": "mssql://sa:pw@localhost:14330?database=dbtools_local"}); err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got["DBTOOLS_LOCAL_URL"] != "mssql://sa:pw@localhost:14330?database=dbtools_local" {
		t.Fatalf("Load()[DBTOOLS_LOCAL_URL] = %q, want %q", got["DBTOOLS_LOCAL_URL"], "mssql://sa:pw@localhost:14330?database=dbtools_local")
	}
}

func TestLoadMissingFile(t *testing.T) {
	chdirTemp(t)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("Load() = %#v, want nil", got)
	}
}

func TestLoadIgnoresBlankAndCommentLines(t *testing.T) {
	chdirTemp(t)

	if err := os.MkdirAll(Dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	if err := os.WriteFile(Path(), []byte("# comment\n\nDBTOOLS_LOCAL_URL=mssql://x\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(got) != 1 || got["DBTOOLS_LOCAL_URL"] != "mssql://x" {
		t.Fatalf("Load() = %#v, want one parsed env var", got)
	}
}

func TestRemove(t *testing.T) {
	chdirTemp(t)

	if err := Write(map[string]string{"X": "1"}); err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}
	if err := Remove(); err != nil {
		t.Fatalf("Remove() returned error: %v", err)
	}
	if _, err := os.Stat(Dir); !os.IsNotExist(err) {
		t.Fatalf("os.Stat(%q) = %v, want os.IsNotExist", Dir, err)
	}
}

func TestRemoveMissingDirIsNotAnError(t *testing.T) {
	chdirTemp(t)

	if err := Remove(); err != nil {
		t.Fatalf("Remove() returned error: %v", err)
	}
}
