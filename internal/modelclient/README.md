# internal/modelclient/

Vendor-neutral ModelClient abstraction. Provider shims live here
once cross-vendor support is needed.

Activation trigger: Phase 3 P3.2 (cross-vendor ModelClient) per
`docs/design.md` phasing - second paying customer + Phase 2 P2.8
in production. Until then, the Anthropic-direct client lives at
`internal/program/provider_anthropic.go`; this directory holds the
interface and second-provider shim when demand materializes.
