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
