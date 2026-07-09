package secrets

import (
	"context"
	"testing"
)

// TestRoutedFetcher_KeychainNameAbsentFallsBackToCanonical asserts a keychain source with no name field preserves pre-#934 Default-chain behaviour.
func TestRoutedFetcher_KeychainNameAbsentFallsBackToCanonical(t *testing.T) {
	cfg := &Config{
		AnthropicAPIKey: &SecretSpec{Source: SourceKeychain},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	rf, ok := f.(routedFetcher)
	if !ok {
		t.Fatalf("BuildFromConfig returned %T, want routedFetcher", f)
	}
	inner, ok := rf.routed[KeyAnthropic]
	if !ok {
		t.Fatalf("routed entry for %s missing", KeyAnthropic)
	}
	// Default(ctx) is the back-compat shape: a chained fetcher whose
	// Name() is the platform chain label, NOT "keychain" (which would
	// indicate the named-fetcher wrapper).
	if inner.Name() == AdapterKeychain {
		t.Fatalf("name absent should route through Default chain, got %s", inner.Name())
	}
}

// TestRoutedFetcher_KeychainNameBindsCustomAccount asserts a keychain source with name= builds a named-account fetcher (#934).
func TestRoutedFetcher_KeychainNameBindsCustomAccount(t *testing.T) {
	cfg := &Config{
		AnthropicAPIKey: &SecretSpec{Source: SourceKeychain, Name: "regatta/anthropic"},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	rf, ok := f.(routedFetcher)
	if !ok {
		t.Fatalf("BuildFromConfig returned %T, want routedFetcher", f)
	}
	inner, ok := rf.routed[KeyAnthropic]
	if !ok {
		t.Fatalf("routed entry for %s missing", KeyAnthropic)
	}
	// Named-account binding MUST surface as a keychain-typed fetcher,
	// NOT the Default chain (otherwise spec.Name is silently ignored).
	if inner.Name() != AdapterKeychain {
		t.Fatalf("name set should bind keychain fetcher, got %s", inner.Name())
	}
}

// TestRoutedFetcher_PassNameBindsCustomEntry asserts a pass source with name= builds a named-entry fetcher (#934).
func TestRoutedFetcher_PassNameBindsCustomEntry(t *testing.T) {
	cfg := &Config{
		GHToken: &SecretSpec{Source: SourcePass, Name: "regatta/gh_token_reviewer"},
	}
	f, err := BuildFromConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}
	rf, ok := f.(routedFetcher)
	if !ok {
		t.Fatalf("BuildFromConfig returned %T, want routedFetcher", f)
	}
	inner, ok := rf.routed[KeyGHToken]
	if !ok {
		t.Fatalf("routed entry for %s missing", KeyGHToken)
	}
	if inner.Name() != AdapterPass {
		t.Fatalf("name set should bind pass fetcher, got %s", inner.Name())
	}
}
