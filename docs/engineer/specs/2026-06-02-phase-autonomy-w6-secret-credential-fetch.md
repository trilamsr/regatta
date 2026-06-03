# PHASE-AUTONOMY W6 — secret-credential autonomic fetch (spec)

Status: locked design (next-wave; ships in Landing 2 alongside W3).
Item: `.regatta/items/phase-autonomy-w6-secret-credential-autonomic-fetch.md`.
Source brief: `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W6.
Memory cites: `feedback_decision_priority`, `feedback_research_design_principles`, `feedback_deletion_default`, `feedback_grade_rubric`, `feedback_adversarial_review`, `feedback_root_cause`, `feedback_spec_pattern_authority`, `feedback_pr_body_hygiene`, `feedback_no_signatures`.

## 1. Problem

The operator boots `regatta serve` unattended under systemd (Linux) and launchd (macOS). Today each wake forces a manual `export ANTHROPIC_API_KEY=… GH_TOKEN=… REGATTA_BRIEF_HMAC_KEYS=…` step — the laptop-closes-for-a-weekend scenario is blocked on a human typing tokens. W6 lifts that step out of the operator's hands by sourcing those secrets from an OS-resident store at supervisor boot and exporting them to the regatta process tree, with env-var fallback so containerized + CI runs still work.

The item (`PHASE-AUTONOMY-W6`) and the brief §11 W6 both name `pass` (GPL-2) + gpg-agent as the adopted Linux store, with env-var fallback. This spec extends that to a generic `Fetcher` interface so macOS Keychain (via `go-keyring`) sits behind the same call site as `pass`, and a future Vault/AWS-SecretsManager adapter (Phase X — external customer or hosted-backend ask) drops in without re-architecting. The brief explicitly rejects Vault for persona-A; this spec keeps that rejection and labels Vault as Phase X only.

GPL-2 container-compliance: `pass` is shelled out as an unmodified upstream binary (no static/dynamic linking, no source modification), which keeps regatta itself outside GPL-2's copyleft scope. For container images, the operator-built image either (a) installs `pass` via the base distro package manager — the distro carries the LICENSE/source-availability obligation, or (b) omits `pass` and relies on env fallback. The regatta image we publish ships **without** `pass` preinstalled; a tracking issue documents the Phase-X "regatta-with-pass" image variant with full GPL-2 NOTICE + source-offer text.

## 2. Scope

In scope (Landing 2, ~150 LoC supervisor + adapters):

- Read four canonical keys at supervisor boot — `regatta.anthropic_api_key`, `regatta.gh_token`, `regatta.brief_hmac_keys` (multi-line, base64-encoded HMAC keyring), `regatta.audit_hmac_key`.
- Adapter chain: macOS Keychain → `pass` (Linux) → env-var fallback. First non-error hit wins; missing-key never panics — degrades to "feature unavailable" with a structured log + substrate event.
- CLI: `regatta secret set|get|list|status` for operator bootstrap, audit, and rotation. `get` refuses to print to stdout without `--unsafe`.
- SIGHUP triggers a one-shot re-read of the chain into the in-process cache.
- `regatta install-service` (from W3) calls `regatta secret set` interactively for every missing key.

Out of scope (Phase X, with explicit reopen-trigger):

- Vault / AWS Secrets Manager / GCP Secret Manager adapter — reopen on first hosted-backend customer ask.
- Linux `libsecret` (gnome-keyring) / KWallet adapters — `pass` covers the same persona; reopen on first operator report of headless-server gpg-agent friction.
- Windows Credential Manager — Phase X; persona-A is macOS + Linux only.
- File-watcher live reload — SIGHUP only in Landing 2 (rotations are operator-initiated). File-watcher reopens on first observed pain.

## 3. Architecture

New package: `internal/secrets/`.

```
internal/secrets/
  secrets.go         // Fetcher interface + Default() composite constructor
  keychain_darwin.go // macOS Keychain via go-keyring (build tag)
  keychain_other.go  // stub: returns ErrUnsupported (build tag !darwin)
  pass_linux.go      // shells out to `pass show <key>` (build tag linux)
  pass_other.go      // stub: returns ErrUnsupported (build tag !linux)
  env.go             // env-var fallback (cross-platform)
  composite.go       // Chain of Fetchers; first non-ErrNotFound wins
  cache.go           // in-process cache; SIGHUP-driven refresh
  cli.go             // `regatta secret …` subcommand implementation
```

Adapter count: **3 active adapters** (macOS Keychain, pass, env) + **2 stub adapters** (keychain_other, pass_other) for cross-compile cleanliness. Phase-X adapter slot count = 3 (Vault, AWS SM, GCP SM) — interface stable.

```go
// Fetcher reads a secret by canonical key; absent secret returns ErrNotFound,
// not an empty string, so chains can distinguish missing-key from empty-value.
type Fetcher interface {
    Get(ctx context.Context, key string) (string, error)
    Name() string // for diagnostics: "keychain", "pass", "env"
}

// Default returns the platform-correct composite chain.
// macOS: keychain -> env. Linux: pass -> env. Other: env.
func Default(ctx context.Context) Fetcher { … }

var ErrNotFound = errors.New("secret not found")
```

`Fetcher` is **read-only on the hot path**. Writes happen only via the `regatta secret set` CLI, which calls platform-specific `setter` helpers (`security add-generic-password` on macOS; `pass insert -e` on Linux). The hot-path interface stays narrow.

OSS adopted:

- `github.com/zalando/go-keyring` v0.2.5 (commit `923f7c4`, MIT) — cross-platform thin shim over macOS Security framework + libsecret. We pin the macOS path only; Linux path stays on `pass` per the brief. macOS-version compatibility validated by integration test `TestKeychain_Darwin_RoundTrip` on the targeted runner; no upstream-tracker compat claim is made here.
- `pass` (GPL-2, https://www.passwordstore.org/) — adopted via subprocess. No CGo dependency, no Go-binding lock-in.
- `gopasspw/gopass` (MIT) — Phase-X alt; tested but not adopted in Landing 2 to keep LoC + dep surface minimal. Trigger: operator reports `pass` subprocess latency >100 ms in boot path.

### `Value` redaction type

```go
// Value wraps a secret byte slice. The struct has ZERO exported fields —
// reflection-based formatters (fmt %v, %+v, encoding/json, slog) cannot
// reach the underlying bytes. The only legitimate access is via Bytes(),
// which is grep-gated to a small allowlist of call sites.
type Value struct {
    b []byte
}

func (v Value) String() string                  { return "<redacted>" }
func (v Value) GoString() string                { return "<redacted>" }
func (v Value) MarshalJSON() ([]byte, error)    { return []byte(`"<redacted>"`), nil }
func (v Value) MarshalText() ([]byte, error)    { return []byte("<redacted>"), nil }
func (v Value) LogValue() slog.Value            { return slog.StringValue("<redacted>") }
func (v Value) Bytes() []byte                   { return v.b }
```

The zero-exported-fields invariant is asserted by `TestValue_StructHasNoExportedFields` (reflect-based field walk; fails CI if a future field is added). `LogValue` is implemented per `log/slog` — slog handlers (Text, JSON, anything composing `slog.Handler`) call `LogValue` instead of reflecting into the struct, which keeps the redaction structural under slog as well.

OSS rejected:

- HashiCorp Vault — too heavy for persona-A (one-binary operator). Reopen: first hosted-backend customer.
- systemd `LoadCredential` — macOS lacks parity; same UX requirement on both platforms blocks adoption.
- `99designs/keyring` (MIT) — broader backend matrix but heavier dep tree; revisit only if Windows or KWallet support becomes load-bearing.

## 4. Canonical keys

| Key                           | Shape                                             | Hot-path consumer                              |
|-------------------------------|---------------------------------------------------|------------------------------------------------|
| `regatta.anthropic_api_key`   | single-line bearer token                          | LLM dispatcher                                 |
| `regatta.gh_token`            | single-line PAT / fine-grained token              | orchestrator GitHub client; PR-watch; auto-merge |
| `regatta.brief_hmac_keys`     | multi-line `<id>:<base64-key>` keyring (1+ lines) | brief signer + verifier; matches `REGATTA_HMAC_KEYRING` env shape (`cmd/regatta/serve.go:477`) |
| `regatta.audit_hmac_key`      | single-line base64 key                            | audit-event signer                             |

Key names are validated against the regex `^regatta\.[a-z0-9_]+$` at CLI and library entry. Unknown keys are rejected — typo-protection.

### Signal-safe SIGHUP rotation

Rotation uses `signal.Notify` + `atomic.Pointer[cacheSnapshot]`, **never** a raw OS signal handler. Concretely:

```go
type cacheSnapshot struct {
    values map[string]Value // canonical key → Value
    source map[string]string // canonical key → adapter name
    at     time.Time
}

type Cache struct {
    ptr atomic.Pointer[cacheSnapshot]
}

// hot path — readers never block, never lock.
func (c *Cache) Get(key string) (Value, string, bool) {
    snap := c.ptr.Load()
    v, ok := snap.values[key]
    return v, snap.source[key], ok
}

// rotation loop — runs in a normal goroutine, not in the OS signal handler.
// `signal.Notify` buffers signals onto a Go channel; the goroutine reads
// from the channel and is free to do anything (lock, allocate, log, syscall).
func (c *Cache) Run(ctx context.Context, fetcher Fetcher) {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGHUP)
    defer signal.Stop(sigCh)
    for {
        select {
        case <-ctx.Done():
            return
        case <-sigCh:
            snap := refetchAll(ctx, fetcher) // re-reads chain for all canonical keys
            c.ptr.Store(snap)                // atomic publish; readers see old or new, never partial
        }
    }
}
```

Why this matters: Go's runtime forbids most operations (mutex acquisition, channel send, allocation, goroutine scheduling) inside an OS signal handler — a raw `os/signal`-handler-style call to `RWMutex.Lock()` is async-signal-unsafe and can deadlock the runtime. `signal.Notify` is the documented escape hatch: the runtime signal handler does the minimum work (push to a buffered channel) and a normal goroutine wakes up to handle the event. The atomic-pointer publish is the standard read-mostly cache pattern from `sync/atomic`'s godoc.

Tests:

- `TestCache_SIGHUPSwapsSnapshotAtomically` — concurrent reader goroutines + a SIGHUP-driven swap; assert every read sees a consistent snapshot (either fully-old or fully-new), never a partial mix.
- `TestCache_ReadersDoNotBlockDuringRotation` — measures reader latency during rotation; asserts no read-side stall.

This replaces the original "RWMutex.Lock in signal handler" sketch, which was async-signal-unsafe.

## 5. Startup flow

Order (top-to-bottom; first non-`ErrNotFound` wins per chain):

1. Supervisor (W3) calls `fetcher := secrets.Default(ctx)`.
2. For each canonical key, supervisor calls `fetcher.Get(ctx, key)`.
3. Resolved secrets are exported to the `regatta serve` child env block AND held in the in-process `atomic.Pointer[cacheSnapshot]` cache for signal-safe SIGHUP reload (see §4).
4. If any single key fails to resolve, supervisor logs `secret_missing key=<canonical> chain=<adapter,…>` + emits substrate event `secret_resolved{key, source}` or `secret_missing{key, attempted}`, and continues. The serve process starts with that secret absent. Hot-path consumers degrade per their own missing-secret behavior (e.g. LLM dispatcher fails the first dispatch with a clear "no API key" error rather than crashing the supervisor).
5. SIGHUP → supervisor re-runs steps 1-3 and swaps the in-process `atomic.Pointer[cacheSnapshot]` atomically. **No re-exec.**

> **Amendment (2026-06-03, ratified per `feedback_spec_pattern_authority`):** the original draft of step 5 called for re-exec of the child process tree on SIGHUP. The shipped implementation publishes a fresh snapshot via `atomic.Pointer.Store` and re-exports the canonical env-var aliases (`ANTHROPIC_API_KEY`, `GH_TOKEN`, etc.) so legacy `os.Getenv`-style readers see the new values without restart. Re-exec was dropped because it drops in-flight HTTP connections (regatta serve's GitHub-client + LLM-dispatcher both keep keep-alive pools open), and operators rotating a token mid-tick are NOT signalling "tear down all sockets". Operator-driven full restart remains available via the supervisor (`regatta restart` / systemd `restart`) when a re-exec is genuinely required. UX > best-practices per `feedback_decision_priority`. The substrate-event readers and the rotation drill (§7) consume the in-process snapshot via `Cache.Get`; the rotation-drill test `TestCache_SIGHUPSwapsSnapshotAtomically` is the load-bearing assertion. See #654.

No secret value crosses the substrate-event boundary — events log `source` (`keychain` / `pass` / `env` / `missing`), never the value. Per `feedback_root_cause`: the failure mode "we accidentally logged a token" is structurally prevented, not lint-suppressed.

## 6. CLI

```
regatta secret set <key>            # prompts via /dev/tty; writes to platform store
regatta secret get <key>            # refuses unless --unsafe; otherwise prints "<key>: <present|missing> source=<…>"
regatta secret get <key> --unsafe   # prints raw value; documented as audit-only
regatta secret status               # canonical key names + source per key (no values) + chain diagnostic + Phase-X adapter availability
regatta secret rm <key>             # removes from platform store; env-fallback unaffected
```

`set` writes to the **first writable** adapter in the chain (macOS Keychain on Darwin; `pass` on Linux). The env-var adapter is read-only — `set` against an env-only system errors with a recovery doc link.

UX guard: `get` without `--unsafe` is the default precisely so muscle-memory `regatta secret get regatta.gh_token | xargs …` shell pipelines do not silently leak tokens into terminal scrollback or shell history. Per `feedback_decision_priority` (UX over speed).

Decision: `regatta secret list` is **dropped**. `status` subsumes it (per-key source rows + chain diagnostic + Phase-X adapter availability) and `feedback_deletion_default` is the tiebreaker — one verb is smaller than two. Operators reaching for muscle-memory parity with `gh secret list` get a clear "did you mean `regatta secret status`?" hint in the unknown-subcommand path.

### Audit logging (A+ tier)

Every `regatta secret get` invocation emits a structured audit event before returning the value (or the redacted summary):

```
audit_event{
  action=secret_get,
  key=<canonical-key>,
  source=<keychain|pass|env|missing>,
  unsafe=<true|false>,                # true iff --unsafe was passed
  user=<os.Getuid + os.Geteuid summary>,
  tty=<isatty(stdout)>,
  timestamp=<RFC3339Nano>,
}
```

Value is **never** in the event. The event is signed by `regatta.audit_hmac_key` (already in scope) and emitted through the existing substrate event channel. `regatta secret set` and `regatta secret rm` emit analogous `secret_set` / `secret_rm` events (without value). Asserted by `TestCLI_SecretGet_EmitsAuditEventWithoutValue` and `TestCLI_SecretGet_UnsafeFlagAuditedAsUnsafe`.

## 7. Rotation

- File-watcher rejected for Landing 2 (LoC + cross-platform fsnotify churn). Reopen-trigger: operator rotates ≥1×/week and SIGHUP friction surfaces.
- SIGHUP re-reads the full chain. Atomic cache swap; in-flight requests use the snapshot taken at request start.
- `regatta secret set <key>` does NOT auto-reload — operator must `kill -HUP $(pgrep regatta-supervisor)` (or `regatta reload-secrets` as a thin wrapper, ~10 LoC).
- Rotation drill (item rubric tier A): `pass insert -e regatta/anthropic_api_key && regatta reload-secrets` rotates the key without supervisor restart. Asserted by `TestRotation_SIGHUPRefreshesCachedValue`.

## 8. Permissions

- regatta supervisor runs under the operator's UID, not root. macOS Keychain access requires the same UID that originally wrote the secret; `pass` decryption requires the GPG private key on that user's keyring. Documented in `docs/engineer/operator-runbook.md` (new section, ~30 lines).
- systemd unit ships with `User=regatta` (matching W3) and `PrivateTmp=yes`. macOS launchd plist runs as the console user. Documented in `regatta install-service` output.
- gpg-agent TTL configurable via `regatta.yaml`: `secrets.gpg_agent_ttl_seconds` (default 28800 = 8h). Invocation pattern: regatta does **not** spawn gpg-agent directly (that is the user-session manager's job — systemd `--user` on Linux, launchd on macOS). Instead, supervisor reads the configured TTL and writes/updates the operator's `~/.gnupg/gpg-agent.conf` with `default-cache-ttl <N>` + `max-cache-ttl <N>` during `regatta install-service`, then sends `gpg-connect-agent reloadagent /bye` to pick up the change. Subsequent `pass` invocations inherit the running gpg-agent via the standard `GPG_AGENT_INFO`/socket discovery — no env plumbing from regatta required. Documented in operator runbook; passphrase-cache expiry mitigation covered by `TestGPGAgentTTL_ExpiredKeyringUsesPassphrase` (asserts that after TTL expiry, `pass show` re-prompts via pinentry — verifies the operator's pinentry path is wired, not that regatta caches the passphrase).

## 9. Operator UX

Bootstrap (one-shot, per fresh laptop):

```
$ regatta install-service
checking for pass… ok (v1.7.4)
checking gpg-agent… ok
regatta.anthropic_api_key: MISSING — prompt to set? [Y/n]
Enter value (echo off): ****
written to pass: regatta/anthropic_api_key
regatta.gh_token: MISSING — prompt to set? [Y/n]
…
regatta.brief_hmac_keys: present (source=pass)
regatta.audit_hmac_key: MISSING — generate fresh key? [Y/n]
generated + written to pass: regatta/audit_hmac_key
installing systemd unit → /etc/systemd/user/regatta.service
done. start with: systemctl --user start regatta
```

Steady state:

```
$ regatta secret status
key                          source     present
regatta.anthropic_api_key    pass       yes
regatta.gh_token             pass       yes
regatta.brief_hmac_keys      pass       yes
regatta.audit_hmac_key       env        yes    [fallback active — consider migrating to pass]
chain: pass → env
phase-x adapters: vault (compiled out), aws-sm (compiled out), gcp-sm (compiled out)
```

Operator never sees a secret value in steady-state diagnostics — only the source label.

## 10. Performance

- One-shot at supervisor boot: 3 adapter calls × 4 keys = 12 lookups, expected wall time <500 ms on macOS, <1 s on Linux (pass + gpg decrypt). Not in tick path; not in hot path.
- SIGHUP-driven refresh: same 12 lookups, same envelope. Operator-paced, not loop-paced.
- In-process cache: O(1) lookup via `atomic.Pointer.Load`; readers never lock, never block on rotation. Rotation publishes via `atomic.Pointer.Store` from a normal goroutine driven by `signal.Notify` — never from an OS signal handler (see §4).

Benchmark target (rubric tier A): `BenchmarkFetcher_Default_4Keys` < 1 s wall on Linux CI runner, asserted in CI.

## 11. Risks (8+, with mitigations)

R1. **Keychain unlock prompt blocks startup.** macOS Keychain may pop a GUI prompt on first access. Mitigation: supervisor warns + sets a 30 s timeout; on timeout falls through to env. Operator pre-authorizes with `security add-generic-password -A` during `install-service` — the `-A` flag grants access to **any** application without a per-process ACL prompt, per the `security(1)` manpage on macOS 14+ ("Allow any application to access this item without warning"); regatta wraps that documented flag with no additional ACL surface. Suppression is asserted by integration test `TestKeychainPromptSuppress_AllowsAnyApp` (runs only when `TEST_KEYCHAIN_PROMPT=1` is set on a real darwin runner). Recovery doc linked from error.

R2. **CI environments lack any keychain.** GitHub Actions runners have neither macOS Keychain access nor a running gpg-agent. Mitigation (decision): the CI fallback is **explicit, not timeout-based**. `REGATTA_SECRETS_DISABLE_KEYCHAIN=1` short-circuits the keychain adapter and the pass adapter to immediate `ErrNotFound`, so the env adapter resolves the chain with no platform-store wall time. Rationale: timeout-then-env adds 30 s of wall time per missing key on every CI boot and silently masks misconfigured runners; an explicit knob fails loudly when set incorrectly and adds zero latency when set. Default workflow templates set this knob; documented in `regatta install-service --ci` output. Asserted by `TestEnvOnly_NoKeychainAvailable` + `TestEnvOnly_ExplicitDisableSkipsPlatformAdaptersWithoutTimeout`.

R3. **Rotation race.** Operator rotates `regatta.gh_token` mid-flight; an in-progress orchestrator call uses the old token; the next call uses the new. Mitigation: cache snapshot taken at request start (already the GitHub client's pattern). Documented as acceptable — rotation is operator-paced, not adversarial.

R4. **Multi-user macOS Keychain shared by family + operator.** Other users on the same Mac could theoretically read regatta secrets. Mitigation: macOS Keychain is per-UID; only the operator's `login.keychain-db` is accessed. Sandboxed by OS, not by us. Documented.

R5. **Linux variants without gpg-agent.** Alpine + minimal Docker images lack gpg-agent by default. Mitigation: `pass_linux.go` detects absence of `gpg-agent` binary, logs once at startup, falls through to env. Operator on Alpine uses env-only (the expected pattern for containerized prod).

R6. **Restored-from-backup keychain has stale tokens.** Operator restores a 6-month-old Time Machine backup; GH token has been revoked upstream. Mitigation: regatta surfaces the upstream GitHub 401 with a `regatta secret rotate-needed key=regatta.gh_token` hint in the error message. Existing GitHub-client retry logic is untouched.

R7. **Revoked GH token without rotation.** Same root cause as R6 but in-session. Mitigation: GitHub client returns 401; orchestrator emits substrate event `external_auth_failed`; W1 alarm webhook (already shipped) escalates. Not unique to W6 — pre-existing failure mode.

R8. **In-process logging accidentally prints secret value.** A debug `log.Printf("got config: %+v", cfg)` could dump a token. Mitigation: secret type is `secrets.Value` — a `[]byte` wrapped in a struct with **zero exported fields**, plus `String()` / `GoString()` / `MarshalJSON()` / `MarshalText()` / `slog.LogValue()` all returning `<redacted>` (see §3). Reflection-based formatters (`fmt %v`/`%+v`, `encoding/json`, slog Text + JSON handlers) cannot reach the underlying bytes. Asserted by: `TestValue_StructHasNoExportedFields` (reflect-walk; fails if a future field is added), `TestValue_FmtSprintfV_Redacts` (`fmt.Sprintf("%v", value)` → `<redacted>`), `TestValue_FmtSprintfPlusV_Redacts` (`%+v` redacts), `TestValue_JSONRedacts` (`json.Marshal` → `"<redacted>"`), `TestValue_SlogInfo_Redacts` (asserts a `slog.NewTextHandler` writing to a `bytes.Buffer` logs `<redacted>` and never the raw secret substring when a `Value` is passed as a slog attribute), `TestValue_StringRedacts`.

R9. **gpg-agent absent on headless Linux server.** Mitigation: detection in `pass_linux.go` (same path as R5); clear error + recovery doc link, no stack trace. Per item rubric tier A+ failure mode (h).

R10. **CLI `get --unsafe` accidentally piped into a tee'd log.** Mitigation: `--unsafe` requires interactive tty by default (refuses if stdin or stdout is not a tty unless `--non-interactive` is also passed). Layered safety.

R11. **Cross-platform build breakage.** `keychain_darwin.go` calling go-keyring on a Linux build server. Mitigation: build tags + stub files (`keychain_other.go`, `pass_other.go`). CI runs `GOOS=linux go build ./…` and `GOOS=darwin go build ./…` (existing matrix).

R12. **Path-traversal via key name.** `pass show ../etc/passwd` if key name unvalidated. Mitigation: canonical-key regex (`^regatta\.[a-z0-9_]+$`) rejected at CLI + library entry. Asserted by `TestKeyName_RejectsPathTraversal`.

## 12. Test plan

Per-adapter unit:

- macOS Keychain adapter — uses a temp keychain (`security create-keychain`); skip on non-darwin. New `keychain_darwin_test.go`.
- `pass` adapter — uses a temp `PASSWORD_STORE_DIR` + ephemeral GPG keyring; skip on non-linux + skip if `pass` binary absent. New `pass_linux_test.go`.
- env adapter — pure unit, runs on all platforms.
- composite — table test over `(adapter ordering, simulated errors) → expected chosen source`.

Integration on real platform store (skip-on-CI by default; runs in nightly on a self-hosted runner):

- macOS Keychain round-trip: `set` → `get` → `rm` against a temp keychain.
- pass round-trip: same against a temp PASSWORD_STORE_DIR.

Env-only fallback:

- `TestEnvOnly_NoKeychainAvailable` (R2) — assert chain resolves entirely via env when keychain disabled.

CLI flow:

- `TestCLI_SecretSet_StripsTrailingNewline` (regression hedge for shell pipelines).
- `TestCLI_SecretGet_RefusesWithoutUnsafe`.
- `TestCLI_SecretStatus_NeverPrintsValues`.
- `TestCLI_KeyNameValidation_RejectsBadNames`.

Redaction guard:

- `TestValue_StructHasNoExportedFields` (reflect-walk; structural redaction invariant).
- `TestValue_StringRedacts`.
- `TestValue_FmtSprintfV_Redacts` + `TestValue_FmtSprintfPlusV_Redacts` (R8).
- `TestValue_JSONRedacts`.
- `TestValue_SlogInfo_Redacts` — slog Text handler on a buffer; assert log line contains `<redacted>` and not the raw secret substring (R8).

Rotation (signal-safe, per §4):

- `TestRotation_SIGHUPRefreshesCachedValue` (item rubric tier A).
- `TestCache_SIGHUPSwapsSnapshotAtomically` — concurrent readers + SIGHUP-driven swap; every read sees fully-old or fully-new snapshot (no partial mix).
- `TestCache_ReadersDoNotBlockDuringRotation` — reader latency unaffected by in-flight rotation.

Audit logging:

- `TestCLI_SecretGet_EmitsAuditEventWithoutValue` — asserts `audit_event{action=secret_get, …}` emitted with no value substring.
- `TestCLI_SecretGet_UnsafeFlagAuditedAsUnsafe` — asserts `unsafe=true` flag set when `--unsafe` is passed.

gpg-agent TTL:

- `TestGPGAgentTTL_ExpiredKeyringUsesPassphrase` — asserts that after TTL expiry, `pass show` triggers the operator's pinentry path (i.e. regatta does not cache the passphrase in-process).

## 13. Test names (10+, 1-line godoc each, per `feedback_test_godoc_one_line`)

```
TestFetcher_Default_MacOSChainsKeychainThenEnv               // macOS Default returns keychain → env chain
TestFetcher_Default_LinuxChainsPassThenEnv                   // Linux Default returns pass → env chain
TestComposite_FirstHitWins                                   // composite returns first non-ErrNotFound
TestComposite_AllMissReturnsErrNotFound                      // composite surfaces ErrNotFound only when every adapter misses
TestEnvFetcher_MapsCanonicalKeyToEnvVarName                  // canonical key → REGATTA_ANTHROPIC_API_KEY etc.
TestEnvOnly_NoKeychainAvailable                              // CI fallback path covered (R2)
TestEnvOnly_ExplicitDisableSkipsPlatformAdaptersWithoutTimeout // REGATTA_SECRETS_DISABLE_KEYCHAIN=1 short-circuits, zero wall time
TestKeychain_Darwin_RoundTrip                                // macOS Keychain set/get/rm against temp keychain
TestKeychainPromptSuppress_AllowsAnyApp                      // `security add-generic-password -A` suppresses prompt (R1, gated by TEST_KEYCHAIN_PROMPT)
TestPass_Linux_RoundTrip                                     // pass set/get/rm against temp PASSWORD_STORE_DIR
TestPass_GPGAgentAbsent_ProducesRecoveryHint                 // gpg-agent missing → single-line error + recovery doc link (R9, rubric A+)
TestKeyName_RejectsPathTraversal                             // canonical-key regex rejects `../etc/passwd` (R12)
TestValue_StructHasNoExportedFields                          // reflect-walk; structural redaction invariant (R8)
TestValue_StringRedacts                                      // Value.String returns "<redacted>" (R8)
TestValue_FmtSprintfV_Redacts                                // fmt.Sprintf("%v", value) returns "<redacted>" (R8)
TestValue_FmtSprintfPlusV_Redacts                            // fmt.Sprintf("%+v", value) returns "<redacted>" (R8)
TestValue_JSONRedacts                                        // Value.MarshalJSON returns "<redacted>" (R8)
TestValue_SlogInfo_Redacts                                   // slog Text handler logs "<redacted>" not raw secret (R8)
TestCLI_SecretGet_RefusesWithoutUnsafe                       // bare `get` never prints raw token
TestCLI_SecretGet_EmitsAuditEventWithoutValue                // audit_event{action=secret_get,...} no value substring
TestCLI_SecretGet_UnsafeFlagAuditedAsUnsafe                  // --unsafe → unsafe=true in audit event
TestCLI_SecretStatus_NeverPrintsValues                       // status output asserts no value substring present
TestRotation_SIGHUPRefreshesCachedValue                      // rubric tier A rotation drill
TestCache_SIGHUPSwapsSnapshotAtomically                      // atomic.Pointer swap; readers see fully-old or fully-new snapshot (§4)
TestCache_ReadersDoNotBlockDuringRotation                    // signal-safe rotation; readers never block (§4)
TestGPGAgentTTL_ExpiredKeyringUsesPassphrase                 // post-TTL `pass show` re-prompts via pinentry (§8, rubric A+ mitigation)
BenchmarkFetcher_Default_4Keys                               // < 1 s wall on Linux CI runner
```

Total: 24 tests + 1 benchmark.

## 14. B/A/A+ scorecard

| Tier        | Criteria |
|-------------|----------|
| B (floor)   | (a) `internal/secrets/` package lands with `Fetcher` interface + env + ≥1 platform adapter (macOS Keychain OR pass). (b) Supervisor boot reads all 4 canonical keys, falls back to env on miss. (c) `regatta secret set / get / list` CLI. (d) Release-notes fence in PR. (e) Banned-phrase + doc-check clean. |
| A (target)  | B + (f) Both macOS Keychain AND `pass` adapters land. (g) Signal-safe SIGHUP-driven cache refresh — `signal.Notify` + `atomic.Pointer` per §4; rotation drill `TestRotation_SIGHUPRefreshesCachedValue` + `TestCache_SIGHUPSwapsSnapshotAtomically` pass. (h) `regatta secret status` shows source-per-key without values. (i) Adversarial reviewer subagent posts on the PR + findings addressed inline or as tracking issues. (j) All 24 tests pass; benchmark < 1 s. (k) Substrate event `secret_resolved` / `secret_missing` emitted with `source` field, never value. |
| A+ (stretch)| A + (l) `gopass` adapter compiled out behind build tag, with a passing test asserting drop-in compatibility. (m) Failure mode parity: gpg-agent absent on Linux produces a single-line error with a recovery doc link, not a stack trace; asserted by `TestPass_GPGAgentAbsent_ProducesRecoveryHint`. (n) `Value` redaction type used everywhere a secret crosses an API boundary (config struct, logger, substrate event); audit asserts no `string` typed secret survives in code grep; `TestValue_StructHasNoExportedFields` + `TestValue_SlogInfo_Redacts` enforce structural redaction. (o) Phase-X interface stability: a stub `vaultFetcher` lands compiled-out, demonstrating the interface accommodates Vault without re-architecture. (p) Audit-event coverage: `secret_get` / `secret_set` / `secret_rm` events emitted with `unsafe` + `user` + `tty` + `timestamp` fields per §6; `TestCLI_SecretGet_EmitsAuditEventWithoutValue` passes. (q) gpg-agent TTL mitigation: `TestGPGAgentTTL_ExpiredKeyringUsesPassphrase` passes. |

Falsifiable thresholds: 24 tests pass; benchmark < 1 s on Linux CI runner; banned-phrase lint clean; markdown link integrity clean; release-notes fence present; reviewer subagent posts ≥1 substantive comment (simplification, deletion, edge case, or risk); zero `string` typed secrets in `internal/secrets` exports (grep gate); `Value` has zero exported fields (reflect-walk asserts).

## 15. Risk-tier adversarial review

Spawn reviewer subagent (per `feedback_review_every_step` + `feedback_adversarial_review`) with this prompt skeleton:

> Read `docs/engineer/specs/2026-06-02-phase-autonomy-w6-secret-credential-fetch.md`. Hunt: (a) simplification — is `Fetcher` interface over-engineered for 3 adapters? Would a `map[string]string` cache populated at boot be smaller? (b) deletion — can `regatta secret list` and `status` collapse into one command? (c) edge case — what if `pass` is installed but the password store directory is unreadable (permission bit flipped)? (d) risk tier — is R3 (rotation race) actually acceptable given orchestrator may hold a 30-min PR-merge in-flight? (e) OSS reuse missed — does Hashicorp's `vault/api` provide a `Fetcher`-shaped interface we should match for Phase-X interface stability? (f) cross-platform — does `go-keyring` v0.2.5 still work on macOS 15 (Sequoia)? Last release was 2024.

Findings policy: fix inline OR file a tracking issue per `feedback_unaddressed_load_bearing` (every load-bearing leftover gets an issue). Reviewer non-empty comment ≠ auto-block; substantive only — per `feedback_review_proportional`.

Pre-emptively addressed in this draft:

- **Simplification (a):** `Fetcher` is retained over `map[string]string` because SIGHUP refresh + per-adapter source labeling (for `regatta secret status`) need the dispatch boundary. A flat map collapses both features.
- **Deletion (b):** `list` and `status` overlap; `list` is **dropped** in §6 — `status` subsumes it (per-key source rows + chain diagnostic + Phase-X adapter availability). Saves one CLI verb per `feedback_deletion_default`. Operators reaching for `gh secret list` parity get a clear "did you mean `regatta secret status`?" hint in the unknown-subcommand path.
- **Edge case (c):** unreadable password store → `pass show` returns nonzero; chain falls through to env. Asserted by `TestPass_StoreUnreadable_FallsThroughToEnv` — added to §13 if reviewer pushes.
- **Risk tier (d):** R3 documented as acceptable; per-request snapshot already protects mid-flight calls.
- **OSS reuse (e):** Vault `vault/api` returns `*Secret` with metadata — heavier than our `Fetcher.Get`. Phase-X adapter will wrap, not match shape. Documented as Phase-X risk if/when adapter lands.
- **Cross-platform (f):** go-keyring v0.2.5 (2024-04) compatibility is validated empirically by integration test `TestKeychain_Darwin_RoundTrip` on the targeted macOS runner; no claim is made about specific OS-version compat from upstream trackers (that claim was speculative and is removed). If round-trip fails on a future macOS, fall through to env on darwin and reopen this spec.

## 16. Followups (inline, per `feedback_unaddressed_load_bearing`)

1. **gopass drop-in adapter (rubric A+).** Tracking issue filed if A+ not hit in implementation PR.
2. **File-watcher live reload.** Reopen-trigger: operator rotation ≥1×/week. Tracking issue filed at spec-merge time with that trigger documented.
3. **Vault/AWS-SM/GCP-SM Phase-X adapters.** Tracking issue + label `phase-x` + label `external-customer-trigger`. Filed at spec-merge time, points to this spec §3 interface.
4. **Linux libsecret + Windows Credential Manager.** Tracking issue filed at spec-merge time. Reopen-trigger: persona-A operator runs regatta on a host where neither macOS Keychain nor `pass`/gpg-agent is available.
5. **`regatta reload-secrets` thin wrapper.** ~10 LoC; lands in same PR as W6 implementation OR follow-up issue if A+ scope creeps.

## 17. Comment sweep

Status: **clean** (prose-only spec; no Go/test code introduced; per `feedback_comments_discipline` no code-comment surface exists to sweep).

Implementation PR's separate comment sweep gate applies to the `internal/secrets/` Go code — to be enforced at impl-PR review time per `feedback_comments_lint_reconcile`.

## 18. Self-host filter

Every claim filtered per `docs/engineer/briefs/2026-06-01-self-host-first.md` §1 — "does the sole internal operator need this to dispatch regatta-the-binary at this repo unattended?".

- macOS Keychain + `pass` adapters: **keep** — the operator's two laptops are macOS + Linux.
- env fallback: **keep** — operator runs CI under GitHub Actions runners (no keychain).
- Vault / AWS-SM / GCP-SM: **defer Phase X** — explicit reopen-trigger = first hosted-backend customer ask.
- libsecret / KWallet: **defer Phase X** — reopen on first operator pain report.
- Windows Credential Manager: **defer Phase X** — operator does not run regatta on Windows.
- gopass: **defer rubric A+** — reopen on `pass` subprocess latency complaint or Go-binding desire.

## 19. Deletion default

Per `feedback_deletion_default` — "what got smaller?":

- The supervisor boot sequence shrinks from "operator types 4 export commands per wake" → "operator types nothing". UX surface area smaller by ~4 lines of operator muscle memory per boot.
- The `cmd/regatta/serve.go` HMAC env-reading paths (`:477`, `:486`, `:494`, `:625`, `:628`) collapse into one `secrets.Default(ctx).Get(ctx, "regatta.brief_hmac_keys")` call. Five env-var fan-out points → one call. Net code shrinkage at impl time: estimated -40 LoC in serve.go, +150 LoC in new package = +110 net. Acceptable because the 110 LoC is consolidated in one tested package and replaces ad-hoc env reads in critical-path code.

## 20. Cites

- `.regatta/items/phase-autonomy-w6-secret-credential-autonomic-fetch.md` — item, source of truth for keys + adapters + rubric.
- `docs/engineer/briefs/2026-06-02-phase-autonomy-amendment.md` §11 W6 — wave context + LoC budget.
- `cmd/regatta/serve.go:477-628` — existing env-var HMAC reads (consolidation target).
- `internal/orchestrator/` — GitHub client consumer of `regatta.gh_token`.
- `pass` v1.7.4 (GPL-2) — adopted Linux store.
- `github.com/zalando/go-keyring` v0.2.5 commit `923f7c4` (MIT) — adopted macOS shim.
- `github.com/gopasspw/gopass` (MIT) — Phase-X Linux alt.
- HashiCorp Vault — Phase-X external-store reopen-trigger.
- systemd `LoadCredential` — rejected (macOS parity blocker).
- `feedback_decision_priority` — UX > performance > best-practices.
- `feedback_research_design_principles` — adopt-first; macOS Keychain + pass adopted; supervisor shim built.
- `feedback_root_cause` — redaction is structural (Value type), not lint-suppression.
- `feedback_spec_pattern_authority` — item is design authority; spec extends, does not contradict.
