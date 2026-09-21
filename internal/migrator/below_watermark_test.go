package migrator

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDir_BelowWatermark pins the classification behind issue #106: only
// files below the watermark that the ledger does not list as applied are
// "ignored" (unreachable by `up`). An applied file below the watermark is
// ordinary history, and a file above the watermark is simply pending.
func TestDir_BelowWatermark(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"20260101000000_a.up.sql",
		"20260102000000_b.up.sql",
		"20260103000000_c.up.sql",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	d, err := ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatalf("ReadDir() returned error: %v", err)
	}

	cases := []struct {
		name          string
		current       uint64
		hasVersion    bool
		applied       []uint64
		wantFilenames []string
	}{
		{
			name:          "all files applied, nothing ignored",
			current:       20260103000000,
			hasVersion:    true,
			applied:       []uint64{20260101000000, 20260102000000, 20260103000000},
			wantFilenames: nil,
		},
		{
			name:          "back-dated file below the watermark is ignored",
			current:       20260103000000,
			hasVersion:    true,
			applied:       []uint64{20260101000000, 20260103000000},
			wantFilenames: []string{"20260102000000_b.up.sql"},
		},
		{
			name:          "file exactly at the watermark is not ignored",
			current:       20260102000000,
			hasVersion:    true,
			applied:       []uint64{20260101000000, 20260102000000},
			wantFilenames: nil,
		},
		{
			name:          "files above the watermark are pending, not ignored",
			current:       20260101000000,
			hasVersion:    true,
			applied:       []uint64{20260101000000},
			wantFilenames: nil,
		},
		{
			name:          "no watermark (nothing ever applied) ignores nothing",
			current:       0,
			hasVersion:    false,
			applied:       nil,
			wantFilenames: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := d.BelowWatermark(tc.current, tc.hasVersion, tc.applied)
			if len(got) != len(tc.wantFilenames) {
				t.Fatalf("BelowWatermark() = %v, want filenames %v", got, tc.wantFilenames)
			}
			for i, f := range got {
				if f.Filename != tc.wantFilenames[i] {
					t.Errorf("BelowWatermark()[%d].Filename = %q, want %q", i, f.Filename, tc.wantFilenames[i])
				}
				if wantPath := filepath.Join(dir, tc.wantFilenames[i]); f.Path != wantPath {
					t.Errorf("BelowWatermark()[%d].Path = %q, want %q", i, f.Path, wantPath)
				}
			}
		})
	}
}
