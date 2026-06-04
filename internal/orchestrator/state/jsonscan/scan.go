// Package jsonscan provides an allocation-light walker for the
// JSON-array-of-strings shape that work_items.depends_on_features
// always carries — pulled out of state/ to break a future
// import cycle when state is split (closes-part-of #795 #739).
package jsonscan

import "fmt"

// Scan walks a JSON array of strings and invokes f on each unquoted
// element. The byte slice handed to f aliases raw for non-escaped
// elements and must not be retained past f's return; escaped elements
// receive a freshly allocated buffer. Supports \" and \\ escapes only —
// other backslash sequences pass through verbatim, matching the
// upstream marshaler's emit set for work-item IDs.
func Scan(raw []byte, f func(s []byte)) error {
	i := 0
	for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
		i++
	}
	if i >= len(raw) || raw[i] != '[' {
		if i >= len(raw) {
			return nil
		}
		return fmt.Errorf("expected '[', got %q", raw[i])
	}
	i++
	for i < len(raw) {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r' || raw[i] == ',') {
			i++
		}
		if i >= len(raw) || raw[i] == ']' {
			return nil
		}
		if raw[i] != '"' {
			return fmt.Errorf("expected '\"', got %q at offset %d", raw[i], i)
		}
		i++
		start := i
		hasEscape := false
		for i < len(raw) && raw[i] != '"' {
			if raw[i] == '\\' {
				hasEscape = true
				i += 2
				continue
			}
			i++
		}
		if i >= len(raw) {
			return fmt.Errorf("unterminated string")
		}
		if !hasEscape {
			f(raw[start:i])
		} else {
			f(Unescape(raw[start:i]))
		}
		i++
	}
	return nil
}

// Unescape collapses the two backslash sequences the work-item-ID
// marshaler can emit (\" and \\) and passes every other byte through —
// the work-item ID charset never contains unicode escapes, so the
// general JSON-string decoder would be overkill.
func Unescape(s []byte) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			out = append(out, s[i+1])
			i++
			continue
		}
		out = append(out, s[i])
	}
	return out
}
