package secrets

import (
	"context"
	"os"
	"strings"
)

// envFetcher reads canonical keys from env vars. Canonical-key to
// env-var name mapping: `regatta.foo_bar` → `REGATTA_FOO_BAR`. The
// env adapter is read-only — `regatta secret set` errors against an
// env-only system with a recovery doc link.
type envFetcher struct{}

// NewEnvFetcher returns the always-available env adapter.
func NewEnvFetcher() Fetcher { return envFetcher{} }

// Name identifies this adapter in audit + diagnostics output.
func (envFetcher) Name() string { return AdapterEnv }

// Get reads the canonical key from its env-var name. Empty string is
// treated as missing — a deployment that needs to express "empty
// allowed" sets the secret elsewhere in the chain.
func (envFetcher) Get(_ context.Context, key string) (Value, error) {
	if err := ValidateKey(key); err != nil {
		return Value{}, err
	}
	env := EnvVarName(key)
	v := os.Getenv(env)
	if v == "" {
		return Value{}, ErrNotFound
	}
	return NewValue([]byte(v)), nil
}

// EnvVarName maps a canonical key to its env-var name: dots → underscores
// + uppercase. Exported so the CLI status output can show the operator
// which env var to set.
func EnvVarName(key string) string {
	return strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// legacyEnvAliases pins back-compat env names to canonical keys so a
// build without a `secrets:` block reads byte-equal to pre-#911.
var legacyEnvAliases = map[string][]string{
	KeyAnthropic:     {"ANTHROPIC_API_KEY"},
	KeyGHToken:       {"GH_TOKEN", "GITHUB_TOKEN"},
	KeyBriefHMACs:    {"REGATTA_HMAC_KEYRING", "REGATTA_HMAC_KEY"},
	KeyAuditHMACKey:  {"REGATTA_AUDIT_HMAC_KEY"},
	KeyApprovalToken: {"REGATTA_APPROVAL_TOKEN_KEY"},
}

// LegacyEnvNames returns the back-compat env-var fallbacks for one canonical key in lookup order.
func LegacyEnvNames(key string) []string {
	out := append([]string(nil), legacyEnvAliases[key]...)
	return out
}

type aliasEnvFetcher struct{}

// NewAliasEnvFetcher returns the always-available legacy-env adapter.
func NewAliasEnvFetcher() Fetcher { return aliasEnvFetcher{} }

// Name identifies this adapter in audit + diagnostics output.
func (aliasEnvFetcher) Name() string { return AdapterAlias }

// Get returns the first legacy env value bound to key, else ErrNotFound so the chain continues.
func (aliasEnvFetcher) Get(_ context.Context, key string) (Value, error) {
	if err := ValidateKey(key); err != nil {
		return Value{}, err
	}
	for _, name := range legacyEnvAliases[key] {
		if v := os.Getenv(name); v != "" {
			return NewValue([]byte(v)), nil
		}
	}
	return Value{}, ErrNotFound
}
