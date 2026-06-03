// Command regatta-alarm-webhook is the sidecar AlertManager receiver.
// Operator picks this binary OR the in-process variant under
// `regatta serve` (W3 supervisor handles either); both wire the same
// internal/alarmwebhook.Handler so behaviour stays byte-equal.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/trilamsr/regatta/internal/alarmwebhook"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "regatta-alarm-webhook: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		listen   = flag.String("listen", ":9099", "HTTP listen address for the AlertManager webhook")
		tokenEnv = flag.String("gh-token-env", "GITHUB_TOKEN", "env var holding the GitHub API token")
		repo     = flag.String("gh-repo", "", "owner/name of the target GitHub repo (required)")
		ghBase   = flag.String("gh-base-url", "", "override GitHub API base URL (default https://api.github.com)")
	)
	flag.Parse()

	if *repo == "" {
		return errors.New("--gh-repo is required (format owner/name)")
	}
	parts := strings.SplitN(*repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("--gh-repo must be owner/name, got %q", *repo)
	}
	token := os.Getenv(*tokenEnv)
	if token == "" {
		return fmt.Errorf("env %s is empty; webhook would fail every GH call", *tokenEnv)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return alarmwebhook.Serve(ctx, alarmwebhook.ServeOptions{
		ListenAddr:  *listen,
		GHRepoOwner: parts[0],
		GHRepoName:  parts[1],
		GHToken:     token,
		GHBaseURL:   *ghBase,
		Logger:      logger,
	})
}
