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

// TestFetchAll_EmitsSummaryAtInfo_PerKeyAtDebug pins one summary INFO line + per-key detail demoted to DEBUG (R19-1b).
func TestFetchAll_EmitsSummaryAtInfo_PerKeyAtDebug(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	_ = fetchAll(context.Background(), cacheTestFetcher{}, logger)

	out := buf.String()
	if !strings.Contains(out, "msg=secrets_resolved") {
		t.Errorf("want summary line msg=secrets_resolved at INFO; got:\n%s", out)
	}
	if !strings.Contains(out, "resolved=0") || !strings.Contains(out, "missing=6") {
		t.Errorf("want resolved=0 missing=6 in summary; got:\n%s", out)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.Contains(line, "msg=secret_resolved ") || strings.Contains(line, "msg=secret_resolved\t") {
			t.Errorf("per-key secret_resolved must NOT emit at INFO; got:\n  %s", line)
		}
		if strings.Contains(line, "msg=secret_missing") && strings.Contains(line, "level=INFO") {
			t.Errorf("per-key secret_missing must NOT emit at INFO (demote to DEBUG); got:\n  %s", line)
		}
	}
}
