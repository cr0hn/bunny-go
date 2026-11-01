package storage_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/cr0hn/bunny-go/storage"
)

func ExampleClient_Upload() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fmt.Println(r.Method, r.URL.EscapedPath(), string(b))
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	// Use storage.WithRegion(storage.NewYork) etc. against the real service.
	c := storage.New("myzone", "zone-password", storage.WithEndpoint(srv.URL))
	err := c.Upload(context.Background(), "docs/hello world.txt", strings.NewReader("hi"),
		storage.UploadOptions{ContentType: "text/plain"})
	fmt.Println(err)
	// Output:
	// PUT /myzone/docs/hello%20world.txt hi
	// <nil>
}

func ExampleClient_List() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `[{"ObjectName":"a.txt","Length":2,"IsDirectory":false}]`)
	}))
	defer srv.Close()

	c := storage.New("myzone", "zone-password", storage.WithEndpoint(srv.URL))
	objs, _ := c.List(context.Background(), "docs")
	fmt.Println(objs[0].Name, objs[0].Size)
	// Output: a.txt 2
}
