// schemas/regatta.v1.cue
//
// CUE schema for regatta.yaml v1.
//
// Usage:
//   cue vet regatta.yaml schemas/regatta.v1.cue
//   regatta validate-config             # uses this schema internally
//   regatta migrate-config --from 1 --to 2   # planned for v2; see docs/design.md §Versioning
//
// Status: draft v1. The major version of `version:` in the YAML must equal
// the major in this schema's package path. Migration tool ships before any
// v2 release.

package regattav1

import "list"

#Config: {
	version: 1
	repo:          #Repo
	spec_adapter:  #SpecAdapter
	ci:            #CI
	pr_template?:  #PRTemplate
	gates:         [...#Gate] & list.MinItems(1)
	lanes?:        [...#Lane]
	hotspots?:     [...string]
	safety:        #Safety
	context?:      #Context
	telemetry?:    #Telemetry
	prompts?:      #Prompts
	programs?:     #Programs
}

#Repo: {
	host:           "github" | "gitlab"
	owner:          string & =~ "^[A-Za-z0-9_.-]+$"
	name:           string & =~ "^[A-Za-z0-9_.-]+$"
	default_branch: *"main" | string
}

// Spec adapter selector. The `type` field discriminates between the per-type
// schemas below.
#SpecAdapter: {
	type: "github_issues" | "gitlab_issues" | "markdown_catalog" | "jira" | "linear" | "custom"

	if type == "github_issues" || type == "gitlab_issues" {
		selector:            string                  // e.g. "label:planned"
		acceptance_section?: *"## Acceptance" | string
	}
	if type == "markdown_catalog" {
		// Directory containing .regatta/items/*.md, relative to repo
		// root. Default "." matches the self-host layout where items
		// live at <repo>/.regatta/items/.
		root:    *"." | string
		format:  *"github_checkbox" | "rubric"   // - [ ] / - [x]  vs.  ☐/⧗/☑
	}
	if type == "jira" {
		project: string
		jql:     string
	}
	if type == "linear" {
		team:    string
		states:  [...string]
	}
	if type == "custom" {
		command:          string                // executable on PATH
		timeout_seconds?: *30 | int & >0 & <=300
	}
}

#CI: {
	command:          string & =~ ".+"
	timeout_minutes:  *30 | int & >0 & <=180
}

#PRTemplate: {
	citation_section_required: *true | bool
	release_notes_required:    *false | bool
}

#Gate: {
	id:                string & =~ "^[a-z0-9_-]+$"
	type:              "deterministic" | "ai" | "approval_gate"

	if type == "deterministic" {
		command:    string & =~ ".+"
		blocks_on:  *["exit_nonzero"] | [...string]
	}
	if type == "ai" {
		model:               string             // e.g. "claude-opus-4-7"
		// models maps a finding category (e.g. "security",
		// "refactor") to a per-category model override. Unmapped
		// categories fall back to the primary `model`. Used by the
		// L4 adversarial gate to escalate `security` to Opus while
		// keeping `refactor` on a cheaper tier.
		models?:             [string]: string
		prompt?:             string             // path relative to repo root
		severity_block:      [...string] & list.MinItems(1)
		rigorous_label?:     string
	}
	if type == "approval_gate" {
		// See spec §5.3 (YAML shape) + §5.5 (invariants V1-V11). The
		// invariants that CUE cannot cross-reference (V3 window<=timeout,
		// V5 auto_approve⇒low, V7 quorum<=|set|, V9 escalate⇒chain) are
		// enforced by the Go-side validator in internal/config; CUE
		// catches the field-shape ones (enums, regex, presence) so an
		// `cue vet regatta.yaml` rejects them without Go in the loop.

		// V11 — name shape mirrors A2's gateNameRE in
		// internal/gates/approval/config.go.
		name:               string & =~ "^[a-zA-Z0-9_-]{1,64}$"
		// V6 — risk class enum.
		risk_class:         "low" | "medium" | "high"
		// V8 — timeout policy enum.
		on_timeout:         "fail" | "auto_approve" | "escalate"
		reviewers:          *[] | [...string & =~ "^[a-zA-Z0-9_:.-]{1,128}$"]
		roles?:             [...string]
		// Quorum lower bound. Upper bound is per-config (cannot exceed
		// |reviewers∪roles|) and lives in the Go validator (V7).
		quorum:             int & >=1
		prevent_self_review?: *false | bool
		// Durations are YAML strings parsed by time.ParseDuration.
		// CUE regex pins the surface syntax so a typo like "24hours"
		// fails at the schema layer.
		timeout:            string & =~ "^[0-9]+(ns|us|µs|ms|s|m|h)$"
		decision_window:    string & =~ "^[0-9]+(ns|us|µs|ms|s|m|h)$"
		// V9 — escalate requires a non-empty chain. The reverse
		// (chain present ⇒ on_timeout=escalate) is NOT enforced;
		// operators may staff a chain for future toggles.
		if on_timeout == "escalate" {
			escalation_chain: [...#ApprovalTier] & list.MinItems(1)
		}
		if on_timeout != "escalate" {
			escalation_chain?: [...#ApprovalTier]
		}
		// V5 — auto_approve foot-gun: requires risk_class=low.
		if on_timeout == "auto_approve" {
			risk_class: "low"
		}
		predicate_cel?:     string
	}
}

// #ApprovalTier is one rung of an escalation chain. Same duration
// regex + quorum lower bound as the top-level gate; cross-field
// invariants (window<=timeout) live in Go.
#ApprovalTier: {
	reviewers:          *[] | [...string & =~ "^[a-zA-Z0-9_:.-]{1,128}$"]
	roles?:             [...string]
	quorum:             int & >=1
	prevent_self_review?: *false | bool
	timeout:            string & =~ "^[0-9]+(ns|us|µs|ms|s|m|h)$"
	decision_window:    string & =~ "^[0-9]+(ns|us|µs|ms|s|m|h)$"
}

#Lane: {
	id:               string & =~ "^[a-z0-9_-]+$"
	paths:            [...string] & list.MinItems(1)
	max_concurrency:  *1 | int & >=1 & <=8
}

#Safety: {
	destructive_ops_deny:    *[] | [...string]
	agent_creds_scope:       *"dev_only" | "test" | "scoped"
	iteration_cap:           *50 | int & >=1 & <=500
	spend_cap_usd:           *50 | int & >=0
	spend_cap_usd_per_day:   *200 | int & >=0
	canary_rate:             *0.05 | float & >=0 & <=0.2
	// soft_cap_mode is the cost-governor soft-cap (80% threshold)
	// posture. `enforce` (default) treats every soft-cap breach as a
	// deny-or-downgrade decision per work_item annotation. `warn`
	// permits the spawn past the soft-cap with only a slog/OTel
	// breach event — a silent-correctness regression vector flagged
	// in PR #211 adversarial review. To prevent accidental opt-in,
	// `warn` requires `soft_cap_acknowledge_overrun: true` (Go-side
	// validator returns ErrSoftCapNotAcknowledged otherwise). Spec
	// §3.6 + issue #226.
	soft_cap_mode:                *"enforce" | "warn"
	// soft_cap_acknowledge_overrun is the explicit operator
	// acknowledgement that `soft_cap_mode: warn` permits cost overruns
	// past the soft cap. No-op when mode is `enforce` (the default);
	// load-bearing only when paired with `warn`. See issue #226.
	soft_cap_acknowledge_overrun: *false | bool
	cost?:                   #CostGovernor
	authz?:                  #Authz
}

// #Authz configures the OPA policy hot-reload surface. Slim single-tenant
// W8 spec (docs/engineer/specs/2026-06-02-s3-t1-w8-opa-slim.md §3.5).
// All fields optional; empty block ⇒ embed.FS default-deny only, no
// watcher, no SIGHUP handler.
#Authz: {
	// Absolute or repo-relative path to <policy_dir>/regatta/v1/default/.
	// Empty ⇒ serve embed.FS bundle and skip both reload triggers.
	policy_dir?:      string

	// fsnotify event coalesce window. Vim atomic-rename + multi-file
	// saves emit storms that 250 ms collapses safely.
	reload_debounce?: *"250ms" | =~"^[0-9]+(ms|s)$"

	// SIGHUP trigger toggle. Operator opts out for windows / CI / any
	// process that already owns SIGHUP (HR3 mitigation).
	reload_sighup?:   *true | bool

	// fsnotify watcher toggle. Operator opts out on filesystems where
	// inotify / kqueue is unreliable (NFS, certain container overlays).
	reload_fsnotify?: *true | bool
}

// #CostGovernor is the optional MVP-4 cost-governor block (spec §3.6).
// Backwards-compatible: unset means MVP-2 byte-equal behaviour. Every
// cap field is optional; the validator (internal/config/validate/cost.go)
// rejects an empty block and the all-caps-zero trap.
#CostGovernor: {
	per_dag_usd?:               int & >=0
	per_operator_usd?:          int & >=0
	per_work_item_usd?:         int & >=0
	period?:                    "1h" | "1d" | "7d" | "30d"
	soft_pct:                   *80 | int & >=50 & <=99
	reconcile_interval:         *"1h" | "5m" | "15m" | "30m" | "6h" | "24h"
	drift_alert_threshold_pct:  *10 | int & >=0 & <=100
	usage_api_key_env:          *"ANTHROPIC_ADMIN_KEY" | string
	// estimation_strategy is the opt-in flag from spec §10 S1
	// (issue #238). Default upper_bound matches Wave-1 behaviour
	// byte-for-byte; setting `history` switches to p95-of-cohort
	// estimation with cold-start fallback to upper_bound when
	// fewer than ~10 prior (tenant, operator, model) samples
	// exist. Additive — no breaking change for existing configs.
	estimation_strategy:        *"upper_bound" | "history"
	// Optional path to a JSON file that overrides or extends the
	// hardcoded pricing table at boot. Per-key merge — each model in
	// the override file replaces the corresponding hardcoded row;
	// rows not present in the override are untouched. Adds new SKUs
	// (Bedrock/Vertex/marketplace) the upstream-mirror table cannot
	// carry. Hard-fails on malformed JSON, unknown fields, non-positive
	// rates, or world-writable file mode (R14 mitigation). Refresh
	// via in-tree PR is still the default; this is the escape hatch
	// for operators who cannot fork. Spec §10 S2 + §3.8.
	pricing_override_path?:     string
}

#Context: {
	trusted_doc_paths?:               [...string]
	agent_guidance_path?:             string
	agent_guidance_codeowners_check:  *true | bool
}

#Telemetry: {
	digest_path:    *"docs/regatta-digest.md" | string
	state_db_path:  *"./.regatta/regatta.db" | string
	audit_sink?:    string   // S3 URL with object-lock or transparency-log endpoint
	audit_chain?:   *true | bool   // hash-chain audit records (Merkle); when false, only per-record HMAC
}

// Prompts pins SHA-256 of each signed prompt artifact. Mismatch
// at load time fails closed. Empty means use the embedded fallback
// constant baked into the binary (build-hermetic).
#Prompts: {
	planner_sha?:        string & =~ "^[0-9a-f]{64}$"
	security_gate_sha?:  string & =~ "^[0-9a-f]{64}$"
	agent_brief_sha?:    string & =~ "^[0-9a-f]{64}$"
}

// Programs configures the multi-feature decomposition layer.
// See docs/design.md §Programs.
#Programs: {
	dir:      *".regatta/programs" | string  // where brief loader polls for *.json
	enabled:  *true | bool                   // when false, kind=program work items halt
}

// Toplevel: every regatta.yaml validates as #Config.
#Config
