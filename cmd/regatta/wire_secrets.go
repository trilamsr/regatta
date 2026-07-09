// wire_secrets.go bridges internal/secrets into the serve boot path
// per PHASE-AUTONOMY W6 §5. Resolved values are exported into the
// process env so the legacy env-var readers (loadBriefKeyring at
// :486, audit signer at :271, LLM dispatcher, GitHub client) consume
// them unchanged. The Cache.Run goroutine handles SIGHUP rotation;
// watchSecretsExport republishes after every rotation so env-var
// readers see fresh values without a full re-exec.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/obs"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/secrets"
)

// startSecretsRotationLoop launches the SIGHUP-driven rotation
// goroutine with an audit-event callback. Separated from serve.go to
// keep that composition root under the file-size ceiling (#737).
func startSecretsRotationLoop(ctx context.Context, cache *secrets.Cache, fetcher secrets.Fetcher, slogger *slog.Logger, db *state.DB) {
	go cache.Run(ctx, fetcher, slogger, func(eventCtx context.Context, chain string, keys int) {
		payload := fmt.Sprintf(`{"chain":%q,"keys":%d}`, chain, keys)
		if recErr := db.RecordEvent(eventCtx, 0, string(obs.EventSecretsRotated), payload); recErr != nil {
			slogger.Warn("secrets_rotated.event_record_failed", "err", recErr)
		}
	})
}

// secretEnvOverrides maps canonical secret keys to the legacy env-var
// names that existing serve.go consumers already read. Setting these
// at boot means we do NOT touch the five env-var fan-out points in
// serve.go — they continue to call os.Getenv unchanged.
// envAnthropicAPIKey is the legacy env var name for the Anthropic API key, referenced from secretEnvOverrides + doctor.go.
const envAnthropicAPIKey = "ANTHROPIC_API_KEY" //nolint:gosec // env-var NAME, not a credential value

// envLinearAPIKey is the legacy env var name for the Linear API key (MAY-91), read by buildSpecAdapter after the router exports it.
const envLinearAPIKey = "LINEAR_API_KEY" //nolint:gosec // env-var NAME, not a credential value

var secretEnvOverrides = map[string][]string{
	secrets.KeyAnthropic:     {envAnthropicAPIKey},
	secrets.KeyLinear:        {envLinearAPIKey},
	secrets.KeyGHToken:       {"GH_TOKEN", "GITHUB_TOKEN"},
	secrets.KeyBriefHMACs:    {"REGATTA_HMAC_KEYRING"},
	secrets.KeyAuditHMACKey:  {"REGATTA_AUDIT_HMAC_KEY"},
	secrets.KeyApprovalToken: {"REGATTA_APPROVAL_TOKEN_KEY"},
}

// buildSecretFetcherFromRepo reads regatta.yaml at repoRoot, returning a Fetcher built from `secrets:` when present, else the Default chain. SecretSpec §11 mitigates yaml-typo risk via CUE rejection — so a non-ENOENT load error MUST surface (WARN + non-nil err) rather than silently fall back. A missing regatta.yaml stays silent and returns Default — zero-config deployments are the documented happy path (mirrors `buildSpecAdapter` #867 contract).
func buildSecretFetcherFromRepo(ctx context.Context, repoRoot string, logger *slog.Logger) (secrets.Fetcher, error) {
	cfgPath := filepath.Join(repoRoot, "regatta.yaml")
	cfg, loadErr := validate.LoadConfigFile(cfgPath)
	if loadErr != nil && !errors.Is(loadErr, fs.ErrNotExist) {
		if logger != nil {
			logger.Warn("secrets.config_load_failed", "path", cfgPath, "err", loadErr)
		}
		return nil, loadErr
	}
	return buildSecretFetcher(ctx, cfg)
}

// buildSecretFetcher returns the operator-configured Fetcher when
// regatta.yaml carries a `secrets:` block, else the platform Default
// chain (back-compat). Adapts the validate.Secrets view to the
// internal/secrets.Config view at the composition root.
func buildSecretFetcher(ctx context.Context, cfg *validate.Config) (secrets.Fetcher, error) {
	if cfg == nil || cfg.Secrets == nil {
		return secrets.Default(ctx), nil
	}
	return secrets.BuildFromConfig(ctx, adaptSecretsConfig(cfg.Secrets))
}

func adaptSecretsConfig(in *validate.Secrets) *secrets.Config {
	if in == nil {
		return nil
	}
	conv := func(s *validate.Secret) *secrets.SecretSpec {
		if s == nil {
			return nil
		}
		return &secrets.SecretSpec{Source: s.Source, Name: s.Name, Path: s.Path, KeyID: s.KeyID}
	}
	return &secrets.Config{
		AnthropicAPIKey: conv(in.AnthropicAPIKey),
		LinearAPIKey:    conv(in.LinearAPIKey),
		GHToken:         conv(in.GHToken),
		BriefHMAC:       conv(in.BriefHMAC),
		AuditHMAC:       conv(in.AuditHMAC),
		ApprovalToken:   conv(in.ApprovalToken),
	}
}

// exportSecretsToEnv walks the cache and sets each legacy env-var
// name from the resolved Value. We never overwrite an env value the
// operator pre-set — env wins per spec §5 footnote (operator-typed
// values are an explicit override).
func exportSecretsToEnv(ctx context.Context, cache *secrets.Cache, logger *slog.Logger) {
	for key, envNames := range secretEnvOverrides {
		v, src, ok := cache.Get(key)
		if !ok {
			continue
		}
		// Alias-source values already came from the same legacy env
		// names we'd be writing — re-exporting risks crossing schemas
		// (e.g. single-key REGATTA_HMAC_KEY value written into the
		// keyring-format REGATTA_HMAC_KEYRING slot).
		if src == secrets.AdapterAlias {
			continue
		}
		for _, env := range envNames {
			if os.Getenv(env) != "" {
				// Operator pre-set this env var — leave it alone.
				continue
			}
			_ = os.Setenv(env, string(v.Bytes()))
			if logger != nil {
				logger.LogAttrs(ctx, slog.LevelInfo, "secret_exported",
					slog.String("env", env),
					slog.String("source", src),
				)
			}
		}
	}
}

// watchSecretsExport polls the cache snapshot pointer on a slow tick
// and re-exports when the snapshot identity changes. Cheaper than
// wiring a notification channel through cache for the small set of
// env vars we care about; rotation latency budget is human-paced.
func watchSecretsExport(ctx context.Context, cache *secrets.Cache, logger *slog.Logger) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	last := cache.Snapshot()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cur := cache.Snapshot()
			if !mapEq(last, cur) {
				exportSecretsToEnv(ctx, cache, logger)
				last = cur
			}
		}
	}
}

// mapEq is a minimal string-map equality helper — the snapshot only
// carries source labels (never values), so this comparison never
// crosses a secret boundary.
func mapEq(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
