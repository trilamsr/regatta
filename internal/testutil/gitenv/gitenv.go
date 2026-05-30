// Package gitenv strips inherited GIT_* env vars so a child `git`
// invocation respects its own cmd.Dir instead of the parent's
// GIT_DIR / GIT_WORK_TREE. Without this, tests run under a
// pre-commit hook (or any other tooling that sets GIT_*) silently
// retarget the parent repo instead of the test temp dir.
package gitenv

import "strings"

var scrub = map[string]bool{
	"GIT_DIR":                          true,
	"GIT_WORK_TREE":                    true,
	"GIT_INDEX_FILE":                   true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_COMMON_DIR":                   true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_AUTHOR_DATE":                  true,
	"GIT_COMMITTER_DATE":               true,
}

// Scrub returns env with every GIT_* variable that could override
// cmd.Dir removed. Pass it as cmd.Env when shelling out to git
// from tests.
func Scrub(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq > 0 && scrub[kv[:eq]] {
			continue
		}
		out = append(out, kv)
	}
	return out
}
