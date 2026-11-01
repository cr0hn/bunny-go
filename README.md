# bunny-go

[![CI](https://github.com/cr0hn/bunny-go/actions/workflows/ci.yml/badge.svg)](https://github.com/cr0hn/bunny-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/cr0hn/bunny-go.svg)](https://pkg.go.dev/github.com/cr0hn/bunny-go)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

An unofficial Go client for [bunny.net](https://bunny.net): Edge Storage, Stream,
and offline signing of CDN URLs, Stream embeds, TUS uploads and webhooks. It uses
only the standard library.

## Contents

- [Status](#status)
- [Install](#install)
- [Quick start](#quick-start)
  - [Storage](#storage)
  - [Stream](#stream)
  - [URL signing](#url-signing)
- [Configuration](#configuration)
- [Errors](#errors)
- [Signing in detail](#signing-in-detail)
- [Testing](#testing)
- [Not covered](#not-covered)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)

## Status

Pre-1.0 (`bunny.Version` is `0.1.0`). The API may change between minor versions. Requires Go 1.24 or newer.

| Package | Covers |
|---|---|
| `bunny` | Shared HTTP client: auth header, retries, typed errors |
| `storage` | Edge Storage: upload, download, list, stat, exists, delete |
| `stream` | Stream: videos, collections, paginated iterators, upload, fetch |
| `signing` | Offline: CDN token auth, Stream embed tokens, TUS signatures, webhook verification |

## Install

```sh
go get github.com/cr0hn/bunny-go
```

## Quick start

### Storage

```go
package main

import (
	"context"
	"io"
	"log"
	"os"
	"strings"

	"github.com/cr0hn/bunny-go/storage"
)

func main() {
	ctx := context.Background()
	c := storage.New("my-zone", os.Getenv("BUNNY_STORAGE_KEY"), storage.WithRegion(storage.NewYork))

	body := strings.NewReader("hello")
	err := c.Upload(ctx, "docs/hello.txt", body, storage.UploadOptions{
		ContentType: "text/plain",
		Size:        int64(body.Len()),
	})
	if err != nil {
		log.Fatal(err)
	}

	rc, err := c.Download(ctx, "docs/hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	log.Printf("%s", data)

	objs, err := c.List(ctx, "docs")
	if err != nil {
		log.Fatal(err)
	}
	for _, o := range objs {
		log.Println(o.Name, o.Size, o.IsDirectory)
	}

	if err := c.Delete(ctx, "docs/hello.txt"); err != nil {
		log.Fatal(err)
	}
}
```

Other methods: `Stat`, `Exists` (returns `false, nil` for a missing object) and
`DeleteDir`. Paths are relative to the zone. Empty, `.` and `..` segments are
rejected and each segment is percent-escaped.

`Upload` streams the body. If the body is an `io.Seeker`, the upload is retried
on transient errors; otherwise it is sent once. Set `UploadOptions.SHA256` to
have bunny.net reject a corrupted upload.

### Stream

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/cr0hn/bunny-go/stream"
)

func main() {
	ctx := context.Background()
	c := stream.New("12345", os.Getenv("BUNNY_LIBRARY_API_KEY"))

	v, err := c.CreateVideo(ctx, "Intro", stream.CreateVideoOptions{})
	if err != nil {
		log.Fatal(err)
	}

	f, err := os.Open("intro.mp4")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		log.Fatal(err)
	}
	if err := c.Upload(ctx, v.GUID, f, stream.UploadOptions{Size: st.Size()}); err != nil {
		log.Fatal(err)
	}

	// Iterate over every video; pages are fetched lazily.
	for v, err := range c.Videos(ctx, stream.ListOptions{Search: "intro"}) {
		if err != nil {
			log.Fatal(err)
		}
		log.Println(v.GUID, v.Title, v.Status, v.Status.Playable())
	}
}
```

Also available: `GetVideo`, `UpdateVideo`, `DeleteVideo`, `ListVideos` (one
`Page[Video]`), `Fetch` (bunny.net downloads a remote URL; the API returns no
video ID), and the same set for collections (`CreateCollection`,
`GetCollection`, `RenameCollection`, `DeleteCollection`, `ListCollections`,
`Collections`).

`Upload` sends the body once and never retries it.

### URL signing

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/cr0hn/bunny-go/signing"
)

func main() {
	u, err := signing.SignURL("https://myzone.b-cdn.net/private/file.zip", "token-auth-key", signing.Options{
		Expires: time.Now().Add(time.Hour),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(u)
}
```

Signing does no network I/O and needs no client.

## Configuration

Every service constructor takes its own options plus `WithCoreOptions(...)`,
which forwards options to the shared `bunny.Client`.

```go
package main

import (
	"net/http"
	"time"

	"github.com/cr0hn/bunny-go"
	"github.com/cr0hn/bunny-go/storage"
	"github.com/cr0hn/bunny-go/stream"
)

func main() {
	hc := &http.Client{Timeout: 30 * time.Second}
	core := storage.WithCoreOptions(
		bunny.WithHTTPClient(hc),
		bunny.WithMaxRetries(5),
		bunny.WithBackoff(100*time.Millisecond, 10*time.Second),
	)
	_ = storage.New("my-zone", "password", storage.WithRegion(storage.Falkenstein), core)
	_ = stream.New("12345", "api-key", stream.WithCoreOptions(bunny.WithHTTPClient(hc)))
}
```

**Storage regions.** Pass `storage.WithRegion` with the region of the zone's
primary storage. `storage.WithEndpoint` takes an arbitrary base URL.

| Constant | Region | Constant | Region |
|---|---|---|---|
| `Falkenstein` (default) | Falkenstein / Frankfurt | `Stockholm` | Stockholm |
| `London` | London | `SaoPaulo` | Sao Paulo |
| `NewYork` | New York | `Johannesburg` | Johannesburg |
| `LosAngeles` | Los Angeles | `Sydney` | Sydney |
| `Singapore` | Singapore | | |

**HTTP client and timeouts.** The default is `http.DefaultClient`, which has no
timeout. Pass your own `*http.Client` with `bunny.WithHTTPClient`, and use the
`context.Context` of each call for deadlines and cancellation.

**Retries.** Idempotent requests (GET, HEAD, PUT, DELETE, OPTIONS) are retried
on network errors, 429 and 5xx, up to 3 times by default
(`bunny.WithMaxRetries`, `0` disables). Backoff is exponential with jitter
(200 ms to 5 s by default, see `bunny.WithBackoff`). `Retry-After` is honoured
and capped at 60 seconds. A request with a body is retried only if it can be
replayed (`Request.GetBody` set, or a seekable upload body). POST is never
retried.

**Other options.** `bunny.WithUserAgent`, `bunny.WithBaseURL` and, per package,
`stream.WithBaseURL` (useful with `httptest`).

For endpoints the SDK does not wrap, use the core client directly:

```go
package main

import (
	"context"
	"net/http"

	"github.com/cr0hn/bunny-go"
)

func main() {
	c := bunny.NewClient(bunny.WithBaseURL("https://video.bunnycdn.com"), bunny.WithAPIKey("api-key"))
	var out map[string]any
	_ = c.Do(context.Background(), bunny.Request{Method: http.MethodGet, Path: "/library/12345/videos"}, &out)
}
```

`Do` decodes JSON; `Raw` returns the `*http.Response` and the caller closes it.

## Errors

Any non-2xx response is returned as `*bunny.APIError` with the status code, the
bunny.net error key and message when the body has them, and the raw body
truncated to `bunny.MaxErrorBody` bytes (`Truncated` tells you if it was cut).
`errors.Is` matches three sentinels:

| Sentinel | Status |
|---|---|
| `bunny.ErrNotFound` | 404 |
| `bunny.ErrUnauthorized` | 401 (and 403) |
| `bunny.ErrRateLimited` | 429 |

```go
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/cr0hn/bunny-go"
	"github.com/cr0hn/bunny-go/stream"
)

func main() {
	c := stream.New("12345", "api-key")
	_, err := c.GetVideo(context.Background(), "missing-guid")

	var apiErr *bunny.APIError
	switch {
	case errors.Is(err, bunny.ErrNotFound):
		fmt.Println("no such video")
	case errors.As(err, &apiErr):
		fmt.Println(apiErr.StatusCode, apiErr.Code, apiErr.Message)
	case err != nil:
		fmt.Println("transport error:", err)
	}
}
```

Context errors are returned as is, so `errors.Is(err, context.DeadlineExceeded)`
works.

## Signing in detail

### CDN URLs

`signing.SignURL` implements advanced token authentication (HMAC-SHA256) with
the pull zone's Token Authentication Key. Only `Expires` is required.

| `Options` field | Effect |
|---|---|
| `Path` | `token_path`: signs a directory scope such as `/videos/stream1/` instead of the file path |
| `InPath` | Puts the token in the path (`/bcdn_token=...`) instead of the query; use it for HLS/DASH so relative segment URLs inherit it |
| `CountriesAllowed`, `CountriesBlocked` | ISO 3166-1 alpha-2 codes |
| `IP` | Binds the token to a client IP (IPv6 at its /64). Enable token IP validation on the pull zone |
| `IgnoreParams` | Excludes the query from validation. Cannot be combined with countries or `SpeedLimit` |
| `SpeedLimit` | Download limit in kB/s |

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/cr0hn/bunny-go/signing"
)

func main() {
	u, err := signing.SignURL("https://myzone.b-cdn.net/videos/stream1/playlist.m3u8", "token-auth-key", signing.Options{
		Expires:          time.Now().Add(time.Hour),
		Path:             "/videos/stream1/",
		InPath:           true,
		CountriesAllowed: []string{"GB", "ES"},
		IP:               "203.0.113.7",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(u)

	// Deprecated MD5 scheme, for pull zones that still use it.
	b, err := signing.SignURLBasic("https://myzone.b-cdn.net/file.zip", "token-auth-key", time.Now().Add(time.Hour), "")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(b)
}
```

A query parameter with several values is rejected, because the signature would
be ambiguous.

### Stream embed

```go
package main

import (
	"fmt"
	"time"

	"github.com/cr0hn/bunny-go/signing"
	"github.com/cr0hn/bunny-go/stream"
)

func main() {
	exp := time.Now().Add(time.Hour)
	fmt.Println(signing.EmbedURL("12345", "video-guid", "library-token-key", exp))

	// The same through a Stream client.
	c := stream.New("12345", "library-api-key")
	fmt.Println(c.EmbedURL("video-guid", "library-token-key", exp))
}
```

`EmbedToken` returns just the token. The key is the library's Token
Authentication Key, not the API key.

### TUS uploads

`SignTUS` produces the headers for a presigned upload to `stream.TUSEndpoint`
(`https://video.bunnycdn.com/tusupload`), so a browser or app can upload
directly without seeing the library API key. Sign on the server.

```go
package main

import (
	"fmt"
	"time"

	"github.com/cr0hn/bunny-go/stream"
)

func main() {
	c := stream.New("12345", "library-api-key")
	up := c.SignTUS("video-guid", time.Now().Add(24*time.Hour))
	fmt.Println(stream.TUSEndpoint, up.Headers())
}
```

The headers are `AuthorizationSignature`, `AuthorizationExpire`, `VideoId`
and `LibraryId`. Create the video first with `CreateVideo`.

### Webhooks

`VerifyWebhook` checks the `X-BunnyStream-Signature` header against the exact
raw body, keyed with the library's Read-Only API key, in constant time. Do not
re-encode the JSON before verifying.

```go
package main

import (
	"io"
	"net/http"

	"github.com/cr0hn/bunny-go/signing"
)

func webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !signing.VerifyWebhook("read-only-api-key", body, r.Header.Get("X-BunnyStream-Signature")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	http.HandleFunc("/bunny/webhook", webhook)
	_ = http.ListenAndServe(":8080", nil)
}
```

The signing output is tested against the vectors in the official
[BunnyCDN.TokenAuthentication](https://github.com/BunnyWay/BunnyCDN.TokenAuthentication)
examples.

## Testing

```sh
make test               # unit tests with -race
make check              # golangci-lint, unit tests, go vet
make test-integration   # opt-in, talks to the real bunny.net API
```

Unit tests run against `httptest` servers and golden requests. Integration
tests carry the `integration` build tag and skip themselves when their
variables are missing:

| Variable | Used by |
|---|---|
| `BUNNY_STORAGE_ZONE`, `BUNNY_STORAGE_KEY` | storage |
| `BUNNY_STORAGE_REGION` or `BUNNY_STORAGE_ENDPOINT` | storage (region code such as `ny`, or full endpoint) |
| `BUNNY_LIBRARY_ID`, `BUNNY_LIBRARY_API_KEY` | stream (also needs `ffmpeg` to build a 2 s test clip) |

Integration tests only touch what they create: storage objects under
`_bunny-go-tests/<random>/` and one stream collection named
`bunny-go-test-<random>` with one video. They delete those at the end and never
list-and-delete. Still, do not point them at an account you are not willing to
touch.

## Not covered

- The rest of the bunny.net API: pull zones, DNS, billing, purge, Shield,
  Optimizer, statistics. Use `bunny.Client.Do` for those.
- Stream management beyond videos and collections (captions, chapters,
  moments, library settings). `stream.Video` models only the commonly used
  fields.
- A TUS client. The SDK signs TUS uploads; it does not perform them.
- Automatic pagination for Storage listings (the API returns the full
  directory).

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md). `make check` must pass, every
exported identifier needs a godoc comment, and behaviour changes need a test.
Commits follow Conventional Commits.

## Security

Report vulnerabilities privately, not in a public issue. See
[SECURITY.md](SECURITY.md).

## License

MIT, see [LICENSE](LICENSE).
