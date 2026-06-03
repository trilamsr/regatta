// Package slogutil collects tiny slog helpers shared across packages
// — avoids duplicate definitions that drift over time.
package slogutil

import "log/slog"

// AttrValue scans r.Attrs for key and returns its slog.Value plus
// found=true. Used by approval/security/l0 gate tests to assert on
// emitted attributes without depending on the order they were added.
func AttrValue(r slog.Record, key string) (slog.Value, bool) {
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
