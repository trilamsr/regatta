---
id: WAVE-4-03
title: formalize G-Eval rubric-as-prompt schema for L4 gate
lane: self-host
status: planned
linked_artifact: https://github.com/trilamsr/regatta/pull/401
---

Source: docs/engineer/research/2026-06-02-wedge-wave-4-emerging-tech.md §2 (Adopt the technique with mandatory cross-family judge) + §0 12-mo prediction + spec/wave-4-amendments F6 (DeepEval/G-Eval row split + cross-family rule promoted)

Brief: G-Eval (NeurIPS '23) is the dominant LLM-as-judge pattern — CoT-prompted form-filling rubric. Per F6 amendment, the cross-family judge rule is mandatory: G-Eval's own paper documents in-family bias, mitigation is to run the judge from a different model family than the implementer. regatta's L4 adversarial-review gate (`internal/gates/`, see S2-T2) must standardize on a CUE-validated rubric-as-prompt schema so judge prompts are versioned, diff-able, swappable. Scope: define rubric schema (CUE), authoring guide, judge-model family-diversity rule enforcement (config-side: refuse to route judge to same family as implementer). Falsifiability gate per §0 12-mo bet-against: this item's payoff is measurable against the 2027-06-01 failure signal (LLM-judge GH Actions surface <5k stars OR <30% OSS Python CI adoption).

## Acceptance criteria

- [planned] c1: CUE schema at `contracts/schemas/judge_rubric.cue` (or sibling path) covering rubric-name, axes, scoring scale, CoT prompt template, judge-model-family pin.
- [planned] c2: Cross-family enforcement: gate config refuses a judge-model whose family matches the implementer model; test covers the refusal path.
- [planned] c3: At least one rubric authored under the new schema (e.g. the existing L4 adversarial-review rubric) and round-tripped through CUE validation + judge invocation in CI.
