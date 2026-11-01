//go:build integration

package stream_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cr0hn/bunny-go/stream"
)

// Real bunny.net round trip. Needs BUNNY_LIBRARY_ID and BUNNY_LIBRARY_API_KEY
// and ffmpeg. It creates ONE collection named bunny-go-test-<random> with one
// tiny video, and deletes both at the end. It never lists-and-deletes and
// never touches anything it did not create.
func TestIntegrationRoundTrip(t *testing.T) {
	id, key := os.Getenv("BUNNY_LIBRARY_ID"), os.Getenv("BUNNY_LIBRARY_API_KEY")
	if id == "" || key == "" {
		t.Skip("BUNNY_LIBRARY_ID / BUNNY_LIBRARY_API_KEY not set")
	}
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found")
	}
	file := filepath.Join(t.TempDir(), "t.mp4")
	out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=10:duration=2",
		"-pix_fmt", "yuv420p", "-c:v", "libx264", file).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	c := stream.New(id, key)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	r := make([]byte, 6)
	_, _ = rand.Read(r)
	name := "bunny-go-test-" + hex.EncodeToString(r)
	col, err := c.CreateCollection(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.DeleteCollection(context.Background(), col.GUID); err != nil {
			t.Errorf("cleanup collection %s: %v", col.GUID, err)
		}
	})

	v, err := c.CreateVideo(ctx, name+"-video", stream.CreateVideoOptions{CollectionID: col.GUID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.DeleteVideo(context.Background(), v.GUID); err != nil {
			t.Errorf("cleanup video %s: %v", v.GUID, err)
		}
	})

	if err := c.Upload(ctx, v.GUID, bytes.NewReader(data), stream.UploadOptions{Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetVideo(ctx, v.GUID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != name+"-video" || got.CollectionID != col.GUID {
		t.Fatalf("unexpected video %+v", got)
	}
	// Listing filtered by OUR collection finds exactly our video.
	n := 0
	for lv, err := range c.Videos(ctx, stream.ListOptions{CollectionID: col.GUID}) {
		if err != nil {
			t.Fatal(err)
		}
		if lv.GUID != v.GUID {
			t.Fatalf("foreign video %s in our collection", lv.GUID)
		}
		n++
	}
	if n != 1 {
		t.Fatalf("collection lists %d videos, want 1", n)
	}
	if err := c.RenameCollection(ctx, col.GUID, name+"-renamed"); err != nil {
		t.Fatal(err)
	}
	if gc, err := c.GetCollection(ctx, col.GUID); err != nil || gc.Name != name+"-renamed" {
		t.Fatalf("collection after rename: %+v %v", gc, err)
	}
}
