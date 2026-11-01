package stream_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/cr0hn/bunny-go/stream"
)

func ExampleClient_Videos() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"totalItems":1,"currentPage":1,"itemsPerPage":100,"items":[{"guid":"abc","title":"Intro","status":4}]}`)
	}))
	defer srv.Close()

	c := stream.New("123", "library-api-key", stream.WithBaseURL(srv.URL))
	for v, err := range c.Videos(context.Background(), stream.ListOptions{}) {
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(v.GUID, v.Title, v.Status)
	}
	// Output: abc Intro finished
}

func ExampleClient_SignTUS() {
	c := stream.New("123", "library-api-key")
	up := c.SignTUS("video-guid", time.Unix(1700000000, 0))
	fmt.Println(stream.TUSEndpoint)
	fmt.Println(up.Headers()["VideoId"], up.Headers()["AuthorizationExpire"])
	// Output:
	// https://video.bunnycdn.com/tusupload
	// video-guid 1700000000
}

func ExampleClient_EmbedURL() {
	c := stream.New("123", "library-api-key")
	u := c.EmbedURL("video-guid", "token-auth-key", time.Unix(1700000000, 0))
	fmt.Println(u[:len("https://player.mediadelivery.net/embed/123/video-guid?token=")])
	// Output: https://player.mediadelivery.net/embed/123/video-guid?token=
}
