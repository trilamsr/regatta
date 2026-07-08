package docker_test

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// coreServices is the docker-compose service list whose logs get appended
// on up-failure. Ordering controls the section order in the error message.
var coreServices = []string{"regatta", "regatta-init", "prometheus", "alertmanager", "grafana"}

// composeUpError wraps a compose-up failure with the captured combined
// output so infra breakage is diagnosable at test-fail time (obs 18471).
// Returns nil when base is nil so callers can drop-in replace `.Run()`.
func composeUpError(base error, output []byte) error {
	if base == nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return base
	}
	return fmt.Errorf("%w\n%s", base, trimmed)
}

// composeUpErrorWithLogs extends composeUpError by appending per-service
// container stdout/stderr fetched via fetcher, because compose CLI stderr
// reports only the health verdict ("... is unhealthy") not the root cause
// which lives inside container logs (MAY-container-logs).
func composeUpErrorWithLogs(base error, output []byte, fetcher func(service string) []byte, services []string) error {
	err := composeUpError(base, output)
	if err == nil || fetcher == nil {
		return err
	}
	var tail strings.Builder
	for _, svc := range services {
		logs := strings.TrimSpace(string(fetcher(svc)))
		if logs == "" {
			continue
		}
		fmt.Fprintf(&tail, "\n\n[%s]\n%s", svc, logs)
	}
	if tail.Len() == 0 {
		return err
	}
	return fmt.Errorf("%w%s", err, tail.String())
}

// composeLogFetcher returns a fetcher that shells `docker compose logs <svc>
// --no-color --tail=200` bounded by a 15s per-service timeout.
func composeLogFetcher(parent context.Context, repoRoot string) func(string) []byte {
	return func(service string) []byte {
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "compose",
			"-f", filepath.Join(repoRoot, "docker-compose.yml"),
			"logs", service, "--no-color", "--tail=200")
		cmd.Dir = repoRoot
		out, _ := cmd.CombinedOutput()
		return out
	}
}
