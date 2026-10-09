package container

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedLabels(t *testing.T) {
	labels := managedLabels("postgres", "/p/dbtools.toml")
	joined := ""
	for i, l := range labels {
		if i%2 == 1 {
			joined += l + "\n"
		}
	}
	for _, want := range []string{
		LabelManaged + "=true",
		LabelEngine + "=postgres",
		LabelConfigPath + "=/p/dbtools.toml",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("managedLabels missing %q in %q", want, joined)
		}
	}
}

func TestInsertLabelsBeforeImage(t *testing.T) {
	args := []string{"run", "-d", "--name", "x", "postgres:17-alpine"}
	got := insertLabels(args, []string{"--label", "k=v"})
	if len(got) != len(args)+2 {
		t.Fatalf("insertLabels len = %d, want %d", len(got), len(args)+2)
	}
	if got[len(got)-1] != "postgres:17-alpine" {
		t.Fatalf("image no longer last: %v", got)
	}
	if got[len(got)-3] != "--label" {
		t.Fatalf("label flag not before image: %v", got)
	}
	// original slice must not be mutated mid-way by aliasing
	if args[len(args)-1] != "postgres:17-alpine" {
		t.Fatalf("insertLabels mutated input: %v", args)
	}
}

func TestDeadOwner(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "dbtools.toml")
	if err := os.WriteFile(existing, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"unlabeled", map[string]string{}, false},
		{"managed but no configpath", map[string]string{LabelManaged: "true"}, false},
		{"managed, empty configpath", map[string]string{LabelManaged: "true", LabelConfigPath: ""}, false},
		{"managed, live config", map[string]string{LabelManaged: "true", LabelConfigPath: existing}, false},
		{"managed, dead config", map[string]string{LabelManaged: "true", LabelConfigPath: filepath.Join(t.TempDir(), "gone", "dbtools.toml")}, true},
		{"managed false, dead config", map[string]string{LabelManaged: "false", LabelConfigPath: "/nonexistent/dbtools.toml"}, false},
	}
	for _, c := range cases {
		if got := deadOwner(c.labels); got != c.want {
			t.Errorf("%s: deadOwner = %v, want %v", c.name, got, c.want)
		}
	}
}
