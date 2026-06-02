package rejectionrouter_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/trilamsr/regatta/internal/orchestrator/rejectionrouter"
)

// TestGHLabeler_AddLabel_AbsentLabel_IncrementsCounter pins reason=absent on "label not found".
func TestGHLabeler_AddLabel_AbsentLabel_IncrementsCounter(t *testing.T) {
	skipIfWindows(t)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	dir := stubGH(t, "could not add label: 'needs-human' not found", 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	lab := rejectionrouter.NewGHLabeler(rejectionrouter.GHLabelerOptions{Meter: meter})
	err := lab.AddLabel(context.Background(), "42", "needs-human")
	if err == nil {
		t.Fatal("AddLabel err = nil; want failure from stub gh")
	}

	requireCounter(t, reader, "regatta.rejection_router.label_failures_total", "absent", 1)
}

// TestGHLabeler_AddLabel_RateLimited_IncrementsCounter pins reason=rate_limited on HTTP 429.
func TestGHLabeler_AddLabel_RateLimited_IncrementsCounter(t *testing.T) {
	skipIfWindows(t)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	dir := stubGH(t, "HTTP 429: API rate limit exceeded for installation", 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	lab := rejectionrouter.NewGHLabeler(rejectionrouter.GHLabelerOptions{Meter: meter})
	err := lab.AddLabel(context.Background(), "42", "needs-human")
	if err == nil {
		t.Fatal("AddLabel err = nil; want failure from stub gh")
	}

	requireCounter(t, reader, "regatta.rejection_router.label_failures_total", "rate_limited", 1)
}

// TestGHLabeler_AddLabel_UnknownError_IncrementsCounter pins reason=unknown on unclassified failures.
func TestGHLabeler_AddLabel_UnknownError_IncrementsCounter(t *testing.T) {
	skipIfWindows(t)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	dir := stubGH(t, "unexpected EOF from server", 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	lab := rejectionrouter.NewGHLabeler(rejectionrouter.GHLabelerOptions{Meter: meter})
	err := lab.AddLabel(context.Background(), "42", "needs-human")
	if err == nil {
		t.Fatal("AddLabel err = nil; want failure from stub gh")
	}

	requireCounter(t, reader, "regatta.rejection_router.label_failures_total", "unknown", 1)
}

// TestGHLabeler_AddLabel_Success_DoesNotIncrement pins zero-failures path emits no counter row.
func TestGHLabeler_AddLabel_Success_DoesNotIncrement(t *testing.T) {
	skipIfWindows(t)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test")

	dir := stubGH(t, "", 0)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	lab := rejectionrouter.NewGHLabeler(rejectionrouter.GHLabelerOptions{Meter: meter})
	if err := lab.AddLabel(context.Background(), "42", "needs-human"); err != nil {
		t.Fatalf("AddLabel err = %v; want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "regatta.rejection_router.label_failures_total" {
				t.Errorf("counter emitted on success path: %+v", m)
			}
		}
	}
}

// TestGHLabeler_NilMeter_FallsBackToGlobal pins the nil-Meter contract.
func TestGHLabeler_NilMeter_FallsBackToGlobal(t *testing.T) {
	skipIfWindows(t)
	dir := stubGH(t, "label not found", 1)
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	lab := rejectionrouter.NewGHLabeler(rejectionrouter.GHLabelerOptions{})
	if err := lab.AddLabel(context.Background(), "42", "needs-human"); err == nil {
		t.Fatal("AddLabel err = nil; want failure")
	}
}

// requireCounter asserts the named counter recorded a datapoint with reason=want at the given value.
func requireCounter(t *testing.T, reader sdkmetric.Reader, name, wantReason string, wantValue int64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect err = %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %q data = %T; want Sum[int64]", name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				reason, _ := dp.Attributes.Value("reason")
				if reason.AsString() == wantReason && dp.Value == wantValue {
					return
				}
			}
			t.Fatalf("metric %q has no datapoint with reason=%q value=%d; got %+v",
				name, wantReason, wantValue, sum.DataPoints)
		}
	}
	t.Fatalf("metric %q not recorded", name)
}

// stubGH writes a shell wrapper named `gh` in a temp dir that prints msg and exits with code,
// so PATH-shadowing the real binary lets tests exercise the classifier without a network round-trip.
func stubGH(t *testing.T, msg string, code int) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n"
	if msg != "" {
		// gh writes errors to stderr; the labeler uses CombinedOutput so either stream
		// makes it into the classifier.
		script += "echo " + shellQuote(msg) + " 1>&2\n"
	}
	script += "exit " + strconv.Itoa(code) + "\n"
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub gh: %v", err)
	}
	return dir
}

func skipIfWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script gh stub assumes POSIX")
	}
}

func shellQuote(s string) string {
	// Single-quote and escape embedded single quotes per POSIX sh rules.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

