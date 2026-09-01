package container

import (
	"strings"
	"testing"
)

// Every docker -p publish must be bound to 127.0.0.1: a bare host:container
// publish makes Docker bind 0.0.0.0, exposing the tool-owned (public,
// well-known-password) database on every host interface.
func TestRunArgsPublishOnlyOnLoopback(t *testing.T) {
	for engine, s := range specs {
		s.name = containerNameFor(engine, "proj")
		s.hostPort = "12345"
		for _, label := range []string{"runArgs", "scratchRunArgs"} {
			argsFn := s.runArgs
			if label == "scratchRunArgs" {
				argsFn = s.scratchRunArgs
			}
			if argsFn == nil {
				continue
			}
			args := argsFn(s)
			for i, arg := range args {
				if arg != "-p" {
					continue
				}
				publish := args[i+1]
				if !strings.HasPrefix(publish, "127.0.0.1:") {
					t.Errorf("%s %s publishes %q — must be bound to 127.0.0.1", engine, label, publish)
				}
			}
		}
	}
}
