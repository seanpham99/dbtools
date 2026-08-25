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
// error alike. Only a hard crash (panic that unwinds past the deferred
// call, or a killed process) skips it — which is the point.
func emitJobSummary(err *error) {
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
