# docker-compose self-host stack (Stage 2)

Reader: operator standing up Regatta on a single host with the full
metrics + dashboards + alerts surface.
Read time: 10 minutes.
Expires when: a service image pin, ingest topology, or dashboard
folder layout changes.

## Status

Stage 2 of containerization. Four services on one bridge:

| Service | Image | Port | Role |
|---|---|---|---|
| `regatta` | `regatta:stage2` (local build) | 8080 | the orchestrator binary from Stage 1. |
| `prometheus` | `prom/prometheus:v3.7.2` | 9090 | OTLP-native receiver + TSDB + alert evaluator. |
| `grafana` | `grafana/grafana:11.4.0` | 3000 | provisioned dashboards from `docs/operator/dashboards/`. |
| `alertmanager` | `prom/alertmanager:v0.28.1` | 9093 | routes Sloth-compiled alerts → `regatta-alarm-webhook`. |

The stack runs everything on one Docker bridge (`regatta-net`) with
four named volumes (`regatta-data`, `prom-data`, `grafana-data`,
`alertmanager-data`).

Stage 1 covered the runtime container alone — see
[`container.md`](container.md). Stage 3 swaps the compose orchestration
for a systemd / launchd / Kubernetes supervisor.

## Quickstart

```sh
cp .env.example .env
$EDITOR .env                  # ANTHROPIC_API_KEY + GH_TOKEN
echo "REPO_PATH=$PWD" >> .env # the repo bind-mount target

docker compose up -d
```

The first `up` pulls the three external images, builds the `regatta`
image from the in-tree `Dockerfile`, and starts all four services.
Initial start takes ~30 s on a warm Docker daemon (Prometheus health
check gate-keeps `regatta` and `grafana`).

The compose runs `regatta serve --ui=${REGATTA_UI:-true}`. The UI is on
by default; flip it off for a headless dispatch loop with
`REGATTA_UI=false` in `.env`. The daemon refuses to boot if `--ui=true`
and `REGATTA_HMAC_KEY` is unset (`cmd/regatta/wire_web.go::preflightUIBoot`);
`--ui=false` makes the key optional. The operator UI lands on
`http://localhost:8080`; RBAC seam lives under
[`rbac-onboarding.md`](rbac-onboarding.md).

Note: compose evaluates `${REGATTA_UI:-true}` against shell env first
then `.env`. If `REGATTA_UI` is exported in the host shell from a
prior session it will shadow the `.env` value silently — `unset
REGATTA_UI` before `docker compose up` if the default is what you want.

### Spawner billing mode (subscription vs pay-as-you-go)

The compose default `REGATTA_SPAWNER_STRIP_API_KEY=1` strips the parent
`ANTHROPIC_API_KEY` from spawned `claude` CLI children so they
authenticate via the operator's subscription credentials at `~/.claude`.
Pay-as-you-go operators set `REGATTA_SPAWNER_STRIP_API_KEY=0` in `.env`
to pass the parent token through.

The subscription path requires the operator to expose `~/.claude` to
the container via a `docker-compose.override.yml` (gitignored). Example:

```yaml
# docker-compose.override.yml (operator-supplied, not in tree)
services:
  regatta:
    volumes:
      - ${HOST_CLAUDE_DIR:-${HOME}/.claude}:/home/nonroot/.claude:ro
```

Override is auto-merged by `docker compose` at boot. The mount is not
baked into the main compose file because (a) the host directory mode is
typically `0700` owned by the operator uid (501 macOS / 1000 Linux)
while the distroless container runs uid 65532, so Linux native hosts
hit `EACCES` without an explicit `--user` override or `chmod`, and (b)
`~/.claude` is the operator's full Claude Code session dir (memory,
plans, file-history), not just credentials — exposing the whole tree
by default is broader than necessary. The per-operator override lets
each host pick the right subset and mount mode.

Without an override AND without `REGATTA_SPAWNER_STRIP_API_KEY=0`,
spawned children emit `Not logged in · Please run /login` and exit
with `exit_reason=auth_precondition_failed`. Either fix the override
or flip the flag; both are explicit operator choices.

## Verify

```sh
# Service health
docker compose ps

# Regatta operator UI — login with REGATTA_HMAC_KEY-derived session.
open http://localhost:8080

# Prometheus UI — confirms scrape + OTLP ingest paths.
open http://localhost:9090

# Grafana — login admin / admin; rotate on first session.
open http://localhost:3000

# AlertManager — confirms rule routing topology.
open http://localhost:9093
```

Inside the Prometheus UI:

1. **Status → Configuration** — sanity-check the OTLP receiver section
   and the scrape job for `regatta:9464`.
2. **Status → Rules** — every Sloth-compiled rule under
   `dashboards/prometheus/rules/` should be listed with green
   `OK` state.
3. **Graph** — query `up{job="prometheus"}` returns 1; query
   `regatta_scheduler_tick_count` returns rows once Regatta has
   ticked at least once.

Inside Grafana:

1. **Dashboards → Regatta** — the seven JSONs from
   `docs/operator/dashboards/` appear as a folder.
2. Open any dashboard, select the `Prometheus` datasource if prompted
   (the provisioning sets it as default; the prompt only fires after
   a Grafana state reset).

## OTLP-native ingest: how metrics flow

```
regatta (OTel SDK)
   │  OTLP/HTTP
   ▼
prometheus :9090/api/v1/otlp/v1/metrics
   │  --web.enable-otlp-receiver
   ▼
TSDB ────► Sloth rules ────► AlertManager ────► regatta-alarm-webhook
   │
   ▼
Grafana :3000 (datasource: Prometheus)
```

No OTel Collector hop. Prometheus 3.x's native receiver writes the
OTLP payload direct to TSDB, with `--enable-feature=otlp-deltatocumulative`
converting the OTel default delta-temporality counters to Prom's
cumulative model at ingest.

Operators who need fan-out (e.g. Prom plus a vendor backend in
parallel) drop in an OTel Collector between Regatta and Prometheus,
point Regatta at the collector's :4317, and configure the collector's
pipeline. No Regatta-side change.

## Update the SLO YAML

Sloth rule files in `dashboards/prometheus/rules/` are compiled output
of the source `slo/*.yaml`. To change an SLO:

```sh
# 1. Edit the source spec.
$EDITOR slo/l4-latency.yaml

# 2. Re-compile (regenerates dashboards/prometheus/rules/l4-latency.yaml).
make slo-compile

# 3. Hot-reload Prometheus — the compose flag --web.enable-lifecycle
#    accepts a POST to /-/reload.
curl -X POST http://localhost:9090/-/reload
```

The mounted rules directory is read on every reload; no container
restart needed.

## Update a dashboard

`docs/operator/dashboards/*.json` is the source of truth. Grafana's
file provider scans the mounted folder every 30 s and applies edits
in place. No restart needed.

To export an in-Grafana change back to the repo:

1. Dashboard → Settings → JSON model → Copy.
2. Paste over the matching file in `docs/operator/dashboards/`.
3. Commit. The next `docker compose up -d` picks up the new bytes.

## Troubleshooting

### Prometheus shows zero `regatta_*` metrics

Three likely causes:

1. **OTLP env vars not reaching the container.** `docker compose exec
   regatta env | grep OTEL` — both `OTEL_EXPORTER_OTLP_ENDPOINT` and
   `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` should be present.
2. **Regatta hasn't ticked yet.** The scheduler emits its first
   metrics on the first tick (~5 s post-boot). `docker compose logs
   regatta` should show `tick=1` within 10 s.
3. **Prometheus rejected the OTLP payload.** Bump verbosity:

   ```sh
   docker compose stop prometheus
   docker compose run --rm prometheus \
     --config.file=/etc/prometheus/prometheus.yml \
     --log.level=debug \
     --web.enable-otlp-receiver
   ```

   Watch for `otlp ... rejected` log lines; usual culprit is a metric
   name with an unsupported character (UTF-8 names are stable in
   Prom 3.x but downstream tools may not be).

### AlertManager fires but no GitHub issue lands

Expected today. `regatta-alarm-webhook` (#458 W1) is not yet
implemented; the webhook URL is a placeholder that returns a connection
error. AlertManager will retry per its back-off schedule; the alerts
are visible at <http://localhost:9093>. When W1 ships, this section
gets updated with the operator-side `gh issue` confirmation path.

### Grafana shows "No data" on every panel

Datasource UID drift. The dashboards reference `${datasource}` with
the provisioned UID `prometheus`. Confirm:

```sh
docker compose exec grafana \
  curl -s -u admin:admin http://localhost:3000/api/datasources \
  | grep uid
```

Should print `"uid":"prometheus"`. If a different UID appears, the
provisioning file under
`docker/grafana/provisioning/datasources/prometheus.yml` was edited
without a matching sweep of `docs/operator/dashboards/*.json`.

### `docker compose up -d` exits with `regatta: build failed`

The Stage 1 `Dockerfile` builds from the repo root. Common build-time
failures:

- `go.sum` mismatch — run `make tidy-check` on the host first.
- `npm install -g @anthropic-ai/claude-code@latest` blocked by a
  corporate proxy — set `HTTPS_PROXY` in the host environment before
  `docker compose build`.

See [`container.md`](container.md) §Troubleshooting for the Stage 1
runtime issues that surface inside the `regatta` service the same
way.

## Teardown

```sh
docker compose down              # stop + remove containers
docker compose down -v           # also delete the four named volumes
```

The `-v` form discards the substrate DB, TSDB data, Grafana state,
and AlertManager silences. Use it for a clean re-bootstrap; omit for
a service restart that preserves history.

## Related

- [`container.md`](container.md) — Stage 1 runtime image runbook.
- [`observability.md`](observability.md) — OTel env-var contract +
  exporter swap.
- [`dashboards/README.md`](dashboards/README.md) — dashboard index +
  per-tile spec references.
