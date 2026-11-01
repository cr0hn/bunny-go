// Package bunny is an independent Go SDK core for bunny.net.
//
// It provides the shared building blocks used by the service clients: an HTTP
// transport with an injectable [http.Client], typed errors ([APIError] and the
// sentinel errors [ErrNotFound], [ErrUnauthorized], [ErrRateLimited]) and
// retries with exponential backoff and jitter for idempotent requests. It uses
// only the standard library.
package bunny
