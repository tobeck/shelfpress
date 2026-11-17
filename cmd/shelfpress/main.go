// Command shelfpress publishes markdown documentation to a BookStack instance.
package main

import (
	"fmt"
	"os"

	"github.com/tobeck/shelfpress/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "shelfpress:", err)
		os.Exit(1)
	}
}
