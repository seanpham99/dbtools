package scratchdb

import (
	"errors"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

// The selection table is the version-parity rule in one place: the pgx
// runtime serves one major, so it may only provision a scratch database for
// a target on that major. Everything else keeps the container, whose image
// can be pinned to the target's major.
func TestChooseWith(t *testing.T) {
	cases := []struct {
		name       string
		engineName string
		series     string
		pgxMajor   string
		pgxErr     error
		want       string
		wantNote   string
	}{
		{name: "postgres at the pgx major", engineName: "postgres", series: "18", pgxMajor: "18", want: "pgx"},
		{name: "postgres on another major", engineName: "postgres", series: "17", pgxMajor: "18", want: "docker", wantNote: "the pgx runtime serves PostgreSQL 18 only"},
		{name: "postgres with an undetectable version", engineName: "postgres", series: "", pgxMajor: "18", want: "docker", wantNote: "could not be determined"},
		{name: "mysql is never pgx", engineName: "mysql", series: "8.0", pgxMajor: "18", want: "docker", wantNote: "serves the postgres engine only"},
		{name: "mssql is never pgx", engineName: "mssql", series: "16", pgxMajor: "18", want: "docker", wantNote: "serves the postgres engine only"},
		{name: "sqlite is never pgx", engineName: "sqlite", series: "", pgxMajor: "18", want: "docker", wantNote: "serves the postgres engine only"},
		{name: "unreadable runtime version", engineName: "postgres", series: "18", pgxMajor: "", pgxErr: errors.New("boom"), want: "docker", wantNote: "could not be read"},
		{name: "runtime version unknown", engineName: "postgres", series: "18", pgxMajor: "", want: "docker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, note := chooseWith(tc.engineName, tc.series, tc.pgxMajor, tc.pgxErr)
			if provider.Name() != tc.want {
				t.Fatalf("chooseWith(%q, %q, %q) = %q, want %q", tc.engineName, tc.series, tc.pgxMajor, provider.Name(), tc.want)
			}
			if tc.wantNote == "" {
				if note != "" {
					t.Fatalf("note = %q, want empty", note)
				}
				return
			}
			if !strings.Contains(note, tc.wantNote) {
				t.Fatalf("note = %q, want it to contain %q", note, tc.wantNote)
			}
		})
	}
}

// pgx is opt-in through [local] provider: a project that never selected it
// keeps the container scratch even on a machine that has the binary.
func TestChooseProvider_KeepsTheContainerWhenTheProjectDidNotOptIn(t *testing.T) {
	provider, note := ChooseProvider("postgres", "18", false)
	if provider.Name() != "docker" {
		t.Fatalf("ChooseProvider() = %q, want docker without an opt-in", provider.Name())
	}
	if note != "" {
		t.Fatalf("note = %q, want empty: not opting in is not a fallback", note)
	}
}

// A caller-supplied scratch database is the caller's choice, and it is
// already parity-checked by diff; provisioning must not replace it.
func TestProvisionSeries_KeepsCallerSuppliedDatabase(t *testing.T) {
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	scratch, note, err := ProvisionSeries(eng, "sqlite:///caller.db", "", false)
	if err != nil {
		t.Fatalf("ProvisionSeries() returned error: %v", err)
	}
	if scratch.URL != "sqlite:///caller.db" {
		t.Fatalf("URL = %q, want the caller's database unchanged", scratch.URL)
	}
	if scratch.Cleanup != nil {
		t.Fatal("Cleanup should be nil for a caller-supplied database")
	}
	if note != "" {
		t.Fatalf("note = %q, want empty", note)
	}
}
