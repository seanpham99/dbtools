package cmd

import (
	"encoding/json"
	"fmt"
)

// emitJobSummary prints one final JSON-lines record to stdout when --json
// is set, marking that this process reached the end of its own control
// flow and is now exiting deliberately — as opposed to being killed or
// crashing mid-write, which never reaches this line at all. Job
// platforms scraping container logs (Azure Container Apps, ECS,
// Kubernetes Jobs) use this to distinguish "the job finished and
// reported" from "the job died mid-write" — see
// skills/using-dbtools/private-network-jobs.md.
//
// Call as `defer emitJobSummary(&err)` from a function with a named
// `err error` return value, so it fires on every return path: success,
// a documented non-zero exit (ExitCodeError), or a genuine runtime
// error alike. A killed process skips it entirely, which is the point —
// but a Go panic does not immediately kill the process, it unwinds
// through deferred calls first, and `err` is still its nil zero value
// mid-unwind. Without the recover() below, a panic would print a false
// "ok":true record moments before the process actually crashes — exactly
// the signal this function exists to prevent. recover()ing here, then
// re-panicking, lets the crash still propagate to the caller (so exit
// status and any top-level panic log are unaffected) while suppressing
// the misleading summary line.
func emitJobSummary(err *error) {
	if r := recover(); r != nil {
		panic(r)
	}
	if !jsonOutput {
		return
	}
	summary := struct {
		Event string `json:"event"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}{Event: "job_complete", OK: *err == nil}
	if *err != nil {
		summary.Error = (*err).Error()
	}
	b, marshalErr := json.Marshal(summary)
	if marshalErr != nil {
		return
	}
	fmt.Println(string(b))
}
