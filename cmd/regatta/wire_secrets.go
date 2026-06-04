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
	"log/slog"
	"os"
	"time"

	"github.com/trilamsr/regatta/internal/secrets"
)

// secretEnvOverrides maps canonical secret keys to the legacy env-var
// names that existing serve.go consumers already read. Setting these
// at boot means we do NOT touch the five env-var fan-out points in
// serve.go — they continue to call os.Getenv unchanged.
var secretEnvOverrides = map[string][]string{
	secrets.KeyAnthropic:    {"ANTHROPIC_API_KEY"},
	secrets.KeyGHToken:      {"GH_TOKEN", "GITHUB_TOKEN"},
	secrets.KeyBriefHMACs:   {"REGATTA_HMAC_KEYRING"},
	secrets.KeyAuditHMACKey: {"REGATTA_AUDIT_HMAC_KEY"},
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
