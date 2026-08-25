package projectid

import "testing"

func TestResolve_DeterministicForSamePath(t *testing.T) {
	id1, err := Resolve("/tmp/projectA/dbtools.toml", "")
	if err != nil {
		t.Fatalf("Resolve() returned error: %v", err)
	}
	id2, err := Resolve("/tmp/projectA/dbtools.toml", "")
	if err != nil {
		t.Fatalf("Resolve() returned error: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("Resolve() = %q then %q, want identical", id1, id2)
	}
	if len(id1) != 8 {
		t.Fatalf("Resolve() = %q, want 8 hex chars", id1)
	}
}

func TestResolve_DiffersForDifferentPaths(t *testing.T) {
	idA, err := Resolve("/tmp/projectA/dbtools.toml", "")
	if err != nil {
		t.Fatalf("Resolve() returned error: %v", err)
	}
	idB, err := Resolve("/tmp/projectB/dbtools.toml", "")
	if err != nil {
		t.Fatalf("Resolve() returned error: %v", err)
	}
	if idA == idB {
		t.Fatalf("Resolve() = %q for both paths, want different", idA)
	}
}

func TestResolve_ConfiguredNameWinsVerbatim(t *testing.T) {
	id, err := Resolve("/tmp/projectA/dbtools.toml", "myapp")
	if err != nil {
		t.Fatalf("Resolve() returned error: %v", err)
	}
	if id != "myapp" {
		t.Fatalf("Resolve() = %q, want %q", id, "myapp")
	}
}

func TestResolve_RejectsInvalidConfiguredName(t *testing.T) {
	_, err := Resolve("/tmp/projectA/dbtools.toml", "not valid!")
	if err == nil {
		t.Fatal("Resolve() with an invalid name returned nil error, want error")
	}
}

func TestResolve_RelativePathIsAbsolutedFirst(t *testing.T) {
	// Two different relative-path spellings of the same file must resolve
	// to the same id once made absolute.
	id1, err := Resolve("./dbtools.toml", "")
	if err != nil {
		t.Fatalf("Resolve() returned error: %v", err)
	}
	id2, err := Resolve("dbtools.toml", "")
	if err != nil {
		t.Fatalf("Resolve() returned error: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("Resolve(./dbtools.toml) = %q, Resolve(dbtools.toml) = %q, want identical", id1, id2)
	}
}
