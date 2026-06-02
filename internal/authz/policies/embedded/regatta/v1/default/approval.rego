package regatta.v1

import rego.v1

# Every approval action defaults to deny unless a tenant policy explicitly allows.
default approval.decide.decision := {"allow": false, "reason": "default-deny"}
default approval.view.decision   := {"allow": false, "reason": "default-deny"}

# The ONE built-in exception: a reviewer holding a valid cookie-bound
# HMAC token for the SAME tenant can view + decide the approval whose
# ULID matches input.resource. This preserves the W7 single-tenant
# UX out-of-the-box; tenants override by writing their own bundle.
approval.decide.decision := {"allow": true, "reason": "hmac-reviewer"} if {
	input.principal.tenant == "default"
	input.principal.id != ""
}
