package spend

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/canon"
	"github.com/trilamsr/regatta/internal/cost/pricing"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// pricingRevTag pins the in-process pricing-table version so reconciler
// audits can detect rows priced under a now-superseded table. Bumped
// alongside any internal/cost/pricing/anthropic.go change.
const pricingRevTag = "anthropic@2026-06-01"

// AttrCostError is the OTel span attribute key that surfaces a write-
// time fault (e.g. pricing_missing) on the active llm_call span. Spec
// §3.7 + R4 open-span-as-smoke-alarm contract: writers MUST set this
// attr on every failure path so the in-flight span carries the cause
// when the caller leaves it open.
const AttrCostError = attribute.Key("regatta.cost.error")

// WriteOptions carries the cross-cutting deps RecordCall needs that
// the CallRecord input does NOT carry: HMAC key for substrate signing
// (R3-mitigation), keyID for keyring rotation, an injectable clock
// for replay-determinism, and the package telemetry Config so the
// emitter resolves the OTel meter under the spend scope. Wave-1
// single-writer keeps key+keyID in-process; W9 will swap for a
// keyring lookup.
type WriteOptions struct {
	Key   []byte
	KeyID string
	Now   func() time.Time
	Cfg   Config
}

// RecordCall prices one LLM call, builds a TokenSpendPayload, and
// appends one substrate `kind='token_spend'` row inside the caller's
// transaction. Spec §3.5 lines 326-333 + §9 R12 nonce derivation.
//
// Pricing-missing path (spec §6 T3 / R4): returns
// errors.Is(pricing.ErrPricingMissing), sets the active span's
// regatta.cost.error attr to "pricing_missing", and writes NO
// substrate row — the open span IS the operator smoke alarm.
//
// Replay path: same CallID + RetrySeq twice yields substrate.ErrReplay
// from the UNIQUE(run_id, written_by, nonce) collision; first write
// stands, second wrapped error returns to the caller.
func RecordCall(ctx context.Context, tx *sql.Tx, r CallRecord, opt WriteOptions) error {
	row, err := pricing.Lookup(r.Model)
	if err != nil {
		if errors.Is(err, pricing.ErrPricingMissing) {
			trace.SpanFromContext(ctx).SetAttributes(
				AttrCostError.String("pricing_missing"),
			)
		}
		return fmt.Errorf("spend: pricing for %q: %w", r.Model, err)
	}

	// Per-token-class rates are float64 per-Mtok; multiply tokens *
	// rate, divide by 1e6 last to keep every intermediate term in the
	// mantissa-exact band (#554 boundary discipline). Each class is
	// summed in micro space so the four cache/input/output components
	// accumulate without ULP drift even when one rate is sub-cent.
	usdMicro := tokensToMicro(r.InputTokens, row.InputUSDPerMTok) +
		tokensToMicro(r.OutputTokens, row.OutputUSDPerMTok) +
		tokensToMicro(r.CacheReadTokens, row.CacheReadUSDPerMTok) +
		tokensToMicro(r.CacheCreationTokens, row.CacheCreationUSDPerMTok)

	payload := TokenSpendPayload{
		USDMicro: usdMicro,
		// Legacy float emission — kept for forward-compat dashboards
		// (#554 shadow window). Reader prefers $.usd_micro.
		USD:                 usdMicro.USD(),
		Model:               r.Model,
		InputTokens:         r.InputTokens,
		OutputTokens:        r.OutputTokens,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		OperatorID:          r.OperatorID,
		DAGID:               r.DAGID,
		WorkItemID:          r.WorkItemID,
		PricingRev:          pricingRev(),
		CallID:              r.CallID,
	}
	// token_spend is a #216 F1 hot-path writer: every LLM call lands one
	// substrate row. canon.Marshal pre-canonicalises once so
	// AppendEventCanonicalized below skips the signedPayload map alloc
	// + round-trip on the substrate side (spec §7 + §12 F1).
	payloadJSON, err := canon.Marshal(payload)
	if err != nil {
		return fmt.Errorf("spend: marshal payload: %w", err)
	}

	now := opt.Now
	if now == nil {
		now = time.Now
	}
	at := now().UTC()

	span := trace.SpanFromContext(ctx).SpanContext()
	ev := substrate.Event{
		ID:            substrate.Mint(at),
		RunID:         r.RunID,
		WorkItemID:    r.WorkItemID,
		TenantID:      r.TenantID,
		TraceID:       span.TraceID().String(),
		SpanID:        span.SpanID().String(),
		Kind:          substrate.KindTokenSpend,
		PayloadJSON:   payloadJSON,
		WrittenBy:     r.WrittenBy,
		WrittenAt:     at.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         nonceFor(r.CallID, r.RetrySeq),
	}
	if err := substrate.AppendEventCanonicalized(ctx, tx, ev, payloadJSON, opt.Key, opt.KeyID); err != nil {
		return fmt.Errorf("spend: append token_spend: %w", err)
	}
	emitMetrics(ctx, opt.Cfg, r, usdMicro.USD())
	return nil
}

// emitMetrics fires the spec §3 Tier-1 row-#1 instruments after a
// successful substrate append. Counters are cumulative per process —
// the SDK aggregator handles temporality so emit-on-write is the only
// per-call cost. Instrument creation errors are dropped: the metric
// surface MUST NOT take down the writer (R4 — substrate row already
// landed, refusing here would create a silent ledger / metric drift).
//
// Cardinality fence: only dag_id + operator_id + direction reach the
// label set. pr_number / run_id / work_item_id / tenant_id / model are
// banned per spec §2.2 (lint enforces; see
// internal/obs/otel/cardinality_lint_test.go).
func emitMetrics(ctx context.Context, cfg Config, r CallRecord, usd float64) {
	meter := cfg.ResolveMeter()

	usdCtr, err := meter.Float64Counter("regatta.cost.usd")
	if err == nil {
		usdCtr.Add(ctx, usd,
			metric.WithAttributes(
				attribute.String("dag_id", r.DAGID),
				attribute.String("operator_id", r.OperatorID),
			))
	}

	tokCtr, err := meter.Int64Counter("regatta.cost.tokens")
	if err != nil {
		return
	}
	// Direction enum is closed at 3 values per spec §2.2 cardinality
	// budget: input | output | cache_read. CacheCreationTokens fold
	// into cache_read so we stay within the 3-enum cap (≤ 50 dag ×
	// ≤ 100 op × 3 dir = 15k cells).
	addToken := func(dir string, n int64) {
		if n <= 0 {
			return
		}
		tokCtr.Add(ctx, n,
			metric.WithAttributes(
				attribute.String("dag_id", r.DAGID),
				attribute.String("operator_id", r.OperatorID),
				attribute.String("direction", dir),
			))
	}
	addToken("input", r.InputTokens)
	addToken("output", r.OutputTokens)
	addToken("cache_read", r.CacheReadTokens+r.CacheCreationTokens)
}

// tokensToMicro prices one token-class in micro-USD with a single
// float→int boundary at the end. Per-Mtok rate × tokens stays in the
// mantissa-exact band for production magnitudes (rates < $100/Mtok,
// tokens < 1e8); dividing by 1e6 last and rounding to int64 lands
// every per-call charge on an exact micro-USD count. Issue #554.
func tokensToMicro(tokens int64, usdPerMTok float64) USDMicro {
	return FromUSD(float64(tokens) * usdPerMTok / 1_000_000.0)
}

// nonceFor derives the substrate idempotency nonce per spec §9 R12:
// sha256(CallID || "|" || RetrySeq)[:16]. The "|" separator prevents
// the (CallID="abc", RetrySeq=12) vs (CallID="abc1", RetrySeq=2)
// collision class.
func nonceFor(callID string, retrySeq int) string {
	h := sha256.Sum256([]byte(callID + "|" + strconv.Itoa(retrySeq)))
	return hex.EncodeToString(h[:16])
}

// pricingRev returns the pricing-table revision marker embedded in
// every TokenSpendPayload so reconciler-side audits can detect rows
// priced under a now-superseded table.
func pricingRev() string {
	return pricingRevTag
}
