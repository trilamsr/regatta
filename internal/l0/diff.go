package l0

import (
	"bufio"
	"strings"
)

// FileChange holds the old and new content of one file in a unified
// diff. /dev/null on either side yields an empty string.
type FileChange struct {
	OldPath string
	NewPath string
	Old     string // reconstructed from ` ` and `-` lines
	New     string // reconstructed from ` ` and `+` lines
}

// ParseUnifiedDiff parses a minimal subset of unified-diff format
// sufficient for L0 fixtures. It reconstructs only the lines that
// appear in the diff hunks; surrounding context is not synthesized.
// For fixtures with /dev/null on one side, the other side's content
// is the full file.
func ParseUnifiedDiff(diff string) []FileChange {
	var out []FileChange
	var cur *FileChange
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
			cur = &FileChange{}
			inHunk = false
		case strings.HasPrefix(line, "--- "):
			if cur == nil {
				cur = &FileChange{}
			}
			cur.OldPath = strings.TrimPrefix(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "+++ "):
			if cur == nil {
				cur = &FileChange{}
			}
			cur.NewPath = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
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
