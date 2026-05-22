// Package l0 implements the L0 spec-immutability gate.
//
// L0 is deterministic and mandatory: it diffs the spec source between
// PR base and HEAD, asserts that each touched acceptance-criterion
// entry's only change is the state flip, and fails hard on any text
// edit (visible or invisible).
//
// Normative behavior is specified in testdata/gates/l0/README.md.
package l0

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Normalize strips the Default_Ignorable ∪ Bidi_Control closure plus
// U+FFFC, then applies NFC. Strip happens BEFORE NFC because some
// stripped code points (notably U+034F CGJ and U+200C ZWNJ) actively
// block NFC composition; a strip-after-NFC ordering would leave their
// effect intact.
func Normalize(s string) string {
	return norm.NFC.String(strip(s))
}

func strip(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if shouldStrip(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// shouldStrip returns true for every code point in the union of:
//   - Unicode Default_Ignorable_Code_Point=Yes (UAX #44)
//   - Unicode Bidi_Control=Yes (UAX #9)
//   - U+FFFC OBJECT REPLACEMENT CHARACTER
//
// Pinned to Unicode 15.1. Bumping Unicode requires a fixture refresh.
func shouldStrip(r rune) bool {
	switch {
	case r == 0x00AD: // SOFT HYPHEN
		return true
	case r == 0x034F: // COMBINING GRAPHEME JOINER — NFC-bypass primitive
		return true
	case r == 0x061C: // ARABIC LETTER MARK — bidi
		return true
	case r >= 0x115F && r <= 0x1160: // Hangul fillers
		return true
	case r >= 0x17B4 && r <= 0x17B5: // Khmer invisible inherent vowels
		return true
	case r >= 0x180B && r <= 0x180F: // Mongolian variation selectors + vowel separator
		return true
	case r >= 0x200B && r <= 0x200F: // zero-width chars + LRM/RLM
		return true
	case r >= 0x202A && r <= 0x202E: // bidi embedding/override
		return true
	case r >= 0x2060 && r <= 0x206F: // word joiner, invisible math, isolate controls
		return true
	case r >= 0xFE00 && r <= 0xFE0F: // variation selectors 1–16
		return true
	case r == 0xFEFF: // BOM / ZWNBSP
		return true
	case r >= 0xFFF0 && r <= 0xFFF8: // reserved invisibles
		return true
	case r == 0xFFFC: // object replacement
		return true
	case r >= 0x1BCA0 && r <= 0x1BCA3: // shorthand format controls
		return true
	case r >= 0x1D173 && r <= 0x1D17A: // musical format controls
		return true
	case r >= 0xE0000 && r <= 0xE0FFF: // Tags block + supplementary variation selectors
		return true
	}
	return false
}
