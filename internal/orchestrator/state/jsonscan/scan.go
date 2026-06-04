// Package jsonscan walks the JSON-array-of-strings shape used by work_items.depends_on_features; split out of state/ to break a future import cycle (#795 #739).
package jsonscan

import "fmt"

// Scan invokes f on each element of a JSON string array; the slice aliases raw for unescaped elements (must not be retained past f's return) and supports only \" and \\ — the marshaler's emit set for work-item IDs.
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

// Unescape collapses \" and \\ (the only sequences the work-item-ID marshaler emits) and passes every other byte through — work-item IDs never carry unicode escapes, so a full JSON decoder is overkill.
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
