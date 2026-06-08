# Testing conventions

Reader: anyone adding a Go test under `internal/`, `tests/`, or `cmd/`.
Read time: 3 minutes.
Expires when: a fourth build-tag namespace lands.

## Tag namespaces

The repo has three build-tagged test modes beyond the default unit run. Each names a distinct cost surface; pick by what the test consumes.

| Tag | Tests | Cost surface | Runner |
|---|---|---|---|
| (no tag) | unit tests | CPU + in-process fixtures | `make check` (every PR) |
| `integration_gh` | single-package real-binary tests (e.g. `gh --version` regex against real binary) | one external binary, no network writes | `make ci-integration` (nightly) |
| `e2e` | full-loop tests against a real GitHub fixture repo + real claude subagent | Anthropic tokens + GitHub writes + network | `make ci-integration` (nightly) |
| `e2e_otel` | Docker-Compose observability fixtures (Jaeger, OTLP collector) | Docker daemon + container images | `go test -tags=e2e_otel ./internal/obs/otel/...` (on-demand) |

## Decision rules

Pick the smallest tag that covers your concern.

- **Default tag (none)** when the test uses only Go fixtures + in-process code. Stays in `make check`.
- **`integration_gh`** when the test shells out to ONE external binary that ships with the runtime image (currently `gh`, `git`). Goal: catch upstream binary contract drift. Pattern: `_, err := exec.LookPath("gh"); if err != nil { t.Skipf(...) }` for skip-without-binary safety. Lives next to the unit tests for the package it covers (e.g. `internal/orchestrator/prwatch/*_integ_test.go`).
- **`e2e`** when the test crosses ≥2 component boundaries (adapter → scheduler → spawner → PR) AND consumes a billable resource (Anthropic API, GitHub writes to a non-fixture repo). Goal: catch seam regressions that unit tests miss. Pattern: SKIP without `REGATTA_E2E_*` env so per-PR CI never spends tokens. Lives under `tests/e2e/...`.
- **`e2e_otel`** when the test requires Docker-Compose fixtures (Jaeger, OTLP collector, etc.). Currently a single file: `internal/obs/otel/e2e_test.go`. Not wired into `make ci-integration` — on-demand only.

## Why three tags instead of one

`integration_gh` predates `e2e`; `e2e_otel` is Docker-gated and runs on-demand. The split is intentional:

- `integration_gh` runs cheaply on any runner that has `gh` installed; safe to flip into PR CI someday.
- `e2e` consumes Anthropic tokens + writes to a real GitHub repo per run; SHOULD NOT flip into PR CI even in the future. Different blast-radius.
- `e2e_otel` needs a Docker daemon + container images; runs on-demand because not every dev/CI host has Docker.

Keep the split. Authors of new tests pick the smallest scope. Do not invent a fourth build tag without updating `make ci-integration` (Makefile target) and this document in the same PR.

## Reopen trigger

If a fourth class of test surfaces (e.g. `integration_db` for real-postgres tests), file an issue + update this table. Closes #920.
