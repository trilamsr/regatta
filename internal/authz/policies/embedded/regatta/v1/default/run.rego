package regatta.v1

import rego.v1

# Run-scoped actions default to deny; tenant bundles override per role.
default run.view.decision      := {"allow": false, "reason": "default-deny"}
default run.cost.view.decision := {"allow": false, "reason": "default-deny"}
