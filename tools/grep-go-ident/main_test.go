package main

import "testing"

// TestFirstIdentMatch asserts only real IDENT references count, while
// comment, string, char, and raw-string mentions are rejected (#1058).
func TestFirstIdentMatch(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
		hit  bool
	}{
		{
			name: "real call is a match",
			src:  "package p\nfunc f() { Wire() }\n",
			want: []string{"Wire"},
			hit:  true,
		},
		{
			name: "selector reference is a match",
			src:  "package p\nimport \"x\"\nfunc f() int { return x.Wire() }\n",
			want: []string{"Wire"},
			hit:  true,
		},
		{
			name: "line comment only is not a match",
			src:  "package p\nfunc f() { // Wire\n_ = 1 }\n",
			want: []string{"Wire"},
			hit:  false,
		},
		{
			name: "block comment only is not a match",
			src:  "package p\nfunc f() { /* Wire */ _ = 1 }\n",
			want: []string{"Wire"},
			hit:  false,
		},
		{
			name: "multiline block comment only is not a match",
			src:  "package p\nfunc f() {\n/*\nWire\n*/\n_ = 1 }\n",
			want: []string{"Wire"},
			hit:  false,
		},
		{
			name: "double-quoted string only is not a match",
			src:  "package p\nfunc f() { _ = \"Wire\" }\n",
			want: []string{"Wire"},
			hit:  false,
		},
		{
			name: "raw string only is not a match",
			src:  "package p\nfunc f() { _ = `Wire` }\n",
			want: []string{"Wire"},
			hit:  false,
		},
		{
			name: "char literal lookalike is not a match",
			src:  "package p\nfunc f() { _ = 'W' }\n",
			want: []string{"W"},
			hit:  false,
		},
		{
			name: "call plus comment still matches",
			src:  "package p\nimport \"x\"\nfunc f() { /* Wire */ x.Wire() }\n",
			want: []string{"Wire"},
			hit:  true,
		},
		{
			name: "no requested symbol present",
			src:  "package p\nfunc f() { Other() }\n",
			want: []string{"Wire"},
			hit:  false,
		},
		{
			name: "substring of an identifier is not a whole match",
			src:  "package p\nfunc f() { WireHarness() }\n",
			want: []string{"Wire"},
			hit:  false,
		},
		{
			name: "malformed source still finds a real ident",
			src:  "package p\nfunc f() { Wire( ",
			want: []string{"Wire"},
			hit:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := firstIdentMatch([]byte(tc.src), tc.want)
			if ok != tc.hit {
				t.Fatalf("firstIdentMatch hit=%v want %v (got %q)", ok, tc.hit, got)
			}
			if ok && got != tc.want[0] {
				t.Fatalf("matched %q want %q", got, tc.want[0])
			}
		})
	}
}
