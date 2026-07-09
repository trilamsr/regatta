package approval

import "testing"

func TestNormalize_StripsInvisibles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no_invisibles", "Hello, world!", "Hello, world!"},
		{"zwsp", "Hel​lo", "Hello"},
		{"zwj", "Hel‍lo", "Hello"},
		{"soft_hyphen", "co­-operate", "co-operate"},
		{"cgj_inline", "a͏b", "ab"},
		{"bidi_rlo", "a‮b", "ab"},
		{"arabic_letter_mark", "a؜b", "ab"},
		{"tag_block_ascii_smuggle", "Hello\U000E0048\U000E0069", "Hello"},
		{"variation_selector_long", "x\U000E0100y", "xy"},
		{"variation_selector_short", "x️y", "xy"},
		{"bom", "\uFEFFHello", "Hello"},
		{"object_replacement", "a￼b", "ab"},
		{"isolate_controls", "a⁦hostile⁩b", "ahostileb"},
		{"word_joiner", "a⁠b", "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := l0Normalize(c.in)
			if got != c.want {
				t.Errorf("l0Normalize(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}

func TestNormalize_StripBeforeNFC(t *testing.T) {
	t.Parallel()
	// "A" + CGJ (U+034F) + combining ring above (U+030A). CGJ blocks NFC
	// composition. Stripping CGJ before NFC must produce U+00C5 (Å),
	// matching the bare composed form.
	poisoned := "A͏̊"
	bare := "Å"
	if l0Normalize(poisoned) != l0Normalize(bare) {
		t.Fatalf("strip-before-NFC failure: l0Normalize(%q)=%q vs l0Normalize(%q)=%q",
			poisoned, l0Normalize(poisoned), bare, l0Normalize(bare))
	}
}

func TestNormalize_NFCEquivalence(t *testing.T) {
	t.Parallel()
	// é as U+00E9 (precomposed) vs U+0065 U+0301 (decomposed).
	a := "café"
	b := "café"
	if l0Normalize(a) != l0Normalize(b) {
		t.Fatalf("NFC equivalence failed: %q vs %q", l0Normalize(a), l0Normalize(b))
	}
}
