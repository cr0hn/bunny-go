package storage

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cr0hn/bunny-go"
)

// Region is the primary region of a storage zone; it selects the endpoint.
type Region string

// Storage regions and their endpoints, from https://docs.bunny.net/storage/http.
const (
	Falkenstein  Region = "" // storage.bunnycdn.com (default, also Frankfurt)
	London       Region = "uk"
	NewYork      Region = "ny"
	LosAngeles   Region = "la"
	Singapore    Region = "sg"
	Stockholm    Region = "se"
	SaoPaulo     Region = "br"
	Johannesburg Region = "jh"
	Sydney       Region = "syd"
)

// Endpoint returns the base URL of the region.
func (r Region) Endpoint() string {
	if r == "" {
		return "https://storage.bunnycdn.com"
	}
	return "https://" + string(r) + ".storage.bunnycdn.com"
}

// Client talks to one storage zone. It is safe for concurrent use.
type Client struct {
	core *bunny.Client
	zone string
}

type config struct {
	endpoint string
	opts     []bunny.Option
}

// Option configures a [Client].
type Option func(*config)

// WithRegion selects the endpoint by region (default [Falkenstein]).
func WithRegion(r Region) Option { return func(c *config) { c.endpoint = r.Endpoint() } }

// WithEndpoint sets the base URL explicitly, for example the host shown on
// the zone's Access page or a test server. It wins over [WithRegion].
func WithEndpoint(u string) Option { return func(c *config) { c.endpoint = u } }

// WithCoreOptions passes options (HTTP client, retries, user agent...) to the
// underlying [bunny.Client].
func WithCoreOptions(o ...bunny.Option) Option {
	return func(c *config) { c.opts = append(c.opts, o...) }
}

// New returns a Client for the zone, authenticated with the zone password.
func New(zone, password string, opts ...Option) *Client {
	cfg := config{endpoint: Falkenstein.Endpoint()}
	for _, o := range opts {
		o(&cfg)
	}
	all := append([]bunny.Option{bunny.WithBaseURL(cfg.endpoint), bunny.WithAPIKey(password)}, cfg.opts...)
	return &Client{core: bunny.NewClient(all...), zone: zone}
}

// UploadOptions are optional upload settings.
type UploadOptions struct {
	// ContentType is sent as the Content-Type header.
	ContentType string
	// Size is the body length in bytes, when known. Without it a reader that
	// is not a bytes/strings reader is sent chunked.
	Size int64
	// SHA256 is the expected hex SHA-256 of the body; bunny.net rejects the
	// upload (400) when it does not match.
	SHA256 string
}

// Object is an entry returned by [Client.List] and [Client.Stat].
type Object struct {
	Name        string
	Path        string // directory that holds the object, as reported by bunny.net
	Size        int64
	IsDirectory bool
	LastChanged time.Time // UTC
	DateCreated time.Time // UTC
}

var errBadPath = errors.New("storage: invalid path")

// escape validates a zone-relative path and percent-escapes each segment.
// Empty, "." and ".." segments are rejected so a path cannot leave its place.
func escape(p string) (string, error) {
	segs := strings.Split(strings.Trim(p, "/"), "/")
	for i, s := range segs {
		if s == "" || s == "." || s == ".." {
			return "", fmt.Errorf("%w: %q", errBadPath, p)
		}
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/"), nil
}

func (c *Client) path(p string) (string, error) {
	e, err := escape(p)
	if err != nil {
		return "", err
	}
	return url.PathEscape(c.zone) + "/" + e, nil
}

// dirPath is like path but allows the zone root ("" or "/") and ends in "/".
func (c *Client) dirPath(p string) (string, error) {
	if strings.Trim(p, "/") == "" {
		return url.PathEscape(c.zone) + "/", nil
	}
	e, err := c.path(p)
	return e + "/", err
}

// Upload stores body at path (creating directories as needed), streaming it.
// If body is an [io.Seeker] the upload is retried on transient errors.
func (c *Client) Upload(ctx context.Context, path string, body io.Reader, o UploadOptions) error {
	p, err := c.path(path)
	if err != nil {
		return err
	}
	h := http.Header{}
	if o.ContentType != "" {
		h.Set("Content-Type", o.ContentType)
	}
	if o.SHA256 != "" {
		if b, err := hex.DecodeString(o.SHA256); err != nil || len(b) != 32 {
			return errors.New("storage: SHA256 must be 64 hex characters")
		}
		h.Set("Checksum", strings.ToUpper(o.SHA256))
	}
	req := bunny.Request{Method: http.MethodPut, Path: p, Header: h, Body: body, ContentLength: o.Size}
	if s, ok := body.(io.ReadSeeker); ok {
		start, err := s.Seek(0, io.SeekCurrent)
		if err != nil {
			return fmt.Errorf("storage: seeking body: %w", err)
		}
		req.GetBody = func() (io.Reader, error) {
			_, err := s.Seek(start, io.SeekStart)
			return s, err
		}
	}
	return c.core.Do(ctx, req, nil)
}

// Download returns the file body; the caller must close it. A missing file
// yields an error matching [bunny.ErrNotFound].
func (c *Client) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	p, err := c.path(path)
	if err != nil {
		return nil, err
	}
	resp, err := c.core.Raw(ctx, bunny.Request{Method: http.MethodGet, Path: p})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

type wireObject struct {
	ObjectName  string
	Path        string
	Length      int64
	IsDirectory bool
	LastChanged string
	DateCreated string
}

func parseTime(s string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05.999999999", s) // zero on failure
	return t
}

// List returns the entries of a directory ("" or "/" is the zone root).
func (c *Client) List(ctx context.Context, dir string) ([]Object, error) {
	p, err := c.dirPath(dir)
	if err != nil {
		return nil, err
	}
	var raw []wireObject
	if err := c.core.Do(ctx, bunny.Request{Method: http.MethodGet, Path: p}, &raw); err != nil {
		return nil, err
	}
	out := make([]Object, len(raw))
	for i, w := range raw {
		out[i] = Object{
			Name: w.ObjectName, Path: w.Path, Size: w.Length, IsDirectory: w.IsDirectory,
			LastChanged: parseTime(w.LastChanged), DateCreated: parseTime(w.DateCreated),
		}
	}
	return out, nil
}

// Stat returns the metadata of a file or directory without downloading it.
// The API has no HEAD call, so it lists the parent directory. A missing
// object yields an error matching [bunny.ErrNotFound].
func (c *Client) Stat(ctx context.Context, path string) (Object, error) {
	if _, err := escape(path); err != nil {
		return Object{}, err
	}
	trimmed := strings.Trim(path, "/")
	dir, name := "", trimmed
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		dir, name = trimmed[:i], trimmed[i+1:]
	}
	objs, err := c.List(ctx, dir)
	if err != nil {
		return Object{}, err
	}
	for _, o := range objs {
		if o.Name == name {
			return o, nil
		}
	}
	return Object{}, &bunny.APIError{StatusCode: http.StatusNotFound, Message: "object not found"}
}

// Exists reports whether a file or directory exists. Only a not-found result
// gives (false, nil).
func (c *Client) Exists(ctx context.Context, path string) (bool, error) {
	_, err := c.Stat(ctx, path)
	if errors.Is(err, bunny.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// Delete removes one file.
func (c *Client) Delete(ctx context.Context, path string) error {
	p, err := c.path(path)
	if err != nil {
		return err
	}
	return c.core.Do(ctx, bunny.Request{Method: http.MethodDelete, Path: p}, nil)
}

// DeleteDir removes a directory and, recursively, everything in it. The zone
// root cannot be deleted through this client.
func (c *Client) DeleteDir(ctx context.Context, dir string) error {
	p, err := c.dirPath(dir)
	if err != nil {
		return err
	}
	if strings.Trim(dir, "/") == "" {
		return fmt.Errorf("%w: refusing to delete the zone root", errBadPath)
	}
	return c.core.Do(ctx, bunny.Request{Method: http.MethodDelete, Path: p}, nil)
}
