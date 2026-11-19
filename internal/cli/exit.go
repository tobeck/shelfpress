package cli

import (
	"errors"
	"fmt"
)

// Exit codes are part of the public interface: CI jobs branch on them, so they
// are covered by semver and must not be renumbered.
const (
	// ExitOK means the command succeeded.
	ExitOK = 0

	// ExitError means a runtime failure — the API was unreachable, a write
	// was rejected, a file could not be read.
	ExitError = 1

	// ExitConfig means the configuration was invalid or incomplete. Separate
	// from ExitError so a pipeline can tell "you configured me wrong" from
	// "BookStack is down" without parsing text.
	ExitConfig = 2

	// ExitChangesPending is returned by `plan --detailed-exitcode` when there
	// are changes to apply, mirroring `terraform plan`. It lets CI gate on
	// documentation drift.
	ExitChangesPending = 3
)

// exitError carries an exit code alongside an error.
type exitError struct {
	code int
	err  error
}

// Error implements error.
func (e *exitError) Error() string { return e.err.Error() }

// Unwrap exposes the underlying error to errors.Is and errors.As.
func (e *exitError) Unwrap() error { return e.err }

// ExitCode reports the process exit code for an error returned by Execute.
// Errors with no explicit code are runtime failures.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return ExitError
}

// configErrorf returns an error that exits with ExitConfig.
func configErrorf(format string, args ...any) error {
	return &exitError{code: ExitConfig, err: fmt.Errorf(format, args...)}
}

// wrapConfig marks an existing error as a configuration problem.
func wrapConfig(err error) error {
	return &exitError{code: ExitConfig, err: err}
}

// changesPending signals that a plan found work to do.
func changesPending(n int) error {
	return &exitError{
		code: ExitChangesPending,
		err:  fmt.Errorf("%d change(s) pending", n),
	}
}
