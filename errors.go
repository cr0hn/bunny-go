package bunny

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// MaxErrorBody is the maximum number of response-body bytes kept in
// [APIError.Body].
const MaxErrorBody = 4096

// Sentinel errors matched by [errors.Is] against an [*APIError].
var (
	// ErrNotFound matches HTTP 404 responses.
	ErrNotFound = errors.New("bunny: not found")
	// ErrUnauthorized matches HTTP 401 (and 403) responses.
	ErrUnauthorized = errors.New("bunny: unauthorized")
	// ErrRateLimited matches HTTP 429 responses.
	ErrRateLimited = errors.New("bunny: rate limited")
)

// APIError is returned for any non-2xx response from bunny.net.
type APIError struct {
	// StatusCode is the HTTP status code.
	StatusCode int
	// Code is the bunny.net error key, when the body carries one.
	Code string
	// Message is the bunny.net error message, when the body carries one.
	Message string
	// Body is the raw response body, truncated to [MaxErrorBody] bytes.
	Body []byte
	// Truncated reports whether Body was cut.
	Truncated bool
}

// Error implements the error interface.
func (e *APIError) Error() string {
	s := fmt.Sprintf("bunny: HTTP %d", e.StatusCode)
	if e.Code != "" {
		s += " " + e.Code
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}

// Is makes [errors.Is] work with the package sentinel errors.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	}
	return false
}

// newAPIError builds an APIError from a (possibly truncated) body. bunny.net
// services use different shapes (ErrorKey/Message, HttpCode, message/statusCode);
// encoding/json matches field names case-insensitively, so one struct covers them.
func newAPIError(status int, body []byte, truncated bool) *APIError {
	e := &APIError{StatusCode: status, Body: body, Truncated: truncated}
	var p struct {
		ErrorKey string
		Message  string
	}
	if json.Unmarshal(body, &p) == nil {
		e.Code, e.Message = p.ErrorKey, p.Message
	}
	return e
}
