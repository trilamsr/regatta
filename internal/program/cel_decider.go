// CELDecider is the substrate gate_verdict producer. It evaluates a
// CEL predicate against a Snapshot and emits ONE signed gate_verdict
// event per Decide call. Fold + eval + emit run inside a single
// substrate transaction per spec §10 #17 so concurrent readers see a
// coherent state.
//
// Spec §2.2 + plan §3 row #2. Wave 1 is the only impl. The Decider
// interface stays unwritten until a second impl (HumanDecider /
// VerifierDecider) forces extraction per spec S5 + followup F9 —
// concrete type now, interface later.
//
// Trap P1 surface: no LLM client import. The predicate gate is
// deterministic; the CEL env is fixed at construction time.

package program

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"go.opentelemetry.io/otel/trace"

	"github.com/trilamsr/regatta/internal/orchestrator/state/substrate"
)

// Beginner is the BeginTx surface CELDecider depends on. *sql.DB
// satisfies it; tests inject a counting wrapper to assert the
// one-tx invariant (spec §10 #17 / TestCELDecider_OneTxForFoldEval
// Emit) without sniffing internal decider state.
//
// Narrowing to BeginTx keeps the decider unaware of pool sizing,
// migration state, or the wider *state.DB surface — the only
// substrate-side operation CELDecider performs is a single
// BeginTx → AppendEvent → Commit.
type Beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// Snapshot is the read-side state CELDecider evaluates a predicate
// against. Inputs and Outputs are CEL-visible maps; values are raw
// JSON so the snapshotter can transit substrate.Event payloads
// without re-decoding.
//
// TraceID + SpanID may be caller-set but Decide OVERWRITES them from
// ctx when a valid OTel span is present — a hostile caller cannot
// forge the trace anchor on the signed gate_verdict event.
type Snapshot struct {
	RunID      string
	WorkItemID string
	TenantID   string
	TraceID    string
	SpanID     string
	Inputs     map[string]json.RawMessage
	Outputs    map[string]json.RawMessage
}

// GateResult is the typed output of Decide.
type GateResult struct {
	Pass   bool
	Reason string
}

// CELDecider holds a pre-compiled CEL program plus signing config.
// One instance per (gate, predicate); reuse across Decide calls is
// cheap because compile happens once at NewCELDecider.
//
// All fields unexported — the construction surface is load-bearing
// (NewCELDecider rejects malformed CEL before any Decide could fire).
type CELDecider struct {
	program   cel.Program
	db        Beginner
	key       []byte
	keyID     string
	writtenBy string
	gateName  string
	now       func() time.Time
}

// NewCELDecider compiles expr against the CEL env (inputs + outputs
// map vars) and binds signing material. Compile failures wrap the
// cel-go issue list; callers treat them as config bugs that should
// surface at plan-time, not at decide-time.
func NewCELDecider(expr string, db Beginner, key []byte, keyID, writtenBy, gateName string) (*CELDecider, error) {
	if expr == "" {
		return nil, fmt.Errorf("cel_decider: empty expression")
	}
	if db == nil {
		return nil, fmt.Errorf("cel_decider: nil db")
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("cel_decider: empty signing key")
	}
	if keyID == "" {
		return nil, fmt.Errorf("cel_decider: empty keyID")
	}
	if writtenBy == "" {
		return nil, fmt.Errorf("cel_decider: empty writtenBy")
	}
	if gateName == "" {
		return nil, fmt.Errorf("cel_decider: empty gateName")
	}
	env, err := buildCELDeciderEnv()
	if err != nil {
		return nil, fmt.Errorf("cel_decider: build env: %w", err)
	}
	ast, iss := env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("cel_decider: compile: %w", iss.Err())
	}
	// Pin predicate output type at compile time so a wrong-shape
	// predicate (e.g. one returning int or string) fails at plan-
	// time rather than at first Decide. Mirrors the planner_v2
	// gate that catches the same trap on conditional-DAG predicates.
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("cel_decider: predicate must return bool, got %s",
			ast.OutputType().String())
	}
	prg, err := env.Program(ast, cel.CostLimit(10_000))
	if err != nil {
		return nil, fmt.Errorf("cel_decider: program: %w", err)
	}
	return &CELDecider{
		program:   prg,
		db:        db,
		key:       key,
		keyID:     keyID,
		writtenBy: writtenBy,
		gateName:  gateName,
		now:       time.Now,
	}, nil
}

// buildCELDeciderEnv returns the CEL env Decide evaluates against.
// inputs + outputs are MapType(string, dyn) so predicates can index
// by string key and dispatch on whatever shape the snapshot ships.
func buildCELDeciderEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("inputs", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("outputs", cel.MapType(cel.StringType, cel.DynType)),
	)
}

// Decide runs the predicate against snap, emits a signed gate_verdict
// event, and returns the typed result. Spec §10 #17: BEGIN → eval →
// emit → COMMIT all happen inside one tx. Eval lives inside the tx so
// a runtime CEL error rolls the tx back via the deferred Rollback —
// no partial state ever leaks. Eval itself doesn't touch the DB
// (Snapshot.Inputs is caller-supplied per spec §2.2), but the tx
// boundary owns the visibility contract: a concurrent reader sees
// either the pre-emit state or the post-emit state, never mid.
//
// Trace anchoring (forge defense): Decide derives TraceID + SpanID
// from the OTel span on ctx — caller-supplied snap.TraceID /
// snap.SpanID are OVERWRITTEN when a valid span is present. With no
// active span the caller's values flow through (so a worker without
// a span context can plumb a pre-flight trace anchor explicitly).
func (c *CELDecider) Decide(ctx context.Context, snap Snapshot) (GateResult, error) {
	if snap.RunID == "" {
		return GateResult{}, fmt.Errorf("cel_decider: snapshot missing run_id")
	}
	if snap.WorkItemID == "" {
		return GateResult{}, fmt.Errorf("cel_decider: snapshot missing work_item_id")
	}
	if snap.TenantID == "" {
		snap.TenantID = substrate.DefaultTenantID
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		snap.TraceID = sc.TraceID().String()
		snap.SpanID = sc.SpanID().String()
	}

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return GateResult{}, fmt.Errorf("cel_decider: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	celIn := map[string]any{
		"inputs":  rawMapToAny(snap.Inputs),
		"outputs": rawMapToAny(snap.Outputs),
	}
	val, _, err := c.program.ContextEval(ctx, celIn)
	if err != nil {
		return GateResult{}, fmt.Errorf("cel_decider: eval: %w", err)
	}
	b, ok := val.(types.Bool)
	if !ok {
		return GateResult{}, fmt.Errorf("cel_decider: predicate returned %s, want bool",
			typeNameOf(val))
	}
	pass := bool(b)
	reason := ""
	if !pass {
		reason = "predicate=false"
	}

	payload, err := json.Marshal(substrate.GateVerdictPayload{
		GateName:   c.gateName,
		Pass:       pass,
		Reason:     reason,
		WorkItemID: snap.WorkItemID,
	})
	if err != nil {
		return GateResult{}, fmt.Errorf("cel_decider: marshal payload: %w", err)
	}

	now := c.now()
	nonce, err := mintNonce()
	if err != nil {
		return GateResult{}, fmt.Errorf("cel_decider: nonce: %w", err)
	}
	event := substrate.Event{
		ID:            substrate.Mint(now),
		RunID:         snap.RunID,
		WorkItemID:    snap.WorkItemID,
		TenantID:      snap.TenantID,
		TraceID:       snap.TraceID,
		SpanID:        snap.SpanID,
		Kind:          substrate.KindGateVerdict,
		Key:           c.gateName,
		PayloadJSON:   payload,
		WrittenBy:     c.writtenBy,
		WrittenAt:     now.UnixMilli(),
		SchemaVersion: 1,
		Nonce:         nonce,
	}

	if err := substrate.AppendEvent(ctx, tx, event, c.key, c.keyID); err != nil {
		return GateResult{}, fmt.Errorf("cel_decider: append: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return GateResult{}, fmt.Errorf("cel_decider: commit: %w", err)
	}
	return GateResult{Pass: pass, Reason: reason}, nil
}

// rawMapToAny unmarshals each json.RawMessage so CEL sees typed
// primitives. Unmarshal failures fall back to the raw string so a
// malformed snapshot value surfaces as a predicate type-mismatch
// rather than a decider crash.
func rawMapToAny(in map[string]json.RawMessage) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, raw := range in {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			out[k] = string(raw)
			continue
		}
		out[k] = v
	}
	return out
}

// mintNonce returns a fresh 16-byte hex nonce for the substrate row.
// crypto/rand failure propagates — a writer that cannot mint a fresh
// nonce cannot uphold UNIQUE(run_id, written_by, nonce).
func mintNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// typeNameOf is the cel-go type-name lookup. Mirrors the helper in
// edge_evaluator.go so a future cel-go bump touches one site.
func typeNameOf(v ref.Val) string {
	if v == nil || v.Type() == nil {
		return "<nil>"
	}
	return v.Type().TypeName()
}
