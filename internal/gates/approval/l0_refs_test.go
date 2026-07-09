package approval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
	"github.com/trilamsr/regatta/internal/testutil/gitenv"
)

// testRepo wraps a temp git repo for harness tests. All operations run with
// committer/author identity pinned so commit hashes are reproducible-enough
// and the runner needs no global git config.
type testRepo struct {
	t   *testing.T
	dir string
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	r := &testRepo{t: t, dir: dir}
	r.git("init", "-q", "-b", "main")
	r.git("config", "user.email", "test@regatta.invalid")
	r.git("config", "user.name", "Regatta Test")
	r.git("config", "commit.gpgsign", "false")
	return r
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = gitenv.Scrub(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *testRepo) write(path, body string) {
	r.t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		r.t.Fatalf("write %s: %v", path, err)
	}
}

func (r *testRepo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

func (r *testRepo) checkout(ref string) {
	r.t.Helper()
	r.git("checkout", "-q", ref)
}

func (r *testRepo) branch(name string) {
	r.t.Helper()
	r.git("checkout", "-q", "-b", name)
}

func (r *testRepo) merge(ref, msg string) string {
	r.t.Helper()
	r.git("merge", "--no-ff", "-q", "-m", msg, ref)
	return r.git("rev-parse", "HEAD")
}

// TestCheckRefs_DiffBaseHidesBaseBranchTightening asserts L0CheckRefs diffs against merge-base (not main-tip) so unrelated base-branch tightening does not fail the PR (§1 contract).
func TestCheckRefs_DiffBaseHidesBaseBranchTightening(t *testing.T) {
	r := newTestRepo(t)
	r.write("MILESTONES.md", "# M1\n- [ ] Alpha criterion.\n- [ ] Bravo criterion.\n")
	r.commit("A: initial spec")

	r.branch("pr")
	r.write("MILESTONES.md", "# M1\n- [x] Alpha criterion. test=TestAlpha\n- [ ] Bravo criterion.\n")
	r.commit("PR: flip Alpha to done with citation")

	r.checkout("main")
	r.write("MILESTONES.md", "# M1\n- [ ] Alpha criterion.\n- [ ] Bravo criterion tightened on main.\n")
	r.commit("B: main tightens Bravo independently")

	res, err := L0CheckRefs(context.Background(), L0Default(), r.dir, "main", "pr")
	if err != nil {
		t.Fatalf("L0CheckRefs: %v", err)
	}
	if res.Verdict != schemas.VerdictPass {
		t.Fatalf("verdict=%s findings=%+v; PR's diff against merge-base should pass — main's Bravo tightening is invisible to L0", res.Verdict, res.Findings)
	}
}

// TestCheckRefs_DiffBaseCatchesPRTextEdit asserts the merge-base diff path still fails when the PR itself edits criterion text.
func TestCheckRefs_DiffBaseCatchesPRTextEdit(t *testing.T) {
	r := newTestRepo(t)
	r.write("MILESTONES.md", "# M1\n- [ ] Alpha criterion.\n")
	r.commit("A: initial spec")

	r.branch("pr")
	r.write("MILESTONES.md", "# M1\n- [ ] Alpha criterion rewritten by agent.\n")
	r.commit("PR: text edit (violation)")

	res, err := L0CheckRefs(context.Background(), L0Default(), r.dir, "main", "pr")
	if err != nil {
		t.Fatalf("L0CheckRefs: %v", err)
	}
	if res.Verdict != schemas.VerdictFail {
		t.Fatalf("verdict=%s; expected fail (PR edits criterion text)", res.Verdict)
	}
}

// TestCheckMergeCommit_CatchesPostMergeRegression asserts L0CheckMergeCommit catches a rubber-stamp merge (`-X theirs`) that clobbers main's post-PR tightening (§7 contract).
func TestCheckMergeCommit_CatchesPostMergeRegression(t *testing.T) {
	r := newTestRepo(t)
	r.write("MILESTONES.md", "# M1\n- [ ] Alpha criterion original.\n")
	r.commit("A: initial spec")

	r.branch("pr")
	r.write("MILESTONES.md", "# M1\n- [ ] Alpha criterion original.\n- [ ] PR-added criterion.\n")
	r.commit("PR: add new criterion (still carries pre-tighten Alpha text)")

	r.checkout("main")
	r.write("MILESTONES.md", "# M1\n- [ ] Alpha criterion tightened on main.\n")
	r.commit("B: main tightens Alpha")

	// `-X theirs` resolves the auto-merge conflict in PR's favor, modeling
	// a rubber-stamp merge that clobbers main's tightening.
	r.git("merge", "--no-ff", "-q", "-X", "theirs", "-m", "merge PR", "pr")
	mergeSHA := r.git("rev-parse", "HEAD")

	res, err := L0CheckMergeCommit(context.Background(), L0Default(), r.dir, mergeSHA)
	if err != nil {
		t.Fatalf("L0CheckMergeCommit: %v", err)
	}
	if res.Verdict != schemas.VerdictFail {
		t.Fatalf("verdict=%s findings=%+v; merge tree reverts main's Alpha tightening and adds a criterion — both are violations", res.Verdict, res.Findings)
	}
}

// TestCheckMergeCommit_CleanMergePasses asserts a clean merge carrying only a valid PR status flip re-runs to pass.
func TestCheckMergeCommit_CleanMergePasses(t *testing.T) {
	r := newTestRepo(t)
	r.write("MILESTONES.md", "# M1\n- [ ] Alpha criterion.\n")
	r.commit("A: initial spec")

	r.branch("pr")
	r.write("MILESTONES.md", "# M1\n- [x] Alpha criterion. test=TestAlpha\n")
	r.commit("PR: flip Alpha to done with citation")

	r.checkout("main")
	mergeSHA := r.merge("pr", "merge PR")

	res, err := L0CheckMergeCommit(context.Background(), L0Default(), r.dir, mergeSHA)
	if err != nil {
		t.Fatalf("L0CheckMergeCommit: %v", err)
	}
	if res.Verdict != schemas.VerdictPass {
		t.Fatalf("verdict=%s findings=%+v; clean merge of a valid PR flip must pass on re-run", res.Verdict, res.Findings)
	}
}
