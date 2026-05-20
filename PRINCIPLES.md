# Principles

Rules in [`STYLE.md`](STYLE.md) tell you *what* to do. This
document explains *why* and gives you the lenses to make new
decisions consistently when no rule exists yet.

It is short on purpose. Word count should not grow with the
codebase.

---

## 1. Trust under fire is the product

Regatta runs autonomous agents that open PRs against repos
operators care about. Operators install us only because they
trust the gate stack. Trust is built in mechanism, not docs:

- **Agents never bypass the gate stack.** Every PR an agent
  opens goes through L0 to L5 with human merge at L6. Bypass
  mechanisms (force-push, admin merge, gate skip) are absent
  by construction, not just discouraged in prose.
- **Deterministic gates run first.** Schema, signature, lint,
  test exit code. AI judges only see candidates that already
  passed every deterministic check.
- **Audit logs are tamper-evident and out-of-band.** A
  compromised in-band log surface is a known incident class;
  regatta writes audit records where the agent cannot reach.

Whenever you write code, ask: *if this misbehaves on a repo we
don't own, does the maintainer trust us less, or thank us for
the breadcrumbs?*

## 2. Reversibility before optionality

Most engineering choices are easy to add and hard to remove.
Default to *not* adding.

- Don't ship features without a user.
- Don't add abstractions before the second concrete use.
- Don't add config knobs to "future-proof"; the future will
  tell you what knobs to add.
- Don't introduce a dependency that solves a problem you might
  have.

When you face a "should we add X now or later?" question, the
answer is **later** unless removing it later is materially
harder than adding it later.

## 3. One mechanism over many

For every concern, there should be one well-known way to do it.
Two ways means contributors guess, drift, and fork conventions.

- `make check` is the single source of truth for what's
  verified locally and in CI.
- Every gate emits findings in one schema (`GateResult`); no
  side-channel formats.
- Every commit follows DCO sign-off and the same imperative-
  present subject style, agent or human.

When you find yourself writing "but for X we do it differently
because...", check whether the difference is load-bearing or
whether you're inventing complexity.

## 4. Don't police what you don't have

Every rule, lint, validator, or test you add is a permanent tax
on every contributor and every PR. Add it only when:

1. The rule has bitten us in real work, **or**
2. The cost of getting it wrong on the first occurrence is much
   higher than the cost of catching it then.

When you propose a new check, the burden of proof is on the
proposer to show the rule is load-bearing *now*.

## 5. The linter is law; don't duplicate it in prose

Anything a linter can catch should not also be in `STYLE.md` as
a rule for humans to remember. Humans forget; CI doesn't.
Agents have the opposite failure mode: they will follow a prose
rule once and ignore it later. Mechanism trumps prose for both
audiences.

Corollary: when you do add a rule, prefer enforcement over
documentation. A linter rule is worth ten paragraphs of style
guide. A `PreToolUse` hook is worth ten saved memories.

## 6. Defaults bias toward private

`internal/` first; `pkg/` only by RFC. Unexported types first;
export only on demand. Single-binary first; multiple binaries
only when something forces a split.

Public APIs are forever. Private code is editable forever.
Choose editability until you have a concrete external consumer.

## 7. Comments earn their place

Default to writing none. Add one only when *removing* it would
confuse a future reader.

A comment must explain something the code cannot:

- A non-obvious *why* (a workaround for a specific bug, a
  constraint from outside, a subtle invariant).
- A trap a contributor (or agent) might walk into if they
  edited naively.

A comment must not:

- Restate the code.
- Reference a current task, ticket, or PR (those rot; use git
  blame).
- Claim "this is performance-critical" without a benchmark.
- Apologize for the code (fix it instead).

The same principle applies to documentation. A doc that
re-states what a config struct already says is rot waiting to
be noticed.

## 8. Names earn their slot

A name should let a reader skip reading the body. If a function
is called `process()` and the caller has to read it to
understand what it processes, the name is wrong.

- No abbreviations except by language convention.
- No prefix-style suffixes on type names (`UserStruct`,
  `LoggerInterface`).
- Package names are lowercase, single-word, no underscores. The
  package name is part of the call site, so don't repeat it in
  symbols.

`GateResult` fields are read by both humans and agents - they
deserve the same naming discipline as any public API.

## 9. Failure modes are part of the API

Every function that can fail must say *how* it fails - through
its return type, sentinel errors, or wrapped errors. A function
that returns `error` without context is a bug waiting to be
filed.

- Wrap with context everywhere. The chain is the call stack.
- Exported sentinel errors when callers need to match.
- Never swallow an error to make a return type nicer. Wrap or
  escalate.

Gates fail in shapes the agent needs to route around. A gate
that returns "false" without saying "why" is unactionable. The
agent's circuit-breaker (K=3 rejections -> escalate) depends on
each rejection being attributable to a specific check.

## 10. Fast feedback over thorough feedback

A test suite that takes a minute and runs every commit beats a
test suite that takes ten and runs nightly.

- `make check` must complete in under 60 seconds on a developer
  laptop.
- Gate-stack latency is agent feedback latency. L3 / L4 / L5
  should target under 5 minutes per gate (summed ~15 minutes
  per PR) so the agent loop stays tight.
- Benchmarks are advisory, not gate-keeping. Slow CI burns
  trust faster than missing coverage.

## 11. Backwards compatibility is something you opt into

We are pre-1.0 and explicit about it. Every release before
v1.0.0 may break.

After v1.0.0:

- Public Go APIs follow semver.
- The `GateResult` schema and the `SpecAdapter` interface
  follow a deprecation cycle: warn for one minor version, fail
  in the next.
- Agent prompt schema and signature pinning are operator-
  visible and break loudly when they change.

Until v1.0.0, breaking changes go in the CHANGELOG and the
release notes. After it, they require a new major.

## 12. Reproducibility is a feature, not a bonus

Two runs of the same agent against the same work item, with the
same prompt SHA and the same target-repo SHA, must produce
gate-stack verdicts that differ only by genuine non-
determinism in the agent itself (and that non-determinism
itself must be visible in the audit log).

Mechanisms (when binary ships):

- `-trimpath` always.
- `SOURCE_DATE_EPOCH` honoured; falls back to git commit time,
  never to `now`.
- Agent prompt SHA pinned; mismatch fails closed.

Reproducibility breakage is a P0 bug.

## 13. The human reviewer is owed transparency

The L6 human merger must be able to understand why the gate
stack approved a PR without re-running every gate. Bake this in
from day one:

- Every gate writes its verdict, inputs, and rationale to the
  PR conversation in one parseable format.
- Every agent action writes to the audit log with enough
  context to identify the work item, the prompt SHA, and the
  decision tree.
- Logs are structured and queryable.

The L6 reviewer is your primary user. Optimize for the
five-minute "is this safe to merge?" they do at the end of
every cycle.

## 14. Honest commits, honest history

Each commit must build (when there is a binary), pass tests,
and tell a clear story. This applies to agent-authored commits
identically.

- Imperative present tense subject, 72 char cap.
- Body explains the *why*, not the *what*.
- DCO sign-off on every commit.
- Atomic: don't bundle a refactor with a feature; don't slip a
  fix into a doc change.

Future-you will read git history more times than future-you
will read any doc. Make it readable. Agents that bundle
unrelated changes get the same feedback a human would.

## 15. Decide late, write it down, revisit honestly

Every load-bearing decision goes in an RFC. Every RFC has a
status (`draft`, `accepted`, `rejected`, `superseded`).
Decisions are made on the latest information; the latest
information changes.

When a decision is wrong:

- Open a new RFC. Mark the old one `superseded by RFC-NNNN`.
- Update the code in the same PR or a directly-following one.
- Don't quietly diverge from a written rule. Either follow it
  or change it.

If you find yourself making the same kind of decision twice,
write it down. If you find yourself defending a written
decision that no longer makes sense, supersede it.

An agent that encounters an RFC ambiguity halts and files a
clarification item rather than guessing. Guessing is a
governance failure mode the L6 reviewer cannot catch.

---

## When this guide is wrong

These are principles, not laws. When applying one would produce
a worse outcome than ignoring it, ignore it - and open an RFC
explaining why. The next reader needs the same shortcut.

When you find a contradiction between this document and
reality, reality wins. Update this document.
