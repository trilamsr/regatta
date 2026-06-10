package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
)

// preflightSpawnerAuth refuses to boot the orchestrator when the configured spawner has no path to a credential. The check is strictly a boundary gate: it never tries to USE the credentials (no claude --version probe, no token decode), only confirms that ONE of the documented auth paths is reachable so the scheduler does not later burn the entire work-item queue producing exit_reason=auth_precondition_failed (#1166 fix proposal 3).
//
// Spawner=claude rules:
//   - REGATTA_SPAWNER_STRIP_API_KEY ∈ {1, true, yes, on, ""} → subscription path. Require $HOME/.claude reachable + readable.
//   - REGATTA_SPAWNER_STRIP_API_KEY ∈ {0, false, no, off} → pay-as-you-go path. Require ANTHROPIC_API_KEY non-empty.
//
// Any other spawner (stub, etc) is skipped — no claude CLI to authenticate. Closes the operator-loud-error surface for #1166 / #1177 / #1181.
func preflightSpawnerAuth(spawnerName string) error {
	if spawnerName != "claude" {
		return nil
	}
	if spawner.IsFalsyEnv(os.Getenv("REGATTA_SPAWNER_STRIP_API_KEY")) {
		if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) == "" {
			return errors.New("spawner auth precondition: REGATTA_SPAWNER_STRIP_API_KEY=0 (pay-as-you-go) but ANTHROPIC_API_KEY is empty. Set the key in .env or flip to subscription (unset the strip flag) and mount ~/.claude")
		}
		return nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		return errors.New("spawner auth precondition: subscription path requires $HOME to be set; the distroless image must export HOME=/home/nonroot (see #1180)")
	}
	claudeDir := filepath.Join(home, ".claude")
	st, err := os.Stat(claudeDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("spawner auth precondition: subscription path expects %s to exist + contain readable credentials (mount ~/.claude via docker-compose.override.yml, or flip REGATTA_SPAWNER_STRIP_API_KEY=0 + set ANTHROPIC_API_KEY for pay-as-you-go). Directory presence is necessary but NOT sufficient — on macOS Docker Desktop the mount completes empty because the token lives in Keychain; see docs/operator/docker-compose.md §Spawner billing mode for the platform matrix", claudeDir)
		}
		return fmt.Errorf("spawner auth precondition: stat %s: %w", claudeDir, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("spawner auth precondition: %s exists but is not a directory", claudeDir)
	}
	return nil
}

