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
