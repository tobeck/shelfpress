// Command shelfpress publishes markdown documentation to a BookStack instance.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/tobeck/shelfpress/internal/cli"
)

// exitInterrupted is the shell convention for a process stopped by SIGINT.
const exitInterrupted = 130

func main() {
	err := cli.Execute()
	code := cli.ExitCode(err)

	switch {
	case err == nil:
	case errors.Is(err, context.Canceled):
		// Ctrl-C is a deliberate stop, not a failure to report.
		code = exitInterrupted
	case code == cli.ExitChangesPending:
		// --detailed-exitcode asks for drift to be signalled through the
		// exit code; an error line alongside it would be noise.
	default:
		fmt.Fprintln(os.Stderr, "shelfpress:", err)
	}

	os.Exit(code)
}
