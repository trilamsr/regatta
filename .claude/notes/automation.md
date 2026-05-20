# Automation

Lessons specific to Claude Code skills, slash commands,
classifier interactions, and parallel-agent workflows in this
repo. Newest-first.

### When a memory captures a recurring failure, ask whether a hook can enforce it

A saved feedback memory ("do X before Y") is advisory: it
relies on the agent discovering and applying the rule at the
right moment. A pre-PR checklist memory can sit in place and
still get bypassed; the same gap closes reliably by a
`PreToolUse` hook that injects the checklist as a system
reminder the agent cannot miss.

Memory remains useful for *rationale* (six-months-cold-reader
can look up *why* the rule exists). Hooks provide *enforcement*
(the agent cannot proceed without acknowledgment). For any
"always do X before Y" pattern, prefer the hook; keep the
memory as the explanation for why the hook exists.

This generalizes: the regatta-side analogue is "prefer a
fail-closed gate over a prompt instruction." A prompt that says
"don't do X" is advisory; a gate that rejects PRs containing X
is enforcement. The choice between them follows the same lens.

Anchor: `.claude/settings.json` `PreToolUse.Bash` hook.

### Match shell-command hooks by regex word-boundary, not substring

Substring patterns like `*"gh pr create"*` fire on any command
that literally contains the target string (`echo gh pr create`,
`bash -c "git push test"`), producing high-volume false
positives. "Starts with" patterns miss compound forms
(`cat > body && gh pr edit`, `var=$(...) && git push`),
producing high-cost false negatives. A regex word-boundary
match lands the balance - three parts: a leading shell-
separator boundary, the alternation, a trailing boundary:

```
(^|[[:space:];&|])       # line start OR shell separator
(gh pr create|git push)  # the actual targets
([[:space:]]|$)          # space OR line end
```

For advisory hooks (surface a checklist, log a warning) bias
toward false positives - the cost of an extra fire is one
acknowledgment; a missed fire on a real action is worse. For
blocking hooks (deny the action via `permissionDecision: deny`)
flip the bias: prefer starts-with so an unrelated command
isn't forcibly stopped.

This trade-off generalizes to any agent hook matching shell
commands by content; PR commands are this entry's example, not
its scope.

### Don't escape backticks inside a single-quoted HEREDOC

A `bash -c '...$(cat <<'EOF' ... EOF)...'`-style PR-body
payload uses a single-quoted HEREDOC delimiter (`'EOF'`) which
disables shell expansion - so backticks inside the body do NOT
need escaping. Writing `\`\`\`release-notes` instead of triple-
backtick produces literal backslashes in the rendered output,
and the `pr-lint` workflow's awk regex (`^\`\`\`release-notes`
matched as `^` + three backticks) then fails to find the block,
failing CI.

The reliable fix bypasses heredoc quoting entirely: write the
body to a file and pass `gh pr create --body-file <path>`. No
quoting, no escaping.

Anchor: prefer `gh pr ... --body-file <path>` over
`--body "$(cat <<'EOF' ... EOF)"`. If the heredoc form is
unavoidable, sanity-check the rendered block via
`gh pr view <N> --json body -q .body | grep -A2 '\`\`\`release-notes'`
before declaring the PR open. The check should show three real
backticks, not `\`\`\``.

### Verify side-effects of slash commands via forensic file check

A Claude Code slash command's setup script can be silently
blocked by the auto-mode permission classifier when its bash
argument contains tokens the repo's grep gate would otherwise
reject. The command appears to succeed - the argument body
passes forward as a regular message - but the script never
executes. Per-worktree forensic check on the expected state
file is the only reliable signal of whether the side effect
occurred.

Anchor: `.claude/settings.local.json` allow-list entries
pinning specific script paths; absence of a matching
`Bash(...sh:*)` entry correlates with silent classifier denials
on subsequent invocations.
