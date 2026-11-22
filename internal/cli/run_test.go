package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tobeck/shelfpress/internal/bookstack"
	"github.com/tobeck/shelfpress/internal/config"
)

// stubClient is an in-memory BookStack for exercising the command tree. It
// mirrors the real API's two awkward behaviours: list responses omit tags, and
// search responses omit body content.
type stubClient struct {
	shelves []bookstack.Shelf
	books   []bookstack.Book
	pages   []bookstack.Page

	writes []string
	nextID int

	failCreatePage error
}

var _ bookstack.Client = (*stubClient)(nil)

func newStub() *stubClient { return &stubClient{nextID: 100} }

func (s *stubClient) id() int { s.nextID++; return s.nextID }

// ListShelves omits books and tags, as the real list endpoint does.
func (s *stubClient) ListShelves(context.Context, bookstack.Filter) ([]bookstack.Shelf, error) {
	stripped := make([]bookstack.Shelf, 0, len(s.shelves))
	for _, sh := range s.shelves {
		sh.Books = nil
		sh.Tags = nil
		stripped = append(stripped, sh)
	}
	return stripped, nil
}

// GetShelf returns the full shelf, as the detail endpoint does.
func (s *stubClient) GetShelf(_ context.Context, id int) (bookstack.Shelf, error) {
	for _, sh := range s.shelves {
		if sh.ID == id {
			return sh, nil
		}
	}
	return bookstack.Shelf{}, &bookstack.APIError{StatusCode: 404, Message: "shelf not found"}
}

func (s *stubClient) CreateShelf(_ context.Context, in bookstack.ShelfInput) (bookstack.Shelf, error) {
	s.writes = append(s.writes, "CreateShelf:"+in.Name)
	sh := bookstack.Shelf{ID: s.id(), Name: in.Name, Tags: in.Tags}
	s.shelves = append(s.shelves, sh)
	return sh, nil
}

func (s *stubClient) UpdateShelfBooks(_ context.Context, id int, bookIDs []int) error {
	s.writes = append(s.writes, "UpdateShelfBooks")
	for i := range s.shelves {
		if s.shelves[i].ID != id {
			continue
		}
		s.shelves[i].Books = nil
		for _, bid := range bookIDs {
			for _, b := range s.books {
				if b.ID == bid {
					s.shelves[i].Books = append(s.shelves[i].Books, b)
				}
			}
		}
	}
	return nil
}

func (s *stubClient) ListBooks(context.Context, bookstack.Filter) ([]bookstack.Book, error) {
	return s.books, nil
}

func (s *stubClient) CreateBook(_ context.Context, in bookstack.BookInput) (bookstack.Book, error) {
	s.writes = append(s.writes, "CreateBook:"+in.Name)
	b := bookstack.Book{ID: s.id(), Name: in.Name, Tags: in.Tags}
	s.books = append(s.books, b)
	return b, nil
}

func (s *stubClient) DeleteBook(context.Context, int) error {
	s.writes = append(s.writes, "DeleteBook")
	return nil
}

func (s *stubClient) ListPages(context.Context, bookstack.Filter) ([]bookstack.Page, error) {
	out := make([]bookstack.Page, 0, len(s.pages))
	for _, p := range s.pages {
		out = append(out, bookstack.Page{ID: p.ID, BookID: p.BookID, Name: p.Name})
	}
	return out, nil
}

func (s *stubClient) GetPage(_ context.Context, id int) (bookstack.Page, error) {
	for _, p := range s.pages {
		if p.ID == id {
			return p, nil
		}
	}
	return bookstack.Page{}, &bookstack.APIError{StatusCode: 404, Endpoint: "GET /api/pages"}
}

func (s *stubClient) CreatePage(_ context.Context, in bookstack.PageInput) (bookstack.Page, error) {
	if s.failCreatePage != nil {
		return bookstack.Page{}, s.failCreatePage
	}
	s.writes = append(s.writes, "CreatePage:"+in.Name)
	p := bookstack.Page{
		ID: s.id(), BookID: in.BookID, Name: in.Name,
		Markdown: in.Markdown, Priority: in.Priority, Tags: in.Tags,
	}
	s.pages = append(s.pages, p)
	return p, nil
}

func (s *stubClient) UpdatePage(_ context.Context, id int, in bookstack.PageInput) (bookstack.Page, error) {
	s.writes = append(s.writes, "UpdatePage:"+in.Name)
	for i := range s.pages {
		if s.pages[i].ID == id {
			s.pages[i].Name = in.Name
			s.pages[i].Markdown = in.Markdown
			s.pages[i].Priority = in.Priority
			s.pages[i].Tags = in.Tags
			return s.pages[i], nil
		}
	}
	return bookstack.Page{}, &bookstack.APIError{StatusCode: 404, Endpoint: "PUT /api/pages"}
}

func (s *stubClient) DeletePage(context.Context, int) error {
	s.writes = append(s.writes, "DeletePage")
	return nil
}

func (s *stubClient) SearchPagesByTag(_ context.Context, name, value string) ([]bookstack.Page, error) {
	var out []bookstack.Page
	for _, p := range s.pages {
		got, ok := p.Tags.Get(name)
		if !ok || (value != "" && got != value) {
			continue
		}
		// Search returns tags but no body, as BookStack does.
		out = append(out, bookstack.Page{ID: p.ID, BookID: p.BookID, Name: p.Name, Tags: p.Tags})
	}
	return out, nil
}

// withStub points the commands at a stub client for the duration of a test.
func withStub(t *testing.T, stub *stubClient) {
	t.Helper()
	original := newClient
	newClient = func(config.Config) (bookstack.Client, error) { return stub, nil }
	t.Cleanup(func() { newClient = original })
}

// docsRoot creates a documentation tree and returns its path.
func docsRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "decisions")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("creating root: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return root
}

// run executes the command tree and returns stdout+stderr and the error.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer

	root := newRoot()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)

	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

// baseArgs returns the flags every command needs.
func baseArgs(root string, extra ...string) []string {
	return append([]string{
		"--root", root,
		"--url", "https://wiki.example.com",
		"--token-id", "tid",
		"--token-secret", "tsec",
		"--repo", "acme/docs",
		"--shelf", "Decisions",
	}, extra...)
}

func TestPlanWritesNothing(t *testing.T) {
	stub := newStub()
	withStub(t, stub)

	root := docsRoot(t, map[string]string{
		"0001-use-quarkus.md": "# Use Quarkus\n\nBody.\n",
	})

	out, err := run(t, append([]string{"plan"}, baseArgs(root)...)...)
	if err != nil {
		t.Fatalf("plan error = %v\n%s", err, out)
	}

	if len(stub.writes) != 0 {
		t.Errorf("plan performed writes: %v", stub.writes)
	}
	for _, want := range []string{"Decisions", "create page", "Use Quarkus", "nothing was written"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSyncThenReSyncIsIdempotent(t *testing.T) {
	stub := newStub()
	withStub(t, stub)

	root := docsRoot(t, map[string]string{
		"0001-use-quarkus.md": "# Use Quarkus\n\nBody.\n",
		"0002-use-kafka.md":   "# Use Kafka\n\nBody.\n",
	})

	if _, err := run(t, append([]string{"sync"}, baseArgs(root)...)...); err != nil {
		t.Fatalf("first sync error = %v", err)
	}
	afterFirst := len(stub.writes)
	if afterFirst == 0 {
		t.Fatal("first sync wrote nothing")
	}

	out, err := run(t, append([]string{"sync"}, baseArgs(root)...)...)
	if err != nil {
		t.Fatalf("second sync error = %v\n%s", err, out)
	}

	// The regression this guards: search responses omit body content, so a
	// planner that compares against them sees every page as changed and
	// rewrites the whole shelf on every run.
	if len(stub.writes) != afterFirst {
		t.Errorf("second sync wrote %v", stub.writes[afterFirst:])
	}
	if !strings.Contains(out, "No changes") {
		t.Errorf("second sync did not report a no-op:\n%s", out)
	}
}

func TestSyncDryRunMatchesPlan(t *testing.T) {
	root := docsRoot(t, map[string]string{"0001-x.md": "# X\n\nBody.\n"})

	stubA := newStub()
	withStub(t, stubA)
	planOut, err := run(t, append([]string{"plan"}, baseArgs(root)...)...)
	if err != nil {
		t.Fatalf("plan error = %v", err)
	}

	stubB := newStub()
	withStub(t, stubB)
	dryOut, err := run(t, append([]string{"sync"}, baseArgs(root, "--dry-run")...)...)
	if err != nil {
		t.Fatalf("sync --dry-run error = %v", err)
	}

	if len(stubB.writes) != 0 {
		t.Errorf("--dry-run performed writes: %v", stubB.writes)
	}
	if planOut != dryOut {
		t.Errorf("plan and sync --dry-run differ:\n--- plan ---\n%s\n--- dry-run ---\n%s", planOut, dryOut)
	}
}

func TestDetailedExitcode(t *testing.T) {
	root := docsRoot(t, map[string]string{"0001-x.md": "# X\n\nBody.\n"})

	t.Run("changes pending exits 3", func(t *testing.T) {
		withStub(t, newStub())

		_, err := run(t, append([]string{"plan"}, baseArgs(root, "--detailed-exitcode")...)...)
		if got := ExitCode(err); got != ExitChangesPending {
			t.Errorf("exit code = %d, want %d (err=%v)", got, ExitChangesPending, err)
		}
	})

	t.Run("no changes exits 0", func(t *testing.T) {
		stub := newStub()
		withStub(t, stub)

		if _, err := run(t, append([]string{"sync"}, baseArgs(root)...)...); err != nil {
			t.Fatalf("sync error = %v", err)
		}
		_, err := run(t, append([]string{"plan"}, baseArgs(root, "--detailed-exitcode")...)...)
		if got := ExitCode(err); got != ExitOK {
			t.Errorf("exit code = %d, want %d (err=%v)", got, ExitOK, err)
		}
	})

	t.Run("without the flag, pending changes still exit 0", func(t *testing.T) {
		withStub(t, newStub())

		_, err := run(t, append([]string{"plan"}, baseArgs(root)...)...)
		if got := ExitCode(err); got != ExitOK {
			t.Errorf("exit code = %d, want %d", got, ExitOK)
		}
	})
}

func TestConfigErrorsExitTwo(t *testing.T) {
	root := docsRoot(t, map[string]string{"0001-x.md": "# X\n"})

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing credentials",
			args: []string{"plan", "--root", root},
			want: "BOOKSTACK_URL",
		},
		{
			name: "missing root",
			args: []string{"plan", "--root", filepath.Join(root, "absent"),
				"--url", "https://w.example.com", "--token-id", "a", "--token-secret", "b"},
			want: "does not exist",
		},
		{
			name: "unknown format",
			args: append([]string{"plan"}, baseArgs(root, "--format", "yaml")...),
			want: "unknown format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withStub(t, newStub())

			out, err := run(t, tt.args...)
			if err == nil {
				t.Fatalf("expected an error\n%s", out)
			}
			if got := ExitCode(err); got != ExitConfig {
				t.Errorf("exit code = %d, want %d", got, ExitConfig)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestConfigErrorNamesFlagAndEnvVar(t *testing.T) {
	withStub(t, newStub())
	root := docsRoot(t, map[string]string{"0001-x.md": "# X\n"})

	_, err := run(t, "plan", "--root", root)
	if err == nil {
		t.Fatal("expected a configuration error")
	}
	// A user hitting this in CI needs both the flag and the variable name.
	for _, want := range []string{"--url", "BOOKSTACK_URL", "--token-id", "BOOKSTACK_TOKEN_ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

func TestSyncReportsApplyFailure(t *testing.T) {
	stub := newStub()
	stub.failCreatePage = errors.New("bookstack returned 500")
	withStub(t, stub)

	root := docsRoot(t, map[string]string{"0001-x.md": "# X\n\nBody.\n"})

	out, err := run(t, append([]string{"sync"}, baseArgs(root)...)...)
	if err == nil {
		t.Fatalf("expected the failure to surface\n%s", out)
	}
	if got := ExitCode(err); got != ExitError {
		t.Errorf("exit code = %d, want %d", got, ExitError)
	}
	// The shelf and book were created before the failure; the user needs to
	// know that rather than assume nothing happened.
	if !strings.Contains(out, "Applied") {
		t.Errorf("output does not report partial progress:\n%s", out)
	}
	if !strings.Contains(out, "bookstack returned 500") {
		t.Errorf("output does not carry the cause:\n%s", out)
	}
}

func TestSubdirectoryWarningGoesToStderr(t *testing.T) {
	withStub(t, newStub())

	root := docsRoot(t, map[string]string{"0001-x.md": "# X\n"})
	if err := os.MkdirAll(filepath.Join(root, "backend"), 0o750); err != nil {
		t.Fatalf("creating subdirectory: %v", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"plan"}, baseArgs(root)...))

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("plan error = %v", err)
	}

	// Warnings must not pollute stdout, which a CI job may redirect into a
	// summary file.
	if strings.Contains(stdout.String(), "warning") {
		t.Errorf("warning leaked into stdout:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "backend") {
		t.Errorf("stderr does not name the skipped directory:\n%s", stderr.String())
	}
}

func TestNoSecretsInCommandOutput(t *testing.T) {
	withStub(t, newStub())

	const secret = "SUPER-SECRET-TOKEN"
	root := docsRoot(t, map[string]string{"0001-x.md": "# X\n\nBody.\n"})

	out, err := run(t, "sync", "--root", root,
		"--url", "https://wiki.example.com",
		"--token-id", "tid", "--token-secret", secret,
		"--repo", "acme/docs", "--shelf", "Decisions")
	if err != nil {
		t.Fatalf("sync error = %v", err)
	}
	if strings.Contains(out, secret) {
		t.Errorf("command output leaked the token:\n%s", out)
	}
}

func TestExitCodeMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: ExitOK},
		{name: "plain error", err: errors.New("boom"), want: ExitError},
		{name: "config error", err: configErrorf("bad"), want: ExitConfig},
		{name: "wrapped config error", err: wrapConfig(errors.New("bad")), want: ExitConfig},
		{name: "changes pending", err: changesPending(3), want: ExitChangesPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestHelpMentionsEnvironmentVariables(t *testing.T) {
	out, err := run(t, "plan", "--help")
	if err != nil {
		t.Fatalf("help error = %v", err)
	}
	for _, want := range []string{"BOOKSTACK_URL", "BOOKSTACK_TOKEN_ID", "SHELFPRESS_ROOT"} {
		if !strings.Contains(out, want) {
			t.Errorf("help does not mention %q:\n%s", want, out)
		}
	}
}
