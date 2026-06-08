# reviewer-verdict/args.sh — flag parser for check-reviewer-verdict.sh.
# Sets PR_NUM, BODY_FILE, LOAD_BEARING, PATHS_FILE, SKIP, PR_AUTHOR,
# AUTOMERGE_ENABLED in caller scope.

rv_parse_args() {
  PR_NUM=""
  BODY_FILE=""
  LOAD_BEARING=0
  PATHS_FILE=""
  SKIP=0
  PR_AUTHOR=""
  AUTOMERGE_ENABLED=0

  while [ $# -gt 0 ]; do
    case "$1" in
      --pr) PR_NUM="$2"; shift 2 ;;
      --body-file) BODY_FILE="$2"; shift 2 ;;
      --load-bearing) LOAD_BEARING=1; shift ;;
      --changed-paths-file) PATHS_FILE="$2"; shift 2 ;;
      --skip) SKIP=1; shift ;;
      --pr-author) PR_AUTHOR="$2"; shift 2 ;;
      --automerge-enabled) AUTOMERGE_ENABLED=1; shift ;;
      -h|--help)
        grep '^#' "$0" | sed 's/^# \{0,1\}//'
        exit 0
        ;;
      *)
        echo "check-reviewer-verdict: unknown flag: $1" >&2
        exit 3
        ;;
    esac
  done
}
