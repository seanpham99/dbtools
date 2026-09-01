package localenv

import (
	"os"
	"path/filepath"
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

func TestWrite_CreatesSelfIgnoringGitignore(t *testing.T) {
	chdirTemp(t)

	if err := Write(map[string]string{"DBTOOLS_LOCAL_URL": "postgres://postgres:pw@localhost:5432/dbtools_local"}); err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(Dir, ".gitignore"))
	if err != nil {
		t.Fatalf("reading %s/.gitignore returned error: %v (Write must create it so local.env's generated password is never committed)", Dir, err)
	}
	if string(got) != "*\n" {
		t.Fatalf("%s/.gitignore contents = %q, want %q", Dir, got, "*\n")
	}
}

func TestWrite_NeverOverwritesExistingGitignore(t *testing.T) {
	chdirTemp(t)

	if err := os.MkdirAll(Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := "# custom, do not touch\n"
	if err := os.WriteFile(filepath.Join(Dir, ".gitignore"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(map[string]string{"DBTOOLS_LOCAL_URL": "postgres://postgres:pw@localhost:5432/dbtools_local"}); err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(Dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != custom {
		t.Fatalf("%s/.gitignore contents = %q, want unchanged %q", Dir, got, custom)
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

func TestWrite_RejectsNewlineInValue(t *testing.T) {
	chdirTemp(t)

	err := Write(map[string]string{"DBTOOLS_LOCAL_URL": "postgres://x\nINJECTED=1"})
	if err == nil {
		t.Fatal("Write() accepted a value containing a newline")
	}
	if _, statErr := os.Stat(Path()); !os.IsNotExist(statErr) {
		t.Fatalf("Write() left a file behind despite rejecting the value: %v", statErr)
	}
}

func TestWrite_RejectsNewlineInKey(t *testing.T) {
	chdirTemp(t)

	// Keys come from config (target url_env): a key with an embedded
	// newline would inject additional assignments into local.env.
	err := Write(map[string]string{"X\nPATH=/attacker\nY": "1"})
	if err == nil {
		t.Fatal("Write() accepted a key containing a newline")
	}
	if _, statErr := os.Stat(Path()); !os.IsNotExist(statErr) {
		t.Fatalf("Write() left a file behind despite rejecting the key: %v", statErr)
	}
}

func TestWrite_RejectsEqualsInKey(t *testing.T) {
	chdirTemp(t)

	if err := Write(map[string]string{"A=B": "1"}); err == nil {
		t.Fatal("Write() accepted a key containing '='")
	}
}

func TestWrite_RefusesSymlinkedFile(t *testing.T) {
	chdirTemp(t)

	if err := os.MkdirAll(Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, Path()); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := Write(map[string]string{"DBTOOLS_LOCAL_URL": "postgres://x"}); err == nil {
		t.Fatal("Write() followed a symlinked local.env")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "original" {
		t.Fatalf("symlink target was modified: contents %q, err %v", got, err)
	}
}

func TestWrite_Enforces0600OnPreexistingFile(t *testing.T) {
	chdirTemp(t)

	if err := os.MkdirAll(Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(map[string]string{"DBTOOLS_LOCAL_URL": "postgres://x"}); err != nil {
		t.Fatalf("Write() returned error: %v", err)
	}
	info, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("local.env permissions = %o, want 600", perm)
	}
}
