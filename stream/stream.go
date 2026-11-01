package stream

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cr0hn/bunny-go"
	"github.com/cr0hn/bunny-go/signing"
)

// BaseURL is the Stream API endpoint.
const BaseURL = "https://video.bunnycdn.com"

// TUSEndpoint is the URL TUS clients upload to with the headers of
// [Client.SignTUS].
const TUSEndpoint = BaseURL + "/tusupload"

// VideoStatus is the processing state of a video.
type VideoStatus int

// Video states, from the official API specification.
const (
	StatusCreated             VideoStatus = 0
	StatusUploaded            VideoStatus = 1
	StatusProcessing          VideoStatus = 2
	StatusTranscoding         VideoStatus = 3
	StatusFinished            VideoStatus = 4
	StatusError               VideoStatus = 5
	StatusUploadFailed        VideoStatus = 6
	StatusJitSegmenting       VideoStatus = 7
	StatusJitPlaylistsCreated VideoStatus = 8
)

var statusNames = [...]string{"created", "uploaded", "processing", "transcoding", "finished", "error", "upload_failed", "jit_segmenting", "jit_playlists_created"}

// String returns a lower-case name, or "unknown(N)".
func (s VideoStatus) String() string {
	if s >= 0 && int(s) < len(statusNames) {
		return statusNames[s]
	}
	return "unknown(" + strconv.Itoa(int(s)) + ")"
}

// Playable reports whether the video can be played (finished or JIT ready).
func (s VideoStatus) Playable() bool { return s == StatusFinished || s == StatusJitPlaylistsCreated }

// Video is a video of the library. Only commonly used fields are modelled.
type Video struct {
	LibraryID            int64       `json:"videoLibraryId"`
	GUID                 string      `json:"guid"`
	Title                string      `json:"title"`
	Description          string      `json:"description,omitempty"`
	DateUploaded         Time        `json:"dateUploaded"`
	Views                int64       `json:"views"`
	IsPublic             bool        `json:"isPublic"`
	LengthSeconds        int         `json:"length"`
	Status               VideoStatus `json:"status"`
	Framerate            float64     `json:"framerate"`
	Width                int         `json:"width"`
	Height               int         `json:"height"`
	AvailableResolutions string      `json:"availableResolutions,omitempty"`
	ThumbnailCount       int         `json:"thumbnailCount"`
	EncodeProgress       int         `json:"encodeProgress"`
	StorageSize          int64       `json:"storageSize"`
	HasMP4Fallback       bool        `json:"hasMP4Fallback"`
	CollectionID         string      `json:"collectionId,omitempty"`
	ThumbnailFileName    string      `json:"thumbnailFileName,omitempty"`
}

// Collection groups videos.
type Collection struct {
	LibraryID       int64    `json:"videoLibraryId"`
	GUID            string   `json:"guid"`
	Name            string   `json:"name"`
	VideoCount      int64    `json:"videoCount"`
	TotalSize       int64    `json:"totalSize"`
	PreviewVideoIDs string   `json:"previewVideoIds,omitempty"`
	PreviewImageURL []string `json:"previewImageUrls,omitempty"`
}

// Page is one page of results (1-based).
type Page[T any] struct {
	TotalItems   int64 `json:"totalItems"`
	CurrentPage  int   `json:"currentPage"`
	ItemsPerPage int   `json:"itemsPerPage"`
	Items        []T   `json:"items"`
}

func (p Page[T]) last() bool {
	return len(p.Items) == 0 || int64(p.CurrentPage)*int64(p.ItemsPerPage) >= p.TotalItems
}

// ListOptions filters and pages a list call. Zero values use API defaults.
type ListOptions struct {
	Page         int    // 1-based
	ItemsPerPage int    // the API clamps to 10-1000
	Search       string // case-insensitive title/name substring
	CollectionID string // videos only
	OrderBy      string // "date" (default) or "title"
}

func (o ListOptions) query(withCollection bool) url.Values {
	q := url.Values{}
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.ItemsPerPage > 0 {
		q.Set("itemsPerPage", strconv.Itoa(o.ItemsPerPage))
	}
	if o.Search != "" {
		q.Set("search", o.Search)
	}
	if withCollection && o.CollectionID != "" {
		q.Set("collection", o.CollectionID)
	}
	if o.OrderBy != "" {
		q.Set("orderBy", o.OrderBy)
	}
	return q
}

// Client talks to one video library. It is safe for concurrent use.
type Client struct {
	core      *bunny.Client
	libraryID string
	apiKey    string
}

type config struct {
	baseURL string
	opts    []bunny.Option
}

// Option configures a [Client].
type Option func(*config)

// WithBaseURL overrides [BaseURL], for tests.
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithCoreOptions passes options (HTTP client, retries...) to the underlying
// [bunny.Client].
func WithCoreOptions(o ...bunny.Option) Option {
	return func(c *config) { c.opts = append(c.opts, o...) }
}

// New returns a Client for the library, authenticated with its API key.
func New(libraryID, apiKey string, opts ...Option) *Client {
	cfg := config{baseURL: BaseURL}
	for _, o := range opts {
		o(&cfg)
	}
	all := append([]bunny.Option{bunny.WithBaseURL(cfg.baseURL), bunny.WithAPIKey(apiKey)}, cfg.opts...)
	return &Client{core: bunny.NewClient(all...), libraryID: libraryID, apiKey: apiKey}
}

func esc(s string) string { return url.PathEscape(s) }

func (c *Client) lib() string { return "/library/" + esc(c.libraryID) }

func jsonReq(method, path string, q url.Values, body []byte) bunny.Request {
	r := bunny.Request{Method: method, Path: path, Query: q}
	if body != nil {
		r.Header = http.Header{"Content-Type": {"application/json"}}
		r.GetBody = func() (io.Reader, error) { return bytesReader(body), nil }
		r.ContentLength = int64(len(body))
	}
	return r
}

// CreateVideoOptions are optional settings for [Client.CreateVideo].
type CreateVideoOptions struct {
	CollectionID  string
	ThumbnailTime int // ms; 0 keeps the default
}

// CreateVideo creates an empty video object; upload the file afterwards with
// [Client.Upload] or a TUS client.
func (c *Client) CreateVideo(ctx context.Context, title string, o CreateVideoOptions) (*Video, error) {
	body := map[string]any{"title": title}
	if o.CollectionID != "" {
		body["collectionId"] = o.CollectionID
	}
	if o.ThumbnailTime > 0 {
		body["thumbnailTime"] = o.ThumbnailTime
	}
	var v Video
	if err := c.postJSON(ctx, c.lib()+"/videos", body, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// GetVideo returns one video.
func (c *Client) GetVideo(ctx context.Context, videoID string) (*Video, error) {
	var v Video
	err := c.core.Do(ctx, bunny.Request{Method: http.MethodGet, Path: c.lib() + "/videos/" + esc(videoID)}, &v)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// UpdateVideo changes the title and/or collection. Empty Title is left
// unchanged. To remove the video from its collection pass a non-nil pointer
// to the empty string in CollectionID.
type UpdateVideo struct {
	Title        string
	CollectionID *string
}

// UpdateVideo applies u to the video.
func (c *Client) UpdateVideo(ctx context.Context, videoID string, u UpdateVideo) error {
	body := map[string]any{}
	if u.Title != "" {
		body["title"] = u.Title
	}
	if u.CollectionID != nil {
		body["collectionId"] = *u.CollectionID
	}
	return c.postJSON(ctx, c.lib()+"/videos/"+esc(videoID), body, nil)
}

// DeleteVideo deletes a video.
func (c *Client) DeleteVideo(ctx context.Context, videoID string) error {
	return c.core.Do(ctx, bunny.Request{Method: http.MethodDelete, Path: c.lib() + "/videos/" + esc(videoID)}, nil)
}

// ListVideos returns one page of videos.
func (c *Client) ListVideos(ctx context.Context, o ListOptions) (*Page[Video], error) {
	var p Page[Video]
	err := c.core.Do(ctx, bunny.Request{Method: http.MethodGet, Path: c.lib() + "/videos", Query: o.query(true)}, &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Videos iterates over all matching videos, fetching pages lazily starting at
// o.Page (default 1). The iteration ends after the first error.
func (c *Client) Videos(ctx context.Context, o ListOptions) iter.Seq2[Video, error] {
	return paginate(o, func(o ListOptions) (*Page[Video], error) { return c.ListVideos(ctx, o) })
}

// UploadOptions are optional settings for [Client.Upload].
type UploadOptions struct {
	// Size is the body length in bytes; when set the body is sent with a
	// Content-Length instead of chunked.
	Size int64
}

// Upload streams the video file to an already created video. The body is sent
// once and never retried.
func (c *Client) Upload(ctx context.Context, videoID string, body io.Reader, o UploadOptions) error {
	return c.core.Do(ctx, bunny.Request{
		Method: http.MethodPut, Path: c.lib() + "/videos/" + esc(videoID),
		Header: http.Header{"Content-Type": {"application/octet-stream"}},
		Body:   body, ContentLength: o.Size,
	}, nil)
}

// FetchOptions are optional settings for [Client.Fetch].
type FetchOptions struct {
	Title         string
	CollectionID  string
	ThumbnailTime int // ms
	Headers       map[string]string
}

// Fetch asks bunny.net to download the video from srcURL into the library.
// The API returns no video ID; find the video by title or collection.
func (c *Client) Fetch(ctx context.Context, srcURL string, o FetchOptions) error {
	body := map[string]any{"url": srcURL}
	if o.Title != "" {
		body["title"] = o.Title
	}
	if len(o.Headers) > 0 {
		body["headers"] = o.Headers
	}
	q := url.Values{}
	if o.CollectionID != "" {
		q.Set("collectionId", o.CollectionID)
	}
	if o.ThumbnailTime > 0 {
		q.Set("thumbnailTime", strconv.Itoa(o.ThumbnailTime))
	}
	return c.core.Do(ctx, jsonReqMarshal(http.MethodPost, c.lib()+"/videos/fetch", q, body), nil)
}

// CreateCollection creates a collection.
func (c *Client) CreateCollection(ctx context.Context, name string) (*Collection, error) {
	var col Collection
	if err := c.postJSON(ctx, c.lib()+"/collections", map[string]any{"name": name}, &col); err != nil {
		return nil, err
	}
	return &col, nil
}

// GetCollection returns one collection.
func (c *Client) GetCollection(ctx context.Context, collectionID string) (*Collection, error) {
	var col Collection
	err := c.core.Do(ctx, bunny.Request{Method: http.MethodGet, Path: c.lib() + "/collections/" + esc(collectionID)}, &col)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

// RenameCollection changes the name of a collection.
func (c *Client) RenameCollection(ctx context.Context, collectionID, name string) error {
	return c.postJSON(ctx, c.lib()+"/collections/"+esc(collectionID), map[string]any{"name": name}, nil)
}

// DeleteCollection deletes a collection (not its videos' files by itself:
// see the bunny.net docs for the effect on contained videos).
func (c *Client) DeleteCollection(ctx context.Context, collectionID string) error {
	return c.core.Do(ctx, bunny.Request{Method: http.MethodDelete, Path: c.lib() + "/collections/" + esc(collectionID)}, nil)
}

// ListCollections returns one page of collections.
func (c *Client) ListCollections(ctx context.Context, o ListOptions) (*Page[Collection], error) {
	var p Page[Collection]
	err := c.core.Do(ctx, bunny.Request{Method: http.MethodGet, Path: c.lib() + "/collections", Query: o.query(false)}, &p)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Collections iterates over all matching collections; see [Client.Videos].
func (c *Client) Collections(ctx context.Context, o ListOptions) iter.Seq2[Collection, error] {
	return paginate(o, func(o ListOptions) (*Page[Collection], error) { return c.ListCollections(ctx, o) })
}

// SignTUS returns the headers a TUS client must send to upload to
// [TUSEndpoint] for an existing video, valid until expires. Call it on your
// server: it uses the library API key.
func (c *Client) SignTUS(videoID string, expires time.Time) signing.TUSUpload {
	return signing.SignTUS(c.libraryID, c.apiKey, videoID, expires)
}

// EmbedURL returns the signed player URL for the video. tokenKey is the
// library's Token Authentication Key (not the API key).
func (c *Client) EmbedURL(videoID, tokenKey string, expires time.Time) string {
	return signing.EmbedURL(c.libraryID, videoID, tokenKey, expires)
}

func paginate[T any](o ListOptions, list func(ListOptions) (*Page[T], error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		if o.Page < 1 {
			o.Page = 1
		}
		for {
			p, err := list(o)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, it := range p.Items {
				if !yield(it, nil) {
					return
				}
			}
			if p.last() {
				return
			}
			o.Page++
		}
	}
}
