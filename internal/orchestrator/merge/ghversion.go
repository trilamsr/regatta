package merge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/trilamsr/regatta/internal/orchestrator/prwatch"
)

// MinGHVersion — gh CLI floor for the merge worker. 2.40 is the
// release that shipped --match-head-commit, the load-bearing safety
// flag for the SHA-pinned merge call (spec §9.10, issue #656).
// Below this floor --match-head-commit silently fails, leaving the
// agent open to head-drift races.
const MinGHVersion = "2.40.0"

// ErrGhVersionUnsupported — boot-time refusal: gh is installed but
// older than MinGHVersion. serve.go converts this to exit 2 so the
// operator sees one clear failure instead of every merge attempt
// failing in a different way.
var ErrGhVersionUnsupported = errors.New("merge: gh CLI version below required floor")

// GhVersionProbe abstracts `gh --version` for tests. Reuses the same
// shape as prwatch.GHVersionProbe — kept package-local so the boot
// wiring in serve.go does not have to thread a cross-package type
// through buildMergeWiring.
type GhVersionProbe interface {
	Version(ctx context.Context) (string, error)
}

// VerifyGhVersion runs the boot-time gh-CLI version check (issue
// #656). Returns ErrGhVersionUnsupported when gh reports below
// MinGHVersion so the caller can log the floor + exit cleanly. A
// missing gh binary surfaces as a wrapped exec.ErrNotFound, NOT as
// ErrGhVersionUnsupported — operators distinguish "gh too old" from
// "gh not installed" at boot.
func VerifyGhVersion(ctx context.Context, probe GhVersionProbe, logger *slog.Logger) error {
	if probe == nil {
		probe = defaultGhVersionProbe{}
	}
	got, err := probe.Version(ctx)
	if err != nil {
		return fmt.Errorf("merge: gh version probe: %w", err)
	}
	ok, err := prwatch.VersionAtLeast(got, MinGHVersion)
	if err != nil {
		return fmt.Errorf("merge: parse gh version %q: %w", got, err)
	}
	if !ok {
		if logger != nil {
			logger.Error("merge.gh_version_too_old",
				"got", got, "floor", MinGHVersion,
				"reason", "regatta merge worker requires gh ≥ "+MinGHVersion+" for --match-head-commit safety")
		}
		return fmt.Errorf("%w: got %s, want ≥ %s", ErrGhVersionUnsupported, got, MinGHVersion)
	}
	if logger != nil {
		logger.Info("merge.gh_version_ok", "gh_version", got, "floor", MinGHVersion)
	}
	return nil
}

// defaultGhVersionProbe shells `gh --version` and returns the first
// line. Mirrors prwatch.GHCLIVersionProbe but kept local so the merge
// boot path has no cross-package construction dependency.
type defaultGhVersionProbe struct{}

// Version executes gh --version and returns the trimmed first line.
func (defaultGhVersionProbe) Version(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "--version") //nolint:gosec // G204: literal binary, no operator input
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("exec gh --version: %w (%s)", err, strings.TrimSpace(errb.String()))
	}
	line := string(bytes.SplitN(out.Bytes(), []byte{'\n'}, 2)[0])
	return strings.TrimSpace(line), nil
}
