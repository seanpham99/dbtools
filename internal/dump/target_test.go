package dump

import (
	"strings"
	"testing"
)

// The dump tool runs inside the server's own container so its version always
// matches the server's. Everything that opts out of that has to be
// deliberate: no container, an explicit --use-host-tools, or an engine whose
// image does not ship the tool.
func TestDumpTarget(t *testing.T) {
	const hostURL = "postgres://u:p@127.0.0.1:54321/db?sslmode=disable"

	cases := []struct {
		name            string
		engine          string
		opts            Options
		wantInContainer bool
	}{
		{"container available", "postgres", Options{ExecIn: "scratch-1"}, true},
		{"mysql image ships mysqldump", "mysql", Options{ExecIn: "scratch-1"}, true},
		{"no container", "postgres", Options{}, false},
		{"host tools forced", "postgres", Options{ExecIn: "scratch-1", UseHostTools: true}, false},
		// mssql-scripter is a separate Python package, not in the image.
		{"mssql has no in-image tool", "mssql", Options{ExecIn: "scratch-1"}, false},
	}

	for _, c := range cases {
		connURL, inContainer, err := dumpTarget(c.engine, hostURL, c.opts)
		if err != nil {
			t.Errorf("%s: dumpTarget returned error: %v", c.name, err)
			continue
		}
		if inContainer != c.wantInContainer {
			t.Errorf("%s: inContainer = %v, want %v", c.name, inContainer, c.wantInContainer)
		}
		if !inContainer && connURL != hostURL {
			t.Errorf("%s: host path should use the host URL, got %q", c.name, connURL)
		}
		if inContainer && connURL == hostURL {
			t.Errorf("%s: container path must not use the host URL — the published port "+
				"does not exist inside the container", c.name)
		}
	}
}

// Inside the container the engine listens on its own fixed port, so the
// in-container URL must not carry the host's published port.
func TestDumpTarget_InContainerURLUsesTheEnginePort(t *testing.T) {
	connURL, inContainer, err := dumpTarget("postgres", "postgres://u:p@127.0.0.1:54321/db", Options{ExecIn: "c"})
	if err != nil {
		t.Fatalf("dumpTarget: %v", err)
	}
	if !inContainer {
		t.Fatal("expected the container path")
	}
	if !strings.Contains(connURL, ":5432/") {
		t.Errorf("in-container URL %q should use postgres's fixed port 5432", connURL)
	}
	if strings.Contains(connURL, "54321") {
		t.Errorf("in-container URL %q still carries the host's published port", connURL)
	}
}
