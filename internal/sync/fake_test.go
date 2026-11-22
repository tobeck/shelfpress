package sync

import (
	"context"
	"fmt"

	"github.com/tobeck/shelfpress/internal/bookstack"
)

// fakeClient is an in-memory BookStack used to test the planner and applier
// without a server. It records every write so tests can assert that a no-op
// plan really writes nothing.
type fakeClient struct {
	shelves []bookstack.Shelf
	books   []bookstack.Book
	pages   []bookstack.Page

	// writes counts mutating calls, in call order.
	writes []string

	// nextID assigns identifiers to created objects.
	nextID int

	// err, when set, is returned by the matching method.
	errListShelves error
	errListBooks   error
	errSearch      error
	errCreatePage  error
}

var _ bookstack.Client = (*fakeClient)(nil)

func newFake() *fakeClient { return &fakeClient{nextID: 100} }

func (f *fakeClient) id() int {
	f.nextID++
	return f.nextID
}

func (f *fakeClient) record(format string, args ...any) {
	f.writes = append(f.writes, fmt.Sprintf(format, args...))
}

// withShelf adds an existing shelf and returns it for chaining.
func (f *fakeClient) withShelf(name string, books ...bookstack.Book) *fakeClient {
	f.shelves = append(f.shelves, bookstack.Shelf{
		ID:    f.id(),
		Name:  name,
		Tags:  bookstack.Tags{{Name: TagManagedBy, Value: ManagedByValue}, {Name: TagRepo, Value: testRepo}},
		Books: books,
	})
	return f
}

// withBook adds an existing book and returns it.
func (f *fakeClient) withBook(name string) bookstack.Book {
	b := bookstack.Book{ID: f.id(), Name: name}
	f.books = append(f.books, b)
	return b
}

// withPage adds an existing managed page for the given source path.
func (f *fakeClient) withPage(bookID int, title, source, markdown string, priority int) bookstack.Page {
	p := bookstack.Page{
		ID:       f.id(),
		BookID:   bookID,
		Name:     title,
		Markdown: markdown,
		Priority: priority,
		Tags: bookstack.Tags{
			{Name: TagManagedBy, Value: ManagedByValue},
			{Name: TagRepo, Value: testRepo},
			{Name: TagSource, Value: source},
		},
	}
	f.pages = append(f.pages, p)
	return p
}

// withUntaggedPage adds a page shelfpress does not own.
func (f *fakeClient) withUntaggedPage(bookID int, title string, tags bookstack.Tags) bookstack.Page {
	p := bookstack.Page{ID: f.id(), BookID: bookID, Name: title, Tags: tags}
	f.pages = append(f.pages, p)
	return p
}

// ListShelves mirrors the real API by stripping books and tags from the
// response: /api/shelves omits both, and a fake that returns them lets a
// planner bug through — which is how the #22 class of defect reaches users.
func (f *fakeClient) ListShelves(_ context.Context, _ bookstack.Filter) ([]bookstack.Shelf, error) {
	if f.errListShelves != nil {
		return nil, f.errListShelves
	}

	stripped := make([]bookstack.Shelf, 0, len(f.shelves))
	for _, s := range f.shelves {
		s.Books = nil
		s.Tags = nil
		stripped = append(stripped, s)
	}
	return stripped, nil
}

// GetShelf returns the full shelf, books and tags included, as the detail
// endpoint does.
func (f *fakeClient) GetShelf(_ context.Context, id int) (bookstack.Shelf, error) {
	for _, s := range f.shelves {
		if s.ID == id {
			return s, nil
		}
	}
	return bookstack.Shelf{}, &bookstack.APIError{StatusCode: 404, Message: "shelf not found"}
}

func (f *fakeClient) CreateShelf(_ context.Context, in bookstack.ShelfInput) (bookstack.Shelf, error) {
	f.record("CreateShelf(%s)", in.Name)
	s := bookstack.Shelf{ID: f.id(), Name: in.Name, Description: in.Description, Tags: in.Tags}
	f.shelves = append(f.shelves, s)
	return s, nil
}

func (f *fakeClient) UpdateShelfBooks(_ context.Context, id int, bookIDs []int) error {
	f.record("UpdateShelfBooks(%d, %v)", id, bookIDs)

	// Membership is replaced wholesale, as the real API does, so a later
	// read reflects the write. Without this the fake would make a
	// non-idempotent sync look idempotent.
	for i := range f.shelves {
		if f.shelves[i].ID != id {
			continue
		}
		books := make([]bookstack.Book, 0, len(bookIDs))
		for _, bid := range bookIDs {
			for _, b := range f.books {
				if b.ID == bid {
					books = append(books, b)
					break
				}
			}
		}
		f.shelves[i].Books = books
		return nil
	}
	return &bookstack.APIError{StatusCode: 404, Endpoint: "PUT /api/shelves"}
}

func (f *fakeClient) ListBooks(_ context.Context, _ bookstack.Filter) ([]bookstack.Book, error) {
	return f.books, f.errListBooks
}

func (f *fakeClient) CreateBook(_ context.Context, in bookstack.BookInput) (bookstack.Book, error) {
	f.record("CreateBook(%s)", in.Name)
	b := bookstack.Book{ID: f.id(), Name: in.Name, Tags: in.Tags}
	f.books = append(f.books, b)
	return b, nil
}

func (f *fakeClient) DeleteBook(_ context.Context, id int) error {
	f.record("DeleteBook(%d)", id)
	return nil
}

func (f *fakeClient) ListPages(_ context.Context, _ bookstack.Filter) ([]bookstack.Page, error) {
	// Mirrors the real API: list responses carry no tags and no body.
	out := make([]bookstack.Page, 0, len(f.pages))
	for _, p := range f.pages {
		out = append(out, bookstack.Page{ID: p.ID, BookID: p.BookID, Name: p.Name, Priority: p.Priority})
	}
	return out, nil
}

func (f *fakeClient) GetPage(_ context.Context, id int) (bookstack.Page, error) {
	for _, p := range f.pages {
		if p.ID == id {
			return p, nil
		}
	}
	return bookstack.Page{}, &bookstack.APIError{StatusCode: 404, Endpoint: "GET /api/pages"}
}

func (f *fakeClient) CreatePage(_ context.Context, in bookstack.PageInput) (bookstack.Page, error) {
	if f.errCreatePage != nil {
		return bookstack.Page{}, f.errCreatePage
	}
	f.record("CreatePage(%s, book=%d)", in.Name, in.BookID)
	p := bookstack.Page{
		ID: f.id(), BookID: in.BookID, Name: in.Name,
		Markdown: in.Markdown, Priority: in.Priority, Tags: in.Tags,
	}
	f.pages = append(f.pages, p)
	return p, nil
}

func (f *fakeClient) UpdatePage(_ context.Context, id int, in bookstack.PageInput) (bookstack.Page, error) {
	f.record("UpdatePage(%d, %s)", id, in.Name)
	for i, p := range f.pages {
		if p.ID == id {
			f.pages[i].Name = in.Name
			f.pages[i].Markdown = in.Markdown
			f.pages[i].Priority = in.Priority
			f.pages[i].Tags = in.Tags
			return f.pages[i], nil
		}
	}
	return bookstack.Page{}, &bookstack.APIError{StatusCode: 404, Endpoint: "PUT /api/pages"}
}

func (f *fakeClient) DeletePage(_ context.Context, id int) error {
	f.record("DeletePage(%d)", id)
	return nil
}

// SearchPagesByTag mirrors the real client: it returns pages carrying the tag,
// with their tags populated.
func (f *fakeClient) SearchPagesByTag(_ context.Context, name, value string) ([]bookstack.Page, error) {
	if f.errSearch != nil {
		return nil, f.errSearch
	}
	var out []bookstack.Page
	for _, p := range f.pages {
		got, ok := p.Tags.Get(name)
		if !ok {
			continue
		}
		if value != "" && got != value {
			continue
		}

		// The real search endpoint returns tags but no body content, so the
		// fake must too — otherwise a planner that forgets to fetch the
		// page detail passes here and fails against BookStack.
		out = append(out, bookstack.Page{
			ID: p.ID, BookID: p.BookID, Name: p.Name, Slug: p.Slug, Tags: p.Tags,
		})
	}
	return out, nil
}
