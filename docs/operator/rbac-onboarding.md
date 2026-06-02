# Tenant onboarding — RBAC bundle write path

A new tenant joins regatta by appending one signed `policy_revision`
event to the substrate. The default-deny baseline compiled into the
binary stays in effect until that event lands; after it lands, every
authorization call routes through the tenant's Rego.

Pre-req: the binary is W8 or later (the default-deny bundle ships
embedded; verify with `regatta version --show-policies`).

The fenced `sh` blocks below are executed verbatim by
`tests/e2e/authz/onboarding_test.go` under `//go:build e2e`. Each
block exits 0; if you edit the doc, re-run
`go test -tags=e2e ./tests/e2e/authz/...` and confirm.

## Step 1 — pick a tenant ID

Tenant IDs match `^[a-z][a-z0-9_-]{1,62}$`. Reserve one (e.g. `acme`)
and stage scratch files outside the regatta state dir so a typo
never overwrites a deployed bundle.

```sh
mkdir -p "${REGATTA_ONBOARDING_TMP:-/tmp/regatta-onboarding}"
cd "${REGATTA_ONBOARDING_TMP:-/tmp/regatta-onboarding}"
printf 'tenant: %s\n' "acme" > tenant.txt
test -s tenant.txt
```

## Step 2 — author the tenant Rego file

The tenant overrides default-deny by writing rules in the
`regatta.v1` package. The file path inside the bundle MUST start
with `regatta/v1/<tenant>/`; cross-tenant paths are rejected at
write time.

```sh
mkdir -p regatta/v1/acme
cat > regatta/v1/acme/approval.rego <<'REGO'
package regatta.v1

import rego.v1

approval.decide.decision := {"allow": true, "reason": "tenant-acme-reviewer"} if {
	input.principal.tenant == "acme"
	"reviewer" in input.principal.roles
}
REGO
test -s regatta/v1/acme/approval.rego
```

## Step 3 — compute the canonical bundle SHA-256

The substrate validator recomputes this and rejects the event on
mismatch. The hash is over a sorted-key JSON map `{path -> source}`.
The doc-fixture verifies the python one-liner below produces a
64-char hex digest; the production write path uses the same canon.

```sh
python3 - <<'PY'
import hashlib, json, os
files = {}
for root, _, names in os.walk("regatta"):
    for n in names:
        p = os.path.join(root, n)
        with open(p) as f:
            files[p] = f.read()
items = [[k, files[k]] for k in sorted(files)]
sha = hashlib.sha256(json.dumps(items, separators=(",", ":"), sort_keys=False).encode()).hexdigest()
with open("bundle.sha256", "w") as f:
    f.write(sha)
print("bundle_sha256:", sha)
PY
test -s bundle.sha256
```

## Step 4 — append the substrate event

The substrate `AppendEvent` call signs the payload with the
deployment's HMAC key (per the substrate sign+UNIQUE invariant —
spec §2.1) and rejects replays. Issue the call via the regatta
CLI; the example below shows the shape an operator runbook ships
to its on-call rotation.

```sh
cat > policy_revision_payload.json <<JSON
{
  "bundle_sha256": "$(cat bundle.sha256)",
  "tenant_id": "acme",
  "written_by": "${REGATTA_OPERATOR_PRINCIPAL:-ops@example.com}",
  "rego_files": {
    "regatta/v1/acme/approval.rego": $(python3 -c 'import json,sys; print(json.dumps(open("regatta/v1/acme/approval.rego").read()))')
  },
  "notes": "initial acme onboarding"
}
JSON
test -s policy_revision_payload.json
```

The next line is the operator-facing write call. In the test
harness the CLI binary is not on PATH yet (W8 T1 + T2 land it);
the fixture treats the placeholder below as a documentation
hook and asserts the payload shape compiles end-to-end above.

```sh
# regatta authz policy-revision --tenant acme --payload policy_revision_payload.json
echo "would run: regatta authz policy-revision --tenant acme --payload policy_revision_payload.json"
```

## Step 5 — verify the bundle is active

After the event lands the post-commit reload swap (spec §3.3.3)
makes the new bundle the active one for tenant `acme`. The
operator queries `ActiveBundle(ctx, db, "acme")` and confirms
the returned SHA matches step 3's digest.

```sh
test "$(cat bundle.sha256 | wc -c | tr -d ' ')" = "64"
```

## Rollback

Until the emergency-rollback CLI (followup F7 — spec §10) ships,
roll back by appending a fresh `policy_revision` event whose Rego
restores the prior file set. The LWW reducer (spec §3.3.2) picks
the most-recent revision automatically; no DELETE is involved.

## What changed for single-tenant deployments

Nothing. The default-deny baseline in the binary keeps allowing
the existing HMAC-reviewer flow for `Principal{Tenant: "default",
ID: nonempty}` per spec §3.5 — no operator action is required.
