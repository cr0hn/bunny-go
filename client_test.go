package bunny_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cr0hn/bunny-go"
)

func newTestClient(srv *httptest.Server, opts ...bunny.Option) *bunny.Client {
	base := make([]bunny.Option, 0, 3+len(opts))
	base = append(base, bunny.WithBaseURL(srv.URL), bunny.WithAPIKey("k"),
		bunny.WithBackoff(time.Millisecond, 5*time.Millisecond))
	return bunny.NewClient(append(base, opts...)...)
}

func TestHeadersAndDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "bunny-go/"+bunny.Version || r.Header.Get("AccessKey") != "k" {
			t.Errorf("bad headers: %v", r.Header)
		}
		if r.URL.String() != "/a/b?x=1" {
			t.Errorf("url = %s", r.URL)
		}
		_, _ = io.WriteString(w, `{"n":5}`)
	}))
	defer srv.Close()
	var out struct{ N int }
	q := map[string][]string{"x": {"1"}}
	err := newTestClient(srv).Do(context.Background(), bunny.Request{Method: "GET", Path: "a/b", Query: q}, &out)
	if err != nil || out.N != 5 {
		t.Fatalf("err=%v out=%+v", err, out)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{{404, bunny.ErrNotFound}, {401, bunny.ErrUnauthorized}, {403, bunny.ErrUnauthorized}, {429, bunny.ErrRateLimited}}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, `{"ErrorKey":"some.key","Message":"boom"}`)
		}))
		err := newTestClient(srv, bunny.WithMaxRetries(0)).Do(context.Background(), bunny.Request{Method: "GET", Path: "/"}, nil)
		srv.Close()
		var ae *bunny.APIError
		if !errors.Is(err, tc.want) || !errors.As(err, &ae) || ae.Code != "some.key" || ae.Message != "boom" || ae.StatusCode != tc.status {
			t.Errorf("status %d: err=%v", tc.status, err)
		}
		if errors.Is(err, bunny.ErrNotFound) != (tc.status == 404) {
			t.Errorf("status %d wrongly matches ErrNotFound", tc.status)
		}
	}
}

func TestStreamStyleErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"success":false,"message":"bad","statusCode":400}`)
	}))
	defer srv.Close()
	err := newTestClient(srv).Do(context.Background(), bunny.Request{Method: "POST", Path: "/"}, nil)
	var ae *bunny.APIError
	if !errors.As(err, &ae) || ae.Message != "bad" {
		t.Fatalf("err=%v", err)
	}
}

func TestLargeBodyTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, strings.Repeat("x", 1<<20))
	}))
	defer srv.Close()
	err := newTestClient(srv).Do(context.Background(), bunny.Request{Method: "GET", Path: "/"}, nil)
	var ae *bunny.APIError
	if !errors.As(err, &ae) || len(ae.Body) != bunny.MaxErrorBody || !ae.Truncated {
		t.Fatalf("err=%v", err)
	}
}

func countingServer(status int, okAfter int32, hdr map[string]string) (*httptest.Server, *atomic.Int32) {
	var n atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) > okAfter {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
	})), &n
}

func TestRetries(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		status   int
		okAfter  int32
		wantCall int32
		wantErr  bool
	}{
		{"GET 503 recovers", "GET", 503, 2, 3, false},
		{"PUT 500 recovers", "PUT", 500, 1, 2, false},
		{"DELETE 429 recovers", "DELETE", 429, 1, 2, false},
		{"GET 404 not retried", "GET", 404, 9, 1, true},
		{"GET 400 not retried", "GET", 400, 9, 1, true},
		{"POST 503 not retried", "POST", 503, 9, 1, true},
		{"GET exhausts retries", "GET", 500, 99, 4, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, n := countingServer(tc.status, tc.okAfter, nil)
			defer srv.Close()
			err := newTestClient(srv).Do(context.Background(), bunny.Request{Method: tc.method, Path: "/"}, nil)
			if (err != nil) != tc.wantErr || n.Load() != tc.wantCall {
				t.Fatalf("err=%v calls=%d", err, n.Load())
			}
		})
	}
}

func TestBodyRetryNeedsGetBody(t *testing.T) {
	srv, n := countingServer(503, 9, nil)
	defer srv.Close()
	c := newTestClient(srv)
	_ = c.Do(context.Background(), bunny.Request{Method: "PUT", Path: "/", Body: strings.NewReader("data")}, nil)
	if n.Load() != 1 {
		t.Fatalf("body without GetBody retried: %d calls", n.Load())
	}
	n.Store(0)
	_ = c.Do(context.Background(), bunny.Request{
		Method: "PUT", Path: "/", Body: strings.NewReader("data"),
		GetBody: func() (io.Reader, error) { return strings.NewReader("data"), nil },
	}, nil)
	if n.Load() != 4 {
		t.Fatalf("GetBody request calls = %d, want 4", n.Load())
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	srv, n := countingServer(429, 1, map[string]string{"Retry-After": "1"})
	defer srv.Close()
	start := time.Now()
	if err := newTestClient(srv).Do(context.Background(), bunny.Request{Method: "GET", Path: "/"}, nil); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < time.Second || n.Load() != 2 {
		t.Fatalf("elapsed %v calls %d: Retry-After not honoured", el, n.Load())
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	date := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	srv, _ := countingServer(503, 1, map[string]string{"Retry-After": date})
	defer srv.Close()
	start := time.Now()
	if err := newTestClient(srv).Do(context.Background(), bunny.Request{Method: "GET", Path: "/"}, nil); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el < time.Second {
		t.Fatalf("elapsed %v: HTTP-date Retry-After not honoured", el)
	}
}

func TestContextCancelDuringBackoff(t *testing.T) {
	srv, n := countingServer(429, 99, map[string]string{"Retry-After": "30"})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := newTestClient(srv).Do(ctx, bunny.Request{Method: "GET", Path: "/"}, nil)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second || n.Load() != 1 {
		t.Fatalf("err=%v elapsed=%v calls=%d", err, time.Since(start), n.Load())
	}
}

func TestContextAlreadyCancelledNoRetry(t *testing.T) {
	srv, n := countingServer(200, 0, nil)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := newTestClient(srv).Do(ctx, bunny.Request{Method: "GET", Path: "/"}, nil)
	if !errors.Is(err, context.Canceled) || n.Load() != 0 {
		t.Fatalf("err=%v calls=%d", err, n.Load())
	}
}

func TestTransportErrorRetriedForIdempotent(t *testing.T) {
	var calls atomic.Int32
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) < 3 {
			return nil, errors.New("connection reset")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: http.Header{}}, nil
	})
	c := bunny.NewClient(bunny.WithHTTPClient(&http.Client{Transport: rt}), bunny.WithBaseURL("http://x"),
		bunny.WithBackoff(time.Millisecond, time.Millisecond))
	if err := c.Do(context.Background(), bunny.Request{Method: "GET", Path: "/"}, nil); err != nil || calls.Load() != 3 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestRawStreamsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "binary") }))
	defer srv.Close()
	resp, err := newTestClient(srv).Raw(context.Background(), bunny.Request{Method: "GET", Path: "/f"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if b, _ := io.ReadAll(resp.Body); string(b) != "binary" {
		t.Fatalf("body %q", b)
	}
}

func TestInvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "nope") }))
	defer srv.Close()
	var out map[string]any
	if err := newTestClient(srv).Do(context.Background(), bunny.Request{Method: "GET", Path: "/"}, &out); err == nil {
		t.Fatal("want decode error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
