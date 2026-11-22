package sync

import (
	"context"
	"fmt"

	"github.com/tobeck/shelfpress/internal/bookstack"
)

// Applier executes a Plan against BookStack.
//
// This is the only type in the package that writes. There is deliberately no
// dry-run branch here: a caller wanting a preview simply does not call Apply,
// so the planned and applied paths can never diverge (CLAUDE.md rule 4).
//
// Apply does not roll back. A plan that fails partway leaves BookStack in the
// state the successful operations produced, and the returned Result reports
// exactly what happened. Rollback would mean deleting content that may have
// existed beforehand, which is more dangerous than stopping.
type Applier struct {
	client bookstack.Client
}

// NewApplier returns an applier for the given client.
func NewApplier(c bookstack.Client) *Applier {
	return &Applier{client: c}
}

// Result records what an apply actually did.
type Result struct {
	// Applied are the operations that completed, in order.
	Applied []Op

	// Created maps each created page's source path to its new ID.
	Created map[string]int

	// ShelfID is the shelf the run targeted, once known.
	ShelfID int

	// Failed is the operation that failed, when one did.
	Failed *Op
}

// Count returns how many operations completed.
func (r Result) Count() int { return len(r.Applied) }

// Summary counts completed operations by kind.
func (r Result) Summary() map[OpKind]int {
	counts := make(map[OpKind]int, len(r.Applied))
	for _, op := range r.Applied {
		counts[op.Kind]++
	}
	return counts
}

// Apply executes the plan's operations in order.
//
// Identifiers created during the run are threaded forward: a book created here
// supplies the book_id for the pages that follow, and for the shelf membership
// update at the end. On failure it returns the partial Result alongside the
// error so the caller can report what did happen.
func (a *Applier) Apply(ctx context.Context, plan Plan) (Result, error) {
	result := Result{Created: map[string]int{}}

	// books maps a book name to its ID, whether pre-existing or created in
	// this run.
	books := map[string]int{}

	for i := range plan.Ops {
		op := plan.Ops[i]

		if err := ctx.Err(); err != nil {
			result.Failed = &op
			return result, fmt.Errorf("applying %s: %w", op, err)
		}

		if err := a.applyOp(ctx, op, &result, books); err != nil {
			result.Failed = &op
			return result, fmt.Errorf("applying %s: %w", op, err)
		}
		result.Applied = append(result.Applied, op)
	}

	return result, nil
}

// applyOp performs one operation, recording any identifier it produces.
func (a *Applier) applyOp(ctx context.Context, op Op, result *Result, books map[string]int) error {
	switch op.Kind {
	case OpCreateShelf:
		return a.createShelf(ctx, op, result)
	case OpCreateBook:
		return a.createBook(ctx, op, books)
	case OpCreatePage:
		return a.createPage(ctx, op, result, books)
	case OpUpdatePage:
		return a.updatePage(ctx, op, books)
	case OpUpdateShelfBooks:
		return a.updateShelfBooks(ctx, op, result, books)
	default:
		return fmt.Errorf("unknown operation kind %q", op.Kind)
	}
}

func (a *Applier) createShelf(ctx context.Context, op Op, result *Result) error {
	if op.Shelf == nil {
		return fmt.Errorf("operation carries no shelf input")
	}
	shelf, err := a.client.CreateShelf(ctx, *op.Shelf)
	if err != nil {
		return err
	}
	result.ShelfID = shelf.ID
	return nil
}

func (a *Applier) createBook(ctx context.Context, op Op, books map[string]int) error {
	if op.Book == nil {
		return fmt.Errorf("operation carries no book input")
	}
	book, err := a.client.CreateBook(ctx, *op.Book)
	if err != nil {
		return err
	}
	books[book.Name] = book.ID
	return nil
}

func (a *Applier) createPage(ctx context.Context, op Op, result *Result, books map[string]int) error {
	if op.Page == nil {
		return fmt.Errorf("operation carries no page input")
	}

	bookID, err := a.resolveBook(ctx, op.BookName, books)
	if err != nil {
		return err
	}

	input := *op.Page
	input.BookID = bookID

	page, err := a.client.CreatePage(ctx, input)
	if err != nil {
		return err
	}

	if source, ok := input.Tags.Get(TagSource); ok {
		result.Created[source] = page.ID
	}
	return nil
}

func (a *Applier) updatePage(ctx context.Context, op Op, books map[string]int) error {
	if op.Page == nil {
		return fmt.Errorf("operation carries no page input")
	}
	if op.ID == 0 {
		return fmt.Errorf("update targets no page ID")
	}

	input := *op.Page
	// A page's book is normally unchanged on update, but resolving it keeps
	// the payload complete if the book was created in this same run.
	if op.BookName != "" {
		bookID, err := a.resolveBook(ctx, op.BookName, books)
		if err != nil {
			return err
		}
		input.BookID = bookID
	}

	_, err := a.client.UpdatePage(ctx, op.ID, input)
	return err
}

func (a *Applier) updateShelfBooks(ctx context.Context, op Op, result *Result, books map[string]int) error {
	shelfID := op.ID
	if shelfID == 0 {
		// The shelf was created earlier in this run.
		shelfID = result.ShelfID
	}
	if shelfID == 0 {
		return fmt.Errorf("no shelf ID available: the shelf was neither found nor created")
	}

	bookID, err := a.resolveBook(ctx, op.BookName, books)
	if err != nil {
		return err
	}

	// Shelf membership is replaced wholesale, so existing books must be
	// preserved rather than overwritten.
	existing, err := a.shelfBookIDs(ctx, shelfID)
	if err != nil {
		return err
	}
	for _, id := range existing {
		if id == bookID {
			return nil
		}
	}

	return a.client.UpdateShelfBooks(ctx, shelfID, append(existing, bookID))
}

// shelfBookIDs returns the books currently on a shelf.
// shelfBookIDs returns the books currently on a shelf.
//
// It reads the shelf's detail endpoint, because /api/shelves omits `books`
// from list responses. Reading membership from the list would return nothing,
// and since the API replaces membership wholesale, every sync would delete
// every other book from a shared shelf — content shelfpress does not own
// (CLAUDE.md rule 3).
func (a *Applier) shelfBookIDs(ctx context.Context, shelfID int) ([]int, error) {
	shelf, err := a.client.GetShelf(ctx, shelfID)
	if err != nil {
		return nil, fmt.Errorf("reading shelf membership: %w", err)
	}

	ids := make([]int, 0, len(shelf.Books))
	for _, b := range shelf.Books {
		ids = append(ids, b.ID)
	}
	return ids, nil
}

// resolveBook finds a book's ID, preferring one created during this run and
// falling back to a lookup for books that already existed.
func (a *Applier) resolveBook(ctx context.Context, name string, books map[string]int) (int, error) {
	if name == "" {
		return 0, fmt.Errorf("operation names no book")
	}
	if id, ok := books[name]; ok {
		return id, nil
	}

	existing, err := a.client.ListBooks(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("looking up book %q: %w", name, err)
	}
	for _, book := range existing {
		if book.Name == name {
			books[name] = book.ID
			return book.ID, nil
		}
	}
	return 0, fmt.Errorf("book %q does not exist and was not created by this plan", name)
}
