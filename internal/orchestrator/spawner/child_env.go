package spawner

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

// envStripAPIKey controls whether the spawner scrubs ANTHROPIC_* keys from spawned claude children. Default: STRIP (children auth via ~/.claude subscription, NOT pay-as-you-go Messages API). Set to "0"/"false"/"no"/"off" to opt OUT and pass through the parent ANTHROPIC_API_KEY (e.g. macOS host without ~/.claude mount). Per operator ask 2026-06-09: default subscription path for children, fall back to token only when explicitly disabled (#1099 follow-up).
const envStripAPIKey = "REGATTA_SPAWNER_STRIP_API_KEY" //nolint:gosec // env-var NAME, not a credential value

var scrubbedChildEnvKeys = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
}

func scrubChildEnv(parent []string) []string {
	if !shouldStripAPIKey() || len(parent) == 0 {
		return parent
	}
	logStripOnce()
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			out = append(out, kv)
			continue
		}
		key := kv[:eq]
		drop := false
		for _, k := range scrubbedChildEnvKeys {
			if key == k {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

var stripLogOnce sync.Once

func logStripOnce() {
	stripLogOnce.Do(func() {
		slog.Default().Info("spawner.api_key_stripped_from_children",
			"env_var", envStripAPIKey,
			"hint", "ANTHROPIC_API_KEY + ANTHROPIC_AUTH_TOKEN scrubbed from spawned claude CLI env; children authenticate via subscription credentials at ~/.claude. Set REGATTA_SPAWNER_STRIP_API_KEY=0 to opt OUT and pass through the parent token (pay-as-you-go billing).",
		)
	})
}

func resetStripLogOnceForTest() { stripLogOnce = sync.Once{} }

// shouldStripAPIKey returns true (strip → subscription path) by default; explicit opt-out via REGATTA_SPAWNER_STRIP_API_KEY=0|false|no|off passes the parent ANTHROPIC_API_KEY through to children (pay-as-you-go billing). Empty env = default behavior.
func shouldStripAPIKey() bool {
	return !isFalsyEnv(os.Getenv(envStripAPIKey))
}

// isFalsyEnv normalizes a free-form env-var string to a bool — recognized falsy tokens are 0|false|no|off (case-insensitive). Used by every spawner env knob to keep the lexicon uniform.
func isFalsyEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off":
		return true
	}
	return false
}
