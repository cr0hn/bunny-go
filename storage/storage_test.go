package storage_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cr0hn/bunny-go"
	"github.com/cr0hn/bunny-go/storage"
)

var update = flag.Bool("update", false, "rewrite golden files")

// fake records each request as text (method, escaped URI, headers, body) and
// answers with the given status and body.
func fake(t *testing.T, status int, reply string) (*storage.Client, *strings.Builder) {
	t.Helper()
	var rec strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(&rec, "%s %s\n", r.Method, r.RequestURI)
		for _, h := range []string{"Accesskey", "Checksum", "Content-Type", "User-Agent"} {
			if v := r.Header.Get(h); v != "" {
				fmt.Fprintf(&rec, "%s: %s\n", h, v)
			}
		}
		fmt.Fprintf(&rec, "Content-Length: %d\nTransfer-Encoding: %v\n\n%s\n", r.ContentLength, r.TransferEncoding, body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return storage.New("zone", "pw", storage.WithEndpoint(srv.URL),
		storage.WithCoreOptions(bunny.WithBackoff(time.Millisecond, time.Millisecond))), &rec
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

func TestUploadGolden(t *testing.T) {
	c, rec := fake(t, 201, `{"HttpCode":201,"Message":"File uploaded."}`)
	sum := strings.Repeat("ab", 32)
	err := c.Upload(context.Background(), "/dir one/ñandú ü.txt", strings.NewReader("hello"),
		storage.UploadOptions{ContentType: "text/plain", SHA256: sum})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "upload", rec.String())
}

// A reader of unknown type with Size set must be sent with Content-Length,
// not chunked.
func TestUploadSizeNotChunked(t *testing.T) {
	c, rec := fake(t, 201, `{}`)
	body := io.MultiReader(bytes.NewReader([]byte("12345")))
	if err := c.Upload(context.Background(), "a.bin", body, storage.UploadOptions{Size: 5}); err != nil {
		t.Fatal(err)
	}
	golden(t, "upload_size", rec.String())
}

func TestDownloadGolden(t *testing.T) {
	c, rec := fake(t, 200, "file-bytes")
	rc, err := c.Download(context.Background(), "a/b c.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "file-bytes" {
		t.Fatalf("body %q", b)
	}
	golden(t, "download", rec.String())
}

func TestDownloadNotFound(t *testing.T) {
	c, _ := fake(t, 404, `{"HttpCode":404,"Message":"Object Not Found"}`)
	_, err := c.Download(context.Background(), "nope")
	if !errors.Is(err, bunny.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

const listing = `[{"ObjectName":"a.txt","Path":"/zone/d/","Length":5,"IsDirectory":false,"LastChanged":"2017-03-10T03:06:48.203","DateCreated":"2017-03-10T03:06:48"},
{"ObjectName":"sub","Path":"/zone/d/","Length":0,"IsDirectory":true,"LastChanged":"2017-03-10T03:06:48.203","DateCreated":"2017-03-10T03:06:48.203"}]`

func TestListGolden(t *testing.T) {
	c, rec := fake(t, 200, listing)
	objs, err := c.List(context.Background(), "d")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || objs[0].Name != "a.txt" || objs[0].Size != 5 || !objs[1].IsDirectory ||
		!objs[0].LastChanged.Equal(time.Date(2017, 3, 10, 3, 6, 48, 203e6, time.UTC)) {
		t.Fatalf("objs = %+v", objs)
	}
	golden(t, "list", rec.String())
	if _, err := c.List(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.String(), "GET /zone/ HTTP") && !strings.Contains(rec.String(), "GET /zone/\n") {
		t.Errorf("root list URL wrong: %s", rec)
	}
}

func TestStatAndExists(t *testing.T) {
	c, rec := fake(t, 200, listing)
	ctx := context.Background()
	o, err := c.Stat(ctx, "d/a.txt")
	if err != nil || o.Size != 5 {
		t.Fatalf("stat = %+v, %v", o, err)
	}
	if ok, err := c.Exists(ctx, "d/sub"); !ok || err != nil {
		t.Fatalf("dir exists = %v, %v", ok, err)
	}
	if ok, err := c.Exists(ctx, "d/missing"); ok || err != nil {
		t.Fatalf("missing exists = %v, %v", ok, err)
	}
	golden(t, "stat", strings.SplitN(rec.String(), "\n\n", 2)[0]+"\n")
}

func TestExistsPropagatesErrors(t *testing.T) {
	c, _ := fake(t, 401, `{}`)
	if ok, err := c.Exists(context.Background(), "x"); ok || !errors.Is(err, bunny.ErrUnauthorized) {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}

func TestDeleteGolden(t *testing.T) {
	c, rec := fake(t, 200, `{"HttpCode":200}`)
	ctx := context.Background()
	if err := c.Delete(ctx, "d/a b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteDir(ctx, "d/sub dir"); err != nil {
		t.Fatal(err)
	}
	golden(t, "delete", rec.String())
}

func TestInvalidPaths(t *testing.T) {
	c, rec := fake(t, 200, `[]`)
	ctx := context.Background()
	for _, p := range []string{"", "/", "a/../b", "./a", "a//b"} {
		if err := c.Delete(ctx, p); err == nil {
			t.Errorf("Delete(%q) accepted", p)
		}
		if err := c.Upload(ctx, p, strings.NewReader("x"), storage.UploadOptions{}); err == nil {
			t.Errorf("Upload(%q) accepted", p)
		}
	}
	for _, p := range []string{"", "/"} {
		if err := c.DeleteDir(ctx, p); err == nil {
			t.Errorf("DeleteDir(%q) accepted the zone root", p)
		}
	}
	if err := c.DeleteDir(ctx, "../x"); err == nil {
		t.Error("DeleteDir ../x accepted")
	}
	if err := c.Upload(ctx, "a", strings.NewReader("x"), storage.UploadOptions{SHA256: "zz"}); err == nil {
		t.Error("bad checksum accepted")
	}
	if rec.Len() != 0 {
		t.Errorf("requests were sent: %s", rec)
	}
}

func TestUploadRetriesSeekableBody(t *testing.T) {
	var n int
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		n++
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	c := storage.New("z", "pw", storage.WithEndpoint(srv.URL),
		storage.WithCoreOptions(bunny.WithBackoff(time.Millisecond, time.Millisecond)))
	if err := c.Upload(context.Background(), "f", bytes.NewReader([]byte("data")), storage.UploadOptions{}); err != nil {
		t.Fatal(err)
	}
	if n != 2 || bodies[0] != "data" || bodies[1] != "data" {
		t.Fatalf("n=%d bodies=%q", n, bodies)
	}
}

func TestRegionEndpoint(t *testing.T) {
	cases := map[storage.Region]string{
		storage.Falkenstein: "https://storage.bunnycdn.com",
		storage.NewYork:     "https://ny.storage.bunnycdn.com",
		storage.Sydney:      "https://syd.storage.bunnycdn.com",
	}
	for r, want := range cases {
		if got := r.Endpoint(); got != want {
			t.Errorf("%q: %s != %s", r, got, want)
		}
	}
}
