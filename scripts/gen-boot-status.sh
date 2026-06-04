#!/usr/bin/env bash
# gen-boot-status.sh - regenerate auto-shipped + auto-priority blocks in the
# autonomous-session boot prompt from live gh queries. Kills the per-wave
# hand-edit tax called out in feedback_boot_prompt_per_wave_refresh.
#
# Flags:
#   --prompt <file>          prompt file to update (default:
#                            docs/engineer/autonomous-session-prompt.md)
#   --since <date>           cutoff for merged-PR query (default: 7 days ago,
#                            YYYY-MM-DD)
#   --label <name>           label filter for the priority issue query (default:
#                            no filter — every open issue, capped at 30)
#   --exclude-label <name>   drop issues carrying this label from the priority
#                            block (default: empty — no exclusion). Used to
#                            suppress parking labels like phase-x.
#   --limit-prs <N>          cap merged-PR result count (default: 50)
#   --limit-issues <N>       cap open-issue result count (default: 30)
#
# Exit codes: 0 success; 1 missing markers in prompt; 2 flag/usage error.

set -euo pipefail

prompt="docs/engineer/autonomous-session-prompt.md"
since=""
label=""
exclude_label=""
limit_prs=50
limit_issues=30

while [ $# -gt 0 ]; do
  case "$1" in
    --prompt)         prompt="$2";         shift 2 ;;
    --since)          since="$2";          shift 2 ;;
    --label)          label="$2";          shift 2 ;;
    --exclude-label)  exclude_label="$2";  shift 2 ;;
    --limit-prs)      limit_prs="$2";      shift 2 ;;
    --limit-issues)   limit_issues="$2";   shift 2 ;;
    -h|--help)
      sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *) echo "gen-boot-status: unknown flag: $1" >&2; exit 2 ;;
  esac
done

if [ -z "$since" ]; then
  # macOS/BSD date and GNU date differ on flag syntax.
  if date -v-7d +%Y-%m-%d >/dev/null 2>&1; then
    since=$(date -v-7d +%Y-%m-%d)
  else
    since=$(date -d '7 days ago' +%Y-%m-%d)
  fi
fi

if [ ! -f "$prompt" ]; then
  echo "gen-boot-status: prompt file does not exist: $prompt" >&2
  exit 2
fi

# Refuse to splice if either marker pair is missing — fail loud over silent no-op.
need_markers=(
  "<!-- BEGIN auto-shipped -->"
  "<!-- END auto-shipped -->"
  "<!-- BEGIN auto-priority -->"
  "<!-- END auto-priority -->"
)
for m in "${need_markers[@]}"; do
  if ! grep -qF "$m" "$prompt"; then
    echo "gen-boot-status: missing marker in $prompt: $m" >&2
    exit 1
  fi
done

# gh failures propagate via set -e; an empty `[]` payload is a legitimate
# zero-result. Silencing stderr here would mask auth/network failures.
shipped_json=$(gh pr list \
  --state merged \
  --search "merged:>$since" \
  --json number,title,mergedAt \
  -L "$limit_prs")

if [ -n "$label" ]; then
  priority_json=$(gh issue list \
    --state open \
    --label "$label" \
    --json number,title,labels \
    -L "$limit_issues")
else
  priority_json=$(gh issue list \
    --state open \
    --json number,title,labels \
    -L "$limit_issues")
fi

# JSON passes through env vars — interpolating into a Python string literal
# would let a PR title containing ''' or \ escape into arbitrary code.
render_shipped() {
  SHIPPED_JSON="$shipped_json" python3 - <<'PY'
import json, os

def sanitize(s):
    # Triple-backtick in a list bullet breaks fenced-code detection downstream;
    # leading `|` collides with GFM table parsing.
    s = (s or "").replace("\n", " ").strip()
    s = s.replace("```", "``")
    if s.startswith("|"):
        s = "\\" + s
    return s

data = json.loads(os.environ.get("SHIPPED_JSON") or "[]")
data.sort(key=lambda r: r.get("number", 0))
if not data:
    print("- _No merged PRs in window._")
else:
    for r in data:
        n = r.get("number", 0)
        title = sanitize(r.get("title"))
        print(f"- #{n} {title}")
PY
}

render_priority() {
  PRIORITY_JSON="$priority_json" EXCLUDE_LABEL="$exclude_label" python3 - <<'PY'
import json, os

def sanitize(s):
    s = (s or "").replace("\n", " ").strip()
    s = s.replace("```", "``")
    if s.startswith("|"):
        s = "\\" + s
    return s

data = json.loads(os.environ.get("PRIORITY_JSON") or "[]")
exclude = (os.environ.get("EXCLUDE_LABEL") or "").strip()
# Post-filter (not `gh --search "-label:..."`) — keeps the stub-gh test
# matrix one-shape and lets multiple excludes layer later via comma-split.
if exclude:
    data = [
        r for r in data
        if exclude not in {l.get("name", "") for l in (r.get("labels") or [])}
    ]
data.sort(key=lambda r: r.get("number", 0))
if not data:
    print("- _No open priority issues._")
else:
    for r in data:
        n = r.get("number", 0)
        title = sanitize(r.get("title"))
        labels = ",".join(sorted(l.get("name","") for l in (r.get("labels") or [])))
        suffix = f" [{labels}]" if labels else ""
        print(f"- #{n} {title}{suffix}")
PY
}

shipped_block=$(render_shipped)
priority_block=$(render_priority)

new_content=$(SHIPPED_BLOCK="$shipped_block" PRIORITY_BLOCK="$priority_block" \
  python3 - "$prompt" <<'PY'
import os, sys
path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    text = fh.read()

shipped = os.environ.get("SHIPPED_BLOCK", "")
priority = os.environ.get("PRIORITY_BLOCK", "")

def splice(text, begin, end, payload):
    i = text.find(begin)
    j = text.find(end, i)
    if i < 0 or j < 0:
        raise SystemExit(f"missing markers: {begin} / {end}")
    head = text[: i + len(begin)]
    tail = text[j:]
    return head + "\n" + payload.rstrip("\n") + "\n" + tail

text = splice(text, "<!-- BEGIN auto-shipped -->",  "<!-- END auto-shipped -->",  shipped)
text = splice(text, "<!-- BEGIN auto-priority -->", "<!-- END auto-priority -->", priority)
sys.stdout.write(text)
PY
)

# Skip write when stable — preserves mtime + lets callers grep for change signal.
current=$(cat "$prompt")
if [ "$current" = "$new_content" ]; then
  echo "gen-boot-status: $prompt already up to date"
  exit 0
fi

printf '%s' "$new_content" > "$prompt"
echo "gen-boot-status: wrote $prompt"
