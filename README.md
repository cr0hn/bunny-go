# bunny-go

[![CI](https://github.com/cr0hn/bunny-go/actions/workflows/ci.yml/badge.svg)](https://github.com/cr0hn/bunny-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/cr0hn/bunny-go.svg)](https://pkg.go.dev/github.com/cr0hn/bunny-go)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

An independent, dependency-free Go SDK for [bunny.net](https://bunny.net):
Edge Storage, Stream and CDN URL signing. Standard library only, context-aware,
typed errors, safe retries.

> Status: v1 in progress. The shared core (HTTP transport, errors, retries)
> is available; the services below are coming soon.

## Install

```sh
go get github.com/cr0hn/bunny-go
```

Requires Go 1.24 or newer.

## Core

```go
c := bunny.NewClient(bunny.WithBaseURL("https://video.bunnycdn.com"), bunny.WithMaxRetries(3))
var out map[string]any
err := c.Do(ctx, bunny.Request{Method: http.MethodGet, Path: "/library/1/videos"}, &out)
if errors.Is(err, bunny.ErrNotFound) { /* ... */ }
var apiErr *bunny.APIError
if errors.As(err, &apiErr) { fmt.Println(apiErr.StatusCode, apiErr.Message) }
```

## Services

| Service | Status |
|---|---|
| Edge Storage (upload, download, list, delete, per region) | coming soon |
| Stream (videos, collections, direct and TUS upload, embed tokens) | coming soon |
| CDN URL signing (token authentication v1/v2) | coming soon |

## Design

- One client per service, functional options, injectable `*http.Client`.
- `*bunny.APIError` carries status, Bunny error code and message, and a bounded raw body; `errors.Is` works with `ErrNotFound`, `ErrUnauthorized`, `ErrRateLimited`.
- Retries with exponential backoff and jitter only for idempotent methods on 429/5xx, honouring `Retry-After` and the context.
- The specs the SDK is checked against live in [`api-reference/`](api-reference/).

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md). MIT licensed.
