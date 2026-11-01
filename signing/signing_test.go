package signing

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Vectors in testdata come from the official BunnyWay/BunnyCDN.TokenAuthentication
// examples (inputs.json/vectors.json: Python reference, checked there against
// every language; extra.json: official Python, cross-checked with the Node
// example) and from the official docs snippets run with PHP/Node (below).

type inputCase struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	UserIP           string `json:"userIp"`
	IsDirectory      bool   `json:"isDirectory"`
	PathAllowed      string `json:"pathAllowed"`
	CountriesAllowed string `json:"countriesAllowed"`
	CountriesBlocked string `json:"countriesBlocked"`
	IgnoreParams     bool   `json:"ignoreParams"`
	SpeedLimit       int    `json:"speedLimit"`
}

func load(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func TestSignURLOfficialVectors(t *testing.T) {
	var in struct {
		Key     string      `json:"key"`
		Expires int64       `json:"expires"`
		Host    string      `json:"host"`
		Cases   []inputCase `json:"cases"`
	}
	var want struct {
		Vectors []struct{ Name, SignedURL string } `json:"vectors"`
	}
	load(t, "inputs.json", &in)
	load(t, "vectors.json", &want)
	if len(in.Cases) != len(want.Vectors) || len(in.Cases) < 15 {
		t.Fatalf("cases %d vectors %d", len(in.Cases), len(want.Vectors))
	}
	for i, c := range in.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, err := SignURL(in.Host+c.Path, in.Key, Options{
				Expires: time.Unix(in.Expires, 0), IP: c.UserIP, Path: c.PathAllowed, InPath: c.IsDirectory,
				CountriesAllowed: split(c.CountriesAllowed), CountriesBlocked: split(c.CountriesBlocked),
				IgnoreParams: c.IgnoreParams, SpeedLimit: c.SpeedLimit,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != want.Vectors[i].SignedURL {
				t.Fatalf("got  %s\nwant %s", got, want.Vectors[i].SignedURL)
			}
		})
	}
}

func TestSignURLExtraVectors(t *testing.T) {
	var x struct {
		Extra []struct {
			Name      string `json:"name"`
			Path      string `json:"path"`
			Opts      map[string]any
			SignedURL string `json:"signedUrl"`
		} `json:"cdn_extra"`
	}
	load(t, "extra.json", &x)
	for _, c := range x.Extra {
		t.Run(c.Name, func(t *testing.T) {
			o := Options{Expires: time.Unix(1598024587, 0)}
			if v, ok := c.Opts["is_directory"].(bool); ok {
				o.InPath = v
			}
			if v, ok := c.Opts["ignore_params"].(bool); ok {
				o.IgnoreParams = v
			}
			if v, ok := c.Opts["path_allowed"].(string); ok {
				o.Path = v
			}
			if v, ok := c.Opts["user_ip"].(string); ok {
				o.IP = v
			}
			if v, ok := c.Opts["countries_allowed"].(string); ok {
				o.CountriesAllowed = split(v)
			}
			if v, ok := c.Opts["countries_blocked"].(string); ok {
				o.CountriesBlocked = split(v)
			}
			if v, ok := c.Opts["speed_limit"].(float64); ok {
				o.SpeedLimit = int(v)
			}
			got, err := SignURL("https://token-tester.b-cdn.net"+c.Path, "SecurityKey", o)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.SignedURL {
				t.Fatalf("got  %s\nwant %s", got, c.SignedURL)
			}
		})
	}
}

func TestSignURLErrors(t *testing.T) {
	exp := time.Unix(1598024587, 0)
	for name, tc := range map[string]struct {
		url, key string
		o        Options
	}{
		"empty key":      {"https://a.b-cdn.net/x", "", Options{Expires: exp}},
		"no expiry":      {"https://a.b-cdn.net/x", "k", Options{}},
		"relative url":   {"/x", "k", Options{Expires: exp}},
		"bad ip":         {"https://a.b-cdn.net/x", "k", Options{Expires: exp, IP: "nope"}},
		"repeated param": {"https://a.b-cdn.net/x?a=1&a=2", "k", Options{Expires: exp}},
		"ignore+country": {"https://a.b-cdn.net/x", "k", Options{Expires: exp, IgnoreParams: true, CountriesAllowed: []string{"ES"}}},
		"negative limit": {"https://a.b-cdn.net/x", "k", Options{Expires: exp, SpeedLimit: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := SignURL(tc.url, tc.key, tc.o); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// Official docs snippets (PHP md5 + base64 + strtr; the Node one gives the same).
func TestSignURLBasic(t *testing.T) {
	exp := time.Unix(1456761770, 0)
	for ip, want := range map[string]string{
		"":            "https://myzone.b-cdn.net/path/to/file.mp4?token=8i6m0aMGm3Zv3yqs02PkLw&expires=1456761770",
		"192.168.1.1": "https://myzone.b-cdn.net/path/to/file.mp4?token=6SR-XJc0vuTW-ZpyjIYDnw&expires=1456761770",
	} {
		got, err := SignURLBasic("https://myzone.b-cdn.net/path/to/file.mp4", "your_security_key", exp, ip)
		if err != nil || got != want {
			t.Fatalf("ip %q: got %s, %v\nwant %s", ip, got, err, want)
		}
	}
	if _, err := SignURLBasic("https://a.b-cdn.net/x", "", exp, ""); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestEmbed(t *testing.T) {
	const want = "a8617f6df2e9b55b65ac7112138c70417766d80614bfe146d0d9bb2bd21fef87" // PHP/Node on the docs example
	exp := time.Unix(1623440202, 0)
	const key, vid = "4742a81b-bf15-42fe-8b1c-8fcb9024c550", "32d140e2-e4f4-4eec-9d53-20371e9be607"
	if got := EmbedToken(key, vid, exp); got != want {
		t.Fatalf("token %s", got)
	}
	if got, w := EmbedURL("759", vid, key, exp), "https://player.mediadelivery.net/embed/759/"+vid+"?token="+want+"&expires=1623440202"; got != w {
		t.Fatalf("url %s", got)
	}
}

func TestSignTUS(t *testing.T) {
	u := SignTUS("759", "api-key-example", "32d140e2-e4f4-4eec-9d53-20371e9be607", time.Unix(1623440202, 0))
	if u.Signature != "daba789072a69f56e82d6610dbfff646c87427a868923576447d0ae14201d194" { // PHP/Node
		t.Fatal(u.Signature)
	}
	h := u.Headers()
	if h["AuthorizationExpire"] != "1623440202" || h["LibraryId"] != "759" || h["VideoId"] != u.VideoID {
		t.Fatalf("%v", h)
	}
}

func TestVerifyWebhook(t *testing.T) {
	body := []byte(`{"VideoGuid":"x","Status":3}`)
	const sig = "1ab5c404e72f216dceeb144b8bad39f742389c4f4351fb752d5e44a5fe251fce" // PHP/Node hmac
	if !VerifyWebhook("read-only-key", body, sig) {
		t.Fatal("valid signature rejected")
	}
	if VerifyWebhook("read-only-key", append(body, ' '), sig) || VerifyWebhook("other", body, sig) {
		t.Fatal("tampered input accepted")
	}
}
