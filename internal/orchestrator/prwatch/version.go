package prwatch

import (
	"fmt"
	"regexp"
	"strconv"
)

// semverPattern extracts the first MAJOR.MINOR.PATCH triple from any
// substring. gh's `gh --version` prints `gh version 2.55.0 (...)` —
// the trailing build metadata + the leading "gh version" preamble
// both vary across releases, so the regex anchors only on the triple.
var semverPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// VersionAtLeast reports whether got >= floor where both are
// MAJOR.MINOR.PATCH semver triples. Either argument may carry
// surrounding text (`gh version 2.55.0 ...`); the first triple wins.
//
// Returns an error when no triple is present in `got`, so the caller
// (Watcher.Start, merge.VerifyGhVersion) refuses to start instead of
// silently proceeding against an unknown gh build.
//
// Exported so the merge package can share the same comparator at
// boot — both subsystems gate on the same gh-≥2.40 floor (#656).
func VersionAtLeast(got, floor string) (bool, error) {
	gMaj, gMin, gPat, err := parseSemver(got)
	if err != nil {
		return false, err
	}
	fMaj, fMin, fPat, err := parseSemver(floor)
	if err != nil {
		return false, err
	}
	switch {
	case gMaj != fMaj:
		return gMaj > fMaj, nil
	case gMin != fMin:
		return gMin > fMin, nil
	default:
		return gPat >= fPat, nil
	}
}

func parseSemver(s string) (major, minor, patch int, err error) {
	m := semverPattern.FindStringSubmatch(s)
	if len(m) != 4 {
		return 0, 0, 0, fmt.Errorf("no semver triple in %q", s)
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	patch, _ = strconv.Atoi(m[3])
	return major, minor, patch, nil
}
