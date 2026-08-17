//go:build integration

package container

import "testing"

func TestStartStopIdempotent(t *testing.T) {
	url, err := Start()
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	if url != mustLocalURL(t) {
		t.Errorf("Start() = %q, want %q", url, mustLocalURL(t))
	}

	// Calling Start() again while already running must be a no-op, not an error.
	url2, err := Start()
	if err != nil {
		t.Fatalf("second Start() returned error: %v", err)
	}
	if url2 != url {
		t.Errorf("second Start() = %q, want %q", url2, url)
	}

	if err := Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	exists, _, err := inspect(mssqlSpec.name)
	if err != nil {
		t.Fatalf("inspect(mssqlSpec.name) after Stop() returned error: %v", err)
	}
	if exists {
		t.Error("expected container to be removed after Stop()")
	}
}
