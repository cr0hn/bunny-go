package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cr0hn/bunny-go"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// jsonReqMarshal builds a JSON request. Marshalling plain maps cannot fail.
func jsonReqMarshal(method, path string, q url.Values, v any) bunny.Request {
	b, _ := json.Marshal(v)
	return jsonReq(method, path, q, b)
}

// postJSON POSTs v and decodes the reply into out (nil to discard). A POST is
// not idempotent, so it is never retried.
func (c *Client) postJSON(ctx context.Context, path string, v, out any) error {
	return c.core.Do(ctx, jsonReqMarshal(http.MethodPost, path, nil, v), out)
}

// Time is a [time.Time] that also decodes the zone-less timestamps bunny.net
// returns (for example "2026-11-01T10:20:30.123"), read as UTC.
type Time struct{ time.Time }

// UnmarshalJSON implements [json.Unmarshaler].
func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil || s == "" {
		return err
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if p, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			t.Time = p
			return nil
		}
	}
	return fmt.Errorf("stream: unrecognised timestamp %q", s)
}
