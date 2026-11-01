package stream_test

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cr0hn/bunny-go"
	"github.com/cr0hn/bunny-go/signing"
	"github.com/cr0hn/bunny-go/stream"
)

var update = flag.Bool("update", false, "rewrite golden files")

type reply struct {
	status int
	body   string
}

// fake records each request (method, escaped URI, headers, body) and answers
// with the queued replies in order (the last one repeats).
func fake(t *testing.T, replies ...reply) (*stream.Client, *strings.Builder) {
	t.Helper()
	var rec strings.Builder
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(&rec, "%s %s\n", r.Method, r.RequestURI)
		for _, h := range []string{"Accesskey", "Content-Type", "User-Agent"} {
			if v := r.Header.Get(h); v != "" {
				fmt.Fprintf(&rec, "%s: %s\n", h, v)
			}
		}
		fmt.Fprintf(&rec, "Content-Length: %d\nTransfer-Encoding: %v\n\n%s\n", r.ContentLength, r.TransferEncoding, body)
		rp := replies[min(n, len(replies)-1)]
		n++
		w.WriteHeader(rp.status)
		_, _ = io.WriteString(w, rp.body)
	}))
	t.Cleanup(srv.Close)
	return stream.New("123", "key", stream.WithBaseURL(srv.URL),
		stream.WithCoreOptions(bunny.WithBackoff(time.Millisecond, time.Millisecond))), &rec
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s mismatch\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}

func sample(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var ctx = context.Background()

func TestVideoRequestsGolden(t *testing.T) {
	ok := reply{200, `{"success":true,"message":"OK","statusCode":200}`}
	c, rec := fake(t, reply{200, sample(t, "video.json")}, ok, ok, ok)
	v, err := c.CreateVideo(ctx, "Intro ñ", stream.CreateVideoOptions{CollectionID: "col-1", ThumbnailTime: 1500})
	if err != nil {
		t.Fatal(err)
	}
	if v.GUID == "" {
		t.Fatal("no guid")
	}
	empty := ""
	if err := c.UpdateVideo(ctx, "vid/1", stream.UpdateVideo{Title: "New", CollectionID: &empty}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteVideo(ctx, "vid 1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Fetch(ctx, "https://example.com/v.mp4", stream.FetchOptions{Title: "T", CollectionID: "col-1", Headers: map[string]string{"X-A": "b"}}); err != nil {
		t.Fatal(err)
	}
	golden(t, "video_requests", rec.String())
}

func TestUploadGolden(t *testing.T) {
	c, rec := fake(t, reply{200, `{"success":true,"statusCode":200}`})
	if err := c.Upload(ctx, "vid", io.MultiReader(strings.NewReader("12345")), stream.UploadOptions{Size: 5}); err != nil {
		t.Fatal(err)
	}
	golden(t, "upload", rec.String())
}

func TestUploadNotRetried(t *testing.T) {
	c, rec := fake(t, reply{500, `{}`}, reply{200, `{}`})
	if err := c.Upload(ctx, "vid", strings.NewReader("x"), stream.UploadOptions{}); err == nil {
		t.Fatal("want error")
	}
	if n := strings.Count(rec.String(), "PUT "); n != 1 {
		t.Fatalf("upload attempts = %d, want 1", n)
	}
}

func TestCollectionRequestsGolden(t *testing.T) {
	ok := reply{200, `{"success":true,"statusCode":200}`}
	c, rec := fake(t, reply{200, sample(t, "collection.json")}, reply{200, sample(t, "collection.json")}, ok, ok)
	col, err := c.CreateCollection(ctx, "Curso")
	if err != nil || col.Name != "Curso" || col.VideoCount != 2 || len(col.PreviewImageURL) != 1 {
		t.Fatalf("%+v %v", col, err)
	}
	if _, err := c.GetCollection(ctx, "col-1"); err != nil {
		t.Fatal(err)
	}
	if err := c.RenameCollection(ctx, "col-1", "Nuevo"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteCollection(ctx, "col-1"); err != nil {
		t.Fatal(err)
	}
	golden(t, "collection_requests", rec.String())
}

func TestDecodeVideo(t *testing.T) {
	c, _ := fake(t, reply{200, sample(t, "video.json")})
	v, err := c.GetVideo(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	if v.Title != "Intro ñ" || v.Status != stream.StatusFinished || !v.Status.Playable() ||
		v.LengthSeconds != 42 || v.Width != 1920 || v.CollectionID != "col-1" || v.DateUploaded.Year() != 2026 ||
		v.LibraryID != 123 || v.Framerate != 29.97 {
		t.Fatalf("%+v", v)
	}
}

func TestStatusString(t *testing.T) {
	for s, want := range map[stream.VideoStatus]string{0: "created", 4: "finished", 8: "jit_playlists_created", 99: "unknown(99)"} {
		if s.String() != want {
			t.Errorf("%d = %s, want %s", int(s), s, want)
		}
	}
}

func TestVideosIteratorPagesAndStops(t *testing.T) {
	p1 := reply{200, sample(t, "videos_page.json")}
	p2 := reply{200, `{"totalItems":3,"currentPage":2,"itemsPerPage":2,"items":[{"guid":"c","title":"C"}]}`}
	c, rec := fake(t, p1, p2)
	var got []string
	for v, err := range c.Videos(ctx, stream.ListOptions{ItemsPerPage: 2, CollectionID: "col-1", Search: "a b"}) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, v.GUID)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("got %v", got)
	}
	golden(t, "videos_pages", rec.String())

	// Early break must not fetch more pages.
	c, rec = fake(t, p1, p2)
	for _, err := range c.Videos(ctx, stream.ListOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if n := strings.Count(rec.String(), "GET "); n != 1 {
		t.Fatalf("fetched %d pages after break", n)
	}
}

func TestIteratorYieldsError(t *testing.T) {
	c, _ := fake(t, reply{401, `{"Message":"nope"}`})
	var errs int
	for _, err := range c.Collections(ctx, stream.ListOptions{}) {
		if !errors.Is(err, bunny.ErrUnauthorized) {
			t.Fatalf("err = %v", err)
		}
		errs++
	}
	if errs != 1 {
		t.Fatalf("errors yielded = %d", errs)
	}
}

func TestNotFound(t *testing.T) {
	c, _ := fake(t, reply{404, `{"success":false,"message":"gone","statusCode":404}`})
	if _, err := c.GetVideo(ctx, "x"); !errors.Is(err, bunny.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// The signed values must be the ones the signing package (verified against
// official vectors) produces for this library.
func TestSigning(t *testing.T) {
	c := stream.New("123", "apikey")
	exp := time.Unix(1700000000, 0)
	if got, want := c.SignTUS("vid", exp), signing.SignTUS("123", "apikey", "vid", exp); got != want {
		t.Fatalf("%+v != %+v", got, want)
	}
	u, err := url.Parse(c.EmbedURL("vid", "tokenkey", exp))
	if err != nil || !strings.HasPrefix(u.Path, "/embed/123/vid") || u.Query().Get("expires") != "1700000000" ||
		u.Query().Get("token") != signing.EmbedToken("tokenkey", "vid", exp) {
		t.Fatalf("%v %v", u, err)
	}
	if h := c.SignTUS("vid", exp).Headers(); h["LibraryId"] != "123" || h["VideoId"] != "vid" {
		t.Fatalf("%v", h)
	}
}
