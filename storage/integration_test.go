//go:build integration

package storage_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cr0hn/bunny-go"
	"github.com/cr0hn/bunny-go/storage"
)

// Real bunny.net round trip. Needs BUNNY_STORAGE_ZONE, BUNNY_STORAGE_KEY and
// BUNNY_STORAGE_REGION (e.g. "ny") or BUNNY_STORAGE_ENDPOINT. It only creates
// and deletes objects under _bunny-go-tests/<random>/; it never lists-and-deletes.
func TestIntegrationRoundTrip(t *testing.T) {
	zone, key := os.Getenv("BUNNY_STORAGE_ZONE"), os.Getenv("BUNNY_STORAGE_KEY")
	if zone == "" || key == "" {
		t.Skip("BUNNY_STORAGE_ZONE / BUNNY_STORAGE_KEY not set")
	}
	opt := storage.WithRegion(storage.Region(os.Getenv("BUNNY_STORAGE_REGION")))
	if ep := os.Getenv("BUNNY_STORAGE_ENDPOINT"); ep != "" {
		opt = storage.WithEndpoint(ep)
	}
	c := storage.New(zone, key, opt)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	r := make([]byte, 8)
	_, _ = rand.Read(r)
	dir := "_bunny-go-tests/" + hex.EncodeToString(r)
	t.Cleanup(func() {
		// Deletes only the directory this test created (name is random and never empty).
		if err := c.DeleteDir(context.Background(), dir); err != nil {
			t.Errorf("cleanup of %s: %v", dir, err)
		}
	})

	payload := strings.Repeat("bunny-go ñ ü\n", 100)
	sum := sha256.Sum256([]byte(payload))
	file := dir + "/sub dir/ñandú.txt"
	if err := c.Upload(ctx, file, strings.NewReader(payload), storage.UploadOptions{
		ContentType: "text/plain", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(payload)),
	}); err != nil {
		t.Fatal("upload:", err)
	}
	if ok, err := c.Exists(ctx, file); !ok || err != nil {
		t.Fatalf("exists = %v, %v", ok, err)
	}
	o, err := c.Stat(ctx, file)
	if err != nil || o.Size != int64(len(payload)) {
		t.Fatalf("stat = %+v, %v", o, err)
	}
	rc, err := c.Download(ctx, file)
	if err != nil {
		t.Fatal("download:", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(got) != payload {
		t.Fatalf("download mismatch: %d bytes", len(got))
	}
	objs, err := c.List(ctx, dir+"/sub dir")
	if err != nil || len(objs) != 1 || objs[0].Name != "ñandú.txt" {
		t.Fatalf("list = %+v, %v", objs, err)
	}
	if err := c.Delete(ctx, file); err != nil {
		t.Fatal("delete:", err)
	}
	if _, err := c.Download(ctx, file); !errors.Is(err, bunny.ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}
