package bookstack

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// BookStack's list endpoints omit tags from their responses (see
// BookStackApp/BookStack#2680), so a page's sp-source tag cannot be recovered
// by listing. The search endpoint does return tags, and its query language
// supports tag filters, which lets shelfpress find everything it manages in
// one request instead of fetching every page's detail.

// searchResult is one entry in a /api/search response.
type searchResult struct {
	ID     int    `json:"id"`
	BookID int    `json:"book_id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Type   string `json:"type"`
	Tags   Tags   `json:"tags"`
}

// SearchPagesByTag returns pages carrying the named tag, with tags populated.
//
// When value is empty, any page with the tag matches; otherwise the tag value
// must be equal. The returned pages carry no markdown — search responses omit
// body content — so a caller comparing content must fetch the page detail.
func (c *APIClient) SearchPagesByTag(ctx context.Context, name, value string) ([]Page, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("bookstack: search tag name is required")
	}

	query := tagQuery(name, value) + " {type:page}"

	var pages []Page
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("query", query)
		q.Set("count", strconv.Itoa(c.pageSize))
		q.Set("page", strconv.Itoa(page))

		var envelope listEnvelope[searchResult]
		if err := c.do(ctx, http.MethodGet, "/api/search", q, nil, &envelope); err != nil {
			return nil, err
		}

		for _, r := range envelope.Data {
			if r.Type != "" && r.Type != "page" {
				continue
			}
			pages = append(pages, Page{
				ID:     r.ID,
				BookID: r.BookID,
				Name:   r.Name,
				Slug:   r.Slug,
				Tags:   r.Tags,
			})
		}

		if len(envelope.Data) < c.pageSize || len(pages) >= envelope.Total {
			return pages, nil
		}
	}
}

// tagQuery builds BookStack's tag search term, [name] or [name=value].
func tagQuery(name, value string) string {
	if value == "" {
		return "[" + escapeTagTerm(name) + "]"
	}
	return "[" + escapeTagTerm(name) + "=" + escapeTagTerm(value) + "]"
}

// escapeTagTerm removes the characters that would terminate or alter a tag
// term, so a value such as a file path cannot break out of the query.
func escapeTagTerm(s string) string {
	return strings.NewReplacer("[", "", "]", "", "=", "").Replace(s)
}
