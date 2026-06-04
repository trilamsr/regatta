package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/trilamsr/regatta/internal/alarmwebhook"
	validateconfig "github.com/trilamsr/regatta/internal/config/validate"
)

// defaultGHTokenEnvVar names the env var that holds the GitHub API
// token when the operator omits alarm_webhook.gh_token_env. The
// string is the env var name, not a credential — gosec G101's
// hardcoded-credentials heuristic false-positives on it. The const
// indirection makes the intent explicit.
const defaultGHTokenEnvVar = "GITHUB_TOKEN" //nolint:gosec // env-var name, not a secret

// startAlarmWebhook is the one-line wire-up for the in-process W1
// receiver. Empty / missing alarm_webhook.listen_addr ⇒ no-op so
// zero-config deployments stay zero-cost. Failures during wire-up
// (bad gh_repo shape, empty token env) log-warn instead of fail-loud
// because the receiver is opt-in observability surface, not boot-
// critical state — `regatta serve` MUST keep coming up even when
// AlertManager is unreachable.
func startAlarmWebhook(ctx context.Context, repoRoot string, slogger *slog.Logger) {
	cfg, _ := validateconfig.LoadConfigFile(filepath.Join(repoRoot, "regatta.yaml"))
	wh := cfg.AlarmWebhookConfig()
	if wh == nil {
		return
	}
	if wh.GHRepo == "" {
		slogger.Warn("alarmwebhook.disabled", "reason", "gh_repo empty",
			"listen_addr", wh.ListenAddr)
		return
	}
	parts := strings.SplitN(wh.GHRepo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		slogger.Warn("alarmwebhook.disabled", "reason", "gh_repo not owner/name",
			"gh_repo", wh.GHRepo)
		return
	}
	tokenEnv := wh.GHTokenEnv
	if tokenEnv == "" {
		tokenEnv = defaultGHTokenEnvVar
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		slogger.Warn("alarmwebhook.disabled", "reason", "token env empty",
			"env", tokenEnv)
		return
	}
	go func() {
		err := alarmwebhook.Serve(ctx, alarmwebhook.ServeOptions{
			ListenAddr:  wh.ListenAddr,
			GHRepoOwner: parts[0],
			GHRepoName:  parts[1],
			GHToken:     token,
			Logger:      slogger,
		})
		if err != nil {
			slogger.Error("alarmwebhook.exit", "err", err)
		}
	}()
}
