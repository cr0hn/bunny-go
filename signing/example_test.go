package signing_test

import (
	"fmt"
	"time"

	"github.com/cr0hn/bunny-go/signing"
)

func ExampleSignURL() {
	u, _ := signing.SignURL("https://myzone.b-cdn.net/videos/stream1/playlist.m3u8", "your-security-key", signing.Options{
		Expires:          time.Unix(1598024587, 0),
		Path:             "/videos/stream1/",
		InPath:           true,
		CountriesAllowed: []string{"GB"},
	})
	fmt.Println(u)
	// Output: https://myzone.b-cdn.net/bcdn_token=HS256-h86lq4ksz5MkT_mBw80KrTvm-fscYpZbXvoHDy8CWZE&token_countries=GB&token_path=%2Fvideos%2Fstream1%2F&expires=1598024587/videos/stream1/playlist.m3u8
}

func ExampleEmbedURL() {
	fmt.Println(signing.EmbedURL("759", "32d140e2-e4f4-4eec-9d53-20371e9be607", "4742a81b-bf15-42fe-8b1c-8fcb9024c550", time.Unix(1623440202, 0)))
	// Output: https://player.mediadelivery.net/embed/759/32d140e2-e4f4-4eec-9d53-20371e9be607?token=a8617f6df2e9b55b65ac7112138c70417766d80614bfe146d0d9bb2bd21fef87&expires=1623440202
}

func ExampleSignTUS() {
	up := signing.SignTUS("759", "api-key-example", "32d140e2-e4f4-4eec-9d53-20371e9be607", time.Unix(1623440202, 0))
	fmt.Println(up.Headers()["AuthorizationSignature"])
	// Output: daba789072a69f56e82d6610dbfff646c87427a868923576447d0ae14201d194
}
