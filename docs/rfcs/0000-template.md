# NNNN. Title goes here

Status: proposed | accepted | superseded by RFC-NNNN
Date: YYYY-MM-DD
Author: name <email>

## Context

What is the situation that requires a decision? Cite the
constraint, the user surface affected, and the decision triggers
from spec section 5 (promotion to contracts/, schema version bump,
third-party dep change, default model/gate/audit-sink change,
post-incident rule change).

Keep this section to a paragraph or two. The reader should be able
to predict the decision before reading the next section.

## Decision

What did we decide? Imperative present. One clear statement.
Optionally, the alternatives considered + why they were rejected
in <=3 bullets each.

## Consequences

What follows from the decision? List in present tense:

- Positive consequence 1.
- Positive consequence 2.
- Negative consequence 1 (cost, friction, debt).
- Activation triggers for follow-up RFCs (if any).

## Compliance

What test, lint rule, workflow, or human ritual will notice if
this decision is violated? No falsifying consumer = the decision
is prose-only and rots (spec D2 + D10).

---

Numbering is monotonic; do not reuse a number. Once Status is
"accepted", do not edit; supersede with a new RFC that links back
via the "superseded by RFC-NNNN" status line.
