// Package sync reconciles a documentation tree against a BookStack instance.
//
// Reconciliation is split into a planner, which reads and produces a Plan, and
// an applier, which executes one. Both `plan` and `sync` run the same planner;
// `plan` simply stops before applying. There is deliberately no separate
// preview path, because a dry run that can diverge from the real run is
// worthless — see CLAUDE.md rule 4.
package sync

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tobeck/shelfpress/internal/bookstack"
)

// Tags shelfpress writes onto everything it creates.
//
// These are a compatibility surface: they live in users' BookStack instances
// and are how a later run recognises its own content. Renaming one strands
// every existing page, so it may only change through an explicit migration —
// see CLAUDE.md rule 2.
const (
	// TagSource records the repo-relative path of the file a page came from.
	TagSource = "sp-source"

	// TagManagedBy marks content shelfpress created.
	TagManagedBy = "sp-managed-by"

	// TagRepo scopes content to one repository, so several repos can publish
	// into the same BookStack without colliding.
	TagRepo = "sp-repo"

	// ManagedByValue is the value of TagManagedBy.
	ManagedByValue = "shelfpress"
)

// OpKind identifies what an operation does.
type OpKind string

// The operations v0.1 can plan. Deletes arrive in v0.2.
const (
	OpCreateShelf      OpKind = "create-shelf"
	OpCreateBook       OpKind = "create-book"
	OpCreatePage       OpKind = "create-page"
	OpUpdatePage       OpKind = "update-page"
	OpUpdateShelfBooks OpKind = "update-shelf-books"
)

// Op is a single change to make in BookStack.
//
// The applier fills in IDs that are only known at apply time, so an Op created
// by the planner may reference a book that does not exist yet.
type Op struct {
	// Kind is what this operation does.
	Kind OpKind

	// Target describes the affected object for humans, e.g. `page "Use
	// Quarkus"`. It is never used to look anything up.
	Target string

	// Detail is an optional clarifying phrase, such as why an update was
	// planned or which book a page belongs to.
	Detail string

	// ID is the BookStack identifier of an existing object this operation
	// modifies. Zero for creations.
	ID int

	// BookName is the book a page belongs to. The applier resolves it to an
	// ID, since the book may itself be created earlier in the same plan.
	BookName string

	// Shelf, Book and Page carry the payload for whichever kind applies.
	Shelf *bookstack.ShelfInput
	Book  *bookstack.BookInput
	Page  *bookstack.PageInput
}

// String renders an operation for logs and reports.
func (o Op) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", o.Kind, o.Target)
	if o.Detail != "" {
		fmt.Fprintf(&b, " (%s)", o.Detail)
	}
	return b.String()
}

// Plan is the ordered set of operations that would make BookStack match the
// repository. Order is significant: a page cannot be created before its book.
type Plan struct {
	// Ops are applied in order.
	Ops []Op

	// ShelfName is the shelf this plan targets, for reporting.
	ShelfName string

	// ShelfExists records whether the shelf was already present.
	ShelfExists bool

	// Unmanaged lists pages carrying shelfpress tags whose source file is
	// gone. v0.1 only reports them; pruning arrives in v0.2.
	Unmanaged []Orphan
}

// Orphan is managed content whose source file no longer exists.
type Orphan struct {
	PageID     int
	Title      string
	SourcePath string
}

// IsEmpty reports whether the plan would change nothing.
func (p Plan) IsEmpty() bool { return len(p.Ops) == 0 }

// Summary counts operations by kind.
func (p Plan) Summary() map[OpKind]int {
	counts := make(map[OpKind]int, len(p.Ops))
	for _, op := range p.Ops {
		counts[op.Kind]++
	}
	return counts
}

// Count returns the number of operations.
func (p Plan) Count() int { return len(p.Ops) }

// managedTags returns the tags every object shelfpress creates carries.
func managedTags(repo string) bookstack.Tags {
	return bookstack.Tags{
		{Name: TagManagedBy, Value: ManagedByValue},
		{Name: TagRepo, Value: repo},
	}
}

// pageTags returns the tags for a page created from the given source file.
func pageTags(repo, sourcePath string) bookstack.Tags {
	return append(managedTags(repo), bookstack.Tag{Name: TagSource, Value: sourcePath})
}

// isManaged reports whether content carries shelfpress's marker for this repo.
//
// Anything failing this check belongs to the user and must never be modified
// or deleted — see CLAUDE.md rule 3.
func isManaged(tags bookstack.Tags, repo string) bool {
	owner, ok := tags.Get(TagManagedBy)
	if !ok || owner != ManagedByValue {
		return false
	}
	tagged, ok := tags.Get(TagRepo)
	return ok && tagged == repo
}

// sortOrphans orders orphans by source path so reports are deterministic.
func sortOrphans(o []Orphan) {
	slices.SortFunc(o, func(a, b Orphan) int {
		return strings.Compare(a.SourcePath, b.SourcePath)
	})
}
