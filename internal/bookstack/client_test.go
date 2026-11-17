package bookstack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testTokenID     = "test-token-id"
	testTokenSecret = "test-token-secret"
)

// newTestClient returns a client pointed at srv with retries sped up so the
// tests do not sleep.
func newTestClient(t *testing.T, srv *httptest.Server, opts ...Option) *APIClient {
	t.Helper()

	base := []Option{
		WithHTTPClient(srv.Client()),
		WithBackoff(func(int) time.Duration { return time.Millisecond }),
	}
	c, err := New(srv.URL, testTokenID, testTokenSecret, append(base, opts...)...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encoding response: %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	tests := []struct {
		name        string
		baseURL     string
		tokenID     string
		tokenSecret string
		wantErr     string
	}{
		{name: "valid", baseURL: "https://wiki.example.com", tokenID: "id", tokenSecret: "secret"},
		{name: "trailing slash is trimmed", baseURL: "https://wiki.example.com/", tokenID: "id", tokenSecret: "secret"},
		{name: "empty url", tokenID: "id", tokenSecret: "secret", wantErr: "base URL is required"},
		{name: "no scheme", baseURL: "wiki.example.com", tokenID: "id", tokenSecret: "secret", wantErr: "scheme and host"},
		{name: "missing token id", baseURL: "https://wiki.example.com", tokenSecret: "secret", wantErr: "token ID and secret"},
		{name: "missing secret", baseURL: "https://wiki.example.com", tokenID: "id", wantErr: "token ID and secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.baseURL, tt.tokenID, tt.tokenSecret)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("New() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("New() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestAuthHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		writeJSON(t, w, http.StatusOK, listEnvelope[Book]{Data: []Book{}, Total: 0})
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv).ListBooks(context.Background(), nil); err != nil {
		t.Fatalf("ListBooks() error = %v", err)
	}

	want := "Token " + testTokenID + ":" + testTokenSecret
	if got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestListPagination(t *testing.T) {
	const total = 7
	var requests int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)

		offset := r.URL.Query().Get("offset")
		var books []Book
		switch offset {
		case "0":
			books = []Book{{ID: 1}, {ID: 2}, {ID: 3}}
		case "3":
			books = []Book{{ID: 4}, {ID: 5}, {ID: 6}}
		case "6":
			books = []Book{{ID: 7}}
		default:
			t.Errorf("unexpected offset %q", offset)
		}
		writeJSON(t, w, http.StatusOK, listEnvelope[Book]{Data: books, Total: total})
	}))
	defer srv.Close()

	books, err := newTestClient(t, srv, WithPageSize(3)).ListBooks(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListBooks() error = %v", err)
	}
	if len(books) != total {
		t.Errorf("got %d books, want %d", len(books), total)
	}
	if n := atomic.LoadInt32(&requests); n != 3 {
		t.Errorf("made %d requests, want 3", n)
	}
}

func TestListStopsOnShortPage(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		// Total claims more than is ever returned; a short page must still stop.
		writeJSON(t, w, http.StatusOK, listEnvelope[Book]{Data: []Book{{ID: 1}}, Total: 99})
	}))
	defer srv.Close()

	books, err := newTestClient(t, srv, WithPageSize(10)).ListBooks(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListBooks() error = %v", err)
	}
	if len(books) != 1 {
		t.Errorf("got %d books, want 1", len(books))
	}
	if n := atomic.LoadInt32(&requests); n != 1 {
		t.Errorf("made %d requests, want 1", n)
	}
}

func TestListFilters(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeJSON(t, w, http.StatusOK, listEnvelope[Page]{})
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).ListPages(context.Background(), Filter{"book_id": "42"})
	if err != nil {
		t.Fatalf("ListPages() error = %v", err)
	}
	if !strings.Contains(query, "filter%5Bbook_id%5D=42") {
		t.Errorf("query = %q, want it to carry filter[book_id]=42", query)
	}
}

func TestErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantMsg    string
		wantNotFnd bool
		wantUnauth bool
	}{
		{
			name:    "bookstack error envelope",
			status:  http.StatusBadRequest,
			body:    `{"error":{"message":"The name field is required."}}`,
			wantMsg: "The name field is required.",
		},
		{
			name:    "plain message field",
			status:  http.StatusBadRequest,
			body:    `{"message":"bad input"}`,
			wantMsg: "bad input",
		},
		{
			name:       "not found",
			status:     http.StatusNotFound,
			body:       `{"error":{"message":"Item not found"}}`,
			wantMsg:    "Item not found",
			wantNotFnd: true,
		},
		{
			name:       "unauthorized",
			status:     http.StatusUnauthorized,
			body:       `{"error":{"message":"Unauthorized"}}`,
			wantMsg:    "Unauthorized",
			wantUnauth: true,
		},
		{
			name:    "non-json body",
			status:  http.StatusBadGateway,
			body:    "upstream exploded",
			wantMsg: "upstream exploded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			// Disable retries so 5xx cases fail immediately.
			_, err := newTestClient(t, srv, WithMaxRetries(0)).GetPage(context.Background(), 1)
			if err == nil {
				t.Fatal("GetPage() = nil error, want an API error")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error is %T, want *APIError", err)
			}
			if apiErr.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tt.status)
			}
			if !strings.Contains(apiErr.Message, tt.wantMsg) {
				t.Errorf("Message = %q, want it to contain %q", apiErr.Message, tt.wantMsg)
			}
			if apiErr.NotFound() != tt.wantNotFnd {
				t.Errorf("NotFound() = %t, want %t", apiErr.NotFound(), tt.wantNotFnd)
			}
			if apiErr.Unauthorized() != tt.wantUnauth {
				t.Errorf("Unauthorized() = %t, want %t", apiErr.Unauthorized(), tt.wantUnauth)
			}
		})
	}
}

func TestErrorsNeverLeakCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Unauthorized"}}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv, WithMaxRetries(0)).GetPage(context.Background(), 1)
	if err == nil {
		t.Fatal("GetPage() = nil error, want an error")
	}
	for _, secret := range []string{testTokenID, testTokenSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaked %q: %v", secret, err)
		}
	}
}

func TestRetryOnTooManyRequests(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(t, w, http.StatusOK, Page{ID: 1, Name: "Recovered"})
	}))
	defer srv.Close()

	page, err := newTestClient(t, srv).GetPage(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}
	if page.Name != "Recovered" {
		t.Errorf("Name = %q, want %q", page.Name, "Recovered")
	}
	if n := atomic.LoadInt32(&attempts); n != 3 {
		t.Errorf("made %d attempts, want 3", n)
	}
}

func TestRetryOnServerError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(t, w, http.StatusOK, Page{ID: 1})
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv).GetPage(context.Background(), 1); err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}
	if n := atomic.LoadInt32(&attempts); n != 2 {
		t.Errorf("made %d attempts, want 2", n)
	}
}

func TestRetriesAreBounded(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv, WithMaxRetries(2)).GetPage(context.Background(), 1)
	if err == nil {
		t.Fatal("GetPage() = nil error, want an error after exhausting retries")
	}
	// One initial attempt plus two retries.
	if n := atomic.LoadInt32(&attempts); n != 3 {
		t.Errorf("made %d attempts, want 3", n)
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv).GetPage(context.Background(), 1); err == nil {
		t.Fatal("GetPage() = nil error, want an error")
	}
	if n := atomic.LoadInt32(&attempts); n != 1 {
		t.Errorf("made %d attempts, want 1 — 4xx must not be retried", n)
	}
}

func TestRetryAfterHeaderIsHonoured(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		writeJSON(t, w, http.StatusOK, Page{ID: 1})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	var slept time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		slept = d
		return nil
	}

	if _, err := c.GetPage(context.Background(), 1); err != nil {
		t.Fatalf("GetPage() error = %v", err)
	}
	if slept != time.Second {
		t.Errorf("slept %v, want 1s from the Retry-After header", slept)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "100000")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newTestClient(t, srv, WithMaxRetries(1))

	var slept time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		slept = d
		return nil
	}

	if _, err := c.GetPage(context.Background(), 1); err == nil {
		t.Fatal("GetPage() = nil error, want an error")
	}
	if slept != maxBackoff {
		t.Errorf("slept %v, want it capped at %v", slept, maxBackoff)
	}
}

func TestContextCancellation(t *testing.T) {
	t.Run("cancelled before the request", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK, Page{ID: 1})
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := newTestClient(t, srv).GetPage(ctx, 1)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	})

	t.Run("cancelled during a retry wait", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		c := newTestClient(t, srv)
		c.sleep = func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		}

		if _, err := c.GetPage(ctx, 1); !errors.Is(err, context.Canceled) {
			t.Errorf("error = %v, want context.Canceled", err)
		}
	})
}
