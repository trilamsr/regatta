package approval

import (
	"bufio"
	"strings"
)

// L0FileChange holds the old and new content of one file in a unified
// diff. /dev/null on either side yields an empty string.
type L0FileChange struct {
	OldPath string
	NewPath string
	Old     string // reconstructed from ` ` and `-` lines
	New     string // reconstructed from ` ` and `+` lines
}

// L0ParseUnifiedDiff parses a minimal subset of unified-diff format
// sufficient for L0 fixtures; reconstructs only lines that appear in
// diff hunks (surrounding context is not synthesized).
func L0ParseUnifiedDiff(diff string) []L0FileChange {
	var out []L0FileChange
	var cur *L0FileChange
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	sc := bufio.NewScanner(strings.NewReader(diff))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inHunk := false
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			cur = &L0FileChange{}
			inHunk = false
		case strings.HasPrefix(line, "--- "):
			if cur == nil {
				cur = &L0FileChange{}
			}
			cur.OldPath = strings.TrimPrefix(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "+++ "):
			if cur == nil {
				cur = &L0FileChange{}
			}
			cur.NewPath = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "rename from "):
			if cur == nil {
				cur = &L0FileChange{}
			}
			cur.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			if cur == nil {
				cur = &L0FileChange{}
			}
			cur.NewPath = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case inHunk && cur != nil:
			if line == "" {
				cur.Old += "\n"
				cur.New += "\n"
				continue
			}
			tag, body := line[0], line[1:]
			switch tag {
			case ' ':
				cur.Old += body + "\n"
				cur.New += body + "\n"
			case '-':
				cur.Old += body + "\n"
			case '+':
				cur.New += body + "\n"
			}
		}
	}
	flush()
	return out
}
