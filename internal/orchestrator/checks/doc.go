// Package checks polls `gh pr checks --required` for a set of PRs
// and emits a CheckRun rollup through Emitter whenever the aggregate
// state changes. Failure-wins semantics: "failure" outranks pending
// outranks "success"; Status flips to "completed" only when every
// required check terminates.
//
// The gh subprocess is wrapped by defaultGHTimeout because R31-I4
// stalled the orchestrator sweep loop indefinitely on a network hang
// — the caller's ctx alone is not enough. Poller keeps a last-seen
// map behind a mutex so a slow gh call cannot block siblings, and
// the first observation per PR always emits so no state edge is lost
// on restart. Does NOT persist state; substrate append is the
// Emitter's responsibility (production wires KindGateVerdict per
// spec §3.2).
package checks
