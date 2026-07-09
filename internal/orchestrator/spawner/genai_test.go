package spawner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/obs/otel"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "stream-json", name))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func newRecorder(t *testing.T) (*tracetest.SpanRecorder, trace.Tracer) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return sr, tp.Tracer("test")
}

func findSpan(spans []sdktrace.ReadOnlySpan, prefix string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if strings.HasPrefix(s.Name(), prefix) {
			return s
		}
	}
	return nil
}

func attrValue(s sdktrace.ReadOnlySpan, key attribute.Key) (attribute.Value, bool) {
	for _, kv := range s.Attributes() {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// TestGenAI_StreamJsonParser_OpensCloseOnInitAndResult pins spec §6 T4 — open on system.init, close on result.
func TestGenAI_StreamJsonParser_OpensCloseOnInitAndResult(t *testing.T) {
	sr, tracer := newRecorder(t)
	if err := ParseStream(context.Background(), tracer, openFixture(t, "success.jsonl"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	spans := sr.Ended()
	var llm int
	for _, s := range spans {
		if strings.HasPrefix(s.Name(), "chat ") {
			llm++
		}
	}
	if llm != 1 {
		t.Fatalf("expected 1 chat span, got %d (spans=%v)", llm, spans)
	}
}

// TestGenAI_AttributesMatchSemconv pins spec §3.4 — every table-row attr lands on the span with correct value+type.
func TestGenAI_AttributesMatchSemconv(t *testing.T) {
	sr, tracer := newRecorder(t)
	if err := ParseStream(context.Background(), tracer, openFixture(t, "success.jsonl"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	span := findSpan(sr.Ended(), "chat ")
	if span == nil {
		t.Fatalf("no chat span found")
	}

	cases := []struct {
		key  attribute.Key
		want any
	}{
		{otel.GenAIOperationName, "chat"},
		{otel.GenAIProviderName, "anthropic"},
		{otel.GenAIRequestModel, "claude-sonnet-4-7"},
		{otel.GenAIResponseModel, "claude-sonnet-4-7"},
		{otel.GenAIUsageInputTokens, int64(120)},
		{otel.GenAIUsageOutputTokens, int64(42)},
		{otel.GenAIUsageCacheReadInputTokens, int64(80)},
		{otel.GenAIConversationID, "claude-1"},
	}
	for _, c := range cases {
		v, ok := attrValue(span, c.key)
		if !ok {
			t.Errorf("attr %s missing", c.key)
			continue
		}
		switch want := c.want.(type) {
		case string:
			if v.AsString() != want {
				t.Errorf("attr %s = %q, want %q", c.key, v.AsString(), want)
			}
		case int64:
			if v.AsInt64() != want {
				t.Errorf("attr %s = %d, want %d", c.key, v.AsInt64(), want)
			}
		}
	}
}

// TestGenAI_SpanNameMatchesSpec pins OTel GenAI §Inference — span name = "{operation} {model}".
func TestGenAI_SpanNameMatchesSpec(t *testing.T) {
	sr, tracer := newRecorder(t)
	if err := ParseStream(context.Background(), tracer, openFixture(t, "success.jsonl"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	span := findSpan(sr.Ended(), "chat ")
	if span == nil {
		t.Fatalf("no chat span found")
	}
	if got, want := span.Name(), "chat claude-sonnet-4-7"; got != want {
		t.Fatalf("span name = %q, want %q", got, want)
	}
}

// TestGenAI_SpanKindClient pins OTel GenAI spec §Inference: kind=CLIENT.
func TestGenAI_SpanKindClient(t *testing.T) {
	sr, tracer := newRecorder(t)
	if err := ParseStream(context.Background(), tracer, openFixture(t, "success.jsonl"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	span := findSpan(sr.Ended(), "chat ")
	if span == nil {
		t.Fatalf("no chat span found")
	}
	if got, want := span.SpanKind(), trace.SpanKindClient; got != want {
		t.Fatalf("span kind = %v, want %v", got, want)
	}
}

// TestGenAI_ErrorEvent_SetsErrorType pins spec §3.4 — is_error=true → error.type attr + Status=Error.
func TestGenAI_ErrorEvent_SetsErrorType(t *testing.T) {
	sr, tracer := newRecorder(t)
	if err := ParseStream(context.Background(), tracer, openFixture(t, "error.jsonl"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	span := findSpan(sr.Ended(), "chat ")
	if span == nil {
		t.Fatalf("no chat span found")
	}
	if span.Status().Code != codes.Error {
		t.Fatalf("status code = %v, want Error", span.Status().Code)
	}
	v, ok := attrValue(span, otel.ErrorType)
	if !ok {
		t.Fatalf("error.type attr missing")
	}
	if v.AsString() == "" {
		t.Fatalf("error.type empty")
	}
}

// TestGenAI_NoStreamJson_NoSpan pins spec §9 R10 — non-stream-json input is a parser no-op.
func TestGenAI_NoStreamJson_NoSpan(t *testing.T) {
	sr, tracer := newRecorder(t)
	if err := ParseStream(context.Background(), tracer, openFixture(t, "legacy_plain.txt"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	for _, s := range sr.Ended() {
		if strings.HasPrefix(s.Name(), "chat ") {
			t.Fatalf("legacy non-stream-json input opened a span: %q", s.Name())
		}
	}
}

// TestGenAI_SensitivePayloadNotEmitted pins §9 R7 — gen_ai.{input,output}.messages must not appear on any span.
func TestGenAI_SensitivePayloadNotEmitted(t *testing.T) {
	sr, tracer := newRecorder(t)
	if err := ParseStream(context.Background(), tracer, openFixture(t, "success.jsonl"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	for _, s := range sr.Ended() {
		for _, kv := range s.Attributes() {
			if kv.Key == otel.GenAIInputMessages || kv.Key == otel.GenAIOutputMessages {
				t.Fatalf("forbidden attr %s present on span %q (R7 sensitive-payload regression)", kv.Key, s.Name())
			}
		}
	}
}

// TestGenAI_LLMSpanIsChildOfActive pins spec §3.5 — llm_call's parent is the active span in ctx.
func TestGenAI_LLMSpanIsChildOfActive(t *testing.T) {
	sr, tracer := newRecorder(t)
	ctx, parent := tracer.Start(context.Background(), "operator_invocation")
	if err := ParseStream(ctx, tracer, openFixture(t, "success.jsonl"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	parent.End()

	spans := sr.Ended()
	child := findSpan(spans, "chat ")
	if child == nil {
		t.Fatalf("no chat span found")
	}
	parentEnded := findSpan(spans, "operator_invocation")
	if parentEnded == nil {
		t.Fatalf("no operator_invocation span found")
	}
	if child.Parent().SpanID() != parentEnded.SpanContext().SpanID() {
		t.Fatalf("chat span parent=%s, want %s", child.Parent().SpanID(), parentEnded.SpanContext().SpanID())
	}
}

// TestParseStream_NilOnResult_NoCallback_BackwardsCompat pins amendment-PR — nil callback = behaviour-preserving.
func TestParseStream_NilOnResult_NoCallback_BackwardsCompat(t *testing.T) {
	sr, tracer := newRecorder(t)
	if err := ParseStream(context.Background(), tracer, openFixture(t, "success.jsonl"), nil); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	span := findSpan(sr.Ended(), "chat ")
	if span == nil {
		t.Fatalf("no chat span found")
	}
	v, ok := attrValue(span, otel.GenAIUsageInputTokens)
	if !ok || v.AsInt64() != 120 {
		t.Fatalf("nil callback path mutated W6 attrs (input_tokens=%v, want 120)", v.AsInt64())
	}
	if span.Status().Code == codes.Error {
		t.Fatalf("nil callback marked span Error; want Unset")
	}
}

// TestParseStream_OnResultFiresExactlyOncePerResultEvent pins amendment — one call per result event.
func TestParseStream_OnResultFiresExactlyOncePerResultEvent(t *testing.T) {
	_, tracer := newRecorder(t)
	var calls int
	var got StreamResultEvent
	cb := func(_ context.Context, ev StreamResultEvent) error {
		calls++
		got = ev
		return nil
	}
	if err := ParseStream(context.Background(), tracer, openFixture(t, "success.jsonl"), cb); err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	if calls != 1 {
		t.Fatalf("callback fired %d times, want 1", calls)
	}
	if got.MessageID != "msg_01abc" || got.Model != "claude-sonnet-4-7" {
		t.Fatalf("projection mis-mapped: msgid=%q model=%q", got.MessageID, got.Model)
	}
	if got.InputTokens != 120 || got.OutputTokens != 42 || got.CacheReadInputTokens != 80 {
		t.Fatalf("usage projection mis-mapped: in=%d out=%d cache_read=%d",
			got.InputTokens, got.OutputTokens, got.CacheReadInputTokens)
	}
}

// TestParseStream_OnResultErrorMarksSpanError pins R4 — callback error ⇒ span Error + error.type=record_call_failed.
func TestParseStream_OnResultErrorMarksSpanError(t *testing.T) {
	sr, tracer := newRecorder(t)
	cb := func(context.Context, StreamResultEvent) error {
		return errors.New("synthetic")
	}
	if err := ParseStream(context.Background(), tracer, openFixture(t, "success.jsonl"), cb); err != nil {
		t.Fatalf("ParseStream returned err=%v; want nil (callback error logged on span, not propagated)", err)
	}
	span := findSpan(sr.Ended(), "chat ")
	if span == nil {
		t.Fatalf("no chat span found")
	}
	if span.Status().Code != codes.Error {
		t.Fatalf("status=%v, want Error", span.Status().Code)
	}
	v, ok := attrValue(span, otel.ErrorType)
	if !ok {
		t.Fatalf("error.type attr missing")
	}
	if v.AsString() != "record_call_failed" {
		t.Fatalf("error.type=%q, want record_call_failed", v.AsString())
	}
}

// blockingReader emits initLine on the first Read, then blocks subsequent
// Reads until stop is closed — models a wedged claude subprocess whose
// stdout has flushed the init event but produces no further output.
type blockingReader struct {
	initLine []byte
	sent     bool
	stop     <-chan struct{}
}

func (b *blockingReader) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		n := copy(p, b.initLine)
		return n, nil
	}
	<-b.stop
	return 0, io.EOF
}

// TestParseStream_HonorsCtxCancel_OnWedgedReader asserts ParseStream returns ctx.Err() when the reader wedges past init (W-BUG10).
func TestParseStream_HonorsCtxCancel_OnWedgedReader(t *testing.T) {
	_, tracer := newRecorder(t)
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	r := &blockingReader{
		initLine: []byte(`{"type":"system","subtype":"init","session_id":"s","model":"m"}` + "\n"),
		stop:     stop,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- ParseStream(ctx, tracer, r, nil) }()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("ParseStream err=%v, want context.DeadlineExceeded", err)
		}
		if elapsed > 500*time.Millisecond {
			t.Fatalf("ParseStream returned after %v, want <500ms", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ParseStream did not return within 2s of ctx deadline — scanner wedged")
	}
}
