package main

import "testing"

// TestParsePublicURL asserts the --public-url flag's URL→host extraction (#304).
func TestParsePublicURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		err  bool
	}{
		{"empty", "", "", false},
		{"https no port", "https://regatta.example.com", "regatta.example.com", false},
		{"https with port", "https://regatta.example.com:8443", "regatta.example.com:8443", false},
		{"http scheme", "http://internal.local:8080", "internal.local:8080", false},
		{"missing scheme", "regatta.example.com", "", true},
		{"garbage", "::not a url", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePublicURL(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("want err for %q, got host=%q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("host = %q, want %q", got, tc.want)
			}
		})
	}
}
