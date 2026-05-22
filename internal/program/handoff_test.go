package programs

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/trilamsr/regatta/schemas"
)

func validHandoff() Handoff {
	return Handoff{
		SchemaVersion: 1,
		ProgramID:     "m-aaaaaaaaaaaa",
		FeatureID:     "F-AUTH-01",
		WorkerRunID:   "11111111-1111-1111-1111-111111111111",
		BaseSHA:       "0123456789abcdef0123456789abcdef01234567",
		HeadSHA:       "fedcba9876543210fedcba9876543210fedcba98",
		SuccessState:  "success",
		CriteriaAddressed: []string{"AC-1", "AC-2"},
		CommandsRun: []HandoffCommand{
			{Cmd: "go test ./...", ExitCode: 0, StdoutSHA: "abcd"},
		},
		Falsifications: []Falsification{
			{
				Hypothesis:      "TestAuth panics on empty token",
				MutationKind:    "empty",
				TargetInvariant: "auth handler returns 401, never panics",
				Citation:        "test=internal/auth/handler_test.go:TestAuth_EmptyToken",
				Outcome:         "disproved_worker",
			},
		},
		TrapPatternsClaimed: []string{"P3"},
		ProducedAt:          time.Now().UTC(),
		Signature: schemas.SignatureBlock{
			Alg: "HMAC-SHA256", KeyID: "k1", MAC: "deadbeef",
		},
	}
}

func TestParseAndValidate_Valid(t *testing.T) {
	h := validHandoff()
	raw, _ := json.Marshal(h)
	got, err := ParseAndValidate(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.FeatureID != "F-AUTH-01" {
		t.Fatalf("feature mismatch: %s", got.FeatureID)
	}
}

func TestParseAndValidate_RejectsBadMissionID(t *testing.T) {
	h := validHandoff()
	h.ProgramID = "not-a-program-id"
	raw, _ := json.Marshal(h)
	_, err := ParseAndValidate(raw)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid, got %v", err)
	}
}

func TestParseAndValidate_RejectsBadSHA(t *testing.T) {
	h := validHandoff()
	h.HeadSHA = "shortie"
	raw, _ := json.Marshal(h)
	_, err := ParseAndValidate(raw)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid for bad SHA, got %v", err)
	}
}

func TestParseAndValidate_RejectsBadSuccessState(t *testing.T) {
	h := validHandoff()
	h.SuccessState = "kinda"
	raw, _ := json.Marshal(h)
	_, err := ParseAndValidate(raw)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid, got %v", err)
	}
}

func TestParseAndValidate_RejectsFalsificationWithoutCitation(t *testing.T) {
	h := validHandoff()
	h.Falsifications[0].Citation = "I tried things"
	raw, _ := json.Marshal(h)
	_, err := ParseAndValidate(raw)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid for bad citation, got %v", err)
	}
}

func TestParseAndValidate_RejectsBadMutationKind(t *testing.T) {
	h := validHandoff()
	h.Falsifications[0].MutationKind = "vibes"
	raw, _ := json.Marshal(h)
	_, err := ParseAndValidate(raw)
	if !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("expected ErrSchemaInvalid for bad mutation_kind, got %v", err)
	}
}

func TestParseAndValidate_RejectsTrapPatternWithoutFalsification(t *testing.T) {
	h := validHandoff()
	h.Falsifications[0].Outcome = "inconclusive"
	raw, _ := json.Marshal(h)
	_, err := ParseAndValidate(raw)
	if !errors.Is(err, ErrFalsificationMissingForPattern) {
		t.Fatalf("expected ErrFalsificationMissingForPattern, got %v", err)
	}
}

func TestCoverageCheck(t *testing.T) {
	h := validHandoff()
	if err := h.CoverageCheck([]string{"AC-1", "AC-2", "AC-3"}); err != nil {
		t.Fatalf("unexpected coverage failure: %v", err)
	}
	if err := h.CoverageCheck([]string{"AC-1"}); !errors.Is(err, ErrCoverageViolation) {
		t.Fatalf("expected coverage violation, got %v", err)
	}
}

func TestReRunMismatch_AllMatch(t *testing.T) {
	h := validHandoff()
	runs := []ReRunResult{
		{Cmd: "go test ./...", ExitCode: 0, StdoutSHA: "abcd"},
	}
	if bad := h.ReRunMismatch(runs); len(bad) != 0 {
		t.Fatalf("expected no mismatch, got %v", bad)
	}
}

func TestReRunMismatch_ExitCodeMismatch(t *testing.T) {
	h := validHandoff()
	runs := []ReRunResult{
		{Cmd: "go test ./...", ExitCode: 1, StdoutSHA: "abcd"},
	}
	bad := h.ReRunMismatch(runs)
	if len(bad) != 1 || bad[0] != 0 {
		t.Fatalf("expected mismatch at index 0, got %v", bad)
	}
}

func TestReRunMismatch_DifferentLength(t *testing.T) {
	h := validHandoff()
	runs := []ReRunResult{}
	bad := h.ReRunMismatch(runs)
	if len(bad) != 1 {
		t.Fatalf("expected length-mismatch flag, got %v", bad)
	}
}

func TestSignAndVerifyRoundTrip(t *testing.T) {
	key := []byte("test-key-for-handoff-signing-32b")
	keyring := map[string][]byte{"k1": key}

	h := validHandoff()
	// Marshal without signature, sign over canonical form, write signature back.
	raw, _ := json.Marshal(h)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)

	sig, err := schemas.Sign(generic, key, "k1")
	if err != nil {
		t.Fatal(err)
	}
	generic["signature"] = map[string]any{
		"alg":    sig.Alg,
		"key_id": sig.KeyID,
		"mac":    sig.MAC,
	}

	if err := schemas.Verify(generic, keyring); err != nil {
		t.Fatalf("verify failed: %v", err)
	}

	// Tamper: flip success_state, verify fails.
	generic["success_state"] = "failure"
	if err := schemas.Verify(generic, keyring); !errors.Is(err, schemas.ErrUnverifiable) {
		t.Fatalf("expected ErrUnverifiable after tamper, got %v", err)
	}
}
