# Auditor docs

Reader: customer security team under NDA evaluating Regatta
against an internal audit standard.
Read time: 1 minute (this index).

## Read order

1. [`threat-model.md`](threat-model.md) — adversary stance,
   defended and out-of-scope threat classes.
2. [`data-flow.md`](data-flow.md) — what data crosses which trust
   boundary; what is and is not phoned home.
3. [`reproducibility.md`](reproducibility.md) — build determinism
   guarantees + customer-runnable verification recipe.
4. [`audit-log.md`](audit-log.md) — tamper-evident audit-trail
   contract (stub today; full wire format lands with the
   audit-sink writer).

## Cross-references

- Procurement-level summary: [`SECURITY.md`](../../SECURITY.md).
- Incident catalog driving the trap patterns: [`docs/incidents.md`](../incidents.md).
