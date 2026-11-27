package localdb

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateMajor(t *testing.T) {
	for _, ok := range []string{"15", "16", "17", "18"} {
		if err := validateMajor(ok); err != nil {
			t.Fatalf("validateMajor(%q) returned error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "abc", "17.6", "0", "-1", "seventeen"} {
		if err := validateMajor(bad); err == nil {
			t.Fatalf("validateMajor(%q) returned nil error, want a rejection", bad)
		}
	}
}

func TestImagePostgresMajor(t *testing.T) {
	cases := map[string]string{
		"postgres:17-alpine": "17",
		"postgres:18-alpine": "18",
		"postgres:18":        "18",
		"mysql:8":            "",
		"postgres":           "",
	}
	for image, want := range cases {
		if got := imagePostgresMajor(image); got != want {
			t.Fatalf("imagePostgresMajor(%q) = %q, want %q", image, got, want)
		}
	}
}

// A project that pins nothing gets the image the local database has always
// used, so an existing data volume keeps working.
func TestDockerMajor_DefaultsToTheHistoricalImage(t *testing.T) {
	major, err := dockerProvider{engineName: "postgres"}.Major("")
	if err != nil {
		t.Fatalf("Major() returned error: %v", err)
	}
	if major != "17" {
		t.Fatalf("Major() = %q, want %q", major, "17")
	}
}

func TestDockerMajor_HonorsAPin(t *testing.T) {
	major, err := dockerProvider{engineName: "postgres"}.Major("16")
	if err != nil {
		t.Fatalf("Major() returned error: %v", err)
	}
	if major != "16" {
		t.Fatalf("Major() = %q, want %q", major, "16")
	}
}

func TestDockerMajor_RejectsGarbage(t *testing.T) {
	if _, err := (dockerProvider{engineName: "postgres"}).Major("abc"); err == nil {
		t.Fatal("Major(\"abc\") returned nil error, want a rejection")
	}
}

func TestDockerMajor_RejectsAPinForANonPostgresEngine(t *testing.T) {
	if _, err := (dockerProvider{engineName: "mssql"}).Major("17"); err == nil {
		t.Fatal("Major() with a postgres pin on an mssql local target returned nil error, want a rejection")
	}
}

// fakePgXVersion puts a stub pgx on PATH that reports the PostgreSQL major
// it serves, which is all the pgx provider's major check needs.
func fakePgXVersion(t *testing.T, compat string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the pgx stub is a POSIX shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "pgx")
	body := "#!/bin/sh\nprintf '{\"postgres_compat\":\"" + compat + "\"}\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPgXMajor_UsesTheRuntimeMajorWhenUnpinned(t *testing.T) {
	fakePgXVersion(t, "18.6")
	major, err := pgxProvider{engineName: "postgres"}.Major("")
	if err != nil {
		t.Fatalf("Major() returned error: %v", err)
	}
	if major != "18" {
		t.Fatalf("Major() = %q, want %q", major, "18")
	}
}

func TestPgXMajor_AcceptsAMatchingPin(t *testing.T) {
	fakePgXVersion(t, "18.6")
	if _, err := (pgxProvider{engineName: "postgres"}).Major("18"); err != nil {
		t.Fatalf("Major(\"18\") returned error: %v", err)
	}
}

// The pgx runtime serves one major; a pin of another must fail here rather
// than let a cluster be created on the runtime's major while the
// configuration says something else.
func TestPgXMajor_RefusesAPinTheRuntimeCannotServe(t *testing.T) {
	fakePgXVersion(t, "18.6")
	_, err := pgxProvider{engineName: "postgres"}.Major("17")
	if err == nil {
		t.Fatal("Major(\"17\") returned nil error, want the pgx provider to refuse it")
	}
	if !strings.Contains(err.Error(), "18") || !strings.Contains(err.Error(), "17") {
		t.Fatalf("error %q does not name both the requested and the available major", err)
	}
}
