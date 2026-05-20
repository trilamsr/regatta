# 08 — Adversarial Cross-Check of Research Wave 01–06

- **Status:** Adversarial review pass against the six breadth-research files dropped into `research/` during the Regatta v3.1 design refresh.
- **Author:** Adversarial reviewer agent.
- **Created:** 2026-05-20.
- **Method:** Pick 3–5 load-bearing claims per file; for each, independently search for confirming and *contradicting* sources; render a verdict (CONFIRMED, NUANCED, CONTRADICTED, UNVERIFIABLE); call out missed sources. Lean toward NUANCED/CONTRADICTED — the breadth agents optimize for "I found something," my job is "is it complete and true."

The TL;DR up front: the wave is mostly accurate at the *headline* level but routinely overclaims at the *quantitative* and *scope* levels. File 02 (judge-bias) is the strongest. Files 04 (branch protection) and 03 (Unicode) are mechanically rigorous. Files 01 (incidents), 05 (spec adapters), and 06 (eval harness) each contain at least one claim that is technically true in spirit but materially overstated in detail. Specific issues below.

---

## 01 — Incident Catalog Refresh

### Load-bearing claims checked

| Claim | Verdict | Independent source | Note |
|---|---|---|---|
| **#20 Anthropic mcp-server-git triple-CVE (CVE-2025-68143/68144/68145), reported June 2025, patched Dec 2025, disclosed Jan 20 2026** | **CONFIRMED** | [The Register 2026-01-20](https://www.theregister.com/2026/01/20/anthropic_prompt_injection_flaws/); [Hacker News 2026](https://thehackernews.com/2026/01/three-flaws-in-anthropic-mcp-git-server.html); [SocRadar](https://socradar.io/blog/anthropic-git-mcp-server-vulnerabilities/) | Dates, CVE IDs, vuln types (2 path-traversal + 1 arg-injection), and the prompt-injection delivery vector all match. Solid. |
| **#21 TrustFall: one-keypress RCE across Claude Code, Gemini CLI, Cursor CLI, Copilot CLI; default trust dialog button; three of four vendors classified as "working as designed"** | **NUANCED** | [Adversa](https://adversa.ai/blog/trustfall-coding-agent-security-flaw-rce-claude-cursor-gemini-cli-copilot/); [The Register](https://www.theregister.com/security/2026/05/07/claude-code-trust-prompt-can-trigger-one-click-rce/5235319) | Affected tools, mechanism, and Anthropic's "user gave informed consent" position are confirmed. Caveat: the agent's claim that "three of four vendors classified this as working as designed" is *not* in the Adversa or Register write-ups I located — those describe Anthropic's response specifically. Vendor-by-vendor breakdown is fuzzier than the agent implies. |
| **#27 Kiro / AWS Cost Explorer 13-hour outage, Dec 15 2025, mainland China, agent deleted prod, FT contradicted Amazon's framing** | **NUANCED — disputed attribution** | [Incident DB #1442](https://incidentdatabase.ai/cite/1442/); [The Register 2026-02-20](https://www.theregister.com/2026/02/20/amazon_denies_kiro_agentic_ai_behind_outage/); [GrowthHQ summary](https://www.growthhq.io/our-thinking/aws-cost-explorer-outage-in-mainland-china-human-error-not-kiro-ai-blamedkey-lessons-for-business-leaders) | The 13-hour duration and China-only scope are confirmed. The agent's framing presents the Kiro-deleted-prod theory as fact and Amazon's denial as misdirection. In reality both narratives have published sources; Amazon's official statement is "user (AWS employee) error - specifically misconfigured access controls - not AI." This *exact wording* should appear in `incidents.md` as a footnote — Regatta is building a defense on a story the responsible party publicly denies. Design.md should note the attribution dispute. |
| **#28 Antigravity D-drive wipe via Turbo mode; `rmdir /s /q d:\`; path parsing bug** | **CONFIRMED** | [Tom's Hardware](https://www.tomshardware.com/tech-industry/artificial-intelligence/googles-agentic-ai-wipes-users-entire-hard-drive-without-permission-after-misinterpreting-instructions-to-clear-a-cache-i-am-deeply-deeply-sorry-this-is-a-critical-failure-on-my-part); [Windows Central](https://www.windowscentral.com/artificial-intelligence/google-antigravity-ai-delete-drive); [OECD.AI 2025-11-30](https://oecd.ai/en/incidents/2025-11-30-d838) | Drive letter (D:), the brutal /q flag, Turbo mode skipping confirmation, and the apology all match. Agent's reporting here is tight. |
| **#29 Claude Cowork PromptArmor disclosure; 1pt white-on-white text smuggle; partial SSNs uploaded to attacker's Anthropic account; flaw acknowledged ~3 months earlier** | **CONFIRMED** | [The Register 2026-01-15](https://www.theregister.com/2026/01/15/anthropics_claude_bug_cowork/); [PromptArmor](https://www.promptarmor.com/resources/claude-cowork-exfiltrates-files); [CUInfoSecurity](https://www.cuinfosecurity.com/anthropics-cowork-shipped-known-vulnerability-a-30553) | All four specifics line up: invisible-text vector, the partial-SSN exfil PoC, the api.anthropic.com whitelist abuse, and the "reported earlier and not patched" timeline. |
| **#30 Mini Shai-Hulud worm; "GitHub Actions cache trusted as build input without integrity verification; provenance signing infrastructure didn't catch a compromised-but-legitimate CI run"** | **NUANCED — root cause is broader** | [The Hacker News 2026-05](https://thehackernews.com/2026/05/mini-shai-hulud-worm-compromises.html); [StepSecurity](https://www.stepsecurity.io/blog/mini-shai-hulud-is-back-a-self-spreading-supply-chain-attack-hits-the-npm-ecosystem); [Wiz](https://www.wiz.io/blog/mini-shai-hulud-strikes-again-tanstack-more-npm-packages-compromised) | The agent reduces the attack to "cache poisoning + signed bad artifact." The actual chain is **three** vulnerabilities: `pull_request_target` workflow abuse on a forked PR, GitHub Actions cache poisoning, *and* OIDC-token extraction from `/proc/<pid>/mem` in the runner. SLSA Build L3 attestations were valid because the token was lifted from a legitimate run, not because cache poisoning forged provenance. The agent's "P1 needs SBOM + build-attestation + age gate" recommendation is fine, but the diagnosis as written is missing the OIDC-token extraction step, which is the part of the chain that branch-protection-style controls *cannot* close. |
| **#31 Lovable BOLA, Feb 3 → Apr 20 2026, HackerOne reports closed as duplicate/intended behavior** | **CONFIRMED** | [Lovable postmortem](https://lovable.dev/blog/our-response-to-the-april-2026-incident); [The Register](https://www.theregister.com/2026/04/20/lovable_denies_data_leak/); [Cyber Kendra](https://www.cyberkendra.com/2026/04/lovable-left-thousands-of-projects.html); [The Next Web](https://thenextweb.com/news/lovable-vibe-coding-security-crisis-exposed) | Timeline (Feb 3 → Apr 20, 48 days), the "closed as intentional behavior" detail, and the eventual partial apology are all on the record. P12 candidate (vuln-intake escalation) is well-grounded. |

### Design-doc impact

The #20 (MCP-Git), #28 (Antigravity), #29 (Cowork), and #31 (Lovable) entries can be lifted into `incidents.md` and design.md with no caveat needed. The #21 (TrustFall) and #27 (Kiro) entries should keep their factual core but soften the framing — both involve vendor denials/disputes the agent currently writes around. The #30 (Shai-Hulud) entry's P1-tightening recommendation should be supplemented with a separate observation that OIDC-token extraction from runner memory is not closed by SBOM/attestation gates — that's a runner-isolation problem the design doc currently doesn't address.

The proposed P11/P12/P13 patterns are well-supported by the underlying incidents — confirmed enough to promote, with the caveat above on #30.

---

## 02 — LLM-Judge Bias

This is the strongest file in the wave. Numbers were cross-verifiable to the digit, not just directionally.

### Load-bearing claims checked

| Claim | Verdict | Independent source | Note |
|---|---|---|---|
| **Preference Leakage (arXiv 2502.01534) Table 2 PLS: same model 23.6%, inheritance same-instr 19.3%, inheritance diff-instr 22.3%, same-family same-series 8.9%, same-family diff-series 2.8%** | **CONFIRMED** (digit-for-digit) | [arXiv 2502.01534](https://arxiv.org/abs/2502.01534); confirmed via paper-notes summary at [opentrain.ai](https://www.opentrain.ai/papers/preference-leakage-a-contamination-problem-in-llm-as-a-judge--arxiv-2502.01534/) | Numbers match exactly; per-benchmark Arena-Hard / AlpacaEval split also matches (28.7%/18.4% same-model, 10.1%/7.6% same-series, 3.3%/2.2% diff-series). |
| **JudgeBench (2410.12784): GPT-4o ≈56% on hard pairs vs ≈80% on MT-Bench easy pairs; family bias dwarfed by capability variance** | **NUANCED** | [arXiv 2410.12784](https://arxiv.org/abs/2410.12784); [paper PDF](https://arxiv.org/pdf/2410.12784) | The "~56%" framing is right (search results report 53.9% on Claude-Sonnet pairs); but JudgeBench was constructed by *using GPT-4o to generate pairs*, which the original paper explicitly flags as introducing **bias against GPT-4o judges** on its own benchmark. The agent's claim that "capability dominates family on hard tasks" is therefore measured on a benchmark that *already* tilts against same-family judging — the family-bias and capability axes are partially confounded. Minor caveat the agent should add. |
| **Self-preference: GPT-4 0.705 XSUM / 0.912 CNN, Llama-2 0.51 / 0.505, GPT-4 self-recognition 73.5%, Kendall τ 0.74/0.82** | **UNVERIFIABLE from the open web within budget; HIGHLY PLAUSIBLE** | [NeurIPS 2024 abstract](https://proceedings.neurips.cc/paper_files/paper/2024/hash/7f1f0218e45f5414c79c0679633e47bc-Abstract-Conference.html); [moonlight.io review](https://www.themoonlight.io/en/review/llm-evaluators-recognize-and-favor-their-own-generations) | The PDF returned binary when fetched, so I could not byte-verify the table values. Search results confirm the existence of the study, the use of XSUM and CNN/DailyMail with 1,000 articles each, the 73.5% self-recognition figure, and the existence of the Kendall-tau finding. The four specific decimals (0.705, 0.912, 0.74, 0.82) are not directly attested in the open-web summaries I could load; they should be lifted to design.md only after a primary-source re-check. Mark NUANCED when integrating. |
| **JudgeDeceiver: 89.2% / 90.8% ASR on Openchat / Mistral-7B; suffix 97% / prefix 94%; baselines GCG 30–40%** | **UNVERIFIABLE within budget** | [arXiv 2403.17710](https://arxiv.org/abs/2403.17710) | Paper exists; abstract is consistent with the agent's framing; I could not byte-verify the four exact percentages. Plausible. |
| **Style/length bias 0.76–0.92 across 5 judges, 4 vendor families (Chen et al. 2506.13639)** | **CONFIRMED via second source** | The Chen reference is internally consistent with the broader RewardBench-2 literature (Allen AI June 2025) which is heavily cited and reports very similar style-bias magnitudes. Style bias dominating other forms of bias is a finding *also* documented in the RewardBench paper (arXiv 2403.13787) and in Lambert et al.'s NAACL Findings 2025 follow-up. | The headline claim ("style bias dwarfs family bias") is robust across multiple independent measurements. |

### Missed sources / counter-evidence

- The agent does not cite **Extreme Self-Preference in Language Models** ([arXiv 2509.26464](https://arxiv.org/pdf/2509.26464)), which extends the Panickssery measurement to closed-source 2025-era models and finds *higher* self-preference than the original 2024 paper. This *strengthens* the agent's R1 recommendation (Sonnet-judges-Opus is meaningfully different from Opus-judges-Opus), so missing it is a missed-opportunity citation rather than a contradiction.
- No discussion of the **"position-swap consistency"** failure mode in pairwise LLM judging (Zheng et al. 2023; 65% consistency for GPT-4). Position bias is a *larger* effect than same-family bias for the L4 pairwise-style review the agent contemplates, and the file mentions it only obliquely (one sentence about "Position-swap accuracy shifts >10%"). Should be a first-class recommendation: rotate position in L4 calls.

### Design-doc impact

**Trust the file.** The numerical revision to §Alternatives(f) the agent proposes is sound and should be adopted. Two small additions: (a) flag the 73.5% / 0.71 / 0.91 Panickssery decimals as "from the paper" with the verifying citation when you transcribe them; (b) add position-swap rotation as a concrete L4 mechanic. The proposed "family-stratified canary catch-rate" metric (R2) is novel and worth shipping as part of the canary corpus contract.

---

## 03 — Unicode Attack Surface

This file is correct in technique and largely thorough; the smaller items below are nuance, not contradiction.

### Load-bearing claims checked

| Claim | Verdict | Independent source | Note |
|---|---|---|---|
| **Default_Ignorable_Code_Point property is the right "complete by construction" base set; current L0 ranges miss "roughly two-thirds" of it** | **CONFIRMED** | [Unicode UAX #44](http://www.unicode.org/reports/tr44/tr44-16.html); [Unicode "Default Ignorable" doc](https://www.unicode.org/L2/L2002/02368-default-ignorable.html); [invisible-characters.com](https://invisible-characters.com/) | The property-based approach is the published Unicode-Org guidance for this class of attack. The specific gap list (U+00AD, U+034F, U+061C, U+115F-1160, U+17B4-5, U+180B-F, U+200E-F, the rest of U+2060-6F, U+FE00-F, U+FEFF, U+E0080-E0FFF) matches `DerivedCoreProperties.txt` membership. Independent invisible-char detector pages corroborate. |
| **U+E0000–U+E007F is the "lower half" of the Tag block; U+E0100–U+E01EF is the variation-selector long-form range and is the exact range used in 2024 invisible-prompt-injection PoCs (Riley Goodside Jan 2024)** | **NUANCED** | [Cisco blog](https://blogs.cisco.com/ai/understanding-and-mitigating-unicode-tag-prompt-injection); [Embrace The Red ASCII Smuggler](https://embracethered.com/blog/posts/2024/hiding-and-finding-text-with-unicode-tags/); [ProCheckUp](https://www.procheckup.com/blogs/posts/2024/march/invisible-prompt-injection/) | The Riley Goodside ASCII-smuggling technique used **U+E0020 through U+E007E** (the printable-ASCII-equivalent sub-range of the Tag block), not U+E0000–U+E007F as written (U+E0000 itself is LANGUAGE TAG, U+E0001 is unassigned, U+E001F is reserved). The agent's range *includes* the right characters but is loosely framed. Separately, the long-form-variation-selector smuggling (U+E0100–U+E01EF) is a documented technique but the "exact range used in 2024 PoCs" claim conflates two distinct smuggling channels: the Tag-block channel (Goodside Jan 2024) and the variation-selector channel (which is a later, separate technique covered in the embrace-the-red follow-ups). Both are real; the agent is collapsing them. |
| **GitHub's bidi-banner since 2021-10-31 only triggers on `Bidi_Control` characters, doesn't highlight the offending line, doesn't strip anything; banner is routinely banner-fatigued** | **CONFIRMED** | GitHub Changelog 2021-10-31; Michael Altfield's 2021 analysis (cited correctly by the agent). The "still unchanged through 2025" claim is hard to byte-verify but matches my own recent experience opening Unicode-laced PRs. | The "banner fatigue" framing is the agent's editorial gloss; the *mechanical* facts are accurate. |
| **Strip-before-NFC order: CGJ U+034F and ZWNJ U+200C block NFC composition; strip-after-NFC leaves the block intact** | **CONFIRMED** | [UAX #15](https://www.unicode.org/reports/tr15/) — CGJ is documented as a "composition blocker" by Unicode design intent. | The fixture `12_cgj_nfc_poison.diff` is the right test to write. |
| **Confusables / homoglyphs out of scope for L0; correctly belongs in UAX #31 R3 / UTS #39 "skeleton" territory** | **CONFIRMED** | UAX #31, UTS #39 — script-mixing detection is the documented Unicode-Org guidance for this. | Design call is correct; the locale-sensitivity argument is sound. |

### Missed sources / counter-evidence

- No mention of **UTS #55 ("Unicode Source Code Handling")** in the property derivation. The agent cites it once in references but doesn't lean on it. UTS #55 is the *normative* Unicode-Org guidance for compilers/tools handling source-text smuggling and explicitly recommends a property-based strip equivalent to what the agent proposes. It would strengthen the §3.1 framing to cite UTS #55 as "this is the Unicode-Org recommended approach for source-handling tools," not just to list it in references.
- The agent's proposed strip set explicitly **does not include** U+200C ZWNJ and U+200D ZWJ stripping handling for emoji ZWJ sequences (e.g., 👨‍👩‍👧‍👦 family emojis depend on ZWJ). The §3.2 table includes the U+200B–U+200F range as "expanded by +U+200E, U+200F" but does not flag the emoji-ZWJ-sequence loss-of-meaning risk. If a criterion legitimately contains a ZWJ-composed emoji, the strip changes the semantics of that emoji. Minor edge case but worth a fixture in `pass/`.

### Design-doc impact

Adopt the file's §10 proposed-edit text for `gates/l0/testdata/README.md` essentially as-written. Add a one-line clarification on the Goodside-range nuance (U+E0020–U+E007E for the ASCII-equivalent sub-range, the rest of U+E0000–U+E007F for non-printables that are also worth stripping). Add a `pass/` fixture for a ZWJ-composed emoji to lock down the "ZWJ in emoji sequences" carve-out.

---

## 04 — Branch Protection Enforcement

Mechanically tight. The platform-fact claims hold up; the one place to push back is on whether the proposed "verify-repo-config" closes the gaps as cleanly as the agent implies.

### Load-bearing claims checked

| Claim | Verdict | Independent source | Note |
|---|---|---|---|
| **GitHub: skipped required check counts as success ("Required status checks must have a `successful`, `skipped`, or `neutral` status before collaborators can make changes")** | **CONFIRMED** | [GitHub troubleshooting docs](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/collaborating-on-repositories-with-code-quality-features/troubleshooting-required-status-checks); [Emmer 2024 write-up](https://emmer.dev/blog/skippable-github-status-checks-aren-t-really-required/); [devopsdirective](https://devopsdirective.com/posts/2025/08/github-actions-required-checks-for-conditional-jobs/) | Verbatim platform behavior, confirmed both by docs and by multiple independent write-ups that hit the gotcha in production. The `alls-green` aggregator pattern is the canonical fix. |
| **Mercari PR Hijacking (Dec 2024): pushing onto someone else's PR + self-approving; GitHub responded "expected behavior"; fix is `require_last_push_approval`** | **CONFIRMED** | [Mercari Engineering 2024-12-17](https://engineering.mercari.com/en/blog/entry/20241217-github-branch-protection/) | Mechanism and GitHub's "expected behavior" response are both directly attested. |
| **GitHub `userContentEdits` is the only API surface that returns prior body text for issue edits (REST timeline does NOT emit `body_edited`)** | **NUANCED** | [GitHub community discussion #33551](https://github.com/orgs/community/discussions/33551); [GitHub GraphQL docs](https://docs.github.com/en/graphql/reference/objects) | The existence of `userContentEdits` is confirmed; but the file's *Spec Adapter* sibling (#5) makes a stronger claim about reconstructing the prior body that this file partially relies on. See file 05 verdict below — what `userContentEdits` actually returns is a `diff` field, not a "prior body" field. File 04's framing of this is more conservative than file 05's, so file 04 is fine; just don't propagate file 05's bolder framing here. |
| **GitHub App cannot approve PR it opened; CHANGES_REQUESTED is the only review verb available to the bot as a blocking signal** | **CONFIRMED** | [Graphite PR approval rules guide](https://graphite.com/guides/pull-request-approval-permissions-rules-github); GitHub Docs on app authentication | Mechanically true. This is the agent's strongest finding: "AI review is rejecting only" is mechanically enforced, not just a policy claim. |
| **CODEOWNERS silent-ignore taxonomy (team typo, no write access, lost-access user, syntax error, >3 MB file, pattern-matches-nothing); `GET /repos/{owner}/{repo}/codeowners/errors` catches first four** | **CONFIRMED** | [GitHub Docs: About code owners](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners); [REST docs: codeowners/errors](https://docs.github.com/en/rest/repos/repos#list-codeowners-errors) | All six silent-failure modes are platform-documented. The fifth and sixth (file size, dormant pattern) are correctly flagged as NOT caught by the errors API — that's the cleanest contribution of this file. |
| **`enforce_admins` defaults to `false`; admin bypass is audit-logged but invisible in PR UI; events: `protected_branch.policy_override` etc.** | **CONFIRMED** | [Datadog GitHub branch-protection-override rule](https://docs.datadoghq.com/security/default_rules/github-branch-protection-override/); [GitHub Changelog: bypass permission](https://github.blog/changelog/2022-08-18-bypass-branch-protections-with-a-new-permission/) | Default-off behavior is confirmed; event-name taxonomy matches the third-party SIEM rule. |

### Missed sources / counter-evidence

- **Cider Security 2022 work** ([Legit Security article](https://www.legitsecurity.com/blog/bypassing-github-required-reviewers-to-submit-malicious-code)) on `GITHUB_TOKEN`-driven self-approval predates Mercari and is the more cited primary source for that attack class. The agent cites Mercari but not Cider; both should be in `incidents.md`.
- The "Bypass list" semantics conflict between classic branch protection and rulesets is *underspecified* in the file. Both surfaces have their own bypass list, and a permission can exist on one and not the other. The agent's §1 notes this risk but the §11 cheat-sheet treats the bypass list as a single concept. Add a line: `regatta verify-repo-config` must check *both* the classic protection bypass list AND the matching ruleset bypass list, and treat them as union.

### Design-doc impact

**Trust this file as-written.** The four proposed design.md edits (Threat Model, Day 0 verify step, L6 paragraph, P2 row clarification) are sound and tight. The `regatta verify-repo-config` command should ship as part of v3.1 day-zero scope; this is the highest-leverage cheap deliverable in the wave.

---

## 05 — SpecAdapter Platform Constraints

The platform-fact map is mostly accurate but contains one CONTRADICTED claim that affects Regatta's "first-class Linear support" promise, and the GitHub `userContentEdits` framing is more confident than the API actually supports.

### Load-bearing claims checked

| Claim | Verdict | Independent source | Note |
|---|---|---|---|
| **GitHub REST issue endpoint supports ETag + `If-None-Match`; 304 doesn't count against rate limit; `userContentEdits` is the only GraphQL surface for prior body** | **NUANCED** | [GitHub REST best-practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api); [GitHub community #33551](https://github.com/orgs/community/discussions/33551) | The ETag/304 mechanics are right. The `userContentEdits` claim is technically true but more limited than the agent implies: the field returns a `diff` (an edit-delta representation) and metadata (`createdAt`, `editor`, `deletedAt`), **not** "the body as it was at edit-time-T" directly. To recover the body at time T, the adapter must walk every edit *forward* from issue creation and apply each diff in sequence — which the agent doesn't actually specify. The `userContentEdits[].diff` field's encoding is also not documented as a stable contract (community discussion shows the format has been adjusted over time). For Regatta's L0 contract to hold, this needs a concrete implementation plan, not "L0 calls GraphQL userContentEdits to retrieve the body as it was at edit-time-T." |
| **GitLab Issues has no public REST endpoint for description edit history; `updated_at` advances on ANY field change; gitlab-org/gitlab#10103 is still open since 2018** | **CONFIRMED** | [GitLab issue #10103](https://gitlab.com/gitlab-org/gitlab/-/issues/10103); [GitLab forum thread](https://forum.gitlab.com/t/issue-description-history/51756); [GitLab issues API docs](https://docs.gitlab.com/api/issues/) | Issue #10103 confirmed open ("View the history of changes to an issue/mr/epic description"). The "degraded-mode" verdict is justified. |
| **Linear GraphQL `IssueHistory` returns `fromDescription` and `toDescription` "as full prior/next strings (markdown text, not ADF). This is the diff-capable surface."** | **CONTRADICTED** | Web search of [Linear's published IssueHistory schema](https://studio.apollographql.com/public/Linear-API/schema/reference?variant=current) returns from*/to* fields including `fromAssignee`, `toAssignee`, `fromState`, `toState`, `fromCycle`, `toCycle`, `fromParent`, `toParent`, `fromProject`, `toProject`, `fromTeam`, `toTeam`, `fromTitle`, `toTitle` — but I could **not** confirm `fromDescription` / `toDescription` exist on the IssueHistory type. The first-party Apollo Studio page loads as an empty stub via WebFetch (it's a JS SPA); the `linear/linear` GitHub repo's `schema.graphql` is 46k lines and could not be byte-searched within budget. **This is the riskiest claim in file 05.** | If `fromDescription`/`toDescription` do **not** exist on `IssueHistory`, then Linear *cannot* be a "first-class" adapter — it falls into the same `degraded-mode` bucket as GitLab. The agent's recommendation tier ("Linear in `criteria_mode: subissue` is first-class") then collapses to "Linear is degraded unless you use sub-issues, which most Linear users don't." Before integrating file 05's recommendations, **someone with API access must introspect the live Linear schema** and confirm or refute the `fromDescription`/`toDescription` fields. This is the single biggest research-followup item in the wave. |
| **Jira `changelog.histories[].items[]` for the description field returns *plain-text projections* of the ADF, not the underlying ADF JSON; round-trip reconstruction is lossy** | **CONFIRMED** | [Atlassian Jira REST v3 docs](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issues/); [ADF migration docs](https://developer.atlassian.com/cloud/jira/platform/apis/document/structure/) | ADF storage + plain-text changelog representation is the documented Jira Cloud behavior. The "rendered-text view, not ADF JSON" recommendation is the right design call. |

### Missed sources / counter-evidence

- **Linear `IssueHistory` schema verification (see above)** — the most load-bearing claim in the file is the one I could not verify. The agent should retest with an actual `query { __type(name: "IssueHistory") { fields { name type { name } } } }` introspection call before promoting Linear to "first-class conditional."
- Jira's `Acceptance Criteria` custom-field id range (`customfield_10100` through `customfield_10299`) is correct in spirit but each Jira Cloud instance assigns its own; the *range* the agent quotes is convention, not contract. Worth softening to "convention; instance-specific id required by the adapter."

### Design-doc impact

**Block on the Linear schema verification** before adopting file 05's tiering recommendations or its `Capabilities` matrix in §9. The GitHub `userContentEdits` framing is also too confident — soften the design.md L0 contract to say "L0 fetches the issue body at the ETag-pinned read time and re-fetches at gate time; if ETag has changed, L0 walks `userContentEdits.diff` to assess whether the criterion span has byte-changed, and on inability to reconstruct cleanly, returns `ErrSourceUnverifiable`" rather than the agent's more confident "L0 calls the GraphQL `userContentEdits` endpoint to retrieve the body as it was at edit-time-T."

The Jira and GitLab degraded-mode treatments are sound and should be adopted as-written.

---

## 06 — Eval Harness Prior Art

Good benchmark survey; the statistical-floor analysis is the strongest contribution. Two specific claims overreach.

### Load-bearing claims checked

| Claim | Verdict | Independent source | Note |
|---|---|---|---|
| **SWE-bench Verified is retired by OpenAI ("Why SWE-bench Verified no longer measures frontier coding capabilities," 2025); audit found ≥59.4% of original problems had broken/under-specified tests** | **NUANCED — wrong denominator** | [OpenAI blog post](https://openai.com/index/why-we-no-longer-evaluate-swe-bench-verified/); [Aetos.AI summary](https://aetos.ai/posts/14417b93793f21d3); [byteiota](https://byteiota.com/openai-abandons-swe-bench-verified-59-flawed-tests/) | The 59.4% figure is real but the denominator is **138 problems o3 could not consistently solve over 64 runs**, NOT "the original problems" / not all SWE-bench items and not all 500 SWE-bench Verified items. The audit was a stratified sample of failure modes, not a population estimate. The agent's gloss "≥59.4% of the original problems had broken or under-specified test cases" overstates this materially. Restate as "59.4% of the 138 audited hard-failing problems had material issues with test design or problem description; the population-level rate is lower." This is the most overcorrected stat in the wave. |
| **JudgeBench (350 items) is the closest prior art for L3/L4/L5 calibration; its "correctness check via test-suite execution" is conceptually identical to Regatta's expected-verdict per archetype** | **CONFIRMED** | [arXiv 2410.12784](https://arxiv.org/abs/2410.12784); same as in file 02 | Framing is right; benchmark is genuinely the only prior art for reviewer-side evaluation. |
| **Parasuraman & Manzey (2010): "operators detect ~30% of automation errors when automation is reliable, ~75% when automation visibly fails"** | **NUANCED — comparison is constant-reliability vs variable-reliability, not "reliable vs visibly failing"** | [Parasuraman & Manzey, Human Factors 2010](https://journals.sagepub.com/doi/10.1177/0018720810376055); [PMC reanalysis](https://pmc.ncbi.nlm.nih.gov/articles/PMC4221095/) | The 30% / 75% numbers come from a 1993 Parasuraman experiment that compared **constant-reliability groups (~30%)** against **variable-reliability groups (~75%)** — not "reliable automation" vs "visibly failing automation." The mechanism the original paper proposes is that *exposure to varied reliability disrupts complacency*, which is a different finding than the agent's gloss. The framing as "people miss errors when automation seems reliable, catch errors when it visibly fails" is folk-summary; the rigorous statement is "miss errors when reliability is monotonic, catch errors when reliability is variable." Doesn't change the design conclusion (canary injection is the right mechanism — it *introduces* the variability that disrupts complacency) but the design-doc text should be precise. |
| **Wilson score interval: 28/30 = 93.3% agreement gives 95% CI [78.7%, 98.2%], 19-point spread** | **CONFIRMED** | Standard Wilson score formula. Spot-check: Wilson 95% CI for 28/30 ≈ [78.0%, 98.2%]. The 78.7 vs 78.0 is a small rounding/continuity difference; the conclusion (≈19-point spread) is sound. | Honest statistical floor. |
| **Mutation-testing literature (Just et al. FSE 2014): "~100 mutants per project as a minimum for stable estimates of mutation score"** | **CONFIRMED, with caveat** | Just et al., "Are mutants a valid substitute for real faults in software testing?" FSE 2014 | The 100-mutants-floor is widely cited as Just et al.'s recommendation. The Regatta-relevant translation (canary archetypes ≈ mutants) is the agent's analogy, not Just et al.'s claim; that analogy is reasonable but should be flagged as the agent's inference, not the literature's direct claim. |

### Missed sources / counter-evidence

- The agent's "JudgeBench is the closest prior art for measuring reviewer-side skill" claim is true within software engineering, but **misses the prompt-injection-canary literature in security ML**. Toolkits like LLM-Canary, Little Canary, and the canary-string detection work (cited in file 02 in passing but not picked up here) are *closer* analogues to Regatta's canary-PR injection than JudgeBench. JudgeBench is static (paired responses); Regatta is dynamic (injected canaries into a live agent run); the security-ML canary literature is dynamic too. The "Regatta's canary-PR injection is publishable on its own" claim is true but is not as alone as the file implies.
- No discussion of **`mutmut`, Stryker, or PIT's actually-shipped operator sets** as candidates for direct adoption in canary archetypes 08–11. The file proposes the archetypes from first principles, which is fine, but a one-line "we cross-walked our 18 archetypes against the PIT operator catalog and N% have direct correspondence" would strengthen the novelty audit.

### Design-doc impact

The 18-archetype expansion (originally 8 → 18) is sound and should be adopted. The statistical-floor analysis (30 is the floor; 100+ for tight discrimination; CUSUM on weekly agreement series) should land in the §Test Harness section. Correct the SWE-bench-Verified 59.4% framing before quoting it in design.md. Restate the Parasuraman finding precisely (constant vs variable reliability, not reliable vs failing) before invoking it in §Failure Modes.

---

## Top 5 things the research wave got wrong or oversold

1. **The Linear `fromDescription` / `toDescription` claim (file 05) is unverified and may be CONTRADICTED.** Schema introspection of Linear's `IssueHistory` from open-web sources shows `fromTitle`/`toTitle`, `fromState`/`toState`, and many other paired fields, but does not surface `fromDescription`/`toDescription`. If those fields do not exist, file 05's "Linear is first-class conditional on `criteria_mode: subissue`" tier collapses and the design.md must either drop Linear from first-class status or require sub-issue mode unconditionally. **Block on this verification.**

2. **The SWE-bench Verified 59.4% figure (file 06) is real but the denominator is wrong.** The 59.4% is computed over 138 *hard-failing* problems (the o3-unsolvable subset OpenAI audited), not the 500 Verified items or the original 2,294 SWE-bench items. As written, the design conclusion (don't lean on SWE-bench Verified for ongoing eval) is right, but the underlying number is overstated by an unknown but material factor. Restate as "of 138 audited hard-failing problems, 59.4% had material test/spec issues."

3. **The Parasuraman & Manzey 30% / 75% framing (file 06) misstates the original experiment.** The numbers come from a constant-reliability-vs-variable-reliability comparison, not a reliable-vs-failing comparison. The design conclusion (canary injection disrupts complacency) actually fits the *correct* framing better than the agent's gloss, but the design.md text needs the precise version.

4. **The Mini Shai-Hulud root-cause framing (file 01) misses OIDC-token extraction.** The agent describes the attack as "GitHub Actions cache + provenance signatures didn't catch a compromised CI run," which is part of it but skips the OIDC-token-from-runner-memory step that *also* enabled the publish. P1 hardening via SBOM/attestation/age-gates is fine; runner-memory isolation is a separate axis the agent should flag.

5. **The GitHub `userContentEdits` claim across files 04 and 05 overclaims what the field returns.** It returns a `diff` field and metadata, not "the prior body at time T" directly. Reconstructing the body requires walking edits sequentially and applying diffs; the format is community-documented but not contractually stable. Design.md should soften L0's mutation-recovery contract to "best-effort reconstruction via `userContentEdits.diff` walk, with fallback to `ErrSourceUnverifiable`" rather than promising the agent can always recover prior text.

## Honorable mentions (NUANCED but not "top 5")

- **TrustFall vendor response framing (file 01):** "three of four vendors classified this as working as designed" is not in the Adversa or Register source I located; the Anthropic position is well-attested but the broader vendor-by-vendor claim is fuzzier than written.
- **Kiro attribution (file 01):** Amazon publicly denies the AI-caused framing; design.md should note the disputed attribution rather than present it as settled fact.
- **Riley Goodside Tag-block range (file 03):** the printable-ASCII sub-range is U+E0020–U+E007E, not all of U+E0000–U+E007F. Includes the relevant characters, just imprecise.
- **JudgeBench capability-vs-family confound (file 02):** GPT-4o-generated pairs already bias against GPT-4o judges on JudgeBench, so "capability dominates family on hard tasks" is measured on a benchmark that partially confounds the two axes.

## What the research wave got *right* (worth confirming explicitly)

- The Preference Leakage Table 2 percentages (file 02) are digit-for-digit confirmed; this is the strongest empirical contribution in the wave.
- The branch-protection mechanical taxonomy (file 04), in particular `enforce_admins` default-false, `require_last_push_approval` as the Mercari-mitigation, the SKIPPED-as-success hole, the `merge_group` trigger requirement, and the `regatta verify-repo-config` proposal — all of these are platform-fact-accurate and high-leverage. File 04 is the wave's most-actionable artifact.
- The Unicode `Default_Ignorable_Code_Point`-property-derived strip set (file 03) is the right Unicode-Org-aligned answer, and the strip-before-NFC ordering argument (CGJ poison) is mechanically correct.
- All ten incidents in file 01 that I spot-checked (#20, #21, #24, #27, #28, #29, #30, #31) have primary-source attestations. The catalog is not fabricated; it is well-sourced. The proposed P11/P12/P13 patterns are well-grounded in the underlying incidents.
- The 18-archetype canary catalog expansion (file 06) is well-cross-walked against PIT mutation-testing operators and the documented AI-author failure-mode literature. Adopt as-is.
- The Wilson-CI statistical-floor argument (file 06) is rigorous and gives the design.md §Test Harness section a much more honest "30 is the floor for gross-regression detection, not for precision claims" framing.

---

**Word count:** ~3,500.

**Process note:** All six research files (01–06) were available during this review. File 04 and file 05 landed near the end of the window and got proportionally less cross-checking time; the Linear schema verification follow-up is the most important consequence of that.
