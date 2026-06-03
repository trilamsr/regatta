// Package canon hosts the canonical-JSON encoder shared by the
// outputs journal (internal/orchestrator/state) and the brief/predicate
// pipeline (internal/program). Single implementation site: every byte
// landing in work_item_outputs.output_json AND every byte hashed for a
// predicate input passes through CanonicaliseJSON. Replay invariant
// (spec §4) depends on the canonical form being byte-stable across
// orchestrator versions.
//
// Lives in its own package because spec decision 18 pins
// "one-way program→state import" — state cannot pull from program, so
// canon must live downstream of both.
package canon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// Marshal returns the canonical JSON encoding of v.
//
// One-step alternative to json.Marshal+CanonicaliseJSON for callers
// that hold a typed Go value (handoff signing, gate-result signing,
// approval-event audit payloads). Routes through CanonicaliseJSON so
// the output is byte-identical to the []byte path — every signed byte
// in the system passes through the same canonicaliser, eliminating
// the MAC-divergence class of bug (issue #553).
//
// Note on dup-keys: a Go-typed value cannot express duplicate object
// keys, so the dup-key rejection in CanonicaliseJSON is unreachable
// from this entry point — the property still holds for the []byte path.
func Marshal(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canon: marshal: %w", err)
	}
	return CanonicaliseJSON(raw)
}

// CanonicaliseJSON returns the canonical encoding of payload: object
// keys sorted by byte-order of their UTF-8 encoded names (equivalent
// to codepoint order for the ASCII-dominant key universe we target),
// no insignificant whitespace, numbers in encoding/json's default
// form, strings as-emitted by the stdlib encoder. Idempotent:
// CanonicaliseJSON(CanonicaliseJSON(x)) == CanonicaliseJSON(x).
//
// Rejects (with error, never panic):
//   - any decode error from the stdlib parser
//   - trailing non-whitespace bytes after the first JSON value
//   - any object body that declares the same key twice (silent
//     last-wins would mask content-SHA drift across re-canonicalisation
//     and is therefore fatal — producers must fix the source)
//
// Predicate fuzz tests in package program exercise adversarial input.
func CanonicaliseJSON(payload []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, fmt.Errorf("canon: decode: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("canon: trailing garbage after JSON value")
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeValue walks the token stream to detect duplicate object keys.
// We cannot use json.Unmarshal into map[string]any because the stdlib
// silently keeps the last value when a key repeats — the very bug we
// must surface. A token-level walk preserves duplicate visibility at
// every nesting depth.
func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeFromToken(dec, tok)
}

func decodeFromToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return decodeObject(dec)
		case '[':
			return decodeArray(dec)
		default:
			return nil, fmt.Errorf("unexpected delim %q", t)
		}
	default:
		return t, nil
	}
}

func decodeObject(dec *json.Decoder) (map[string]any, error) {
	out := map[string]any{}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("object key not a string: %v", keyTok)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("duplicate object key %q", key)
		}
		val, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
	if _, err := dec.Token(); err != nil && err != io.EOF {
		return nil, err
	}
	return out, nil
}

func decodeArray(dec *json.Decoder) ([]any, error) {
	var out []any
	for dec.More() {
		val, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		out = append(out, val)
	}
	if _, err := dec.Token(); err != nil && err != io.EOF {
		return nil, err
	}
	return out, nil
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
