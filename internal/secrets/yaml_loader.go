package secrets

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// Source enum values mirror CUE schema #Secret.source.
const (
	SourceEnv      = "env"
	SourceKeychain = "keychain"
	SourcePass     = "pass"
	SourceFile     = "file"
)

// Spec is the per-key yaml entry shape.
type Spec struct {
	Source string `yaml:"source" json:"source"`
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
	Path   string `yaml:"path,omitempty" json:"path,omitempty"`
	KeyID  string `yaml:"key_id,omitempty" json:"key_id,omitempty"`
}

// Config is the Go view of the `secrets:` block, one field per canonical key.
type Config struct {
	AnthropicAPIKey *Spec `yaml:"anthropic_api_key,omitempty" json:"anthropic_api_key,omitempty"`
	GHToken         *Spec `yaml:"gh_token,omitempty" json:"gh_token,omitempty"`
	BriefHMAC       *Spec `yaml:"brief_hmac,omitempty" json:"brief_hmac,omitempty"`
	AuditHMAC       *Spec `yaml:"audit_hmac,omitempty" json:"audit_hmac,omitempty"`
	ApprovalToken   *Spec `yaml:"approval_token,omitempty" json:"approval_token,omitempty"`
}

// BuildFromConfig returns a Fetcher routing each yaml-mapped key to its source; nil cfg ⇒ Default (back-compat).
func BuildFromConfig(ctx context.Context, cfg *Config) (Fetcher, error) {
	def := Default(ctx)
	if cfg == nil {
		return def, nil
	}
	mapping := map[string]*Spec{
		KeyAnthropic:     cfg.AnthropicAPIKey,
		KeyGHToken:       cfg.GHToken,
		KeyBriefHMACs:    cfg.BriefHMAC,
		KeyAuditHMACKey:  cfg.AuditHMAC,
		KeyApprovalToken: cfg.ApprovalToken,
	}
	routed := map[string]Fetcher{}
	for key, spec := range mapping {
		if spec == nil {
			continue
		}
		f, err := fetcherForSpec(ctx, key, spec)
		if err != nil {
			return nil, fmt.Errorf("secrets[%s]: %w", key, err)
		}
		routed[key] = f
	}
	if len(routed) == 0 {
		return def, nil
	}
	return routedFetcher{routed: routed, fallback: def}, nil
}

func fetcherForSpec(ctx context.Context, key string, spec *Spec) (Fetcher, error) {
	switch spec.Source {
	case SourceEnv:
		if spec.Name == "" {
			return nil, errors.New("source=env requires name")
		}
		return namedEnvFetcher{key: key, envName: spec.Name}, nil
	case SourceFile:
		if spec.Path == "" {
			return nil, errors.New("source=file requires path")
		}
		f := NewFileFetcher(key, spec.Path)
		if key == KeyBriefHMACs {
			if spec.KeyID == "" {
				return nil, errors.New("source=file for brief_hmac requires key_id")
			}
			return briefHMACKeyringFormatter{inner: f, keyID: spec.KeyID}, nil
		}
		return f, nil
	case SourceKeychain:
		if spec.Name != "" {
			return newNamedKeychainFetcher(spec.Name), nil
		}
		return Default(ctx), nil
	case SourcePass:
		if spec.Name != "" {
			return newNamedPassFetcher(spec.Name), nil
		}
		return Default(ctx), nil
	default:
		return nil, fmt.Errorf("unknown source %q (want env|keychain|pass|file)", spec.Source)
	}
}

// briefHMACKeyringFormatter reshapes a brief_hmac raw value into the `keyID:hex(raw)` shape parseBriefKeyring expects — without this, a raw file dump exported into REGATTA_HMAC_KEYRING silently fails parse → empty keyring (#932 HIGH-2).
type briefHMACKeyringFormatter struct {
	inner Fetcher
	keyID string
}

func (b briefHMACKeyringFormatter) Name() string { return b.inner.Name() }

func (b briefHMACKeyringFormatter) Get(ctx context.Context, key string) (Value, error) {
	v, err := b.inner.Get(ctx, key)
	if err != nil {
		return Value{}, err
	}
	raw := v.Bytes()
	if len(raw) == 0 {
		return Value{}, ErrNotFound
	}
	return NewValue([]byte(b.keyID + ":" + hex.EncodeToString(raw))), nil
}

// unsupportedFetcher reports its adapter name but always returns
// ErrUnsupported — used as the cross-platform stub for named
// keychain/pass binds on platforms that lack the backend (#934).
type unsupportedFetcher struct{ adapter string }

func (u unsupportedFetcher) Name() string                          { return u.adapter }
func (u unsupportedFetcher) Get(_ context.Context, _ string) (Value, error) {
	return Value{}, ErrUnsupported
}

type namedEnvFetcher struct {
	key     string
	envName string
}

func (f namedEnvFetcher) Name() string { return AdapterEnv }

func (f namedEnvFetcher) Get(_ context.Context, key string) (Value, error) {
	if key != f.key {
		return Value{}, ErrNotFound
	}
	if err := ValidateKey(key); err != nil {
		return Value{}, err
	}
	v := os.Getenv(f.envName)
	if v == "" {
		return Value{}, ErrNotFound
	}
	return NewValue([]byte(v)), nil
}

type routedFetcher struct {
	routed   map[string]Fetcher
	fallback Fetcher
}

func (r routedFetcher) Name() string { return "yaml→default" }

// Get walks the routed entry for key when present, then the Default chain. The Default-chain fallback on routed-miss is INTENTIONAL: a yaml-configured `source: env, name: GH_TOKEN_REVIEWER` whose env var is unset SHOULD still resolve via legacy aliases (back-compat with pre-#911). Operators who want strict routing get it implicitly by leaving the legacy env unset.
func (r routedFetcher) Get(ctx context.Context, key string) (Value, error) {
	if err := ValidateKey(key); err != nil {
		return Value{}, err
	}
	if f, ok := r.routed[key]; ok {
		v, err := f.Get(ctx, key)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrUnsupported) {
			return Value{}, err
		}
	}
	return r.fallback.Get(ctx, key)
}

func (r routedFetcher) GetWithSource(ctx context.Context, key string) (Value, string, error) {
	if err := ValidateKey(key); err != nil {
		return Value{}, "", err
	}
	if f, ok := r.routed[key]; ok {
		v, err := f.Get(ctx, key)
		if err == nil {
			return v, f.Name(), nil
		}
		if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrUnsupported) {
			return Value{}, f.Name(), err
		}
	}
	return GetWithSource(ctx, r.fallback, key)
}
