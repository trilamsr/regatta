package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/trilamsr/regatta/internal/authz"
	"github.com/trilamsr/regatta/internal/authz/policies/disk"
	"github.com/trilamsr/regatta/internal/authz/policies/embedded"
	"github.com/trilamsr/regatta/internal/authz/policies/reload"
	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
	"github.com/trilamsr/regatta/internal/orchestrator/review"
)

// buildAuthorizer constructs the W8 OPA authorizer and (when the
// operator declares safety.authz.policy_dir) spawns the hot-reload
// goroutine bound to ctx. Empty / missing regatta.yaml ⇒ embed.FS
// default-deny only; the authorizer is still hydrated so call sites
// always see a non-nil store.
//
// Reloader lifecycle: bound to ctx — SIGINT/SIGTERM cancel the parent,
// which drains both the watcher and signal goroutines. SIGHUP is
// claimed by the reloader's own signal.Notify, which does not steal
// from the parent context's signal.NotifyContext (it listens on a
// disjoint signal set).
func buildAuthorizer(ctx context.Context, repoRoot string, slogger *slog.Logger) (*authz.OPAAuthorizer, error) {
	cfg, _ := validateconfig.LoadConfigFile(filepath.Join(repoRoot, "regatta.yaml"))
	authzCfg := cfg.AuthzConfig()

	// disk.Loader with embedded fallback is the canonical wiring: an
	// empty / missing policy_dir falls through to embed.FS so single-
	// tenant deployments boot zero-config.
	loader := &disk.Loader{Fallback: embedded.NewLoader()}
	if authzCfg != nil && authzCfg.PolicyDir != "" {
		dir := authzCfg.PolicyDir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(repoRoot, dir)
		}
		loader.Dir = dir
	}

	az, err := authz.NewOPAAuthorizer(authz.Config{Loader: loader})
	if err != nil {
		return nil, fmt.Errorf("new authorizer: %w", err)
	}
	if err := az.Hydrate(ctx); err != nil {
		return nil, fmt.Errorf("hydrate: %w", err)
	}

	// Reloader only spins up when policy_dir is set — there is nothing
	// to hot-reload for an embed.FS-only deployment.
	if loader.Dir == "" {
		return az, nil
	}

	r := &reload.Reloader{
		Authorizer: az,
		Loader:     loader,
		Tenant:     authz.DefaultTenant,
		Logger:     slogger,
	}
	if d := parseAuthzDebounce(authzCfg.ReloadDebounce); d > 0 {
		r.Debounce = d
	}
	if authzCfg.ReloadSighup != nil && !*authzCfg.ReloadSighup {
		r.DisableSighup = true
	}
	if authzCfg.ReloadFsnotify != nil && !*authzCfg.ReloadFsnotify {
		r.DisableFsnotify = true
	}
	// OnStart fires after BOTH triggers (watcher + SIGHUP handler) are
	// registered — commit 6341256 added the SIGHUP-readiness gate so
	// operators (and tests) can rely on this log as the
	// pid-can-receive-SIGHUP signal.
	r.OnStart = func() {
		slogger.Info("authz reload: ready",
			"policy_dir", loader.Dir,
			"sighup_enabled", !r.DisableSighup,
			"fsnotify_enabled", !r.DisableFsnotify,
		)
	}
	go func() {
		if err := r.Run(ctx); err != nil {
			slogger.Error("authz reload: exited", "err", err)
		}
	}()
	return az, nil
}

// parseAuthzDebounce parses the CUE-validated `^[0-9]+(ms|s)$` debounce
// string. Returns 0 on empty / parse error — reload.Reloader.Run then
// falls back to reload.DefaultDebounce. The CUE schema already rejects
// malformed values at validate-config time, so the parse-error branch
// covers only a forward-compat drift between schema + Go parsing.
func parseAuthzDebounce(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}

// startReviewReconciler constructs the W7 L4-as-review Approver and
// (when env opts in) spins the in-process Reconciler. Empty env keeps
// the Approver nil so default-off behaviour matches the spec
// (regatta.yaml::gates.l4_posts_review opt-in). #623: L4-side
// producers Enqueue signed Verdicts; Run serializes the Approver's
// GH calls. Returns an error on misconfig only — the nil/no-op path
// returns (nil) so callers stay flat.
func startReviewReconciler(ctx context.Context, slogger *slog.Logger) error {
	rev, err := buildReviewApprover(slogger)
	if err != nil {
		return fmt.Errorf("review approver: %w", err)
	}
	if rev == nil {
		return nil
	}
	slogger.Info("review.approver_ready",
		"reviewer", os.Getenv("GH_USER_REVIEWER"),
		"author_bot", os.Getenv("GH_USER_BOT"))
	rec, err := review.NewReconciler(review.ReconcilerConfig{Approver: rev, Logger: slogger})
	if err != nil {
		return fmt.Errorf("review reconciler: %w", err)
	}
	go rec.Run(ctx)
	return nil
}

// buildReviewApprover constructs the W7 L4-as-review Approver from
// env-driven config. Returns (nil, nil) — opt-in via env — when any of
// the required vars (REGATTA_REVIEW_REPO, GH_TOKEN_REVIEWER,
// GH_USER_REVIEWER, GH_USER_BOT) is empty so default-off matches spec
// §2: "Default-off; opt-in via regatta.yaml: gates.l4_posts_review:
// true". Env over yaml keeps the secret out of the file + avoids a CUE
// schema change for this wave; yaml-driven config can land alongside
// the install-service surface (spec §12 carry-forward).
func buildReviewApprover(logger *slog.Logger) (*review.Approver, error) {
	repo := os.Getenv("REGATTA_REVIEW_REPO")
	token := os.Getenv("GH_TOKEN_REVIEWER")
	reviewer := os.Getenv("GH_USER_REVIEWER")
	authorBot := os.Getenv("GH_USER_BOT")
	if repo == "" || token == "" || reviewer == "" || authorBot == "" {
		return nil, nil
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("REGATTA_REVIEW_REPO=%q: want owner/name", repo)
	}
	// #658: reuse the brief HMAC keyring for verdict-sig verify. Empty
	// keyring leaves the Approver in legacy WARN-only mode so the gate
	// stays default-off until operators have rotated keys into place.
	return review.New(review.Config{
		Owner:          owner,
		Repo:           name,
		ReviewerToken:  token,
		ReviewerLogin:  reviewer,
		AuthorBotLogin: authorBot,
		Logger:         logger,
		Keyring:        loadBriefKeyring(),
	})
}
