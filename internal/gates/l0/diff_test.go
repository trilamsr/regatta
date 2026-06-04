package l0

import "testing"

func TestParseUnifiedDiff_Minimal(t *testing.T) {
	t.Parallel()
	d := `diff --git a/MILESTONES.md b/MILESTONES.md
--- a/MILESTONES.md
+++ b/MILESTONES.md
@@ -1,3 +1,3 @@
 # M1
-- [ ] Add foo.
+- [x] Add foo. test=TestFoo
 - [ ] Unchanged.
`
	changes := ParseUnifiedDiff(d)
	if len(changes) != 1 {
		t.Fatalf("len(changes)=%d", len(changes))
	}
	c := changes[0]
	if c.OldPath != "MILESTONES.md" || c.NewPath != "MILESTONES.md" {
		t.Errorf("paths: old=%q new=%q", c.OldPath, c.NewPath)
	}
	wantOld := "# M1\n- [ ] Add foo.\n- [ ] Unchanged.\n"
	wantNew := "# M1\n- [x] Add foo. test=TestFoo\n- [ ] Unchanged.\n"
	if c.Old != wantOld {
		t.Errorf("Old=%q want %q", c.Old, wantOld)
	}
	if c.New != wantNew {
		t.Errorf("New=%q want %q", c.New, wantNew)
	}
}

func TestParseUnifiedDiff_NewFile(t *testing.T) {
	t.Parallel()
	d := `diff --git a/new.md b/new.md
--- /dev/null
+++ b/new.md
@@ -0,0 +1,2 @@
+# New
+- [ ] Brand new criterion.
`
	changes := ParseUnifiedDiff(d)
	if len(changes) != 1 || changes[0].Old != "" {
		t.Fatalf("got %+v", changes)
	}
	if changes[0].New != "# New\n- [ ] Brand new criterion.\n" {
		t.Errorf("New=%q", changes[0].New)
	}
}

// TestParseUnifiedDiff_PureRename asserts pure-rename diffs (no `--- a/`/`+++ b/` headers) yield in-scope paths via `rename from`/`rename to` (L0 README §5).
func TestParseUnifiedDiff_PureRename(t *testing.T) {
	t.Parallel()
	d := `diff --git a/MILESTONES_OLD.md b/MILESTONES.md
similarity index 100%
rename from MILESTONES_OLD.md
rename to MILESTONES.md
`
	changes := ParseUnifiedDiff(d)
	if len(changes) != 1 {
		t.Fatalf("len(changes)=%d", len(changes))
	}
	c := changes[0]
	if c.OldPath != "MILESTONES_OLD.md" {
		t.Errorf("OldPath=%q; want MILESTONES_OLD.md", c.OldPath)
	}
	if c.NewPath != "MILESTONES.md" {
		t.Errorf("NewPath=%q; want MILESTONES.md", c.NewPath)
	}
	if c.Old != "" || c.New != "" {
		t.Errorf("Old=%q New=%q; want both empty (pure rename has no hunks)", c.Old, c.New)
	}
}
