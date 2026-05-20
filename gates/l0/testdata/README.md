# L0 fixture corpus

The L0 spec-immutability gate is the only deterministic, mandatory, hard-block
gate in the Regatta stack. Its correctness must be certifiable from fixtures,
not from prose. This directory holds those fixtures.

## Layout

```
testdata/
├── README.md           — this file
├── pass/               — diffs L0 must allow (status flips, citations added)
│   ├── 00_simple_status_flip.diff
│   ├── 01_status_flip_with_citation.diff
│   └── ...
├── fail/               — diffs L0 must block (criterion text edits, etc.)
│   ├── 00_criterion_text_edit.diff
│   ├── 01_invisible_glyph_injection.diff
│   ├── 02_rename_with_text_edit.diff
│   └── ...
└── edge/               — diffs that exercise edge cases (encoding, renames)
    ├── 00_utf8_nfc_normalization.diff
    ├── 01_file_rename_pure.diff
    └── ...
```

## Pass/fail contract

A fixture file is a unified diff applied against the corpus's base tree.
The gate runs with the diff as input and emits a `GateResult` (see
`schemas/gate_result.schema.json`).

- `pass/*.diff`  → expected verdict: `pass`. Empty `findings[]`.
- `fail/*.diff`  → expected verdict: `fail`. At least one finding with
  `severity: critical`. The finding's `trap_pattern` should be `P3` or `P1`
  as appropriate.
- `edge/*.diff`  → expected verdict declared in a sibling `*.expected.json`
  file. These cases exist to nail down behavior that is otherwise easy to
  get wrong (UTF-8 NFC, file renames, encoding round-trips).

## Normative behavior

The L0 gate operates by these rules. Fixtures must demonstrate each.

1. **Diff base.** L0 uses `git merge-base <pr_head> <base_branch>` as the
   diff base. Re-runs at merge time use the merge commit's parent on the
   base side. (Closes the TOCTOU window — see `docs/design.md`
   §Failure modes row "spec mutated on `main` mid-flight".)

2. **UTF-8 NFC normalization.** Both sides of the diff are normalized to
   Unicode NFC before byte-equality comparison. This means visually-identical
   strings expressed in different normalization forms compare equal, and
   the gate cannot be evaded by switching forms.

3. **Byte-equality scope.** L0 compares the *body text* of each acceptance
   criterion, byte-for-byte after NFC. Whitespace inside the body is
   significant. Leading status glyphs (e.g. ☐/⧗/☑ or `- [ ]`/`- [x]`) are
   parsed out before comparison and may change.

4. **Citation extraction.** Each criterion flipping to `done` must carry a
   citation matching `^(test|file|commit)=\S+$`. Multiple citations
   permitted, comma-separated. Missing citation → fail.

5. **File rename handling.** L0 follows rename detection at the standard
   git threshold (50% similarity). A criterion in a renamed file is still
   tracked; a criterion that disappears under rename + diff is a fail.

6. **Invisible-glyph normalization.** Before any comparison, both sides
   have these Unicode ranges stripped:
   - U+200B–U+200D (zero-width characters)
   - U+202A–U+202E (bidirectional overrides)
   - U+2066–U+2069 (isolate controls)
   - U+E0000–U+E007F (Tags block)
   A criterion whose only difference is invisible glyphs is a fail (this
   detects the Rules-File-Backdoor / MCPoison class — see Trap Catalog P10).

7. **Re-run at merge time.** L0 is re-run as a status check on the merge
   commit (not just the PR head). This closes the window where a PR
   passes L0, then `main` is updated to tighten a criterion, then the PR
   merges. The merge-time re-run uses the merged tree as both base and head.

## Adding a fixture

A new fixture lands as a PR touching only this directory plus
`gates/l0/cmd/check-rubric/` (the implementation under test). The L0 unit
test sweeps every `*.diff` in `pass/`, `fail/`, and `edge/` and asserts
against the expected verdict.

## Status

The fixture corpus ships pre-implementation as a contract. Target: 200
fixtures by v1.0 (50/100/50 across pass/fail/edge). At present this
directory is a skeleton; fixture authoring is the second milestone after
L0 implementation lands.
