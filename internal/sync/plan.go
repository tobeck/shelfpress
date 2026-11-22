package sync

import (
	"context"
	"fmt"
	"strings"

	"github.com/tobeck/shelfpress/internal/bookstack"
	"github.com/tobeck/shelfpress/internal/config"
	"github.com/tobeck/shelfpress/internal/scan"
)

// Planner diffs scanned documents against a BookStack instance.
//
// It performs reads only; nothing here writes. That separation is what lets
// `plan` and `sync` share a single code path.
type Planner struct {
	client bookstack.Client
	cfg    config.Config
}

// NewPlanner returns a planner for the given client and configuration.
func NewPlanner(c bookstack.Client, cfg config.Config) *Planner {
	return &Planner{client: c, cfg: cfg}
}

// Plan computes the operations needed to make BookStack match docs.
//
// Running it twice against an unchanged repository produces an empty plan the
// second time; see CLAUDE.md rule 5.
func (p *Planner) Plan(ctx context.Context, docs []scan.Document) (Plan, error) {
	state, err := p.fetchState(ctx)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		ShelfName:   p.cfg.Shelf,
		ShelfExists: state.shelf != nil,
	}

	// v0.1 maps the whole flat directory to a single book named for the
	// shelf. Directory-derived books arrive in v0.3.
	bookName := p.cfg.Shelf

	if state.shelf == nil {
		plan.Ops = append(plan.Ops, Op{
			Kind:   OpCreateShelf,
			Target: fmt.Sprintf("%q", p.cfg.Shelf),
			Shelf: &bookstack.ShelfInput{
				Name:        p.cfg.Shelf,
				Description: p.cfg.ShelfDescription,
				Tags:        managedTags(p.cfg.Repo),
			},
		})
	}

	book := state.books[bookName]
	if book == nil {
		plan.Ops = append(plan.Ops, Op{
			Kind:   OpCreateBook,
			Target: fmt.Sprintf("%q", bookName),
			Book: &bookstack.BookInput{
				Name: bookName,
				Tags: managedTags(p.cfg.Repo),
			},
		})
	}

	plan.Ops = append(plan.Ops, p.pageOps(docs, state, bookName)...)
	plan.Unmanaged = orphans(docs, state)

	if op, ok := p.shelfMembershipOp(state, bookName, book); ok {
		plan.Ops = append(plan.Ops, op)
	}

	return plan, nil
}

// state is the BookStack content shelfpress already manages for this repo.
type state struct {
	shelf *bookstack.Shelf

	// books and pages are keyed by book name and source path respectively.
	books map[string]*bookstack.Book
	pages map[string]bookstack.Page
}

// fetchState reads everything the diff needs.
func (p *Planner) fetchState(ctx context.Context) (state, error) {
	s := state{
		books: map[string]*bookstack.Book{},
		pages: map[string]bookstack.Page{},
	}

	shelves, err := p.client.ListShelves(ctx, nil)
	if err != nil {
		return state{}, fmt.Errorf("listing shelves: %w", err)
	}
	for _, shelf := range shelves {
		if shelf.Name != p.cfg.Shelf {
			continue
		}

		// The list response omits the shelf's books, exactly as it omits
		// tags and as /api/search omits page bodies. Reading membership
		// from the list would find the book missing every time and
		// re-write the shelf on every run — the same defect as #22, one
		// level up. The detail endpoint is the only source for `books`.
		detail, err := p.client.GetShelf(ctx, shelf.ID)
		if err != nil {
			return state{}, fmt.Errorf("reading shelf %d (%s): %w", shelf.ID, shelf.Name, err)
		}
		s.shelf = &detail
		break
	}

	books, err := p.client.ListBooks(ctx, nil)
	if err != nil {
		return state{}, fmt.Errorf("listing books: %w", err)
	}
	for i, book := range books {
		// Books listed by the API carry no tags, so ownership is confirmed
		// against the shelf's own book list below.
		if _, taken := s.books[book.Name]; !taken {
			s.books[book.Name] = &books[i]
		}
	}

	// Pages are found by tag: list responses omit tags entirely, so the
	// search endpoint is the only way to recover sp-source in one request.
	managed, err := p.client.SearchPagesByTag(ctx, TagRepo, p.cfg.Repo)
	if err != nil {
		return state{}, fmt.Errorf("searching managed pages: %w", err)
	}

	for _, page := range managed {
		if !isManaged(page.Tags, p.cfg.Repo) {
			continue
		}
		source, ok := page.Tags.Get(TagSource)
		if !ok || source == "" {
			continue
		}

		// Search results carry tags but no body, so the stored markdown has
		// to be fetched per page before it can be compared. Skipping this
		// makes every page look changed on every run, which defeats
		// idempotency entirely.
		detail, err := p.client.GetPage(ctx, page.ID)
		if err != nil {
			return state{}, fmt.Errorf("reading page %d (%s): %w", page.ID, source, err)
		}
		detail.Tags = page.Tags

		s.pages[source] = detail
	}

	return s, nil
}

// pageOps produces the create and update operations for the scanned documents.
func (p *Planner) pageOps(docs []scan.Document, s state, bookName string) []Op {
	ops := make([]Op, 0, len(docs))

	for _, doc := range docs {
		existing, found := s.pages[doc.SourcePath]
		input := &bookstack.PageInput{
			Name:     doc.Title,
			Markdown: doc.Body,
			Priority: doc.Priority,
			Tags:     pageTags(p.cfg.Repo, doc.SourcePath),
		}

		if !found {
			ops = append(ops, Op{
				Kind:     OpCreatePage,
				Target:   fmt.Sprintf("%q", doc.Title),
				Detail:   "from " + doc.SourcePath,
				BookName: bookName,
				Page:     input,
			})
			continue
		}

		reason, differs := pageDiffers(existing, doc)
		if !differs {
			continue
		}
		ops = append(ops, Op{
			Kind:     OpUpdatePage,
			Target:   fmt.Sprintf("%q", doc.Title),
			Detail:   reason,
			ID:       existing.ID,
			BookName: bookName,
			Page:     input,
		})
	}
	return ops
}

// pageDiffers reports whether a page needs updating, and why.
//
// Returning false for an unchanged page is what makes a no-op run issue no
// writes at all.
func pageDiffers(existing bookstack.Page, doc scan.Document) (string, bool) {
	var reasons []string

	if existing.Name != doc.Title {
		reasons = append(reasons, "title changed")
	}
	if normaliseBody(existing.Markdown) != normaliseBody(doc.Body) {
		reasons = append(reasons, "body changed")
	}
	if existing.Priority != doc.Priority {
		reasons = append(reasons, "order changed")
	}
	if len(reasons) == 0 {
		return "", false
	}
	return strings.Join(reasons, ", "), true
}

// normaliseBody makes markdown comparable across a BookStack round trip.
//
// Verified against a live instance (see test/integration): BookStack returns
// the markdown it was given, except that it strips leading and trailing blank
// lines. Interior content — trailing spaces on a line, CRLF endings, tabs,
// fenced blocks — survives verbatim.
//
// Both ends matter. Stripping frontmatter leaves a leading newline where it
// was, which BookStack drops on store; comparing without trimming the left
// side makes every document with frontmatter — every MADR record — look
// changed on every run.
func normaliseBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// orphans lists managed pages whose source file is no longer present.
func orphans(docs []scan.Document, s state) []Orphan {
	if len(s.pages) == 0 {
		return nil
	}

	present := make(map[string]struct{}, len(docs))
	for _, doc := range docs {
		present[doc.SourcePath] = struct{}{}
	}

	var out []Orphan
	for source, page := range s.pages {
		if _, ok := present[source]; ok {
			continue
		}
		out = append(out, Orphan{PageID: page.ID, Title: page.Name, SourcePath: source})
	}

	// Map iteration is random; sort so reports and tests are stable.
	sortOrphans(out)
	return out
}

// shelfMembershipOp returns an operation adding the book to the shelf, when
// that is not already the case.
func (p *Planner) shelfMembershipOp(s state, bookName string, book *bookstack.Book) (Op, bool) {
	// A shelf being created always needs its membership set afterwards,
	// because the book may not exist yet either.
	if s.shelf == nil {
		return Op{
			Kind:     OpUpdateShelfBooks,
			Target:   fmt.Sprintf("%q", p.cfg.Shelf),
			Detail:   "onto the shelf",
			BookName: bookName,
		}, true
	}

	if book != nil {
		for _, shelved := range s.shelf.Books {
			if shelved.ID == book.ID {
				return Op{}, false
			}
		}
	}

	return Op{
		Kind:     OpUpdateShelfBooks,
		Target:   fmt.Sprintf("%q", p.cfg.Shelf),
		Detail:   "onto the shelf",
		ID:       s.shelf.ID,
		BookName: bookName,
	}, true
}
