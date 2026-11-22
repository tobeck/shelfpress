//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/tobeck/shelfpress/internal/sync"
)

// TestSyncIsIdempotent is the regression that reordered the roadmap.
//
// #22 shipped a tool that rewrote every page on every run: /api/search
// returns tags but no page body, so the planner compared the file against the
// empty string and saw every page as changed. A unit suite at 91% coverage
// passed throughout, because the fake returned bodies the real API never
// sends. Only a live instance can prove this one.
//
// CLAUDE.md rule 5 is the contract: an update op is emitted only when title,
// body, or tags actually differ.
func TestSyncIsIdempotent(t *testing.T) {
	client := newClient(t)

	root := fixture(t, map[string]string{
		"0001-use-quarkus.md": "# Use Quarkus\n\nWe will use Quarkus for the backend.\n",
		"0002-use-kafka.md":   "# Use Kafka\n\nKafka carries events between services.\n",
	})

	cfg := testConfig(t, root)
	cleanupShelf(t, client, cfg)

	first := syncDocs(t, client, cfg)
	if first.IsEmpty() {
		t.Fatal("first sync planned nothing, want a shelf, a book and two pages")
	}

	// Planning again against unchanged files must produce nothing at all.
	second := planDocs(t, client, cfg)
	if !second.IsEmpty() {
		t.Errorf("second plan has %d operations, want 0:\n%s", second.Count(), describeOps(second))
	}

	// "Planned nothing" and "wrote nothing" are different claims. Applying
	// the second plan closes the gap: the applier must issue no writes.
	result, err := sync.NewApplier(client).Apply(testContext(t), second)
	if err != nil {
		t.Fatalf("applying the second (empty) plan: %v", err)
	}
	if result.Count() != 0 {
		t.Errorf("second apply recorded %d writes, want 0: %v", result.Count(), result.Summary())
	}
}

// TestIdempotencyAcrossBodyShapes covers the markdown that a round trip is
// most likely to alter.
//
// BookStack strips the trailing newline from every body it stores but
// otherwise returns the markdown unchanged — interior trailing whitespace,
// CRLF, tabs and fences all survive verbatim. Verified against a live
// instance while writing this test; it is why normaliseBody's final
// TrimRight is the part actually holding idempotency together.
func TestIdempotencyAcrossBodyShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{
			name: "trailing whitespace",
			body: "# Trailing\n\nA line with trailing spaces   \nand a tab\t\n",
		},
		{
			name: "CRLF line endings",
			body: "# Windows\r\n\r\nWritten on Windows.\r\n",
		},
		{
			name: "fenced code block",
			body: "# Fenced\n\n```go\nfunc main() {\n\tprintln(\"hi\")\n}\n```\n",
		},
		{
			name: "YAML frontmatter",
			body: "---\nstatus: accepted\ndate: 2026-11-22\n---\n\n# Frontmatter\n\nBody below the frontmatter.\n",
		},
		{
			name: "no trailing newline",
			body: "# Bare\n\nA body that does not end in a newline.",
		},
		{
			name: "multiple trailing newlines",
			body: "# Padded\n\nA body with several trailing newlines.\n\n\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newClient(t)

			root := fixture(t, map[string]string{"0001-shape.md": tc.body})
			cfg := testConfig(t, root)
			cleanupShelf(t, client, cfg)

			if syncDocs(t, client, cfg).IsEmpty() {
				t.Fatal("first sync planned nothing, want a page")
			}

			second := planDocs(t, client, cfg)
			if !second.IsEmpty() {
				page := findPage(t, client, cfg, "0001-shape.md")
				t.Errorf("re-planning this body produced %d operations, want 0:\n%s\n"+
					"sent     : %q\nstored   : %q",
					second.Count(), describeOps(second), tc.body, page.Markdown)
			}
		})
	}
}

// TestChangedFileProducesExactlyOneUpdate proves the diff is neither too
// eager nor too quiet: editing one file of two updates that page and leaves
// the other alone.
func TestChangedFileProducesExactlyOneUpdate(t *testing.T) {
	client := newClient(t)

	const changed = "0001-use-quarkus.md"
	const untouched = "0002-use-kafka.md"

	files := map[string]string{
		changed:   "# Use Quarkus\n\nWe will use Quarkus for the backend.\n",
		untouched: "# Use Kafka\n\nKafka carries events between services.\n",
	}
	root := fixture(t, files)
	cfg := testConfig(t, root)
	cleanupShelf(t, client, cfg)

	syncDocs(t, client, cfg)

	// Rewrite one file's body, leaving its title and the other file alone.
	rewrite(t, root, changed, "# Use Quarkus\n\nRevised: Quarkus, for its startup time.\n")

	plan := planDocs(t, client, cfg)
	if plan.Count() != 1 {
		t.Fatalf("editing one file planned %d operations, want exactly 1:\n%s",
			plan.Count(), describeOps(plan))
	}

	op := plan.Ops[0]
	if op.Kind != sync.OpUpdatePage {
		t.Errorf("operation kind = %s, want %s", op.Kind, sync.OpUpdatePage)
	}
	if !strings.Contains(op.Detail, "body changed") {
		t.Errorf("operation detail = %q, want it to mention the body changing", op.Detail)
	}

	// Applying it must leave the edited page updated and the other intact.
	if _, err := sync.NewApplier(client).Apply(testContext(t), plan); err != nil {
		t.Fatalf("applying the update: %v", err)
	}

	if got := findPage(t, client, cfg, changed); !strings.Contains(got.Markdown, "startup time") {
		t.Errorf("edited page body = %q, want the revised text", got.Markdown)
	}
	if got := findPage(t, client, cfg, untouched); !strings.Contains(got.Markdown, "carries events") {
		t.Errorf("untouched page body = %q, want the original text", got.Markdown)
	}

	// And the run must settle: no further changes pending.
	if after := planDocs(t, client, cfg); !after.IsEmpty() {
		t.Errorf("plan after applying the update has %d operations, want 0:\n%s",
			after.Count(), describeOps(after))
	}
}

// TestSearchResultsCarryNoBody pins the API behaviour the planner works
// around.
//
// fetchState calls GetPage for every managed page because search results omit
// the body. That extra request looks redundant to anyone who has not read
// #22. If BookStack ever starts returning bodies from search, the person
// removing the call should be told by this test — not by a stranger whose
// shelf got rewritten.
func TestSearchResultsCarryNoBody(t *testing.T) {
	client := newClient(t)

	const source = "0001-searchable.md"
	const marker = "A body that search must not return."

	root := fixture(t, map[string]string{source: "# Searchable\n\n" + marker + "\n"})
	cfg := testConfig(t, root)
	cleanupShelf(t, client, cfg)

	syncDocs(t, client, cfg)

	found, err := client.SearchPagesByTag(testContext(t), sync.TagRepo, cfg.Repo)
	if err != nil {
		t.Fatalf("searching managed pages: %v", err)
	}
	if len(found) == 0 {
		t.Fatalf("search returned no pages for %s=%s", sync.TagRepo, cfg.Repo)
	}

	for _, page := range found {
		if page.Markdown != "" {
			t.Errorf("search returned a page body (%q) — BookStack's behaviour has changed.\n"+
				"The GetPage call in sync.fetchState exists only because search omits it; "+
				"re-read #22 before simplifying anything.", page.Markdown)
		}
		// Tags, by contrast, are the reason search is used at all.
		if _, ok := page.Tags.Get(sync.TagSource); !ok {
			t.Errorf("search returned a page without its %s tag, which is how pages are identified",
				sync.TagSource)
		}
	}

	// The body is reachable via the detail endpoint — the fallback the
	// planner relies on.
	if got := findPage(t, client, cfg, source); !strings.Contains(got.Markdown, marker) {
		t.Errorf("GetPage body = %q, want it to contain %q", got.Markdown, marker)
	}
}
