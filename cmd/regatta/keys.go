// keys subcommand tree: `list` shows the current HMAC keyring, `rotate`
// validates new key material and prints the operator's next-step env-var
// line, `retire` runs the row-scan pre-flight against substrate_events
// and blocks if any row still verifies under the retiring keyID.
//
// Spec: docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md §3.1
// (CLI surface) + §3.4 (recovery + retire pre-flight). The keyring
// itself lives in `REGATTA_HMAC_KEYRING`; rotate does NOT mutate env —
// that is an out-of-band operator action (secret-store + restart). The
// CLI is the drill orchestrator + audit gate, not a secret store.
//
// Substrate event emission for `hmac_key_rotated` / `hmac_key_retired`
// kinds is deferred until S3-T3-B lands the migration relaxing the
// `substrate_events.kind` CHECK; see spec §3.4.3.
package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/orchestrator/state"
)

// keysDeps injects every side-effect the keys path touches so tests
// substitute fake stdio + DSN.
type keysDeps struct {
	Stdout io.Writer
	Stderr io.Writer
	DSN    string
}

// runKeys dispatches the `keys ...` subcommand tree.
func runKeys(args []string) int {
	return runKeysWith(keysDeps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		DSN:    state.DSN(defaultDBPath(args)),
	}, args)
}

func runKeysWith(deps keysDeps, args []string) int {
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta keys: expected sub-subcommand (list|rotate|retire)")
		return 2
	}
	// Subcommand literals: goconst exemption is intentional — the test
	// suite re-uses the same words at its dispatch site and a shared
	// const across cmd/regatta/{approval,keys}.go would couple two
	// independent subcommand trees that happen to share verb names.
	switch args[0] {
	case "list": //nolint:goconst
		return runKeysList(deps, args[1:])
	case "rotate": //nolint:goconst
		return runKeysRotate(deps, args[1:])
	case "retire": //nolint:goconst
		return runKeysRetire(deps, args[1:])
	default:
		_, _ = fmt.Fprintf(deps.Stderr, "regatta keys: unknown subcommand %q\n", args[0])
		return 2
	}
}

// runKeysList prints the configured keyring + active marker. The
// loader returns ("", "") on empty config — we surface that as exit 1
// so a misconfigured operator sees the failure at the CLI, not later
// when briefs land.
func runKeysList(deps keysDeps, _ []string) int {
	keys, active := loadBriefKeyringWithActive()
	if len(keys) == 0 {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta keys list: no keyring configured (set REGATTA_HMAC_KEYRING or REGATTA_HMAC_KEY)")
		return 1
	}
	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if id == active {
			_, _ = fmt.Fprintf(deps.Stdout, "%s (active)\n", id)
			continue
		}
		_, _ = fmt.Fprintln(deps.Stdout, id)
	}
	return 0
}

// runKeysRotate validates new key material and prints the operator's
// next-step env-var assignment. It does NOT mutate env — the secret
// store + pod restart are the actuator; the CLI is the validation +
// audit gate. The new keyID must not already exist in the current
// keyring (silent overwrite would lose the older verify path).
func runKeysRotate(deps keysDeps, args []string) int {
	fs := flag.NewFlagSet("keys rotate", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	envFlag := fs.String("new-key-env", "", "Env var holding the new HMAC key hex material (required)")
	idFlag := fs.String("new-key-id", "", "keyID to assign to the new key (required)")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta keys rotate --new-key-env <ENV> --new-key-id <id>")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *envFlag == "" || *idFlag == "" {
		fs.Usage()
		return 2
	}

	current, _ := loadBriefKeyringWithActive()
	if _, dup := current[*idFlag]; dup {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta keys rotate: duplicate keyID %q already in keyring\n", *idFlag)
		return 1
	}

	newHex := os.Getenv(*envFlag)
	if newHex == "" {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta keys rotate: env %s is empty or unset\n", *envFlag)
		return 1
	}
	decoded, err := hex.DecodeString(newHex)
	if err != nil {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta keys rotate: %s: hex decode: %v\n", *envFlag, err)
		return 1
	}
	if len(decoded) < schemas.MinKeyLen {
		_, _ = fmt.Fprintf(deps.Stderr, "regatta keys rotate: %s: weak key (%d bytes, MinKeyLen=%d)\n", *envFlag, len(decoded), schemas.MinKeyLen)
		return 1
	}

	merged := mergedKeyringEnv(current, *idFlag, newHex)
	_, _ = fmt.Fprintln(deps.Stdout, "regatta keys rotate: new key validated. Operator next step:")
	_, _ = fmt.Fprintln(deps.Stdout, "")
	_, _ = fmt.Fprintf(deps.Stdout, "  export REGATTA_HMAC_KEYRING=%q\n", merged)
	_, _ = fmt.Fprintln(deps.Stdout, "")
	_, _ = fmt.Fprintf(deps.Stdout, "Then restart `regatta serve`. Active write-key becomes %s; verify accepts all entries.\n", *idFlag)
	return 0
}

// mergedKeyringEnv builds the colon-comma keyring string with the new
// entry appended last (active-on-restart by §3.3 last-entry rule). The
// current order is recovered from the loader's view; absent a recorded
// order we sort the existing keyIDs alphabetically for determinism.
func mergedKeyringEnv(current map[string][]byte, newID, newHex string) string {
	ids := make([]string, 0, len(current))
	for id := range current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	pairs := make([]string, 0, len(current)+1)
	for _, id := range ids {
		pairs = append(pairs, fmt.Sprintf("%s:%s", id, hex.EncodeToString(current[id])))
	}
	pairs = append(pairs, fmt.Sprintf("%s:%s", newID, newHex))
	return strings.Join(pairs, ",")
}

// runKeysRetire opens the substrate DB and counts rows whose
// `sig_key_id` matches the retiring keyID. Non-zero count blocks the
// retire (exit 1) with the count + sample row id. The CLI does NOT
// rewrite the keyring — it is a precondition gate; the operator
// removes the entry from REGATTA_HMAC_KEYRING and restarts after the
// pre-flight passes.
func runKeysRetire(deps keysDeps, args []string) int {
	fs := flag.NewFlagSet("keys retire", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	keyIDFlag := fs.String("key-id", "", "keyID to retire (required)")
	_ = fs.String("db", "regatta.db", "Path to sqlite state DB")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(deps.Stderr, "Usage: regatta keys retire --key-id <id> [--db <path>]")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *keyIDFlag == "" {
		fs.Usage()
		return 2
	}

	dsn := deps.DSN
	if dsn == "" {
		dsn = state.DSN(defaultDBPath(args))
	}

	ctx := context.Background()
	db, err := state.Open(ctx, dsn)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta keys retire: open db:", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	count, sampleID, err := countRowsSignedBy(ctx, db.SQL(), *keyIDFlag)
	if err != nil {
		_, _ = fmt.Fprintln(deps.Stderr, "regatta keys retire: pre-flight query:", err)
		return 1
	}
	if count > 0 {
		_, _ = fmt.Fprintf(deps.Stderr,
			"regatta keys retire: pre-flight FAILED — %d substrate_events row(s) still signed by %q (sample id=%s). Run `regatta keys resign` before retiring.\n",
			count, *keyIDFlag, sampleID)
		return 1
	}
	_, _ = fmt.Fprintf(deps.Stdout, "regatta keys retire: %s safe to retire (0 rows in substrate_events signed by this key).\n", *keyIDFlag)
	_, _ = fmt.Fprintln(deps.Stdout, "")
	_, _ = fmt.Fprintln(deps.Stdout, "Operator next step: remove the entry from REGATTA_HMAC_KEYRING and restart `regatta serve`.")
	return 0
}

// countRowsSignedBy returns the count of substrate_events rows whose
// sig_key_id matches keyID plus a sample row id for the operator
// message. The query is bounded by the existing idx on (run_id, kind,
// key, written_at) — sig_key_id is not indexed today; a full table
// scan is acceptable because retire is an interactive operator command
// and the substrate is single-host for the self-host phase.
func countRowsSignedBy(ctx context.Context, db *sql.DB, keyID string) (int, string, error) {
	var count int
	var sampleID sql.NullString
	row := db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(MIN(id), '') FROM substrate_events WHERE sig_key_id = ?`,
		keyID,
	)
	if err := row.Scan(&count, &sampleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", nil
		}
		return 0, "", err
	}
	return count, sampleID.String, nil
}
