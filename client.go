package bunny

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version, sent in the User-Agent header.
const Version = "0.1.0"

// UserAgent is the default User-Agent header value.
const UserAgent = "bunny-go/" + Version

// maxRetryAfter bounds how long a Retry-After header can make us wait.
const maxRetryAfter = 60 * time.Second

// Client is the shared HTTP transport. It is safe for concurrent use.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	userAgent  string
	maxRetries int
	backoff    time.Duration
	maxBackoff time.Duration
}

// Option configures a [Client].
type Option func(*Client)

// WithHTTPClient sets the underlying [http.Client]. A nil value is ignored.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// WithBaseURL sets the base URL that request paths are appended to.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithAPIKey sets the AccessKey header sent with every request.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithUserAgent replaces the default User-Agent.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// WithMaxRetries sets how many times an idempotent request is retried after
// the first attempt (default 3). Zero disables retries.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = max(n, 0) }
}

// WithBackoff sets the base and maximum delay of the exponential backoff
// (defaults 200ms and 5s).
func WithBackoff(base, maxDelay time.Duration) Option {
	return func(c *Client) { c.backoff, c.maxBackoff = base, maxDelay }
}

// NewClient returns a Client with the given options applied.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: http.DefaultClient,
		userAgent:  UserAgent,
		maxRetries: 3,
		backoff:    200 * time.Millisecond,
		maxBackoff: 5 * time.Second,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Request describes one API call.
type Request struct {
	// Method is the HTTP method.
	Method string
	// Path is appended to the client base URL (or used as is if absolute).
	Path string
	// Query holds optional query parameters.
	Query url.Values
	// Header holds extra request headers.
	Header http.Header
	// Body is the request body. Without GetBody it can be sent only once, so
	// the request is never retried.
	Body io.Reader
	// GetBody returns a fresh copy of the body for each attempt; it enables
	// retries for requests with a body.
	GetBody func() (io.Reader, error)
	// ContentLength is the body length in bytes when known and positive;
	// otherwise the body is sent chunked unless Go can infer its length.
	ContentLength int64
}

func (r *Request) idempotent() bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return r.Body == nil || r.GetBody != nil
	}
	return false
}

// Do performs the request and, when out is non-nil, decodes the JSON response
// into it. Non-2xx responses return an [*APIError].
func (c *Client) Do(ctx context.Context, req Request, out any) error {
	resp, err := c.Raw(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("bunny: decoding response: %w", err)
	}
	return nil
}

// Raw performs the request and returns the 2xx response; the caller must close
// its body. Non-2xx responses return an [*APIError]. Retries apply.
func (c *Client) Raw(ctx context.Context, req Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := c.once(ctx, req)
		retryable := req.idempotent() && attempt < c.maxRetries
		var wait time.Duration
		switch {
		case err != nil:
			if ctx.Err() != nil || !retryable {
				return nil, err
			}
			wait = c.delay(attempt, 0)
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp, nil
		default:
			apiErr := readAPIError(resp)
			if !retryable || (resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500) {
				return nil, apiErr
			}
			wait = c.delay(attempt, parseRetryAfter(resp.Header.Get("Retry-After")))
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

func (c *Client) once(ctx context.Context, r Request) (*http.Response, error) {
	u := r.Path
	if !strings.Contains(u, "://") {
		u = c.baseURL + "/" + strings.TrimLeft(r.Path, "/")
	}
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	body := r.Body
	if r.GetBody != nil {
		b, err := r.GetBody()
		if err != nil {
			return nil, fmt.Errorf("bunny: rewinding body: %w", err)
		}
		body = b
	}
	hr, err := http.NewRequestWithContext(ctx, r.Method, u, body)
	if err != nil {
		return nil, fmt.Errorf("bunny: building request: %w", err)
	}
	if r.ContentLength > 0 {
		hr.ContentLength = r.ContentLength
	}
	for k, v := range r.Header {
		hr.Header[k] = v
	}
	hr.Header.Set("User-Agent", c.userAgent)
	hr.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		hr.Header.Set("AccessKey", c.apiKey)
	}
	return c.httpClient.Do(hr)
}

func readAPIError(resp *http.Response) *APIError {
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorBody+1))
	trunc := len(b) > MaxErrorBody
	if trunc {
		b = b[:MaxErrorBody]
	}
	return newAPIError(resp.StatusCode, b, trunc)
}

// delay is full-jitter exponential backoff; a Retry-After value is a floor.
func (c *Client) delay(attempt int, retryAfter time.Duration) time.Duration {
	d := c.backoff << min(attempt, 20)
	if d <= 0 || d > c.maxBackoff {
		d = c.maxBackoff
	}
	if d > 0 {
		d = time.Duration(rand.Int64N(int64(d) + 1)) //nolint:gosec // jitter, not security
	}
	return max(d, retryAfter)
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil {
		return min(max(time.Duration(s)*time.Second, 0), maxRetryAfter)
	}
	if t, err := http.ParseTime(v); err == nil {
		return min(max(time.Until(t), 0), maxRetryAfter)
	}
	return 0
}
