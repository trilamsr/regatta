package lowrisk

import (
	"testing"
	"time"
)

// TestParseGhPRView_MapsFilesLOCOpenedAt asserts gh JSON reduces to paths + DiffLOC=additions+deletions + parsed OpenedAt (MAY-86).
func TestParseGhPRView_MapsFilesLOCOpenedAt(t *testing.T) {
	data := []byte(`{"files":[{"path":"docs/a.md"},{"path":"README.md"}],"additions":7,"deletions":3,"createdAt":"2026-06-21T10:00:00Z"}`)
	pr, err := parseGhPRView(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pr.ChangedPaths) != 2 || pr.ChangedPaths[0] != "docs/a.md" {
		t.Fatalf("paths=%v; want [docs/a.md README.md]", pr.ChangedPaths)
	}
	if pr.DiffLOC != 10 {
		t.Fatalf("DiffLOC=%d; want 10", pr.DiffLOC)
	}
	want := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	if !pr.OpenedAt.Equal(want) {
		t.Fatalf("OpenedAt=%v; want %v", pr.OpenedAt, want)
	}
}

// TestParseGhPRView_BadTimestampErrors asserts an unparseable createdAt errors rather than yielding a zero OpenedAt that falsely satisfies soak (MAY-86).
func TestParseGhPRView_BadTimestampErrors(t *testing.T) {
	data := []byte(`{"files":[],"additions":0,"deletions":0,"createdAt":"not-a-time"}`)
	if _, err := parseGhPRView(data); err == nil {
		t.Fatalf("want error on bad createdAt; got nil")
	}
}
