package container

import (
	"strings"
	"testing"
)

func TestPostgresImageFor(t *testing.T) {
	for _, major := range []string{"15", "16", "17", "18"} {
		if got, want := PostgresImageFor(major), "postgres:"+major+"-alpine"; got != want {
			t.Fatalf("PostgresImageFor(%q) = %q, want %q", major, got, want)
		}
	}
}

// PostgreSQL 18 moved PGDATA under a major-versioned path, and its image
// refuses to initialise when a volume is mounted at the pre-18 path. The
// mount has to follow the major.
func TestPostgresDataDirFor(t *testing.T) {
	cases := map[string]string{
		"15": "/var/lib/postgresql/data",
		"16": "/var/lib/postgresql/data",
		"17": "/var/lib/postgresql/data",
		"18": "/var/lib/postgresql",
	}
	for major, want := range cases {
		if got := PostgresDataDirFor(major); got != want {
			t.Fatalf("PostgresDataDirFor(%q) = %q, want %q", major, got, want)
		}
	}
}

// The default spec must stay the image and layout every existing local
// database was created with, so this change cannot move an unpinned
// project's data.
func TestPostgresSpecDefaultMatchesTheHistoricalLayout(t *testing.T) {
	s, err := specFor("postgres")
	if err != nil {
		t.Fatal(err)
	}
	if s.image != "postgres:17-alpine" {
		t.Fatalf("postgres default image = %q, want %q", s.image, "postgres:17-alpine")
	}
	if s.dataDir != "/var/lib/postgresql/data" {
		t.Fatalf("postgres default data dir = %q, want %q", s.dataDir, "/var/lib/postgresql/data")
	}
}

func TestWithPostgresMajor(t *testing.T) {
	s, err := specFor("postgres")
	if err != nil {
		t.Fatal(err)
	}
	pinned := withPostgresMajor(s, "16")
	if pinned.image != "postgres:16-alpine" {
		t.Fatalf("image = %q, want postgres:16-alpine", pinned.image)
	}
	if pinned.dataDir != "/var/lib/postgresql/data" {
		t.Fatalf("data dir = %q, want the pre-18 path", pinned.dataDir)
	}
	pinned18 := withPostgresMajor(s, "18")
	if pinned18.dataDir != "/var/lib/postgresql" {
		t.Fatalf("data dir = %q, want the PostgreSQL 18 path", pinned18.dataDir)
	}
	if unpinned := withPostgresMajor(s, ""); unpinned.image != s.image || unpinned.dataDir != s.dataDir {
		t.Fatalf("withPostgresMajor(\"\") changed the template: %+v", unpinned)
	}
	mssql, err := specFor("mssql")
	if err != nil {
		t.Fatal(err)
	}
	if got := withPostgresMajor(mssql, "16"); got.image != mssql.image {
		t.Fatalf("withPostgresMajor applied a postgres major to mssql: image = %q", got.image)
	}
}

func TestLocalImageMismatchNamesBothImagesAndTheRemedy(t *testing.T) {
	s, err := specFor("postgres")
	if err != nil {
		t.Fatal(err)
	}
	err = localImageMismatch(withPostgresMajor(s, "18"), "postgres:17-alpine")
	for _, want := range []string{"postgres:17-alpine", "postgres:18-alpine", "dbtools stop --no-backup"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}
