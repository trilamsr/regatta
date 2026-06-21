package lowrisk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// ghPRView mirrors the `gh pr view --json` shape the fetcher needs:
// changed-file paths, the additions/deletions that sum to diff LOC, and
// the open time for the stateless soak. The JSON allowlist pins the
// request so a gh schema change surfaces as a parse error, not silent
// payload widening (same discipline as merge.GhProber).
type ghPRView struct {
	Files []struct {
		Path string `json:"path"`
	} `json:"files"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	CreatedAt string `json:"createdAt"`
}

// NewGhFetcher returns a PRFetcher that shells out to `gh pr view`. The
// 30s timeout caps the worst GitHub-API edge; DiffLOC is additions +
// deletions so a large delete is held just like a large add.
func NewGhFetcher() PRFetcher {
	return func(ctx context.Context, prNumber int, _ string) (PR, error) {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cctx, "gh", "pr", "view", strconv.Itoa(prNumber), //nolint:gosec // G204: literal binary; prNumber is an int.
			"--json", "files,additions,deletions,createdAt").Output()
		if err != nil {
			return PR{}, fmt.Errorf("lowrisk: gh pr view %d: %w", prNumber, err)
		}
		return parseGhPRView(out)
	}
}

func parseGhPRView(data []byte) (PR, error) {
	var v ghPRView
	if err := json.Unmarshal(bytes.TrimSpace(data), &v); err != nil {
		return PR{}, fmt.Errorf("lowrisk: decode gh pr view: %w", err)
	}
	paths := make([]string, 0, len(v.Files))
	for _, f := range v.Files {
		paths = append(paths, f.Path)
	}
	opened, err := time.Parse(time.RFC3339, v.CreatedAt)
	if err != nil {
		return PR{}, fmt.Errorf("lowrisk: parse createdAt %q: %w", v.CreatedAt, err)
	}
	return PR{ChangedPaths: paths, DiffLOC: v.Additions + v.Deletions, OpenedAt: opened}, nil
}
