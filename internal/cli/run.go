package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/tobeck/shelfpress/internal/bookstack"
	"github.com/tobeck/shelfpress/internal/config"
	"github.com/tobeck/shelfpress/internal/report"
	"github.com/tobeck/shelfpress/internal/scan"
	"github.com/tobeck/shelfpress/internal/sync"
)

// newClient builds the BookStack client. It is a variable so tests can point
// the commands at a stub server without reaching the network.
var newClient = func(cfg config.Config) (bookstack.Client, error) {
	return bookstack.New(cfg.BookStack.URL, cfg.BookStack.TokenID, cfg.BookStack.TokenSecret)
}

func newPlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show what a sync would change, without writing anything",
		Long: `plan reads the documentation root and BookStack, then prints the changes a
sync would make. It performs no writes.

It runs the same planner sync does, so what it shows is exactly what sync
would apply.`,
		Example: `  shelfpress plan --root docs/decisions
  shelfpress plan --detailed-exitcode   # exit 3 when changes are pending`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			detailed, err := cmd.Flags().GetBool("detailed-exitcode")
			if err != nil {
				return err
			}
			return runPipeline(cmd, pipelineOptions{apply: false, detailedExit: detailed})
		},
	}

	cmd.Flags().Bool("detailed-exitcode", false,
		"exit 3 instead of 0 when there are changes to apply")
	return cmd
}

func newSyncCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Publish the documentation to BookStack",
		Long: `sync reads the documentation root and makes BookStack match it.

Only content shelfpress created is modified; pages written by hand in
BookStack are left alone. With --dry-run it behaves exactly like plan.`,
		Example: `  shelfpress sync --root docs/decisions
  shelfpress sync --dry-run             # same as plan`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dryRun, err := cmd.Flags().GetBool("dry-run")
			if err != nil {
				return err
			}
			return runPipeline(cmd, pipelineOptions{apply: !dryRun})
		},
	}

	cmd.Flags().Bool("dry-run", false, "show what would change without writing")
	return cmd
}

// pipelineOptions distinguishes the two commands.
//
// Note what is absent: there is no separate preview code path. `plan` and
// `sync --dry-run` run the identical pipeline and simply stop before applying
// (CLAUDE.md rule 4).
type pipelineOptions struct {
	// apply writes the plan to BookStack. When false nothing is written.
	apply bool

	// detailedExit returns ExitChangesPending when a plan has work to do.
	detailedExit bool
}

// runPipeline is the single path behind both commands: load config, scan,
// plan, report, and — only for sync — apply.
func runPipeline(cmd *cobra.Command, opts pipelineOptions) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	docs, err := scan.Walk(scan.Options{
		Root:  cfg.Root,
		Base:  ".",
		Warnf: warnFunc(cmd.ErrOrStderr()),
	})
	if err != nil {
		return err
	}

	client, err := newClient(cfg)
	if err != nil {
		return wrapConfig(err)
	}

	plan, err := sync.NewPlanner(client, cfg).Plan(ctx, docs)
	if err != nil {
		return err
	}

	if !opts.apply {
		if err := report.Plan(out, plan, report.PlanOptions{DryRun: true}); err != nil {
			return err
		}
		if opts.detailedExit && !plan.IsEmpty() {
			return changesPending(plan.Count())
		}
		return nil
	}

	if err := report.Plan(out, plan, report.PlanOptions{}); err != nil {
		return err
	}
	if plan.IsEmpty() {
		return nil
	}

	result, applyErr := sync.NewApplier(client).Apply(ctx, plan)

	// The result is reported either way: a failed run still needs to say
	// what it managed to do before stopping.
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if err := report.Result(out, result, applyErr); err != nil {
		return err
	}
	return applyErr
}

// warnFunc routes non-fatal notices to stderr, so stdout carries only the
// report and stays safe to redirect into a file or a CI summary.
func warnFunc(w io.Writer) func(string, ...any) {
	return func(format string, args ...any) {
		// A warning that cannot be written is not worth failing a sync over.
		_, _ = fmt.Fprintf(w, "warning: "+format+"\n", args...)
	}
}
