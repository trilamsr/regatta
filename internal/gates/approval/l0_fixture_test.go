package approval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/contracts/schemas"
)

// TestFixtureCorpus asserts every testdata/gates/l0/{pass,fail,edge}/*.diff fixture produces its expected verdict (pass empty, fail ≥1 blocking, edge per sibling *.expected.json).
func TestFixtureCorpus(t *testing.T) {
	for _, kind := range []string{"pass", "fail", "edge"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join("..", "..", "..", "testdata", "gates", "l0", kind)
			ents, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("fixture dir %q unreadable: %v (path drift?)", dir, err)
			}
			seen := 0
			for _, e := range ents {
				if !strings.HasSuffix(e.Name(), ".diff") {
					continue
				}
				seen++
				name := e.Name()
				t.Run(name, func(t *testing.T) {
					body, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil {
						t.Fatalf("read: %v", err)
					}
					r := L0Check(L0Default(), L0ParseUnifiedDiff(string(body)))
					switch kind {
					case "pass":
						if r.Verdict != schemas.VerdictPass {
							t.Errorf("expected pass; got %s findings=%+v", r.Verdict, r.Findings)
						}
					case "fail":
						if r.Verdict != schemas.VerdictFail {
							t.Errorf("expected fail; got %s findings=%+v", r.Verdict, r.Findings)
						}
						if !r.Blocking {
							t.Errorf("expected blocking=true on fail; got %+v", r)
						}
						if len(r.Findings) == 0 {
							t.Errorf("expected at least one finding on fail; got %+v", r)
						}
					case "edge":
						expectedPath := strings.TrimSuffix(filepath.Join(dir, name), ".diff") + ".expected.json"
						b, err := os.ReadFile(expectedPath)
						if err != nil {
							t.Fatalf("read expected: %v", err)
						}
						var want struct {
							Verdict schemas.Verdict `json:"verdict"`
						}
						if err := json.Unmarshal(b, &want); err != nil {
							t.Fatalf("unmarshal expected: %v", err)
						}
						if r.Verdict != want.Verdict {
							t.Errorf("expected %s; got %s findings=%+v", want.Verdict, r.Verdict, r.Findings)
						}
					}
				})
			}
			if seen == 0 {
				t.Skipf("%s/ contains no .diff fixtures yet", kind)
			}
		})
	}
}
