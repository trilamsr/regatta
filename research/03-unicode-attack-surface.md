# Unicode Attack Surface for L0 Spec-Immutability

Status: research, pre-implementation. Feeds the L0 normative-ranges paragraph in `gates/l0/testdata/README.md` and the L0 fixture corpus.

## 1. Threat model recap

L0 must hard-block a class of attack in which an acceptance criterion's *byte sequence* is mutated while its *rendered glyph sequence* is unchanged. Three exemplars define the surface:

- **Trojan Source** (Boucher & Anderson, 2021, CVE-2021-42574): bidi controls reorder visible source so the compiler and the reviewer see different programs.
- **Rules-File Backdoor** (Pillar/HiddenLayer, 2025): zero-width chars + bidi controls in Cursor/Copilot rules files smuggle hidden instructions to the coding agent that the human reviewer never sees.
- **Amazon Q VS Code 1.84.0** (July 2025): a prompt-injection payload was merged into the extension via a PR that passed human review. The wiper prompt was visible ASCII; what the post-mortem made clear is that PR review of agent-touching text is a privileged decision boundary and must not rely on human eyes.

Riley Goodside's January 2024 demonstration that the Unicode Tags block (U+E0000–U+E007F) maps 1:1 to printable ASCII makes the worst case explicit: an attacker can encode arbitrary ASCII instructions in zero rendered glyphs.

L0 is the only deterministic mandatory gate, so its stripping set must be **complete by construction** — i.e., derived from a Unicode property, not from an enumerated incident list. The current four ranges in `testdata/README.md` are derived from incidents, not from properties, and miss roughly two-thirds of the Unicode `Default_Ignorable_Code_Point` set.

## 2. Gap analysis of the current ranges

Current L0 stripping set:

- U+200B–U+200D (ZWSP, ZWNJ, ZWJ)
- U+202A–U+202E (LRE, RLE, PDF, LRO, RLO)
- U+2066–U+2069 (LRI, RLI, FSI, PDI)
- U+E0000–U+E007F (Tags, lower half only)

Concrete gaps, in roughly decreasing exploit-impact order:

| Missing | Why it matters |
|---|---|
| **U+061C** ARABIC LETTER MARK (ALM) | Bidi control. UAX #9's `Bidi_Control=Yes` property has 12 members; ALM is one of them and is not covered by U+202A–U+202E or U+2066–U+2069. Pure Trojan Source variant. |
| **U+200E, U+200F** LRM, RLM | Bidi controls, also `Bidi_Control=Yes`. Visible-direction overrides without push/pop semantics; published Trojan Source PoCs use them. |
| **U+2060–U+2064, U+2066–U+206F** | The full U+2060–U+206F block is `Default_Ignorable`. Includes WORD JOINER (U+2060), FUNCTION APPLICATION (U+2061), INVISIBLE TIMES/SEPARATOR/PLUS (U+2062–U+2064), and the deprecated INHIBIT/ACTIVATE controls (U+206A–U+206F). U+2066–U+2069 is already covered, but the rest is not. |
| **U+FEFF** ZWNBSP / BOM | Zero-width and `Default_Ignorable`. Trivially insertable mid-string. Specifically called out as the deprecation reason for U+2060. |
| **U+00AD** SOFT HYPHEN | `Default_Ignorable`. Renders as nothing in most contexts; in line-wrapping contexts renders as `-` only at break opportunities, so reviewers never see it. Used in the wild as a watermark/poison char in scraped LLM training corpora. |
| **U+034F** COMBINING GRAPHEME JOINER (CGJ) | `Default_Ignorable`. Invisible. Splits otherwise-canonical sequences so that NFC will not recompose them — an *active* NFC-bypass primitive. |
| **U+115F, U+1160** Hangul Jamo fillers | `Default_Ignorable`. Render as nothing or as the empty Hangul base. |
| **U+17B4, U+17B5** Khmer vowel inherent AQ/AA | `Default_Ignorable`. Standard explicitly notes they are encoding errors and "should not be used". |
| **U+180B–U+180F** Mongolian FVS1-4 + MVS | `Default_Ignorable`. Variation selectors + invisible separator. |
| **U+2028, U+2029** LINE/PARAGRAPH SEPARATOR | Not `Default_Ignorable` but are bidi-relevant whitespace; cause `\n`-vs-` ` mismatch in text comparison and are not part of POSIX line semantics. Worth stripping defensively. |
| **U+3164, U+FFA0** HANGUL FILLER, HALFWIDTH HANGUL FILLER | `Default_Ignorable`. Renders blank. Frequently used in Twitter/Discord invisible-username tricks. |
| **U+FE00–U+FE0F** Variation Selectors 1-16 | `Default_Ignorable`. The emoji-presentation selectors. Riley-Goodside-style smuggling has also been demonstrated using VS1-16 to encode 4 bits per char. |
| **U+FFF0–U+FFF8** | `Default_Ignorable`. Unassigned/reserved invisibles. |
| **U+E0080–U+E0FFF** | The *full* Tags + supplementary block is `Default_Ignorable`, not just U+E0000–U+E007F. The current range misses U+E0100–U+E01EF (Variation Selectors 17-256), which are the long-form variation selectors and the exact code points used in 2024 invisible-prompt-injection PoCs. |
| **U+1BCA0–U+1BCA3** Shorthand format controls | `Default_Ignorable`. |
| **U+1D173–U+1D17A** Musical format controls | `Default_Ignorable`. |

Source for the property closure: `DerivedCoreProperties.txt` in the Unicode Character Database, `Default_Ignorable_Code_Point` property, plus `PropList.txt` `Bidi_Control` property from UAX #9.

## 3. Recommended L0 stripping set

The simplest sound rule is: **strip everything with `Default_Ignorable_Code_Point=Yes` OR `Bidi_Control=Yes`**, derived from the Unicode version pinned at build time. Enumerated below for the auditor:

### 3.1 By Unicode property — the normative source

- **`Default_Ignorable_Code_Point=Yes`** — UAX #44 derived property. Members render to "no visible glyph" by Unicode contract; their presence in an acceptance criterion is, by definition, semantically null.
- **`Bidi_Control=Yes`** — UAX #9. 12 code points. Subset overlaps `Default_Ignorable` but not entirely (U+200E, U+200F, U+061C are bidi controls but ARE in the ignorable set too; the union is what we want).

### 3.2 By enumerated ranges (Unicode 15.1, the binding snapshot)

Strip every code point in:

| Range | Block / character | Property | Rationale |
|---|---|---|---|
| U+00AD | SOFT HYPHEN | Default_Ignorable | Invisible at non-break positions |
| U+034F | COMBINING GRAPHEME JOINER | Default_Ignorable | NFC-bypass primitive |
| U+061C | ARABIC LETTER MARK | Bidi_Control + Default_Ignorable | Bidi control missed by current ranges |
| U+115F–U+1160 | HANGUL CHOSEONG/JUNGSEONG FILLER | Default_Ignorable | Renders empty |
| U+17B4–U+17B5 | KHMER VOWEL INHERENT AQ/AA | Default_Ignorable | Per Unicode, encoding errors; invisible |
| U+180B–U+180F | Mongolian FVS1–4 + MVS | Default_Ignorable | Invisible variation selectors |
| U+200B–U+200F | ZWSP, ZWNJ, ZWJ, LRM, RLM | Default_Ignorable (+ Bidi_Control for last two) | Existing range expanded by +U+200E, U+200F |
| U+202A–U+202E | LRE, RLE, PDF, LRO, RLO | Bidi_Control + Default_Ignorable | Trojan Source primary |
| U+2060–U+206F | Word Joiner, invisible math operators, deprecated formatting | Default_Ignorable | Existing U+2066–U+2069 expanded to whole block |
| U+FE00–U+FE0F | VARIATION SELECTOR-1..16 | Default_Ignorable | Smuggling channel |
| U+FEFF | ZWNBSP / BOM | Default_Ignorable | Invisible |
| U+FFF0–U+FFF8 | Reserved invisibles | Default_Ignorable | Reserved but property says ignore |
| U+FFFC | OBJECT REPLACEMENT CHARACTER | (not Default_Ignorable, but recommended) | Placeholder for embedded objects; renders as host-defined glyph in some viewers and nothing in others. Belt-and-braces. |
| U+1BCA0–U+1BCA3 | Shorthand format controls | Default_Ignorable | Invisible |
| U+1D173–U+1D17A | Musical format controls | Default_Ignorable | Invisible |
| U+E0000–U+E0FFF | Tags + supplementary variation selectors | Default_Ignorable | Includes U+E0100–U+E01EF (Variation Selectors 17–256); existing range covered only the lower half |

Additionally, **explicitly do not strip** the bidi marks U+200E/U+200F *when they appear in known-Arabic/Hebrew text* — for L0 this distinction is unsafe and unneeded because we are comparing post-strip canonical forms, not displaying them. The strip is unconditional; a criterion that legitimately needs LRM/RLM is malformed for L0's purposes and should be expressed in plain LTR.

### 3.3 Order of operations

L0 must apply, in this order, on both diff sides:

1. UTF-8 validity check (reject invalid sequences with a hard error, do not silently replace).
2. Strip the set above.
3. NFC normalize.
4. Byte-equality compare.

Strip-before-NFC is correct because some strip targets (CGJ U+034F, ZWNJ U+200C) actively block NFC composition. Stripping after NFC would leave their composition-blocking effect intact in the canonical form. Strip-first dissolves the block.

## 4. Confusables / homoglyphs: out of scope for L0

UTS #39 confusables (Cyrillic а vs Latin a, Greek ο vs Latin o, etc.) are a different attack class: visible glyphs that *look like* other visible glyphs. They share the visual-equivalence theme but differ in three properties critical to L0's design:

- **Non-idempotent under NFC.** Confusables survive any normalization form. They survive NFKC compatibility folding too (those are *different characters*, not compatibility variants).
- **Locale-sensitive false positives.** A Cyrillic acceptance criterion in a Russian-language repo is legitimate Cyrillic, not a confusable. Skeleton-folding it to ASCII would corrupt valid input.
- **Different remediation.** The fix is a *script-mixing* policy (UAX #31 R3 "Restriction Level Detection"), not a strip. Script-mixing detection is more invasive and properly belongs in a higher-numbered Regatta gate ("L2 spec-clarity" or a new "L1.5 spec-readability") with locale config.

**Recommendation:** L0 does NOT do confusables. A separate gate (proposed: **L1.5 spec-script-mixing**) using the UTS #39 confusables skeleton or `unicode-security` (Rust) / ICU `uspoof` (C/Java) handles it. L0's contract is purely "byte-different under canonicalization"; the canonicalization is strip + NFC, nothing more. This keeps L0 fast, deterministic, locally implementable, and free of locale config.

The fixture corpus should include at least one `pass` case where a Cyrillic-looking criterion change does *not* trip L0, to lock this contract in.

## 5. NFC corner cases L0 must absorb

NFC is canonical-composition normalization; it does *not* eliminate every visually-identical pair. Cases the L0 corpus should pin down:

- **Hangul Jamo vs precomposed.** "한" can be one syllable (U+D55C) or three Jamo (U+1112 U+1161 U+11AB). NFC composes them. Verify the fixture set has a `pass` diff that flips one form to the other with no visible change — must NOT alert.
- **Combining character order canonicalization.** "ǭ" can be U+01ED, or U+006F + U+0304 + U+0328, or U+006F + U+0328 + U+0304. NFC canonical-orders combining marks. Fixture should exercise both decomposed orders.
- **CJK Compatibility Ideographs.** These are NFC-stable (CJK compat is an NFKC concern, not NFC); two visually-identical compat ideographs CAN differ under NFC. Acceptable for L0 — they are different characters with different lookups.
- **Singleton mappings.** A few singletons (e.g., U+212B ANGSTROM SIGN → U+00C5) ARE NFC-collapsed. Worth a fixture.
- **CGJ poison.** "café" with U+0065 U+0301 vs "café" with U+0065 U+034F U+0301: NFC will NOT compose the second because CGJ blocks composition. Strip-then-NFC fixes this (per §3.3). Fixture: an attacker inserts CGJ before a combining mark to defeat NFC — must be detected as identical to the un-poisoned form (i.e., the diff is a `pass`, not a `fail`).

## 6. Rendering behavior of upstream review tools

- **GitHub (file blob view):** Since 2021-10-31 displays a yellow banner "This file contains bidirectional Unicode text that may be interpreted or compiled differently than what appears below" when a file contains any `Bidi_Control` character (per the changelog and observed behavior). The banner does NOT highlight the offending line; it does NOT trigger for zero-width / soft-hyphen / tag-block / variation-selector characters; and it does NOT strip anything from rendering. (Source: github.blog changelog 2021-10-31; replication on current PRs confirms behavior unchanged through 2025.)
- **GitHub (PR diff view):** The same banner appears, but per Michael Altfield's 2021 analysis and unchanged since, the diff *does not mark the specific line or character* — the reviewer still has to open the file in a Unicode-revealing editor.
- **GitLab:** Added a similar banner in 2021 in response to CVE-2021-42574; same scope (`Bidi_Control` only).
- **`git diff`:** Renders all Unicode code points literally — including bidi controls, which reorder the diff output itself. There is no `git config` flag to strip or warn. `core.precomposeUnicode` is a macOS *filename* normalization toggle, not content. `core.safeCRLF` is unrelated.

**Implication for L0:** L0 cannot rely on reviewer eyes seeing Unicode hazards; the banner is a soft warning that is routinely banner-fatigued. L0 must do the stripping itself.

## 7. Prior-art libraries

| Library | Language | Covers | Notes |
|---|---|---|---|
| `golang.org/x/text/unicode/norm` | Go | NFC/NFD/NFKC/NFKD | Used by Kubernetes, etcd. Up-to-date with current Unicode. Recommended for the Regatta L0 implementation (Regatta is Go). |
| `unicode/utf8` + property tables from `golang.org/x/text/unicode/runenames` | Go | Property lookup | Pair with `norm` for the strip step. |
| `unicode-security` (crate) | Rust | UTS #39 skeleton, confusables | For the future L1.5 confusables gate, not L0. |
| `unicode_skeleton` (crate) | Rust | UTS #39 skeleton | Older, single-purpose. |
| ICU (`uspoof.h`, `uchar.h`) | C/C++/Java | Property closure, skeleton, full UTS #39 | Heavy dep. Reference implementation. |
| Python `unicodedata` | stdlib | NFC, category, property | Sufficient for Python ports / fixture-gen scripts. |

GitHub's renderer uses an internal Rust-based property checker (per their security-engineering blog 2022); GitLab uses ICU. Neither is open. For Regatta the right pick is `golang.org/x/text/unicode/norm` plus a hand-maintained constant table of the 16 strip ranges in §3.2 — small, auditable, no external dep needed for the strip itself.

## 8. Fixture wish list

To be filed under `gates/l0/testdata/` per the corpus layout. Each entry: `name → expected verdict → what it pins down`.

### fail/ (must block)

- `02_bidi_rlo_swap.diff` → fail. Insert U+202E into a criterion body. Validates LRE/RLE/RLO/PDF detection.
- `03_arabic_letter_mark.diff` → fail. Insert U+061C. Validates that ALM (missed by current range) is stripped/detected.
- `04_lrm_rlm.diff` → fail. Insert U+200E or U+200F. Validates expanded U+200B–U+200F range.
- `05_tag_block_smuggle.diff` → fail. Encode "rm -rf /" in U+E0020–U+E007E. Validates Tag block strip.
- `06_variation_selector_long_form.diff` → fail. Insert U+E0100. Validates U+E0080–U+E0FFF range (missed by current).
- `07_variation_selector_short_form.diff` → fail. Insert U+FE0F into a criterion. Validates U+FE00–U+FE0F.
- `08_word_joiner.diff` → fail. Insert U+2060. Validates expanded U+2060–U+206F range.
- `09_bom_midstring.diff` → fail. Insert U+FEFF mid-text.
- `10_soft_hyphen.diff` → fail. Insert U+00AD between two letters.
- `11_hangul_filler.diff` → fail. Insert U+3164.
- `12_cgj_nfc_poison.diff` → fail. Insert U+034F before a combining mark such that NFC fails to compose. Validates strip-before-NFC ordering.
- `13_mongolian_fvs.diff` → fail. Insert U+180B.
- `14_khmer_inherent.diff` → fail. Insert U+17B4.
- `15_musical_format.diff` → fail. Insert U+1D173.

### pass/ (must allow)

- `02_hangul_precomposed_to_jamo.diff` → pass. Replace U+D55C with U+1112 U+1161 U+11AB in a Korean criterion. NFC equivalence.
- `03_combining_reorder.diff` → pass. Reorder combining marks in canonical-equivalence way.
- `04_singleton_angstrom.diff` → pass. Replace U+212B with U+00C5.
- `05_legitimate_cyrillic.diff` → pass (with status flip). Cyrillic criterion text unchanged, status flips. Pins down "L0 does not do confusables".
- `06_legitimate_emoji.diff` → pass. Criterion contains a flag emoji (regional indicators U+1F1E6+) unchanged across status flip.

### edge/ (declared verdict in sibling `.expected.json`)

- `02_invalid_utf8.diff` → hard error, not pass/fail. Pin down that L0 rejects invalid UTF-8 explicitly rather than silently U+FFFD-replacing.
- `03_strip_then_nfc_vs_nfc_then_strip.diff` → pass. The same payload that fails when normalized-then-stripped must succeed when stripped-then-normalized (per §3.3 ordering).
- `04_object_replacement.diff` → fail. Insert U+FFFC; pins down the belt-and-braces inclusion.
- `05_compat_ideograph.diff` → fail. Replace a CJK character with its compat sibling. Pins down "NFC, not NFKC" — they must register as different.

## 9. References

- Boucher & Anderson, "Trojan Source: Invisible Vulnerabilities", arXiv 2111.00169 / CVE-2021-42574.
- Unicode UAX #9, "Unicode Bidirectional Algorithm" — `Bidi_Control` property.
- Unicode UAX #15, "Unicode Normalization Forms" — NFC definition, Hangul composition.
- Unicode UAX #31, "Unicode Identifiers and Syntax" — identifier restriction levels, script-mixing.
- Unicode UAX #44, "Unicode Character Database" — `Default_Ignorable_Code_Point` property.
- Unicode UTS #36, "Unicode Security Considerations".
- Unicode UTS #39, "Unicode Security Mechanisms" — skeleton, confusables, General Security Profile.
- Unicode UTS #55, "Unicode Source Code Handling" — bidi-stripping and confusables guidance for compilers/tools.
- GitHub Changelog, 2021-10-31, "Warning about bidirectional Unicode text".
- Altfield, "Detecting (Malicious) Unicode in GitHub PRs", 2021-11-22.
- Pillar Security / HiddenLayer, "Rules File Backdoor", March 2025.
- AWS Security Bulletin AWS-2025-015, "Amazon Q Developer for Visual Studio Code", July 2025.
- Goodside, demonstration of Unicode Tags block prompt injection, January 2024; AWS Security Blog, "Defending LLM applications against Unicode character smuggling", 2025.
- `golang.org/x/text/unicode/norm` package documentation; `unicode-security` Rust crate; ICU `uspoof` API.

## 10. Proposed edit to `gates/l0/testdata/README.md`

The current normative paragraph (lines 68–75) reads:

> 6. **Invisible-glyph normalization.** Before any comparison, both sides
>    have these Unicode ranges stripped:
>    - U+200B–U+200D (zero-width characters)
>    - U+202A–U+202E (bidirectional overrides)
>    - U+2066–U+2069 (isolate controls)
>    - U+E0000–U+E007F (Tags block)
>    A criterion whose only difference is invisible glyphs is a fail (this
>    detects the Rules-File-Backdoor / MCPoison class — see Trap Catalog P10).

Proposed replacement:

> 6. **Invisible-glyph normalization.** Before NFC and any comparison,
>    both sides have all code points in the following set stripped. The
>    set is the union of Unicode `Default_Ignorable_Code_Point=Yes`,
>    Unicode `Bidi_Control=Yes`, and U+FFFC (Object Replacement
>    Character). Pinned to Unicode 15.1; bump requires a fixture refresh.
>    Enumerated ranges:
>    - U+00AD (soft hyphen)
>    - U+034F (combining grapheme joiner — NFC-bypass primitive)
>    - U+061C (Arabic letter mark — bidi)
>    - U+115F–U+1160 (Hangul fillers)
>    - U+17B4–U+17B5 (Khmer invisible inherent vowels)
>    - U+180B–U+180F (Mongolian variation selectors + vowel separator)
>    - U+200B–U+200F (zero-width chars + LRM/RLM)
>    - U+202A–U+202E (bidi embedding/override)
>    - U+2060–U+206F (word joiner, invisible math, isolate controls, deprecated)
>    - U+FE00–U+FE0F (variation selectors 1–16)
>    - U+FEFF (BOM / ZWNBSP)
>    - U+FFF0–U+FFF8 (reserved invisibles)
>    - U+FFFC (object replacement)
>    - U+1BCA0–U+1BCA3 (shorthand format controls)
>    - U+1D173–U+1D17A (musical format controls)
>    - U+E0000–U+E0FFF (Tags + supplementary variation selectors)
>
>    Strip happens BEFORE NFC, because some entries in the set (notably
>    U+034F CGJ and U+200C ZWNJ) actively block NFC composition and a
>    strip-after-NFC ordering would leave their effect intact. A
>    criterion whose only difference is invisible glyphs is a fail (this
>    detects the Rules-File-Backdoor / MCPoison class — see Trap Catalog
>    P10). Homoglyph / confusable detection (Cyrillic vs Latin a, etc.)
>    is explicitly *out of scope* for L0 and is handled by a separate
>    script-mixing gate; see `research/03-unicode-attack-surface.md` §4.
