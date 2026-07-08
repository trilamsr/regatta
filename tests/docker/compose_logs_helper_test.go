//go:build docker

package docker_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"time"
)

// coreServices is the docker-compose service list whose logs get appended
// on up-failure. Ordering controls the section order in the error message.
var coreServices = []string{"regatta", "regatta-init", "prometheus", "alertmanager", "grafana"}

// composeLogFetcher returns a fetcher that shells `docker compose logs <svc>
// --no-color --tail=200`, bounded by a 15s per-service timeout so a wedged
// container cannot block error reporting past the parent test deadline.
func composeLogFetcher(parent context.Context, repoRoot string) func(string) []byte {
	return func(service string) []byte {
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "compose",
			"-f", filepath.Join(repoRoot, "docker-compose.yml"),
			"logs", service, "--no-color", "--tail=200")
		cmd.Dir = repoRoot
		// Diagnostic best-effort: any docker-CLI error (timeout, missing
		// service, no logs) is intentionally discarded — partial output
		// still helps triage; a hard error here would mask the compose
		// failure the caller is already reporting.
		out, _ := cmd.CombinedOutput()
		return out
	}
}
