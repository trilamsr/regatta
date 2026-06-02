package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trilamsr/regatta/internal/program"
)

// keysResignTestBrief writes a v1 brief signed with key/keyID.
func keysResignTestBrief(t *testing.T, dir string, key []byte, keyID string) string {
	t.Helper()
	brief := &program.ProgramBrief{
		SchemaVersion:    1,
		ProgramID:        "m-aaaaaaaaaaaa",
		ParentWorkItemID: "RFC-X",
		ParentCriteria: []program.PlanCriterion{
			{ID: "AC-1", Text: "criterion one"},
		},
		PlannerModelID: "stub:test",
		Features: []program.PlannedFeature{
			{ID: "F-A", Title: "feature A", Fulfills: []string{"AC-1"}},
		},
		ProducedAt: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
	}
	signed, err := brief.Sign(key, keyID)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, signed.ProgramID+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// TestKeysResignBriefs_HappyPath: re-signed brief verifies after k1 retired.
func TestKeysResignBriefs_HappyPath(t *testing.T) {
	dir := t.TempDir()
	progDir := filepath.Join(dir, ".regatta", "programs")
	if err := os.MkdirAll(progDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oldKey := []byte("old-key-material-32-bytes-padding")
	newKey := []byte("new-key-material-32-bytes-padding")
	briefPath := keysResignTestBrief(t, progDir, oldKey, "k1")

	t.Setenv("REGATTA_OLDK", string(oldKey))
	t.Setenv("REGATTA_NEWK", string(newKey))

	rc := runKeys([]string{
		"re-sign-briefs",
		"-old-key-id", "k1",
		"-old-key-env", "REGATTA_OLDK",
		"-new-key-id", "k2",
		"-new-key-env", "REGATTA_NEWK",
		"-dir", progDir,
	})
	if rc != 0 {
		t.Fatalf("runKeys exit=%d want 0", rc)
	}

	raw, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatalf("read re-signed brief: %v", err)
	}
	var out program.ProgramBrief
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Signature.KeyID != "k2" {
		t.Fatalf("key_id=%q want k2", out.Signature.KeyID)
	}

	// After retiring k1, the brief still verifies under the new
	// keyring — the whole point of the rotation tool.
	postRetire := map[string][]byte{"k2": newKey}
	if err := out.VerifySignature(postRetire); err != nil {
		t.Fatalf("verify post-retire: %v", err)
	}
}

// TestKeysResignBriefs_Idempotent: second run leaves brief bytes unchanged.
func TestKeysResignBriefs_Idempotent(t *testing.T) {
	dir := t.TempDir()
	progDir := filepath.Join(dir, ".regatta", "programs")
	if err := os.MkdirAll(progDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	oldKey := []byte("old-key-material-32-bytes-padding")
	newKey := []byte("new-key-material-32-bytes-padding")
	briefPath := keysResignTestBrief(t, progDir, newKey, "k2")

	t.Setenv("REGATTA_OLDK", string(oldKey))
	t.Setenv("REGATTA_NEWK", string(newKey))

	before, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	rc := runKeys([]string{
		"re-sign-briefs",
		"-old-key-id", "k1",
		"-old-key-env", "REGATTA_OLDK",
		"-new-key-id", "k2",
		"-new-key-env", "REGATTA_NEWK",
		"-dir", progDir,
	})
	if rc != 0 {
		t.Fatalf("runKeys exit=%d want 0", rc)
	}
	after, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("brief mutated on idempotent re-run")
	}
}

// TestKeysResignBriefs_WrongOldKeyFailsLoud: wrong key aborts before write.
func TestKeysResignBriefs_WrongOldKeyFailsLoud(t *testing.T) {
	dir := t.TempDir()
	progDir := filepath.Join(dir, ".regatta", "programs")
	if err := os.MkdirAll(progDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	realOld := []byte("real-old-key-32-bytes-padding-AA")
	wrongOld := []byte("wrong-old-key-32-bytes-padding-A")
	newKey := []byte("new-key-material-32-bytes-padding")
	briefPath := keysResignTestBrief(t, progDir, realOld, "k1")
	before, _ := os.ReadFile(briefPath)

	t.Setenv("REGATTA_OLDK", string(wrongOld))
	t.Setenv("REGATTA_NEWK", string(newKey))

	rc := runKeys([]string{
		"re-sign-briefs",
		"-old-key-id", "k1",
		"-old-key-env", "REGATTA_OLDK",
		"-new-key-id", "k2",
		"-new-key-env", "REGATTA_NEWK",
		"-dir", progDir,
	})
	if rc == 0 {
		t.Fatalf("runKeys exit=0 want non-zero on wrong old key")
	}
	after, _ := os.ReadFile(briefPath)
	if string(before) != string(after) {
		t.Fatalf("brief mutated despite verify failure (silent data loss)")
	}
}

// TestKeysResignBriefs_SkipsForeignKeyID: non-target key_id left untouched.
func TestKeysResignBriefs_SkipsForeignKeyID(t *testing.T) {
	dir := t.TempDir()
	progDir := filepath.Join(dir, ".regatta", "programs")
	if err := os.MkdirAll(progDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	otherKey := []byte("other-key-material-32-bytes-pad-A")
	newKey := []byte("new-key-material-32-bytes-padding")
	// Brief signed under "k99" — not the retiring keyID "k1".
	briefPath := keysResignTestBrief(t, progDir, otherKey, "k99")
	before, _ := os.ReadFile(briefPath)

	t.Setenv("REGATTA_OLDK", string(otherKey))
	t.Setenv("REGATTA_NEWK", string(newKey))

	rc := runKeys([]string{
		"re-sign-briefs",
		"-old-key-id", "k1",
		"-old-key-env", "REGATTA_OLDK",
		"-new-key-id", "k2",
		"-new-key-env", "REGATTA_NEWK",
		"-dir", progDir,
	})
	if rc != 0 {
		t.Fatalf("runKeys exit=%d want 0 (skip is success)", rc)
	}
	after, _ := os.ReadFile(briefPath)
	if string(before) != string(after) {
		t.Fatalf("foreign-key brief mutated; rotation must filter by --old-key-id")
	}
}

// TestKeysResignBriefs_MissingEnvFailsUsage: empty env exits 2.
func TestKeysResignBriefs_MissingEnvFailsUsage(t *testing.T) {
	dir := t.TempDir()
	progDir := filepath.Join(dir, ".regatta", "programs")
	if err := os.MkdirAll(progDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("REGATTA_OLDK", "")
	t.Setenv("REGATTA_NEWK", "new-key-material-32-bytes-padding")

	rc := runKeys([]string{
		"re-sign-briefs",
		"-old-key-id", "k1",
		"-old-key-env", "REGATTA_OLDK",
		"-new-key-id", "k2",
		"-new-key-env", "REGATTA_NEWK",
		"-dir", progDir,
	})
	if rc != 2 {
		t.Fatalf("runKeys exit=%d want 2 on empty env", rc)
	}
}

