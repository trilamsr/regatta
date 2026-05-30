package program

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// stubClient is a deterministic ModelClient for tests. It returns
// the configured plan; the planner pipeline fills in the rest.
type stubClient struct {
	plan *ProgramBrief
	err  error
	id   string
}

func (s *stubClient) Plan(_ context.Context, _ schemas.WorkItem) (*ProgramBrief, error) {
	if s.err != nil {
		return nil, s.err
	}
	cp := *s.plan
	return &cp, nil
}
func (s *stubClient) ModelID() string {
	if s.id == "" {
		return "stub:test"
	}
	return s.id
}

func sampleParent() schemas.WorkItem {
	return schemas.WorkItem{
		ID:    "RFC-AUTH",
		Title: "auth rewrite",
		AcceptanceCriteria: []schemas.Criterion{
			{ID: "AC-1", Text: "JWT validation rejects empty token"},
			{ID: "AC-2", Text: "session writes serialize under a mutex"},
			{ID: "AC-3", Text: "deprecation banner on the old endpoint"},
		},
	}
}

func goodModelPlan() *ProgramBrief {
	return &ProgramBrief{
		Features: []PlannedFeature{
			{ID: "F-JWT", Title: "JWT validation hardening", Fulfills: []string{"AC-1"}},
			{ID: "F-SESSION", Title: "session mutex", Fulfills: []string{"AC-2"}, DependsOnFeatures: []string{"F-JWT"}},
			{ID: "F-BANNER", Title: "deprecation banner", Fulfills: []string{"AC-3"}},
		},
	}
}

func TestRun_HappyPath(t *testing.T) {
	plan, err := Run(context.Background(), PlannerOptions{
		Client:    &stubClient{plan: goodModelPlan(), id: "anthropic:claude-opus-4-7"},
		HMACKey:   []byte("planner-test-key-32-bytes-padding"),
		HMACKeyID: "k1",
	}, sampleParent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.ProgramID == "" || plan.ProgramID[:2] != "m-" {
		t.Fatalf("bad program_id: %q", plan.ProgramID)
	}
	if plan.PlannerModelID != "anthropic:claude-opus-4-7" {
		t.Fatalf("planner_model_id not stamped: %q", plan.PlannerModelID)
	}
	if len(plan.ParentCriteria) != 3 {
		t.Fatalf("parent_criteria not copied")
	}
	if time.Since(plan.ProducedAt) > time.Minute {
		t.Fatalf("produced_at not stamped")
	}
	if plan.Signature.MAC == "" || len(plan.Signature.MAC) != 64 {
		t.Fatalf("plan unsigned: %+v", plan.Signature)
	}
}

func TestRun_CoverageIncomplete(t *testing.T) {
	bad := goodModelPlan()
	bad.Features = bad.Features[:2] // drops F-BANNER, leaves AC-3 unclaimed
	_, err := Run(context.Background(), PlannerOptions{
		Client:    &stubClient{plan: bad},
		HMACKey:   []byte("k"),
		HMACKeyID: "k1",
	}, sampleParent())
	if !errors.Is(err, ErrCoverageIncomplete) {
		t.Fatalf("expected ErrCoverageIncomplete, got %v", err)
	}
}

func TestRun_CoverageOverlap(t *testing.T) {
	bad := goodModelPlan()
	bad.Features[1].Fulfills = []string{"AC-1", "AC-2"} // AC-1 also claimed by F-JWT
	_, err := Run(context.Background(), PlannerOptions{
		Client:    &stubClient{plan: bad},
		HMACKey:   []byte("k"),
		HMACKeyID: "k1",
	}, sampleParent())
	if !errors.Is(err, ErrCoverageOverlap) {
		t.Fatalf("expected ErrCoverageOverlap, got %v", err)
	}
}

func TestRun_CoveragePhantom(t *testing.T) {
	bad := goodModelPlan()
	bad.Features[0].Fulfills = []string{"AC-DOES-NOT-EXIST"}
	_, err := Run(context.Background(), PlannerOptions{
		Client:    &stubClient{plan: bad},
		HMACKey:   []byte("k"),
		HMACKeyID: "k1",
	}, sampleParent())
	if !errors.Is(err, ErrCoveragePhantom) {
		t.Fatalf("expected ErrCoveragePhantom, got %v", err)
	}
}

func TestRun_FeatureCycle(t *testing.T) {
	bad := &ProgramBrief{
		Features: []PlannedFeature{
			{ID: "F-A", Title: "a", Fulfills: []string{"AC-1"}, DependsOnFeatures: []string{"F-B"}},
			{ID: "F-B", Title: "b", Fulfills: []string{"AC-2"}, DependsOnFeatures: []string{"F-A"}},
			{ID: "F-C", Title: "c", Fulfills: []string{"AC-3"}},
		},
	}
	_, err := Run(context.Background(), PlannerOptions{
		Client:    &stubClient{plan: bad},
		HMACKey:   []byte("k"),
		HMACKeyID: "k1",
	}, sampleParent())
	if !errors.Is(err, ErrFeatureCycle) {
		t.Fatalf("expected ErrFeatureCycle, got %v", err)
	}
}

func TestRun_UnknownDep(t *testing.T) {
	bad := goodModelPlan()
	bad.Features[0].DependsOnFeatures = []string{"F-DOES-NOT-EXIST"}
	_, err := Run(context.Background(), PlannerOptions{
		Client:    &stubClient{plan: bad},
		HMACKey:   []byte("k"),
		HMACKeyID: "k1",
	}, sampleParent())
	if !errors.Is(err, ErrFeatureUnknownDep) {
		t.Fatalf("expected ErrFeatureUnknownDep, got %v", err)
	}
}

func TestRun_ModelError(t *testing.T) {
	wantErr := errors.New("upstream 500")
	_, err := Run(context.Background(), PlannerOptions{
		Client:    &stubClient{err: wantErr},
		HMACKey:   []byte("k"),
		HMACKeyID: "k1",
	}, sampleParent())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped upstream error, got %v", err)
	}
}

func TestRun_NilClient(t *testing.T) {
	_, err := Run(context.Background(), PlannerOptions{HMACKey: []byte("k"), HMACKeyID: "k1"}, sampleParent())
	if err == nil {
		t.Fatal("expected error on nil client")
	}
}

func TestRun_NoKey(t *testing.T) {
	_, err := Run(context.Background(), PlannerOptions{Client: &stubClient{plan: goodModelPlan()}}, sampleParent())
	if err == nil {
		t.Fatal("expected error on missing HMAC key")
	}
}

func TestSignedPlanVerifies(t *testing.T) {
	plan, err := Run(context.Background(), PlannerOptions{
		Client:    &stubClient{plan: goodModelPlan()},
		HMACKey:   []byte("planner-test-key-32-bytes-padding"),
		HMACKeyID: "k1",
	}, sampleParent())
	if err != nil {
		t.Fatal(err)
	}
	keyring := map[string][]byte{"k1": []byte("planner-test-key-32-bytes-padding")}
	if err := plan.VerifySignature(keyring); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
}
