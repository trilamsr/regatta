# Observability dev fixture

This directory ships a docker-compose file that runs
[Jaeger all-in-one](https://www.jaegertracing.io/docs/latest/getting-started/)
locally so you can see regatta's traces without provisioning a backend.

The fixture is dev and CI only — production deploys point regatta at
their own collector and route to a long-lived backend (Tempo,
Honeycomb, Datadog, etc.).

## What it is

`docker-compose.yml` brings up one container, `regatta-jaeger`, with:

- OTLP gRPC ingest on `localhost:4317`
- OTLP HTTP ingest on `localhost:4318`
- Jaeger query UI + HTTP API on `localhost:16686`

The image is pinned at `jaegertracing/all-in-one:1.76.0`. Bump
deliberately when the upstream cuts a release that has been validated
against the regatta E2E suite (`go test -tags e2e_otel ./internal/obs/otel/...`).

## How to run it

```sh
docker compose -f examples/observability/docker-compose.yml up -d
```

Wait for the healthcheck to flip the container to healthy
(`docker compose ps` shows `(healthy)`). Then point regatta at it:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317 regatta serve
```

Open the UI in a browser:

```sh
open http://localhost:16686
```

Pick `regatta` from the service dropdown. Each scheduler tick that
materialises a work item creates one trace; click into it to see the
full `program → tick → work_item → operator_invocation → chat <model>`
hierarchy.

## What the operator sees

The trace tree shows every layer regatta opens spans at: the
`program` root, one `tick` per scheduler tick, one `work_item` per
pending row, one `operator_invocation` per spawned agent, one `chat
<model>` per LLM call. The GenAI semantic-convention attributes
(`gen_ai.request.model`, `gen_ai.usage.input_tokens`, etc.) appear on
the chat span; click "Tags" in the Jaeger UI to inspect them.

For the env-var contract, the sensitive-payload policy, sampler
customization, and the cost-reconciliation contract see
[the operator observability doc](../../docs/operator/observability.md).

## Sensitive-payload reminder

This fixture talks to localhost only. If you point regatta at a
shared backend, remember that regatta does NOT emit prompt or
response bodies (`gen_ai.input.messages` / `gen_ai.output.messages`)
by design — see the operator doc's "Sensitive payload policy"
section. Any custom span attributes you add via
`OTEL_RESOURCE_ATTRIBUTES` are your responsibility.

## Teardown

```sh
docker compose -f examples/observability/docker-compose.yml down
```

Jaeger all-in-one runs an in-memory store, so this drops every trace.
For development that wants persistence across restarts, swap to
`jaegertracing/jaeger` (the v2 unified binary) plus a configured
storage backend; that path is out of scope for this fixture.
