//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/tobeck/shelfpress/internal/sync"
)

// TestSyncCreatesShelfBookAndPages is the harness's end-to-end proof: a
// markdown tree becomes a shelf, a book, and pages, with the identity tags
// that let a later run recognise its own content.
//
// Every assertion reads back from the live API rather than trusting the
// applier's own Result, because what BookStack *stores* is the thing the next
// run diffs against.
func TestSyncCreatesShelfBookAndPages(t *testing.T) {
	client := newClient(t)

	root := fixture(t, map[string]string{
		"0001-use-quarkus.md": "# Use Quarkus\n\nWe will use Quarkus for the backend.\n",
		"0002-use-kafka.md":   "# Use Kafka\n\nKafka carries events between services.\n",
	})

	cfg := testConfig(t, root)
	cleanupShelf(t, client, cfg)

	plan := syncDocs(t, client, cfg)
	if plan.IsEmpty() {
		t.Fatal("first sync planned no operations, want a shelf, a book and two pages")
	}

	t.Run("shelf exists", func(t *testing.T) {
		shelf := findShelf(t, client, cfg.Shelf)
		requireTag(t, shelf.Tags, sync.TagManagedBy, sync.ManagedByValue)
		requireTag(t, shelf.Tags, sync.TagRepo, cfg.Repo)
	})

	t.Run("book is on the shelf", func(t *testing.T) {
		shelf := findShelf(t, client, cfg.Shelf)
		// v0.1 maps the whole flat directory to one book named for the shelf.
		var found bool
		for _, book := range shelf.Books {
			if book.Name == cfg.Shelf {
				found = true
				break
			}
		}
		if !found {
			names := make([]string, 0, len(shelf.Books))
			for _, book := range shelf.Books {
				names = append(names, book.Name)
			}
			t.Errorf("shelf holds books %v, want one named %q", names, cfg.Shelf)
		}
	})

	t.Run("pages have the expected content and tags", func(t *testing.T) {
		for _, tc := range []struct {
			source   string
			title    string
			body     string
			priority int
		}{
			{"0001-use-quarkus.md", "Use Quarkus", "We will use Quarkus for the backend.", 1},
			{"0002-use-kafka.md", "Use Kafka", "Kafka carries events between services.", 2},
		} {
			t.Run(tc.source, func(t *testing.T) {
				page := findPage(t, client, cfg, tc.source)

				if page.Name != tc.title {
					t.Errorf("title = %q, want %q", page.Name, tc.title)
				}
				if !strings.Contains(page.Markdown, tc.body) {
					t.Errorf("body %q does not contain %q", page.Markdown, tc.body)
				}
				if page.Priority != tc.priority {
					t.Errorf("priority = %d, want %d from the NNNN- prefix", page.Priority, tc.priority)
				}

				requireTag(t, page.Tags, sync.TagSource, tc.source)
				requireTag(t, page.Tags, sync.TagManagedBy, sync.ManagedByValue)
				requireTag(t, page.Tags, sync.TagRepo, cfg.Repo)
			})
		}
	})
}

// TestSyncLeavesUnmanagedContentAlone covers CLAUDE.md rule 3: content
// shelfpress did not create is reported, never modified. A tool that edits
// hand-written pages is one nobody can safely point at a real wiki.
func TestSyncLeavesUnmanagedContentAlone(t *testing.T) {
	client := newClient(t)
	ctx := testContext(t)

	// A book and page created outside shelfpress, carrying none of its tags.
	book, err := client.CreateBook(ctx, bookInput("unmanaged "+uniqueSuffix(t)))
	if err != nil {
		t.Fatalf("creating an unmanaged book: %v", err)
	}
	t.Cleanup(func() {
		if err := client.DeleteBook(testContext(t), book.ID); err != nil && !isNotFound(err) {
			t.Logf("cleanup: deleting unmanaged book %d: %v", book.ID, err)
		}
	})

	page, err := client.CreatePage(ctx, pageInput(book.ID, "Hand written", "Written by a person.\n"))
	if err != nil {
		t.Fatalf("creating an unmanaged page: %v", err)
	}

	before, err := client.GetPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("reading the unmanaged page: %v", err)
	}

	// Sync an unrelated fixture through the same instance.
	root := fixture(t, map[string]string{
		"0001-managed.md": "# Managed\n\nCreated by shelfpress.\n",
	})
	cfg := testConfig(t, root)
	cleanupShelf(t, client, cfg)
	syncDocs(t, client, cfg)

	after, err := client.GetPage(testContext(t), page.ID)
	if err != nil {
		t.Fatalf("re-reading the unmanaged page: %v", err)
	}

	if after.Name != before.Name {
		t.Errorf("unmanaged page title changed: %q -> %q", before.Name, after.Name)
	}
	if after.Markdown != before.Markdown {
		t.Errorf("unmanaged page body changed:\nbefore: %q\nafter:  %q", before.Markdown, after.Markdown)
	}
	if len(after.Tags) != len(before.Tags) {
		t.Errorf("unmanaged page tags changed: %v -> %v", before.Tags, after.Tags)
	}
}
