package container

import (
	"strings"
	"testing"
)

func TestStartScratch_UnsupportedEngine(t *testing.T) {
	_, _, err := StartScratch("sqlite")
	if err == nil {
		t.Fatal("StartScratch(\"sqlite\") should error — sqlite has no container template, callers must use a tempfile instead")
	}
}

func TestScratchNameFor_NeverCollidesWithDevContainerName(t *testing.T) {
	scratch := scratchNameFor("postgres")
	dev := containerNameFor("postgres", "someproject")
	if scratch == dev {
		t.Fatal("scratch and dev container names must never collide")
	}
	if strings.HasPrefix(scratch, "dbtools-postgres-") && !strings.Contains(scratch, "scratch") {
		t.Errorf("scratchNameFor(%q) = %q, want it clearly distinguishable from containerNameFor's naming scheme", "postgres", scratch)
	}
}
