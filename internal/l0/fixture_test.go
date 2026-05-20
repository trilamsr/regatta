package l0

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trilamsr/regatta/schemas"
)

// TestFixtureCorpus sweeps gates/l0/testdata/{pass,fail,edge}/ and
// asserts each *.diff fixture produces the expected verdict.
//
// - pass/*.diff   → schemas.VerdictPass, empty findings
// - fail/*.diff   → schemas.VerdictFail, ≥1 blocking finding
// - edge/*.diff   → verdict specified in sibling *.expected.json
func TestFixtureCorpus(t *testing.T) {
	for _, kind := range []string{"pass", "fail", "edge"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join("..", "..", "gates", "l0", "testdata", kind)
			ents, err := os.ReadDir(dir)
			if err != nil {
				t.Skipf("no %s dir yet: %v", kind, err)
				return
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
					r := Check(Default(), ParseUnifiedDiff(string(body)))
					switch kind {
					case "pass":
						if r.Verdict != schemas.VerdictPass {
							t.Errorf("expected pass; got %s findings=%+v", r.Verdict, r.Findings)
						}
					case "fail":
						if r.Verdict != schemas.VerdictFail {
							t.Errorf("expected fail; got %s findings=%+v", r.Verdict, r.Findings)
						}
						blocking := false
						for _, f := range r.Findings {
							if f.Blocking {
								blocking = true
								break
							}
						}
						if !blocking {
							t.Errorf("expected at least one blocking finding; got %+v", r.Findings)
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
