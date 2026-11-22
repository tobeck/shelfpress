package sync

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tobeck/shelfpress/internal/bookstack"
	"github.com/tobeck/shelfpress/internal/config"
	"github.com/tobeck/shelfpress/internal/scan"
)

const (
	testRepo  = "tobeck/shelfpress"
	testShelf = "shelfpress"
)

func testConfig() config.Config {
	return config.Config{
		Shelf: testShelf,
		Repo:  testRepo,
		Root:  "docs/decisions",
	}
}

// doc builds a scanned document.
func doc(source, title, body string, priority int) scan.Document {
	return scan.Document{SourcePath: source, Title: title, Body: body, Priority: priority}
}

// kinds lists the operation kinds in a plan, in order.
func kinds(p Plan) []OpKind {
	out := make([]OpKind, len(p.Ops))
	for i, op := range p.Ops {
		out[i] = op.Kind
	}
	return out
}

func sameKinds(got []OpKind, want ...OpKind) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestPlanEmptyBookStack(t *testing.T) {
	f := newFake()
	docs := []scan.Document{
		doc("docs/decisions/0001-use-quarkus.md", "Use Quarkus", "# Use Quarkus\n", 1),
		doc("docs/decisions/0002-use-kafka.md", "Use Kafka", "# Use Kafka\n", 2),
	}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	got := kinds(plan)
	want := []OpKind{OpCreateShelf, OpCreateBook, OpCreatePage, OpCreatePage, OpUpdateShelfBooks}
	if !sameKinds(got, want...) {
		t.Fatalf("ops = %v, want %v", got, want)
	}
	if plan.ShelfExists {
		t.Error("ShelfExists = true, want false for an empty instance")
	}

	// Planning must not write anything.
	if len(f.writes) != 0 {
		t.Errorf("planner performed writes: %v", f.writes)
	}
}

func TestPlanIsIdempotent(t *testing.T) {
	f := newFake()
	book := f.withBook(testShelf)
	f.withShelf(testShelf, book)
	f.withPage(book.ID, "Use Quarkus", "docs/decisions/0001-use-quarkus.md", "# Use Quarkus\n", 1)

	docs := []scan.Document{
		doc("docs/decisions/0001-use-quarkus.md", "Use Quarkus", "# Use Quarkus\n", 1),
	}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if !plan.IsEmpty() {
		t.Errorf("plan is not empty: %v", plan.Ops)
	}
	if plan.Count() != 0 {
		t.Errorf("Count() = %d, want 0", plan.Count())
	}
}

func TestPlanDetectsChanges(t *testing.T) {
	const source = "docs/decisions/0001-x.md"

	tests := []struct {
		name       string
		existing   func(*fakeClient, int) bookstack.Page
		document   scan.Document
		wantUpdate bool
		wantReason string
	}{
		{
			name: "identical",
			existing: func(f *fakeClient, book int) bookstack.Page {
				return f.withPage(book, "Title", source, "body\n", 1)
			},
			document:   doc(source, "Title", "body\n", 1),
			wantUpdate: false,
		},
		{
			name: "title changed",
			existing: func(f *fakeClient, book int) bookstack.Page {
				return f.withPage(book, "Old Title", source, "body\n", 1)
			},
			document:   doc(source, "New Title", "body\n", 1),
			wantUpdate: true,
			wantReason: "title changed",
		},
		{
			name: "body changed",
			existing: func(f *fakeClient, book int) bookstack.Page {
				return f.withPage(book, "Title", source, "old body\n", 1)
			},
			document:   doc(source, "Title", "new body\n", 1),
			wantUpdate: true,
			wantReason: "body changed",
		},
		{
			name: "priority changed",
			existing: func(f *fakeClient, book int) bookstack.Page {
				return f.withPage(book, "Title", source, "body\n", 1)
			},
			document:   doc(source, "Title", "body\n", 5),
			wantUpdate: true,
			wantReason: "order changed",
		},
		{
			name: "trailing newline only",
			existing: func(f *fakeClient, book int) bookstack.Page {
				return f.withPage(book, "Title", source, "body", 1)
			},
			document:   doc(source, "Title", "body\n\n", 1),
			wantUpdate: false,
		},
		{
			name: "CRLF versus LF",
			existing: func(f *fakeClient, book int) bookstack.Page {
				return f.withPage(book, "Title", source, "line one\nline two\n", 1)
			},
			document:   doc(source, "Title", "line one\r\nline two\r\n", 1),
			wantUpdate: false,
		},
		{
			name: "trailing spaces on a line",
			existing: func(f *fakeClient, book int) bookstack.Page {
				return f.withPage(book, "Title", source, "body\n", 1)
			},
			document:   doc(source, "Title", "body   \n", 1),
			wantUpdate: false,
		},
		{
			name: "leading whitespace is significant",
			existing: func(f *fakeClient, book int) bookstack.Page {
				return f.withPage(book, "Title", source, "body\n", 1)
			},
			document:   doc(source, "Title", "    body\n", 1),
			wantUpdate: true,
			wantReason: "body changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()
			book := f.withBook(testShelf)
			f.withShelf(testShelf, book)
			tt.existing(f, book.ID)

			plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), []scan.Document{tt.document})
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}

			if !tt.wantUpdate {
				if !plan.IsEmpty() {
					t.Fatalf("expected no changes, got %v", plan.Ops)
				}
				return
			}

			if len(plan.Ops) != 1 || plan.Ops[0].Kind != OpUpdatePage {
				t.Fatalf("ops = %v, want a single update-page", kinds(plan))
			}
			if !strings.Contains(plan.Ops[0].Detail, tt.wantReason) {
				t.Errorf("Detail = %q, want it to mention %q", plan.Ops[0].Detail, tt.wantReason)
			}
		})
	}
}

func TestPlanIgnoresUnmanagedContent(t *testing.T) {
	f := newFake()
	book := f.withBook(testShelf)
	f.withShelf(testShelf, book)

	// Hand-authored pages, and a page managed for a different repository.
	f.withUntaggedPage(book.ID, "Hand Written", nil)
	f.withUntaggedPage(book.ID, "Someone Else's", bookstack.Tags{
		{Name: TagManagedBy, Value: ManagedByValue},
		{Name: TagRepo, Value: "other/repo"},
		{Name: TagSource, Value: "docs/decisions/0001-x.md"},
	})

	// The repo has a file at the same source path the other repo claims.
	docs := []scan.Document{doc("docs/decisions/0001-x.md", "Mine", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	for _, op := range plan.Ops {
		if op.Kind == OpUpdatePage {
			t.Errorf("planned an update against unmanaged content: %v", op)
		}
	}
	// It must create its own page rather than adopt the other repo's.
	if len(plan.Ops) != 1 || plan.Ops[0].Kind != OpCreatePage {
		t.Errorf("ops = %v, want a single create-page", kinds(plan))
	}
	if len(plan.Unmanaged) != 0 {
		t.Errorf("Unmanaged = %v, want empty — another repo's pages are not our orphans", plan.Unmanaged)
	}
}

func TestPlanReportsOrphans(t *testing.T) {
	f := newFake()
	book := f.withBook(testShelf)
	f.withShelf(testShelf, book)
	f.withPage(book.ID, "Kept", "docs/decisions/0001-kept.md", "body\n", 1)
	f.withPage(book.ID, "Removed", "docs/decisions/0002-removed.md", "body\n", 2)
	f.withPage(book.ID, "Also Removed", "docs/decisions/0003-gone.md", "body\n", 3)

	docs := []scan.Document{doc("docs/decisions/0001-kept.md", "Kept", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if len(plan.Unmanaged) != 2 {
		t.Fatalf("Unmanaged = %v, want 2 orphans", plan.Unmanaged)
	}
	// Sorted by source path for stable reporting.
	if plan.Unmanaged[0].SourcePath != "docs/decisions/0002-removed.md" {
		t.Errorf("orphans are not sorted: %v", plan.Unmanaged)
	}

	// v0.1 reports orphans but must not plan deletions.
	for _, op := range plan.Ops {
		if strings.Contains(string(op.Kind), "delete") {
			t.Errorf("planned a delete in v0.1: %v", op)
		}
	}
}

func TestPlanExistingShelfAndBook(t *testing.T) {
	f := newFake()
	book := f.withBook(testShelf)
	f.withShelf(testShelf, book)

	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if !plan.ShelfExists {
		t.Error("ShelfExists = false, want true")
	}
	// Only the page is missing; the shelf already holds the book.
	if got := kinds(plan); !sameKinds(got, OpCreatePage) {
		t.Errorf("ops = %v, want only create-page", got)
	}
}

func TestPlanBookNotOnShelf(t *testing.T) {
	f := newFake()
	book := f.withBook(testShelf)
	// Shelf exists but does not contain the book.
	f.withShelf(testShelf)

	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	var found bool
	for _, op := range plan.Ops {
		if op.Kind == OpUpdateShelfBooks {
			found = true
			if op.BookName != book.Name {
				t.Errorf("BookName = %q, want %q", op.BookName, book.Name)
			}
		}
	}
	if !found {
		t.Errorf("ops = %v, want shelf membership to be updated", kinds(plan))
	}
}

func TestPlanOrdersOperations(t *testing.T) {
	f := newFake()
	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	// A page cannot be created before its book, nor a book shelved before
	// the shelf exists.
	order := map[OpKind]int{}
	for i, op := range plan.Ops {
		if _, seen := order[op.Kind]; !seen {
			order[op.Kind] = i
		}
	}
	if order[OpCreateShelf] > order[OpCreateBook] {
		t.Error("shelf must be created before the book")
	}
	if order[OpCreateBook] > order[OpCreatePage] {
		t.Error("book must be created before its pages")
	}
	if order[OpUpdateShelfBooks] < order[OpCreateBook] {
		t.Error("shelf membership must be set after the book exists")
	}
}

func TestPlanTagsCreatedContent(t *testing.T) {
	f := newFake()
	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	for _, op := range plan.Ops {
		var tags bookstack.Tags
		switch {
		case op.Shelf != nil:
			tags = op.Shelf.Tags
		case op.Book != nil:
			tags = op.Book.Tags
		case op.Page != nil:
			tags = op.Page.Tags
		default:
			continue
		}

		if v, ok := tags.Get(TagManagedBy); !ok || v != ManagedByValue {
			t.Errorf("%s: missing %s tag", op.Kind, TagManagedBy)
		}
		if v, ok := tags.Get(TagRepo); !ok || v != testRepo {
			t.Errorf("%s: missing or wrong %s tag", op.Kind, TagRepo)
		}
		if op.Page != nil {
			if v, ok := tags.Get(TagSource); !ok || v != "docs/decisions/0001-x.md" {
				t.Errorf("page: %s = %q, want the source path", TagSource, v)
			}
		}
	}
}

func TestPlanNoDocuments(t *testing.T) {
	f := newFake()
	book := f.withBook(testShelf)
	f.withShelf(testShelf, book)

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !plan.IsEmpty() {
		t.Errorf("plan = %v, want empty for an empty docs tree", plan.Ops)
	}
}

func TestPlanPropagatesErrors(t *testing.T) {
	sentinel := errors.New("boom")

	tests := []struct {
		name   string
		set    func(*fakeClient)
		detail string
	}{
		{name: "shelves", set: func(f *fakeClient) { f.errListShelves = sentinel }, detail: "listing shelves"},
		{name: "books", set: func(f *fakeClient) { f.errListBooks = sentinel }, detail: "listing books"},
		{name: "search", set: func(f *fakeClient) { f.errSearch = sentinel }, detail: "searching managed pages"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()
			tt.set(f)

			_, err := NewPlanner(f, testConfig()).Plan(context.Background(), nil)
			if !errors.Is(err, sentinel) {
				t.Fatalf("error = %v, want it to wrap the client error", err)
			}
			if !strings.Contains(err.Error(), tt.detail) {
				t.Errorf("error = %v, want it to mention %q", err, tt.detail)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	p := Plan{Ops: []Op{
		{Kind: OpCreatePage}, {Kind: OpCreatePage}, {Kind: OpUpdatePage},
	}}

	got := p.Summary()
	if got[OpCreatePage] != 2 {
		t.Errorf("create-page = %d, want 2", got[OpCreatePage])
	}
	if got[OpUpdatePage] != 1 {
		t.Errorf("update-page = %d, want 1", got[OpUpdatePage])
	}
	if p.Count() != 3 {
		t.Errorf("Count() = %d, want 3", p.Count())
	}
}

func TestNormaliseBody(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		same bool
	}{
		{name: "identical", a: "x\n", b: "x\n", same: true},
		{name: "CRLF versus LF", a: "x\r\ny\n", b: "x\ny\n", same: true},
		{name: "bare CR", a: "x\ry\n", b: "x\ny\n", same: true},
		{name: "trailing newlines", a: "x", b: "x\n\n\n", same: true},
		{name: "trailing spaces", a: "x   \ny\n", b: "x\ny\n", same: true},
		{name: "trailing tabs", a: "x\t\n", b: "x\n", same: true},
		{name: "leading spaces differ", a: "  x\n", b: "x\n", same: false},
		{name: "content differs", a: "x\n", b: "y\n", same: false},
		{name: "blank line in the middle matters", a: "x\n\ny\n", b: "x\ny\n", same: false},
		{name: "both empty", a: "", b: "\n", same: true},

		// Stripping frontmatter leaves a leading newline where it was, and
		// BookStack drops it on store. Without trimming the left side,
		// every document with frontmatter — every MADR record — looks
		// changed on every run. Verified against a live instance; see
		// test/integration/idempotency_test.go.
		{name: "leading newline from stripped frontmatter", a: "\n# T\n\nbody\n", b: "# T\n\nbody", same: true},
		{name: "several leading newlines", a: "\n\n\n# T\n", b: "# T", same: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normaliseBody(tt.a) == normaliseBody(tt.b); got != tt.same {
				t.Errorf("normalise(%q)==normalise(%q) = %t, want %t", tt.a, tt.b, got, tt.same)
			}
		})
	}
}
