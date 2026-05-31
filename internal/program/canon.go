// Package program — canonical JSON for the outputs journal.
//
// Determinism boundary: every byte landing in work_item_outputs.output_json
// passes through canonicaliseJSON. Replay invariant (spec §4) depends on
// the canonical form being byte-stable across orchestrator versions.
// Single implementation site; do NOT inline json.Marshal elsewhere when
// hashing.
package program

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// canonicaliseJSON returns the canonical encoding of payload: object
// keys sorted lexicographically, no insignificant whitespace, numbers
// in encoding/json's default form, strings as-emitted by the stdlib
// encoder. Idempotent: canonicaliseJSON(canonicaliseJSON(x)) == canonicaliseJSON(x).
//
// Invalid JSON returns an error. The function never panics on
// adversarial input; predicate fuzz tests exercise this.
func canonicaliseJSON(payload []byte) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("program: canonicalise decode: %w", err)
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch tv := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(tv))
		for k := range tv {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeCanonical(buf, tv[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []any:
		buf.WriteByte('[')
		for i, item := range tv {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		b, err := json.Marshal(tv)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}
