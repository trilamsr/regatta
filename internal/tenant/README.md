# internal/tenant/

Customer-isolation boundary. Anything that could leak between
customer deployments (shared state, default secrets, telemetry
endpoints) lives here so auditors can find the boundary in one
place.

Activation trigger: Phase 3 P3.5 (hosted multi-tenant service) per
`docs/design.md` phasing. Until then, regatta is single-tenant
self-hosted; this directory holds policy stubs only.
