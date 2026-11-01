// Package storage is a client for the bunny.net Edge Storage HTTP API:
// upload, download, list and delete files in a storage zone.
//
// The zone password (not the account API key) authenticates every call. The
// endpoint depends on the zone's primary region; see [Region].
package storage
