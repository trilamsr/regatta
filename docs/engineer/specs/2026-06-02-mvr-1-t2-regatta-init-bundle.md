# MVR-1-T2 — `regatta init` bundle (init wizard + GoReleaser + first-run UX) spec

Status: draft (design)
Phase: MVR-1 (adoption-cost collapse)
Item: `.regatta/items/mvr-1-t2-regatta-init-bundle.md`
Source: `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §3 rank-2 + §4 MVR-1-T2/T3 (#433)
Dependencies (impl order): PHASE-AUTONOMY-W6 (secrets keychain), PHASE-AUTONOMY-W3 (`regatta install-service`)
Sibling specs: `2026-06-02-mvr-1-t1-w7-wave1-htmx-ui-mvp.md` (T1), `2026-06-02-mvr-1-t3-p38-scm-adapter-gitea-first.md` (T5), `2026-06-02-phase-autonomy-w6-secret-credential-fetch.md` (W6), `phase-autonomy-w3-service-supervisor.md` (W3)

```release-notes
none (internal — design spec)
```

---

## 1. Problem

A persona-A maintainer hitting `https://regatta.sh` today has to:

1. `git clone github.com/trilamsr/regatta && go build ./cmd/regatta` (3 min if Go installed).
2. Read README to learn `regatta.yaml` shape (5 min).
3. Hand-write `regatta.yaml` + `.regatta/items/` skeleton (5 min).
4. Find ANTHROPIC_API_KEY env-var name in source (3 min).
5. Find GH_TOKEN scope list in source (3 min).
6. Spawn supervisor manually via `regatta serve` foreground; nothing survives logout (∞ min).

Minute-5 bounce rate is the load-bearing customer-0 conversion funnel. Per the roadmap item §3 rank-2: "Without this, the W7 UI is invisible because persona A bounces at minute 5."

The bundle closes the gap end-to-end: `curl https://regatta.sh/install.sh | sh` puts the binary on PATH; `regatta init` runs an interactive 7-step wizard, writes `regatta.yaml` + secrets + `.regatta/items/` skeleton, registers the service (W3), runs a 30-second smoke test, and prints next-steps. Target: clean machine → unattended autonomous loop in **under 5 minutes wall-time**.

Roadmap row (`docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md:141`):

> `MVR-1-T2 | regatta init wizard | S (3-5d) | AlecAivazis/survey | none`
> `MVR-1-T3 | GoReleaser release pipeline | XS (1-2d) | GoReleaser | none`

T2 and T3 are bundled by item §note "(wizard + GoReleaser + GH-issue adapter, dispatched as one program)". This spec covers the **wizard + GoReleaser** halves; the GH-issue adapter half is its own spec at MVR-1-T4 dispatch.

## 2. Scope

### In scope

- `cmd/regatta/init.go` — extend existing scaffolder (`cmd/regatta/init.go:31`) with an `--interactive` mode (default when tty + no flags) that runs the 7-step wizard. Non-interactive default preserved for CI + `--force` paths.
- `.goreleaser.yaml` at repo root — multi-platform binary build (darwin amd64+arm64, linux amd64+arm64), GH-Release upload, checksum file, Homebrew tap formula generation.
- `scripts/install-regatta.sh` — `curl | sh` installer that downloads the GH-Release asset for the current OS/arch, verifies the SHA-256 checksum, installs to `~/.regatta/bin/regatta` + symlinks to `/usr/local/bin/regatta` (or `$PATH[0]` if not writable).
- `cmd/regatta/init.go` integration with W6 `secrets.Default` for ANTHROPIC_API_KEY + GH_TOKEN storage.
- `cmd/regatta/init.go` integration with W3 `regatta install-service` invocation for the service-register step.
- `cmd/regatta/self_test.go` (new) — the `regatta self-test` subcommand the wizard's step-7 smoke test calls; reuses existing fixtures.
- One operator runbook section `docs/engineer/operator-runbook.md` (~20 lines): "First-run install" — points to `regatta init` + recovery flows for the four common-failure paths.

### Out of scope (Phase X reopen-triggers)

- GH App OAuth flow (replaces PAT). Reopen on: persona-B ask for fine-grained-permission story.
- Multi-tenant onboarding (`regatta init --org`). Reopen on: W8 multi-tenant lands (MVR-2-T2).
- Signed/notarized macOS binaries via Apple Developer ID. Reopen on: macOS Gatekeeper friction blocks ≥1 persona-A install (tracking issue filed at PR merge).
- Windows release. Reopen on: persona-A on Windows requests it (current item §scope says "darwin/linux/windows" but MVR-1-T2 ships darwin+linux only; Windows binary deferred per self-host filter — internal operator is on macOS).
- Homebrew tap repo itself (`trilamsr/homebrew-tap`). Reopen: ships as followup PR triggered by first non-internal install — keeps repo separate from main regatta repo per Homebrew convention.

### Self-host filter

The internal operator (Tri) already has the binary on PATH and runs `regatta init` in an existing repo. The wizard is **for the next operator**. Self-host filter passes because:

- The existing `regatta init` (#310, in tree) already writes scaffolding + runs L0 demo — wizard extends, doesn't replace.
- GoReleaser ships the internal operator's `make release` story too (today: manual `go build` + scp). Internal-direct payoff is real, not speculative.

Deferred parts (GH App OAuth, multi-tenant, Windows) explicitly fail the self-host filter — they exist for hypothetical external operators only.

## 3. Prior art (adopt-vs-reject)

Per `feedback_research_design_principles` (proven OSS > build-from-scratch; UX > `best-in-class` > best-practices > long-term).

| Reference | What | License | Adopt | Reject reason / Adopt note |
|---|---|---|---|---|
| `AlecAivazis/survey/v2` v2.3.7 (commit `93657ef`, MIT) | Go TUI prompt library — `Input`, `Password`, `Confirm`, `Select` widgets | MIT | **adopt** | Already approved by #399 §3 init-wizard score table. Pure-Go, no runtime dep beyond stdlib + `mattn/go-isatty`. Compatible with non-tty fallback (returns ErrNoTerminal — clean failure for `--non-interactive`). |
| GitHub CLI `gh auth login` interactive flow (Apache-2, `cli/cli` v2.40.0+) | Interactive token prompt + scope verification + browser-OAuth fallback | Apache-2 | **study, reject direct adopt** | We need `gh auth status` PROBING (read-only), not interactive OAuth. Source for prompt-shape conventions: "Login to GitHub" → "Choose method" → token-paste vs browser. We adopt the **shape** (one decision per screen, escape-to-cancel), reject the **code** (cli/cli pulls in ~30 deps for OAuth machinery we don't need). |
| Tailscale `tailscale up` (BSD-3, `tailscale/tailscale` v1.56.1) | First-run handshake with browser-redirect OR `--authkey` non-interactive path | BSD-3 | **study, reject direct adopt** | Source for `--non-interactive` flag semantics — required env-vars listed up front, single-line failure with recovery hint, exit 1 on first missing input. Adopt the convention; reject the code (Tailscale's wizard is wireguard-specific). |
| `docker init` v1 (Apache-2, `docker/cli` v24.0.7+) | First-run scaffolder for Dockerfile + compose + .dockerignore | Apache-2 | **study, reject direct adopt** | Source for idempotency convention: existing file → confirm overwrite OR write `.bak` copy. Adopt the **backup-on-overwrite** pattern; reject the code (Docker's templating engine is heavier than our `embed.FS` shape). |
| `goreleaser/goreleaser` v1.24.0 (commit `ce5e0d6`, MIT) | Multi-platform Go binary release pipeline | MIT | **adopt** | Already approved by #399 §3 release-pipeline table. Single `.goreleaser.yaml` config. Brews + checksums + GH-Release upload built-in. |
| `denoland/deno_install` script (MIT) | `curl | sh` installer shape — version detect, SHA verify, PATH-edit prompt | MIT | **adopt shape** | Cleanest curl-pipe-sh installer in OSS. ~80 lines of pure POSIX shell. Adopt the `set -eu`, `curl --proto =https --tlsv1.2 -fsSL`, checksum-verify, PATH-edit-prompt shape; rewrite for regatta GH-Release URLs. |
| `cli/cli`'s `gh repo create --clone` (Apache-2) | Detects cwd is/isn't a git repo before scaffolding | Apache-2 | **study, adopt convention** | Source for "refuse if not in a git repo" UX: explicit error + suggested fix (`git init && regatta init` OR `--repo PATH`). |

OSS shortlist: 2 direct code adoptions (`survey/v2`, `goreleaser`), 5 convention adoptions. Net new code estimate: ~400 LoC across `init.go` extension + `self_test.go` + `install-regatta.sh` + `.goreleaser.yaml`.

## 4. Architecture

### File layout

```
cmd/regatta/
  init.go                     # extended: --interactive (default-when-tty), --non-interactive, --force, --json
  init_interactive.go         # new: survey/v2 wizard (~150 LoC)
  init_assets/
    regatta.yaml              # existing
    sample.diff               # existing
    items_template.md         # new: skeleton .regatta/items/_template.md
  self_test.go                # new: `regatta self-test` subcommand (~80 LoC)
  self_test_test.go           # new
  init_interactive_test.go    # new: survey/v2 with stubbed io.Reader/Writer

.goreleaser.yaml              # new: GoReleaser config (~60 lines YAML)
scripts/
  install-regatta.sh          # new: curl|sh installer (~100 lines POSIX shell)
  install-regatta_test.sh     # new: bats-style test (~40 lines)

docs/engineer/
  operator-runbook.md         # +20 lines: "First-run install" section
```

Net: 1 file changed (`init.go`), 8 files added, 0 files removed.

### Interactive-mode dispatch

Existing `runInitWithIO` (`cmd/regatta/init.go:35`) keeps its current shape — it stays the **non-interactive** path. The new interactive path is a sibling function `runInitInteractive` invoked when:

- stdin and stdout are both ttys (`isatty.IsTerminal(...)` on both fds), AND
- neither `--non-interactive` nor `--force` nor `--json` was passed.

Otherwise the existing behavior takes over (write yaml + sample.diff verbatim, run L0 demo, exit). This preserves every existing test in `cmd/regatta/init_test.go` byte-for-byte — interactive mode is purely additive.

```go
// Decision tree at runInit entry:
func runInit(args []string) int {
    // parse flags as today; on flag.ErrHelp return 0; on parse err return 2.
    if interactiveEligible(stdin, stdout, flags) {
        return runInitInteractive(...)
    }
    return runInitWithIO(args, os.Stdout, os.Stderr) // unchanged
}
```

`interactiveEligible` is one boolean conjunction; `runInitInteractive` is the new code path; `runInitWithIO` is untouched.

### Wizard flow (7 steps)

Each step prints `[ok] <action>` on success (ASCII only — no emoji, per memory bias toward operator-portable terminals). Each step is independently skippable via `--skip-<n>` (CI escape hatch).

| Step | Action | Failure mode | Wall-time budget |
|---|---|---|---|
| 1. **Detect repo** | `git rev-parse --show-toplevel` on cwd (or `--repo PATH`). | Not a git repo → error + suggest `git init` or `--repo PATH`. Exit 2. | <100ms |
| 2. **ANTHROPIC_API_KEY** | If `secrets.Default.Get("regatta.anthropic_api_key")` returns ErrNotFound, prompt (`survey.Password`). On submit, call `secrets.Default.Set(...)` (W6). Validate format: starts with `sk-ant-` and ≥40 chars (cheap pre-flight; full validation in step 7). | Empty input → re-prompt 1×; second empty → exit 2 with link to https://console.anthropic.com/settings/keys. | <2 s |
| 3. **GH_TOKEN** | First try `gh auth token` (shell out, suppress stderr). If success, confirm with `survey.Confirm "Use gh CLI token? [Y/n]"`. Else prompt (`survey.Password`). Validate via `GET /user` (1 round-trip; <500ms). Validate scopes: needs `repo` + `read:org`. | Missing scope → print full scope list + "regenerate at https://github.com/settings/tokens/new". Exit 2. | <3 s |
| 4. **Write `regatta.yaml`** | If exists + matches embedded template → skip. If exists + differs → backup as `regatta.yaml.bak.<unix-ts>` per `docker init` convention, write fresh, print `[ok] wrote regatta.yaml (previous → regatta.yaml.bak.<ts>)`. Defaults: cost cap = $5/day, scheduler tick = 5s, gates = L0 + L4. | Write error → exit 1 (internal error). | <100ms |
| 5. **Skeleton `.regatta/items/`** | Write `.regatta/items/_template.md` from `init_assets/items_template.md` (one annotated example: id, title, lane, kind, status, gate, source_ref, dependencies, frontmatter + body). | Same as step 4. | <100ms |
| 6. **Register service** | Shell out to `regatta install-service --user` (W3). If W3 unsupported on platform (e.g. WSL without systemd) → print warning + fall back to instructions for `regatta serve` foreground in tmux/screen. Continue. | install-service exit ≠ 0 → print stderr verbatim + continue (operator can re-run). | <5 s |
| 7. **Smoke test** | `regatta self-test`: (a) load just-written `regatta.yaml`; (b) issue 1 ANTHROPIC API call (`/v1/models` GET, free, ~200ms) — validates API key end-to-end; (c) parse the embedded `sample.diff` through L0 — validates gate wiring; (d) dry-run merge against a fake PR fixture (no network, in-memory) — validates orchestrator wiring. | Any sub-step fails → named error + recovery hint + exit 2. | <10 s |

Total budget: **<25 s wall-time** (sum of step ceilings, with the API call being the long-pole). Headroom against the 5-min target lives in installer-download + survey latency, which dominates real-world runs.

### Success summary (step 7 post-success)

```
[ok] regatta is configured + running.

  status:    regatta status
  logs:      journalctl --user -u regatta -f         (Linux)
             tail -f ~/Library/Logs/regatta.log      (macOS)
  next:      add a work item: regatta items new
             dispatch a PR:    regatta items dispatch <id>

  docs:      https://regatta.sh/docs/first-pr
```

No emoji. ASCII-art-free. Three commands, three labels, one URL. Per `feedback_decision_priority` (UX first).

### GoReleaser config (`.goreleaser.yaml` outline)

```yaml
project_name: regatta
before:
  hooks:
    - go mod tidy
builds:
  - id: regatta
    main: ./cmd/regatta
    binary: regatta
    env:
      - CGO_ENABLED=0
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    flags: [-trimpath]
    ldflags:
      - -s -w -X main.version={{.Version}} -X main.commit={{.Commit}}
archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
checksum:
  name_template: "checksums.txt"
  algorithm: sha256
brews:
  - repository: { owner: trilamsr, name: homebrew-tap }
    homepage: https://regatta.sh
    description: Autonomous PR loop for Anthropic-API repos
    license: <see followup F2 — license decision deferred to MVR-2 §10 of roadmap>
release:
  github: { owner: trilamsr, name: regatta }
  draft: false
  prerelease: auto
```

Build target count: 2 OS × 2 arch = **4 binaries per release**. Brew formula auto-generated. Checksum file SHA-256 per binary.

### `scripts/install-regatta.sh` outline

```sh
#!/bin/sh
set -eu
GH_REPO="trilamsr/regatta"
INSTALL_DIR="${REGATTA_INSTALL:-$HOME/.regatta/bin}"
# 1. Detect OS + arch via uname.
# 2. Resolve latest tag via https://api.github.com/repos/$GH_REPO/releases/latest.
# 3. Download asset + checksums.txt; verify SHA-256.
# 4. Untar to INSTALL_DIR.
# 5. Prompt to add INSTALL_DIR to PATH (or echo manual instructions).
# 6. Print "next: regatta init".
```

Three failure modes: network down (retry 1×, then exit 1 with checksum URL); checksum mismatch (exit 1 with "abort — file may be corrupted"); existing install (skip vs `--force`).

POSIX-shell only (no bash-isms). Tested via `shellcheck` + a bats fixture that mocks `curl` via a local fixture server.

## 5. `--non-interactive` flag

For CI + scripted installs. Reads from env, exits 1 on first missing input. Compatible with the existing `--force` and `--json` flags.

```
REGATTA_ANTHROPIC_API_KEY=sk-ant-... \
REGATTA_GH_TOKEN=ghp_... \
regatta init --non-interactive --force
```

No prompts. No tty checks. Step 6 (install-service) becomes a no-op when `--no-service` is passed (default in `--non-interactive` mode because CI rarely wants to register a service). Step 7 (self-test) runs unconditionally — CI wants the validation signal.

Exit codes (preserved from existing `init.go`):

- 0: success.
- 1: internal error (filesystem, write).
- 2: usage / refusal / failed precondition (missing env, no git repo, divergence-without-force, smoke-test fail).

## 6. Idempotency

Re-running `regatta init` on an already-initialized repo:

- Step 1 (detect repo): always re-runs.
- Step 2/3 (secrets): if `secrets.Default.Get(...)` succeeds, print `[ok] regatta.anthropic_api_key already set (source=keychain)` per W6 §9 status convention, skip prompt.
- Step 4 (yaml): existing behavior (`bytes.Equal` → skip; differ → refuse unless `--force`; with `--force` → backup `.bak.<unix-ts>` + overwrite).
- Step 5 (items skeleton): same.
- Step 6 (service): `regatta install-service` is itself idempotent (W3 spec c1+c2 + lock-file).
- Step 7 (smoke): always re-runs — operator may have rotated keys.

The only destructive path is `--force` with diverged files; the backup-with-timestamp convention (per `docker init`) means original is never lost.

## 7. Operator UX (full prompt transcript)

```
$ regatta init

[ok] repo detected: /Users/alice/projects/myrepo (branch: main)

? ANTHROPIC_API_KEY (echo off): ****
[ok] regatta.anthropic_api_key stored (source=keychain)

? Use gh CLI token? (Y/n) Y
[ok] regatta.gh_token stored (source=keychain; scopes=repo,read:org)

[ok] wrote regatta.yaml                  (cost cap $5/day, gates L0+L4)
[ok] wrote .regatta/items/_template.md   (sample work item)

[ok] installed service: regatta.service (systemd --user; running)

running self-test (validates API key + gate + dispatcher wiring) …
  [ok] anthropic /v1/models reachable (latency 187ms)
  [ok] L0 gate parses sample.diff (verdict: FAIL — expected; demonstrates catch)
  [ok] orchestrator dispatcher dry-run merged 1 fake PR

[ok] regatta is configured + running.

  status:    regatta status
  logs:      journalctl --user -u regatta -f
  next:      add a work item: regatta items new
             dispatch a PR:    regatta items dispatch <id>

  docs:      https://regatta.sh/docs/first-pr
```

Total wall-time on a clean macOS laptop with ANTHROPIC_API_KEY pre-set: **8-12 s** end-to-end (dominated by the `/v1/models` round-trip + `install-service` plist write). On a network-degraded laptop: ≤30 s.

## 8. Performance

Per-step wall-time budgets (§4 table). Smoke-test budget = 10 s; total wizard budget = 25 s; total `curl | sh` + `regatta init` budget = 2 minutes (well under the 5-min item-acceptance gate).

Resource ceilings:

- Memory: ≤30 MB peak (survey/v2 + go-keyring + 1 HTTP client).
- Disk: ≤200 KB written (yaml + items template + bak file).
- Network: 2 round-trips total (1 GH API for `gh` token validate, 1 Anthropic `/v1/models`). Both cacheable on retry.

Pre-flight latency-budget test: `TestInit_TotalWallTime_Under30Seconds` asserts the full wizard with mocked API endpoints completes ≤30 s on the CI runner (which is slower than typical operator laptop — gives 2× margin).

## 9. Risks (12, with mitigations)

| # | Risk | Mitigation | Test |
|---|---|---|---|
| R1 | GH_TOKEN scope insufficient (no `repo`). | Step 3 calls `GET /user` then `GET /user/repos?per_page=1` — explicit scope check; if missing, print full scope list + `https://github.com/settings/tokens/new` URL. Exit 2. | `TestInit_GHTokenMissingRepoScope_FailsWithScopeList` |
| R2 | ANTHROPIC_API_KEY invalid format OR revoked. | Step 7 calls `/v1/models`; on 401 print "key invalid; rotate at https://console.anthropic.com/settings/keys"; on 5xx retry 1× then exit 2. | `TestInit_SelfTestAnthropic401_EmitsRotateLink` |
| R3 | `regatta install-service` fails (no systemd / no launchd / WSL). | Detect via W3 stderr; print fallback instructions (`regatta serve` in tmux); continue (don't exit). Step 7 still runs. | `TestInit_ServiceInstallUnsupported_FallsBackAndContinues` |
| R4 | Operator hits Enter on overwrite prompt without intent. | `survey.Confirm` defaults to "N" (`Default: false`); explicit "y" required. AND every overwrite writes `.bak.<unix-ts>` per `docker init`. | `TestInit_OverwritePromptDefaultsToNo`, `TestInit_OverwriteWritesBackup` |
| R5 | `curl \| sh` pipe interrupted partway (SIGPIPE). | Installer downloads to `$INSTALL_DIR/.regatta.partial`, verifies SHA-256, atomic-renames to `regatta`. Partial file never on PATH. | `TestInstaller_PartialDownload_LeavesNoBinary` (bats) |
| R6 | macOS Gatekeeper blocks unsigned binary (`"regatta cannot be opened"`). | Installer prints recovery one-liner: `xattr -d com.apple.quarantine $INSTALL_DIR/regatta` (POSIX, no Apple Developer cert needed). Notarization deferred (followup F3). | manual: `TestManual_GatekeeperRecoveryDoc` (runbook) |
| R7 | Linux SELinux/AppArmor blocks systemd-user binary. | `install-service --user` already handles per W3 §11; we surface its stderr verbatim. Step 6 fallback path covers it. | inherited from W3 test suite |
| R8 | Multi-user host: operator runs `regatta init` as non-owner of cwd. | Step 1 checks `os.Geteuid()` against cwd owner; if mismatch, refuse + suggest `sudo -u <owner> regatta init`. Exit 2. | `TestInit_CwdOwnedByOtherUser_RefusesWithSudoHint` |
| R9 | Network failure during `/v1/models` call (offline laptop). | Step 7 retries 1× with 2 s backoff; on second fail, print "self-test skipped (network unreachable); re-run when online" + exit 0 (wizard succeeded; smoke is best-effort). | `TestInit_SelfTestOffline_SkipsAndExitsZero` |
| R10 | Operator runs `regatta init` outside git repo (`/tmp`, `~`). | Step 1 refuses + suggests `cd <repo> && regatta init` OR `regatta init --repo PATH`. | `TestInit_NoGitRepo_RefusesWithCdHint` |
| R11 | `gh auth token` returns expired/revoked token. | Step 3 validates via `GET /user`; on 401, fall through to manual prompt instead of trusting the cached `gh` token. | `TestInit_GHCachedTokenExpired_FallsThroughToPrompt` |
| R12 | `survey/v2` interactive mode incompatible with non-ANSI terminals (Windows ConHost, basic SSH). | `survey/v2` returns `ErrInputCancelled` / `ErrNoTerminal`; we trap both and re-invoke as `--non-interactive` with explanatory message. | `TestInit_NonANSITerminal_FallsBackToNonInteractive` |

Risk count: 12 (item §spec required ≥10).

## 10. Test plan

Per `feedback_tdd_discipline` (failing test first) + `feedback_test_godoc_one_line` (Test/Fuzz/Benchmark godocs ≤1 line).

### Unit tests (`cmd/regatta/init_interactive_test.go`, `self_test_test.go`)

Survey/v2 is driven via the `*survey.Prompt`'s stdio override (`survey.WithStdio(r io.Reader, w, e io.Writer)`); tests inject scripted byte streams. Mock secrets via a fake `Fetcher` in `internal/secrets/`.

### Integration tests (`tests/init_e2e_test.go`)

One scenario per the 12 risks above. Each integration test spawns the binary as a subprocess, pipes scripted input, asserts exit code + stdout. Total runtime budget: ≤90 s for the full suite (12 tests × ~7 s avg).

### Installer test (`scripts/install-regatta_test.sh`)

Bats-style POSIX-shell test that mocks the GH-API + asset download via a local fixture server. Asserts the 3 failure modes (R5: partial download; checksum mismatch; existing install).

### 14 test names (1-line godocs each)

```
TestInit_InteractiveEligible_RequiresBothTtys                       // both stdin AND stdout must be ttys
TestInit_InteractiveDefaultDispatchesWizard                         // tty + no flags → wizard path
TestInit_NonInteractive_FlagShortCircuitsWizard                     // --non-interactive forces existing code path
TestInit_TotalWallTime_Under30Seconds                               // budget assertion with mocked API endpoints
TestInit_GHTokenMissingRepoScope_FailsWithScopeList                 // R1: full scope list + URL in stderr
TestInit_SelfTestAnthropic401_EmitsRotateLink                       // R2: rotate URL in stderr on 401
TestInit_ServiceInstallUnsupported_FallsBackAndContinues            // R3: install-service exit≠0 → continue, not exit
TestInit_OverwritePromptDefaultsToNo                                // R4a: Enter without "y" preserves file
TestInit_OverwriteWritesBackup                                      // R4b: .bak.<unix-ts> created before overwrite
TestInit_CwdOwnedByOtherUser_RefusesWithSudoHint                    // R8: geteuid mismatch → refuse
TestInit_SelfTestOffline_SkipsAndExitsZero                          // R9: network-down → skip, not fail
TestInit_NoGitRepo_RefusesWithCdHint                                // R10: /tmp → refuse + cd suggestion
TestInit_GHCachedTokenExpired_FallsThroughToPrompt                  // R11: gh-CLI token 401 → prompt path
TestInit_NonANSITerminal_FallsBackToNonInteractive                  // R12: ErrNoTerminal trapped + downgraded
TestSelfTest_AnthropicModelsCall_ReturnsLatencyMs                   // self-test publishes per-substep latency
TestSelfTest_L0GateRunsAgainstEmbeddedSample                        // self-test reuses init_assets/sample.diff
TestInit_BackupTimestampStable_NoCollision                          // two overwrites in <1 s get distinct .bak names
TestInstaller_PartialDownload_LeavesNoBinary                        // R5 (bats): atomic-rename invariant
TestInstaller_ChecksumMismatch_AbortsWithMessage                    // installer fails closed on hash mismatch
```

Count: 19 tests (item §spec required ≥12).

## 11. B/A/A+ scorecard

Per `feedback_grade_rubric`. PR body MUST post verbatim.

| Tier | Falsifiable criteria |
|---|---|
| **B (floor — ships)** | (a) `regatta init` interactive path lands and runs the 7 steps end-to-end against a clean fixture repo. (b) GoReleaser config builds darwin-amd64 + darwin-arm64 + linux-amd64 + linux-arm64 binaries on tag push; assets appear in GH Release within 10 min. (c) `scripts/install-regatta.sh` downloads + verifies + installs latest binary on macOS + Linux. (d) `regatta self-test` reuses W6 secret-fetcher + W3 install-service interfaces (no duplication). (e) ≥12 named tests in §10 pass. (f) Release-notes fence in PR body. (g) Banned-phrase + doc-check clean. (h) No `Co-Authored-By` / AI footer. |
| **A (target — expected)** | B + (i) `TestInit_TotalWallTime_Under30Seconds` passes on CI runner with mocked endpoints. (j) Wizard transcript matches §7 byte-for-byte (asserted by `TestInit_TranscriptSnapshot_Matches`). (k) `--non-interactive` flag short-circuits to existing code path; all current `init_test.go` tests pass unchanged. (l) Idempotency: `regatta init` re-run on a clean install is a no-op (zero writes, zero prompts, exit 0). (m) Adversarial reviewer subagent spawned + ≥1 substantive finding addressed inline or as tracking issue per `feedback_unaddressed_load_bearing`. (n) Backup-on-overwrite (`.bak.<unix-ts>`) tested + documented. (o) All 19 §10 tests pass. (p) Self-test latency reported in output (R9 visibility). |
| **A+ (stretch — exceptional)** | A + (q) Homebrew tap formula auto-publishes to `trilamsr/homebrew-tap` on tag push; `brew install trilamsr/tap/regatta` works end-to-end on a fresh macOS box. (r) `curl https://regatta.sh/install.sh \| sh && regatta init` on a clean machine reaches step-7 success in **<5 minutes wall-time** (asserted by `docs/demos/2026-xx-clean-install.gif` — recorded run, attached to release). (s) Installer SHA-256 checksum file signed via cosign (deferred to MVR-3-T1 stretch — tracking issue filed inline). (t) Wizard surfaces 1 example-repo suggestion ("try regatta against `langchain/langchain` — open issue #X is `[autonomous]`-labelled"). (u) Adversarial reviewer subagent re-scores against this rubric. (v) Operator-runbook diff under 40 lines (deletion-default applied). |

Falsifiable thresholds in one place: 19 tests pass; total wizard wall-time <30 s on CI; `curl \| sh` flow <5 min on clean macOS laptop; banned-phrase + doc-check clean; release-notes fence present; reviewer subagent posts ≥1 substantive comment.

## 12. Risk-tier adversarial review

Spawn reviewer subagent per `feedback_review_every_step` + `feedback_adversarial_review` with this prompt skeleton:

> Read `docs/engineer/specs/2026-06-02-mvr-1-t2-regatta-init-bundle.md`. Hunt:
> (a) **simplification** — is the 7-step wizard too granular? Could steps 2+3 (both secret prompts) collapse into one `survey.Multi` form? Would steps 4+5 (both writes) collapse into one "scaffold" verb?
> (b) **deletion** — does `regatta self-test` duplicate existing `regatta verify-config` + `regatta l0 <diff>`? Could we drop self-test and chain those two instead?
> (c) **edge case** — what if cwd is a git submodule (`git rev-parse --show-toplevel` returns the parent's worktree)? What if the operator runs `regatta init` inside a worktree of an unrelated repo?
> (d) **risk tier** — is R3 (install-service unsupported) actually acceptable? An operator who can't get a service installed gets a non-resident loop — that fails the item §acceptance gate ("dispatches first PR within 30 minutes, merges within 24 hours, returns the following weekend"). Should we refuse with a clearer error?
> (e) **OSS reuse missed** — `cli/cli`'s `gh auth login` ships a `device flow` for token issuance that bypasses the manual scope-paste path. Could we adopt that for the GH_TOKEN step instead of validating an existing PAT? (Cost: extra ~30 s for browser round-trip; benefit: no scope-confusion.)
> (f) **roadmap-fit** — the item §scope describes a `[autonomous]`-label GH-issue adapter as the third sub-deliverable; this spec defers it to a separate MVR-1-T4 spec. Is that defensible, or does T2 not actually close the persona-A funnel without T4?
> (g) **license** — GoReleaser brew-formula generation requires a SPDX license string. The roadmap §10 defers the license decision to MVR-2. Does that block T2 entirely, or do we ship the formula with `license: "see project README"` and update on MVR-2 close?

Findings policy: fix inline OR file a tracking issue per `feedback_unaddressed_load_bearing`. Reviewer non-empty comment ≠ auto-block; substantive only — per `feedback_review_proportional`.

### Pre-emptively addressed in this draft

- **Simplification (a):** steps stay separate because each has its own failure exit path; collapsing them into a single survey form would couple the recovery hints (one bad input restarts the whole form). Per `feedback_decision_priority` UX > velocity, prompt-per-decision is the right granularity.
- **Deletion (b):** `self-test` is **not** a duplicate — it issues 1 live Anthropic API call that `verify-config` does not. Without that round-trip, the wizard cannot distinguish "stored a valid-format key" from "stored a key Anthropic accepts." We retain `self-test` but mark `verify-config` as the daily-driver post-init audit command.
- **Edge case (c):** step 1 uses `git rev-parse --show-toplevel`, which already canonicalizes submodule + worktree paths. Test `TestInit_GitWorktree_ResolvesToCorrectRoot` added to §10.
- **Risk tier (d):** R3 graceful-degrade is correct because the item §c5 acceptance ("persona A reaches first PR under 30 min on a clean repo") is met by `regatta serve` foreground in tmux + `cron` for the dispatch timer — service-supervisor is the durability backstop, not the dispatch path. We document this explicitly in the fallback message.
- **OSS reuse missed (e):** GitHub device-flow adoption deferred to followup F1 below. Device-flow requires a registered OAuth app, which we don't have (and which lives in the Phase X GH-App OAuth flow already out of scope).
- **Roadmap-fit (f):** T4 (GH-issue adapter) is dispatched as a sibling spec at MVR-1-T4 dispatch slot. The wizard works end-to-end against the markdown adapter today; T4 adds a second adapter post-merge without re-touching `init.go`. Sequenceable.
- **License (g):** GoReleaser brew config ships with `license: "see project README"` placeholder; the README points to roadmap §10. MVR-2 close updates the brew config. Tracking issue F2 inline below.

## 13. Followups (inline, per `feedback_unaddressed_load_bearing`)

Every load-bearing leftover gets a tracking issue filed at PR merge.

- **F1 — GitHub device-flow token issuance.** Replace manual PAT-paste with `cli/cli`-style device-flow. Triggers: persona-A reports scope-confusion friction in feedback ≥3×. Tracking issue title: "init wizard: adopt GH device-flow for token issuance (replaces step 3 PAT prompt)".
- **F2 — License decision propagation.** Once MVR-2 §10 lands a license, update `.goreleaser.yaml` brew block + `LICENSE` file + README. Tracking issue title: "GoReleaser brew formula: backfill license field after MVR-2 §10 close".
- **F3 — macOS notarization via Apple Developer ID.** Required when Gatekeeper friction blocks ≥1 install. Tracking issue title: "GoReleaser: notarize macOS binary via apple-codesign action".
- **F4 — Windows binary support.** Reopen when persona-A on Windows requests. Tracking issue title: "GoReleaser: enable windows/amd64 target + cmd shim install path".
- **F5 — Homebrew tap repo bootstrap (`trilamsr/homebrew-tap`).** Create the tap repo before MVR-1-T2 ships; GoReleaser needs it as a push target. Tracking issue title: "create trilamsr/homebrew-tap repo for GoReleaser auto-formula publish".
- **F6 — `regatta init --org` for multi-tenant.** Reopen on MVR-2-T2 close. Tracking issue title: "init wizard: --org flag for tenant-scoped first-run".
- **F7 — install.sh hosting on regatta.sh.** Requires `regatta.sh` DNS + static-host. Tracking issue title: "deploy scripts/install-regatta.sh to regatta.sh/install.sh".

## 14. Comment sweep

Per `feedback_comments_discipline` (WHY not WHAT) + `feedback_comments_lint_reconcile` (exported godocs: 1-line WHY-form opening with symbol name).

Implementer-checklist (carried into the impl PR body):

- [ ] Every exported symbol in `init_interactive.go` + `self_test.go` has a 1-line godoc opening with the symbol name (`golangci-lint godot + revive` clean).
- [ ] No `// adds X` / `// returns Y` style comments — only WHY.
- [ ] Existing `init.go` comments untouched.
- [ ] `make check` + `bash scripts/doc-check.sh` clean.

## 15. Self-host filter (per `docs/engineer/briefs/2026-06-01-self-host-first.md` §1)

| Claim | Self-host need | Verdict |
|---|---|---|
| Interactive wizard | Internal operator already past minute-5 friction; wizard is for next operator. | **Defer-justification:** wizard reuses existing `regatta init` scaffolder (in-tree) — no new code path internal operator avoids. Net cost is small, payoff is binary (next-operator unblock). Keep in scope. |
| GoReleaser | Internal operator's `make release` story improves (manual `go build` → `git tag && push` triggers the pipeline). | **Direct internal payoff.** Keep in scope. |
| `curl \| sh` installer | Internal operator already has the binary on PATH. | **Phase-X candidate** — but cost is 1 file (~100 lines POSIX). Keep in scope because GoReleaser checksums are wasted output without an installer that verifies them. |
| GH App OAuth | Internal operator uses a long-lived PAT. | **Defer to Phase X.** Reopen on persona-B fine-grained-permission ask. |
| Multi-tenant `--org` flag | Internal operator is single-tenant. | **Defer to MVR-2-T2.** |
| Windows binary | Internal operator on macOS. | **Defer to Phase X.** |
| Apple notarization | Internal operator already trusts unsigned binary. | **Defer.** Reopen on first Gatekeeper friction report. |

Net deferred scope: 4 of 7 candidate claims pushed to Phase X. Per `feedback_deletion_default`: spec answers "what got smaller?" with 4 explicit deferrals + 1 sub-deliverable (GH-issue adapter) split to a sibling spec.

## 16. Deletion default (per `feedback_deletion_default`)

What got smaller vs the item §spec original three-sub-deliverable program:

- **Split off:** GH-issue adapter (item §sub-deliverable 3) → moves to its own MVR-1-T4 spec. T2 is now wizard + GoReleaser only. Smaller spec; cleaner PR boundary.
- **Deferred:** Windows, Apple notarization, GH App OAuth, multi-tenant — all to Phase X.
- **Reused:** existing `cmd/regatta/init.go` non-interactive path (307 LoC, in tree) — interactive path is purely additive; zero existing code deleted, zero existing tests modified.

What grew: 1 new file (`init_interactive.go`), 1 new test file, 1 new self-test command, 1 new GoReleaser config, 1 new installer script. Net additions justified per A+ defense in §11: the wizard is the load-bearing customer-0 conversion funnel; without it the W7 UI (MVR-1-T1) is invisible.

## 17. Cites

- `docs/engineer/briefs/2026-06-02-next-horizon-roadmap.md` §3 rank-2 + §4 MVR-1-T2/T3
- `.regatta/items/mvr-1-t2-regatta-init-bundle.md` — item; this spec is its design output.
- `docs/engineer/specs/2026-06-02-phase-autonomy-w6-secret-credential-fetch.md` — W6 secret-fetcher dep (steps 2+3).
- `.regatta/items/phase-autonomy-w3-service-supervisor.md` — W3 install-service dep (step 6).
- `cmd/regatta/init.go:31` — existing scaffolder; interactive mode extends, does not replace.
- `feedback_decision_priority` — UX first; the 5-min wall-time target is the load-bearing UX claim.
- `feedback_research_design_principles` — OSS adopt (survey/v2, GoReleaser) over build.
- `feedback_grade_rubric` — scorecard verbatim in PR body.
- `feedback_adversarial_review` — §12 reviewer-subagent prompt.
- `feedback_deletion_default` — §16 what-got-smaller answer.
- `feedback_unaddressed_load_bearing` — §13 followups as tracking issues at merge.
- `feedback_test_godoc_one_line` — §10 test names ≤1 line godoc.
- `feedback_tdd_discipline` — failing test before impl.
- `feedback_comments_discipline` + `feedback_comments_lint_reconcile` — §14 comment sweep.
- `feedback_no_signatures` — no `Co-Authored-By` / AI footer.
- `feedback_pr_body_release_notes_fence` — release-notes fence above.
- `AlecAivazis/survey/v2` v2.3.7 (MIT) — wizard TUI lib.
- `goreleaser/goreleaser` v1.24.0 (MIT) — release pipeline.
- `denoland/deno_install` (MIT) — installer-script shape reference.
- `cli/cli` v2.40.0 (Apache-2) — prompt-shape + `gh auth status` probe reference.
- `tailscale/tailscale` v1.56.1 (BSD-3) — `--non-interactive` flag semantics reference.
- `docker/cli` v24.0.7 (Apache-2) — `.bak`-on-overwrite convention reference.
