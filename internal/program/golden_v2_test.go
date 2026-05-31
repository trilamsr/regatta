// Golden fixtures for v2 conditional-DAG briefs (MVP-2 W2-B).
//
// Fixtures live unsigned under testdata/v2_briefs/. Each test loads
// the raw JSON, signs it under the canonical test HMAC key at runtime,
// then validates. Checking in unsigned payloads avoids signature rot
// when canonical-JSON byte layout or HMAC bytes shift.

package program

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// goldenHMACKey mirrors the test key used by brief_loader_test.go and
// cmd/regatta/program_plan_test.go. 32 bytes — schemas.MinKeyLen.
var goldenHMACKey = []byte("test-key-32-bytes-aaaaaaaaaaaaaaa")

const goldenKeyID = "k1"

// loadV2Fixture reads testdata/v2_briefs/<name> and unmarshals into
// ProgramBriefV2. It does NOT sign. Sign at the call site so each test
// can decide whether to exercise the signing path.
func loadV2Fixture(t *testing.T, name string) *ProgramBriefV2 {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "v2_briefs", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var b ProgramBriefV2
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return &b
}

// loadV1Fixture reads testdata/v2_briefs/<name> as a v1 ProgramBrief.
// Used for the v1-lowered-equivalent fixture which is wire-format v1
// but semantically the same DAG.
func loadV1Fixture(t *testing.T, name string) *ProgramBrief {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "v2_briefs", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var b ProgramBrief
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return &b
}

// rawFixture returns the raw bytes for fixture-shape assertions
// (IsV2Brief sniffing, etc).
func rawFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "v2_briefs", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

// signFixture signs raw fixture bytes via the canonical-JSON signer.
// Lower-level than ProgramBrief.Sign which gates on schema_version=1;
// for v2 fixtures we must bypass that gate until W2-A lands ValidateV2
// and a v2-aware sign path. Returns the SignatureBlock so tests can
// round-trip through Verify on the same payload.
func signFixture(t *testing.T, raw []byte) schemas.SignatureBlock {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal for signing: %v", err)
	}
	sig, err := schemas.Sign(payload, goldenHMACKey, goldenKeyID)
	if err != nil {
		t.Fatalf("schemas.Sign: %v", err)
	}
	return sig
}

func TestGolden_HappyTriage_LoadAndValidate(t *testing.T) {
	raw := rawFixture(t, "happy_triage.json")
	if !IsV2Brief(raw) {
		t.Fatalf("happy_triage.json: IsV2Brief returned false; expected v2 (schema_version=2)")
	}

	b := loadV2Fixture(t, "happy_triage.json")
	if b.SchemaVersion != 2 {
		t.Fatalf("schema_version=%d want 2", b.SchemaVersion)
	}
	if len(b.FeaturesV2) != 3 {
		t.Fatalf("FeaturesV2 len=%d want 3 (F-A scan, F-B remediate-high, F-C remediate-low)", len(b.FeaturesV2))
	}

	byID := map[string]PlannedFeatureV2{}
	for _, f := range b.FeaturesV2 {
		byID[f.ID] = f
	}
	scan, ok := byID["F-A"]
	if !ok {
		t.Fatalf("missing F-A (scan)")
	}
	if scan.OutputsSchema == nil {
		t.Fatalf("F-A missing outputs_schema; conditional fan-out requires upstream schema")
	}
	if len(scan.Edges) != 2 {
		t.Fatalf("F-A edges=%d want 2 (one predicated to F-B, one to F-C)", len(scan.Edges))
	}

	var sawHigh, sawLow bool
	for _, e := range scan.Edges {
		if e.From != "F-A" {
			t.Fatalf("F-A edge.From=%s want F-A", e.From)
		}
		if e.Predicate == "" {
			t.Fatalf("F-A edge to %s missing predicate; conditional fan-out demands non-empty predicate", e.To)
		}
		switch e.To {
		case "F-B":
			sawHigh = true
		case "F-C":
			sawLow = true
		default:
			t.Fatalf("F-A edge to unknown target %s", e.To)
		}
	}
	if !sawHigh || !sawLow {
		t.Fatalf("F-A must edge to both F-B and F-C (sawHigh=%v sawLow=%v)", sawHigh, sawLow)
	}

	sig := signFixture(t, raw)
	if sig.KeyID != goldenKeyID {
		t.Fatalf("signFixture key_id=%q want %q", sig.KeyID, goldenKeyID)
	}
	if sig.Alg != schemas.SigAlg {
		t.Fatalf("signFixture alg=%q want %q", sig.Alg, schemas.SigAlg)
	}
}

func TestGolden_V1LoweredEquivalent_LoadAndValidate(t *testing.T) {
	raw := rawFixture(t, "v1_lowered_equivalent.json")
	if IsV2Brief(raw) {
		t.Fatalf("v1_lowered_equivalent.json: IsV2Brief returned true; this fixture is wire-format v1")
	}

	v1 := loadV1Fixture(t, "v1_lowered_equivalent.json")
	if v1.SchemaVersion != 1 {
		t.Fatalf("schema_version=%d want 1", v1.SchemaVersion)
	}
	if err := v1.Validate(); err != nil {
		t.Fatalf("v1 fixture failed Validate: %v", err)
	}

	lowered := LowerV1ToV2(v1)
	if lowered.SchemaVersion != 2 {
		t.Fatalf("lowered SchemaVersion=%d want 2", lowered.SchemaVersion)
	}
	if len(lowered.FeaturesV2) != len(v1.Features) {
		t.Fatalf("lowered features=%d want %d", len(lowered.FeaturesV2), len(v1.Features))
	}

	var totalEdges, totalDeps int
	for _, f := range v1.Features {
		totalDeps += len(f.DependsOnFeatures)
	}
	for _, f := range lowered.FeaturesV2 {
		totalEdges += len(f.Edges)
	}
	if totalEdges != totalDeps {
		t.Fatalf("lowered edges=%d want %d (one edge per v1 depends_on_features entry)", totalEdges, totalDeps)
	}

	for _, f := range lowered.FeaturesV2 {
		for _, e := range f.Edges {
			if e.Predicate != "" {
				t.Fatalf("lowered edge %s->%s has predicate %q; expected unconditional", e.From, e.To, e.Predicate)
			}
		}
	}
}

func TestGolden_AllDefaultNoPredicates_BackwardCompat(t *testing.T) {
	raw := rawFixture(t, "all_default_no_predicates.json")
	if !IsV2Brief(raw) {
		t.Fatalf("all_default_no_predicates.json: IsV2Brief returned false; expected v2")
	}

	b := loadV2Fixture(t, "all_default_no_predicates.json")
	if b.SchemaVersion != 2 {
		t.Fatalf("schema_version=%d want 2", b.SchemaVersion)
	}
	if len(b.FeaturesV2) == 0 {
		t.Fatalf("no features in fixture")
	}

	for _, f := range b.FeaturesV2 {
		for _, e := range f.Edges {
			if e.Predicate != "" {
				t.Fatalf("feature %s edge %s->%s has predicate %q; this fixture must be predicate-free for backward-compat coverage",
					f.ID, e.From, e.To, e.Predicate)
			}
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sig := signFixture(t, raw)
	payload[schemas.SigKey] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    sig.MAC,
	}
	if err := schemas.Verify(payload, map[string][]byte{goldenKeyID: goldenHMACKey}); err != nil {
		t.Fatalf("verify roundtrip: %v", err)
	}
}

func TestGolden_MultiHop_PredicatedChain(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "v2_briefs", "multi_hop.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("multi_hop.json fixture not present (optional)")
	}

	raw := rawFixture(t, "multi_hop.json")
	if !IsV2Brief(raw) {
		t.Fatalf("multi_hop.json: IsV2Brief returned false; expected v2")
	}
	b := loadV2Fixture(t, "multi_hop.json")
	if len(b.FeaturesV2) < 4 {
		t.Fatalf("multi_hop expected ≥4 features for A->B->C->D chain, got %d", len(b.FeaturesV2))
	}

	var predicated int
	for _, f := range b.FeaturesV2 {
		for _, e := range f.Edges {
			if e.Predicate != "" {
				predicated++
			}
		}
	}
	if predicated == 0 {
		t.Fatalf("multi_hop has zero predicated edges; chain must exercise predicate evaluation")
	}
}
