package bunny_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/cr0hn/bunny-go"
)

func ExampleClient_Do() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"Title":"intro"}`)
	}))
	defer srv.Close()

	c := bunny.NewClient(bunny.WithBaseURL(srv.URL), bunny.WithAPIKey("secret"))
	var video struct{ Title string }
	err := c.Do(context.Background(), bunny.Request{Method: http.MethodGet, Path: "/library/1/videos/abc"}, &video)
	fmt.Println(video.Title, err)
	// Output: intro <nil>
}

func ExampleAPIError() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"ErrorKey":"video.not_found","Message":"no such video"}`)
	}))
	defer srv.Close()

	c := bunny.NewClient(bunny.WithBaseURL(srv.URL))
	err := c.Do(context.Background(), bunny.Request{Method: http.MethodGet, Path: "/x"}, nil)
	var apiErr *bunny.APIError
	fmt.Println(errors.Is(err, bunny.ErrNotFound), errors.As(err, &apiErr), apiErr.Code)
	// Output: true true video.not_found
}
