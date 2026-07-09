package approval

import "testing"

func TestExtract(t *testing.T) {
	t.Parallel()
	in := `# Heading

Some prose.

- [ ] First criterion text.
- [x] Second criterion done. test=TestFoo
- [X] Third with multi-citation. test=TestBar,file=foo.go:42
- not a checkbox
- [ ] Fourth, plain.
`
	got := l0Extract(in)
	if len(got) != 4 {
		t.Fatalf("len=%d; want 4 (got: %+v)", len(got), got)
	}
	if got[0].State != L0StatePlanned || got[0].Text != "First criterion text." || got[0].Citation != "" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].State != L0StateDone || got[1].Text != "Second criterion done." || got[1].Citation != "test=TestFoo" {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[2].State != L0StateDone || got[2].Text != "Third with multi-citation." || got[2].Citation != "test=TestBar,file=foo.go:42" {
		t.Errorf("got[2] = %+v", got[2])
	}
	if got[3].State != L0StatePlanned || got[3].Text != "Fourth, plain." {
		t.Errorf("got[3] = %+v", got[3])
	}
}
