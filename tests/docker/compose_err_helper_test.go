package docker_test

import (
	"fmt"
	"strings"
)

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

