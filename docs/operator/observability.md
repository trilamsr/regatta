# Observability

Reader: customer-operator wiring Regatta to an OpenTelemetry backend
(Jaeger, Tempo, Honeycomb, Datadog, etc.).
Read time: 8 minutes.
Goal: traces and logs from `regatta serve` flowing into your backend,
with the operator knowing every knob and the payload-safety policy.
Expires when: env-var contract in
`docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md` §3.6 changes.

## What you get

Regatta emits OpenTelemetry traces and logs through the standard
OTLP/gRPC exporters. Default off — with no env vars set, the binary is
byte-identical to the pre-W6 behaviour (no sockets opened, no
goroutines started). Opt in by setting `OTEL_EXPORTER_OTLP_ENDPOINT`.

The span hierarchy lands as:

```
program
  └─ tick                (one per scheduler tick)
       └─ work_item      (one per pending row that tick touches)
            ├─ gate.evaluate
            ├─ operator_invocation
            │    └─ chat <model>   (the LLM call, with gen_ai.* attrs)
            └─ reaper.sweep
```

Every span carries the resource attributes `service.name=regatta`,
`service.version=<build>`, and `regatta.tenant_id=default`. The
`gen_ai.*` attribute set on the `chat` span follows the OpenTelemetry
GenAI semantic conventions; see [the W6 design spec](../engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md)
§3.4 for the full attribute table.

## Local demo: Jaeger in five seconds

The repo ships a docker-compose fixture that runs Jaeger all-in-one.
Bring it up, point regatta at it, and open the Jaeger UI:

```sh
docker compose -f examples/observability/docker-compose.yml up -d
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 regatta serve
open http://localhost:16686
```

The UI lists `regatta` as a service after the first tick exports a
span. Click a trace to see the full `tick → work_item →
operator_invocation → chat` tree.

See [examples/observability/README.md](../../examples/observability/README.md)
for the fixture's full surface (ports, image version, teardown).

## Environment variables

Regatta does not invent a parallel YAML schema for OTel; the SDK's
own env-var contract is the single source of truth. Every variable
below is read by the OTel Go SDK at `Setup` time and shapes the
provider that `cmd/regatta` installs once at boot.

| Variable | Purpose |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Set to enable OTLP/gRPC export (e.g. `http://collector:4317`). Unset → no exporter wired. |
| `OTEL_EXPORTER_OTLP_HEADERS` | Comma-separated key=value pairs sent on every OTLP request. Typical use: vendor auth tokens. |
| `OTEL_SERVICE_NAME` | Override the `service.name` resource attribute. Default is `regatta`; you only override it when running multiple regatta deployments against a shared backend. |
| `OTEL_TRACES_SAMPLER` | Override the head sampler. Regatta defaults to `parentbased_always_on`. See [Sampler customization](#sampler-customization). |
| `OTEL_RESOURCE_ATTRIBUTES` | Comma-separated key=value pairs merged into the resource attribute set. Use this to add tenant, region, or environment tags. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | Per-signal endpoint override. Honoured by the SDK; presence behaves like `OTEL_EXPORTER_OTLP_ENDPOINT` for exporter wiring. |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` | Per-signal endpoint override for logs. |
| `OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT` | Operator-side cardinality cap. See [Cardinality](#cardinality). |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` (default) or `http/protobuf`. Regatta wires the gRPC exporter today; setting this to `http/protobuf` is a documented future-compat hook. |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` skips TLS verification — set this for the local Jaeger fixture; never in production. |

Regatta-specific flag:

| Flag | Purpose |
|---|---|
| `--otel-dev-stdout` | Routes spans and logs to stdout in human-readable JSON instead of OTLP. Useful for one-shot dev runs where you do not want a collector. Mutually exclusive with `OTEL_EXPORTER_OTLP_ENDPOINT` — combining the two returns `ErrOTelExporterConflict` at boot. |

The OTel Go SDK reads more env vars than the table covers (timeouts,
compression, certificate paths). Anything the SDK documents at
[opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/](https://opentelemetry.io/docs/languages/sdk-configuration/otlp-exporter/)
flows through regatta unchanged; the variables above are the ones an
operator typically needs.

## Sensitive payload policy

Regatta does not emit `gen_ai.input.messages` or `gen_ai.output.messages`
on any span. These OTel GenAI attributes carry the full prompt and
response payload — they are off by default in W6 and are not
configurable. The runtime test `TestGenAI_SensitivePayloadNotEmitted`
fails closed if any future code path attempts to set them.

Why off:

- PII and credential bleed risk: prompts routinely contain operator
  secrets, customer data, internal URLs.
- Unbounded cardinality: every prompt body is unique, which breaks
  index assumptions in every backend.

The W6 follow-up tracking issues define the redaction contract
required before the attributes can be flipped on. Until then,
operators who need a prompt audit trail should pipe the slog event
stream into a redacted sink — not the OTel spans.

## Sampler customization

Regatta wires `sdktrace.ParentBased(sdktrace.AlwaysSample())` as the
default head sampler. Operators who need a different policy override
it through `OTEL_TRACES_SAMPLER`, which the SDK reads at provider
construction time. The supported values are documented by the OTel
SDK; the common cases:

- `parentbased_always_on` — the default; record every span.
- `parentbased_always_off` — record nothing.
- `parentbased_traceidratio` with `OTEL_TRACES_SAMPLER_ARG=0.1` —
  sample 10% of root spans, with children inheriting the parent's
  decision.
- `always_on` / `always_off` / `traceidratio` — same families
  without the parent-respecting wrapper.

The default is `parentbased_always_on` because regatta's tick rate
is low (default 5 s, expected single-digit-per-second peak) and the
operator value of a complete trace outweighs the sampling overhead.
At higher volumes, head-sample at the SDK or tail-sample at the
collector.

### Sampling and cost reconciliation

Cost reconciliation does NOT depend on the sample ratio. Token counts
are sourced from the slog event stream, which is unsampled — the
slog→OTel bridge fans every record through the bridge handler, and
the bridge does not consult the sampler. Spans correlate cost events
to causal context but do not carry the canonical count. See
[the W6 spec](../engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md)
§9 R6 for the contract.

## Swapping the exporter

The SDK ships exporters for OTLP/gRPC (regatta's default), OTLP/HTTP,
stdout, and several vendor-native protocols. Regatta wires only OTLP
exporters; vendor protocols (Datadog APM, New Relic, Honeycomb's
native protocol) belong at the collector, not in the regatta binary.

To swap exporters in practice:

- **Local development without a backend**: pass `--otel-dev-stdout`.
- **OTLP/HTTP instead of gRPC**: set
  `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`. (Note: regatta's
  current wiring constructs the gRPC exporter; HTTP protobuf is a
  documented hook for the next SDK refresh.)
- **Vendor protocol**: deploy the OTel Collector, configure regatta
  to send OTLP to the collector, and configure the collector's
  exporter pipeline to translate. This is the upstream-recommended
  shape and the one regatta tests against.

## Cardinality

Per-span attributes that regatta emits are bounded (the `gen_ai.*`
set is fixed; resource attributes are constants). Operator-extended
attributes via `OTEL_RESOURCE_ATTRIBUTES` are operator-controlled. If
a backend index starts blowing up, set
`OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT` to cap per-value byte length at
the SDK level. This is the supported OTel knob; regatta does not
enforce a cap of its own.

## Avoiding double-export

If your OTel Collector pipeline routes OTel logs back to a stderr
sink AND your existing slog handler also writes to stderr, you will
see log records duplicated. Two options:

- Configure the collector's filter processor to drop
  `service.name=regatta` log records before the stderr exporter.
- Disable the local stderr handler in regatta's logger config and
  rely solely on the OTel-side sink.

Regatta does not infer which path you want; the local stream is
unchanged by default so operators who do not run a collector still
see their logs.

## Verifying the wiring

After bringing up the local Jaeger fixture:

```sh
docker compose -f examples/observability/docker-compose.yml up -d
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 regatta serve --tick-interval 5s
```

Within one tick you should see at `http://localhost:16686`:

- Service: `regatta`
- Operations: `program`, `tick`, `work_item`, `operator_invocation`,
  `gate.evaluate`, `chat <model>`, `reaper.sweep`.

If no trace appears within ~30 seconds, check:

1. `OTEL_EXPORTER_OTLP_ENDPOINT` includes the scheme
   (`http://` for the local fixture).
2. The Jaeger container is healthy: `docker compose ps`.
3. No firewall is blocking port 4317.

The E2E gate `go test -tags e2e_otel ./internal/obs/otel/...` runs
the same handshake end-to-end against the same docker-compose file;
keep that test green when adjusting the fixture.

## Metrics, dashboards, SLOs

This doc covers the trace + log pipe shipped in W6. The metric layer
(env-var contract for OTLP push vs Prom pull, the seven Grafana
dashboards, SLO-1 + SLO-2 alert wiring, the cardinality budget, and
trace head-sampling knobs) lives in
[observability-metrics.md](observability-metrics.md).

## Where the spec lives

The single source of truth for what regatta emits, what env vars it
reads, and what attributes appear on which span is the W6 design
spec: [docs/engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md](../engineer/specs/2026-05-31-mvp-3-w6-otel-backbone.md).

When this doc drifts from the spec, the spec wins; file an issue.
