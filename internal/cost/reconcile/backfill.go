package reconcile

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/trilamsr/regatta/internal/cost/pricing"
	"github.com/trilamsr/regatta/internal/cost/spend"
	"github.com/trilamsr/regatta/internal/dbutil"
	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// writtenByBackfill tags every backfill-emitted token_spend row so
// operator audits can distinguish recovery rows from spawner emissions
// (`cost-reconciler` vs `cost-backfill` vs spawner tag).
const writtenByBackfill = "cost-backfill"

// backfillCallIDPrefix synthesises the substrate-required CallID for
// backfill-emitted rows. Real CallID is unavailable post-incident; the
// `(run_id, bucket_start, model)` triple is the deterministic identity.
const backfillCallIDPrefix = "backfill"

// ErrRunNotFound surfaces when the requested run_id has no substrate
// events — the CLI cannot derive a window without at least one row.
var ErrRunNotFound = errors.New("run not found in substrate")

// BackfillConfig wires one invocation. DB + Key are mandatory; missing
// Now defaults to time.Now; missing HTTPClient defaults to a 30s client.
// Tag (when non-empty) overrides the vcs-revision pricing_rev fallback
// so multiple vendored builds without `vcs.revision` can still produce
// distinct audit tags (#705 R1).
type BackfillConfig struct {
	DB         *sql.DB
	RunID      string
	TenantID   string
	BaseURL    string
	HTTPClient *http.Client
	Key        []byte
	KeyID      string
	Tag        string
	Now        func() time.Time
}

// BackfillResult counts rows the backfill landed vs replay-skipped so
// the CLI exit message names the recovery delta in one number.
// SingleEventWindow flags the MIN==MAX case where the window is a
// 1h synthetic bucket — the Usage API typically returns zero rows and
// the operator should know the recovery emitted nothing because the
// window itself is degenerate (#705 R4).
type BackfillResult struct {
	WindowStart       time.Time
	WindowEnd         time.Time
	RowsEmitted       int
	RowsSkipped       int
	PricingRev        string
	SingleEventWindow bool
}

// Backfill re-derives token_spend rows for a run window from the
// Anthropic Usage API (spec §9 R4 recovery primitive). Idempotent via
// deterministic nonce sha256(run_id|bucket_start|model)[:16] — re-runs
// collide on UNIQUE(run_id, written_by, nonce) and are counted as
// RowsSkipped instead of erroring.
func Backfill(ctx context.Context, cfg BackfillConfig) (BackfillResult, error) {
	if cfg.DB == nil {
		return BackfillResult{}, errors.New("reconcile.Backfill: DB nil")
	}
	if cfg.RunID == "" {
		return BackfillResult{}, errors.New("reconcile.Backfill: RunID empty")
	}
	if len(cfg.Key) == 0 {
		return BackfillResult{}, errors.New("reconcile.Backfill: HMAC key empty — REGATTA_HMAC_KEY unset?")
	}
	if cfg.TenantID == "" {
		cfg.TenantID = substrate.DefaultTenantID
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	start, end, err := runWindow(ctx, cfg.DB, cfg.RunID)
	if err != nil {
		return BackfillResult{}, err
	}
	singleEvent := start.Equal(end)
	// Floor start, ceil end to hour boundary so the Anthropic
	// bucket_width=1h grouping covers the whole window inclusively.
	wStart := start.UTC().Truncate(time.Hour)
	wEnd := end.UTC().Truncate(time.Hour).Add(time.Hour)

	client := NewClient(ClientConfig{
		BaseURL:    cfg.BaseURL,
		HTTPClient: cfg.HTTPClient,
	})
	usage, _, err := client.FetchUsage(ctx, wStart, wEnd, time.Hour)
	if err != nil {
		return BackfillResult{}, fmt.Errorf("fetch usage: %w", err)
	}

	rev := backfillPricingRev(cfg.Tag)
	witemID, err := backfillWorkItemID(cfg.RunID)
	if err != nil {
		return BackfillResult{}, err
	}
	res := BackfillResult{
		WindowStart:       wStart,
		WindowEnd:         wEnd,
		PricingRev:        rev,
		SingleEventWindow: singleEvent,
	}

	for _, b := range usage.Data {
		usdMicro, err := priceUsageBucket(b)
		if err != nil {
			return res, fmt.Errorf("price bucket %s/%s: %w", b.BucketStart.Format(time.RFC3339), b.Model, err)
		}
		written := cfg.Now().UTC()
		payload, err := buildBackfillPayload(b, usdMicro, rev, witemID)
		if err != nil {
			return res, err
		}
		ev := substrate.SubstrateEvent{
			ID:            substrate.Mint(written),
			RunID:         cfg.RunID,
			WorkItemID:    witemID,
			TenantID:      cfg.TenantID,
			Kind:          substrate.KindTokenSpend,
			PayloadJSON:   payload,
			WrittenBy:     writtenByBackfill,
			WrittenAt:     written.UnixMilli(),
			SchemaVersion: 1,
			Nonce:         backfillNonce(cfg.RunID, b.BucketStart, b.Model),
		}
		err = dbutil.WithTx(ctx, cfg.DB, func(tx *sql.Tx) error {
			return substrate.AppendEvent(ctx, tx, ev, cfg.Key, cfg.KeyID)
		})
		switch {
		case err == nil:
			res.RowsEmitted++
		case errors.Is(err, substrate.ErrReplay):
			res.RowsSkipped++
		default:
			return res, fmt.Errorf("append bucket %s/%s: %w", b.BucketStart.Format(time.RFC3339), b.Model, err)
		}
	}
	return res, nil
}

// runWindow reads MIN/MAX(written_at) across substrate_events for the
// run. Returns ErrRunNotFound when the run has zero rows — the CLI
// surfaces the operator-visible "unknown run" message from there.
func runWindow(ctx context.Context, db *sql.DB, runID string) (time.Time, time.Time, error) {
	var minMs, maxMs sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT MIN(written_at), MAX(written_at) FROM substrate_events WHERE run_id = ?`,
		runID).Scan(&minMs, &maxMs)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("query run window: %w", err)
	}
	if !minMs.Valid || !maxMs.Valid {
		return time.Time{}, time.Time{}, ErrRunNotFound
	}
	return time.UnixMilli(minMs.Int64).UTC(), time.UnixMilli(maxMs.Int64).UTC(), nil
}

// priceUsageBucket maps Anthropic Usage tokens to micro-USD via the
// in-tree pricing table. Backfill always re-prices locally because the
// Usage API does not surface USD.
func priceUsageBucket(b UsageBucket) (spend.USDMicro, error) {
	row, err := pricing.Lookup(b.Model)
	if err != nil {
		return 0, err
	}
	usd := float64(b.UncachedInputTokens)*row.InputUSDPerMTok +
		float64(b.OutputTokens)*row.OutputUSDPerMTok +
		float64(b.CacheReadInputTokens)*row.CacheReadUSDPerMTok +
		float64(b.CacheCreationInputTokens)*row.CacheCreationUSDPerMTok
	return spend.FromUSD(usd / 1_000_000.0), nil
}

// buildBackfillPayload assembles the substrate TokenSpendPayload from a
// Usage bucket. Synthetic CallID encodes the (work_item_id, bucket_start,
// model) triple so audit-grepping the row trail is reversible — the
// per-invocation random WorkItemID guards against the operator-named
// `backfill:` collision (#705 R2).
func buildBackfillPayload(b UsageBucket, usdMicro spend.USDMicro, rev, workItemID string) (json.RawMessage, error) {
	p := spend.TokenSpendPayload{
		USDMicro:            usdMicro,
		USD:                 usdMicro.USD(),
		Model:               b.Model,
		InputTokens:         b.UncachedInputTokens,
		OutputTokens:        b.OutputTokens,
		CacheReadTokens:     b.CacheReadInputTokens,
		CacheCreationTokens: b.CacheCreationInputTokens,
		WorkItemID:          workItemID,
		PricingRev:          rev,
		CallID:              fmt.Sprintf("%s:%s:%d:%s", backfillCallIDPrefix, workItemID, b.BucketStart.Unix(), b.Model),
	}
	return json.Marshal(p)
}

// backfillWorkItemID mints `backfill:<run_id>:<8-hex>` so the synthetic
// work_item identity cannot collide with an operator-named work item
// that happens to be prefixed `backfill:` (#705 R2). The hex suffix is
// per-invocation random; all rows in one Backfill call share it so the
// (run_id, work_item_id) pair stays grep-affine for audit.
func backfillWorkItemID(runID string) (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("backfill work_item_id randomness: %w", err)
	}
	return fmt.Sprintf("backfill:%s:%s", runID, hex.EncodeToString(buf[:])), nil
}

// backfillNonce derives the substrate idempotency nonce from the
// (run_id, bucket_start, model) triple. Deterministic so re-running
// backfill against the same Anthropic response replays into the
// UNIQUE(run_id, written_by, nonce) constraint instead of double-counting.
func backfillNonce(runID string, bucketStart time.Time, model string) string {
	h := sha256.Sum256([]byte(runID + "|" + bucketStart.UTC().Format(time.RFC3339) + "|" + model))
	return hex.EncodeToString(h[:16])
}

// backfillPricingRev returns the `backfill:<disambiguator>` pricing_rev
// tag every backfilled row carries. Resolution order (#705 R1): the
// operator-supplied tag wins, then vcs.revision (12-char SHA), then
// vcs.time (build-commit timestamp), then `unknown`. The vcs.time
// fallback prevents two vendored builds without `vcs.revision` from
// colliding on `backfill:unknown` and corrupting audit grep.
func backfillPricingRev(tag string) string {
	if tag != "" {
		return "backfill:" + tag
	}
	disambig := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		var rev, vcsTime string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.time":
				vcsTime = s.Value
			}
		}
		switch {
		case rev != "":
			disambig = rev
			if len(disambig) > 12 {
				disambig = disambig[:12]
			}
		case vcsTime != "":
			disambig = vcsTime
		}
	}
	return "backfill:" + disambig
}
