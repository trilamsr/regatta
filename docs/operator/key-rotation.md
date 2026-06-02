# HMAC key rotation — operator runbook

Reader: customer-operator rotating the HMAC key that signs substrate
events.
Read time: 8 minutes.
Goal: rotate keys without losing pre-rotation events; recover when a
key is lost.
Expires when: the `regatta keys` subcommand surface in
`cmd/regatta/keys.go` changes, or the substrate sign chain in
`internal/orchestrator/state/substrate/sign.go` adopts a non-HMAC
algorithm.

## What you get

- **Multi-key keyring.** `REGATTA_HMAC_KEYRING="k1:hex,k2:hex"` keeps
  every listed key valid for verify; the last entry signs new writes.
- **`regatta keys` CLI.** Four subcommands cover the drill end-to-end:
  `list`, `rotate`, `retire`, `recover`. Each is an explicit operator
  action — no signal handlers, no implicit re-reads, every rotation
  shows up in shell history.
- **Append-only audit.** Substrate `event_log` is the rotation audit
  trail. The `recover` subcommand is the only mutation surface and
  lives in a sibling package (`substraterecovery`) so a grep for
  `UPDATE substrate_events` returns one hit, in one file.

## When to rotate

Three triggers, three paths:

| Trigger | Path | Section |
|---|---|---|
| Scheduled rotation (calendar — quarterly is typical) | rotate → resign → retire | §1 |
| Active key compromised (signing key leaked) | rotate → recover → retire | §2 |
| Retired key still needed (operator lost the new material) | recover with `--extra-key` | §3 |

## §1 — Scheduled rotation

You hold the existing key `k1` in your secret store, want to roll
forward to `k2`, and have no urgency (no compromise).

### Step 1: generate the new key locally

```sh
NEW_KEY_HEX=$(openssl rand -hex 32)
export REGATTA_HMAC_KEY_FRESH="$NEW_KEY_HEX"
```

The key never leaves your shell until step 4 writes it back to your
secret store.

### Step 2: validate the new material against the live keyring

```sh
regatta keys rotate \
  --new-key-env=REGATTA_HMAC_KEY_FRESH \
  --new-key-id=k2
```

Exit 0 prints the merged keyring as a `REGATTA_HMAC_KEYRING=` export
line. Exit 1 means weak hex, duplicate keyID, or empty env — fix the
input and retry.

The CLI does NOT mutate environment or restart `regatta serve`. Both
are out-of-band operator actions and intentionally outside the CLI's
audit scope.

### Step 3: list the live keyring

```sh
regatta keys list
```

Shows every configured keyID with `(active)` on the write key. Before
restart this still shows `k1 (active)`. After restart with the new
env, `k2 (active)`.

### Step 4: write to secret store + restart

Push the merged `REGATTA_HMAC_KEYRING` to your secret store (Kubernetes
`Secret`, Vault, AWS Secrets Manager — operator's choice). Bounce
`regatta serve`. New writes sign under `k2`; reads accept either key.

### Step 5: re-sign the old rows

```sh
export REGATTA_OLD_KEY_HEX=<the k1 hex you held in secret store>
regatta keys recover \
  --extra-key=k1:REGATTA_OLD_KEY_HEX \
  --dry-run
```

Dry-run reports the row count without mutating. If the count matches
your expectations (every event since the last rotation), drop
`--dry-run` to perform the sweep. Recover wraps every UPDATE in a
single transaction — a partial sweep cannot leave the substrate in a
mixed-key state.

### Step 6: retire the old key

```sh
regatta keys retire --key-id=k1
```

The pre-flight scans `substrate_events.sig_key_id` and refuses if any
row still verifies under `k1`. After a clean recover the count is
zero, retire prints `safe to retire`, and the next step is yours:
remove `k1:hex` from `REGATTA_HMAC_KEYRING` in your secret store and
restart `regatta serve` once more.

## §2 — Active key compromised

The active key has leaked (insider exfil, accidental commit, hostile
log scrape). The threat is forged events under the leaked key. Speed
matters but correctness matters more.

1. Rotate immediately (§1 steps 1-4) to a fresh `k3`. New writes now
   sign under `k3`; the compromised `k2` is no longer the write key
   but is still in the keyring.
2. Audit `substrate_events` for any row signed by `k2` written AFTER
   the compromise timestamp:

   ```sh
   sqlite3 regatta.db \
     "SELECT id, run_id, written_at FROM substrate_events \
      WHERE sig_key_id='k2' AND written_at > <compromise_ts_ms>;"
   ```

   These are the rows an attacker could have forged. Triage per your
   incident-response policy.
3. Re-sign the legitimate `k2` rows under `k3`:

   ```sh
   regatta keys recover --extra-key=k2:REGATTA_K2_HEX
   ```

4. Retire `k2`:

   ```sh
   regatta keys retire --key-id=k2
   ```

5. Delete the compromised `k2` material from every secret store
   replica. The `event_log` retains the rotation audit but the key
   material is gone.

## §3 — Recover from a lost retired key

You held `k_old`, rotated to `k_new`, retired `k_old` from the
keyring, then later realized you never re-signed the old rows. The
operator who held `k_old` no longer has access (left the team, lost
the laptop, etc.) but a backup of the secret store has the hex.

1. Restore the `k_old` hex into an env var: `export
   REGATTA_K_OLD_BACKUP=<hex>`.
2. Re-add `k_old` to the recover supplied via `--extra-key`. Do NOT
   add it back to `REGATTA_HMAC_KEYRING` — keep the live keyring lean.

   ```sh
   regatta keys recover --extra-key=k_old:REGATTA_K_OLD_BACKUP
   ```

3. After recover, retire passes:

   ```sh
   regatta keys retire --key-id=k_old
   ```

If you no longer have any backup of `k_old`, the `k_old`-signed rows
are unverifiable. Recover refuses the sweep (no silent rewrite of
tampered rows). Options:

- Restore the substrate DB from a snapshot taken before the rotation.
- Accept the data loss for the affected runs. The
  `lint-substrate-queries` test pins that no read path silently skips
  unverifiable rows — they surface as `ErrUnverifiable` at every
  consumer.

## What the CLI will NOT do

- **Mutate environment variables.** `rotate` prints the next-step
  export line; the operator's secret store + restart loop is the
  actuator.
- **Restart `regatta serve`.** Out of scope; deploy systems are
  operator-managed.
- **Skip verify on unverifiable rows.** `recover` refuses to rewrite a
  row that does not verify under the supplied keyring. There is no
  `--force` flag and no env-var bypass.
- **Delete substrate rows.** The substrate is append-only; the only
  mutation surface is the sig triple (sig_alg, sig_key_id, sig_mac).

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `regatta keys rotate: weak key` | hex decodes to <32 bytes | regenerate with `openssl rand -hex 32` |
| `regatta keys rotate: duplicate keyID` | new keyID matches an existing entry | pick a fresh keyID (`k3` not `k2`) |
| `regatta keys retire: pre-flight FAILED — N row(s) still signed` | rows not yet re-signed | run `regatta keys recover --extra-key=…` first |
| `regatta keys recover: N row(s) unverifiable — missing --extra-key` | recover saw a foreign sig_key_id with no key material | supply `--extra-key=<id>:ENVNAME` for every retired key |
| `regatta keys recover: nothing to do` | every row already verifies under live keyring | rotation drill is already complete |

## Cross-references

- CLI source: [`cmd/regatta/keys.go`](../../cmd/regatta/keys.go)
- Recovery package: [`internal/orchestrator/state/substraterecovery/`](../../internal/orchestrator/state/substraterecovery/)
- Substrate sign chain: [`internal/orchestrator/state/substrate/sign.go`](../../internal/orchestrator/state/substrate/sign.go)
- Append-only invariant test: `TestSubstrate_NoUpdateDeleteInSubstratePackage`
