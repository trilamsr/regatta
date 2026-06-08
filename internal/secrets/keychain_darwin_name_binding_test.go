//go:build darwin

package secrets

import (
	"context"
	"os/exec"
	"testing"
)

// TestKeychain_NameBindsCustomAccount_Darwin asserts the keychain fetcher passes spec.Name as `-a <account>` instead of the canonical key (#934).
func TestKeychain_NameBindsCustomAccount_Darwin(t *testing.T) {
	var capturedArgs []string
	prev := keychainCommand
	keychainCommand = func(name string, args ...string) *exec.Cmd {
		capturedArgs = append([]string{name}, args...)
		// echo a sentinel value; security(8) exits 0 with stdout = password
		return exec.Command("/bin/echo", "-n", "sentinel-value")
	}
	t.Cleanup(func() { keychainCommand = prev })

	f := newNamedKeychainFetcher("regatta/anthropic")
	v, err := f.Get(context.Background(), KeyAnthropic)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(v.Bytes()) != "sentinel-value" {
		t.Fatalf("got %q want sentinel-value", v.Bytes())
	}

	// Assert `-a regatta/anthropic` is in the captured args, NOT `-a regatta.anthropic_api_key`.
	var sawAccount bool
	for i, a := range capturedArgs {
		if a == "-a" && i+1 < len(capturedArgs) {
			got := capturedArgs[i+1]
			if got != "regatta/anthropic" {
				t.Fatalf("-a arg = %q, want regatta/anthropic (#934 name binding)", got)
			}
			sawAccount = true
		}
	}
	if !sawAccount {
		t.Fatalf("no -a flag in args: %v", capturedArgs)
	}
}

// TestKeychain_NameAbsentUsesCanonicalKey_Darwin asserts back-compat — empty account preserves `-a <canonical-key>`.
func TestKeychain_NameAbsentUsesCanonicalKey_Darwin(t *testing.T) {
	var capturedArgs []string
	prev := keychainCommand
	keychainCommand = func(name string, args ...string) *exec.Cmd {
		capturedArgs = append([]string{name}, args...)
		return exec.Command("/bin/echo", "-n", "back-compat")
	}
	t.Cleanup(func() { keychainCommand = prev })

	// Direct construction with empty account → preserves canonical key path.
	f := keychainFetcher{service: "regatta"}
	if _, err := f.Get(context.Background(), KeyAnthropic); err != nil {
		t.Fatalf("Get: %v", err)
	}
	for i, a := range capturedArgs {
		if a == "-a" && i+1 < len(capturedArgs) {
			got := capturedArgs[i+1]
			if got != KeyAnthropic {
				t.Fatalf("-a arg = %q, want %q (back-compat: empty account ⇒ canonical key)", got, KeyAnthropic)
			}
		}
	}
}
