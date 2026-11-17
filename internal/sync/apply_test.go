package sync

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/tobeck/shelfpress/internal/bookstack"
	"github.com/tobeck/shelfpress/internal/scan"
)

func TestApplyCreatesEverythingInOrder(t *testing.T) {
	f := newFake()
	docs := []scan.Document{
		doc("docs/decisions/0001-use-quarkus.md", "Use Quarkus", "# Use Quarkus\n", 1),
		doc("docs/decisions/0002-use-kafka.md", "Use Kafka", "# Use Kafka\n", 2),
	}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	result, err := NewApplier(f).Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if result.Count() != len(plan.Ops) {
		t.Errorf("applied %d operations, want %d", result.Count(), len(plan.Ops))
	}
	if result.Failed != nil {
		t.Errorf("Failed = %v, want nil", result.Failed)
	}
	if len(result.Created) != 2 {
		t.Errorf("Created = %v, want two pages", result.Created)
	}
	for _, source := range []string{"docs/decisions/0001-use-quarkus.md", "docs/decisions/0002-use-kafka.md"} {
		if result.Created[source] == 0 {
			t.Errorf("no ID recorded for %s", source)
		}
	}
}

func TestApplyThreadsCreatedBookID(t *testing.T) {
	f := newFake()
	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if _, err := NewApplier(f).Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// The book did not exist when the plan was made, so the page must have
	// been created against the ID the book got during this run.
	var bookID int
	for _, b := range f.books {
		if b.Name == testShelf {
			bookID = b.ID
		}
	}
	if bookID == 0 {
		t.Fatal("no book was created")
	}

	var found bool
	for _, p := range f.pages {
		if p.Name == "X" {
			found = true
			if p.BookID != bookID {
				t.Errorf("page BookID = %d, want the created book %d", p.BookID, bookID)
			}
		}
	}
	if !found {
		t.Error("page was not created")
	}
}

func TestApplyTagsEverythingItCreates(t *testing.T) {
	f := newFake()
	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if _, err := NewApplier(f).Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	checkManaged := func(kind string, tags bookstack.Tags) {
		t.Helper()
		if v, ok := tags.Get(TagManagedBy); !ok || v != ManagedByValue {
			t.Errorf("%s: %s = %q, want %q", kind, TagManagedBy, v, ManagedByValue)
		}
		if v, ok := tags.Get(TagRepo); !ok || v != testRepo {
			t.Errorf("%s: %s = %q, want %q", kind, TagRepo, v, testRepo)
		}
	}

	for _, s := range f.shelves {
		checkManaged("shelf", s.Tags)
	}
	for _, b := range f.books {
		checkManaged("book", b.Tags)
	}
	for _, p := range f.pages {
		checkManaged("page", p.Tags)
		if v, ok := p.Tags.Get(TagSource); !ok || v != "docs/decisions/0001-x.md" {
			t.Errorf("page: %s = %q, want the source path", TagSource, v)
		}
	}
}

// TestPlanApplyReplanIsStable is the property that matters most: after a sync,
// planning again must produce nothing to do.
func TestPlanApplyReplanIsStable(t *testing.T) {
	f := newFake()
	docs := []scan.Document{
		doc("docs/decisions/0001-use-quarkus.md", "Use Quarkus", "# Use Quarkus\n\nBody.\n", 1),
		doc("docs/decisions/0002-use-kafka.md", "Use Kafka", "# Use Kafka\n\nBody.\n", 2),
	}

	planner := NewPlanner(f, testConfig())

	first, err := planner.Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("first Plan() error = %v", err)
	}
	if _, err := NewApplier(f).Apply(context.Background(), first); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	writesAfterFirst := len(f.writes)

	second, err := planner.Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("second Plan() error = %v", err)
	}
	if !second.IsEmpty() {
		t.Fatalf("second plan is not empty: %v", second.Ops)
	}

	// Applying an empty plan must issue no API calls at all.
	result, err := NewApplier(f).Apply(context.Background(), second)
	if err != nil {
		t.Fatalf("applying the empty plan errored: %v", err)
	}
	if result.Count() != 0 {
		t.Errorf("applied %d operations for an empty plan", result.Count())
	}
	if len(f.writes) != writesAfterFirst {
		t.Errorf("empty plan wrote: %v", f.writes[writesAfterFirst:])
	}
}

func TestApplyUpdatesChangedPage(t *testing.T) {
	f := newFake()
	book := f.withBook(testShelf)
	f.withShelf(testShelf, book)
	existing := f.withPage(book.ID, "Old Title", "docs/decisions/0001-x.md", "old body\n", 1)

	docs := []scan.Document{doc("docs/decisions/0001-x.md", "New Title", "new body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if _, err := NewApplier(f).Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	var updated bookstack.Page
	for _, p := range f.pages {
		if p.ID == existing.ID {
			updated = p
		}
	}
	if updated.Name != "New Title" {
		t.Errorf("Name = %q, want %q", updated.Name, "New Title")
	}
	if updated.Markdown != "new body\n" {
		t.Errorf("Markdown = %q, want the new body", updated.Markdown)
	}
}

func TestApplyEmptyPlanIssuesNoCalls(t *testing.T) {
	f := newFake()

	result, err := NewApplier(f).Apply(context.Background(), Plan{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Count() != 0 {
		t.Errorf("Count() = %d, want 0", result.Count())
	}
	if len(f.writes) != 0 {
		t.Errorf("empty plan performed writes: %v", f.writes)
	}
}

func TestApplyStopsAtFailureAndReportsPartialResult(t *testing.T) {
	f := newFake()
	f.errCreatePage = errors.New("bookstack exploded")

	docs := []scan.Document{
		doc("docs/decisions/0001-a.md", "A", "body\n", 1),
		doc("docs/decisions/0002-b.md", "B", "body\n", 2),
	}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	result, err := NewApplier(f).Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("Apply() = nil error, want the failure surfaced")
	}

	// The shelf and book were created before the failing page.
	if result.Count() == 0 {
		t.Error("Result records nothing; the partial progress is lost")
	}
	if result.Failed == nil {
		t.Fatal("Failed is nil, want the failing operation")
	}
	if result.Failed.Kind != OpCreatePage {
		t.Errorf("Failed.Kind = %q, want %q", result.Failed.Kind, OpCreatePage)
	}
	if !strings.Contains(err.Error(), "create-page") {
		t.Errorf("error = %v, want it to name the failing operation", err)
	}
	if !strings.Contains(err.Error(), "bookstack exploded") {
		t.Errorf("error = %v, want it to wrap the client error", err)
	}
}

func TestApplyRespectsContextCancellation(t *testing.T) {
	f := newFake()
	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := NewApplier(f).Apply(ctx, plan)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if result.Count() != 0 {
		t.Errorf("applied %d operations after cancellation", result.Count())
	}
	if len(f.writes) != 0 {
		t.Errorf("wrote despite cancellation: %v", f.writes)
	}
}

func TestApplyPreservesExistingShelfBooks(t *testing.T) {
	f := newFake()
	// A shelf already holding an unrelated book.
	other := f.withBook("Someone Else's Book")
	f.withShelf(testShelf, other)
	f.withBook(testShelf)

	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if _, err := NewApplier(f).Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Shelf membership is replaced wholesale by the API, so the existing
	// book must be sent back alongside ours or it silently vanishes.
	var sawMembership bool
	for _, w := range f.writes {
		if !strings.HasPrefix(w, "UpdateShelfBooks") {
			continue
		}
		sawMembership = true
		if !strings.Contains(w, strconv.Itoa(other.ID)) {
			t.Errorf("membership update %q dropped the pre-existing book %d", w, other.ID)
		}
	}
	if !sawMembership {
		t.Error("shelf membership was never updated")
	}
}

func TestApplyUnknownOpKind(t *testing.T) {
	f := newFake()
	plan := Plan{Ops: []Op{{Kind: OpKind("teleport"), Target: "nothing"}}}

	_, err := NewApplier(f).Apply(context.Background(), plan)
	if err == nil {
		t.Fatal("Apply() = nil error, want an error for an unknown kind")
	}
	if !strings.Contains(err.Error(), "teleport") {
		t.Errorf("error = %v, want it to name the unknown kind", err)
	}
}

func TestApplyMissingPayload(t *testing.T) {
	tests := []struct {
		name string
		op   Op
	}{
		{name: "shelf without input", op: Op{Kind: OpCreateShelf, Target: "shelf"}},
		{name: "book without input", op: Op{Kind: OpCreateBook, Target: "book"}},
		{name: "page without input", op: Op{Kind: OpCreatePage, Target: "page", BookName: "b"}},
		{name: "update without ID", op: Op{Kind: OpUpdatePage, Target: "page", Page: &bookstack.PageInput{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewApplier(newFake()).Apply(context.Background(), Plan{Ops: []Op{tt.op}})
			if err == nil {
				t.Fatal("Apply() = nil error, want a descriptive failure")
			}
		})
	}
}

func TestApplyResolvesPreexistingBook(t *testing.T) {
	f := newFake()
	book := f.withBook(testShelf)
	f.withShelf(testShelf, book)

	docs := []scan.Document{doc("docs/decisions/0001-x.md", "X", "body\n", 1)}

	plan, err := NewPlanner(f, testConfig()).Plan(context.Background(), docs)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if _, err := NewApplier(f).Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// The book was not created in this run, so it had to be looked up.
	for _, p := range f.pages {
		if p.Name == "X" && p.BookID != book.ID {
			t.Errorf("page BookID = %d, want the existing book %d", p.BookID, book.ID)
		}
	}
}

func TestResultSummary(t *testing.T) {
	r := Result{Applied: []Op{
		{Kind: OpCreatePage}, {Kind: OpCreatePage}, {Kind: OpUpdatePage},
	}}

	got := r.Summary()
	if got[OpCreatePage] != 2 {
		t.Errorf("create-page = %d, want 2", got[OpCreatePage])
	}
	if r.Count() != 3 {
		t.Errorf("Count() = %d, want 3", r.Count())
	}
}
