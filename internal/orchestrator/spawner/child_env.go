package spawner

import (
	"os"
	"strings"
)

// envStripAPIKey opts the spawner into removing ANTHROPIC_API_KEY +
// ANTHROPIC_AUTH_TOKEN from the spawned claude CLI child's env. Empty
// (default) preserves the prior pass-through behaviour; "1"/"true"
// activates strip. Operators on Linux with a file-backed claude-code
// subscription (~/.claude/.credentials.json) set this so agents bill
// against the subscription instead of pay-as-you-go Messages API
// credits. macOS keychain auth is not container-portable today, so
// strip-mode there would lock children out of any auth. See #1099.
const envStripAPIKey = "REGATTA_SPAWNER_STRIP_API_KEY"

// scrubbedChildEnvKeys names the env vars stripped when
// REGATTA_SPAWNER_STRIP_API_KEY is set. The daemon's own L4 +
// planner paths resolve the key in-process via secrets.Default, so
// stripping it from the child does not regress them.
var scrubbedChildEnvKeys = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
}

// scrubChildEnv conditionally removes the scrubbedChildEnvKeys from
// parent when REGATTA_SPAWNER_STRIP_API_KEY is enabled. Opt-in
// because the safe default for a fresh operator is whichever auth
// path the daemon was started with; stripping unconditionally would
// regress macOS Docker operators whose only auth surface is the
// inherited env. The slice is allocated fresh; parent is never
// mutated.
func scrubChildEnv(parent []string) []string {
	if !shouldStripAPIKey() || len(parent) == 0 {
		return parent
	}
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

func shouldStripAPIKey() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envStripAPIKey))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
