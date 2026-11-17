// Package bookstack is a client for the BookStack REST API.
//
// It knows nothing about markdown, files, or MADR: it speaks only BookStack's
// shelf/book/page hierarchy. The Client interface is the seam the planner and
// applier are tested against.
package bookstack

import (
	"fmt"
	"net/http"
	"strings"
)

// Tag is a BookStack content tag. Order is assigned by BookStack and is only
// populated on responses.
type Tag struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
	Order int    `json:"order,omitempty"`
}

// Tags is a tag collection with lookup helpers.
type Tags []Tag

// Get returns the value of the named tag and whether it was present.
func (t Tags) Get(name string) (string, bool) {
	for _, tag := range t {
		if strings.EqualFold(tag.Name, name) {
			return tag.Value, true
		}
	}
	return "", false
}

// Shelf is a BookStack shelf.
type Shelf struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Tags        Tags   `json:"tags"`
	Books       []Book `json:"books"`
}

// Book is a BookStack book.
type Book struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Tags        Tags   `json:"tags"`
}

// Page is a BookStack page. Markdown is only populated by GetPage and the
// create/update calls; it is absent from list responses.
type Page struct {
	ID       int    `json:"id"`
	BookID   int    `json:"book_id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Priority int    `json:"priority"`
	Markdown string `json:"markdown"`
	HTML     string `json:"html"`
	Tags     Tags   `json:"tags"`
}

// ShelfInput creates or updates a shelf. Books, when non-nil, replaces the
// shelf's book membership.
type ShelfInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Tags        Tags   `json:"tags,omitempty"`
	Books       []int  `json:"books,omitempty"`
}

// BookInput creates or updates a book.
type BookInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Tags        Tags   `json:"tags,omitempty"`
}

// PageInput creates or updates a page.
//
// Markdown is sent rather than HTML so BookStack stores the source form; see
// CLAUDE.md rule 7.
type PageInput struct {
	BookID   int    `json:"book_id,omitempty"`
	Name     string `json:"name"`
	Markdown string `json:"markdown"`
	Tags     Tags   `json:"tags,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

// Filter constrains a list request. Each entry becomes a filter[key]=value
// query parameter.
type Filter map[string]string

// APIError is a non-2xx response from BookStack.
//
// Its message deliberately carries no credentials: see CLAUDE.md rule 6.
type APIError struct {
	StatusCode int
	Message    string
	Endpoint   string
}

// Error implements error.
func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("bookstack: %s returned %d %s",
			e.Endpoint, e.StatusCode, http.StatusText(e.StatusCode))
	}
	return fmt.Sprintf("bookstack: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

// NotFound reports whether the error was a 404.
func (e *APIError) NotFound() bool { return e.StatusCode == http.StatusNotFound }

// Unauthorized reports whether the credentials were rejected.
func (e *APIError) Unauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}
