//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobeck/shelfpress/internal/bookstack"
	"github.com/tobeck/shelfpress/internal/config"
	"github.com/tobeck/shelfpress/internal/scan"
	"github.com/tobeck/shelfpress/internal/sync"
)

// newClient returns a client for the configured instance, skipping the test
// when none is configured. Every test obtains its client here, so this is the
// single gate that keeps an unconfigured run from attempting any requests.
func newClient(t *testing.T) *bookstack.APIClient {
	t.Helper()

	requireInstance(t)

	client, err := bookstack.New(
		os.Getenv(envURL),
		os.Getenv(envTokenID),
		os.Getenv(envTokenSecret),
	)
	if err != nil {
		t.Fatalf("building client for %s: %v", os.Getenv(envURL), err)
	}
	return client
}

// testContext returns a context with a deadline, so a hung request fails the
// test instead of stalling the suite until the Go test timeout.
func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// testConfig returns a Config scoped to this test alone.
//
// The shelf name and repo both carry a random suffix so concurrent or
// repeated runs cannot collide, and so one test's content is never visible to
// another's planner — the sp-repo tag is what scopes a sync, so a shared repo
// value would make tests see each other's pages.
func testConfig(t *testing.T, root string) config.Config {
	t.Helper()

	id := uniqueSuffix(t)
	return config.Config{
		Root:  root,
		Shelf: "shelfpress-test " + id,
		Repo:  "shelfpress/test-" + id,
		BookStack: config.BookStack{
			URL:         os.Getenv(envURL),
			TokenID:     os.Getenv(envTokenID),
			TokenSecret: os.Getenv(envTokenSecret),
		},
	}
}

// uniqueSuffix builds a short identifier from the test name plus randomness.
// The test name makes leftover content traceable to the test that made it;
// the random part keeps repeated runs from colliding.
func uniqueSuffix(t *testing.T) string {
	t.Helper()

	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("generating a unique suffix: %v", err)
	}

	// Subtest names contain slashes, which BookStack would fold into the
	// slug; keep the name readable but harmless.
	name := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	return name + "-" + hex.EncodeToString(b[:])
}

// syncDocs runs the real pipeline — scan, plan, apply — exactly as the CLI
// does, and returns the plan that was applied.
//
// Tests drive the assembled library rather than the built binary: the bug this
// milestone exists to catch lived in the planner, and shelling out to
// dist/shelfpress would add process plumbing without adding coverage.
func syncDocs(t *testing.T, client bookstack.Client, cfg config.Config) sync.Plan {
	t.Helper()

	plan := planDocs(t, client, cfg)
	if plan.IsEmpty() {
		return plan
	}

	if _, err := sync.NewApplier(client).Apply(testContext(t), plan); err != nil {
		t.Fatalf("applying plan: %v", err)
	}
	return plan
}

// planDocs scans and plans without applying, for tests asserting on what
// would happen — notably the idempotency checks, where the whole point is
// that the second plan is empty.
func planDocs(t *testing.T, client bookstack.Client, cfg config.Config) sync.Plan {
	t.Helper()

	docs, err := scan.Walk(scan.Options{
		Root:  cfg.Root,
		Base:  cfg.Root,
		Warnf: func(format string, args ...any) { t.Logf("scan: "+format, args...) },
	})
	if err != nil {
		t.Fatalf("scanning %s: %v", cfg.Root, err)
	}

	plan, err := sync.NewPlanner(client, cfg).Plan(testContext(t), docs)
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	return plan
}

// fixture writes a markdown tree into a temporary directory and returns its
// path. Keys are file names, values file contents.
func fixture(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if dir := filepath.Dir(path); dir != root {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatalf("creating fixture directory %s: %v", dir, err)
			}
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("writing fixture %s: %v", name, err)
		}
	}
	return root
}

// cleanupShelf removes everything the test created, registered so that it
// runs even when the test fails — a failing run must not leave content that
// makes the next run fail for a different reason.
//
// BookStack's deletes are soft (recycle bin), which is fine: the dev instance
// is disposable and `make dev-clean` is the hard reset.
func cleanupShelf(t *testing.T, client bookstack.Client, cfg config.Config) {
	t.Helper()

	t.Cleanup(func() {
		// A fresh context: the test's own may already be cancelled, and
		// cleanup still has to run.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Pages first, then the books holding them, so a book is empty
		// before it is removed.
		pages, err := client.SearchPagesByTag(ctx, sync.TagRepo, cfg.Repo)
		if err != nil {
			t.Logf("cleanup: searching pages for %s: %v", cfg.Repo, err)
		}
		for _, page := range pages {
			if err := client.DeletePage(ctx, page.ID); err != nil {
				t.Logf("cleanup: deleting page %d: %v", page.ID, err)
			}
		}

		books, err := client.ListBooks(ctx, nil)
		if err != nil {
			t.Logf("cleanup: listing books: %v", err)
		}
		for _, book := range books {
			if book.Name == cfg.Shelf {
				if err := client.DeleteBook(ctx, book.ID); err != nil {
					t.Logf("cleanup: deleting book %d: %v", book.ID, err)
				}
			}
		}

		// Shelves have no delete in the Client interface (v0.1 never needs
		// one), so the test shelf is left behind. It is uniquely named and
		// harmless on a disposable instance; `make dev-clean` clears it.
		t.Logf("cleanup: leaving shelf %q — no shelf delete in the client yet", cfg.Shelf)
	})
}

// findPage returns the managed page for a source path, failing the test when
// it is absent.
func findPage(t *testing.T, client bookstack.Client, cfg config.Config, source string) bookstack.Page {
	t.Helper()

	pages, err := client.SearchPagesByTag(testContext(t), sync.TagRepo, cfg.Repo)
	if err != nil {
		t.Fatalf("searching managed pages: %v", err)
	}

	for _, page := range pages {
		if value, ok := page.Tags.Get(sync.TagSource); ok && value == source {
			// Search results omit the body, so the detail has to be
			// fetched before anything can assert on content. This is the
			// API behaviour that caused #22.
			full, err := client.GetPage(testContext(t), page.ID)
			if err != nil {
				t.Fatalf("reading page %d (%s): %v", page.ID, source, err)
			}
			full.Tags = page.Tags
			return full
		}
	}

	t.Fatalf("no managed page found with %s=%s (searched %d pages for repo %s)",
		sync.TagSource, source, len(pages), cfg.Repo)
	return bookstack.Page{}
}

// findShelf returns the shelf by name, failing the test when it is absent.
//
// The shelf is located with a list call but returned from its detail
// endpoint, because /api/shelves omits both `tags` and `books` — the same
// shape of omission that caused #22 for pages. Asserting on a shelf straight
// out of a list response sees no tags and an empty book list, and concludes a
// working sync is broken. (It did, while this harness was being written.)
func findShelf(t *testing.T, client *bookstack.APIClient, name string) bookstack.Shelf {
	t.Helper()

	shelves, err := client.ListShelves(testContext(t), nil)
	if err != nil {
		t.Fatalf("listing shelves: %v", err)
	}
	for _, shelf := range shelves {
		if shelf.Name == name {
			detail, err := client.GetShelf(testContext(t), shelf.ID)
			if err != nil {
				t.Fatalf("reading shelf %d (%s): %v", shelf.ID, name, err)
			}
			return detail
		}
	}

	t.Fatalf("no shelf named %q among %d shelves", name, len(shelves))
	return bookstack.Shelf{}
}

// requireTag asserts a tag is present with the expected value.
func requireTag(t *testing.T, tags bookstack.Tags, name, want string) {
	t.Helper()

	got, ok := tags.Get(name)
	if !ok {
		t.Errorf("tag %s is missing, want %q", name, want)
		return
	}
	if got != want {
		t.Errorf("tag %s = %q, want %q", name, got, want)
	}
}

// describeOps renders a plan's operations for a failure message.
//
// "want 0 ops, got 7" sends the next person to a debugger; naming each
// operation and its target usually identifies the cause on sight.
func describeOps(plan sync.Plan) string {
	if plan.IsEmpty() {
		return "  (none)"
	}

	var b strings.Builder
	for _, op := range plan.Ops {
		fmt.Fprintf(&b, "  %-18s %s", op.Kind, op.Target)
		if op.Detail != "" {
			fmt.Fprintf(&b, " — %s", op.Detail)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// rewrite replaces a fixture file's contents in place, for tests that need a
// second sync to see an edit.
func rewrite(t *testing.T, root, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatalf("rewriting fixture %s: %v", name, err)
	}
}

// bookInput builds a book carrying none of shelfpress's tags, for tests that
// need content the tool must not touch.
func bookInput(name string) bookstack.BookInput {
	return bookstack.BookInput{Name: name, Description: "created outside shelfpress"}
}

// pageInput builds an untagged page in the given book.
func pageInput(bookID int, name, markdown string) bookstack.PageInput {
	return bookstack.PageInput{BookID: bookID, Name: name, Markdown: markdown}
}

// isNotFound reports whether an error is BookStack's 404, so cleanup can
// ignore content a test already removed.
func isNotFound(err error) bool {
	var apiErr *bookstack.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == 404
}
