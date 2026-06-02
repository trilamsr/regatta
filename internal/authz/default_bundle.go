package authz

import "context"

// defaultLoader serves the inline default-deny baseline bundle for the
// "default" tenant. T4 will ship the canonical embed.FS baseline +
// onboarding doc; T1's loader keeps this package self-contained until
// T4 lands (file-disjoint per plan §1).
type defaultLoader struct {
	sha   string
	files map[string]string
}

func newDefaultLoader() *defaultLoader {
	files := defaultBundleFiles()
	return &defaultLoader{sha: bundleSHA(files), files: files}
}

func (d *defaultLoader) Tenants(_ context.Context) ([]string, error) {
	return []string{DefaultTenant}, nil
}

func (d *defaultLoader) ActiveBundle(_ context.Context, tenant string) (string, map[string]string, error) {
	if tenant != DefaultTenant {
		// Unknown tenant — return empty triple so Hydrate / Reload
		// skip; Check returns ErrTenantUnknown via store lookup miss.
		return "", nil, ErrPolicyMissing
	}
	out := make(map[string]string, len(d.files))
	for k, v := range d.files {
		out[k] = v
	}
	return d.sha, out, nil
}

// defaultBundleFiles returns the spec §3.5 baseline modules — every
// action defaults to deny; the ONE exception is approval.{view,decide}
// for a non-empty principal in the "default" tenant (HMAC-reviewer
// rule preserves the W7 single-tenant UX out-of-the-box).
func defaultBundleFiles() map[string]string {
	return map[string]string{
		"regatta/v1/default/approval_view.rego":   approvalViewRego,
		"regatta/v1/default/approval_decide.rego": approvalDecideRego,
		"regatta/v1/default/run_view.rego":        runViewRego,
		"regatta/v1/default/run_cost_view.rego":   runCostViewRego,
	}
}

// Rego packages: spec §3.2 queries data.regatta.v1.<action>.decision.
// Action strings contain dots ("approval.decide") so we split on dot
// and use the segments as nested package components.
const approvalViewRego = `package regatta.v1.approval.view

default decision := {"allow": false, "reason": "default-deny"}

decision := {"allow": true, "reason": "hmac-reviewer"} if {
	input.principal.tenant == "default"
	input.principal.id != ""
}
`

const approvalDecideRego = `package regatta.v1.approval.decide

default decision := {"allow": false, "reason": "default-deny"}

decision := {"allow": true, "reason": "hmac-reviewer"} if {
	input.principal.tenant == "default"
	input.principal.id != ""
}
`

const runViewRego = `package regatta.v1.run.view

default decision := {"allow": false, "reason": "default-deny"}
`

const runCostViewRego = `package regatta.v1.run["cost.view"]

default decision := {"allow": false, "reason": "default-deny"}
`
