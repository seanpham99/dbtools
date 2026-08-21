package cmd

import (
	"os"
)

// IsNonInteractive reports whether the current process should avoid interactive prompts.
// Active when DBTOOLS_NO_PROMPT=1, CI=1, CI=true, or when --json output is requested.
func IsNonInteractive() bool {
	if os.Getenv("DBTOOLS_NO_PROMPT") == "1" || os.Getenv("CI") == "true" || os.Getenv("CI") == "1" {
		return true
	}
	if jsonOutput {
		return true
	}
	return false
}
