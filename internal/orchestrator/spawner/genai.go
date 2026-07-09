// Package spawner — genai.go parses the claude CLI's `--output-format=
// stream-json` event stream and opens one OTel `chat <model>` span per
// observed LLM call. Spec docs/engineer/specs/2026-05-31-mvp-3-w6-otel-
// backbone.md §3.4 + §3.5: the span lives as a child of T5's
// `operator_invocation` span and carries the GenAI semconv attribute set
// verbatim from the spec table.
//
// Why a separate seam (ParseStream) rather than inlining into Spawn:
// the parser is the single shared call site for both the Stub and the
// production ClaudeSpawner — both ultimately receive a stdout stream
// from the subprocess. Pulling the JSONL decode out keeps the integration
// at every call site to a single line and makes the parser unit-testable
// against captured fixtures without booting an exec.Cmd.
//
// Sensitive-payload safety: this parser MUST NOT set
// gen_ai.input.messages or gen_ai.output.messages (spec §2 Out-of-scope,
// §9 R7). TestGenAI_SensitivePayloadNotEmitted is the runtime gate.
package spawner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	rotel "github.com/trilamsr/regatta/internal/obs/otel"
)

// streamEvent is the union shape claude emits per line on stdout in
// stream-json mode. Only the fields the OTel attribute set in §3.4 needs
// are decoded; unknown fields are tolerated by encoding/json so future
// CLI versions that add fields stay compatible without re-spec.
type streamEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`

	Model     string `json:"model,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`

	IsError    bool            `json:"is_error,omitempty"`
	MessageID  string          `json:"message_id,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"`
	Usage      *streamUsage    `json:"usage,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

type streamUsage struct {
	InputTokens             int64 `json:"input_tokens"`
	OutputTokens            int64 `json:"output_tokens"`
	CacheReadInputTokens    int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// StreamResultEvent is the cross-package projection of one stream-json
// `result` event — the only event shape ResultEventCallback consumers
// need. Mirroring just the cost-governor-required fields keeps the
// internal streamEvent private (future field additions stay non-breaking
// for callback authors) and prevents accidental coupling to
// claude-CLI-internal fields the caller has no business reading.
type StreamResultEvent struct {
	Model                    string
	MessageID                string
	StopReason               string
	IsError                  bool
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
}

// ResultEventCallback fires once per `result` event after the W6 attrs
// are set but before span.End. Cost-governor (P8) wires this to write a
// substrate `token_spend` row inside the parser's span-close path so
// recorded usage and the closing span are mutually visible. A non-nil
// returned error marks the span Error+error.type=record_call_failed per
// the R4 open-span-as-smoke-alarm contract; it is not propagated to
// ParseStream's return value (callers classify subprocess errors apart).
type ResultEventCallback func(ctx context.Context, ev StreamResultEvent) error

// ParseStream reads claude CLI stream-json events from r and opens one
// `chat <model>` span on the system.init line, closing it on the matching
// result line. Spec §3.4 attribute set, §3.5 child-of-operator_invocation
// invariant. No-op when r's first non-empty line is not a stream-json
// event (legacy CLI invocation without --output-format=stream-json — spec
// §9 R10 nondisruption contract).
//
// onResult, when non-nil, fires once per `result` event after finalizeResult
// has set the W6 attribute set but before span.End. Backwards-compatible:
// onResult=nil preserves every pre-cost-gov behaviour byte-equal.
//
// The returned error surfaces only on stream I/O failures; a non-zero
// is_error in a result event is recorded as span status + error.type and
// returns nil so the caller (Spawn) does not double-classify subprocess
// errors.
func ParseStream(ctx context.Context, tracer trace.Tracer, r io.Reader, onResult ResultEventCallback) error {
	if tracer == nil || r == nil {
		return nil
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	// scanCh decouples scanner.Scan (blocking on r.Read) from ctx.Done so a
	// wedged claude subprocess whose stdout stalls past the init event does
	// not hold ParseStream past ctx deadline (W-BUG10). One-slot buffered so
	// the goroutine can exit even if ctx cancel wins the select race.
	scanCh := make(chan bool, 1)
	scanNext := func() { scanCh <- scanner.Scan() }
	go scanNext()

	var (
		span    trace.Span
		gotInit bool
	)
	for {
		var ok bool
		select {
		case <-ctx.Done():
			if span != nil {
				span.SetStatus(codes.Error, ctx.Err().Error())
				span.SetAttributes(rotel.ErrorTypeAttr("stream_truncated"))
				span.End()
			}
			return ctx.Err()
		case ok = <-scanCh:
		}
		if !ok {
			break
		}
		line := scanner.Bytes()
		// Copy bytes off the scanner buffer before relaunching the goroutine
		// — scanner.Bytes() is only valid until the next Scan call.
		lineCopy := make([]byte, len(line))
		copy(lineCopy, line)
		line = lineCopy
		go scanNext()
		if len(line) == 0 {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// First-line gate: any non-JSON content means this is not a
			// stream-json stream; bail without opening a span. Per-spec
			// §9 R10 — legacy operator config nondisruption.
			if !gotInit {
				return nil
			}
			// Past init: tolerate one bad line rather than abort an
			// in-flight span. Claude can prepend stderr noise into stdout
			// in some shells; record + skip.
			continue
		}

		switch {
		case ev.Type == "system" && ev.Subtype == "init":
			if gotInit {
				continue
			}
			gotInit = true
			model := ev.Model
			if model == "" {
				model = "unknown"
			}
			_, span = tracer.Start(ctx, "chat "+model,
				trace.WithSpanKind(trace.SpanKindClient),
				trace.WithAttributes(
					rotel.GenAIOperationName.String(rotel.GenAIOperationChat),
					rotel.GenAIProviderName.String(rotel.GenAIProviderAnthropic),
					rotel.GenAIRequestModel.String(model),
					rotel.GenAIConversationID.String(ev.SessionID),
				),
			)
			if ev.MaxTokens > 0 {
				span.SetAttributes(rotel.GenAIRequestMaxTokens.Int(ev.MaxTokens))
			}
		case ev.Type == "result":
			if span == nil {
				continue
			}
			finalizeResult(span, ev)
			if onResult != nil {
				proj := StreamResultEvent{
					Model:      ev.Model,
					MessageID:  ev.MessageID,
					StopReason: ev.StopReason,
					IsError:    ev.IsError,
				}
				if ev.Usage != nil {
					proj.InputTokens = ev.Usage.InputTokens
					proj.OutputTokens = ev.Usage.OutputTokens
					proj.CacheReadInputTokens = ev.Usage.CacheReadInputTokens
					proj.CacheCreationInputTokens = ev.Usage.CacheCreationInputTokens
				}
				if err := onResult(ctx, proj); err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					span.SetAttributes(rotel.ErrorTypeAttr("record_call_failed"))
				}
			}
			span.End()
			span = nil
		}
	}
	if err := scanner.Err(); err != nil {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.End()
		}
		return fmt.Errorf("genai: stream scan: %w", err)
	}
	// Unterminated stream (no result event): close the span with an
	// error so traces do not leak open. Caller's Spawn typically logs
	// spawn.failed for the same condition.
	if span != nil {
		span.SetStatus(codes.Error, "stream ended before result event")
		span.SetAttributes(rotel.ErrorTypeAttr("stream_truncated"))
		span.End()
	}
	return nil
}

func finalizeResult(span trace.Span, ev streamEvent) {
	if ev.MessageID != "" {
		span.SetAttributes(rotel.GenAIResponseID.String(ev.MessageID))
	}
	if ev.Model != "" {
		span.SetAttributes(rotel.GenAIResponseModel.String(ev.Model))
	}
	if ev.StopReason != "" {
		span.SetAttributes(rotel.GenAIResponseFinishReasons.StringSlice([]string{ev.StopReason}))
	}
	if ev.Usage != nil {
		span.SetAttributes(
			rotel.GenAIUsageInputTokens.Int64(ev.Usage.InputTokens),
			rotel.GenAIUsageOutputTokens.Int64(ev.Usage.OutputTokens),
		)
		if ev.Usage.CacheReadInputTokens > 0 {
			span.SetAttributes(rotel.GenAIUsageCacheReadInputTokens.Int64(ev.Usage.CacheReadInputTokens))
		}
	}
	if ev.IsError {
		typ := ev.Subtype
		if typ == "" {
			typ = "claude_cli_error"
		}
		span.SetAttributes(rotel.ErrorTypeAttr(typ))
		msg := "claude CLI reported is_error=true"
		if len(ev.Result) > 0 {
			msg = string(ev.Result)
		}
		span.SetStatus(codes.Error, msg)
		span.RecordError(errors.New(typ))
	}
}
