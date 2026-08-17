package cmd

import "testing"

func TestPushCmd_RequiresYesFlag(t *testing.T) {
	pushYes = false
	err := pushCmd.RunE(pushCmd, []string{"nonexistent-target"})
	if err == nil {
		t.Fatal("expected error when --yes is not set, got nil")
	}
}
