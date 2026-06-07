# Secrets config unification — `secrets:` block in `regatta.yaml`

_Phase-S2 trust-the-loop, brief `docs/engineer/briefs/2026-06-01-self-host-first.md` §1 (self-host filter)._

Memory rule cites: `feedback_default_simpler`, `feedback_decision_priority`, `feedback_grade_rubric`, `feedback_no_memory_reread`.

## 1. Problem

Today an operator wiring regatta on a new repo must discover and set 8+ env-var names, each named via inconsistent convention, all undocumented in one place:

- `ANTHROPIC_API_KEY`, `GITHUB_TOKEN` (vendor-canonical)
- `REGATTA_HMAC_KEY`, `REGATTA_HMAC_KEY_ENV`, `REGATTA_HMAC_KEY_ID`, `REGATTA_HMAC_KEYRING` (HMAC keyring)
- `REGATTA_AUDIT_HMAC_KEY`, `REGATTA_AUDIT_HMAC_KEY_ID` (audit chain)
- `REGATTA_APPROVAL_TOKEN_KEY_ENV`, `REGATTA_APPROVAL_TOKEN_KEY_ID` (approval HMAC)
- `<UsageAPIKeyEnv>` (cost reconciliation; CUE-modeled at `#CostGovernor.usage_api_key_env`)
- `gh_token_env` (alarm webhook; CUE-modeled at `#AlarmWebhook.gh_token_env`)

Three lookup paths for the same job — (a) raw `os.Getenv`, (b) CLI `--*-env` flag, (c) CUE `*_env` field — fragment operator UX. The secrets package already has a clean `Fetcher` interface (`internal/secrets/secrets.go:69`) with `composite` ordering (keychain → env on darwin, pass → env on linux) and canonical key namespace (`regatta.anthropic_api_key`, `regatta.gh_token`, `regatta.brief_hmac_keys`, `regatta.audit_hmac_key`), but it is wired to FOUR of the surfaces; the rest still raw-`Getenv`.

Self-host operator pain: cannot answer "which env var sets X" without grepping. Reading `regatta.yaml` does not reveal where secrets come from.

## 2. Goal

ONE `secrets:` block in `regatta.yaml` lists every logical secret + its source. CUE validates shape. Code reads through one `secrets.Fetcher` chain. Env-var raw lookups for secrets disappear.

Operator wires a new repo by editing one yaml block. `regatta doctor` (separate spec) reads the same block to preflight-check every secret resolves.

## 3. Scope (in)

3.1 New CUE block:
```cue
#Secret: {
    source: "env" | "keychain" | "pass" | "file"
    name?:  string           // env var name (source=env) or keychain/pass entry (source=keychain|pass)
    path?:  string           // file path (source=file)
    key_id?: string          // for keyed secrets (HMAC)
}

#Secrets: {
    anthropic_api_key?: #Secret
    gh_token?:          #Secret
    brief_hmac?:        #Secret
    audit_hmac?:        #Secret
    approval_token?:    #Secret
}
```

3.2 Default behavior (no `secrets:` block): unchanged from today. Composite fetcher with platform-native first + env fallback. Canonical keys resolved against existing env-var names (back-compat).

3.3 With `secrets:` block: every named secret routes through the configured source. `source: env, name: GH_TOKEN_REVIEWER` keeps an operator-chosen env name; `source: keychain, name: regatta-bot/gh` uses an OS keychain entry.

3.4 Migrate FIVE prod call sites off raw `os.Getenv` onto `secrets.Fetcher.Get(ctx, canonicalKey)`:
- `internal/gates/l4/adapter.go:62` (`ANTHROPIC_API_KEY`)
- `internal/program/provider_anthropic.go:49` (`ANTHROPIC_API_KEY`)
- `cmd/regatta/wire_keyring.go:40,48,52,56` (`REGATTA_HMAC_KEY*`)
- `cmd/regatta/audit.go:271,272` (`REGATTA_AUDIT_HMAC_KEY*`)
- `cmd/regatta/approval_decide.go:195,203` (`REGATTA_APPROVAL_TOKEN_KEY*`)

3.5 Documentation: ONE table in `docs/operator/configure.md` listing every secret + canonical key + default env-var fallback + `secrets:` block syntax.

## 4. Scope (out)

4.1 **Telemetry env vars** (`OTEL_*`, `OTEL_EXPORTER_*`) — not secrets, follow OTel spec convention, operator expects vendor-canonical names. Leave.

4.2 **Operator-tuning env vars** (`REGATTA_REVIEW_REPO`, `GH_USER_REVIEWER`, `EnvModel`, `REGATTA_SECRETS_DISABLE_KEYCHAIN`) — not secrets, not part of `secrets:` block. Separate `tuning:` block deferred to Phase-X.

4.3 **`HOME` / `USER` / `envNotifySocket`** — platform-mandated. Leave.

4.4 **Test-only env vars** (`REGATTA_E2E_*`) — test harness, no operator surface. Leave.

4.5 **Cobra subcommand grouping** — deferred Phase-X per audit re-rank.

4.6 **New secret backends** (Vault, AWS Secrets Manager, etc.). Out — single-operator self-host does not need cloud KMS. Phase-X (external-customer trigger).

4.7 **Secret rotation UX beyond existing `keys re-sign-briefs`**. Out.

4.8 **Wipe/zeroize on `secrets.Value`** — separate hardening spec; `<redacted>` formatter already covers leak surface.

## 5. Schema delta (CUE)

```cue
// contracts/schemas/regatta.v1.cue

#Config: {
    // ... existing fields ...
    secrets?: #Secrets
}

#Secret: {
    source: "env" | "keychain" | "pass" | "file"
    // exactly one of `name` or `path`
    name?:   string
    path?:   string
    key_id?: string
}

#Secrets: {
    anthropic_api_key?: #Secret
    gh_token?:          #Secret
    brief_hmac?:        #Secret & {key_id?: string}
    audit_hmac?:        #Secret & {key_id?: string}
    approval_token?:    #Secret & {key_id?: string}
}
```

Defaults preserved by code, NOT CUE: if a secret is absent, fetcher uses canonical key + existing env-var convention (back-compat).

## 6. Code delta

6.1 `internal/secrets/yaml_loader.go` (NEW, ~80 LOC):
```go
// BuildFromConfig wires a Fetcher chain from the yaml secrets: block.
// Each canonical key (regatta.anthropic_api_key, ...) maps to one Source.
// Falls back to Default() chain for keys with no yaml mapping.
func BuildFromConfig(secrets *schemas.Secrets) (Fetcher, error)
```

6.2 `internal/secrets/file_fetcher.go` (NEW, ~40 LOC): `NewFileFetcher(path)` — reads file contents, trims trailing newline. Permission-check 0600.

6.3 Five call-site migrations (3.4). Each replaces `os.Getenv("X")` with `fetcher.Get(ctx, secrets.KeyX)`. Bytes returned as `secrets.Value` (already redacts on log).

6.4 `cmd/regatta/wire_secrets.go` (EXTEND): build fetcher via `BuildFromConfig` if `cfg.Secrets != nil`, else `secrets.Default(ctx)`.

6.5 `cmd/regatta/reload_secrets.go` — no change. SIGHUP-triggered atomic-pointer rotation already covers new fetcher.

## 7. Examples delta

`examples/minimal/regatta.yaml` — unchanged (no `secrets:` block; defaults).

`examples/full/regatta.yaml` — add commented `secrets:` block exercising all four sources:
```yaml
secrets:
  anthropic_api_key:
    source: keychain
    name: regatta/anthropic
  gh_token:
    source: env
    name: GH_TOKEN_REVIEWER
  brief_hmac:
    source: file
    path: /etc/regatta/brief.key
    key_id: brief-2026-06
  audit_hmac:
    source: pass
    name: regatta/audit-hmac
    key_id: audit-2026-06
```

## 8. Tests

8.1 `TestSecrets_BuildFromConfig_EnvSource` — yaml `source: env, name: FOO` resolves to `os.Getenv("FOO")`.

8.2 `TestSecrets_BuildFromConfig_FallsBackToDefault` — empty `secrets:` block ⇒ `Default()` chain.

8.3 `TestSecrets_BuildFromConfig_FileSource_PermsTooOpen` — file with 0644 ⇒ error.

8.4 `TestSecrets_KeyringMigration_BackCompat` — with no `secrets:` block, `REGATTA_HMAC_KEY` still resolves (back-compat).

8.5 `TestSecrets_YamlSourceOverridesEnv` — yaml `secrets.anthropic_api_key.source=env, name=MY_KEY` beats raw `ANTHROPIC_API_KEY` env.

8.6 CUE validation test: `regatta validate-config` rejects `source: vault` (out of schema enum).

8.7 Integration: full `serve` boot with `secrets:` block populated from a tempfile + env mix; assert each call site reads through the new fetcher (use a counting decorator).

## 9. Migration / rollout

9.1 Land CUE + loader + 5 call-site migrations in ONE PR (file-disjoint within `internal/secrets/` + 5 prod sites; per `feedback_dispatch_brief_only` keep single-implementer).

9.2 Back-compat: no `secrets:` block ⇒ behavior identical to today. Zero operator action required.

9.3 Deprecate raw `os.Getenv` for the five canonical keys via lint follow-up (out of this spec; F1 below).

9.4 Document migration in `docs/operator/configure.md` "Secrets" section: 3-line "if you use defaults, change nothing".

## 10. A+ scorecard

| Tier | Criterion | Evidence |
|---|---|---|
| (B1) | CUE schema validates new block | `contracts/schemas/regatta.v1.cue` + CUE-validation test (8.6) |
| (B2) | Back-compat preserved | TestSecrets_KeyringMigration_BackCompat (8.4) |
| (A1) | Five raw-`Getenv` sites migrated | `internal/gates/l4/adapter.go:62`, `internal/program/provider_anthropic.go:49`, `cmd/regatta/wire_keyring.go:40,48,52,56`, `cmd/regatta/audit.go:271,272`, `cmd/regatta/approval_decide.go:195,203` |
| (A2) | One operator-facing table replaces grep-the-codebase | `docs/operator/configure.md` Secrets section |
| (A+1) | Future backend additions trivial | `Fetcher` interface unchanged; `file_fetcher.go` is the only new adapter; Vault/AWS-SM = ~40-LOC each, behind same enum |
| (A+2) | `regatta doctor` companion preflight reads same block | depends on separate doctor spec (filed as companion issue) |

## 11. Risk + adversarial pass

- **Risk**: operator sets `source: file, path: /etc/regatta/X` with world-readable perms ⇒ secret leak. **Mitigate**: 0600 check in `NewFileFetcher`, fail-closed on read.
- **Risk**: yaml typo (`anthropic_api_keyy:`) silently ignored, fetcher falls back to env. **Mitigate**: CUE rejects unknown fields; `validate-config` flags typo at startup.
- **Risk**: scope creep — operator asks for Vault next. **Mitigate**: 4.6 deferred Phase-X; spec body cites trigger.
- **Risk**: doubling secret-fetch latency on every call. **Mitigate**: existing `secrets/cache.go` (SIGHUP-rotated atomic pointer) already amortizes; new loader plugs into same cache.

## 12. Out-of-band followups (file as separate issues if approved)

- F1: lint gate `scripts/check-no-raw-secret-env.sh` deprecating raw-`Getenv` for canonical keys (after one-month soak)
- F2: `secrets.Value.Wipe()` zeroize on rotation (hardening)
- F3: `regatta doctor` preflight command (separate spec — high-impact UX win from audit)
- F4: Cloud backends (Vault / AWS-SM) — Phase-X, external-customer trigger
