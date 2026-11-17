package bookstack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTagQuery(t *testing.T) {
	tests := []struct {
		name     string
		tagName  string
		tagValue string
		want     string
	}{
		{name: "name only", tagName: "sp-managed-by", want: "[sp-managed-by]"},
		{name: "name and value", tagName: "sp-repo", tagValue: "tobeck/shelfpress", want: "[sp-repo=tobeck/shelfpress]"},
		{
			name:     "brackets in value are stripped",
			tagName:  "sp-source",
			tagValue: "docs/[weird]/a.md",
			want:     "[sp-source=docs/weird/a.md]",
		},
		{
			name:     "equals in value is stripped",
			tagName:  "sp-source",
			tagValue: "a=b.md",
			want:     "[sp-source=ab.md]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tagQuery(tt.tagName, tt.tagValue); got != tt.want {
				t.Errorf("tagQuery(%q, %q) = %q, want %q", tt.tagName, tt.tagValue, got, tt.want)
			}
		})
	}
}

func TestSearchPagesByTag(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		writeJSON(t, w, http.StatusOK, listEnvelope[searchResult]{
			Data: []searchResult{
				{
					ID: 7, BookID: 3, Name: "Use Quarkus", Type: "page",
					Tags: Tags{
						{Name: "sp-source", Value: "docs/decisions/0001-use-quarkus.md"},
						{Name: "sp-repo", Value: "tobeck/shelfpress"},
					},
				},
			},
			Total: 1,
		})
	}))
	defer srv.Close()

	pages, err := newTestClient(t, srv).SearchPagesByTag(context.Background(), "sp-repo", "tobeck/shelfpress")
	if err != nil {
		t.Fatalf("SearchPagesByTag() error = %v", err)
	}

	if !strings.Contains(gotQuery, "[sp-repo=tobeck/shelfpress]") {
		t.Errorf("query = %q, want it to carry the tag term", gotQuery)
	}
	if !strings.Contains(gotQuery, "{type:page}") {
		t.Errorf("query = %q, want it restricted to pages", gotQuery)
	}

	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1", len(pages))
	}
	// The tags the list endpoints omit must survive the search path.
	source, ok := pages[0].Tags.Get("sp-source")
	if !ok || source != "docs/decisions/0001-use-quarkus.md" {
		t.Errorf("sp-source = %q (present: %t)", source, ok)
	}
	if pages[0].BookID != 3 {
		t.Errorf("BookID = %d, want 3", pages[0].BookID)
	}
}

func TestSearchPagesByTagPaginates(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		if got := r.URL.Query().Get("page"); got == "" {
			t.Error("search request carried no page parameter")
		}

		if n == 1 {
			writeJSON(t, w, http.StatusOK, listEnvelope[searchResult]{
				Data:  []searchResult{{ID: 1, Type: "page"}, {ID: 2, Type: "page"}},
				Total: 3,
			})
			return
		}
		writeJSON(t, w, http.StatusOK, listEnvelope[searchResult]{
			Data:  []searchResult{{ID: 3, Type: "page"}},
			Total: 3,
		})
	}))
	defer srv.Close()

	pages, err := newTestClient(t, srv, WithPageSize(2)).
		SearchPagesByTag(context.Background(), "sp-managed-by", "shelfpress")
	if err != nil {
		t.Fatalf("SearchPagesByTag() error = %v", err)
	}
	if len(pages) != 3 {
		t.Errorf("got %d pages, want 3", len(pages))
	}
	if n := atomic.LoadInt32(&requests); n != 2 {
		t.Errorf("made %d requests, want 2", n)
	}
}

func TestSearchFiltersNonPageResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, listEnvelope[searchResult]{
			Data: []searchResult{
				{ID: 1, Type: "page"},
				{ID: 2, Type: "book"},
				{ID: 3, Type: "chapter"},
			},
			Total: 3,
		})
	}))
	defer srv.Close()

	pages, err := newTestClient(t, srv).SearchPagesByTag(context.Background(), "sp-managed-by", "")
	if err != nil {
		t.Fatalf("SearchPagesByTag() error = %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("got %d results, want only the page", len(pages))
	}
	if pages[0].ID != 1 {
		t.Errorf("ID = %d, want 1", pages[0].ID)
	}
}

func TestSearchRequiresTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("server should not have been called")
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv).SearchPagesByTag(context.Background(), "  ", "x"); err == nil {
		t.Fatal("SearchPagesByTag() = nil error, want an error for an empty tag name")
	}
}

func TestSearchEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, listEnvelope[searchResult]{Data: []searchResult{}, Total: 0})
	}))
	defer srv.Close()

	pages, err := newTestClient(t, srv).SearchPagesByTag(context.Background(), "sp-managed-by", "shelfpress")
	if err != nil {
		t.Fatalf("SearchPagesByTag() error = %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("got %d pages, want 0", len(pages))
	}
}
