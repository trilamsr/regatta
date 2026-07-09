package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// etagHash returns a hex-encoded sha256 of the JSON encoding of v so
// htmx 5s polls return 304 when the underlying row-set is unchanged
// (R-MEGA-2 P2). Empty input or marshal failure returns "" so the caller
// can skip the ETag header rather than emitting a misleading constant.
func etagHash(v any) string {
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
