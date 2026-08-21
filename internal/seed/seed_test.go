package seed

import (
	"github.com/seanpham99/dbtools/internal/engine/mssqlengine"
	"os"
	"testing"
)

func TestSplitBatches(t *testing.T) {
	sql := "INSERT INTO a VALUES (1);\nGO\nINSERT INTO b VALUES (2);\n"
	got := splitBatches(sql)
	if len(got) != 2 {
		t.Fatalf("splitBatches() = %v, want 2 batches", got)
	}
	if got[0] != "INSERT INTO a VALUES (1);" || got[1] != "INSERT INTO b VALUES (2);" {
		t.Fatalf("splitBatches() = %q, want two non-empty batches", got)
	}
}

func TestSplitBatches_NoGoSeparator(t *testing.T) {
	sql := "INSERT INTO a VALUES (1);"
	got := splitBatches(sql)
	if len(got) != 1 || got[0] != "INSERT INTO a VALUES (1);" {
		t.Fatalf("splitBatches() = %v, want single batch unchanged", got)
	}
}

func TestSplitBatches_IgnoresBlankBatches(t *testing.T) {
	sql := "GO\nGO\nINSERT INTO a VALUES (1);\nGO\n"
	got := splitBatches(sql)
	if len(got) != 1 {
		t.Fatalf("splitBatches() = %v, want 1 non-empty batch", got)
	}
}

func TestRun_NoSeedFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() returned error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir() returned error: %v", err)
	}

	if err := Run(mssqlengine.MSSQL{}, "not-a-real-connection-string"); err != nil {
		t.Fatalf("Run() with no seed.sql returned error: %v", err)
	}
}
