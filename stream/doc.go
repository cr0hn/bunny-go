// Package stream is a client for the bunny.net Stream API (video library):
// videos, collections, direct upload, fetch from URL, and the signed values
// needed for TUS uploads and token-protected embeds.
//
// The library API key authenticates every call. Signing is delegated to the
// [github.com/cr0hn/bunny-go/signing] package; this package does not
// implement a TUS client, it only builds what a TUS client needs.
package stream
