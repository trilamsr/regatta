package spend

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
// (R3-mitigation), keyID for keyring rotation, and an injectable
// clock for replay-determinism. Wave-1 single-writer keeps key+keyID
// in-process; W9 will swap for a keyring lookup.
type WriteOptions struct {
	Key   []byte
	KeyID string
	Now   func() time.Time
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

	usd := tokensToUSD(r.InputTokens, row.InputUSDPerMTok) +
		tokensToUSD(r.OutputTokens, row.OutputUSDPerMTok) +
		tokensToUSD(r.CacheReadTokens, row.CacheReadUSDPerMTok) +
		tokensToUSD(r.CacheCreationTokens, row.CacheCreationUSDPerMTok)

	payload := TokenSpendPayload{
		USD:                 usd,
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
	payloadJSON, err := json.Marshal(payload)
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
	if err := substrate.AppendEvent(ctx, tx, ev, opt.Key, opt.KeyID); err != nil {
		return fmt.Errorf("spend: append token_spend: %w", err)
	}
	return nil
}

// tokensToUSD converts a per-million-token rate to a per-token cost.
// Pricing rows store USD-per-Mtok; CallRecord holds absolute token
// counts. The pure-arithmetic form keeps RecordCall replay-deterministic.
func tokensToUSD(tokens int64, usdPerMTok float64) float64 {
	return float64(tokens) * usdPerMTok / 1_000_000.0
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
