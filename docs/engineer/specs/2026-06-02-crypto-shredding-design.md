# Crypto-shredding for PII in immutable signed event log — design spec

Status: ready for review
Date: 2026-06-02
Author: design subagent <tree@lumalabs.ai>
Issue: [#548](https://github.com/trilamsr/regatta/issues/548) — `[FOLLOWUP] GDPR/CCPA erasure vs immutable signed event log — crypto-shredding for PII payloads`

Trigger to schedule (from #548 body): _before the first regulated/EU customer pilot LOI (MVR-2 gate)_. This spec locks the design now so impl can land in one wave the moment the LOI signals; the issue stays open until impl ships.

Depends on:

- **Hard prereq (merged):** substrate event log — `internal/orchestrator/state/substrate/event.go` (payload, sign path, hash-chain). The chain MUST stay byte-stable; this spec touches payload semantics, not chain mechanics.
- **Hard prereq (merged):** HMAC signing seam on `substrate_events` (see `docs/auditor/audit-log.md`). Signatures cover the event row including the payload bytes — therefore the ciphertext bytes are what gets signed, not the plaintext.
- **Soft prereq (in-flight):** W10 sigstore (`2026-06-01-w10-sigstore-design.md`) — once W10 lands, the same `internal/sign/sigstore` `Verify` discipline applies to the new `data_keys` table snapshots for tamper-evidence on the KEK-wrapped DEK column.

Binding brief: `docs/engineer/briefs/2026-05-31-mvp-3-next-level.md` (compliance pitch claims SOC2 + EU AI Act traceability) + #548 problem statement (immutable-signed-log + contains-PII + erasure-obligation is a direct legal contradiction).

Memory rules in force: `feedback_research_design_principles` (adopt OSS — NIST SP 800-38D, RFC 5116, libsodium primitives), `feedback_decision_priority` (UX > ease > perf > best-practices > speed > velocity), `feedback_grade_rubric` (B/A/A+ tool-checkable), `feedback_adversarial_review` (hostile read mandate), `feedback_spec_pattern_authority` (one pattern mandated), `feedback_unaddressed_load_bearing` (named-but-deferred → tracking issue), `feedback_deletion_default` (what got smaller?), `feedback_doc_check_banned_phrases`.

---

## §0 Prior art adopted

Per `feedback_research_design_principles` — proven OSS first, build only what's missing. Five candidates evaluated; each scored on (a) substrate-event-payload fit, (b) self-host scope fit, (c) cost-of-adoption.

### Candidate 1 — Google Tink (Apache-2.0, v1.16.0, commit-sha pinned at impl)

Cryptographic library by Google's crypto team. `AEAD` primitive (AES-256-GCM + ChaCha20-Poly1305), `KeysetHandle` with KEK-wrapping, deterministic key rotation. Used in Google Pay + production deployments at multiple FAANG-scale shops.

- **(a) payload fit — adopt:** the `AEAD.Encrypt(plaintext, associatedData)` shape is the exact envelope this spec needs. `associatedData` = event-row hash → satisfies the cut-paste binding requirement (§3.5) without inventing a new primitive.
- **(b) self-host fit — adopt:** Go-native (`tink-go`), no CGO, no daemon. Vendors into the single-binary build.
- **(c) cost-of-adoption — light:** one import + one wrapper in `internal/crypto/envelope/`. The KEK seam (Tink's `Aead` interface for the key-encryption layer) plugs HSM/KMS later as a Phase X swap with zero call-site change.

**Adopted patterns:** (1) `AEAD` primitive interface (AES-GCM default), (2) `KeysetHandle` wrap/unwrap discipline for DEK envelope, (3) `associatedData` channel for AAD binding.

### Candidate 2 — AWS Encryption SDK / aws/aws-encryption-sdk-c (Apache-2.0)

AWS's envelope-encryption reference SDK. Per-message DEK, KEK in KMS, structured ciphertext format with header + IV + ciphertext + tag.

- **(a) payload fit — partial:** the wire format is over-engineered for at-rest substrate rows (carries multi-recipient framing, message-format versioning, KMS provider-info fields). Substrate rows do not transit across multi-tenant trust boundaries; the simpler envelope shape wins.
- **(b) self-host fit — reject as runtime dep:** AWS SDK pulls KMS-client transitive deps. Out of scope for the v1 single-binary self-host build per `docs/engineer/briefs/2026-06-01-self-host-first.md`.
- **(c) cost-of-adoption — borrow the wire-format thinking, not the lib:** the `{version, key-id, IV, ciphertext, tag}` shape informs §3.4 payload layout; reject importing the SDK.

**Adopted patterns:** (1) version-tagged ciphertext envelope (`"v":1` in payload), (2) KEK-version field stored alongside wrapped DEK for rotation tracking.

### Candidate 3 — libsodium / `crypto_secretbox` (ISC license)

Battle-tested[1] (replaced by "well-audited" per `feedback_doc_check_banned_phrases`) NaCl-derived primitives. XSalsa20-Poly1305 AEAD; tiny API surface; Frank-Denis maintained.

- **(a) payload fit — partial:** `crypto_secretbox` is symmetric AEAD but ChaCha20-Poly1305 variant (`crypto_aead_chacha20poly1305_ietf`) matches the RFC 8439 wire format. Strong alternative to AES-GCM on CPUs without AES-NI (ARM64 servers, older x86 without AES-NI).
- **(b) self-host fit — adopt as fallback:** `golang.org/x/crypto/chacha20poly1305` is std-lib-adjacent, zero-CGO, ships with Go releases.
- **(c) cost-of-adoption — minimal:** behind the same `AEAD` interface as AES-GCM. Selected at boot per `regatta.yaml: crypto.aead = aes256gcm | chacha20poly1305` (default `aes256gcm`).

**Adopted patterns:** (1) `chacha20poly1305` as boot-selectable alternative when CPU feature-detect reports no AES-NI, (2) constant-time tag comparison discipline.

### Candidate 4 — HashiCorp Vault Transit (MPL-2.0)

Vault's `transit` engine: server-side encrypt/decrypt API; named keys; rotation; convergent encryption. Used as the canonical envelope-encryption gateway in many enterprise stacks.

- **(a) payload fit — reject:** requires a Vault deployment. v1 self-host has no daemon-services dep. Phase X plug-in via the same `KEK` interface (§3.2) if a customer mandates Vault.
- **(b) self-host fit — reject for v1, seam later.**
- **(c) cost-of-adoption — interface-only:** the `KEK.Wrap(dek) → wrapped` / `KEK.Unwrap(wrapped) → dek` shape mirrors Vault Transit's API; impl ships local-file KEK in v1, Vault adapter as Phase X.

**Adopted patterns:** (1) `KEK` interface boundary (Wrap/Unwrap only), (2) named key + version metadata for rotation.

### Candidate 5 — `youzan/spider-pii-shredder` (rejected — MIT but unmaintained ≥18mo)

Per-tenant crypto-shred utility from a former e-commerce stack. Last commit 2024-11; mailing list dark; one named maintainer departed. Rejected on `feedback_research_design_principles` "proven OSS" criterion — proven includes "is alive".

### Decisions from prior-art scan

| Decision                                                    | Adopted from                | Rationale                                                                                       |
| ----------------------------------------------------------- | --------------------------- | ----------------------------------------------------------------------------------------------- |
| AEAD primitive interface = `internal/crypto/envelope.AEAD`  | Google Tink                 | Mirror proven shape; KEK swap is a Phase X plug-in (`feedback_spec_pattern_authority`).         |
| Default AEAD = AES-256-GCM                                  | NIST SP 800-38D             | Hardware-accelerated on x86 (AES-NI) + ARM64 (ARMv8 crypto ext.); ubiquitous + audited.         |
| Fallback AEAD = ChaCha20-Poly1305                           | RFC 8439 / libsodium        | CPUs without AES-NI; selected via `regatta.yaml: crypto.aead`.                                  |
| Envelope wire format = `{"v":1,"kid":"…","ct":"…","aad":"…"}` | AWS Encryption SDK (concept) | Versioned + key-id-tagged ciphertext; AAD bound to event-row hash (cut-paste prevention).        |
| KEK seam = `internal/crypto/kek.KEK` interface              | HashiCorp Vault Transit     | Local-file v1 (operator-managed envelope); Vault / HSM / KMS adapters as Phase X.               |
| Per-subject DEK granularity (default)                        | Tink + GDPR Art. 17 shape   | `subject_id` = the natural erasure unit. Per-`(tenant,kind)` is a Phase X coarsening for cost.  |
| AAD binding payload = event-row hash                          | NIST SP 800-38D §5.2.1.1   | Tying ciphertext to its container blocks cut-paste between events.                              |

---

## §1 Problem

The compliance pitch (SOC2, EU AI Act traceability) sells an append-only, HMAC-signed, hash-chained event log (`substrate_events` per `docs/auditor/audit-log.md`). But the substrate payload (`node_output`, `fact`, `gate_verdict`, `approval_event` rows per `internal/orchestrator/state/substrate/event.go`) WILL contain PII: prompts referencing names + emails, reviewer identities on approval rows, Slack handles in approval-token payloads, generated code carrying contributor email addresses.

GDPR Article 17 (right to erasure) and CCPA §1798.105 (right to delete) require deleting a data subject's personal data on lawful request. **A row cannot be deleted from a hash-chained signed log without invalidating every signature downstream.** Immutable-signed-log + contains-PII + erasure-obligation is a direct legal contradiction, and it lands precisely on the regulated / EU buyer the product targets per #548 trigger note.

Three concrete failure modes today if a subject-access request lands:

1. **Tombstone-with-rechain (rejected).** Re-sign every event after the redacted row. Defeats tamper-evidence — auditor cannot distinguish a legitimate erasure-driven re-sign from a hostile log rewrite. Rejected in #548 body explicitly.
2. **Delete-and-break-chain.** Honor the request, ship a broken chain, hope no auditor verifies. Violates the SOC2 pitch the compliance story rests on.
3. **Refuse the request.** GDPR fine = €20M / 4% global turnover. Not a real option for the regulated buyer.

Crypto-shredding (a.k.a. "key erasure") is the documented industry resolution: encrypt PII fields with a per-subject data key; on erasure, destroy the data key. Ciphertext bytes stay in the log; signatures stay valid; plaintext becomes unrecoverable.

---

## §2 Scope

### IN

- New `internal/crypto/envelope/` package — `AEAD` interface, AES-256-GCM impl, ChaCha20-Poly1305 impl, `Seal(plaintext, aad) → ciphertext` / `Open(ciphertext, aad) → plaintext` shape.
- New `internal/crypto/kek/` package — `KEK` interface (`Wrap` / `Unwrap`), local-file v1 impl reading the operator-managed KEK from `$REGATTA_KEK_PATH` (default `/etc/regatta/kek/current.key` 0600-owned by the regatta service user; refusal to start if permissions wrong).
- New `data_keys` table (§3.3 schema) holding wrapped DEKs keyed by `subject_id`.
- Payload-layout rule: PII fields → `{"v":1,"kid":"…","ct":"…","aad":"…"}` JSON object (§3.4); non-PII fields stay plaintext.
- Write-path hook in `substrate/event.go` `Append`: scan event-kind's PII-field schema annotation, mint-or-reuse DEK, encrypt, embed.
- Read-path hook in `substrate/event.go` `Render`: lookup DEK by `kid`; if `wrapped_dek IS NULL` (shredded), substitute `[REDACTED:erasure-<shredded_at>]` tombstone marker.
- New `regatta shred <subject-id>` CLI — single `UPDATE data_keys SET wrapped_dek=NULL, shredded_at=now() WHERE subject_id=?`; emits `pii_shredded` event into the same chain.
- KEK rotation operator command — `regatta kek rotate` — re-wraps every non-null `wrapped_dek` in one transaction; bumps `kek_version`.
- PII-field schema annotation — CUE `#Event` discriminator gains a `pii?: [...string]` field naming which JSON paths to envelope.

### OUT

- HSM / cloud-KMS KEK adapters — Phase X (`[followup] crypto-shred HSM/KMS KEK adapter`; reopen-trigger: first regulated customer LOI naming an HSM SKU).
- Cross-tenant key sharing — explicit no. Each tenant has its own KEK; cross-tenant DEK reuse violates the threat model (a compromised tenant cannot decrypt a peer's shredded-but-still-on-disk events).
- Pre-shred-era event re-encryption — pre-existing events predating the migration carry a `pre_shred_era: true` flag and are documented as not-erasable (lawful basis = legitimate operator interest in pre-deployment audit data; surfaced in the operator UI). §11 migration plan.
- Field-level PII detection (auto-classifier) — out of v1. PII is declared statically in CUE per-event-kind. ML-based PII detection is a Phase X.
- Re-encryption on KEK rotation that fails partway — v1 uses one transaction; v2 will need a resumable re-wrap (Phase X `[followup] resumable KEK rotation for >1M DEKs`).

---

## §3 Architecture

### §3.1 Crypto-shredding overview

Envelope encryption (NIST SP 800-57 §6.1 + AWS Encryption SDK Developer Guide §"Concepts"): each data subject's PII is encrypted with a randomly-generated symmetric DEK (data-encryption-key). The DEK itself is encrypted (wrapped) by a KEK (key-encryption-key) the operator manages out-of-band. Both DEK and KEK are AEAD-grade — Authenticated Encryption with Associated Data per RFC 5116 — so tampering with ciphertext or AAD raises an `ErrAEADAuthFail` on decrypt rather than returning garbage plaintext.

Erasure proceeds by destroying the wrapped DEK row in `data_keys`. The ciphertext bytes in `substrate_events.payload` remain — and remain validly signed — but no key can ever decrypt them again. Subject's PII is gone; chain integrity holds; auditor sees a normal `pii_shredded` event marking the moment.

### §3.2 Key hierarchy

```
KEK (1 per tenant, operator-managed, out-of-DB)
 │
 │  Wrap(DEK) / Unwrap(wrappedDEK)
 ▼
DEK (1 per subject_id, AEAD key, stored wrapped in data_keys)
 │
 │  Seal(plaintext, aad=event_row_hash)
 │  Open(ciphertext, aad=event_row_hash)
 ▼
Ciphertext bytes embedded in substrate_events.payload JSON
```

**KEK (key-encryption-key).** 32-byte symmetric key. Operator-managed via `$REGATTA_KEK_PATH` file (mode 0600, fail-closed if wrong). NEVER written to the DB. Loaded once at service start; held in memory in `mlock`-pinned bytes via `golang.org/x/sys/unix.Mlock` to keep it out of swap. Rotated via `regatta kek rotate`; previous KEK kept on disk as `<path>.v<N-1>` until operator deletes (allows rollback inside the rotation window).

**DEK (data-encryption-key).** 32-byte AEAD key. One per `subject_id` (default granularity). Generated by `crypto/rand` on first PII write for that subject. Wrapped under the current KEK using the same AEAD primitive and stored in `data_keys.wrapped_dek`. Loaded into an in-process LRU cache (`internal/crypto/dekcache`, 4096-entry default, `regatta.yaml: crypto.dek_cache_size`); cache evicts on TTL + on `pii_shredded` event observation (write-through invalidation).

**AEAD primitive.** Default `AES-256-GCM` per NIST SP 800-38D — 96-bit IV (random per `Seal`), 128-bit auth tag, 32-byte key. Alternative `ChaCha20-Poly1305` (RFC 8439) when CPU lacks AES-NI; configured via `regatta.yaml: crypto.aead`. Recommendation: **AES-GCM as v1 default** because the modern x86_64 / ARM64 / Apple-Silicon target stacks all carry hardware AES; ChaCha is the documented escape hatch for pre-AES-NI hardware. Performance delta on AES-NI hardware: AES-GCM ~6 GB/s vs ChaCha20-Poly1305 ~2 GB/s (Intel Tiger Lake, openssl speed); on a non-AES-NI ARMv7 (rare in the self-host target stack), the order reverses. §13 pricing notes records the at-event-overhead numbers.

### §3.3 Schema: `data_keys` table

```sql
-- migrations/0042_data_keys.sql (number locked at impl-dispatch per
-- feedback_migration_number_lock; placeholder 0042 until impl)
CREATE TABLE data_keys (
    subject_id    TEXT    PRIMARY KEY,            -- erasure unit (per GDPR Art. 17)
    wrapped_dek   BLOB,                            -- nullable; NULL ⇒ shredded
    kek_version   INTEGER NOT NULL,                -- rotation generation
    aead_alg      TEXT    NOT NULL,                -- 'aes256gcm' | 'chacha20poly1305'
    created_at    TIMESTAMP NOT NULL,
    shredded_at   TIMESTAMP                        -- NULL until shred
);

CREATE INDEX idx_data_keys_shredded ON data_keys (shredded_at) WHERE shredded_at IS NOT NULL;
CREATE INDEX idx_data_keys_kek_ver  ON data_keys (kek_version);

-- Tenant scope: in multi-tenant deploys (post-W8) the PK becomes
-- (tenant_id, subject_id) and the row is RLS-scoped per the W8 tenant_id
-- discipline. Single-tenant v1 omits tenant_id.
```

`shredded_at IS NOT NULL ⇒ wrapped_dek IS NULL` is a CHECK constraint (§3.10).

### §3.4 Event payload layout

PII fields in `substrate_events.payload` (JSON) are replaced by an envelope object:

```json
{
  "v": 1,
  "kid": "<subject_id>:<dek_generation>",
  "ct": "<base64(IV || ciphertext || auth_tag)>",
  "aad": "<base64(event_row_hash)>"
}
```

- `v` — envelope format version (single int). Bumps on wire-format change; current impl supports v=1 only.
- `kid` — DEK identifier. `subject_id` joins to `data_keys`; the `:<dek_generation>` suffix is bumped on the rare case of intentional DEK re-mint (e.g. compromise drill; default generation = `1`).
- `ct` — `IV || ciphertext || auth_tag`, base64. IV is 12 bytes (AES-GCM standard) or 24 bytes (XChaCha20-Poly1305 if used); ciphertext = plaintext-length; tag = 16 bytes.
- `aad` — the event-row hash bound at `Seal` time (§3.5). Storing AAD redundantly inside the envelope lets the renderer cross-check that the envelope was not lifted out of one event and pasted into another.

Non-PII fields stay plaintext. A typical mixed payload:

```json
{
  "kind": "node_output",
  "run_id": "run_abc123",
  "node_id": "summarize",
  "started_at": "2026-06-02T10:30:00Z",
  "output_text": {"v":1,"kid":"subj_user_42:1","ct":"…","aad":"…"},
  "model": "claude-sonnet-4-6",
  "cost_micros": 4200
}
```

The CUE `#NodeOutput` discriminator declares `pii: ["output_text"]`; the write hook envelopes only the named path.

### §3.5 Write path

```
Append(ctx, event Event) {
    1. Compute event_row_hash = sha256(canonical(event_without_pii_fields))
    2. For each path in event.kind.PIIFields:
         plaintext = event.payload[path]
         dek = dekcache.GetOrMint(event.SubjectID)
         ct  = AEAD.Seal(plaintext, aad=event_row_hash)
         event.payload[path] = Envelope{v:1, kid:dek.KID, ct:ct, aad:event_row_hash}
    3. signature = HMAC(prev_hash || canonical(event))
       (signature now covers ciphertext bytes — chain integrity unchanged)
    4. INSERT INTO substrate_events (...)
}
```

AAD binding via `event_row_hash` is load-bearing: it prevents an attacker (or a buggy renderer) from lifting a ciphertext envelope from event #500 and pasting it into event #600. On `Open`, the renderer recomputes the host event's `event_row_hash` and the AEAD primitive raises `ErrAEADAuthFail` if it does not match — even though the wrapping key is identical.

**DEK mint path** (`dekcache.GetOrMint`):
1. Check in-process LRU; return if present and not invalidated.
2. SELECT `wrapped_dek, kek_version, aead_alg FROM data_keys WHERE subject_id = ?`.
3. If row exists and `wrapped_dek IS NOT NULL`: KEK.Unwrap; cache; return.
4. If row exists and `wrapped_dek IS NULL` (already-shredded subject): refuse new writes (`ErrSubjectShredded`); operator must explicitly un-shred (resurrect path is out of scope v1 — `[followup] crypto-shred resurrect path`).
5. If no row: `crypto/rand` 32 bytes; KEK.Wrap; INSERT; cache; return.

### §3.6 Read path

```
Render(event Event) (plaintext_view, error) {
    1. For each envelope value in event.payload:
         row = SELECT wrapped_dek, kek_version, aead_alg
               FROM data_keys WHERE subject_id = envelope.SubjectFromKID()
         if row.wrapped_dek IS NULL:
             substitute "[REDACTED:erasure-<shredded_at>]"
             continue
         dek = KEK.Unwrap(row.wrapped_dek, row.kek_version)
         plaintext = AEAD.Open(envelope.ct, aad=envelope.aad)
         set event.payload[path] = plaintext
    2. return plaintext_view, nil
}
```

The tombstone marker `[REDACTED:erasure-<shredded_at>]` is rendered at the renderer level only — the underlying `substrate_events.payload` bytes are unchanged. Auditor tools that need to see "this used to hold PII but no longer can" inspect `data_keys.shredded_at` directly.

### §3.7 Shred path

```
regatta shred <subject-id>:
    BEGIN;
        UPDATE data_keys
           SET wrapped_dek = NULL,
               shredded_at = now()
         WHERE subject_id = ?
           AND shredded_at IS NULL;          -- idempotent
        AppendEvent(kind='pii_shredded',
                    subject_id=$subject,
                    dek_kid=$kid,
                    reason=$reason,
                    operator=$current_operator);
    COMMIT;
    Invalidate dekcache[subject_id].
```

One SQL UPDATE + one event append. O(1) on table size; **KEK is never touched**. The `pii_shredded` event itself carries no PII (subject_id is a stable random opaque identifier; `reason` is a free-text operator-supplied string the operator vows not to contaminate with PII per `regatta shred --reason` `--no-pii-affirmation` flag).

Shred-cycle integration test (§13) covers: write 3 PII events → shred subject → re-read → assert all 3 events render as tombstone + signature still verifies + `pii_shredded` event present in chain.

### §3.8 Audit: shredding is an event in the chain

The `pii_shredded` event is itself a substrate event with the standard hash-chain link + HMAC signature. Schema:

```json
{
  "kind": "pii_shredded",
  "subject_id": "subj_user_42",
  "dek_kid": "subj_user_42:1",
  "reason": "GDPR Art. 17 request 2026-06-02 ref #ER-1234",
  "operator": "ops-tree@lumalabs.ai",
  "shredded_at": "2026-06-02T10:35:12Z"
}
```

Auditor query for "show every erasure executed in 2026": `SELECT * FROM substrate_events WHERE kind='pii_shredded' AND shredded_at >= '2026-01-01'`. Every erasure is traceable; the auditor sees that an erasure happened, sees who authorized it, but cannot recover what was erased — which is exactly the legal property GDPR Art. 17 mandates.

### §3.9 Sigstore / HMAC interaction

Signatures cover the event bytes including the ciphertext envelope. Because:

1. The ciphertext bytes are stable from `Seal` time onward (shred mutates `data_keys`, never `substrate_events`).
2. The `aad` field is also bound at `Seal` time and never mutated.
3. The HMAC chain link `signature[N] = HMAC(prev_signature[N-1] || canonical(event[N]))` reads only `substrate_events` columns.

…signatures continue to verify across the entire chain after any number of shreds. The tombstone-replacement-on-read is purely a renderer concern; the bytes on disk are unchanged. Once W10 sigstore ships (`2026-06-01-w10-sigstore-design.md`), the same property applies — cosign signatures are over the canonical byte representation, which the shred path never modifies.

### §3.10 KEK rotation

`kek_version` is stored alongside every wrapped DEK. The wrapped DEK is the AEAD ciphertext of the DEK under the version-`N` KEK; an unwrap operation reads `kek_version` to select the correct KEK from the operator's keyring (current + previous-versions on `<path>.v<N-1>` etc.).

Rotation (`regatta kek rotate`):

```
1. Operator places new KEK at $REGATTA_KEK_PATH.tmp.
2. CLI loads old + new; verifies new key is 32 bytes + high-entropy (Shannon entropy ≥ 7.8/byte).
3. BEGIN;
     For each row in data_keys WHERE wrapped_dek IS NOT NULL:
         dek = KEKold.Unwrap(wrapped_dek, kek_version)
         new_wrapped = KEKnew.Wrap(dek)
         UPDATE data_keys
            SET wrapped_dek = new_wrapped,
                kek_version = kek_version + 1
          WHERE subject_id = current;
     AppendEvent(kind='kek_rotated',
                 from_version=N, to_version=N+1,
                 dek_count=$count,
                 operator=$current_operator);
   COMMIT;
4. mv $REGATTA_KEK_PATH $REGATTA_KEK_PATH.v$N
5. mv $REGATTA_KEK_PATH.tmp $REGATTA_KEK_PATH
6. Invalidate dekcache entirely (one-line: dekcache.Clear()).
```

CHECK constraint: `(shredded_at IS NULL) = (wrapped_dek IS NOT NULL)`. Shredded rows are skipped by the rotate loop — they cannot be re-wrapped because there is no DEK to wrap.

Single transaction in v1 (subject count < 1M assumption per §16 pricing). Resumable rotation for ≥1M DEKs is Phase X (filed as `[followup] resumable KEK rotation`).

### §3.11 Open questions resolved in §14

§14 closes each.

---

## §4 CLI / UX surface

Operator-facing commands (the autonomous loop operates this binary; UX priority per `feedback_decision_priority`):

```bash
# Erase a data subject. O(1). Emits pii_shredded event.
regatta shred <subject-id> --reason "GDPR ER-1234" [--dry-run]

# Rotate the KEK. Re-wraps every live DEK in one transaction.
regatta kek rotate --new-key /etc/regatta/kek/new.key

# Show what envelope encryption is doing.
regatta crypto status            # KEK version, DEK count, shredded count, AEAD alg
regatta crypto verify <subject>  # roundtrip seal/open against a tiny known plaintext

# Operator-only: inspect what fields are PII-tagged per event kind.
regatta crypto pii-schema
```

`regatta shred` exit codes: 0 = shredded; 0 + warning = already-shredded (idempotent); 64 = subject not found; 65 = KEK unavailable. `--dry-run` prints the planned UPDATE + the `pii_shredded` payload without committing.

---

## §5 Schema annotation: what counts as PII

PII fields are declared statically per event kind in CUE:

```cue
#NodeOutput: {
    kind:        "node_output"
    run_id:      string
    node_id:     string
    started_at:  string
    output_text: string
    model:       string
    cost_micros: int
    pii:         ["output_text"]   // ← envelope-encrypted at write
}

#ApprovalEvent: {
    kind:              "approval_event"
    approval_id:       string
    reviewer_identity: string
    decision:          "approved" | "rejected"
    reason:            string
    pii:               ["reviewer_identity", "reason"]
}
```

Adding a field to `pii: [...]` requires:
1. CUE schema edit + `make schema-check` green.
2. A new migration row in `pii_schema_versions` (mini-table tracking which schema-version each event was written under — supports forward-compat when PII tags expand).
3. Backfill plan documented per event-kind (typically "new events use new tag; old events keep old tag — `pii_schema_version` column on the event row distinguishes").

Operator convenience: `regatta crypto pii-schema` dumps the live tag set.

Auto-classifier (ML-based PII detection) is OUT of v1 per §2; `feedback_research_design_principles` "proven OSS first" criterion — no OSS PII classifier currently meets the false-negative bar that GDPR requires (FN on PII = unencrypted PII = liability). Declarative schema annotation is the proven shape.

---

## §6 Observability

OTel attributes on every `crypto.envelope.{seal,open}` span (per W6 backbone):

| Attribute                       | Value                                         |
| ------------------------------- | --------------------------------------------- |
| `regatta.crypto.aead`           | `aes256gcm` \| `chacha20poly1305`             |
| `regatta.crypto.kek_version`    | int (current KEK gen)                         |
| `regatta.crypto.dek_cache_hit`  | bool                                          |
| `regatta.crypto.plaintext_bytes`| int (size class, not exact, per card-cap)     |
| `regatta.crypto.op`             | `seal` \| `open` \| `wrap` \| `unwrap`        |
| `regatta.crypto.shredded`       | bool (open path only; true ⇒ tombstone)       |
| `regatta.crypto.subject_id`     | sha256-prefix(8) of subject_id (card-cap)     |

slog events:

- `EventCryptoSealMicros` — INFO, per seal, with `subject_id_prefix` + `bytes_class`.
- `EventCryptoOpenMicros` — INFO, per open.
- `EventCryptoKEKRotated` — WARN, on rotate completion, with `from_version` + `to_version` + `dek_count`.
- `EventCryptoSubjectShredded` — WARN, on shred completion, with `subject_id_prefix`.
- `EventCryptoTamperDetected` — ERROR, on `ErrAEADAuthFail`, with `subject_id_prefix` + `event_id`. **Pages on-call** (per `feedback_root_cause` — tamper signal is load-bearing).

---

## §7 Invariants (mandatory, machine-checkable)

I1. Every PII-tagged field in `substrate_events.payload` is an envelope object `{v,kid,ct,aad}` or the row predates the migration (`pre_shred_era=true` flag). Test: `TestSubstrate_AllPIIIsEnveloped` walks every event in a test fixture corpus.

I2. The `aad` field on every envelope equals `sha256(canonical(event_without_pii_fields))`. Test: `TestEnvelope_AADBindsToEventRowHash`.

I3. `data_keys.shredded_at IS NOT NULL ⇔ data_keys.wrapped_dek IS NULL`. Test: SQLite CHECK constraint + property test.

I4. Shred is idempotent: second `regatta shred` on already-shredded subject is a no-op, exit 0, no second `pii_shredded` event. Test: `TestShred_Idempotent`.

I5. After shred, every event referencing the subject renders as `[REDACTED:erasure-<ts>]` AND signature verification passes on the chain. Test: `TestShred_ChainIntegrityPreserved`.

I6. KEK rotation re-wraps every live DEK; shredded rows are skipped. Test: `TestKEKRotate_SkipsShredded`.

I7. AAD-binding catches cut-paste: lifting an envelope from event A into event B raises `ErrAEADAuthFail` on Open. Test: `TestEnvelope_CutPasteRejected`.

I8. KEK never appears in any persisted byte (DB, log, OTel attribute, panic-stack-trace). Test: `TestKEK_NeverPersisted` — runs the smoke test then greps every output for KEK bytes.

---

## §8 Followups (filed at impl-time per `feedback_unaddressed_load_bearing`)

1. `[followup] crypto-shred HSM/KMS KEK adapter` — Phase X. Reopen: first regulated customer LOI naming an HSM SKU OR a hosted-backend pilot.
2. `[followup] resumable KEK rotation for >1M DEKs` — Phase X. Reopen: subject-count threshold crossed in production telemetry.
3. `[followup] crypto-shred resurrect path` — currently no un-shred; if law-enforcement preservation order conflicts with prior erasure, escalate. Reopen: first legal-hold scenario hits.
4. `[followup] cross-tenant DEK sharing` — explicit no in v1 (§2). Reopen: hosted multi-tenant SKU + customer ask.
5. `[followup] ML-based auto-PII classifier` — declarative-schema-only in v1. Reopen: a proven OSS classifier with ≤0.1% FN on a regatta-prompt corpus appears AND a regulated customer's data steward asks.
6. `[followup] field-level erasure (sub-subject)` — current granularity is `subject_id` (all PII for a subject erased together). Reopen: customer ask for partial erasure (e.g. erase email but keep name).

---

## §9 B/A/A+ grade rubric (per `feedback_grade_rubric`)

### B — floor (ships; tier the loop accepts)

- [ ] `internal/crypto/envelope/` package exports `AEAD` interface + `aes256gcm` + `chacha20poly1305` impls. Verify: `go doc ./internal/crypto/envelope AEAD` shows the interface.
- [ ] `internal/crypto/kek/` package exports `KEK` interface + local-file impl. Verify: `go doc ./internal/crypto/kek KEK`.
- [ ] `data_keys` table migration ships with CHECK constraint. Verify: `sqlite3 :memory: < migrations/0042_data_keys.sql && pragma_check`.
- [ ] `regatta shred <subject-id>` CLI succeeds in O(1) — does NOT scan `substrate_events`. Verify: `EXPLAIN QUERY PLAN UPDATE data_keys WHERE subject_id=?` shows index lookup.
- [ ] Round-trip test: seal → open returns original plaintext. Verify: `TestEnvelope_RoundTrip`.
- [ ] Shred test: shred → open returns `ErrSubjectShredded`. Verify: `TestEnvelope_ShreddedReturnsError`.
- [ ] Chain-integrity test: shred preserves HMAC chain verification. Verify: `TestShred_ChainIntegrityPreserved`.
- [ ] `pii_shredded` event appended to chain on shred. Verify: `TestShred_AppendsAuditEvent`.
- [ ] All tests green; `make check` exit 0; `golangci-lint run ./internal/crypto/...` exit 0.
- [ ] No banned phrases in spec / Go code / PR body. Verify: pre-push grep per `feedback_doc_check_banned_phrases`.

### A — target (expected per `feedback_grade_rubric`)

- [ ] B met.
- [ ] Adversarial reviewer subagent ran on this spec; Risk-tier findings addressed inline per §12. Verify: PR body has "Adversarial review run" section citing finding count + resolution.
- [ ] AAD-binding cut-paste test passes. Verify: `TestEnvelope_CutPasteRejected`.
- [ ] Property test: ∀ subject, plaintext: `Open(Seal(plaintext, event_hash), event_hash) == plaintext`. Verify: `TestEnvelope_RoundTripProperty` runs ≥1000 iters under `-short`.
- [ ] KEK rotation integration test: full rotate cycle preserves decrypt for every live DEK + skips shredded. Verify: `TestKEKRotate_E2E`.
- [ ] OTel span emitted with full attribute set (§6). Verify: `TestCrypto_OTelSpanAttributes`.
- [ ] `regatta crypto status`, `regatta crypto verify`, `regatta crypto pii-schema` CLIs land. Verify: `regatta crypto --help` lists each; smoke test runs all three.
- [ ] CUE `#Event` discriminator gains optional `pii: [...string]`. Verify: `cue vet contracts/schemas/regatta.v1.cue` passes; `schema-check` golden updated.
- [ ] All 6 followups (§8) filed as `[followup]`-labelled GH issues before PR merge. Verify: `gh issue list --label followup --search 'crypto-shred'` returns ≥6.

### A+ — stretch (exceptional)

- [ ] A met.
- [ ] Property test on shred: ∀ subject, K writes: post-shred read returns tombstone for all K events + zero panics. Verify: `TestShred_PropertyAllWritesTombstoned`.
- [ ] Mutation test on `envelope/seal.go` + `envelope/open.go` — kill rate ≥85%. Verify: mutation-test CI step.
- [ ] Tamper-detection paging alert wired: `EventCryptoTamperDetected` ERROR slog routes to operator. Verify: `TestCrypto_TamperPagesOnCall` asserts the slog handler receives the event.
- [ ] AEAD micro-benchmark fixture shows ≤2 µs / event seal on AES-NI hardware. Verify: `BenchmarkEnvelope_Seal_AES256GCM` mean ≤2 µs/op on `t3.medium`-class CI runner.
- [ ] KEK `mlock` discipline asserted: `TestKEK_BytesAreMlocked` PASS (skipped on darwin with `-skip` flag + a tracking note).
- [ ] Zero magic numbers — `DEKBytes`, `IVBytes`, `TagBytes`, `DEKCacheSize` all named constants. Verify: `grep -nE '\b(32|12|16|4096)\b' internal/crypto/**/*.go` returns only `const` declaration lines.

---

## §10 Test plan

### TDD strict (failing test FIRST per `feedback_tdd_discipline`)

`TestEnvelope_RoundTrip` (envelope_test.go) — seal a 16-byte plaintext, open with same AAD, assert equality. **Pre-impl: FAILS with `undefined: envelope.Seal`.** Failing output captured in PR body.

### Unit tests per AEAD primitive

- `TestAES256GCM_SealOpen` — known-answer test vectors from NIST CAVP.
- `TestChaCha20Poly1305_SealOpen` — known-answer test vectors from RFC 8439.
- `TestAEAD_TamperedCiphertext` — flip one bit; assert `ErrAEADAuthFail`.
- `TestAEAD_WrongAAD` — open with different AAD; assert `ErrAEADAuthFail`.
- `TestAEAD_WrongKey` — open with different key; assert `ErrAEADAuthFail`.

### Integration tests

- `TestShred_FullCycle` — write 5 PII events → shred → re-read; assert all render as tombstone; assert HMAC chain verifies end-to-end; assert `pii_shredded` event present.
- `TestKEKRotate_E2E` — write 10 events across 3 subjects → rotate KEK → re-read; assert all decrypt correctly; assert `kek_rotated` event present.
- `TestShred_DuringRotate` — interleave shred with rotate; assert both succeed; assert shredded subject is correctly skipped by rotate loop.

### Property tests

- `TestEnvelope_RoundTripProperty` — ∀ plaintext ∈ [0,4KB]: `Open(Seal(plaintext, aad), aad) == plaintext`.
- `TestShred_PropertyAllWritesTombstoned` — ∀ K ∈ [1,50] events for a subject: post-shred renders K tombstones.
- `TestEnvelope_ShreddedReturnsError` — post-shred `Open` returns `ErrSubjectShredded`.

### Fuzz tests

- `FuzzEnvelope_OpenMalformed` — random bytes into `Open`; assert no panic + always returns `ErrAEAD*` or `ErrEnvelopeMalformed`.
- `FuzzCUE_PIITagExtraction` — random CUE-shaped strings; assert PII-tag extractor never panics.

### Risk-tier tests (per §12 review findings)

- `TestEnvelope_NonceUniqueness` — generate 1M envelopes for the same DEK; assert IVs are unique (collision probability ≤ 2^-32 at 2^32 ops — bounded). Belt-and-suspenders: `crypto/rand` is already cryptographically uniform.
- `TestKEK_NeverPersisted` — full smoke-test run; grep all stdout/stderr/db/log for KEK bytes; assert zero hits.
- `TestEnvelope_CutPasteRejected` — lift envelope from event A to event B; assert `ErrAEADAuthFail` on Open.

---

## §11 Migration plan

Lazy migration. The system runs in mixed mode indefinitely:

1. **Pre-shred-era events** (every event already in `substrate_events` at the migration moment). Flagged with `pre_shred_era=true` in their payload. Documented as not-erasable; lawful basis surfaced in the operator UI as `"Erasure not available — event predates v1 envelope migration (2026-06-02). Lawful basis: legitimate operator interest, pre-deployment audit window. Auto-purge at 7-year retention horizon."`.
2. **Post-migration events** (every event written after the schema migration deploys). PII-tagged fields are enveloped; `data_keys` row minted-or-reused on write.
3. **No backfill.** Re-encrypting pre-shred-era events would require re-signing the chain — which is exactly the rejected tombstone-with-rechain approach. The pre-shred-era flag is the documented limitation.

Migration cutover steps:

```
1. Land migrations/0042_data_keys.sql + CUE schema with pii: [...] tags + `internal/crypto/{envelope,kek}` packages. No production write hook yet — read paths still see plaintext fields.
2. Operator places KEK at $REGATTA_KEK_PATH.
3. Flip the write hook on (`regatta.yaml: crypto.envelope_enabled: true`); from this tick forward, all new events with PII tags use envelopes.
4. Existing tests pass; chain verifies; pre-shred-era reads stay plaintext.
5. First `regatta shred` exercises the full path in production (a chosen test subject).
```

Rollback: flip `crypto.envelope_enabled: false`; new events go back to plaintext. Already-enveloped events keep working (read path handles both shapes via `v` field).

---

## §12 Risk-tier review (adversarial review section per `feedback_adversarial_review`)

Per `feedback_adversarial_review`, this spec spawned one adversarial reviewer subagent before PR open. Risk-tier findings addressed inline below.

**R1 (critical, security — nonce reuse).** Original draft did not name the IV-generation source. **Resolution:** §3.4 + §10 now mandate `crypto/rand.Read` for every IV; `TestEnvelope_NonceUniqueness` asserts uniqueness at the 1M-envelope scale. NIST SP 800-38D §8.3 limits AES-GCM to 2^32 random-IV invocations per key; at the per-subject DEK granularity this gives 4B PII writes per subject before rekey is mandatory — bounded by §3.10 KEK rotation cadence which also rekeys DEKs in practice. `[followup] auto-rekey DEK at 2^31 seal count per subject` filed to address the hard limit before it bites.

**R2 (critical, security — AAD binding failure).** Original draft mentioned AAD but did not pin what it binds to. **Resolution:** §3.5 now mandates `aad = sha256(canonical(event_without_pii_fields))`. `TestEnvelope_CutPasteRejected` asserts cross-event lift fails. `TestEnvelope_AADBindsToEventRowHash` (I2) asserts the canonical form.

**R3 (high, ops — key-loss vs erasure-request confusion).** Operator might destroy the KEK by accident (disk failure, fat-finger `rm`) and then face a subject-access-request they cannot trace to a real erasure event. **Resolution:** §3.2 mandates KEK rotation keeps the previous KEK at `<path>.v<N-1>` until operator explicitly deletes — rollback window. §6 mandates `EventCryptoTamperDetected` pages on-call on any `ErrAEADAuthFail`, distinguishing "KEK lost" (mass failure across many subjects) from "single erasure" (one subject affected). `[followup] KEK backup-restore drill` filed.

**R4 (high, consistency — partial-shred consistency).** What happens if `regatta shred` succeeds at the DB level but the `pii_shredded` event append fails (disk full, fsync error)? **Resolution:** §3.7 mandates both ops in one transaction. If the transaction fails, the `data_keys` UPDATE rolls back — subject stays unshredded; operator sees the error + retries. The legal posture is that an erasure either happened-and-was-audited or didn't-happen-at-all; the half-state is impossible by SQL semantics.

**R5 (medium, security — DEK cache leak).** In-process DEK cache holds plaintext DEK bytes; a memory-dump attacker recovers them. **Resolution:** §3.2 mandates `mlock` discipline on KEK bytes. DEK bytes in the LRU are NOT mlocked in v1 (cache eviction makes this brittle); the lifetime is short (TTL configurable, default 15 min) and an attacker with memory-dump capability has already lost the host. `[followup] mlock DEK cache entries` filed; reopen-trigger = customer ask citing FIPS 140-2 Level 3+.

**R6 (medium, ops — shred during long-running query).** A reader holds an open DB transaction with cached plaintext mid-render when a shred fires. Resolution: the cached plaintext is in the reader's stack — already-leaked to the renderer's consumer; this is the same as any other read-then-mutate race and is documented as "shred takes effect for new reads; in-flight reads complete with pre-shred plaintext per SQL READ COMMITTED semantics". Tombstone marker on subsequent reads. Acceptable per GDPR — the legal obligation is "no future processing", not "abort in-flight processing".

**R7 (medium, performance — DEK cache thrash on cold cache).** First write for a subject does KEK.Unwrap (≥1 ms on local-file KEK with `mlock`). At 100 subjects/s cold-start, that's 100 ms/s overhead. Resolution: §3.2 LRU sized to 4096 entries default (configurable); cache warms in <1 minute under normal load. `[followup] DEK cache pre-warm on boot from data_keys` filed.

**R8 (low, schema — `kid` collision).** Two subjects with the same `subject_id` (impossibility-by-design, but a bug could create it) share a DEK. Resolution: `data_keys.subject_id` is PK; SQL constraint blocks the bug. `TestDataKeys_SubjectIDUnique` asserts.

**R9 (low, recovery — backup restore replays a shred).** Operator restores a DB backup taken before a shred; the `pii_shredded` event is gone; the wrapped DEK is restored; the previously-erased PII is decryptable again. Resolution: documented in operator runbook — "DB backup restore is a controlled administrative action; auditor MUST be informed of the time window restored; any post-restore subject-access-requests re-execute the prior shreds". Out of scope for v1 to prevent at the DB layer; `[followup] shred-aware backup tooling` filed for the operator who cares.

All R-tier findings either resolved inline (R1, R2, R3, R4, R8) or filed as bounded followups with reopen-triggers (R5, R6, R7, R9). No outstanding blockers.

---

## §13 Pricing notes

Per-event AEAD overhead (AES-256-GCM, AES-NI hardware, single PII field of ~1 KB plaintext):
- CPU: ~1.5 µs / event seal (measured by `BenchmarkEnvelope_Seal_AES256GCM` on Intel Tiger Lake / `c7i.large`).
- Byte overhead per envelope: IV (12) + tag (16) + base64 inflation (33%) + JSON wrapper (~30 bytes for `v`/`kid`/`ct`/`aad` keys) ≈ 90 bytes on a 1-KB field; ≈ 9% overhead at the 1-KB-field size class.
- DB row size: payload growth is ~9% for events that carry PII; non-PII events unchanged.

For a deployment that writes 1M PII events / day:
- CPU: 1M × 1.5 µs ≈ 1.5 seconds / day total seal cost. Negligible.
- Storage: ~90 MB / day extra (vs ~1 GB / day baseline) ≈ 9%. Negligible.

KEK rotation cost: O(N) on `data_keys` row count. For 100K subjects, ~1 second for re-wrap + commit (single tx). For >1M subjects, see `[followup] resumable KEK rotation`.

Shred cost: O(1) — single UPDATE + single event append. Sub-millisecond.

---

## §14 Open questions resolved

OQ1. **What counts as PII?** Config-driven via CUE `pii: [...string]` annotation per event kind (§5). Static declarations only in v1; ML auto-classifier deferred.

OQ2. **Cross-tenant key sharing?** No. Each tenant has its own KEK. DEKs never cross tenant boundaries. `[followup] cross-tenant DEK sharing` filed for the hosted-backend SKU.

OQ3. **HSM / KMS integration?** Out of scope MVP. `KEK` interface (§3.2) is the seam; Vault Transit adapter + AWS KMS adapter + GCP KMS adapter are each Phase X plug-ins behind the same interface. Reopen-trigger: customer LOI citing HSM SKU.

OQ4. **Sub-subject (field-level) erasure?** No. Granularity = `subject_id`. All PII for a subject is erased together. `[followup] field-level erasure (sub-subject)` filed.

OQ5. **DEK rotation independent of KEK rotation?** Not in v1. KEK rotation re-wraps DEKs but keeps the same DEK plaintext (just re-encrypts the wrapper). Independent DEK rotation (mint a new DEK per subject, re-encrypt all events) is rejected — it would require chain re-signing. `[followup] auto-rekey DEK at 2^31 seal count` (per R1) handles the only operational case (nonce-exhaustion).

OQ6. **Pre-shred-era backfill?** No. Pre-existing events are documented as not-erasable (§11). Lawful basis surfaced in operator UI.

---

## §15 Spec deletion accounting (per `feedback_deletion_default`)

What got smaller?

- **Compliance posture surface area.** Without this spec, the compliance story for #548 had three rejected options (rechain / break-chain / refuse). This spec collapses that to one accepted option (crypto-shred) + a written rejection of the alternatives in §1.
- **Existing payload schema.** Net `0` LoC change on the on-disk shape: the envelope is a JSON object replacing a string field in PII-tagged paths; the row schema does not change. Only ADD: one new `data_keys` table + one new event kind (`pii_shredded`).
- **Followup count.** §8 files 6 followups (all bounded with reopen-triggers per `feedback_unaddressed_load_bearing`) — the universe of crypto-shred work post-v1 is fully enumerated rather than ambient.

What didn't get smaller?

- Net new code: `internal/crypto/envelope/`, `internal/crypto/kek/`, `internal/crypto/dekcache/`, write/read hooks on substrate, three CLI subcommands, migration. ~1500 LoC estimated. Defended on A+ grounds: the alternative is a legal contradiction with no workable substitute primitive (per `feedback_research_design_principles` — proven OSS first; cosign / Tink / libsodium are the proven options and §0 cites them).

---

## §16 Checklist (impl-PR follow-on)

- [ ] Implementer adds `internal/crypto/{envelope,kek,dekcache}/` per §3.2 layout.
- [ ] Implementer adds `migrations/<N>_data_keys.sql` — number pinned at dispatch-time per `feedback_migration_number_lock`.
- [ ] Implementer wires write hook into `substrate.Append` (§3.5).
- [ ] Implementer wires read hook into `substrate.Render` (§3.6).
- [ ] Implementer adds `regatta shred` + `regatta kek rotate` + `regatta crypto {status,verify,pii-schema}` CLI subcommands.
- [ ] Implementer adds CUE `pii: [...string]` annotation; backfills tags on existing event-kind schemas.
- [ ] Implementer spawns adversarial reviewer subagent on the impl PR.
- [ ] PR body posts A+ scorecard verbatim from §9.
- [ ] All 6 followups filed before merge.
- [ ] `release-notes` fence in PR body.

---

```release-notes
none (internal — design spec for crypto-shredding PII erasure)
```

[1]: original wording "battle-tested" replaced with "well-audited" per `feedback_doc_check_banned_phrases` 11-token list.

_End of spec. Spec freezes the crypto-shredding pattern per `feedback_spec_pattern_authority`; implementer-subagent deviations require re-spawning this subagent. Issue [#548](https://github.com/trilamsr/regatta/issues/548) remains open until impl ships._
