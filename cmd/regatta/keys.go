// keys subcommand: HMAC key rotation operator tooling. The re-sign-briefs
// verb walks .regatta/programs/*.json, verifies each brief under the
// retiring key, and rewrites it signed under the new active key so the
// retiring key can be removed from the keyring without breaking
// brief_loader verification at the next sync.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/trilamsr/regatta/internal/program"
)

const (
	keysVerbResignBriefs = "re-sign-briefs"
	briefJSONExt         = ".json"
)

// runKeys dispatches the `keys ...` subcommand tree. Today the only verb
// is re-sign-briefs; future rotation operations (e.g. key-strength
// audit, dump-active-key) hang off the same tree.
func runKeys(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "regatta keys: expected sub-subcommand (re-sign-briefs)")
		return 2
	}
	switch args[0] {
	case keysVerbResignBriefs:
		return runKeysResignBriefs(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "regatta keys: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runKeysResignBriefs re-signs every v1 brief in -dir whose signature
// key_id matches -old-key-id. Verification under the retiring key
// gates rewrite, so an operator who supplies the wrong old key gets a
// loud failure rather than silently corrupted briefs (#79 spec: "fail
// loud if any don't match"). Atomicity: temp file + os.Rename per
// brief, identical to atomicWriteBrief used by `program plan -write`.
// Idempotent: a brief already on -new-key-id verifies under the new
// key and is left untouched. Foreign key_ids (some unrelated rotation
// cohort) are skipped without error so one rotation does not bleed
// across cohorts.
func runKeysResignBriefs(args []string) int {
	fs := flag.NewFlagSet("keys re-sign-briefs", flag.ExitOnError)
	oldKeyID := fs.String("old-key-id", "", "key_id of the retiring key to re-sign FROM (required)")
	oldKeyEnv := fs.String("old-key-env", "", "Env var holding the retiring key material (required)")
	newKeyID := fs.String("new-key-id", "", "key_id to stamp into the re-signed brief (required)")
	newKeyEnv := fs.String("new-key-env", "", "Env var holding the new active key material (required)")
	dir := fs.String("dir", filepath.Join(".regatta", "programs"), "directory containing brief JSON files")
	dryRun := fs.Bool("dry-run", false, "report what would change without writing")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: regatta keys re-sign-briefs -old-key-id ID -old-key-env ENV -new-key-id ID -new-key-env ENV [-dir DIR] [-dry-run]")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	if *oldKeyID == "" || *newKeyID == "" || *oldKeyEnv == "" || *newKeyEnv == "" {
		fs.Usage()
		return 2
	}
	if *oldKeyID == *newKeyID {
		fmt.Fprintln(os.Stderr, "regatta keys re-sign-briefs: -old-key-id and -new-key-id must differ")
		return 2
	}
	oldKey := os.Getenv(*oldKeyEnv)
	if oldKey == "" {
		fmt.Fprintf(os.Stderr, "regatta keys re-sign-briefs: $%s is empty\n", *oldKeyEnv)
		return 2
	}
	newKey := os.Getenv(*newKeyEnv)
	if newKey == "" {
		fmt.Fprintf(os.Stderr, "regatta keys re-sign-briefs: $%s is empty\n", *newKeyEnv)
		return 2
	}

	entries, err := readBriefJSONNames(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "regatta keys re-sign-briefs:", err)
		return 1
	}

	var resigned, skipped int
	for _, name := range entries {
		path := filepath.Join(*dir, name)
		action, err := resignOneBrief(path, *oldKeyID, []byte(oldKey), *newKeyID, []byte(newKey), *dryRun)
		if err != nil {
			fmt.Fprintf(os.Stderr, "regatta keys re-sign-briefs: %s: %v\n", path, err)
			return 1
		}
		switch action {
		case actionResigned:
			resigned++
		case actionSkipped:
			skipped++
		}
	}
	verb := "re-signed"
	if *dryRun {
		verb = "would re-sign"
	}
	_, _ = fmt.Fprintf(os.Stdout, "regatta keys re-sign-briefs: %s %d brief(s), skipped %d under %s\n", verb, resigned, skipped, *dir)
	return 0
}

// resignAction reports what resignOneBrief did so the caller can tally
// without re-parsing stdout. actionSkipped covers both foreign key_ids
// and idempotent already-on-new-key briefs.
type resignAction int

const (
	actionSkipped resignAction = iota
	actionResigned
)

// resignOneBrief reads path as a v1 brief, verifies under the retiring
// key when sig.key_id == oldKeyID, and rewrites it under newKey. v2
// briefs are skipped (re-signing the v2 wire format needs a different
// canonicalisation path; tracked separately).
func resignOneBrief(path, oldKeyID string, oldKey []byte, newKeyID string, newKey []byte, dryRun bool) (resignAction, error) {
	raw, err := readBriefCapped(path)
	if err != nil {
		return actionSkipped, err
	}
	if program.IsV2Brief(raw) {
		// v2 re-sign is a separate code path (different canonical body); not in scope for #79.
		return actionSkipped, nil
	}
	var brief program.ProgramBrief
	if err := json.Unmarshal(raw, &brief); err != nil {
		return actionSkipped, fmt.Errorf("unmarshal: %w", err)
	}
	if brief.Signature.KeyID == newKeyID {
		// Idempotent: already on the new key. Verify under newKey so a
		// stale file with the right key_id but wrong MAC still surfaces
		// as a loud error rather than silently passing.
		if err := brief.VerifySignature(map[string][]byte{newKeyID: newKey}); err != nil {
			return actionSkipped, fmt.Errorf("brief already on key_id %q but signature does not verify under new key: %w", newKeyID, err)
		}
		return actionSkipped, nil
	}
	if brief.Signature.KeyID != oldKeyID {
		// Foreign cohort — leave it alone.
		return actionSkipped, nil
	}
	if err := brief.VerifySignature(map[string][]byte{oldKeyID: oldKey}); err != nil {
		return actionSkipped, fmt.Errorf("verify under -old-key-env: %w", err)
	}
	signed, err := brief.Sign(newKey, newKeyID)
	if err != nil {
		return actionSkipped, fmt.Errorf("re-sign: %w", err)
	}
	if dryRun {
		return actionResigned, nil
	}
	out, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		return actionSkipped, fmt.Errorf("marshal: %w", err)
	}
	if err := atomicWriteBrief(path, out, true); err != nil {
		return actionSkipped, fmt.Errorf("write: %w", err)
	}
	return actionResigned, nil
}

// maxResignBriefSize caps how many bytes the rotation tool will read
// from any single brief. Matches internal/program.maxBriefSize (1 MiB)
// so a stray garbage file in .regatta/programs cannot pin unbounded
// memory; legitimate briefs are KB.
const maxResignBriefSize int64 = 1 << 20

// readBriefCapped reads at most maxResignBriefSize bytes and errors on
// overflow so a hostile or corrupted file fails loud instead of being
// re-signed wholesale.
func readBriefCapped(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 — operator-supplied dir, rotation tool intentionally walks the tree.
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	defer func() { _ = f.Close() }()
	lr := &io.LimitedReader{R: f, N: maxResignBriefSize + 1}
	raw, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if int64(len(raw)) > maxResignBriefSize {
		return nil, fmt.Errorf("brief %s exceeds %d-byte cap", path, maxResignBriefSize)
	}
	return raw, nil
}

// readBriefJSONNames returns sorted *.json filenames in dir. Sort gives
// deterministic operator output across filesystems whose readdir order
// varies (ext4 hash vs APFS insertion).
func readBriefJSONNames(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != briefJSONExt {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no *.json briefs in %s", dir)
	}
	return names, nil
}
