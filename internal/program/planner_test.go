package program

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
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

func TestLoadPlannerPrompt_FromDiskNoSHA(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	want := "custom prompt"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPlannerPrompt(path, "")
	if err != nil {
		t.Fatalf("LoadPlannerPrompt: %v", err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLoadPlannerPrompt_FromDiskCorrectSHA(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	content := "pinned prompt"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(content))
	got, err := LoadPlannerPrompt(path, hex.EncodeToString(h[:]))
	if err != nil {
		t.Fatalf("LoadPlannerPrompt: %v", err)
	}
	if got != content {
		t.Fatalf("got %q want %q", got, content)
	}
}

func TestLoadPlannerPrompt_SHAMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	if err := os.WriteFile(path, []byte("actual"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPlannerPrompt(path, "0000000000000000000000000000000000000000000000000000000000000000")
	if !errors.Is(err, orchestrator.ErrBriefSHAMismatch) {
		t.Fatalf("err=%v want ErrBriefSHAMismatch", err)
	}
}

func TestLoadPlannerPrompt_FallbackOnMissingPath(t *testing.T) {
	got, err := LoadPlannerPrompt(filepath.Join(t.TempDir(), "nope.md"), "")
	if err != nil {
		t.Fatalf("LoadPlannerPrompt: %v", err)
	}
	if got != defaultPlannerPrompt {
		t.Fatalf("did not fall back to defaultPlannerPrompt")
	}
}

// Fix #1: SHA pin must fail closed when the file disappears. Falling
// back to defaultPlannerPrompt would silently swap the contents of a
// pinned prompt, defeating the pin.
func TestLoadPlannerPrompt_PinnedButMissingFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.md")
	pin := "0000000000000000000000000000000000000000000000000000000000000000"
	got, err := LoadPlannerPrompt(missing, pin)
	if err == nil {
		t.Fatalf("expected error when pinned path missing, got prompt %q", got)
	}
	if !errors.Is(err, orchestrator.ErrPlannerPromptMissing) {
		t.Fatalf("err=%v want wrap of ErrPlannerPromptMissing", err)
	}
}

// Fix #2: protect against operator typo pointing at a huge file
// (e.g. /var/log/system.log). Reject before allocating gigabytes.
func TestLoadPlannerPrompt_TooLargeRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 << 20); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = LoadPlannerPrompt(path, "")
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err=%v want 'too large' phrase", err)
	}
}

// Fix #3: a path that is not a regular file (directory, pipe, device)
// must be rejected — os.ReadFile on a pipe blocks forever.
func TestLoadPlannerPrompt_DirectoryPathRejected(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadPlannerPrompt(dir, "")
	if err == nil {
		t.Fatal("expected error when path is a directory")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("err=%v want 'regular file' phrase", err)
	}
}

// Fix #4: operators paste uppercase hex from CertUtil / OpenSSL.
// Lowercase normalize so the compare doesn't silently miss.
func TestLoadPlannerPrompt_UppercaseHexAccepted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	content := "pinned prompt"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte(content))
	got, err := LoadPlannerPrompt(path, strings.ToUpper(hex.EncodeToString(h[:])))
	if err != nil {
		t.Fatalf("LoadPlannerPrompt: %v", err)
	}
	if got != content {
		t.Fatalf("got %q want %q", got, content)
	}
}

// Fix #5: when path is missing AND no SHA pin, the silent fallback
// to the embedded prompt must leave a breadcrumb so operators can
// see why their custom prompt isn't loading.
func TestLoadPlannerPrompt_FallbackLogsMissing(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))

	missing := filepath.Join(t.TempDir(), "nope.md")
	if _, err := LoadPlannerPrompt(missing, ""); err != nil {
		t.Fatalf("LoadPlannerPrompt: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "planner.prompt.fallback") {
		t.Fatalf("expected fallback slog line, got:\n%s", out)
	}
	if !strings.Contains(out, "missing_file") {
		t.Fatalf("expected reason=missing_file in log, got:\n%s", out)
	}
}

// Fix #6: empty file content would short-circuit downstream API
// calls with an empty system prompt. Reject up-front.
func TestLoadPlannerPrompt_EmptyFileRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPlannerPrompt(path, "")
	if err == nil {
		t.Fatal("expected error for empty file")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err=%v want 'empty' phrase", err)
	}
}

// Fix #8: unreadable file. Skip on Windows (perm bits noop) and root
// (chmod 000 still readable).
func TestLoadPlannerPrompt_UnreadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file mode 000 is not enforced on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses mode 000")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	if err := os.WriteFile(path, []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, err := LoadPlannerPrompt(path, "")
	if err == nil {
		t.Fatal("expected error reading mode-000 file")
	}
}

// Fix #9: the SHA mismatch error should surface both got/want so
// operators can diff without re-hashing on the command line.
func TestLoadPlannerPrompt_SHAMismatchErrorIncludesGotWant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	if err := os.WriteFile(path, []byte("actual"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPlannerPrompt(path, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected SHA mismatch error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "got=") || !strings.Contains(msg, "want=") {
		t.Fatalf("err message must include got=/want=, got: %s", msg)
	}
}

// Fix #10: malformed SHA pin (wrong length or non-hex) should
// surface a config error early, not be treated as a content
// mismatch later.
func TestLoadPlannerPrompt_MalformedSHARejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"deadbeef",
		"0000000000000000000000000000000000000000000000000000000000000000z",
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
	}
	for _, pin := range cases {
		_, err := LoadPlannerPrompt(path, pin)
		if err == nil {
			t.Fatalf("pin=%q: expected error, got nil", pin)
		}
		if !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("pin=%q: err=%v want 'malformed' phrase", pin, err)
		}
	}
}
