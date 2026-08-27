package diff

import (
	"strings"
	"testing"
)

// The remediation has to name the version the user needs — the target's —
// not the one they supplied. The first draft of this message used the wrong
// positional argument and told the user to supply the version they already
// had, which is the kind of slip a live two-version test would catch far too
// late.
func TestMismatchError_RemediationNamesTheTargetVersion(t *testing.T) {
	err := newMismatchError("postgres", "15", "18")
	msg := err.Error()

	for _, want := range []string{
		"--against database is postgres 15",
		"the target is postgres 18",
		"Supply a scratch database on postgres 18",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Supply a scratch database on postgres 15") {
		t.Errorf("remediation names the supplied version instead of the target's:\n%s", msg)
	}
}

// Says why the result would be wrong, not merely noisy — the reason this is
// a refusal rather than a warning.
func TestMismatchError_ExplainsWhyItRefuses(t *testing.T) {
	msg := newMismatchError("mysql", "8.0", "8.4").Error()
	if !strings.Contains(msg, "wrong rather than merely noisy") {
		t.Errorf("message does not explain the refusal:\n%s", msg)
	}
}
