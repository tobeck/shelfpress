package bookstack

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// listEnvelope is the shape every BookStack list endpoint returns.
type listEnvelope[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}

// listAll pages through a list endpoint until every record has been read.
//
// It stops when a page comes back short or the declared total is reached, and
// guards against a server that keeps returning records so a bad response
// cannot spin forever.
func listAll[T any](ctx context.Context, c *APIClient, path string, f Filter) ([]T, error) {
	var all []T

	for offset := 0; ; {
		q := url.Values{}
		q.Set("count", strconv.Itoa(c.pageSize))
		q.Set("offset", strconv.Itoa(offset))
		for key, value := range f {
			q.Set("filter["+key+"]", value)
		}

		var page listEnvelope[T]
		if err := c.do(ctx, http.MethodGet, path, q, nil, &page); err != nil {
			return nil, err
		}

		all = append(all, page.Data...)

		if len(page.Data) < c.pageSize || len(all) >= page.Total {
			return all, nil
		}
		offset += len(page.Data)
	}
}

// ListShelves returns every shelf matching the filter.
func (c *APIClient) ListShelves(ctx context.Context, f Filter) ([]Shelf, error) {
	return listAll[Shelf](ctx, c, "/api/shelves", f)
}

// GetShelf returns a single shelf, including its books and tags.
func (c *APIClient) GetShelf(ctx context.Context, id int) (Shelf, error) {
	var s Shelf
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/shelves/%d", id), nil, nil, &s)
	return s, err
}

// CreateShelf creates a shelf.
func (c *APIClient) CreateShelf(ctx context.Context, in ShelfInput) (Shelf, error) {
	var s Shelf
	err := c.do(ctx, http.MethodPost, "/api/shelves", nil, in, &s)
	return s, err
}

// UpdateShelfBooks replaces the shelf's book membership.
//
// BookStack sets membership from the shelf side, so this sends the complete
// list rather than a delta. A nil slice is sent as an empty array so that
// clearing a shelf is expressible.
func (c *APIClient) UpdateShelfBooks(ctx context.Context, id int, bookIDs []int) error {
	if bookIDs == nil {
		bookIDs = []int{}
	}
	body := struct {
		Books []int `json:"books"`
	}{Books: bookIDs}

	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/shelves/%d", id), nil, body, nil)
}

// ListBooks returns every book matching the filter.
func (c *APIClient) ListBooks(ctx context.Context, f Filter) ([]Book, error) {
	return listAll[Book](ctx, c, "/api/books", f)
}

// CreateBook creates a book.
func (c *APIClient) CreateBook(ctx context.Context, in BookInput) (Book, error) {
	var b Book
	err := c.do(ctx, http.MethodPost, "/api/books", nil, in, &b)
	return b, err
}

// DeleteBook sends a book to BookStack's recycle bin.
func (c *APIClient) DeleteBook(ctx context.Context, id int) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/books/%d", id), nil, nil, nil)
}

// ListPages returns every page matching the filter.
//
// BookStack omits tags and body content from list responses; use GetPage or
// SearchPagesByTag when those are needed.
func (c *APIClient) ListPages(ctx context.Context, f Filter) ([]Page, error) {
	return listAll[Page](ctx, c, "/api/pages", f)
}

// GetPage returns a single page including its tags and markdown.
func (c *APIClient) GetPage(ctx context.Context, id int) (Page, error) {
	var p Page
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/pages/%d", id), nil, nil, &p)
	return p, err
}

// CreatePage creates a page from markdown.
func (c *APIClient) CreatePage(ctx context.Context, in PageInput) (Page, error) {
	var p Page
	err := c.do(ctx, http.MethodPost, "/api/pages", nil, in, &p)
	return p, err
}

// UpdatePage replaces a page's content.
func (c *APIClient) UpdatePage(ctx context.Context, id int, in PageInput) (Page, error) {
	var p Page
	err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/pages/%d", id), nil, in, &p)
	return p, err
}

// DeletePage sends a page to BookStack's recycle bin.
func (c *APIClient) DeletePage(ctx context.Context, id int) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/pages/%d", id), nil, nil, nil)
}
