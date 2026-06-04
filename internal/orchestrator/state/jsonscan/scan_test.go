package jsonscan_test

import (
	"strings"
	"testing"

	// Blank-import rapid so the test binary registers -rapid.checks; the
	// repo-wide `make property-test` runs that flag across every package
	// under ./internal/orchestrator/state/... and unknown-flag aborts here
	// would block pre-push-check even though jsonscan itself has no rapid tests.
	_ "pgregory.net/rapid"

	"github.com/trilamsr/regatta/internal/orchestrator/state/jsonscan"
)

// TestScan_EmptyArray asserts '[]' yields zero elements with no error (#795).
func TestScan_EmptyArray(t *testing.T) {
	var got []string
	if err := jsonscan.Scan([]byte(`[]`), func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 elements, got %d (%v)", len(got), got)
	}
}

// TestScan_BasicArray asserts a plain string array decodes element-by-element (#795).
func TestScan_BasicArray(t *testing.T) {
	var got []string
	if err := jsonscan.Scan([]byte(`["a","b","c"]`), func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("want a,b,c got %v", got)
	}
}

// TestScan_EscapedQuote asserts \" inside string yields the literal quote (#795).
func TestScan_EscapedQuote(t *testing.T) {
	var got []string
	if err := jsonscan.Scan([]byte(`["a\"b"]`), func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0] != `a"b` {
		t.Fatalf("want [a\"b], got %v", got)
	}
}

// TestScan_EscapedBackslash asserts \\ yields a single backslash (#795).
func TestScan_EscapedBackslash(t *testing.T) {
	var got []string
	if err := jsonscan.Scan([]byte(`["a\\b"]`), func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0] != `a\b` {
		t.Fatalf("want a\\b, got %v", got)
	}
}

// TestScan_UnicodeEscape asserts \u escapes pass through verbatim per impl note (#795).
func TestScan_UnicodeEscape(t *testing.T) {
	var got []string
	// IDs never carry \uXXXX in our system; Unescape drops the leading
	// backslash and keeps subsequent bytes verbatim — pin the
	// non-standard JSON behavior so future refactors don't silently
	// "fix" it without updating callers.
	raw := []byte("[\"\\u0041\"]")
	if err := jsonscan.Scan(raw, func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0] != "u0041" {
		t.Fatalf("want u0041 (passthrough \\u), got %q", got)
	}
}

// TestScan_MalformedNoBracket asserts non-'[' lead returns a hard error (#795).
func TestScan_MalformedNoBracket(t *testing.T) {
	err := jsonscan.Scan([]byte(`{"x":1}`), func(s []byte) {})
	if err == nil {
		t.Fatal("want error for non-array input, got nil")
	}
}

// TestScan_UnterminatedString asserts a missing closing quote errors (#795).
func TestScan_UnterminatedString(t *testing.T) {
	err := jsonscan.Scan([]byte(`["abc`), func(s []byte) {})
	if err == nil {
		t.Fatal("want error for unterminated string, got nil")
	}
}

// TestScan_NonStringElement asserts non-string elements error out (#795).
func TestScan_NonStringElement(t *testing.T) {
	err := jsonscan.Scan([]byte(`[1,2,3]`), func(s []byte) {})
	if err == nil {
		t.Fatal("want error for non-string element, got nil")
	}
}

// TestScan_EmptyInput asserts empty bytes yield no error and no callback (#795).
func TestScan_EmptyInput(t *testing.T) {
	called := 0
	if err := jsonscan.Scan(nil, func(s []byte) { called++ }); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if called != 0 {
		t.Fatalf("want 0 callbacks, got %d", called)
	}
}

// TestScan_LeadingWhitespace asserts whitespace before '[' is skipped (#795).
func TestScan_LeadingWhitespace(t *testing.T) {
	var got []string
	if err := jsonscan.Scan([]byte("  \t\n\r[\"x\"]"), func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0] != "x" {
		t.Fatalf("want [x], got %v", got)
	}
}

// TestScan_VeryLongString asserts a 64KB element decodes without truncation (#795).
func TestScan_VeryLongString(t *testing.T) {
	long := strings.Repeat("a", 64*1024)
	raw := []byte(`["` + long + `"]`)
	var got string
	if err := jsonscan.Scan(raw, func(s []byte) {
		got = string(s)
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got != long {
		t.Fatalf("length mismatch: want %d got %d", len(long), len(got))
	}
}

// TestScan_MixedEscape asserts a string with both \" and \\ unescapes both (#795).
func TestScan_MixedEscape(t *testing.T) {
	var got []string
	if err := jsonscan.Scan([]byte(`["a\\b\"c"]`), func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0] != `a\b"c` {
		t.Fatalf("want a\\b\"c, got %q", got)
	}
}

// TestScan_BOM asserts a UTF-8 BOM prefix yields a hard error (no BOM tolerance) (#795).
func TestScan_BOM(t *testing.T) {
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`["x"]`)...)
	err := jsonscan.Scan(raw, func(s []byte) {})
	if err == nil {
		t.Fatal("want error for BOM-prefixed input, got nil")
	}
}

// TestScan_NullByteInString asserts a literal null byte inside a string passes through (#795).
func TestScan_NullByteInString(t *testing.T) {
	raw := []byte("[\"a\x00b\"]")
	var got []string
	if err := jsonscan.Scan(raw, func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(got) != 1 || got[0] != "a\x00b" {
		t.Fatalf("want a\\x00b, got %q", got[0])
	}
}

// TestScan_MultipleElementsWithWhitespace asserts inter-element whitespace tolerated (#795).
func TestScan_MultipleElementsWithWhitespace(t *testing.T) {
	var got []string
	if err := jsonscan.Scan([]byte(`[ "a" , "b" , "c" ]`), func(s []byte) {
		got = append(got, string(s))
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("want a,b,c got %v", got)
	}
}

// TestScan_AliasingContract asserts the byte slice passed to f aliases input for non-escaped strings (#795).
func TestScan_AliasingContract(t *testing.T) {
	raw := []byte(`["F-abc"]`)
	var ptr *byte
	if err := jsonscan.Scan(raw, func(s []byte) {
		if len(s) > 0 {
			ptr = &s[0]
		}
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Non-escaped path must alias the input buffer. The byte at offset 2 in
	// raw (`F`) is the first char of the element.
	if ptr == nil || ptr != &raw[2] {
		t.Fatalf("want callback slice to alias input at raw[2], got different backing")
	}
}

// TestUnescape_PassThroughNoEscape asserts a no-escape input returns equivalent bytes (#795).
func TestUnescape_PassThroughNoEscape(t *testing.T) {
	got := jsonscan.Unescape([]byte("abc"))
	if string(got) != "abc" {
		t.Fatalf("want abc, got %q", got)
	}
}

// TestUnescape_BackslashSequences asserts \\ and \" collapse to single bytes (#795).
func TestUnescape_BackslashSequences(t *testing.T) {
	got := jsonscan.Unescape([]byte(`a\"b\\c`))
	if string(got) != `a"b\c` {
		t.Fatalf("want a\"b\\c, got %q", got)
	}
}

// TestUnescape_TrailingBackslash asserts a dangling backslash passes through (#795).
func TestUnescape_TrailingBackslash(t *testing.T) {
	got := jsonscan.Unescape([]byte(`abc\`))
	if string(got) != `abc\` {
		t.Fatalf("want abc\\, got %q", got)
	}
}
