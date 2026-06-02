# S3-T3 — HMAC key-rotation drill + operator recovery doc

**Status**: spec
**Owner**: self-host phase S3
**Brief**: [docs/engineer/briefs/2026-06-01-self-host-first.md](../briefs/2026-06-01-self-host-first.md) §3 S3-T3
**Memory cites**: `feedback_research_design_principles` · `feedback_grade_rubric` · `feedback_pr_body_file_only` · `feedback_pr_body_release_notes_mandatory` · `feedback_test_godoc_one_line`

---

## 1. Problem

Substrate W1 (#224) shipped the HMAC chain: every event in `event_log` carries `sig_alg`, `sig_key_id`, `sig_mac` over the canonical payload (`internal/orchestrator/state/substrate/sign.go`). The signing primitive supports multi-key verify by design — `Verify(e, keyring map[string][]byte)` looks the key up by `e.SigKeyID`. **Write** is single-key per `NewSubstrate(db, key, keyID)`; **read** is multi-key.

The W1 A+ rubric flagged a gap: **no operator-tested key-rotation procedure**. Today's loader (`cmd/regatta/serve.go:loadBriefKeyring`) reads exactly one `REGATTA_HMAC_KEY` + `REGATTA_HMAC_KEY_ID`. An operator who runs `kubectl create secret generic --from-literal=REGATTA_HMAC_KEY=$(openssl rand -hex 32)` and bounces the pod gets:

1. New events sign under `k2` — fine.
2. Old events in `event_log` verify against `k2` only — **all pre-rotation rows fail `ErrUnverifiable`**.
3. Operator has no rollback path other than restoring the old key from backups and re-doing rotation, which loses the new k2-signed events.

S3-T3 ships the **drill** (CLI surface + multi-key window + recovery procedure) plus the **operator doc** that walks through it end-to-end. The doc is the deliverable; the small code surface (multi-key keyring parsing + `regatta keys rotate` subcommand) follows the doc. Spec-only PR here; an implementation PR follows the spec land per `feedback_research_design_principles` (proven surface before code).

## 2. Prior art (≥2 OSS, per feedback_research_design_principles)

### 2.1 HashiCorp Vault — `transit` secret-engine key rotation

[Vault `transit/keys/<name>/rotate`](https://developer.hashicorp.com/vault/api-docs/secret/transit#rotate-key) maintains a numbered key history: every rotation increments the `latest_version`, and Vault keeps all prior versions available for `decrypt` while only `latest_version` signs new ciphertext. Operators set `min_decryption_version` to retire old keys after re-encrypting downstream data (`rewrap`). Two patterns we adopt:

1. **Latest-writes / all-read** asymmetry — exactly the substrate's `Sign(key, keyID)` + `Verify(keyring)` shape today. No code change needed to adopt this part; the doc just names it.
2. **Explicit retire step** — Vault does not delete old key versions until the operator runs `trim` with a `min_available_version`. We mirror with a `regatta keys retire --key-id=k1` subcommand that **first verifies every row in `event_log` has `sig_key_id != k1`** before allowing the removal. No silent retire.

Vault citation: `https://developer.hashicorp.com/vault/api-docs/secret/transit#rotate-key` (Apache-2.0 OSS, ~12k LoC Go in `vault/builtin/logical/transit/`).

### 2.2 Signal Protocol — double-ratchet key rotation

The [Signal double-ratchet](https://signal.org/docs/specifications/doubleratchet/) rotates per-message keys after every send/receive, but the **session keeps a small skipped-message keys buffer** so out-of-order messages still decrypt. The operationally relevant idea: **a rotation window where both keys are simultaneously valid for decrypt, bounded by an explicit retire condition** (the buffer drops keys after N skipped messages).

For substrate the analogous window is "both k_old and k_new accept verify until the operator runs `regatta keys retire --key-id=k_old` and the pre-flight check passes". The retire pre-flight is the analog of Signal's "N skipped messages" bound — an explicit condition, not a wall-clock timer. Wall-clock timers are operator-hostile (forgets which key was active when); the row-count check is decidable.

Signal citation: `https://signal.org/docs/specifications/doubleratchet/` §2.6 ("Out-of-order messages") (CC-BY-4.0 spec; libsignal Rust impl is GPL-3.0).

### 2.3 AWS KMS — automatic & manual key-rotation playbook

[AWS KMS automatic rotation](https://docs.aws.amazon.com/kms/latest/developerguide/rotate-keys.html) keeps **all** prior key material indefinitely (no operator retire step) because KMS hides the key-id mapping from callers — a `Decrypt` call carries the ciphertext's embedded key-id and KMS routes internally. Manual rotation requires the operator to update every alias and **re-encrypt all ciphertexts under the new key** before the old key CMK can be scheduled for deletion (7-30 day window).

We **reject** the "keep forever" KMS variant: substrate's `event_log` is append-only and growing; keeping every retired key indefinitely turns the keyring into a slow leak. We **adopt** KMS's mandatory re-encrypt-before-delete pattern as the row-scan precondition in §5.3 — substrate's analog is "every existing row's `sig_key_id` ∈ {keys not being retired}".

AWS docs citation: `https://docs.aws.amazon.com/kms/latest/developerguide/rotate-keys.html` (operator playbook; KMS impl is closed-source but the operator interface is public).

### Synthesis

Three patterns, one adoption per principle:

| Principle | Source | Substrate adoption |
|---|---|---|
| Latest-writes / all-read | Vault `transit` | already structurally present in `Sign` + `Verify`; doc names it |
| Explicit retire (no wall-clock) | Vault + Signal | `regatta keys retire --key-id=k1` with row-scan precondition |
| Re-encrypt-before-delete (re-verify-before-retire) | AWS KMS | row-scan blocks retire until all rows verify under remaining keys |

No external dependency added. The code delta is parser + one subcommand + one row-scan query.

## 3. Drill surface

### 3.1 Trigger: CLI subcommand, not a signal

**Decision**: trigger via `regatta keys rotate` subcommand under `cmd/regatta/`. Reject signals, config-flag re-reads, and SIGHUP-based reloads.

Rationale:

- **CLI subcommand** — operator-typed, audit-trail-friendly (the shell history names the rotation), composable in scripts. The existing `regatta program plan` + `regatta serve` siblings already establish this surface.
- **Not a signal** (SIGHUP) — signals are fire-and-forget, no exit code, no stdout, no audit. An operator who SIGHUPs the wrong PID gets silent failure. Rejected per `feedback_decision_priority` (UX > velocity).
- **Not a config-flag re-read** — the rotation IS a write event (`hmac_key_rotated` in `event_log`); making it implicit-on-flag-edit hides the rotation point in the substrate trail. Rejected.

Surface (new code, **NOT in this spec's PR**; this spec specifies the contract):

```sh
# Rotate to a new key. Old key stays in keyring for verify; new key becomes active for write.
regatta keys rotate \
  --new-key-env=REGATTA_HMAC_KEY_NEW \
  --new-key-id=k2

# Retire an old key. Pre-flight scans event_log for any row signed by the retiring key.
regatta keys retire --key-id=k1

# Inspect current keyring state.
regatta keys list
```

### 3.2 Multi-key window: how long do both keys stay valid?

**Decision**: window is **operator-bounded by row-scan pre-condition, not wall-clock**.

The window stays open as long as `event_log` has at least one row with `sig_key_id = k_old`. The operator closes the window with `regatta keys retire --key-id=k_old` only after either:

1. **Natural drain** — wait until the oldest k_old-signed row is older than `event_log_retention` (Phase X; today event_log is append-only). Not a path today; documented for completeness.
2. **Explicit re-sign sweep** (preferred) — operator runs `regatta keys resign --from=k1 --to=k2` (Phase B, **NOT in this PR**) which rewrites every event's signature with the new key. After the sweep, the row-scan precondition passes immediately.

For Phase S3-T3, the drill ships path #2 with the resign sweep **specced but not implemented**. The doc walks the operator through the rotate→serve-resigns→retire flow.

**Edge case** — a third party's exported event bundle (Phase X cross-instance replay) might re-introduce k_old rows after retire. The retire query records the retire timestamp in `event_log` itself (`hmac_key_retired` kind); any future replay attempt re-imports the retire event, fails verify against the now-missing key, surfaces `ErrUnknownKeyID` to the operator. This is the correct loud failure mode — the drill doc names it.

### 3.3 Multi-key keyring loader

Today: `loadBriefKeyring()` in `cmd/regatta/serve.go:402` reads one env var into a one-entry `map[string][]byte`.

After S3-T3 (code lands in follow-up PR):

```go
// Pseudocode, NOT this PR.
//
//   REGATTA_HMAC_KEYRING = "k1:hex,k2:hex,k3:hex"   ← multi-key, active = last
//   REGATTA_HMAC_KEY_ID  = "k2"                      ← override active for write
//
// Parser:
//   1. Split on ','.
//   2. For each pair: split on ':' once. Left = keyID, right = hex.
//   3. Validate len(decoded) >= schemas.MinKeyLen.
//   4. Reject duplicate keyIDs.
//   5. Active = REGATTA_HMAC_KEY_ID if set, else last entry.
//
// Backwards compat: if REGATTA_HMAC_KEY is set and REGATTA_HMAC_KEYRING
// is empty, fall back to today's one-entry behaviour. Existing operators
// don't have to rotate to roll forward.
```

Spec contract (this PR pins the shape; implementation PR honors it):

- Multi-key parser MUST accept the single-key legacy env unchanged.
- Multi-key parser MUST reject duplicate keyIDs with `ErrDuplicateKeyID` (no silent overwrite).
- Active write-key defaults to the **last** entry in keyring order (not alphabetical; rotation IS append). `REGATTA_HMAC_KEY_ID` overrides for the rare case the operator wants to write under an older key during a manual recovery.
- Verify path consumes the full keyring; substrate's existing `Verify(e, keyring)` needs zero change.

### 3.4 Recovery: substrate event-log replay + key-history table

Recovery is **two paths** depending on what the operator lost:

#### 3.4.1 Operator lost only the active key (new key compromised)

1. Operator generates a fresh key locally: `openssl rand -hex 32`.
2. `regatta keys rotate --new-key-env=REGATTA_HMAC_KEY_FRESH --new-key-id=k3` — keyring is now `{k1, k2, k3}` with k3 active.
3. (Out of scope this PR) `regatta keys resign --from=k2 --to=k3` — re-signs every row signed by the compromised k2.
4. `regatta keys retire --key-id=k2` — pre-flight passes (zero k2-signed rows), keyring becomes `{k1, k3}`.
5. Operator deletes the compromised k2 material from their secret store.

#### 3.4.2 Operator lost an old key (k_old material gone, event_log rows still signed by it)

1. Operator sets `REGATTA_HMAC_KEY_RECOVERY_MODE=1`. **Verify becomes warn-only for unknown-key-id rows** (logs `event.unverifiable_recovery`, does not block read).
2. Operator runs `regatta serve --tick-once` — orchestrator reduces the event_log into in-memory state. Reducer treats unverifiable rows as **structurally valid but signature-skipped**. Recovery-mode logs every skipped row to stderr.
3. Operator sweeps a re-sign pass against the active key (Phase B impl).
4. Operator unsets `REGATTA_HMAC_KEY_RECOVERY_MODE`, restarts `regatta serve` — verify is strict again.

Recovery mode is a **separate env var** (not a CLI flag) because it MUST be obvious in `ps aux` and `kubectl describe pod` output. Operators auditing a running instance see the env var or its absence at a glance.

#### 3.4.3 Key-history table — do we need one?

**No.** The substrate `event_log` IS the key-history. `hmac_key_rotated` and `hmac_key_retired` event kinds carry the keyring delta in their payload (key-id, retire-precondition row counts, operator identity, timestamp). A separate `key_history` table would be a denormalization of these events.

**Required schema delta**: `substrate_events.kind` carries a `CHECK (kind IN (…))` constraint pinned by `internal/orchestrator/state/migrations/0006_substrate.sql:44`. Adding two new kinds requires migration `0010_substrate_kind_hmac_key_events.sql` to relax the CHECK to include `'hmac_key_rotated'` and `'hmac_key_retired'`. The migration is one `ALTER TABLE`-equivalent rebuild (SQLite-style — drop + recreate with new CHECK + copy data) plus a corresponding `EventKind` constant pair in `internal/orchestrator/state/substrate/event.go` and the parity test (`TestSubstrate_EventKindEnumMatchesSQLCheck`) auto-updates.

No new **tables**. One new migration. Two new enum members. Per `feedback_deletion_default`: net add of two constants and one CHECK relax beats a brand-new table by a wide margin.

## 4. File-disjoint task breakdown

Implementation lands as **three follow-up PRs** after this spec, file-disjoint per `feedback_plan_subagent_dup_files`:

| PR | Files | Surface |
|---|---|---|
| S3-T3-A | `cmd/regatta/serve.go` (modify `loadBriefKeyring`), `cmd/regatta/serve_keyring_test.go` (new) | Multi-key keyring parser. Backwards-compat fallback. |
| S3-T3-B | `internal/orchestrator/state/migrations/0010_substrate_kind_hmac_key_events.sql` (new), `internal/orchestrator/state/substrate/event.go` (add two `EventKind` constants) | CHECK relax + enum extension. Migration-only PR per `feedback_migration_number_lock` (locks 0010). |
| S3-T3-C | `cmd/regatta/keys.go` (new), `cmd/regatta/keys_test.go` (new) | `regatta keys rotate / retire / list` subcommands + event-emit. Depends on -B. |
| S3-T3-D | `docs/operator/quickstart.md` (append §"Key rotation"), `docs/operator/key-rotation-drill.md` (new) | Operator drill doc; cross-link from quickstart Troubleshooting. Depends on -C land for CLI examples. |

PR boundaries explicit: **A and C touch only `cmd/regatta/`; B touches only `internal/orchestrator/state/`; D touches only `docs/operator/`**. A and B run in parallel (different packages). C waits on B (kind constant must exist). D waits on C (doc cites real CLI output). The resign sweep (`regatta keys resign`) is **deferred to S3-T3-E** with its own spec.

## 5. Adversarial reviewer notes

Expected reviewer findings (file the adversarial-reviewer subagent against this spec before opening any implementation PR):

- **Q: Why not just rotate the key with downtime + DB re-sign script and skip the multi-key window entirely?** A: downtime is acceptable for one operator today, but the substrate is meant to host external customers post-Phase X. The multi-key window keeps the substrate W1 design (`keyring map[string][]byte`) honest — refusing to use it because the operator is solo today leaves the API mis-shaped for the multi-tenant case. Per `feedback_decision_priority` (long-term > short-term).
- **Q: Recovery-mode env var means a hostile insider can disable verify just by setting it. Threat?** A: yes, that is the threat surface of any kill-switch. Mitigations: (a) recovery-mode emits `event.recovery_mode_entered` at boot — visible in audit; (b) `ps aux` shows the env var to any sibling-pod reader; (c) recovery mode is a documented operator action, not a hidden flag. Same surface AWS KMS exposes via `--bypass-policy-lockout-safety-check` on `aws kms put-key-policy`. Accepting.
- **Q: Doc-only PR can't fail CI in a way that catches drift between doc and impl. How is this enforced?** A: the implementation PRs (S3-T3-A / -B / -C) each cite this spec by path + commit SHA in their PR body. PR-A and PR-B add a smoke test that calls `regatta keys rotate / retire / list` and asserts the documented CLI shape. The doc-vs-impl drift surfaces as a test failure in the next implementation PR, not as silent doc-drift.
- **Q: Why a `hmac_key_rotated` event kind instead of a config-side audit log?** A: substrate IS the audit log. Adding a parallel config-side log would split the audit trail and force replay to merge two sources. The `event_log` schema already supports arbitrary `kind` strings; adding two literals (`hmac_key_rotated`, `hmac_key_retired`) is zero schema churn.
- **Q: Multi-key parser env var format `k1:hex,k2:hex` — why not JSON?** A: JSON in an env var forces shell quoting; the colon-comma format is `kubectl create secret generic` and Docker `--env`-pipeline friendly. The duplicate-keyID and weak-key validation runs at parse, not at use; malformed env fails loud at boot per `feedback_root_cause`.
- **Q: What if the operator runs `regatta keys retire` while `regatta serve` is mid-tick?** A: retire is a CLI subcommand that opens its own DB connection and runs the row-scan pre-flight under a single transaction. If a `serve` tick interleaves a new write under the retiring key, the pre-flight count is stale by the time the retire COMMITs. Mitigation: the retire transaction takes a SQLite reserved lock for its lifetime, and emits the `hmac_key_retired` event in the same tx. A concurrent `serve` tick blocks behind the lock or aborts on `SQLITE_BUSY`; either way the keyring is consistent. Implementation PR-B verifies with a property test.

## 6. Acceptance rubric

Per `feedback_grade_rubric`. Scorecard MUST appear verbatim in the PR body.

### B (floor — ships)

- [ ] `docs/engineer/specs/2026-06-02-s3-t3-key-rotation-drill.md` exists and is link-target-valid from the brief.
- [ ] Three prior-art citations resolve to public URLs.
- [ ] `bash scripts/doc-check.sh` exits 0 (no banned phrases, link integrity).
- [ ] `make pre-push-check` exits 0.
- [ ] PR body carries the scorecard verbatim and the `release-notes` fence.
- [ ] `[DOCS]` prefix on PR title.

### A (target — expected)

- [ ] B met.
- [ ] Three follow-up PRs (S3-T3-A / -B / -C) are file-disjoint and the disjointness is visible from the file-table in §4.
- [ ] Multi-key window mechanism (latest-writes / all-read) named in spec with substrate citation (`internal/orchestrator/state/substrate/sign.go:Verify`).
- [ ] Recovery procedure has both the "lost active key" and "lost old key" branches; neither branch requires DB surgery.
- [ ] Adversarial reviewer subagent run before any S3-T3-A implementation; findings folded back.
- [ ] Doc-vs-impl drift defense named (PR-A and PR-B smoke tests assert documented CLI shape).

### A+ (stretch — exceptional)

- [ ] A met.
- [ ] No new database tables. One migration (`0010_substrate_kind_hmac_key_events.sql`) for the CHECK relax — explicit, audited, named in §3.4.3.
- [ ] No new package. All code lands in existing `cmd/regatta/` package (multi-key parser, three subcommands) + one `EventKind` constant pair in `internal/orchestrator/state/substrate/event.go`.
- [ ] Spec under 400 lines including front-matter and rubric (anti-bloat — `feedback_deletion_default`).
- [ ] Backwards-compat: existing `REGATTA_HMAC_KEY` + `REGATTA_HMAC_KEY_ID` callers see zero behavioural change after S3-T3-A lands.

## 7. Implementation phasing (TDD strict for the follow-up PRs)

This PR is spec-only. Implementation phasing:

1. **S3-T3-A** (multi-key parser): failing test `TestLoadBriefKeyring_MultiKey` asserts the colon-comma format. Implementation lands the parser. Backwards-compat test `TestLoadBriefKeyring_LegacySingleKey` asserts zero behavioural change for existing env.
2. **S3-T3-B** (migration + enum): failing test `TestSubstrate_EventKindEnumMatchesSQLCheck` (existing) expanded to include the two new kinds. Migration 0010 lands. No CLI surface yet — pure schema + enum.
3. **S3-T3-C** (CLI subcommands, depends on -B): failing test `TestKeysRotate_EmitsEvent` asserts `hmac_key_rotated` lands in `substrate_events` with the keyring delta payload. `TestKeysRetire_PreFlight_Blocks_When_Rows_Signed` asserts the row-scan precondition. `TestKeysRetire_AtomicWithLock` asserts the SQLite reserved-lock behaviour.
4. **S3-T3-D** (operator doc, depends on -C): walks both recovery paths end-to-end against a fixture DB. Cross-link from quickstart Troubleshooting block (the existing brief.rejected entry already names "stale HMAC key after rotation" — we now have the procedure to point to).

## 8. Open questions

- **Resign sweep granularity** (S3-T3-D, separate spec): one-shot batch (`regatta keys resign`) vs incremental during normal ticks. Decision deferred to that spec; current S3-T3 land does not need it because the multi-key window is open-ended by design.
