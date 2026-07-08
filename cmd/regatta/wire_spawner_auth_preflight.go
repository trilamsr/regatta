package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// preflightSpawnerAuth refuses to boot the orchestrator when the configured spawner has no reachable credential path — boundary presence check only, never probes the credential itself (#1166 fix 3). The subscription branch accepts either a mounted ~/.claude OR a long-lived CLAUDE_CODE_OAUTH_TOKEN (issued by `claude setup-token`); the latter unblocks macOS Docker Desktop where the Keychain is not mountable into a Linux container.
func preflightSpawnerAuth(spawnerName string) error {
	if spawnerName != spawnerNameClaude {
		return nil
	}
	if spawner.IsFalsyEnv(os.Getenv("REGATTA_SPAWNER_STRIP_API_KEY")) {
		if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" {
			return errors.New("spawner auth precondition: REGATTA_SPAWNER_STRIP_API_KEY=0 (pay-as-you-go) but ANTHROPIC_API_KEY is empty. Set the key in .env or flip to subscription (unset the strip flag) and mount ~/.claude OR set CLAUDE_CODE_OAUTH_TOKEN (run `claude setup-token` on host)")
		}
		return nil
	}
	// Subscription path #1: long-lived OAuth token from `claude setup-token`.
	// Containerized hosts (macOS Docker Desktop) cannot mount the Keychain,
	// so this is the only subscription credential they can present.
	if strings.TrimSpace(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")) != "" {
		return nil
	}
	// Subscription path #2: mounted ~/.claude directory.
	home := os.Getenv("HOME")
	if home == "" {
		return errors.New("spawner auth precondition: subscription path requires $HOME to be set OR CLAUDE_CODE_OAUTH_TOKEN; the distroless image must export HOME=/home/nonroot (see #1180)")
	}
	claudeDir := filepath.Clean(filepath.Join(home, ".claude"))
	st, err := os.Stat(claudeDir) //nolint:gosec // G304/G703: HOME is operator-controlled boundary input; boundary gate trusts it after Clean
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("spawner auth precondition: subscription path expects %s to exist + contain readable credentials (mount ~/.claude via docker-compose.override.yml, OR set CLAUDE_CODE_OAUTH_TOKEN — run `claude setup-token` on host — OR flip REGATTA_SPAWNER_STRIP_API_KEY=0 + set ANTHROPIC_API_KEY for pay-as-you-go). Directory presence is necessary but NOT sufficient — on macOS Docker Desktop the mount completes empty because the token lives in Keychain; the CLAUDE_CODE_OAUTH_TOKEN path is the recommended workaround. See docs/operator/docker-compose.md §Spawner billing mode for the platform matrix", claudeDir)
		}
		return fmt.Errorf("spawner auth precondition: stat %s: %w", claudeDir, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("spawner auth precondition: %s exists but is not a directory", claudeDir)
	}
	return nil
}

