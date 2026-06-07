package secrets

import (
	"context"
	"errors"
	"fmt"
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
		return NewFileFetcher(key, spec.Path), nil
	case SourceKeychain, SourcePass:
		return Default(ctx), nil
	default:
		return nil, fmt.Errorf("unknown source %q (want env|keychain|pass|file)", spec.Source)
	}
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
	v := getenvNoEmpty(f.envName)
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
