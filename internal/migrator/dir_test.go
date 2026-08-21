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

	d, err := ReadDir(dir)
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
}

func TestDir_NextVersion(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	clockVer := uint64(20260115100000)

	d, err := ReadDir(dir)
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
	d, err = ReadDir(dir)
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
