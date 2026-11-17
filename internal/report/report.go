// Package report renders plans and results for people to read.
//
// Rendering is deliberately separate from planning and applying so that the
// same Plan can be shown before a run and the same Result after one, without
// either of those packages knowing about presentation.
//
// v0.1 ships the human format only. The Format switch exists so that the json
// and markdown renderers in v0.3 slot in without changing callers.
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/tobeck/shelfpress/internal/sync"
)

// Format selects a renderer.
type Format string

// The formats a report can be rendered in. Only FormatHuman is implemented in
// v0.1; the others are reserved so the CLI's flag surface does not change.
const (
	FormatHuman Format = "human"
)

// opLabel is the verb shown for each operation kind.
//
// A table rather than string munging on the kind, so the wording is a
// deliberate choice and the output cannot change just because a constant is
// renamed.
var opLabel = map[sync.OpKind]string{
	sync.OpCreateShelf:      "create shelf",
	sync.OpCreateBook:       "create book",
	sync.OpCreatePage:       "create page",
	sync.OpUpdatePage:       "update page",
	sync.OpUpdateShelfBooks: "shelve book",
}

// summaryOrder fixes the order kinds appear in a summary line. Ranging over
// the summary map directly would give different output on every run.
var summaryOrder = []sync.OpKind{
	sync.OpCreateShelf,
	sync.OpCreateBook,
	sync.OpCreatePage,
	sync.OpUpdatePage,
	sync.OpUpdateShelfBooks,
}

// PlanOptions controls how a plan is rendered.
type PlanOptions struct {
	// DryRun states that nothing will be written.
	DryRun bool
}

// Plan renders a pending plan.
func Plan(w io.Writer, p sync.Plan, opts PlanOptions) error {
	var b strings.Builder

	writeShelfLine(&b, p)

	if p.IsEmpty() {
		b.WriteString("\nNo changes: BookStack already matches the repository.\n")
		writeOrphans(&b, p)
		_, err := io.WriteString(w, b.String())
		return err
	}

	b.WriteString("\nPlanned changes:\n")
	for _, op := range p.Ops {
		writeOp(&b, op)
	}

	fmt.Fprintf(&b, "\n%s.\n", summarise(p.Summary(), p.Count()))
	if opts.DryRun {
		b.WriteString("Dry run — nothing was written.\n")
	}

	writeOrphans(&b, p)

	_, err := io.WriteString(w, b.String())
	return err
}

// Result renders what an apply actually did. failure is the error the run
// ended with, or nil when it completed.
func Result(w io.Writer, r sync.Result, failure error) error {
	var b strings.Builder

	if r.Count() == 0 && failure == nil {
		b.WriteString("No changes were needed.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	if r.Count() > 0 {
		b.WriteString("Applied:\n")
		for _, op := range r.Applied {
			writeOp(&b, op)
		}
		fmt.Fprintf(&b, "\n%s.\n", summarise(r.Summary(), r.Count()))
	}

	if failure != nil {
		if r.Failed != nil {
			fmt.Fprintf(&b, "\nFailed at %s %s.\n", opLabel[r.Failed.Kind], r.Failed.Target)
		}
		fmt.Fprintf(&b, "Error: %s\n", failure)
		if r.Count() > 0 {
			b.WriteString("Earlier changes were kept; re-running will resume from where this stopped.\n")
		}
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// writeShelfLine names the target shelf and whether it already exists.
func writeShelfLine(b *strings.Builder, p sync.Plan) {
	state := "will be created"
	if p.ShelfExists {
		state = "exists"
	}
	fmt.Fprintf(b, "Shelf: %s (%s)\n", p.ShelfName, state)
}

// writeOp renders one operation as a bullet.
func writeOp(b *strings.Builder, op sync.Op) {
	label, ok := opLabel[op.Kind]
	if !ok {
		label = string(op.Kind)
	}

	fmt.Fprintf(b, "  %-12s %s", label, op.Target)
	if op.Detail != "" {
		fmt.Fprintf(b, " — %s", op.Detail)
	}
	b.WriteString("\n")
}

// writeOrphans lists managed pages whose source file is gone.
func writeOrphans(b *strings.Builder, p sync.Plan) {
	if len(p.Unmanaged) == 0 {
		return
	}

	fmt.Fprintf(b, "\n%s in BookStack with no file in the repository:\n", plural(len(p.Unmanaged), "page", "pages"))
	for _, o := range p.Unmanaged {
		fmt.Fprintf(b, "  %s — was %s\n", o.Title, o.SourcePath)
	}
	b.WriteString("Left in place. Removing them automatically arrives in v0.2.\n")
}

// summarise renders the per-kind counts in a fixed order.
func summarise(counts map[sync.OpKind]int, total int) string {
	parts := make([]string, 0, len(counts))

	for _, kind := range summaryOrder {
		if n := counts[kind]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, opLabel[kind]))
		}
	}

	// Any kind added later but missing from summaryOrder still appears,
	// sorted, rather than being silently dropped.
	if len(parts) < len(counts) {
		var extra []string
		for kind, n := range counts {
			if n == 0 || knownKind(kind) {
				continue
			}
			extra = append(extra, fmt.Sprintf("%d %s", n, kind))
		}
		sort.Strings(extra)
		parts = append(parts, extra...)
	}

	if len(parts) == 0 {
		return "No changes"
	}
	return fmt.Sprintf("%s (%s)", plural(total, "change", "changes"), strings.Join(parts, ", "))
}

// knownKind reports whether a kind has a fixed position in the summary.
func knownKind(k sync.OpKind) bool {
	for _, known := range summaryOrder {
		if known == k {
			return true
		}
	}
	return false
}

// plural renders a count with the appropriate noun.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
