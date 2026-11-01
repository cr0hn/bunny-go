// Package signing builds the signed values bunny.net asks for, offline and
// with the standard library only: CDN token authentication URLs (advanced
// HMAC-SHA256 and the deprecated basic MD5 scheme), Stream embed view tokens,
// Stream TUS upload signatures and Stream webhook verification.
//
// Every function is pure: it never touches the network. The algorithms follow
// the official documentation and are checked in the tests against vectors
// produced by the official BunnyWay/BunnyCDN.TokenAuthentication examples.
package signing

import (
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // basic token authentication is defined as MD5 by bunny.net
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Options configures SignURL. Only Expires is required.
type Options struct {
	// Expires is the instant the URL stops being valid (sent as a Unix
	// timestamp in seconds). Required.
	Expires time.Time
	// IP locks the token to a client address (token IP validation must be
	// enabled on the pull zone). IPv6 addresses are bound at their /64.
	IP string
	// Path is the token_path directory scope, for example "/videos/stream1/".
	// When set, it is signed instead of the URL path.
	Path string
	// InPath puts the token in the path (/bcdn_token=...) instead of the query
	// string. Use it for HLS/DASH, where players resolve relative segment URLs.
	InPath bool
	// CountriesAllowed and CountriesBlocked are ISO 3166-1 alpha-2 codes.
	CountriesAllowed []string
	CountriesBlocked []string
	// IgnoreParams excludes the URL query from validation, so callers can
	// append arbitrary parameters after signing. The original query is not
	// carried into the signed URL, as in the official examples. It cannot be
	// combined with countries or SpeedLimit, which would be silently dropped.
	IgnoreParams bool
	// SpeedLimit is the download speed limit in kB/s; 0 means no limit.
	SpeedLimit int
}

// SignURL signs rawURL with the pull zone's Token Authentication Key using
// advanced token authentication (HMAC-SHA256).
func SignURL(rawURL, securityKey string, o Options) (string, error) {
	if securityKey == "" {
		return "", errors.New("signing: security key must not be empty")
	}
	if o.Expires.IsZero() {
		return "", errors.New("signing: Expires is required")
	}
	if o.SpeedLimit < 0 {
		return "", errors.New("signing: SpeedLimit must not be negative")
	}
	if o.IgnoreParams && (len(o.CountriesAllowed) > 0 || len(o.CountriesBlocked) > 0 || o.SpeedLimit > 0) {
		return "", errors.New("signing: IgnoreParams cannot be combined with countries or SpeedLimit")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("signing: invalid URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("signing: URL must be absolute")
	}
	params := map[string]string{}
	if o.IgnoreParams {
		params["token_ignore_params"] = "true"
	} else {
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "", fmt.Errorf("signing: invalid query: %w", err)
		}
		for k, v := range q {
			if len(v) > 1 {
				return "", fmt.Errorf("signing: multi-valued query parameter %q is not supported", k)
			}
			params[k] = v[0]
		}
		if len(o.CountriesAllowed) > 0 {
			params["token_countries"] = strings.Join(o.CountriesAllowed, ",")
		}
		if len(o.CountriesBlocked) > 0 {
			params["token_countries_blocked"] = strings.Join(o.CountriesBlocked, ",")
		}
		if o.SpeedLimit > 0 {
			params["limit"] = strconv.Itoa(o.SpeedLimit)
		}
	}
	if o.Path != "" {
		params["token_path"] = o.Path
	}
	var ipBytes []byte
	flags := ""
	if o.IP != "" {
		if ipBytes, err = ipForSigning(o.IP); err != nil {
			return "", err
		}
		flags = "1-"
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	signing := make([]string, len(keys))
	query := make([]string, len(keys))
	for i, k := range keys {
		signing[i] = k + "=" + params[k]
		query[i] = k + "=" + strings.ReplaceAll(url.QueryEscape(params[k]), "+", "%20")
	}
	signPath := o.Path
	if signPath == "" {
		signPath = u.Path
	}
	expires := strconv.FormatInt(o.Expires.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(securityKey))
	mac.Write([]byte(signPath))
	mac.Write([]byte(expires))
	mac.Write(ipBytes)
	mac.Write([]byte(strings.Join(signing, "&")))
	token := "HS256-" + flags + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	tail := ""
	if len(query) > 0 {
		tail = "&" + strings.Join(query, "&")
	}
	base := u.Scheme + "://" + u.Host
	if o.InPath {
		return base + "/bcdn_token=" + token + tail + "&expires=" + expires + u.Path, nil
	}
	return base + u.Path + "?token=" + token + tail + "&expires=" + expires, nil
}

// ipForSigning returns the bytes bunny.net signs: 4 bytes for IPv4, the /64
// prefix zero-padded to 16 bytes for IPv6.
func ipForSigning(s string) ([]byte, error) {
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return nil, fmt.Errorf("signing: %q is not a valid IP address", s)
	}
	if a.Is4() {
		b := a.As4()
		return b[:], nil
	}
	b := a.As16()
	for i := 8; i < 16; i++ {
		b[i] = 0
	}
	return b[:], nil
}

// SignURLBasic signs rawURL with basic token authentication (MD5). bunny.net
// deprecates it; prefer SignURL. ip is optional. The query string is kept and
// is not part of the signature.
func SignURLBasic(rawURL, securityKey string, expires time.Time, ip string) (string, error) {
	if securityKey == "" {
		return "", errors.New("signing: security key must not be empty")
	}
	if expires.IsZero() {
		return "", errors.New("signing: expires is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("signing: invalid URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("signing: URL must be absolute")
	}
	if ip != "" {
		if _, err := netip.ParseAddr(ip); err != nil {
			return "", fmt.Errorf("signing: %q is not a valid IP address", ip)
		}
	}
	e := strconv.FormatInt(expires.Unix(), 10)
	sum := md5.Sum([]byte(securityKey + u.Path + e + ip)) //nolint:gosec // see import
	token := base64.RawURLEncoding.EncodeToString(sum[:])
	q := ""
	if u.RawQuery != "" {
		q = u.RawQuery + "&"
	}
	return u.Scheme + "://" + u.Host + u.Path + "?" + q + "token=" + token + "&expires=" + e, nil
}

// EmbedToken returns the Stream embed view token (hex SHA256 of
// key+videoID+expires) for the library's Token Authentication Key.
func EmbedToken(securityKey, videoID string, expires time.Time) string {
	return sha256Hex(securityKey + videoID + strconv.FormatInt(expires.Unix(), 10))
}

// EmbedURL returns the signed player embed URL
// https://player.mediadelivery.net/embed/{libraryID}/{videoID}?token=...&expires=...
func EmbedURL(libraryID, videoID, securityKey string, expires time.Time) string {
	return "https://player.mediadelivery.net/embed/" + url.PathEscape(libraryID) + "/" + url.PathEscape(videoID) +
		"?token=" + EmbedToken(securityKey, videoID, expires) + "&expires=" + strconv.FormatInt(expires.Unix(), 10)
}

// TUSUpload holds the values a client needs for a presigned TUS upload to
// https://video.bunnycdn.com/tusupload.
type TUSUpload struct {
	LibraryID string
	VideoID   string
	Expires   time.Time
	Signature string
}

// Headers returns the TUS request headers: AuthorizationSignature,
// AuthorizationExpire, VideoId and LibraryId.
func (t TUSUpload) Headers() map[string]string {
	return map[string]string{
		"AuthorizationSignature": t.Signature,
		"AuthorizationExpire":    strconv.FormatInt(t.Expires.Unix(), 10),
		"VideoId":                t.VideoID,
		"LibraryId":              t.LibraryID,
	}
}

// SignTUS signs a TUS upload: hex SHA256 of libraryID+apiKey+expires+videoID.
// apiKey is the library API key; sign on the server, never in the browser.
func SignTUS(libraryID, apiKey, videoID string, expires time.Time) TUSUpload {
	return TUSUpload{
		LibraryID: libraryID,
		VideoID:   videoID,
		Expires:   expires,
		Signature: sha256Hex(libraryID + apiKey + strconv.FormatInt(expires.Unix(), 10) + videoID),
	}
}

// VerifyWebhook reports whether signature (header X-BunnyStream-Signature) is
// the valid v1 signature of the exact raw body, keyed with the library's
// Read-Only API key. The comparison is constant time.
func VerifyWebhook(readOnlyKey string, body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(readOnlyKey))
	mac.Write(body)
	return hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(strings.ToLower(signature)))
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
