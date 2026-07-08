package program

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator"
)

// isSignatureError reports whether err arose from HMAC verification
// (missing signature, tampered payload, unknown/weak key). Drives the
// RequireSigning fail-closed branch in BriefLoader.Sync (#1364); other
// error classes — parse, size cap, validate — stay warn+skip regardless
// of the flag because they are not the signature-enforcement contract.
func isSignatureError(err error) bool {
	return errors.Is(err, orchestrator.ErrHMACInvalid) ||
		errors.Is(err, schemas.ErrUnverifiable) ||
		errors.Is(err, schemas.ErrUnknownKeyID) ||
		errors.Is(err, schemas.ErrWeakKey)
}

// maxBriefSize caps the bytes read for any single brief. 1 MiB is ~3
// orders of magnitude above realistic and bounds the OOM blast radius
// of a malicious or corrupt drop. Rejected at Stat() — never reaches
// json.Unmarshal.
const maxBriefSize = 1 << 20

// LoadAndVerifyBrief reads path, unmarshals into ProgramBrief, runs
// Validate, then VerifySignature under keyring. Returns ErrHMACInvalid
// (wrapped) when the signature does not check out under any key.
// Rejects briefs whose on-disk size exceeds maxBriefSize before any
// read into RAM. v2 briefs route through LoadAndVerifyBriefV2 then
// project to v1 so downstream Sync (v1-only) operates unchanged.
func LoadAndVerifyBrief(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBrief, error) {
	if len(keyring) == 0 {
		return nil, fmt.Errorf("program: keyring required to verify briefs")
	}
	raw, err := readBriefBytes(fsys, path)
	if err != nil {
		return nil, err
	}
	if IsV2Brief(raw) {
		v2, err := loadAndVerifyV2FromBytes(raw, keyring)
		if err != nil {
			return nil, err
		}
		return projectV2ToV1(v2), nil
	}
	var brief ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return nil, fmt.Errorf("program: parse brief: %w", err)
	}
	if err := brief.Validate(); err != nil {
		return nil, fmt.Errorf("program: validate brief: %w", err)
	}
	if err := brief.VerifySignature(keyring); err != nil {
		return nil, fmt.Errorf("%w: %w", orchestrator.ErrHMACInvalid, err)
	}
	return &brief, nil
}

// loadAndVerifyBriefBoth returns the v1-projected brief and, when the
// source was v2, the raw v2 view (so Sync can lower edges +
// outputs_schemas without re-reading). v1 briefs return (brief, nil).
// Centralised here so LoadAndVerifyBrief stays the v1-projection
// caller and Sync gets both representations from one parse.
func loadAndVerifyBriefBoth(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBrief, *ProgramBriefV2, error) {
	if len(keyring) == 0 {
		return nil, nil, fmt.Errorf("program: keyring required to verify briefs")
	}
	raw, err := readBriefBytes(fsys, path)
	if err != nil {
		return nil, nil, err
	}
	if IsV2Brief(raw) {
		v2, err := loadAndVerifyV2FromBytes(raw, keyring)
		if err != nil {
			return nil, nil, err
		}
		return projectV2ToV1(v2), v2, nil
	}
	var brief ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return nil, nil, fmt.Errorf("program: parse brief: %w", err)
	}
	if err := brief.Validate(); err != nil {
		return nil, nil, fmt.Errorf("program: validate brief: %w", err)
	}
	if err := brief.VerifySignature(keyring); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", orchestrator.ErrHMACInvalid, err)
	}
	return &brief, nil, nil
}

// LoadAndVerifyBriefV2 is the v2-typed sibling of LoadAndVerifyBrief.
// v1 briefs are lowered via LowerV1ToV2 so callers see one
// representation.
func LoadAndVerifyBriefV2(fsys fs.FS, path string, keyring map[string][]byte) (*ProgramBriefV2, error) {
	if len(keyring) == 0 {
		return nil, fmt.Errorf("program: keyring required to verify briefs")
	}
	raw, err := readBriefBytes(fsys, path)
	if err != nil {
		return nil, err
	}
	if IsV2Brief(raw) {
		return loadAndVerifyV2FromBytes(raw, keyring)
	}
	var brief ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return nil, fmt.Errorf("program: parse brief: %w", err)
	}
	if err := brief.Validate(); err != nil {
		return nil, fmt.Errorf("program: validate brief: %w", err)
	}
	if err := brief.VerifySignature(keyring); err != nil {
		return nil, fmt.Errorf("%w: %w", orchestrator.ErrHMACInvalid, err)
	}
	return LowerV1ToV2(&brief), nil
}

func readBriefBytes(fsys fs.FS, path string) ([]byte, error) {
	info, err := fs.Stat(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("program: stat brief: %w", err)
	}
	if info.Size() > maxBriefSize {
		return nil, fmt.Errorf("program: brief %s size %d exceeds cap %d", path, info.Size(), maxBriefSize)
	}
	raw, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("program: read brief: %w", err)
	}
	return raw, nil
}

// loadAndVerifyV2FromBytes parses raw as v2, runs ValidateV2, then
// verifies HMAC against the canonicalised payload. HMAC reuse: marshal
// to JSON, decode into a generic map, run schemas.Verify on that map.
// Because the embedded ProgramBrief carries Signature, schemas.Verify
// operates on the same canonical body the v1 path uses.
func loadAndVerifyV2FromBytes(raw []byte, keyring map[string][]byte) (*ProgramBriefV2, error) {
	var v2 ProgramBriefV2
	if err := json.Unmarshal(raw, &v2); err != nil {
		return nil, fmt.Errorf("program: parse v2 brief: %w", err)
	}
	if err := v2.ValidateV2(); err != nil {
		return nil, fmt.Errorf("program: validate v2 brief: %w", err)
	}
	if err := v2.VerifySignatureV2(keyring); err != nil {
		return nil, fmt.Errorf("%w: %w", orchestrator.ErrHMACInvalid, err)
	}
	return &v2, nil
}
