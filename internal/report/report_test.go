package report

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/tobeck/shelfpress/internal/bookstack"
	"github.com/tobeck/shelfpress/internal/config"
	"github.com/tobeck/shelfpress/internal/sync"
)

// renderPlan renders to a buffer and returns the text.
func renderPlan(t *testing.T, p sync.Plan, opts PlanOptions) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Plan(&buf, p, opts); err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	return buf.String()
}

// renderResult renders a result to a buffer and returns the text.
func renderResult(t *testing.T, r sync.Result, failure error) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Result(&buf, r, failure); err != nil {
		t.Fatalf("Result() error = %v", err)
	}
	return buf.String()
}

func mixedPlan() sync.Plan {
	return sync.Plan{
		ShelfName:   "Architecture Decisions",
		ShelfExists: true,
		Ops: []sync.Op{
			{Kind: sync.OpCreateBook, Target: `"Backend"`},
			{Kind: sync.OpCreatePage, Target: `"Use Kafka"`, Detail: "from docs/decisions/0002-use-kafka.md"},
			{Kind: sync.OpUpdatePage, Target: `"Use Quarkus"`, Detail: "body changed"},
		},
	}
}

func TestPlanRendersEachOperation(t *testing.T) {
	out := renderPlan(t, mixedPlan(), PlanOptions{})

	for _, want := range []string{
		"Architecture Decisions",
		"exists",
		"create book",
		`"Backend"`,
		"create page",
		`"Use Kafka"`,
		"docs/decisions/0002-use-kafka.md",
		"update page",
		"body changed",
		"3 changes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPlanEmptyIsExplicit(t *testing.T) {
	out := renderPlan(t, sync.Plan{ShelfName: "ADRs", ShelfExists: true}, PlanOptions{})

	if strings.TrimSpace(out) == "" {
		t.Fatal("empty plan rendered nothing; a user would not know it ran")
	}
	if !strings.Contains(out, "No changes") {
		t.Errorf("output does not say there are no changes:\n%s", out)
	}
	if !strings.Contains(out, "ADRs") {
		t.Errorf("output does not name the shelf:\n%s", out)
	}
}

func TestPlanShelfState(t *testing.T) {
	tests := []struct {
		name   string
		exists bool
		want   string
	}{
		{name: "existing shelf", exists: true, want: "exists"},
		{name: "new shelf", exists: false, want: "will be created"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderPlan(t, sync.Plan{ShelfName: "ADRs", ShelfExists: tt.exists}, PlanOptions{})
			if !strings.Contains(out, tt.want) {
				t.Errorf("output = %q, want it to say %q", out, tt.want)
			}
		})
	}
}

func TestPlanDryRunSaysNothingWasWritten(t *testing.T) {
	t.Run("dry run", func(t *testing.T) {
		out := renderPlan(t, mixedPlan(), PlanOptions{DryRun: true})
		if !strings.Contains(out, "nothing was written") {
			t.Errorf("dry run output does not say nothing was written:\n%s", out)
		}
	})

	t.Run("not a dry run", func(t *testing.T) {
		out := renderPlan(t, mixedPlan(), PlanOptions{})
		if strings.Contains(out, "nothing was written") {
			t.Errorf("non-dry-run output claims nothing was written:\n%s", out)
		}
	})
}

func TestPlanReportsOrphans(t *testing.T) {
	p := mixedPlan()
	p.Unmanaged = []sync.Orphan{
		{PageID: 7, Title: "Use Redis", SourcePath: "docs/decisions/0003-use-redis.md"},
	}

	out := renderPlan(t, p, PlanOptions{})

	for _, want := range []string{"Use Redis", "docs/decisions/0003-use-redis.md", "Left in place", "v0.2"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPlanOrphansShownWhenNoOtherChanges(t *testing.T) {
	p := sync.Plan{
		ShelfName:   "ADRs",
		ShelfExists: true,
		Unmanaged: []sync.Orphan{
			{PageID: 7, Title: "Gone", SourcePath: "docs/decisions/0009-gone.md"},
		},
	}

	out := renderPlan(t, p, PlanOptions{})
	if !strings.Contains(out, "No changes") {
		t.Errorf("output should still say there are no changes:\n%s", out)
	}
	if !strings.Contains(out, "Gone") {
		t.Errorf("orphans must be reported even when the plan is empty:\n%s", out)
	}
}

func TestPlanIsDeterministic(t *testing.T) {
	p := mixedPlan()
	p.Unmanaged = []sync.Orphan{
		{Title: "A", SourcePath: "a.md"},
		{Title: "B", SourcePath: "b.md"},
	}

	first := renderPlan(t, p, PlanOptions{})
	// Rendering repeatedly must not vary: the summary iterates a map, so a
	// naive implementation would reorder between runs.
	for i := 0; i < 50; i++ {
		if got := renderPlan(t, p, PlanOptions{}); got != first {
			t.Fatalf("output differs between renders:\n%s\n---\n%s", first, got)
		}
	}
}

func TestSummaryCountsInFixedOrder(t *testing.T) {
	p := sync.Plan{
		ShelfName: "ADRs",
		Ops: []sync.Op{
			{Kind: sync.OpUpdatePage, Target: "page a"},
			{Kind: sync.OpCreatePage, Target: "page b"},
			{Kind: sync.OpCreatePage, Target: "page c"},
			{Kind: sync.OpCreateBook, Target: "book d"},
		},
	}

	out := renderPlan(t, p, PlanOptions{})

	if !strings.Contains(out, "4 changes") {
		t.Errorf("output missing the total:\n%s", out)
	}
	// Books before pages, creates before updates, regardless of op order.
	bookAt := strings.Index(out, "1 create book")
	createAt := strings.Index(out, "2 create page")
	updateAt := strings.Index(out, "1 update page")
	if bookAt < 0 || createAt < 0 || updateAt < 0 {
		t.Fatalf("summary is missing counts:\n%s", out)
	}
	if bookAt >= createAt || createAt >= updateAt {
		t.Errorf("summary is not in the fixed order:\n%s", out)
	}
}

func TestSummaryIncludesUnknownKinds(t *testing.T) {
	p := sync.Plan{
		ShelfName: "ADRs",
		Ops:       []sync.Op{{Kind: sync.OpKind("delete-page"), Target: "page x"}},
	}

	out := renderPlan(t, p, PlanOptions{})
	// A kind added later must not vanish from the summary just because the
	// renderer has not been taught about it.
	if !strings.Contains(out, "delete-page") {
		t.Errorf("unknown kind was dropped from the output:\n%s", out)
	}
}

func TestPlanSingularWording(t *testing.T) {
	p := sync.Plan{
		ShelfName: "ADRs",
		Ops:       []sync.Op{{Kind: sync.OpCreatePage, Target: "page a"}},
	}

	out := renderPlan(t, p, PlanOptions{})
	if !strings.Contains(out, "1 change ") && !strings.Contains(out, "1 change(") {
		t.Errorf("want singular wording for one change:\n%s", out)
	}
	if strings.Contains(out, "1 changes") {
		t.Errorf("output says %q:\n%s", "1 changes", out)
	}
}

func TestResultRendersApplied(t *testing.T) {
	r := sync.Result{
		Applied: []sync.Op{
			{Kind: sync.OpCreateBook, Target: `"Backend"`},
			{Kind: sync.OpCreatePage, Target: `"Use Kafka"`},
		},
	}

	out := renderResult(t, r, nil)

	for _, want := range []string{"Applied", "create book", `"Use Kafka"`, "2 changes"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestResultNoChanges(t *testing.T) {
	out := renderResult(t, sync.Result{}, nil)

	if strings.TrimSpace(out) == "" {
		t.Fatal("rendered nothing for an empty result")
	}
	if !strings.Contains(out, "No changes") {
		t.Errorf("output does not say there were no changes:\n%s", out)
	}
}

func TestResultFailure(t *testing.T) {
	failing := sync.Op{Kind: sync.OpCreatePage, Target: `"Use Kafka"`}
	r := sync.Result{
		Applied: []sync.Op{{Kind: sync.OpCreateBook, Target: `"Backend"`}},
		Failed:  &failing,
	}

	out := renderResult(t, r, errors.New("bookstack returned 500"))

	for _, want := range []string{
		"create book",            // what succeeded
		"Failed at create page",  // where it stopped
		"bookstack returned 500", // why
		"Earlier changes were kept",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestResultFailureWithNothingApplied(t *testing.T) {
	out := renderResult(t, sync.Result{}, errors.New("unauthorized"))

	if !strings.Contains(out, "unauthorized") {
		t.Errorf("output missing the error:\n%s", out)
	}
	// Nothing was applied, so the resume hint would be misleading.
	if strings.Contains(out, "Earlier changes were kept") {
		t.Errorf("output claims earlier changes were kept when none were:\n%s", out)
	}
}

// TestNoSecretsInOutput is the rule-6 guard: a report must never carry a token,
// however the plan was built.
func TestNoSecretsInOutput(t *testing.T) {
	const (
		tokenID     = "SECRET-TOKEN-ID"
		tokenSecret = "SECRET-TOKEN-VALUE"
	)

	cfg := config.Config{
		Shelf: "ADRs",
		Repo:  "tobeck/shelfpress",
		BookStack: config.BookStack{
			URL:         "https://wiki.example.com",
			TokenID:     tokenID,
			TokenSecret: tokenSecret,
		},
	}

	// A plan carrying tags and inputs, as a real one does.
	p := sync.Plan{
		ShelfName:   cfg.Shelf,
		ShelfExists: false,
		Ops: []sync.Op{
			{
				Kind:   sync.OpCreateShelf,
				Target: `"ADRs"`,
				Shelf: &bookstack.ShelfInput{
					Name: cfg.Shelf,
					Tags: bookstack.Tags{{Name: "sp-repo", Value: cfg.Repo}},
				},
			},
			{
				Kind:   sync.OpCreatePage,
				Target: `"X"`,
				Page: &bookstack.PageInput{
					Name:     "X",
					Markdown: "body",
					Tags:     bookstack.Tags{{Name: "sp-source", Value: "docs/decisions/0001-x.md"}},
				},
			},
		},
		Unmanaged: []sync.Orphan{{Title: "Gone", SourcePath: "docs/decisions/0009-gone.md"}},
	}

	rendered := renderPlan(t, p, PlanOptions{}) +
		renderResult(t, sync.Result{Applied: p.Ops}, errors.New("failed")) +
		renderResult(t, sync.Result{}, errors.New(cfg.Redacted().String()))

	for _, secret := range []string{tokenID, tokenSecret} {
		if strings.Contains(rendered, secret) {
			t.Errorf("report leaked %q:\n%s", secret, rendered)
		}
	}
}

func TestWriterErrorIsReturned(t *testing.T) {
	if err := Plan(failingWriter{}, mixedPlan(), PlanOptions{}); err == nil {
		t.Error("Plan() = nil error, want the writer failure surfaced")
	}
	if err := Result(failingWriter{}, sync.Result{Applied: mixedPlan().Ops}, nil); err == nil {
		t.Error("Result() = nil error, want the writer failure surfaced")
	}
}

// failingWriter always fails, standing in for a closed pipe or full disk.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
