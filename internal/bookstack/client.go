package bookstack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is the set of BookStack operations shelfpress needs. The planner and
// applier depend on this interface so they can be tested without a server.
type Client interface {
	ListShelves(ctx context.Context, f Filter) ([]Shelf, error)

	// GetShelf returns one shelf including its books and tags. The list
	// endpoint omits both, so membership can only be read from here.
	GetShelf(ctx context.Context, id int) (Shelf, error)

	CreateShelf(ctx context.Context, s ShelfInput) (Shelf, error)
	UpdateShelfBooks(ctx context.Context, id int, bookIDs []int) error

	ListBooks(ctx context.Context, f Filter) ([]Book, error)
	CreateBook(ctx context.Context, b BookInput) (Book, error)
	DeleteBook(ctx context.Context, id int) error

	ListPages(ctx context.Context, f Filter) ([]Page, error)
	GetPage(ctx context.Context, id int) (Page, error)
	CreatePage(ctx context.Context, p PageInput) (Page, error)
	UpdatePage(ctx context.Context, id int, p PageInput) (Page, error)
	DeletePage(ctx context.Context, id int) error

	// SearchPagesByTag returns pages carrying the named tag, with their tags
	// populated. The list endpoints omit tags, so this is the only way to
	// recover shelfpress-managed pages in one request.
	SearchPagesByTag(ctx context.Context, name, value string) ([]Page, error)
}

// APIClient implements Client.
var _ Client = (*APIClient)(nil)

// Defaults for a client created without options.
const (
	DefaultTimeout    = 30 * time.Second
	DefaultMaxRetries = 3
	DefaultPageSize   = 100

	// maxPageSize is the largest count BookStack accepts on a list endpoint.
	maxPageSize = 500

	// maxBackoff caps the wait between retries, including a server-sent
	// Retry-After, so a hostile or misconfigured header cannot stall a run.
	maxBackoff = 30 * time.Second
)

// httpClient is the subset of *http.Client the client uses, so tests can
// substitute a transport without a live server.
type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// APIClient talks to a BookStack instance over HTTP.
type APIClient struct {
	baseURL     *url.URL
	tokenID     string
	tokenSecret string

	http       httpClient
	maxRetries int
	pageSize   int
	backoff    func(attempt int) time.Duration
	sleep      func(ctx context.Context, d time.Duration) error
}

// Option customises a client.
type Option func(*APIClient)

// WithHTTPClient replaces the underlying HTTP client.
func WithHTTPClient(h httpClient) Option {
	return func(c *APIClient) { c.http = h }
}

// WithMaxRetries sets how many times a retryable response is retried. Zero
// disables retrying.
func WithMaxRetries(n int) Option {
	return func(c *APIClient) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// WithPageSize sets how many records are requested per list page.
func WithPageSize(n int) Option {
	return func(c *APIClient) {
		if n > 0 && n <= maxPageSize {
			c.pageSize = n
		}
	}
}

// WithBackoff replaces the retry delay schedule. Intended for tests.
func WithBackoff(f func(attempt int) time.Duration) Option {
	return func(c *APIClient) { c.backoff = f }
}

// New returns a client for the BookStack instance at baseURL.
func New(baseURL, tokenID, tokenSecret string, opts ...Option) (*APIClient, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("bookstack: base URL is required")
	}
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("bookstack: parsing base URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("bookstack: base URL %q must include a scheme and host", baseURL)
	}
	if tokenID == "" || tokenSecret == "" {
		return nil, errors.New("bookstack: token ID and secret are required")
	}

	c := &APIClient{
		baseURL:     u,
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		http:        &http.Client{Timeout: DefaultTimeout},
		maxRetries:  DefaultMaxRetries,
		pageSize:    DefaultPageSize,
		backoff:     exponentialBackoff,
		sleep:       sleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// exponentialBackoff waits 200ms, 400ms, 800ms, … capped at maxBackoff.
func exponentialBackoff(attempt int) time.Duration {
	d := time.Duration(math.Pow(2, float64(attempt))) * 200 * time.Millisecond
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// sleepCtx waits for d, returning early if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// do performs a request, retrying 429 and 5xx responses, and decodes a JSON
// body into out when out is non-nil.
func (c *APIClient) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("bookstack: encoding request for %s: %w", path, err)
		}
	}

	endpoint := method + " " + path

	for attempt := 0; ; attempt++ {
		resp, err := c.attempt(ctx, method, path, query, payload)
		if err != nil {
			return err
		}

		retryAfter, retryable := retryDelay(resp)
		if retryable && attempt < c.maxRetries {
			drain(resp)
			wait := retryAfter
			if wait <= 0 {
				wait = c.backoff(attempt)
			}
			if wait > maxBackoff {
				wait = maxBackoff
			}
			if err := c.sleep(ctx, wait); err != nil {
				return fmt.Errorf("bookstack: %s: %w", endpoint, err)
			}
			continue
		}

		return decode(resp, endpoint, out)
	}
}

// attempt issues a single HTTP request.
func (c *APIClient) attempt(ctx context.Context, method, path string, query url.Values, payload []byte) (*http.Response, error) {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + path
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	var reader io.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("bookstack: building request for %s %s: %w", method, path, err)
	}

	req.Header.Set("Authorization", "Token "+c.tokenID+":"+c.tokenSecret)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// The URL in a transport error can contain credentials only if the
		// caller embedded them in baseURL; report path rather than url.Error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("bookstack: %s %s: %w", method, path, ctxErr)
		}
		return nil, fmt.Errorf("bookstack: %s %s: %w", method, path, redactURLError(err))
	}
	return resp, nil
}

// retryDelay reports whether a response should be retried and how long to wait
// if the server asked.
func retryDelay(resp *http.Response) (time.Duration, bool) {
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
	case resp.StatusCode >= 500:
	default:
		return 0, false
	}

	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second, true
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return d, true
			}
			return 0, true
		}
	}
	return 0, true
}

// decode turns a response into an error or a decoded value.
func decode(resp *http.Response, endpoint string, out any) error {
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{
			StatusCode: resp.StatusCode,
			Message:    errorMessage(resp.Body),
			Endpoint:   endpoint,
		}
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("bookstack: decoding response from %s: %w", endpoint, err)
	}
	return nil
}

// maxErrorBody bounds how much of an error response is read into a message.
const maxErrorBody = 4 << 10

// errorMessage extracts BookStack's error text, falling back to raw body text.
func errorMessage(r io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(r, maxErrorBody))
	if err != nil || len(data) == 0 {
		return ""
	}

	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &envelope) == nil {
		if envelope.Error.Message != "" {
			return envelope.Error.Message
		}
		if envelope.Message != "" {
			return envelope.Message
		}
	}
	return strings.TrimSpace(string(data))
}

// drain consumes and closes a body so the connection can be reused.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	_ = resp.Body.Close()
}

// redactURLError strips the URL from a transport error, which may carry
// userinfo credentials if the base URL contained any.
func redactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
