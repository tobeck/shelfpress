// Package cli wires the shelfpress command tree.
//
// This package parses flags, builds configuration, and calls the packages that
// do the work. It holds no sync logic of its own, and it reads no CI platform's
// environment variables — wrappers translate those into flags (CLAUDE.md rule 1).
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/tobeck/shelfpress/internal/config"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// Execute runs the root command, returning an error whose exit code is
// available from ExitCode.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return newRoot().ExecuteContext(ctx)
}

// newRoot builds the command tree.
func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "shelfpress",
		Short: "Publish your repo's markdown docs to BookStack",
		Long: `shelfpress publishes a repository's markdown documentation to BookStack.

Any markdown tree works. MADR architecture decision records get first-class
support: their frontmatter becomes BookStack tags and their NNNN- filename
prefix sets page order.

Content shelfpress creates is tagged, and it only ever modifies its own
content — pages written by hand in BookStack are never touched.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	addSharedFlags(root.PersistentFlags())

	root.AddCommand(
		newPlanCommand(),
		newSyncCommand(),
		newVersionCommand(),
	)
	return root
}

// addSharedFlags defines the flags both plan and sync accept.
//
// Each flag's help names its environment variable, because a user hitting a
// missing-value error in CI needs to know what to set, not just which flag
// exists.
func addSharedFlags(f *pflag.FlagSet) {
	f.String("config", "", "config file (default: ./.shelfpress.yml)")
	f.String("root", "", "directory of markdown files to publish [SHELFPRESS_ROOT]")
	f.String("url", "", "BookStack base URL [BOOKSTACK_URL]")
	f.String("token-id", "", "BookStack API token ID [BOOKSTACK_TOKEN_ID]")
	f.String("token-secret", "", "BookStack API token secret [BOOKSTACK_TOKEN_SECRET]")
	f.String("repo", "", "project identifier for content tags (default: the git remote) [SHELFPRESS_REPO]")
	f.String("shelf", "", "BookStack shelf name (default: the repo name)")
	f.String("format", "", "output format: human")
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the shelfpress version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}

// loadConfig resolves and validates configuration for a command.
//
// Validation failures are marked as configuration errors so the process exits
// with ExitConfig rather than a generic failure.
func loadConfig(cmd *cobra.Command) (config.Config, error) {
	file, err := cmd.Flags().GetString("config")
	if err != nil {
		return config.Config{}, err
	}

	cfg, err := config.Load(config.Options{File: file, Flags: cmd.Flags()})
	if err != nil {
		return config.Config{}, wrapConfig(err)
	}

	if err := cfg.Validate(); err != nil {
		return config.Config{}, wrapConfig(err)
	}
	return cfg, nil
}
