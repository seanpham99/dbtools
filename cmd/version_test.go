package cmd

import (
	"testing"
)

func TestVersionCommand(t *testing.T) {
	if versionCmd == nil {
		t.Fatal("expected versionCmd to be defined")
	}
	if versionCmd.Use != "version" {
		t.Errorf("versionCmd.Use = %q, want %q", versionCmd.Use, "version")
	}
}
