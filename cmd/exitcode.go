package cmd

import "fmt"

// ExitCodeError is an error that carries an explicit process exit code.
// CLI commands return this when they need to signal specific states
// (such as exit code 2 for drift or pending changes) to CI systems and AI agents.
type ExitCodeError struct {
	Code    int
	Message string
	Err     error
}

func (e *ExitCodeError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func (e *ExitCodeError) Unwrap() error {
	return e.Err
}

// ExitCode returns an ExitCodeError with code and message.
func ExitCode(code int, msg string) error {
	return &ExitCodeError{Code: code, Message: msg}
}
