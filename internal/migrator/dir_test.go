package migrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "20260101000000_create_users.up.sql"), []byte("CREATE TABLE users (id INT);"), 0o644)
	os.WriteFile(filepath.Join(dir, "20260101000000_create_users.down.sql"), []byte("DROP TABLE users;"), 0o644)
	os.WriteFile(filepath.Join(dir, "20260102000000_add_orders.up.sql"), []byte("CREATE TABLE orders (id INT);"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a migration"), 0o644)

	d, err := ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatalf("ReadDir() returned error: %v", err)
	}

	files := d.List()
	if len(files) != 2 {
		t.Fatalf("d.List() returned %d files, want 2 up migrations", len(files))
	}
	if files[0].Version != 20260101000000 || files[1].Version != 20260102000000 {
		t.Fatalf("unexpected versions: %+v", files)
	}

	f, err := d.Find(20260102000000)
	if err != nil {
		t.Fatalf("d.Find() returned error: %v", err)
	}
	if f.Filename != "20260102000000_add_orders.up.sql" {
		t.Errorf("d.Find() = %q, want 20260102000000_add_orders.up.sql", f.Filename)
	}

	_, err = d.Find(99999999999999)
	if err == nil {
		t.Fatal("d.Find(nonexistent) expected error, got nil")
	}

	pending := d.PendingAfter(20260101000000, true)
	if len(pending) != 1 || pending[0].Version != 20260102000000 {
		t.Errorf("d.PendingAfter() = %+v, want [20260102000000]", pending)
	}

	pendingAll := d.PendingAfter(0, false)
	if len(pendingAll) != 2 {
		t.Errorf("d.PendingAfter(0, false) = %+v, want 2 files", pendingAll)
	}

	hash, err := d.ContentHash(20260101000000)
	if err != nil {
		t.Fatalf("d.ContentHash() error: %v", err)
	}
	if len(hash) != 64 {
		t.Errorf("d.ContentHash() = %q, want 64-char hex string", hash)
	}

	// Down file checks
	downFiles := d.ListDown()
	if len(downFiles) != 1 {
		t.Fatalf("d.ListDown() returned %d files, want 1", len(downFiles))
	}
	if downFiles[0].Version != 20260101000000 {
		t.Errorf("down file version = %d, want 20260101000000", downFiles[0].Version)
	}

	downFile, err := d.FindDown(20260101000000)
	if err != nil {
		t.Fatalf("d.FindDown() returned error: %v", err)
	}
	if downFile.Filename != "20260101000000_create_users.down.sql" {
		t.Errorf("d.FindDown() = %q, want 20260101000000_create_users.down.sql", downFile.Filename)
	}

	downHash, err := d.DownContentHash(20260101000000)
	if err != nil {
		t.Fatalf("d.DownContentHash() error: %v", err)
	}
	if len(downHash) != 64 {
		t.Errorf("d.DownContentHash() = %q, want 64-char hex string", downHash)
	}
}

func TestDir_DownPlan(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "20260101000000_create_users.up.sql"), []byte("CREATE TABLE users (id INT);"), 0o644)
	os.WriteFile(filepath.Join(dir, "20260101000000_create_users.down.sql"), []byte("DROP TABLE users;"), 0o644)
	os.WriteFile(filepath.Join(dir, "20260102000000_add_orders.up.sql"), []byte("CREATE TABLE orders (id INT);"), 0o644)
	os.WriteFile(filepath.Join(dir, "20260102000000_add_orders.down.sql"), []byte("DROP TABLE orders;"), 0o644)

	d, err := ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}

	applied := []uint64{20260101000000, 20260102000000}
	plan1, err := d.DownPlan(applied, 1)
	if err != nil {
		t.Fatalf("DownPlan(1) error: %v", err)
	}
	if len(plan1) != 1 || plan1[0].Version != 20260102000000 {
		t.Errorf("DownPlan(1) = %+v, want [20260102000000]", plan1)
	}

	planAll, err := d.DownPlan(applied, 0)
	if err != nil {
		t.Fatalf("DownPlan(0) error: %v", err)
	}
	if len(planAll) != 2 || planAll[0].Version != 20260102000000 || planAll[1].Version != 20260101000000 {
		t.Errorf("DownPlan(0) = %+v, want [20260102000000, 20260101000000]", planAll)
	}

	// Missing down file error
	os.Remove(filepath.Join(dir, "20260102000000_add_orders.down.sql"))
	d2, _ := ReadDir(dir, ".up.sql")
	_, err = d2.DownPlan(applied, 1)
	if err == nil {
		t.Fatal("expected error when down file missing, got nil")
	}
}

func TestDir_NextVersion(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	clockVer := uint64(20260115100000)

	d, err := ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}
	ver, err := d.NextVersion(now)
	if err != nil {
		t.Fatal(err)
	}
	if ver != clockVer {
		t.Errorf("NextVersion() empty dir = %d, want clock %d", ver, clockVer)
	}

	// Future-dated migration present
	os.WriteFile(filepath.Join(dir, "20260120000000_future.up.sql"), []byte("SELECT 1;"), 0o644)
	d, err = ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}
	ver, err = d.NextVersion(now)
	if err != nil {
		t.Fatal(err)
	}
	if ver != 20260120000001 {
		t.Errorf("NextVersion() with future = %d, want 20260120000001", ver)
	}
}

func TestReadDir_CustomUpSuffix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.sql"), []byte("-- sql"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A real .up.sql file must NOT also match when a custom suffix is set.
	if err := os.WriteFile(filepath.Join(dir, "2_create_gadgets.up.sql"), []byte("-- sql"), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := ReadDir(dir, ".sql")
	if err != nil {
		t.Fatalf("ReadDir() returned error: %v", err)
	}
	versions := d.ListVersions()
	if len(versions) != 1 || versions[0] != 1 {
		t.Errorf("ListVersions() = %v, want [1]", versions)
	}
}

