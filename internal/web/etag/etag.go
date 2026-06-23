// Package etag computes weak ETags for dashboard panel responses so
// htmx 5s polls return 304 when the underlying row-set is unchanged
// (R-MEGA-2 P2).
package etag

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Hash returns a hex-encoded sha256 of the JSON encoding of v. Empty
// input or marshal failure returns "" so the caller can skip the ETag
// header rather than emitting a misleading constant.
func Hash(v any) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
