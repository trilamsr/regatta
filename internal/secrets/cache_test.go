package secrets

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

type cacheTestFetcher struct{}

func (cacheTestFetcher) Get(_ context.Context, _ string) (Value, error) {
	return Value{}, ErrNotFound
}
func (cacheTestFetcher) Name() string { return "test" }

// TestFetchAll_OptionalMissingKeys_LogsBelowWarn pins that optional secrets do not spam WARN at boot.
func TestFetchAll_OptionalMissingKeys_LogsBelowWarn(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_ = fetchAll(context.Background(), cacheTestFetcher{}, logger)

	optional := map[string]bool{KeyLinear: true, KeyAuditHMACKey: true, KeyApprovalToken: true}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if !strings.Contains(line, "msg=secret_missing") || !strings.Contains(line, "level=WARN") {
			continue
		}
		for key := range optional {
			if strings.Contains(line, "key="+key) {
				t.Errorf("optional key %q logged at WARN; want INFO or below:\n  %s", key, line)
			}
		}
	}
}
