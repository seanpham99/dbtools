package cmd

import (
	"strings"
	"testing"
)

// TestVerifyCommand_MissingTargetStillPrintsUsage guards against RunE's
// exit-2 usage-silencing bleeding into cobra's own argument-count
// validation (Args: cobra.ExactArgs(1)) — that error must still show
// usage, since RunE's SilenceUsage toggle only applies to the documented
// ExitCodeError case and never runs for an argument-count failure.
func TestVerifyCommand_MissingTargetStillPrintsUsage(t *testing.T) {
	// See plan's identical test for why this reset is needed: verifyCmd
	// is a package-level singleton, and an argument-count error never
	// reaches RunE (where SilenceUsage otherwise gets reset per-invocation).
	origSilenceUsage := verifyCmd.SilenceUsage
	verifyCmd.SilenceUsage = false
	t.Cleanup(func() { verifyCmd.SilenceUsage = origSilenceUsage })

	// Cobra writes the error via PrintErrln (stderr) but the usage block
	// via Println (stdout) — capture both into one buffer so the
	// assertion doesn't depend on which stream cobra picks.
	var out strings.Builder
	rootCmd.SetErr(&out)
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"verify"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("verify with no target returned nil error, want an argument-count error")
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("verify with a missing required argument did not print usage: %s", out.String())
	}
}
