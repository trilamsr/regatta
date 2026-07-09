package approval

import "log/slog"

// attrValue scans r.Attrs for key so tests assert on emitted attributes
// without depending on the order they were added.
func attrValue(r slog.Record, key string) (slog.Value, bool) {
	var out slog.Value
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			out = a.Value
			found = true
			return false
		}
		return true
	})
	return out, found
}
