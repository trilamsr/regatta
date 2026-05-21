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
   Unicode NFC *after* invisible-glyph stripping (item 6) and before
   byte-equality comparison. This means visually-identical strings expressed
   in different normalization forms compare equal, and the gate cannot be
   evaded by switching forms. Ordering is load-bearing: some stripped code
   points (notably U+034F CGJ and U+200C ZWNJ) actively block NFC
   composition, so a strip-after-NFC ordering would leave their effect
   intact and allow a poisoned vs un-poisoned pair to compare unequal even
   though their stripped forms are identical.

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

6. **Invisible-glyph normalization.** Before NFC and any comparison, both
   sides have all code points in the following set stripped. The set is
   the union of Unicode `Default_Ignorable_Code_Point=Yes`, Unicode
   `Bidi_Control=Yes`, and U+FFFC (Object Replacement Character). Pinned
   to Unicode 15.1; bump requires a fixture refresh. Enumerated ranges:
   - U+00AD (soft hyphen)
   - U+034F (combining grapheme joiner — NFC-bypass primitive)
   - U+061C (Arabic letter mark — bidi)
   - U+115F–U+1160 (Hangul fillers)
   - U+17B4–U+17B5 (Khmer invisible inherent vowels)
   - U+180B–U+180F (Mongolian variation selectors + vowel separator)
   - U+200B–U+200F (zero-width chars + LRM/RLM)
   - U+202A–U+202E (bidi embedding/override)
   - U+2060–U+206F (word joiner, invisible math, isolate controls, deprecated)
   - U+FE00–U+FE0F (variation selectors 1–16)
   - U+FEFF (BOM / ZWNBSP)
   - U+FFF0–U+FFF8 (reserved invisibles)
   - U+FFFC (object replacement)
   - U+1BCA0–U+1BCA3 (shorthand format controls)
   - U+1D173–U+1D17A (musical format controls)
   - U+E0000–U+E0FFF (Tags block + supplementary variation selectors)

   Both sides are stripped before comparison; a criterion whose ONLY
   difference is invisible glyphs from the closure above therefore
   compares equal and the gate verdicts pass. This is the Rules-File-
   Backdoor / MCPoison defense (Trap Catalog P10): invisibles can neither
   smuggle semantic change nor block evasion via NFC, because the strip
   set neutralizes both before any compare.
   Homoglyph / confusable detection (Cyrillic vs Latin `a`, etc.) is
   explicitly *out of scope* for L0 and is handled by a separate
   script-mixing gate. Confusable substitutions are byte-different and
   are caught as plain text edits (see `fail/17_homoglyph_cyrillic_a.diff`).

7. **Re-run at merge time.** L0 is re-run as a status check on the merge
   commit (not just the PR head). This closes the window where a PR
   passes L0, then `main` is updated to tighten a criterion, then the PR
   merges. The merge-time re-run uses the merged tree as both base and head.

## Rules not exercised by .diff fixtures

Rules §1 (diff-base selection) and §7 (merge-time re-run) are properties
of the *harness* that invokes the gate — diff selection happens before
the gate runs and re-run scheduling happens after. A static `.diff`
file cannot exercise either path. Coverage for these lives in the
orchestrator integration tests, not this corpus.

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
