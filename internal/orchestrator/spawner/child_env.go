package spawner

import (
	"log/slog"
	"os"
	"strings"
	"sync"
)

// envStripAPIKey opts the spawner into routing children to claude-code subscription auth instead of pay-as-you-go Messages API billing. Opt-in because macOS keychain creds are not container-portable (#1099).
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
			"hint", "ANTHROPIC_API_KEY + ANTHROPIC_AUTH_TOKEN scrubbed from spawned claude CLI env; children authenticate via subscription credentials. Unset REGATTA_SPAWNER_STRIP_API_KEY to restore pre-#1099 pass-through.",
		)
	})
}

func resetStripLogOnceForTest() { stripLogOnce = sync.Once{} }

func shouldStripAPIKey() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envStripAPIKey))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
