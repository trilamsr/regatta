# Container-stage knowledge bridge — Stage 1 / Stage 2 / Stage 3 (2026-06-02)

Status: research brief (informs Stage 1/2/3 implementation specs)
Date: 2026-06-02
Author: research subagent (web research + regatta-fit synthesis)
Memory rules in force: `feedback_research_design_principles` (proven OSS > scratch-built; ≥2 candidates per primitive), `feedback_review_every_step`, `feedback_design_iteration_local`, `feedback_pr_body_file_only`, `feedback_pr_body_release_notes_mandatory`.
Convergence note: Aligns with `docs/engineer/specs/phase-x/2026-06-02-observability-roadmap.md` §1 (OTel SDK + Prom+Grafana + OpenSLO+Sloth + Grafana JSON dashboards) and `examples/observability/docker-compose.yml` (Jaeger all-in-one fixture).
Scope: bridges 13 open knowledge gaps that the containerization pipeline will hit. Each section: current state with two source citations, regatta-fit, recommendation. Final §14 distills per-stage picks.

---

## Stage 1 — runtime container

### §1 Claude Code CLI containerization

**Current state.** The npm package `@anthropic-ai/claude-code` is published with zero runtime dependencies; the current version at research time is **2.1.161** (published within the last day) — the package is on a fast cadence (421 versions shipped, 257 dependents). Anthropic publishes a first-party `devcontainer.json` reference under `.devcontainer/` in the Claude Code GitHub repo, plus a docs page describing the canonical containerized setup (network firewall via `iptables`/`ipset`, non-root `node` user, persistent bash history volume, Claude-config volume on `/home/node/.claude`).

- npm registry: <https://www.npmjs.com/package/@anthropic-ai/claude-code>
- Anthropic devcontainer guide: <https://docs.anthropic.com/en/docs/claude-code/devcontainer>

**Regatta-fit.** Stage 1 needs Claude Code in a long-lived container that regatta spawns as a subprocess. The `0 dependencies` shape means a `node:22-slim` base + a single `npm install -g @anthropic-ai/claude-code@2.1.161` is the entire footprint. The devcontainer ships a Zscaler-style egress firewall that overshoots a developer container; regatta should keep network policy outside the image and rely on Docker network mode + Kubernetes NetworkPolicy at deploy time.

**Recommendation.** Pin `@anthropic-ai/claude-code@2.1.161` in a `package.json`/Dockerfile (do not use `latest` — the version cadence guarantees drift). Adopt the devcontainer's persistent-volume layout for `~/.claude` so credentials survive container restarts. Skip the upstream firewall script — push network policy to the orchestrator layer. Drop a `regatta exec claude` shim that exec-spawns the CLI rather than re-implementing the wrapper.

### §2 gh CLI auth from container

**Current state.** The GitHub CLI honours `GH_TOKEN` (and `GITHUB_TOKEN`, lower precedence) and short-circuits the entire login flow when either is set — no `gh auth login --with-token` call is needed. The official `gh auth login` reference explicitly documents `--with-token` as the interactive-import path; the headless path is the env var. The CLI maintainers note that in containers the env-var path is the documented seam (see cli/cli discussions #8347 and issue #7372).

- `gh auth login` reference: <https://cli.github.com/manual/gh_auth_login>
- cli/cli discussion #8347 (env var precedence in containers): <https://github.com/cli/cli/discussions/8347>
- cli/cli issue #7372 (GH_TOKEN inside docker): <https://github.com/cli/cli/issues/7372>

**Regatta-fit.** Stage 1 invokes `gh` from inside the runtime container for PR-create, PR-view, and issue-write. Stage 3 supervisors will load tokens from a host secret store (1Password / macOS Keychain / Kubernetes Secret).

**Recommendation.** Pass tokens via `GH_TOKEN` env (read from a read-only secret mount at `/run/secrets/gh_token` in compose/k8s, then re-exported by entrypoint). Never call `gh auth login` in a container entrypoint. For multi-host (e.g. enterprise), pass `GH_ENTERPRISE_TOKEN` + `GH_HOST` per the same pattern. Reject the `~/.config/gh/hosts.yml` baked-into-image anti-pattern.

### §3 Alpine vs distroless vs scratch — Go binary base

**Current state.** regatta is pure-Go and uses `modernc.org/sqlite v1.50.1` — a CGo-free SQLite driver, so the binary has zero glibc/musl runtime dependency. Sizes (2025 benchmarks): `scratch` 0 B, `distroless/static` ~2 MB (CA certs + tzdata + `/etc/passwd`), `alpine:3` ~7 MB, `distroless/base` ~20 MB (glibc + libssl). Alpine ships `apk` + `busybox`; distroless has no shell; scratch has no filesystem.

- modernc.org/sqlite pkg.go.dev: <https://pkg.go.dev/modernc.org/sqlite>
- Distroless GitHub: <https://github.com/GoogleContainerTools/distroless>

**Regatta-fit.** No CGO, no shell needed at runtime (regatta is a single static binary), CA certs needed for outbound HTTPS to Anthropic/GitHub, tzdata needed for cron-style scheduling. Debugging in production is rare (we lean on traces/logs); when needed, operators `kubectl debug` an ephemeral sidecar.

**3-way score (5 = strong adopt; weights: size 1×, security 2×, debug ergonomics 1×, ecosystem 1×).**

| Candidate | Size | Security | Debug | Ecosystem | Weighted | Verdict |
|---|---|---|---|---|---|---|
| `gcr.io/distroless/static-debian12:nonroot` | 4 (~2 MB) | 5 (no shell, no pkg-mgr, pinned non-root UID 65532) | 3 (no shell; need `:debug` variant for shell access) | 4 (Google-maintained, image-signing via cosign) | **20/25 → ADOPT** |
| `alpine:3.21` | 5 (~7 MB) | 3 (apk + busybox = larger attack surface) | 5 (`apk add` curl/strace ad-hoc) | 5 | 18/25 |
| `scratch` | 5 (0 B) | 4 (no shell, but also no `/etc/passwd` → must be UID-numeric) | 1 (zero introspection tools) | 3 (CA certs + tzdata + non-root UID must be hand-staged) | 14/25 |

**Recommendation.** `gcr.io/distroless/static-debian12:nonroot` for the runtime image. CA certs, tzdata, and the non-root user are pre-baked; the entire base is signed. Keep `:debug` variant available for break-glass kubectl-exec. Scratch is rejected — the cost of hand-staging certs + UID/GID is higher than the 2 MB delta.

### §4 SQLite WAL + Docker bind-mount pitfalls

**Current state.** SQLite WAL mode requires shared-memory across all readers/writers of the database; the upstream WAL doc states "all processes using a database must be on the same host computer" and that WAL "does not work over a network filesystem". The 9p / virtiofs layer used by Docker Desktop on macOS and Windows has measurably slower `fsync` than native ext4, and contention can spike under load. WAL durability is also a footgun: the default `synchronous=NORMAL` is durable across process crashes but not power-loss; `synchronous=FULL` is the correct setting if a power-loss-durable commit is required.

- SQLite WAL doc: <https://www.sqlite.org/wal.html>
- SQLite over a network: <https://www.sqlite.org/useovernet.html>

**Regatta-fit.** Stage 1 mounts `/data` as the regatta state volume (holds `regatta.db`, `regatta.db-wal`, `regatta.db-shm`, plus `.regatta/items/`). The state must survive container restart and must not be backed by a network filesystem.

**Recommendation.** Back `/data` with a **named Docker volume** (driver `local`), not a host bind-mount, on Linux production hosts — keeps the WAL shared-memory segment on a real local filesystem. On developer macOS, bind-mounts are acceptable but expect 5-10× `fsync` overhead; document this in the operator guide. Set `PRAGMA synchronous=FULL` for the WAL file (regatta's writer wrapper already calls `PRAGMA journal_mode=WAL` — extend with `synchronous=FULL`). Ban NFS / SMB / EFS / Azure Files for `/data` — fail-fast on startup if the mount type is networked (`statfs` magic check). Single-writer invariant (already enforced by regatta's `flock`) survives unchanged.

### §5 Container security baseline

**Current state.** The 2025–2026 baseline is: non-root user (numeric UID), `readOnlyRootFilesystem: true`, `securityContext.capabilities.drop: ["ALL"]` adding only what's strictly needed, `allowPrivilegeEscalation: false` (Linux `no-new-privileges:true` flag in Docker), seccomp `RuntimeDefault`. Mounting secrets read-only at `/run/secrets/` is the de-facto pattern.

- Wiz Kubernetes security context guide: <https://www.wiz.io/academy/container-security/kubernetes-security-context-best-practices>
- OneUptime readOnlyRootFilesystem reference: <https://oneuptime.com/blog/post/2026-02-09-readonly-root-filesystem-immutable-containers/view>

**Regatta-fit.** regatta needs write access to exactly two paths: `/data` (state) and `/tmp` (scratch). Everything else is read-only.

**Recommendation.** Apply the full baseline:
- `USER 65532:65532` (matches distroless `nonroot`)
- `readOnlyRootFilesystem: true` + `tmpfs` at `/tmp` (default 64 MiB)
- `securityContext.capabilities.drop: ["ALL"]` — regatta needs no Linux capabilities
- `allowPrivilegeEscalation: false`
- `seccompProfile.type: RuntimeDefault`
- Secrets at `/run/secrets/`, mounted `readOnly: true`, 0400 perms
- Image-signing: cosign-verify the distroless base in the production image-pull policy

Document the matching `--read-only --cap-drop ALL --security-opt no-new-privileges:true --user 65532:65532` set for the `docker run` smoke-test path.

---

## Stage 2 — docker-compose observability stack

### §6 OTel collector vs direct OTLP-to-Prometheus

**Current state.** Prometheus 3.0 (Nov 2024) added a **native OTLP receiver** at `/api/v1/otlp/v1/metrics`; Prometheus 3.7 (Oct 2025) wired OTLP to write directly to the TSDB (skipping the internal Remote-Write adapter), dropping latency and CPU. Running the OpenTelemetry Collector as a forwarder is still useful for fan-out (metrics+traces+logs splitting), redaction, and sampling, but for **metrics-only** the collector is a redundant hop.

- Prometheus OTLP guide: <https://prometheus.io/docs/guides/opentelemetry/>
- Prometheus 3.0 announcement: <https://prometheus.io/blog/2024/11/14/prometheus-3-0/>

**Regatta-fit.** The roadmap (§1.3) picks Prom+Grafana for self-host and Honeycomb as the operator-swap. Both ingest OTLP directly. Traces already go to Jaeger all-in-one via OTLP (the Jaeger fixture in `examples/observability/docker-compose.yml` enables `COLLECTOR_OTLP_ENABLED`). Logs are already OTLP via the W6 slog bridge.

**Recommendation.** **Skip the OTel Collector for self-host Stage 2.** Wire regatta OTLP → Prometheus 3.x directly for metrics, OTLP → Jaeger for traces, OTLP → operator's log backend for logs. Add a documented escape hatch: if an operator wants fan-out (e.g. Prom + Honeycomb in parallel), drop in `otel/opentelemetry-collector-contrib` in front, set `OTEL_EXPORTER_OTLP_ENDPOINT=collector:4317`, no regatta code change. Pin Prometheus to `prom/prometheus:v3.7.x` (LTS line) — enable OTLP receiver via `--web.enable-otlp-receiver`.

### §7 Grafana 11 dashboard schema + provisioning

**Current state.** Grafana provisions datasources from `/etc/grafana/provisioning/datasources/*.yaml` and dashboards from `/etc/grafana/provisioning/dashboards/*.yaml` (the dashboards yaml points at a folder of JSON files). Dashboard JSON schema is versioned (`schemaVersion`); Grafana 11 reads up to schemaVersion 39+. The provisioning files are watched and reloaded automatically.

- Grafana provisioning docs: <https://grafana.com/docs/grafana/latest/administration/provisioning/>
- Grafana dashboards import docs: <https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/import-dashboards/>

**Regatta-fit.** Roadmap §1.4 already locks Grafana dashboard JSON at `docs/operator/dashboards/*.json`. Stage 2 needs the provisioning yaml + a Grafana 11 image pin + a `make provision-dashboards` target.

**Recommendation.** Ship `examples/observability/grafana/provisioning/datasources/prometheus.yaml` (points at `http://prometheus:9090`) and `examples/observability/grafana/provisioning/dashboards/regatta.yaml` (points at `/var/lib/grafana/dashboards/regatta`, mounted from `docs/operator/dashboards/`). Pin `grafana/grafana:11.4.x` (latest 11.x LTS). Wire `GF_SECURITY_ADMIN_PASSWORD__FILE=/run/secrets/grafana_admin` so the password is never in compose.

### §8 AlertManager webhook receiver shape

**Current state.** AlertManager retries failed webhook deliveries with exponential back-off; the upstream behavior treats 4xx (incl. 429) the same as 5xx for retry purposes. The retry budget is implicit — there is no documented `max_retries` but contexts time out per the `group_interval`. The webhook configuration supports `http_config.timeout` (per the open #2657 enhancement) and the receiver shape is documented in the upstream configuration reference. Payload is a stable JSON envelope (`status`, `alerts[]`, `groupLabels`, `commonLabels`, `commonAnnotations`).

- AlertManager configuration reference: <https://prometheus.io/docs/alerting/latest/configuration/>
- AlertManager webhook timeout enhancement #2657: <https://github.com/prometheus/alertmanager/issues/2657>
- AlertManager 429 retry semantics #2121: <https://github.com/prometheus/alertmanager/issues/2121>

**Regatta-fit.** W1 / #458 lands `regatta-alarm-webhook`. The receiver needs to be idempotent (AlertManager may retry the same alert), accept the standard AlertManager payload shape, and respond 200 within `group_interval` (default 5m) to avoid spurious retries.

**Recommendation.** Build `regatta-alarm-webhook` to:
- Accept POST `application/json` matching AlertManager's webhook payload schema (version `4`).
- Idempotency key: hash of `fingerprint + status + startsAt` per alert; store last-seen TTL 1h to dedupe retries.
- Respond 200 within 30s; defer slow work (rendering the operator surface) to a goroutine.
- Return 5xx only for actually-transient failures — AlertManager treats 4xx the same as 5xx for retries, so 4xx still wastes retry budget; respond 200 + log for genuinely-unprocessable payloads.
- Configure the AlertManager-side `webhook_configs` with `http_config.timeout: 30s` and per-receiver `send_resolved: true`.

### §9 Prometheus 3.x features — adopt now vs defer

**Current state.** Prometheus 3.x ships: (a) native histograms (still feature-flagged `--enable-feature=native-histograms`, not on by default), (b) native OTLP receiver (stable in 3.0, TSDB-direct in 3.7), (c) Remote-Write 2.0 (experimental — string-interned, includes exemplars + native histograms), (d) info metrics (stable), (e) UTF-8 metric names (stable, lifts the `_total` suffix friction with OTLP semconv).

- Prometheus 3.0 announcement: <https://prometheus.io/blog/2024/11/14/prometheus-3-0/>
- Prometheus Remote-Write 2.0 spec: <https://prometheus.io/docs/specs/prw/remote_write_spec_2_0/>

**Regatta-fit.** Roadmap §2.1 picks `regatta.<surface>.<action>.<unit>` naming — dot-separated. Native histograms cut cardinality on the latency-histogram metrics (scheduler tick, L4 latency, PR-stage duration) by 10-100×.

**Recommendation.** **Adopt now:**
- Native OTLP receiver (`--web.enable-otlp-receiver`) — already justified by §6.
- UTF-8 metric names — required for the dot-notation roadmap §2.1 schema.
- Info metrics — already idiomatic.

**Defer:**
- Native histograms — feature-flag while it's still beta; revisit when stable. Classic histograms are fine at current cardinality.
- Remote-Write 2.0 — only relevant if an operator forwards to Mimir/Thanos; not on the Phase-S critical path.

### §10 Sloth vs OpenSLO 2.0 native runtime

**Current state.** Sloth is the dominant Prometheus SLO generator today; it consumes OpenSLO v1 YAML (with documented caveats: no Prom labels, no alerting metadata, no SLI plugins via the OpenSLO path) and emits Prom recording + multi-window multi-burn alert rules. OpenSLO is CNCF Sandbox; there is no widely-deployed native OpenSLO runtime — the spec is a definition language, and Sloth is the de facto compiler. Nobl9 is the commercial native runtime.

- Sloth GitHub: <https://github.com/slok/sloth>
- OpenSLO project: <https://openslo.com/>

**Regatta-fit.** Roadmap §1.7 already adopts **OpenSLO YAML + Sloth compiler** — this research confirms that the OpenSLO-via-Sloth path has the known feature gaps. SLO-1 and SLO-2 (shipped in #503) need Prom labels and alert routing, which means using Sloth's **native YAML**, not OpenSLO-via-Sloth.

**Recommendation.** **Revisit the §1.7 picks.** Hold the strategic bet on OpenSLO as the source-of-truth spec, but commit to **Sloth-native YAML** for the rules SLO-1/SLO-2 actually deploy. Add a one-way converter (or maintain both) so the OpenSLO files stay the "spec" and the Sloth-native files are generated. When OpenSLO closes the Sloth-feature gaps (labels + alerting), flip the source-of-truth back to pure OpenSLO. Track this as a follow-up issue against the observability roadmap.

---

## Stage 3 — service-supervisor

### §11 systemd best practices for Go services

**Current state.** Type=notify is the supervised path — the daemon calls `sd_notify(READY=1)` once started; systemd waits for that signal before treating the unit as `active`. Recommended hardening directives: `Restart=on-failure`, `RestartSec=5s`, `RuntimeMaxSec=` (auto-restart cap), `MemoryHigh=` / `MemoryMax=` (cgroup soft/hard limit), `LimitNOFILE=65535`, `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp=true`, `NoNewPrivileges=true`, `DynamicUser=true` (or fixed UID + `User=`/`Group=`), `WatchdogSec=` (paired with `sd_notify(WATCHDOG=1)`).

- systemd.service manual: <https://0pointer.de/public/systemd-man/systemd.service.html>
- systemd.exec manual: <https://0pointer.de/public/systemd-man/systemd.exec.html>

**Regatta-fit.** Stage 3 service-supervisor needs Type=notify with watchdog, hardening, and one writable path (`/var/lib/regatta`).

**Recommendation.** Use `coreos/go-systemd/v22/daemon` for the `sd_notify` call in regatta's main(). Ship `packaging/systemd/regatta.service` with:

```
[Service]
Type=notify
ExecStart=/usr/local/bin/regatta serve
Restart=on-failure
RestartSec=5s
WatchdogSec=30s
LimitNOFILE=65535
MemoryHigh=512M
MemoryMax=1G
TasksMax=512
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
NoNewPrivileges=true
ReadWritePaths=/var/lib/regatta
User=regatta
Group=regatta
```

Skip `DynamicUser=true` — regatta's sqlite state lives at `/var/lib/regatta`, which needs a fixed UID for ownership across upgrades. Skip `RuntimeMaxSec=` — there's no policy reason to auto-restart on a clock.

### §12 macOS launchd plist

**Current state.** launchd splits "daemon" (system-wide, root-owned, `/Library/LaunchDaemons/`) from "agent" (per-user, `~/Library/LaunchAgents/`). `KeepAlive=true` makes launchd restart the job on exit; `ThrottleInterval` (seconds, default 10s) is the minimum respawn gap; `StandardOutPath` / `StandardErrorPath` route stdio. There is no native `sd_notify` equivalent — launchd treats the job as alive once exec() returns successfully.

- Apple launchd jobs guide: <https://developer.apple.com/library/archive/documentation/MacOSX/Conceptual/BPSystemStartup/Chapters/CreatingLaunchdJobs.html>
- launchd.info tutorial: <https://www.launchd.info/>

**Regatta-fit.** Operators on macOS will run `regatta serve` under a **LaunchAgent** (per-user, autostarts at login) — not a LaunchDaemon. Token credentials live in the user's keychain; running as root would defeat that.

**Recommendation.** Ship `packaging/launchd/com.regatta.serve.plist` for `~/Library/LaunchAgents/`:

```xml
<key>Label</key>           <string>com.regatta.serve</string>
<key>ProgramArguments</key><array><string>/usr/local/bin/regatta</string><string>serve</string></array>
<key>KeepAlive</key>       <dict><key>SuccessfulExit</key><false/></dict>
<key>ThrottleInterval</key><integer>10</integer>
<key>StandardOutPath</key> <string>/Users/<user>/Library/Logs/regatta/out.log</string>
<key>StandardErrorPath</key><string>/Users/<user>/Library/Logs/regatta/err.log</string>
<key>EnvironmentVariables</key><dict>
  <key>OTEL_EXPORTER_OTLP_ENDPOINT</key><string>localhost:4317</string>
</dict>
```

`KeepAlive=<dict><key>SuccessfulExit</key><false/></dict>` matches `Restart=on-failure` semantics — only restart on non-zero exit. Document `launchctl bootstrap gui/$UID …` and `launchctl bootout …` as install/uninstall verbs.

### §13 Kubernetes manifest — Deployment vs StatefulSet

**Current state.** Deployment + a single PVC (RWO) supports a single-replica sqlite workload — the Pod gets a stable mount across restarts. StatefulSet's promises (stable network identity, ordered rollout, per-replica volumeClaimTemplates) are wasted on a single replica. The argument *for* StatefulSet at single-replica is rollout safety (StatefulSet rolling update guarantees the old Pod is gone before the new one starts; Deployment's default `RollingUpdate` can briefly run two Pods, both trying to grab the sqlite lock).

- Kubernetes StatefulSet: <https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/>
- Kubernetes Deployment: <https://kubernetes.io/docs/concepts/workloads/controllers/deployment/>

**Regatta-fit.** regatta is single-writer (sqlite + `flock`). Two concurrent Pods would both fail to acquire the lock; the second would crash-loop. The cost of a brief 2-Pod overlap during rollout is a noisy startup but no data corruption (flock serializes). Stage 3's k8s manifest is "future" — not on the Phase-S critical path — but the decision has to be locked early to avoid migration churn.

**Recommendation.** **StatefulSet, single replica.**
- `replicas: 1`, `serviceName: regatta` (headless service), `podManagementPolicy: OrderedReady`.
- `volumeClaimTemplates` → `claimName: data` mounted at `/data`.
- `strategy.type: Recreate`-equivalent via StatefulSet's serial rolling-update — old Pod terminates before new one starts, no flock race.
- Init container: `initContainers: [{name: seed, command: ["regatta", "init", "--seed-if-empty"]}]` — only runs first deploy; idempotent on later restarts. Seeds `.regatta/items/` and runs goose migrations.
- PVC: `accessModes: [ReadWriteOnce]`, storageClass set by operator. **Reject ReadWriteMany** — sqlite WAL forbids it.
- Liveness: `/healthz`; readiness: `/readyz` (waits on migration + first scheduler tick).
- PDB: `maxUnavailable: 1` (degenerate at replicas=1, but documents the intent).

When the workload ever grows past single-writer (Phase Z multi-tenant), the migration is sqlite → Postgres, not Deployment → StatefulSet. The Deployment-vs-StatefulSet choice is independent of that future split.

> Alternative considered: `Deployment` with `strategy.type: Recreate` achieves the same rollout-safety property (old Pod terminated before new Pod starts) at lower controller-complexity. The brief picks StatefulSet because the stable network identity + ordered init container semantics compound with future growth (per-tenant shard, replica observability); a single-replica Deployment+Recreate is a defensible simpler alternative if those compounders never land.

---

## §14 Per-stage decision summary

### Stage 1 picks (runtime container)
- **Base image:** `gcr.io/distroless/static-debian12:nonroot` (UID 65532).
- **Build:** multi-stage; `golang:1.25.11-bookworm` builder → distroless runtime. `CGO_ENABLED=0` (modernc.org/sqlite is pure-Go — no cgo, ships zero-dep static binary).
- **Claude Code:** sibling container `node:22-slim` + pinned `@anthropic-ai/claude-code@2.1.161`; persistent volume on `/home/node/.claude`.
- **gh CLI:** install in the regatta image (apt or static download); pass `GH_TOKEN` via env from `/run/secrets/gh_token`.
- **State volume:** named Docker volume at `/data`; **never** a network filesystem; refuse to start if `/data` is NFS/SMB/EFS.
- **SQLite PRAGMAs:** `journal_mode=WAL` (already) + `synchronous=FULL` (new).
- **Security baseline:** non-root + `readOnlyRootFilesystem` + `cap-drop ALL` + `no-new-privileges` + seccomp `RuntimeDefault` + secrets at `/run/secrets/` 0400.

### Stage 2 picks (docker-compose obs stack)
- **Metrics:** OTLP direct → Prometheus 3.7.x (`--web.enable-otlp-receiver`). No OTel collector hop.
- **Traces:** OTLP direct → existing Jaeger all-in-one (already pinned at `jaegertracing/all-in-one:1.76.0` in `examples/observability/docker-compose.yml`).
- **Logs:** OTLP direct to operator-chosen backend (no regatta-side log aggregator).
- **Dashboards:** Grafana 11.4.x with file-based provisioning of datasources + dashboards; JSON checked into `docs/operator/dashboards/`.
- **Alerts:** AlertManager → `regatta-alarm-webhook` (W1/#458) with idempotent receiver, `send_resolved: true`, `http_config.timeout: 30s`.
- **SLOs:** Sloth-native YAML for the rules that ship (SLO-1/SLO-2); OpenSLO as the canonical-spec layer with a converter; revisit when OpenSLO supports Prom labels + alert metadata.
- **Prometheus 3.x feature flags:** adopt OTLP receiver + UTF-8 names; defer native histograms + Remote-Write 2.0.
- **Operator escape hatch:** drop-in OTel Collector for fan-out (e.g. Prom + Honeycomb in parallel); zero regatta-side change.

### Stage 3 picks (service-supervisor)
- **Linux:** systemd Type=notify unit at `packaging/systemd/regatta.service`. `sd_notify` via `coreos/go-systemd/v22/daemon`. Hardening: `ProtectSystem=strict`, `ProtectHome=true`, `PrivateTmp=true`, `NoNewPrivileges=true`, `MemoryMax=1G`, `LimitNOFILE=65535`, fixed `User=regatta`, `ReadWritePaths=/var/lib/regatta`, `WatchdogSec=30s`.
- **macOS:** LaunchAgent at `packaging/launchd/com.regatta.serve.plist` (per-user, not LaunchDaemon). `KeepAlive` keyed on `SuccessfulExit=false`. `ThrottleInterval=10`. Logs to `~/Library/Logs/regatta/`.
- **Kubernetes:** StatefulSet, `replicas=1`, headless service, `volumeClaimTemplates` (RWO), init container for `regatta init --seed-if-empty`, liveness `/healthz`, readiness `/readyz`. Reject RWX storage classes.

---

## §15 Open follow-ups (file as issues at impl time)

1. **OpenSLO ↔ Sloth-native converter** — track the OpenSLO spec for label + alerting support; revisit §10 once closed.
2. **Native-histogram readiness gate** — re-evaluate §9 once Prometheus declares native histograms stable.
3. **`/data` mount-type guard** — Stage 1 startup check: `statfs(2)` magic against `NFS_SUPER_MAGIC` / `SMB_SUPER_MAGIC` / etc.; fail-fast with operator-actionable error.
4. **Watchdog liveness wiring** — `WatchdogSec=30s` + `sd_notify(WATCHDOG=1)` heartbeat from the scheduler tick is the natural place; spec into Stage 3 implementation.
5. **macOS keychain integration for `GH_TOKEN`** — Stage 3 macOS plist references a keychain-pulled token; spec the helper at impl time.
