package bookstack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capture records what the server received, so tests can assert on the
// request shape rather than only the decoded response.
type capture struct {
	method string
	path   string
	body   map[string]any
}

// recordingServer answers every request with respond and records the request.
func recordingServer(t *testing.T, got *capture, status int, respond any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path

		if data, err := io.ReadAll(r.Body); err == nil && len(data) > 0 {
			got.body = map[string]any{}
			if err := json.Unmarshal(data, &got.body); err != nil {
				t.Errorf("request body is not JSON: %v", err)
			}
		}
		if respond == nil || status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		writeJSON(t, w, status, respond)
	}))
}

func TestCreatePageSendsMarkdown(t *testing.T) {
	var got capture
	srv := recordingServer(t, &got, http.StatusOK, Page{ID: 7, Name: "Use Quarkus"})
	defer srv.Close()

	in := PageInput{
		BookID:   3,
		Name:     "Use Quarkus",
		Markdown: "# Use Quarkus\n\nBody.",
		Priority: 1,
		Tags:     Tags{{Name: "sp-source", Value: "docs/decisions/0001-use-quarkus.md"}},
	}

	page, err := newTestClient(t, srv).CreatePage(context.Background(), in)
	if err != nil {
		t.Fatalf("CreatePage() error = %v", err)
	}
	if page.ID != 7 {
		t.Errorf("ID = %d, want 7", page.ID)
	}

	if got.method != http.MethodPost || got.path != "/api/pages" {
		t.Errorf("request = %s %s, want POST /api/pages", got.method, got.path)
	}
	if _, ok := got.body["markdown"]; !ok {
		t.Error("request body has no markdown field")
	}
	if _, ok := got.body["html"]; ok {
		t.Error("request body contains html; markdown must be sent instead (CLAUDE.md rule 7)")
	}
	if got.body["book_id"] != float64(3) {
		t.Errorf("book_id = %v, want 3", got.body["book_id"])
	}
}

func TestUpdatePage(t *testing.T) {
	var got capture
	srv := recordingServer(t, &got, http.StatusOK, Page{ID: 7})
	defer srv.Close()

	_, err := newTestClient(t, srv).UpdatePage(context.Background(), 7, PageInput{
		Name:     "Updated",
		Markdown: "new body",
	})
	if err != nil {
		t.Fatalf("UpdatePage() error = %v", err)
	}

	if got.method != http.MethodPut || got.path != "/api/pages/7" {
		t.Errorf("request = %s %s, want PUT /api/pages/7", got.method, got.path)
	}
	if got.body["markdown"] != "new body" {
		t.Errorf("markdown = %v", got.body["markdown"])
	}
}

func TestDeleteEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		call     func(*APIClient) error
		wantPath string
	}{
		{
			name:     "page",
			call:     func(c *APIClient) error { return c.DeletePage(context.Background(), 9) },
			wantPath: "/api/pages/9",
		},
		{
			name:     "book",
			call:     func(c *APIClient) error { return c.DeleteBook(context.Background(), 4) },
			wantPath: "/api/books/4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got capture
			srv := recordingServer(t, &got, http.StatusNoContent, nil)
			defer srv.Close()

			if err := tt.call(newTestClient(t, srv)); err != nil {
				t.Fatalf("delete error = %v", err)
			}
			if got.method != http.MethodDelete || got.path != tt.wantPath {
				t.Errorf("request = %s %s, want DELETE %s", got.method, got.path, tt.wantPath)
			}
		})
	}
}

func TestUpdateShelfBooks(t *testing.T) {
	t.Run("sends the full membership list", func(t *testing.T) {
		var got capture
		srv := recordingServer(t, &got, http.StatusOK, Shelf{ID: 1})
		defer srv.Close()

		if err := newTestClient(t, srv).UpdateShelfBooks(context.Background(), 1, []int{3, 4, 5}); err != nil {
			t.Fatalf("UpdateShelfBooks() error = %v", err)
		}

		if got.method != http.MethodPut || got.path != "/api/shelves/1" {
			t.Errorf("request = %s %s, want PUT /api/shelves/1", got.method, got.path)
		}
		books, ok := got.body["books"].([]any)
		if !ok {
			t.Fatalf("books = %T, want an array", got.body["books"])
		}
		if len(books) != 3 {
			t.Errorf("sent %d books, want 3", len(books))
		}
	})

	t.Run("nil clears the shelf", func(t *testing.T) {
		var got capture
		srv := recordingServer(t, &got, http.StatusOK, Shelf{ID: 1})
		defer srv.Close()

		if err := newTestClient(t, srv).UpdateShelfBooks(context.Background(), 1, nil); err != nil {
			t.Fatalf("UpdateShelfBooks() error = %v", err)
		}
		books, ok := got.body["books"].([]any)
		if !ok {
			t.Fatalf("books = %v, want an empty array rather than null", got.body["books"])
		}
		if len(books) != 0 {
			t.Errorf("sent %d books, want 0", len(books))
		}
	})
}

func TestCreateShelfAndBook(t *testing.T) {
	t.Run("shelf", func(t *testing.T) {
		var got capture
		srv := recordingServer(t, &got, http.StatusOK, Shelf{ID: 2, Name: "ADRs"})
		defer srv.Close()

		shelf, err := newTestClient(t, srv).CreateShelf(context.Background(), ShelfInput{
			Name: "ADRs",
			Tags: Tags{{Name: "sp-managed-by", Value: "shelfpress"}},
		})
		if err != nil {
			t.Fatalf("CreateShelf() error = %v", err)
		}
		if shelf.ID != 2 {
			t.Errorf("ID = %d, want 2", shelf.ID)
		}
		if got.path != "/api/shelves" {
			t.Errorf("path = %q", got.path)
		}
		if _, ok := got.body["tags"]; !ok {
			t.Error("tags were not sent")
		}
	})

	t.Run("book", func(t *testing.T) {
		var got capture
		srv := recordingServer(t, &got, http.StatusOK, Book{ID: 5, Name: "Backend"})
		defer srv.Close()

		book, err := newTestClient(t, srv).CreateBook(context.Background(), BookInput{Name: "Backend"})
		if err != nil {
			t.Fatalf("CreateBook() error = %v", err)
		}
		if book.ID != 5 {
			t.Errorf("ID = %d, want 5", book.ID)
		}
		if got.path != "/api/books" {
			t.Errorf("path = %q", got.path)
		}
	})
}

func TestGetPageDecodesTagsAndMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"id":       7,
			"book_id":  3,
			"name":     "Use Quarkus",
			"markdown": "# Use Quarkus",
			"priority": 1,
			"tags": []map[string]any{
				{"name": "sp-source", "value": "docs/decisions/0001-use-quarkus.md", "order": 0},
				{"name": "sp-managed-by", "value": "shelfpress", "order": 1},
			},
		})
	}))
	defer srv.Close()

	page, err := newTestClient(t, srv).GetPage(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}

	if page.Markdown != "# Use Quarkus" {
		t.Errorf("Markdown = %q", page.Markdown)
	}
	source, ok := page.Tags.Get("sp-source")
	if !ok {
		t.Fatal("sp-source tag missing")
	}
	if source != "docs/decisions/0001-use-quarkus.md" {
		t.Errorf("sp-source = %q", source)
	}
}

func TestTagsGet(t *testing.T) {
	tags := Tags{{Name: "sp-source", Value: "a.md"}, {Name: "Status", Value: "accepted"}}

	tests := []struct {
		name      string
		lookup    string
		wantValue string
		wantOK    bool
	}{
		{name: "present", lookup: "sp-source", wantValue: "a.md", wantOK: true},
		{name: "case insensitive", lookup: "status", wantValue: "accepted", wantOK: true},
		{name: "absent", lookup: "nope", wantValue: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tags.Get(tt.lookup)
			if got != tt.wantValue || ok != tt.wantOK {
				t.Errorf("Get(%q) = (%q, %t), want (%q, %t)", tt.lookup, got, ok, tt.wantValue, tt.wantOK)
			}
		})
	}
}

func TestEmptyListIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, listEnvelope[Shelf]{Data: []Shelf{}, Total: 0})
	}))
	defer srv.Close()

	shelves, err := newTestClient(t, srv).ListShelves(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListShelves() error = %v", err)
	}
	if len(shelves) != 0 {
		t.Errorf("got %d shelves, want 0", len(shelves))
	}
}

func TestBaseURLWithSubpath(t *testing.T) {
	var got capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		writeJSON(t, w, http.StatusOK, listEnvelope[Book]{})
	}))
	defer srv.Close()

	c, err := New(srv.URL+"/wiki", testTokenID, testTokenSecret, WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.ListBooks(context.Background(), nil); err != nil {
		t.Fatalf("ListBooks() error = %v", err)
	}
	if got.path != "/wiki/api/books" {
		t.Errorf("path = %q, want %q — a base URL subpath must be preserved", got.path, "/wiki/api/books")
	}
}
