// Package cli wires the shelfpress command tree.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// Execute runs the root command.
func Execute() error {
	return newRoot().Execute()
}

// newRoot builds the command tree.
func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "shelfpress",
		Short:         "Publish your repo's markdown docs to BookStack",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the shelfpress version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	})

	return root
}
