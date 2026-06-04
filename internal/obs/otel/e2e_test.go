//go:build e2e_otel

// e2e_test.go drives the full W6 span tree against the Jaeger all-in-one
// container defined by examples/observability/docker-compose.yml and
// asserts the resulting trace is queryable through Jaeger's HTTP API.
//
// Tag-gated (`e2e_otel`) so `go test ./...` stays Docker-free; the test
// runs in CI only when the operator opts in via the build tag.
//
// Spec §6 T6 + §7 A+1: this is the proof that the operator-facing
// fixture actually round-trips one synthetic Tick through scheduler →
// spawner → ParseStream → OTLP → Jaeger within 5s.
package otel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	otelpkg "github.com/trilamsr/regatta/internal/obs/otel"
	"github.com/trilamsr/regatta/internal/orchestrator/scheduler"
	"github.com/trilamsr/regatta/internal/orchestrator/spawner"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
	"github.com/trilamsr/regatta/internal/strutil"
	"github.com/trilamsr/regatta/internal/testutil"
	"github.com/trilamsr/regatta/internal/testutil/statetest"
)

const (
	jaegerOTLPPort  = "4317"
	jaegerQueryPort = "16686"
	composeService  = "jaeger"
)

// TestE2E_TraceReachesJaeger pins spec §6 T6 + §7 A+1 — synthetic Tick materialises the full span tree into Jaeger within 5s.
func TestE2E_TraceReachesJaeger(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available; e2e test requires docker compose")
	}

	composePath := resolveComposePath(t)

	startCompose(t, composePath)
	t.Cleanup(func() { stopCompose(t, composePath) })

	waitForJaegerReady(t, 30*time.Second)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:"+jaegerOTLPPort)
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")

	ctx := context.Background()
	shutdown, err := otelpkg.Setup(ctx, otelpkg.Config{
		ServiceName:    "regatta",
		ServiceVersion: "e2e-test",
	})
	if err != nil {
		t.Fatalf("otelpkg.Setup: %v", err)
	}

	traceID := runSyntheticTick(t, ctx)

	// Flush spans by shutting the provider down before polling. The
	// BatchSpanProcessor's default schedule delay (5s) is longer than
	// the assertion budget, so we drain explicitly via shutdown — which
	// is the same call cmd/regatta makes on signal-driven exit. Setup's
	// returned shutdown is idempotent (TestSetup_ShutdownIsIdempotent).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Logf("shutdown returned err (non-fatal): %v", err)
	}

	spanNames, err := pollJaegerForTrace(t, traceID, 5*time.Second)
	if err != nil {
		t.Fatalf("trace %s not found in Jaeger within 5s: %v", traceID, err)
	}

	// Spec §4.1 span hierarchy: every layer of the synthetic Tick must
	// reach the backend. Anything less proves only that the OTLP pipe
	// works, not that the full causal chain survives the round trip.
	wantSpans := []string{"program", "tick", "work_item", "operator_invocation", "chat claude-sonnet-4-7"}
	for _, want := range wantSpans {
		if !contains(spanNames, want) {
			t.Errorf("missing span %q in trace; got %v", want, spanNames)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// runSyntheticTick exercises the full W6 span hierarchy — scheduler
// tick → work_item → operator_invocation → llm_call — and returns the
// observed trace ID. The whole tree must share one trace ID so a single
// Jaeger query lookup proves the entire chain reached the backend.
func runSyntheticTick(t *testing.T, ctx context.Context) string {
	t.Helper()

	db := statetest.OpenDB(t)
	seedWorkItem(t, db, "F-E2E", "server")

	tracer := otel.Tracer("e2e")

	rootCtx, programSpan := tracer.Start(ctx, "program")
	defer programSpan.End()

	tickCtx, tickSpan := tracer.Start(rootCtx, "tick")
	traceID := tickSpan.SpanContext().TraceID().String()

	sched := scheduler.New(db, scheduler.Config{Tracer: tracer})
	if _, err := sched.Tick(tickCtx); err != nil {
		tickSpan.End()
		t.Fatalf("scheduler.Tick: %v", err)
	}

	sp := spawner.New(spawner.Config{DB: db, Tracer: tracer})
	opCtx, opSpan := tracer.Start(tickCtx, "operator_invocation")
	if _, err := sp.Spawn(opCtx, spawner.Request{AgentID: 1, WorkItemID: "F-E2E", Lane: "server"}); err != nil {
		opSpan.End()
		tickSpan.End()
		t.Fatalf("spawner.Spawn: %v", err)
	}

	fixture := openStreamJSONFixture(t)
	defer fixture.Close()
	if err := spawner.ParseStream(opCtx, tracer, fixture, nil); err != nil {
		opSpan.End()
		tickSpan.End()
		t.Fatalf("spawner.ParseStream: %v", err)
	}

	opSpan.End()
	tickSpan.End()

	if traceID == "" || strings.Trim(traceID, "0") == "" {
		t.Fatalf("trace id missing — Setup did not wire a real tracer provider")
	}
	return traceID
}

func seedWorkItem(t *testing.T, db *state.DB, id, lane string) {
	t.Helper()
	wi := state.WorkItem{
		ID:     id,
		Kind:   state.KindFeature,
		Title:  id,
		Lane:   lane,
		Status: state.WorkStatusPlanned,
	}
	if err := db.UpsertWorkItem(context.Background(), wi, state.SourceBrief, time.Now()); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func openStreamJSONFixture(t *testing.T) *os.File {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	path := filepath.Join(root, "internal", "orchestrator", "spawner", "testdata", "stream-json", "success.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open stream-json fixture %s: %v", path, err)
	}
	return f
}

// resolveComposePath returns the docker-compose.yml absolute path that
// hosts the Jaeger fixture. Centralising the resolution makes the test
// portable across `go test` invocations from any directory.
func resolveComposePath(t *testing.T) string {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	p := filepath.Join(root, "examples", "observability", "docker-compose.yml")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("docker-compose.yml not found at %s: %v", p, err)
	}
	return p
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func startCompose(t *testing.T, composePath string) {
	t.Helper()
	cmd := exec.Command("docker", "compose", "-f", composePath, "up", "-d", composeService)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("docker compose up: %v", err)
	}
}

func stopCompose(t *testing.T, composePath string) {
	t.Helper()
	cmd := exec.Command("docker", "compose", "-f", composePath, "down", "--remove-orphans")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Logf("docker compose down: %v (non-fatal)", err)
	}
}

// waitForJaegerReady blocks until Jaeger's OTLP gRPC ingest port + UI
// HTTP port both accept connections, or the deadline elapses. The
// healthcheck inside docker-compose tracks UI readiness; the gRPC
// listener is separately probed here so the OTLP exporter does not
// connect to a half-warm container.
func waitForJaegerReady(t *testing.T, deadline time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	testutil.Eventually(t, ctx, 500*time.Millisecond, func() bool {
		return dialOK("localhost:"+jaegerOTLPPort) && dialOK("localhost:"+jaegerQueryPort)
	}, fmt.Sprintf("Jaeger not ready within %s", deadline))
}

func dialOK(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// pollJaegerForTrace queries Jaeger's HTTP API for the trace ID until
// every span we expect appears, or the deadline elapses. Returns the
// observed span-name list so the caller can assert on hierarchy
// coverage (spec §4.1). Spans land asynchronously even after the OTel
// batch processor flushes — Jaeger's storage write is itself queued —
// so we wait until the count stabilises at ≥5 spans.
func pollJaegerForTrace(t *testing.T, traceID string, deadline time.Duration) ([]string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline+5*time.Second)
	defer cancel()
	url := fmt.Sprintf("http://localhost:%s/api/traces/%s", jaegerQueryPort, traceID)
	var (
		lastErr error
		names   []string
	)
	testutil.EventuallyT(t, ctx, 500*time.Millisecond, func() bool {
		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			return false
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			names = traceSpanNames(body)
			if len(names) >= 5 {
				return true
			}
		}
		lastErr = fmt.Errorf("status=%d body=%s", resp.StatusCode, strutil.Truncate(string(body), 200))
		return false
	}, "Jaeger trace not complete")
	if len(names) >= 5 {
		return names, nil
	}
	if len(names) > 0 {
		return names, fmt.Errorf("incomplete span set (got %d, want ≥5): %v; last_err=%v", len(names), names, lastErr)
	}
	return nil, lastErr
}

// traceSpanNames extracts the operation-name list from Jaeger's trace
// response. An unknown trace renders `data: []` so this returns an
// empty slice; the caller treats that as not-yet-arrived.
func traceSpanNames(body []byte) []string {
	var resp struct {
		Data []struct {
			Spans []struct {
				OperationName string `json:"operationName"`
			} `json:"spans"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	var out []string
	for _, tr := range resp.Data {
		for _, s := range tr.Spans {
			out = append(out, s.OperationName)
		}
	}
	return out
}

