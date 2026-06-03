package spawner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// streamStarter is a ProcessStarter that writes the contents of a
// stream-json fixture into the caller-supplied stdout writer, then
// runs `true` as the surrogate process. Lets the parser tee see real
// content without booting a real claude binary.
type streamStarter struct {
	fixture string
}

func (s *streamStarter) start(ctx context.Context, _ string, _ []string, _ io.Reader, stdout io.Writer, _ string) (*exec.Cmd, error) {
	data, err := os.ReadFile(s.fixture)
	if err != nil {
		return nil, err
	}
	go func() {
		_, _ = stdout.Write(data)
	}()
	cmd := exec.CommandContext(ctx, "true")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// TestClaudeSpawn_StreamJsonOpensOperatorAndLLMSpans pins §3.5 — Spawn emits operator_invocation + child chat span.
func TestClaudeSpawn_StreamJsonOpensOperatorAndLLMSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	cs, _, _ := newClaudeHarness(t)
	cs.cfg.Tracer = tp.Tracer("spawner")
	cs.SetStarter((&streamStarter{fixture: "testdata/stream-json/success.jsonl"}).start)

	if _, err := cs.Spawn(context.Background(), Request{AgentID: 1, WorkItemID: "WI-1", Lane: "server"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var ops, llm int
	for time.Now().Before(deadline) {
		ops, llm = 0, 0
		for _, sp := range sr.Ended() {
			switch {
			case sp.Name() == "operator_invocation":
				ops++
			case strings.HasPrefix(sp.Name(), "chat "):
				llm++
			}
		}
		if ops == 1 && llm == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ops != 1 {
		t.Fatalf("operator_invocation spans=%d, want 1", ops)
	}
	if llm != 1 {
		t.Fatalf("chat spans=%d, want 1", llm)
	}

	var parent, child sdktrace.ReadOnlySpan
	for _, sp := range sr.Ended() {
		switch {
		case sp.Name() == "operator_invocation":
			parent = sp
		case strings.HasPrefix(sp.Name(), "chat "):
			child = sp
		}
	}
	if child.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("chat span parent=%s, want operator_invocation span id=%s",
			child.Parent().SpanID(), parent.SpanContext().SpanID())
	}
}

// TestClaudeSpawn_LegacyStdout_NoLLMSpan pins §9 R10 — non-stream-json subprocess output opens no chat span.
func TestClaudeSpawn_LegacyStdout_NoLLMSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	cs, _, _ := newClaudeHarness(t)
	cs.cfg.Tracer = tp.Tracer("spawner")
	cs.SetStarter((&streamStarter{fixture: "testdata/stream-json/legacy_plain.txt"}).start)

	if _, err := cs.Spawn(context.Background(), Request{AgentID: 1, WorkItemID: "WI-1"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	for _, sp := range sr.Ended() {
		if strings.HasPrefix(sp.Name(), "chat ") {
			t.Fatalf("legacy stdout opened chat span: %q", sp.Name())
		}
	}
}

