package prwatch

import (
	"context"
	"strings"
	"testing"
)

// TestGHCLILister_DecodesMergeStateStatus asserts the prwatch decoder surfaces mergeStateStatus from gh's JSON (#operator-console-S0).
func TestGHCLILister_DecodesMergeStateStatus(t *testing.T) {
	t.Parallel()
	r := &fakeRunner{out: map[string][]byte{
		"gh pr list --state open --head regatta/agent-7 --json " + ghJSONFields: []byte(
			`[{"number":42,"headRefOid":"sha1","state":"OPEN","headRefName":"regatta/agent-7","title":"[agent-7] x","author":{"login":"me"},"mergeStateStatus":"DIRTY"}]`,
		),
		"gh pr list --state open --search in:title [agent-7] --json " + ghJSONFields: []byte(`[]`),
	}}
	g := &GHCLILister{Runner: r.run}
	prs, err := g.ListOpenByHead(context.Background(), "regatta/agent-7", "[agent-7]")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d prs", len(prs))
	}
	if !strings.EqualFold(prs[0].MergeStateStatus, "DIRTY") {
		t.Errorf("MergeStateStatus: got %q want DIRTY", prs[0].MergeStateStatus)
	}
}
