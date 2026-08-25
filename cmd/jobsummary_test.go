package cmd

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() returned error: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer returned error: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading pipe returned error: %v", err)
	}
	return string(out)
}

func TestEmitJobSummary_NoOpWithoutJSON(t *testing.T) {
	origJSON := jsonOutput
	jsonOutput = false
	t.Cleanup(func() { jsonOutput = origJSON })

	var err error
	out := captureStdout(t, func() { emitJobSummary(&err) })
	if out != "" {
		t.Fatalf("emitJobSummary() without --json printed %q, want nothing", out)
	}
}

func TestEmitJobSummary_SuccessRecordWithJSON(t *testing.T) {
	origJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = origJSON })

	var err error
	out := captureStdout(t, func() { emitJobSummary(&err) })
	if !strings.Contains(out, `"event":"job_complete"`) {
		t.Fatalf("emitJobSummary() output = %q, want it to contain the job_complete event", out)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("emitJobSummary() output = %q, want ok:true for a nil error", out)
	}
	if strings.Contains(out, `"error"`) {
		t.Fatalf("emitJobSummary() output = %q, want no error field for a nil error", out)
	}
}

func TestEmitJobSummary_ErrorRecordWithJSON(t *testing.T) {
	origJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = origJSON })

	err := errors.New("connection refused")
	out := captureStdout(t, func() { emitJobSummary(&err) })
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("emitJobSummary() output = %q, want ok:false for a non-nil error", out)
	}
	if !strings.Contains(out, `"error":"connection refused"`) {
		t.Fatalf("emitJobSummary() output = %q, want the error message included", out)
	}
}

// TestUpCommand_EmitsJobSummaryOnRefusal proves the defer fires even on
// an early-return error path (not just the success path) — up refuses a
// non-local target before doing anything else.
func TestUpCommand_EmitsJobSummaryOnRefusal(t *testing.T) {
	origTarget := upTarget
	origJSON := jsonOutput
	t.Cleanup(func() {
		upTarget = origTarget
		jsonOutput = origJSON
	})

	rootCmd.SetArgs([]string{"up", "--target", "prod", "--json"})
	out := captureStdout(t, func() {
		_ = rootCmd.Execute()
	})
	if !strings.Contains(out, `"event":"job_complete"`) {
		t.Fatalf("up --target prod --json output = %q, want a job_complete summary even on refusal", out)
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("up --target prod --json output = %q, want ok:false since up refuses non-local targets", out)
	}
}
